package actressranking

import (
	"context"
	"fmt"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
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
	defaultBrowserTimeout = 70 * time.Second
	defaultSettleDelay    = 1500 * time.Millisecond
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

type browserService struct{}

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

func (b *browserService) fetchOfficialMonthlyHTML(proxyValue string) (string, string, string, error) {
	ctx, cancel, err := newBrowserContext(proxyValue)
	if err != nil {
		return "", "", "", err
	}
	defer cancel()

	if err := prepareBrowserContext(ctx, defaultAcceptLanguage); err != nil {
		return "", "", "", err
	}

	var pageURL string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(buildAgePassURL(officialMonthlyURL)),
		chromedp.Sleep(defaultSettleDelay),
		chromedp.Location(&pageURL),
	); err != nil {
		return "", "", "", err
	}

	if strings.Contains(pageURL, "/age_check/") {
		_ = chromedp.Run(ctx,
			chromedp.Click(`a[href*="/age_check/=/declared=yes/"]`, chromedp.ByQuery),
			chromedp.Sleep(defaultSettleDelay),
		)
	}

	if err := chromedp.Run(ctx,
		chromedp.Navigate(officialMonthlyURL),
		chromedp.Sleep(defaultSettleDelay),
	); err != nil {
		return "", "", "", err
	}

	_ = chromedp.Run(ctx, chromedp.WaitVisible(".area-rank .rank", chromedp.ByQuery))

	var htmlSource string
	var pageTitle string
	err = chromedp.Run(ctx,
		chromedp.OuterHTML("html", &htmlSource, chromedp.ByQuery),
		chromedp.Location(&pageURL),
		chromedp.Title(&pageTitle),
	)
	if err != nil {
		return "", "", "", err
	}

	return htmlSource, pageURL, pageTitle, nil
}

func isBrowserProxyError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(message, "err_proxy_connection_failed") ||
		strings.Contains(message, "proxy connection failed") ||
		strings.Contains(message, "proxy") ||
		strings.Contains(message, "tunnel connection failed") ||
		strings.Contains(message, "socks") ||
		strings.Contains(message, "econnrefused")
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
