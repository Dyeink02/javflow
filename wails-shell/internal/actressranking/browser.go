package actressranking

import (
	"context"
	"fmt"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// browser.go owns the browser-assisted ranking fetch path for sites that are
// not reliably parseable through plain HTTP alone.
//
// Ownership summary:
// 1) execute browser-assisted ranking fetches when plain HTTP is insufficient
// 2) manage browser launch/cookie/settle behavior for ranking sources
// 3) keep ranking-browser fallback separate from ranking parsing/cache policy
//
// File map for maintainers:
// 1) browser launch defaults and executable discovery
// 2) browser-assisted ranking fetch entrypoint
// 3) page settle, cookie, and session bootstrap helpers

const (
	defaultBrowserTimeout = 180 * time.Second
	// Ranking pages are captured after the DOM wait below. A short settle delay
	// keeps client-side rendering stable without adding 1.5s to every page.
	defaultSettleDelay    = 800 * time.Millisecond
	defaultUserAgent      = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36"
	defaultAcceptLanguage = "ja-JP,ja;q=0.9,zh-CN;q=0.8,en;q=0.7"
	browserPathEnvName    = "JAV_AUTO_BROWSER_PATH"
)

var browserCandidatePaths = []string{
	`C:\Program Files\Google\Chrome\Application\chrome.exe`,
	`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
	`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
	`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
	`C:\Users\%USERNAME%\AppData\Local\Google\Chrome\Application\chrome.exe`,
	`C:\Users\%USERNAME%\AppData\Local\Microsoft\Edge\Application\msedge.exe`,
}

type officialBrowserPage struct {
	HTML  string
	URL   string
	Title string
}

type officialPageProgressFunc func(stage string, page int, targetURL string)

// officialPageCapture is one ranking page loaded inside a shared browser
// session. Duration is retained for progress diagnostics.
type officialPageCapture struct {
	HTML     string
	PageURL  string
	Title    string
	Duration time.Duration
}

type browserService struct {
	// A single batch owns one browser session. The mutex prevents concurrent
	// ranking requests from launching several Chrome instances at once while
	// still allowing AVfan's independent transport to run normally.
	batchMu sync.Mutex
}

func newBrowserService() *browserService {
	return &browserService{}
}

func expandWindowsPath(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), "%USERNAME%", os.Getenv("USERNAME"))
}

func candidateBrowserPaths() []string {
	candidates := make([]string, 0, len(browserCandidatePaths)+12)

	if envPath := strings.TrimSpace(os.Getenv(browserPathEnvName)); envPath != "" {
		candidates = append(candidates, envPath)
	}

	if executablePath, err := os.Executable(); err == nil {
		executableDir := filepath.Dir(executablePath)
		candidates = append(candidates,
			filepath.Join(executableDir, "chrome.exe"),
			filepath.Join(executableDir, "msedge.exe"),
			filepath.Join(executableDir, "browser", "chrome.exe"),
			filepath.Join(executableDir, "browser", "msedge.exe"),
		)
	}

	if workingDir, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(workingDir, "chrome.exe"),
			filepath.Join(workingDir, "msedge.exe"),
			filepath.Join(workingDir, "browser", "chrome.exe"),
			filepath.Join(workingDir, "browser", "msedge.exe"),
		)
	}

	candidates = append(candidates, browserCandidatePaths...)
	return candidates
}

func getBrowserExecutablePath() (string, error) {
	visited := map[string]struct{}{}
	for _, candidate := range candidateBrowserPaths() {
		resolved := normalizeBrowserPath(expandWindowsPath(candidate))
		if resolved == "" {
			continue
		}
		if _, seen := visited[resolved]; seen {
			continue
		}
		visited[resolved] = struct{}{}
		if info, err := os.Stat(resolved); err == nil && !info.IsDir() {
			return resolved, nil
		}
	}

	return "", fmt.Errorf("未找到可用的 Chrome / Edge 浏览器。可直接使用客户本机已安装的 Chrome 或 Edge；如未安装，请先安装后重试。也可设置环境变量 %s 指向便携版浏览器，例如 chrome.exe。", browserPathEnvName)
}

func normalizeBrowserProxy(proxyValue string) string {
	normalized := strings.TrimSpace(proxyValue)
	if normalized == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(normalized), "http://") || strings.HasPrefix(strings.ToLower(normalized), "https://") {
		return normalized
	}
	return "http://" + normalized
}

func blockedURLPatterns() []string {
	return []string{
		"*.png",
		"*.jpg",
		"*.jpeg",
		"*.gif",
		"*.webp",
		"*.woff",
		"*.woff2",
		"*.ttf",
		"*googletagmanager*",
		"*google-analytics*",
		"*doubleclick*",
		"*analytics.tiktok*",
		"*px.ladsp.com*",
		"*adservice*",
	}
}

func newBrowserContext(proxyValue string) (context.Context, context.CancelFunc, error) {
	browserPath, err := getBrowserExecutablePath()
	if err != nil {
		return nil, nil, err
	}

	allocatorOptions := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(browserPath),
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-setuid-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("lang", "ja-JP"),
	)
	if proxyServer := normalizeBrowserProxy(proxyValue); proxyServer != "" {
		allocatorOptions = append(allocatorOptions, chromedp.Flag("proxy-server", proxyServer))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), allocatorOptions...)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	timeoutCtx, timeoutCancel := context.WithTimeout(browserCtx, defaultBrowserTimeout)

	cancel := func() {
		timeoutCancel()
		browserCancel()
		allocCancel()
	}

	return timeoutCtx, cancel, nil
}

func prepareBrowserContext(ctx context.Context, acceptLanguage string) error {
	if strings.TrimSpace(acceptLanguage) == "" {
		acceptLanguage = defaultAcceptLanguage
	}

	headers := network.Headers(map[string]any{
		"accept-language": acceptLanguage,
		"cache-control":   "no-cache",
		"pragma":          "no-cache",
	})

	antiDetectScript := `
Object.defineProperty(navigator, 'webdriver', { get: () => undefined });
Object.defineProperty(navigator, 'languages', { get: () => ['ja-JP', 'ja', 'zh-CN'] });
Object.defineProperty(navigator, 'plugins', { get: () => [1, 2, 3] });
window.chrome = window.chrome || { runtime: {} };
`

	return chromedp.Run(ctx,
		network.Enable(),
		network.SetBlockedURLs(blockedURLPatterns()),
		emulation.SetUserAgentOverride(defaultUserAgent).WithAcceptLanguage(acceptLanguage),
		network.SetExtraHTTPHeaders(headers),
		chromedp.ActionFunc(func(inner context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(antiDetectScript).Do(inner)
			return err
		}),
	)
}

func (b *browserService) fetchAVFanHTML(targetURL string, proxyValue string) (string, string, string, error) {
	ctx, cancel, err := newBrowserContext(proxyValue)
	if err != nil {
		return "", "", "", err
	}
	defer cancel()

	if err := prepareBrowserContext(ctx, defaultAcceptLanguage); err != nil {
		return "", "", "", err
	}

	var htmlSource string
	var pageURL string
	var pageTitle string

	err = chromedp.Run(ctx,
		chromedp.Navigate(targetURL),
		chromedp.Sleep(defaultSettleDelay),
		chromedp.OuterHTML("html", &htmlSource, chromedp.ByQuery),
		chromedp.Location(&pageURL),
		chromedp.Title(&pageTitle),
	)
	if err != nil {
		return "", "", "", err
	}

	return htmlSource, pageURL, pageTitle, nil
}

func buildAgePassURL(targetURL string) string {
	return "https://www.dmm.co.jp/age_check/=/declared=yes/?rurl=" + neturl.QueryEscape(targetURL)
}

// fetchOfficialRankingHTML completes the DMM/FANZA age declaration in one
// browser session, then opens the requested ranking URL with the declared
// session cookie. Monthly and historical rental rankings share this transport.
func (b *browserService) fetchOfficialRankingHTML(targetURL string, proxyValue string) (string, string, string, error) {
	pages, err := b.fetchOfficialRankingHTMLBatch([]string{targetURL}, proxyValue)
	if err != nil {
		return "", "", "", err
	}
	if len(pages) == 0 {
		return "", "", "", fmt.Errorf("官方榜单未返回页面内容")
	}
	return pages[0].HTML, pages[0].URL, pages[0].Title, nil
}

// fetchOfficialRankingHTMLBatch opens one browser, completes age verification
// once, and captures every requested ranking page in that same cookie session.
// This removes the repeated Chrome cold-start cost from five-page Top 100
// requests and keeps the source's session semantics intact.
func (b *browserService) fetchOfficialRankingHTMLBatch(targetURLs []string, proxyValue string) ([]officialBrowserPage, error) {
	return b.fetchOfficialRankingHTMLBatchWithProgress(targetURLs, proxyValue, nil)
}

func (b *browserService) fetchOfficialRankingHTMLBatchWithProgress(targetURLs []string, proxyValue string, progress officialPageProgressFunc) ([]officialBrowserPage, error) {
	if len(targetURLs) == 0 {
		return nil, fmt.Errorf("官方榜单未提供目标页面")
	}
	b.batchMu.Lock()
	defer b.batchMu.Unlock()

	ctx, cancel, err := newBrowserContext(proxyValue)
	if err != nil {
		return nil, err
	}
	defer cancel()

	if err := prepareBrowserContext(ctx, defaultAcceptLanguage); err != nil {
		return nil, err
	}

	var pageURL string
	if progress != nil {
		progress("age-check.start", 0, targetURLs[0])
	}
	if err := chromedp.Run(ctx,
		chromedp.Navigate(buildAgePassURL(targetURLs[0])),
		chromedp.Sleep(defaultSettleDelay),
		chromedp.Location(&pageURL),
	); err != nil {
		return nil, err
	}

	if strings.Contains(pageURL, "/age_check/") {
		_ = chromedp.Run(ctx,
			chromedp.Click(`a[href*="/age_check/=/declared=yes/"]`, chromedp.ByQuery),
			chromedp.Sleep(defaultSettleDelay),
		)
	}

	if err := chromedp.Run(ctx,
		chromedp.Navigate(targetURLs[0]),
		chromedp.Sleep(defaultSettleDelay),
	); err != nil {
		return nil, err
	}
	if progress != nil {
		progress("age-check.done", 0, targetURLs[0])
	}

	pages := make([]officialBrowserPage, 0, len(targetURLs))
	for index, targetURL := range targetURLs {
		if progress != nil {
			progress("page.start", index+1, targetURL)
		}
		if index > 0 {
			if err := chromedp.Run(ctx,
				chromedp.Navigate(targetURL),
				chromedp.Sleep(defaultSettleDelay),
			); err != nil {
				return nil, err
			}
		}
		_ = chromedp.Run(ctx, chromedp.WaitVisible(".area-rank .rank", chromedp.ByQuery))
		var htmlSource string
		var pageTitle string
		if err := chromedp.Run(ctx,
			chromedp.OuterHTML("html", &htmlSource, chromedp.ByQuery),
			chromedp.Location(&pageURL),
			chromedp.Title(&pageTitle),
		); err != nil {
			return nil, err
		}
		pages = append(pages, officialBrowserPage{HTML: htmlSource, URL: pageURL, Title: pageTitle})
		if progress != nil {
			progress("page.fetched", index+1, targetURL)
		}
	}
	return pages, nil
}

// fetchOfficialRankingPages loads all requested ranking pages in one browser
// session. Page one runs first so region/age failures stop before parallel
// work; remaining pages use separate tabs on the same browser and failed tabs
// receive one serial retry while cookies and proxy state stay warm.
func (b *browserService) fetchOfficialRankingPages(
	pageURLs []string,
	proxyValue string,
	onPageDone func(index int, capture officialPageCapture, err error),
) ([]officialPageCapture, error) {
	if len(pageURLs) == 0 {
		return nil, fmt.Errorf("官方榜单未提供目标页面")
	}
	b.batchMu.Lock()
	defer b.batchMu.Unlock()

	ctx, cancel, err := newBrowserContext(proxyValue)
	if err != nil {
		return nil, err
	}
	defer cancel()
	sessionCtx, sessionCancel := context.WithTimeout(ctx, defaultOfficialTimeout)
	defer sessionCancel()
	ctx = sessionCtx

	if err := prepareBrowserContext(ctx, defaultAcceptLanguage); err != nil {
		return nil, err
	}
	var passURL string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(buildAgePassURL(pageURLs[0])),
		chromedp.Sleep(defaultSettleDelay),
		chromedp.Location(&passURL),
	); err != nil {
		return nil, err
	}
	if strings.Contains(passURL, "/age_check/") {
		_ = chromedp.Run(ctx,
			chromedp.Click(`a[href*="/age_check/=/declared=yes/"]`, chromedp.ByQuery),
			chromedp.Sleep(defaultSettleDelay),
		)
	}

	captures := make([]officialPageCapture, len(pageURLs))
	first, firstErr := captureRankingPage(ctx, pageURLs[0])
	if onPageDone != nil {
		onPageDone(0, first, firstErr)
	}
	if firstErr != nil {
		return nil, firstErr
	}
	captures[0] = first

	type pageResult struct {
		index   int
		capture officialPageCapture
		err     error
	}
	results := make(chan pageResult, len(pageURLs)-1)
	var wg sync.WaitGroup
	for index := 1; index < len(pageURLs); index++ {
		wg.Add(1)
		go func(pageIndex int) {
			defer wg.Done()
			tabCtx, tabCancel := chromedp.NewContext(ctx)
			defer tabCancel()
			capture, pageErr := captureRankingPage(tabCtx, pageURLs[pageIndex])
			if onPageDone != nil {
				onPageDone(pageIndex, capture, pageErr)
			}
			results <- pageResult{index: pageIndex, capture: capture, err: pageErr}
		}(index)
	}
	wg.Wait()
	close(results)

	failed := make([]int, 0)
	for result := range results {
		if result.err != nil {
			failed = append(failed, result.index)
			continue
		}
		captures[result.index] = result.capture
	}
	for _, index := range failed {
		capture, retryErr := captureRankingPage(ctx, pageURLs[index])
		if onPageDone != nil {
			onPageDone(index, capture, retryErr)
		}
		if retryErr != nil {
			return nil, retryErr
		}
		captures[index] = capture
	}
	return captures, nil
}

func captureRankingPage(ctx context.Context, targetURL string) (officialPageCapture, error) {
	startedAt := time.Now()
	var capture officialPageCapture
	if err := chromedp.Run(ctx,
		chromedp.Navigate(targetURL),
		chromedp.Sleep(defaultSettleDelay),
	); err != nil {
		return capture, err
	}
	_ = chromedp.Run(ctx, chromedp.WaitVisible(".area-rank .rank", chromedp.ByQuery))
	if err := chromedp.Run(ctx,
		chromedp.OuterHTML("html", &capture.HTML, chromedp.ByQuery),
		chromedp.Location(&capture.PageURL),
		chromedp.Title(&capture.Title),
	); err != nil {
		return capture, err
	}
	capture.Duration = time.Since(startedAt)
	return capture, nil
}

func normalizeBrowserPath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if absolute, err := filepath.Abs(trimmed); err == nil {
		return absolute
	}
	return filepath.Clean(trimmed)
}
