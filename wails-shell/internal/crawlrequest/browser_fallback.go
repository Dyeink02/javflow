package crawlrequest

import (
	"context"
	"fmt"
	"net/http"
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

// Browser fallback owns the explicit chromedp-based compatibility lane for
// age-check / Cloudflare-like page access. Keep this boundary narrow so normal
// HTTP fetch behavior stays readable and testable elsewhere.
//
// Ownership summary:
// 1) execute the explicit browser-based fallback fetch path
// 2) resolve browser executable/cookie/session setup for fallback requests
// 3) keep challenge-bypass behavior separate from the normal HTTP lane
//
// File map for maintainers:
// 1) browser executable discovery and launch configuration
// 2) browser-based page fetch entrypoint
// 3) cookie/session/header setup helpers for fallback requests

const browserPathEnvName = "JAV_AUTO_BROWSER_PATH"

var browserCandidatePaths = []string{
	`C:\Program Files\Google\Chrome\Application\chrome.exe`,
	`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
	`C:\Users\%USERNAME%\AppData\Local\Google\Chrome\Application\chrome.exe`,
}

var browserVerificationMu sync.Mutex

func (c *Client) getPageWithBrowserFallback(ctx context.Context, targetURL string, cookieOverride string, reason string, challengeBody string) (PageResponse, error) {
	timeout := c.options.Timeout
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	interactiveChallenge := IsDriverVerificationQuizResponse(challengeBody)
	if IsAgeVerificationResponse(challengeBody) {
		browserVerificationMu.Lock()
		defer browserVerificationMu.Unlock()

		// Another batch worker may have completed verification while this one
		// waited for the lock. Reuse the shared cookies before opening Chrome.
		if shared := sharedBrowserCookieHeader(); shared != "" {
			c.updateCloudflareCookies(shared)
			if retryResponse, retryErr := c.getPageWithRetry(ctx, targetURL, cookieOverride, 0); retryErr == nil && IsUsablePageResponse(retryResponse) {
				return retryResponse, nil
			}
		}
	}

	browserPath, err := getBrowserExecutablePath()
	if err != nil {
		return PageResponse{}, fmt.Errorf("%s; chromedp fallback unavailable: %w", reason, err)
	}

	ua := firstNonEmpty(c.options.UserAgent, "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")

	// 审计 H-16：复用同一个 allocator，避免每次 fallback 都启动新的 Chrome 进程。
	allocCtx, _, err := c.getOrCreateBrowserAllocator(browserPath, ua, c.options.Proxy, !IsAgeVerificationResponse(challengeBody))
	if err != nil {
		return PageResponse{}, fmt.Errorf("%s; chromedp fallback allocator unavailable: %w", reason, err)
	}
	// allocator 本身保持长生命周期；单次请求再叠加超时 context。
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()
	browserTimeout := timeout + 15*time.Second
	if interactiveChallenge || IsAgeVerificationResponse(challengeBody) {
		browserTimeout = 3 * time.Minute
	}
	requestCtx, requestCancel := context.WithTimeout(browserCtx, browserTimeout)
	defer requestCancel()

	headers := BuildPageRequestHeaders(BuildPageRequestHeadersOptions{
		RequestHeaders:      c.options.Headers,
		ConfigCookie:        c.options.ConfigCookie,
		CookieOverride:      firstNonEmpty(cookieOverride, c.options.CookieOverride),
		CloudflareCookies:   c.options.CloudflareCookies,
		DefaultCookieHeader: DefaultCookieHeader,
	})
	headers["Accept-Language"] = firstNonEmpty(headers["Accept-Language"], "zh-CN,zh;q=0.9,en;q=0.8,en-US;q=0.7")
	delete(headers, "User-Agent")
	chromeHeaders := network.Headers{}
	for key, value := range headers {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			chromeHeaders[key] = value
		}
	}

	antiDetectScript := `
Object.defineProperty(navigator, 'webdriver', { get: () => undefined });
Object.defineProperty(navigator, 'languages', { get: () => ['zh-CN', 'zh', 'en-US'] });
Object.defineProperty(navigator, 'plugins', { get: () => [1, 2, 3] });
window.chrome = window.chrome || { runtime: {} };
`

	var htmlSource string
	var pageURL string
	if err := chromedp.Run(requestCtx,
		network.Enable(),
		network.SetBlockedURLs([]string{"*.png", "*.jpg", "*.jpeg", "*.gif", "*.webp", "*.woff", "*.woff2", "*.ttf", "*googletagmanager*", "*google-analytics*", "*doubleclick*"}),
		emulation.SetUserAgentOverride(ua).WithAcceptLanguage(headers["Accept-Language"]),
		network.SetExtraHTTPHeaders(chromeHeaders),
		chromedp.ActionFunc(func(inner context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(antiDetectScript).Do(inner)
			return err
		}),
		chromedp.ActionFunc(func(inner context.Context) error {
			_, _, _, err := page.Navigate(targetURL).Do(inner)
			return err
		}),
		chromedp.Sleep(2500*time.Millisecond),
		chromedp.ActionFunc(func(inner context.Context) error {
			// JavBus currently gates pages behind a checkbox + submit modal.
			// Keep these selectors here so all callers reuse the same age-check
			// fallback instead of reimplementing it in subscription/organizer code.
			var submitted bool
			_ = chromedp.Evaluate(`(() => {
			  const checkbox = document.querySelector('#ageVerify input[type="checkbox"]');
			  if (!checkbox) return false;
			  checkbox.checked = true;
			  checkbox.dispatchEvent(new Event('change', { bubbles: true }));
			  const form = checkbox.closest('form');
			  if (form) { form.submit(); return true; }
			  return false;
			})()`, &submitted).Do(inner)
			_ = chromedp.Click(`a[href*="age=verified"]`, chromedp.ByQuery).Do(inner)
			_ = chromedp.Click(`a[href*="agecheck"]`, chromedp.ByQuery).Do(inner)
			_ = chromedp.Click(`button.alert_common_btn`, chromedp.ByQuery).Do(inner)
			return nil
		}),
		chromedp.Sleep(1500*time.Millisecond),
	); err != nil {
		return PageResponse{}, fmt.Errorf("%s; chromedp fallback failed: %w", reason, err)
	}

	for {
		if err := chromedp.Run(requestCtx,
			chromedp.OuterHTML("html", &htmlSource, chromedp.ByQuery),
			chromedp.Location(&pageURL),
		); err == nil {
			if IsUsablePageBody(htmlSource) && !IsAgeVerificationResponse(htmlSource) && !IsCloudflareChallengeResponse(http.StatusOK, htmlSource) {
				cookies, cookieErr := network.GetCookies().WithURLs([]string{targetURL, pageURL}).Do(requestCtx)
				if cookieErr == nil {
					parts := make([]string, 0, len(cookies))
					for _, cookie := range cookies {
						if cookie != nil && strings.TrimSpace(cookie.Name) != "" && IsValidCookieValue(cookie.Value) {
							parts = append(parts, cookie.Name+"="+cookie.Value)
						}
					}
					c.updateCloudflareCookies(strings.Join(parts, "; "))
				}
				break
			}
			if IsDriverVerificationQuizResponse(htmlSource) {
				interactiveChallenge = true
			}
		}

		select {
		case <-requestCtx.Done():
			if interactiveChallenge {
				return PageResponse{}, fmt.Errorf("%s；JavBus 已切换为问答验证，请在弹出的浏览器窗口中完成一次验证后重试", reason)
			}
			return PageResponse{}, fmt.Errorf("%s; chromedp fallback failed: %w", reason, requestCtx.Err())
		case <-time.After(750 * time.Millisecond):
		}
	}
	if strings.TrimSpace(pageURL) == "" {
		pageURL = strings.TrimSpace(targetURL)
	}
	return PageResponse{URL: pageURL, StatusCode: http.StatusOK, Body: htmlSource}, nil
}

func shouldUseBrowserFallback(response PageResponse) bool {
	return !IsUsablePageBody(response.Body) ||
		IsCloudflareChallengeResponse(response.StatusCode, response.Body) ||
		IsAgeVerificationResponse(response.Body)
}

func shouldFallbackToBrowserOnError(err error) bool {
	if err == nil {
		return false
	}
	normalized := strings.ToLower(err.Error())
	for _, token := range []string{
		"http 403",
		"http 429",
		"http 503",
		"cloudflare",
		"challenge",
		"age verification",
		"empty response",
		"tls:",
		"tls handshake",
		"first record does not look like a tls handshake",
		"connection refused",
		"no such host",
		"i/o timeout",
		"temporary failure in name resolution",
		"eof",
	} {
		if strings.Contains(normalized, token) {
			return true
		}
	}
	return false
}

func getBrowserExecutablePath() (string, error) {
	visited := map[string]struct{}{}
	for _, candidate := range candidateBrowserPathsForFallback() {
		resolved := strings.TrimSpace(strings.ReplaceAll(candidate, "%USERNAME%", os.Getenv("USERNAME")))
		if resolved == "" {
			continue
		}
		cleaned, err := filepath.Abs(resolved)
		if err == nil {
			resolved = cleaned
		}
		if _, seen := visited[strings.ToLower(resolved)]; seen {
			continue
		}
		visited[strings.ToLower(resolved)] = struct{}{}
		if info, err := os.Stat(resolved); err == nil && !info.IsDir() {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("Google Chrome executable not found; please install Chrome or set %s to chrome.exe", browserPathEnvName)
}

func candidateBrowserPathsForFallback() []string {
	candidates := make([]string, 0, len(browserCandidatePaths)+10)
	if envPath := strings.TrimSpace(os.Getenv(browserPathEnvName)); envPath != "" {
		candidates = append(candidates, envPath)
	}
	if executablePath, err := os.Executable(); err == nil {
		dir := filepath.Dir(executablePath)
		candidates = append(candidates,
			filepath.Join(dir, "chrome.exe"),
			filepath.Join(dir, "browser", "chrome.exe"),
		)
	}
	if workingDir, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(workingDir, "chrome.exe"),
			filepath.Join(workingDir, "browser", "chrome.exe"),
		)
	}
	return append(candidates, browserCandidatePaths...)
}

// getOrCreateBrowserAllocator returns a shared chromedp allocator for this
// client. The first call starts a single Chrome process; subsequent calls reuse
// it until Client.Close() is invoked.
func (c *Client) getOrCreateBrowserAllocator(browserPath, ua, proxy string, headless bool) (context.Context, context.CancelFunc, error) {
	if c == nil {
		return nil, nil, fmt.Errorf("client is nil")
	}
	c.allocatorMu.Lock()
	defer c.allocatorMu.Unlock()

	if c.browserAllocator != nil && c.browserModeSet && c.browserHeadless == headless {
		return c.browserAllocator, c.browserAllocCancel, nil
	}
	if c.browserAllocCancel != nil {
		c.browserAllocCancel()
		c.browserAllocator = nil
		c.browserAllocCancel = nil
	}

	allocatorOptions := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(browserPath),
		// Only age/questionnaire verification is visible. Ordinary Cloudflare
		// recovery remains headless and does not interrupt the desktop.
		chromedp.Flag("headless", headless),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-setuid-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("lang", "zh-CN"),
		chromedp.UserAgent(ua),
	)
	if profileDir := fallbackBrowserProfileDir(); profileDir != "" {
		if err := os.MkdirAll(profileDir, 0o755); err == nil {
			allocatorOptions = append(allocatorOptions, chromedp.UserDataDir(profileDir))
		}
	}
	if proxyServer := normalizeProxyURL(proxy); proxyServer != "" {
		allocatorOptions = append(allocatorOptions, chromedp.Flag("proxy-server", proxyServer))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), allocatorOptions...)
	c.browserAllocator = allocCtx
	c.browserAllocCancel = allocCancel
	c.browserHeadless = headless
	c.browserModeSet = true
	return allocCtx, allocCancel, nil
}

func fallbackBrowserProfileDir() string {
	if configured := strings.TrimSpace(os.Getenv("JAV_AUTO_BROWSER_PROFILE")); configured != "" {
		return configured
	}
	configDir, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(configDir) == "" {
		return ""
	}
	return filepath.Join(configDir, "jav-auto", "browser-profile")
}
