package bridge

import (
	"context"
	"fmt"
	"time"

	"javflow/internal/crawlrequest"
)

// Crawl mode selection is kept separate from the task-controller wrappers so
// future debugging can answer two different questions quickly:
// 1) which controller handled the request
// 2) which execution path was selected
//
// Ownership summary:
// 1) select the crawl execution mode at the bridge boundary
// 2) isolate the explicit fallback transition into the sidecar compatibility lane
// 3) keep mode-selection diagnostics out of runner internals
//
// File map for maintainers:
// 1) sidecar fallback boundary helper
// 2) crawl start mode selection entrypoint
// 3) Cloudflare/challenge probe helpers

// fallbackToLegacySidecar is the explicit compatibility boundary to the old
// Node/Electron-origin crawl path. Keep this narrow so future pure-Go work does
// not spread sidecar assumptions across the bridge.
func (a *API) fallbackToLegacySidecar(domain string, action string, payload map[string]any) (string, error) {
	// Compatibility path inherited from raawaa/jav-scrapy. Ordinary crawl
	// traffic should stay on the Go runner; the sidecar remains for flows that
	// still need the Node/Puppeteer implementation, mainly Cloudflare bypass.
	a.setGoTaskExecutionMode(crawlExecutionModeCloudflareCompat)
	return a.callSidecar(domain, action, payload)
}

// dispatchCrawlStart decides between the stable Go runner path and the legacy
// compatibility sidecar path. This is the main mode switch kept visible for
// diagnostics because many crawl bugs reduce to "which path actually started".
func (a *API) dispatchCrawlStart(payload map[string]any) (string, error) {
	mode := a.resolveCrawlExecutionMode(payload)
	if mode == crawlExecutionModeCloudflareCompat {
		a.setGoTaskExecutionMode(crawlExecutionModeCloudflareCompat)
		a.emitLogEntry("info", "[diagnostic] executionMode=compat-sidecar controllerMode=go-task-controller")
		if payloadBool(payload["magnetContentValidation"]) {
			a.emitLogEntry("info", "已启用磁力内容严格校验，将读取候选 torrent 文件列表并仅保留已验证的单番号磁力。")
		} else {
			a.emitLogEntry("info", "Cloudflare bypass enabled or challenge detected, using Node sidecar compatibility path.")
		}
		result, err := a.fallbackToLegacySidecar("crawl", "start", payload)
		if err != nil {
			return "", fmt.Errorf("Node sidecar start failed: %w", err)
		}
		return result, nil
	}

	a.setGoTaskExecutionMode(crawlExecutionModeGoNative)
	a.emitLogEntry("info", "[diagnostic] executionMode=go-native controllerMode=go-task-controller")
	a.emitLogEntry("info", "Using Go native crawl runner.")
	return a.handleGoCrawlNativeStart(payload)
}

func (a *API) needsCloudflareBypass(targetURL string) bool {
	if a.crawl.crawlFetch == nil {
		return true
	}

	// This is only a probe. Network errors are treated as "no challenge
	// detected" so transient proxy/site failures still surface through the
	// normal Go runner instead of silently forcing the legacy sidecar.
	proxy := ""
	configCookie := ""
	timeout := 8 * time.Second
	settingsMap := a.loadBridgeSettingsSnapshot()
	if v, ok := settingsMap["proxy"].(string); ok {
		proxy = v
	}
	if v, ok := settingsMap["cookie"].(string); ok {
		configCookie = v
	}
	if v, ok := settingsMap["timeout"].(float64); ok && v > 0 {
		timeout = time.Duration(v) * time.Millisecond
	}

	client, err := crawlrequest.NewClient(crawlrequest.PageRequestOptions{
		Proxy:        proxy,
		ConfigCookie: configCookie,
		Timeout:      timeout,
	})
	if err != nil {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	resp, err := client.GetPage(ctx, targetURL, "")
	if err != nil {
		return false
	}
	if crawlrequest.IsAgeVerificationResponse(resp.Body) {
		return true
	}
	if crawlrequest.IsCloudflareChallengeResponse(resp.StatusCode, resp.Body) {
		return true
	}
	return false
}
