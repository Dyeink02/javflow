// Ownership summary:
//   This file implements domain logic for the javflow backend.
//
// File map for maintainers:
//   1) Domain-specific types and helpers.
//   2) Internal service implementation.
//

package crawlrequest

import (
	"context"
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
	"github.com/chromedp/chromedp"

	"javflow/internal/crawlparse"
)

// BuildMagnetAjaxURL centralizes the canonical JAV ajax endpoint assembly so
// every caller uses the same detail-page metadata mapping.
func BuildMagnetAjaxURL(metadata crawlparse.Metadata, baseURL string) string {
	origin := strings.TrimRight(baseURL, "/")
	if parsed, err := url.Parse(baseURL); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		origin = parsed.Scheme + "://" + parsed.Host
	}
	normalizedImageParam := NormalizeAjaxImageParam(metadata.Img)
	return fmt.Sprintf(
		"%s/ajax/uncledatoolsbyajax.php?gid=%s&lang=zh&img=%s&uc=%s&floor=%d",
		origin,
		metadata.GID,
		normalizedImageParam,
		metadata.UC,
		time.Now().UnixNano()%1000+1,
	)
}

// FetchMagnetCandidates reproduces the main JAV crawler's canonical magnet
// lookup lane: one AJAX request, parse the returned payload, and hand back the
// raw candidates for downstream policy/filter selection.
func FetchMagnetCandidates(ctx context.Context, client *Client, metadata crawlparse.Metadata, baseURL string) ([]ParsedMagnetCandidate, error) {
	if client == nil {
		return nil, fmt.Errorf("magnet fetch client is not configured")
	}

	ajaxURL := BuildMagnetAjaxURL(metadata, baseURL)
	resp, err := client.GetXMLHttpRequest(ctx, ajaxURL)
	if err != nil {
		return nil, fmt.Errorf("magnet xhr request failed: %w", err)
	}

	magnetLinks := ExtractMagnetLinks(resp.Body)
	sizeTokens := ExtractSizeTokens(resp.Body)
	if len(magnetLinks) == 0 || len(sizeTokens) == 0 {
		return nil, fmt.Errorf("no magnet links parsed")
	}

	return BuildParsedMagnetCandidates(magnetLinks, sizeTokens), nil
}

// FetchMagnetCandidatesWithFallback keeps the canonical JAV crawler lane
// intact: direct XHR, referer XHR, then browser HTML fallback.
func FetchMagnetCandidatesWithFallback(ctx context.Context, client *Client, metadata crawlparse.Metadata, baseURL string, detailURL string) ([]ParsedMagnetCandidate, error) {
	if client == nil {
		return nil, fmt.Errorf("magnet fetch client is not configured")
	}

	ajaxURL := BuildMagnetAjaxURL(metadata, baseURL)
	if resp, err := client.GetXMLHttpRequest(ctx, ajaxURL); err == nil {
		if candidates, buildErr := buildParsedMagnetCandidatesFromText(resp.Body, resp.StatusCode); buildErr == nil {
			return candidates, nil
		}
	}

	refererURL := strings.TrimSpace(detailURL)
	if refererURL == "" {
		refererURL = strings.TrimSpace(baseURL)
	}
	if resp, err := client.GetXMLHttpRequestWithReferer(ctx, ajaxURL, refererURL); err == nil {
		if candidates, buildErr := buildParsedMagnetCandidatesFromText(resp.Body, resp.StatusCode); buildErr == nil {
			return candidates, nil
		}
	}

	return fetchMagnetCandidatesViaBrowser(ctx, client, ajaxURL, refererURL)
}

// FetchMagnetResult is the convenience wrapper for callers that want the
// canonical selected magnet result without additional filtering.
func FetchMagnetResult(ctx context.Context, client *Client, metadata crawlparse.Metadata, baseURL string) (*MagnetResult, error) {
	candidates, err := FetchMagnetCandidates(ctx, client, metadata, baseURL)
	if err != nil {
		return nil, err
	}
	return BuildMagnetResult(candidates, false, 3), nil
}

// FetchMagnetResultWithFallback is the direct selected-result variant of the
// canonical JAV magnet lane.
func FetchMagnetResultWithFallback(ctx context.Context, client *Client, metadata crawlparse.Metadata, baseURL string, detailURL string) (*MagnetResult, error) {
	candidates, err := FetchMagnetCandidatesWithFallback(ctx, client, metadata, baseURL, detailURL)
	if err != nil {
		return nil, err
	}
	return BuildMagnetResult(candidates, false, 3), nil
}

func buildParsedMagnetCandidatesFromText(responseText string, status int) ([]ParsedMagnetCandidate, error) {
	magnetLinks := ExtractMagnetLinks(responseText)
	sizeTokens := ExtractSizeTokens(responseText)
	if len(magnetLinks) == 0 {
		preview := strings.TrimSpace(responseText)
		if len(preview) > 220 {
			preview = preview[:220]
		}
		return nil, fmt.Errorf("browser response contained no magnet links: status=%d preview=%s", status, compactDiagnosticText(preview))
	}
	return BuildParsedMagnetCandidates(magnetLinks, sizeTokens), nil
}

func fetchMagnetCandidatesViaBrowser(ctx context.Context, client *Client, ajaxURL string, refererURL string) ([]ParsedMagnetCandidate, error) {
	timeout := 45 * time.Second
	browserCtx, cancel := context.WithTimeout(ctx, timeout+10*time.Second)
	defer cancel()

	browserPath, pathErr := findBrowserPath()
	if pathErr != nil {
		return nil, fmt.Errorf("browser unavailable: %w", pathErr)
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

	cookie := DefaultCookieHeader
	if strings.TrimSpace(options.ConfigCookie) != "" {
		cookie = options.ConfigCookie
	}

	headers := network.Headers{
		"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8,en-US;q=0.7",
		"Cookie":          cookie,
		"Referer":         refererURL,
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
		return nil, fmt.Errorf("browser magnet request failed: %w", err)
	}

	combined := strings.TrimSpace(htmlSource + "\n" + bodyText)
	if combined == "" {
		return nil, fmt.Errorf("browser response empty")
	}

	return buildParsedMagnetCandidatesFromText(combined, http.StatusOK)
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

	return "", fmt.Errorf("Google Chrome not found, install Chrome or set JAV_AUTO_BROWSER_PATH")
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
