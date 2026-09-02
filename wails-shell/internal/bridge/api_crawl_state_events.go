package bridge

import (
	"encoding/json"
	"time"
)

// Shared crawl event emitters stay in one place so UI feed stability is easier
// to audit when multiple panels subscribe to the same raw topics.

// `crawl.state` is the shared raw state feed. Frontend panels may consume
// richer derived events as well, but this event must stay stable because it is
// also used by resume, diagnostics, and compatibility fallbacks.
// resolveCrawlExecutionMode and emitLogEntry keep shared crawl event payload
// normalization in one place for all lifecycle paths.
//
// Ownership summary:
// 1) centralize raw crawl state/log event emission helpers
// 2) resolve execution mode and shared event payload normalization
// 3) keep crawl event envelope writing separate from lifecycle command code
//
// File map for maintainers:
// 1) execution-mode resolution helper
// 2) shared crawl state/log event emitters
func (a *API) resolveCrawlExecutionMode(payload map[string]any) string {
	needCF := payloadBool(payload["cloudflare"]) || payloadBool(payload["useCloudflareBypass"])
	if needCF {
		return crawlExecutionModeCloudflareCompat
	}

	// Magnet metadata inspection is implemented by the maintained legacy
	// RequestHandler (magnet2torrent-js). Route this explicitly to the sidecar
	// compatibility lane until the equivalent Go torrent-metadata reader exists;
	// otherwise the checkbox would be accepted by the UI but silently ignored by
	// the normal Go runner.
	if payloadBool(payload["magnetContentValidation"]) {
		return crawlExecutionModeCloudflareCompat
	}

	if a.crawl.crawlFetch == nil {
		return crawlExecutionModeCloudflareCompat
	}

	baseURL := resolveCrawlerBaseURL(payload)
	if a.needsCloudflareBypass(baseURL) {
		return crawlExecutionModeCloudflareCompat
	}

	return crawlExecutionModeGoNative
}

func (a *API) emitLogEntry(level string, message string) {
	raw, err := json.Marshal(map[string]any{
		"level":   level,
		"message": message,
	})
	if err != nil {
		return
	}
	a.runtime.bus.Publish("", "log", "crawl.log", "", "", "", time.Now().Format(time.RFC3339), raw)
}
