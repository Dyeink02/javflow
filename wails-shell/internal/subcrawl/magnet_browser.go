// Ownership summary:
//   This file implements domain logic for the javflow backend.
//
// File map for maintainers:
//   1) Domain-specific types and helpers.
//   2) Internal service implementation.
//

package subcrawl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"javflow/internal/crawlrequest"
)

type browserAjaxPayload struct {
	Status       int    `json:"status"`
	StatusText   string `json:"statusText"`
	ResponseText string `json:"responseText"`
	Headers      string `json:"headers"`
	Location     string `json:"location"`
	Error        string `json:"error"`
}

// FetchMagnetWithFallback keeps the exact same primary XHR lane as the main
// JAV crawl path, then escalates to compatibility fallbacks only if that lane
// cannot produce a usable magnet list.
func FetchMagnetWithFallback(
	ctx context.Context,
	client *crawlrequest.Client,
	ajaxURL string,
	detailURL string,
) (*crawlrequest.MagnetResult, error) {
	resp, err := client.GetXMLHttpRequest(ctx, ajaxURL)
	if err == nil {
		if result, buildErr := buildMagnetResultFromText(resp.Body, resp.StatusCode); buildErr == nil {
			return result, nil
		}
	}

	if resp, err = client.GetXMLHttpRequestWithReferer(ctx, ajaxURL, detailURL); err == nil {
		if result, buildErr := buildMagnetResultFromText(resp.Body, resp.StatusCode); buildErr == nil {
			return result, nil
		}
	}

	return fetchMagnetViaBrowser(ctx, client, ajaxURL, detailURL)
}

func fetchMagnetViaBrowser(
	ctx context.Context,
	client *crawlrequest.Client,
	ajaxURL string,
	detailURL string,
) (*crawlrequest.MagnetResult, error) {
	timeout := 45 * time.Second
	browserCtx, cancel := context.WithTimeout(ctx, timeout+10*time.Second)
	defer cancel()

	browserPath, pathErr := findBrowserPath()
	if pathErr != nil {
		return nil, fmt.Errorf("浏览器不可用: %w", pathErr)
	}

	options := client.CloneOptions()
	userAgent := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"
	if strings.TrimSpace(options.UserAgent) != "" {
		userAgent = options.UserAgent
	}

	allocatorOptions := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(browserPath),
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-setuid-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("lang", "zh-CN"),
		chromedp.UserAgent(userAgent),
	)
	if proxy := normalizeProxy(options.Proxy); proxy != "" {
		allocatorOptions = append(allocatorOptions, chromedp.Flag("proxy-server", proxy))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(browserCtx, allocatorOptions...)
	defer allocCancel()

	tabCtx, tabCancel := chromedp.NewContext(allocCtx)
	defer tabCancel()

	cookie := crawlrequest.DefaultCookieHeader
	if strings.TrimSpace(options.ConfigCookie) != "" {
		cookie = options.ConfigCookie
	}

	headers := network.Headers{
		"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8,en-US;q=0.7",
		"Cookie":          cookie,
		"Referer":         detailURL,
	}

	antiDetectScript := `
Object.defineProperty(navigator, 'webdriver', { get: () => undefined });
Object.defineProperty(navigator, 'languages', { get: () => ['zh-CN', 'zh', 'en-US'] });
Object.defineProperty(navigator, 'plugins', { get: () => [1, 2, 3] });
window.chrome = window.chrome || { runtime: {} };
`

	var htmlSource string
	var bodyText string
	if err := chromedp.Run(
		tabCtx,
		network.Enable(),
		network.SetBlockedURLs([]string{
			"*.png", "*.jpg", "*.jpeg", "*.gif", "*.webp", "*.woff", "*.woff2", "*.ttf",
			"*googletagmanager*", "*google-analytics*",
		}),
		emulation.SetUserAgentOverride(userAgent).WithAcceptLanguage("zh-CN,zh;q=0.9,en;q=0.8,en-US;q=0.7"),
		network.SetExtraHTTPHeaders(headers),
		chromedp.ActionFunc(func(inner context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(antiDetectScript).Do(inner)
			return err
		}),
		chromedp.Navigate(ajaxURL),
		chromedp.Sleep(3*time.Second),
		chromedp.OuterHTML("html", &htmlSource, chromedp.ByQuery),
		chromedp.Text("body", &bodyText, chromedp.ByQuery),
	); err != nil {
		return nil, fmt.Errorf("浏览器页面直提磁力失败: %w", err)
	}

	combined := strings.TrimSpace(htmlSource + "\n" + bodyText)
	if combined == "" {
		return nil, fmt.Errorf("浏览器页面直提磁力响应为空")
	}

	if result, buildErr := buildMagnetResultFromText(combined, http.StatusOK); buildErr == nil {
		return result, nil
	}

	return nil, fmt.Errorf("浏览器页面直提磁力失败: 未找到磁力链接")
}

func buildMagnetResultFromText(responseText string, status int) (*crawlrequest.MagnetResult, error) {
	magnetLinks := crawlrequest.ExtractMagnetLinks(responseText)
	sizeTokens := crawlrequest.ExtractSizeTokens(responseText)
	if len(magnetLinks) == 0 {
		preview := strings.TrimSpace(responseText)
		if len(preview) > 220 {
			preview = preview[:220]
		}
		return nil, fmt.Errorf(
			"浏览器响应中未找到磁力链接 status=%d preview=%s",
			status,
			compactDiagnosticText(preview),
		)
	}

	candidates := crawlrequest.BuildParsedMagnetCandidates(magnetLinks, sizeTokens)
	return crawlrequest.BuildMagnetResult(candidates, false, 3), nil
}

func runBrowserAjaxScript(tabCtx context.Context, script string) (browserAjaxPayload, error) {
	var raw any
	if err := chromedp.Run(tabCtx, chromedp.Evaluate(script, &raw, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
		return p.WithAwaitPromise(true)
	})); err != nil {
		return browserAjaxPayload{}, fmt.Errorf("浏览器磁力脚本执行失败: %w", err)
	}

	payloadBytes, err := normalizeBrowserAjaxPayload(raw)
	if err != nil {
		return browserAjaxPayload{}, err
	}

	var payload browserAjaxPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return browserAjaxPayload{}, fmt.Errorf("浏览器磁力响应解析失败: %w", err)
	}
	return payload, nil
}

func normalizeBrowserAjaxPayload(raw any) ([]byte, error) {
	switch typed := raw.(type) {
	case nil:
		return nil, fmt.Errorf("浏览器磁力响应为空")
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil, fmt.Errorf("浏览器磁力响应为空")
		}
		return []byte(typed), nil
	default:
		payloadBytes, err := json.Marshal(typed)
		if err != nil {
			return nil, fmt.Errorf("浏览器磁力响应序列化失败: %w", err)
		}
		if strings.TrimSpace(string(payloadBytes)) == "" || string(payloadBytes) == "null" {
			return nil, fmt.Errorf("浏览器磁力响应为空")
		}
		return payloadBytes, nil
	}
}

func compactDiagnosticText(value string) string {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.ReplaceAll(trimmed, "\r", " ")
	trimmed = strings.ReplaceAll(trimmed, "\n", " ")
	for strings.Contains(trimmed, "  ") {
		trimmed = strings.ReplaceAll(trimmed, "  ", " ")
	}
	return trimmed
}

func findBrowserPath() (string, error) {
	if envPath := strings.TrimSpace(os.Getenv("JAV_AUTO_BROWSER_PATH")); envPath != "" {
		if info, err := os.Stat(envPath); err == nil && !info.IsDir() {
			return envPath, nil
		}
	}

	candidates := []string{
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
	}

	if username := os.Getenv("USERNAME"); username != "" {
		candidates = append(candidates,
			fmt.Sprintf(`C:\Users\%s\AppData\Local\Google\Chrome\Application\chrome.exe`, username),
		)
	}

	if execPath, err := os.Executable(); err == nil {
		dir := filepath.Dir(execPath)
		candidates = append([]string{
			filepath.Join(dir, "chrome.exe"),
			filepath.Join(dir, "browser", "chrome.exe"),
		}, candidates...)
	}

	visited := map[string]struct{}{}
	for _, candidate := range candidates {
		abs, err := filepath.Abs(candidate)
		if err != nil {
			abs = candidate
		}
		lower := strings.ToLower(abs)
		if _, seen := visited[lower]; seen {
			continue
		}
		visited[lower] = struct{}{}
		if info, err := os.Stat(abs); err == nil && !info.IsDir() {
			return abs, nil
		}
	}

	return "", fmt.Errorf("未找到 Google Chrome 浏览器，请先安装 Chrome，或设置 JAV_AUTO_BROWSER_PATH 指向 chrome.exe")
}

func normalizeProxy(proxy string) string {
	trimmed := strings.TrimSpace(proxy)
	if trimmed == "" {
		return ""
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "http://" + trimmed
	}
	if parsed, err := url.Parse(trimmed); err == nil && parsed.Host != "" {
		return parsed.String()
	}
	return ""
}
