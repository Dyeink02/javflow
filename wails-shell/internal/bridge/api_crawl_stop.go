package bridge

import "strings"

// Stop/restart paths are kept apart from startup because they blend runner
// ownership with compatibility fallback and are a common source of regressions.
//
// Ownership summary:
// 1) handle crawl stop/restart transitions at the bridge boundary
// 2) coordinate Go-runner stop behavior with sidecar fallback when needed
// 3) keep stop/restart logic separate from start payload setup
//
// File map for maintainers:
// 1) stop path bridge helper
// 2) restart path bridge helper
func (a *API) handleCrawlStop() (string, error) {
	a.setGoTaskExecutionMode(a.currentExecutionMode())
	if a.hasActiveRunner() {
		if err := a.stopAndWait(); err != nil {
			return "", err
		}
		a.setExecutionMode(crawlExecutionModeIdle)
		a.emitLogEntry("info", "Go native runner stopped.")
		return marshalResult(map[string]any{"status": "stopped", "mode": "go-native"})
	}

	snapshotMode := strings.TrimSpace(a.currentExecutionMode())
	if snapshotMode == crawlExecutionModeCloudflareCompat && a.runtime.manager != nil {
		a.emitLogEntry("info", "Go native runner is not active; trying Node sidecar stop.")
		result, err := a.fallbackToLegacySidecar("crawl", "stop", nil)
		if err == nil {
			a.setExecutionMode(crawlExecutionModeIdle)
		}
		return result, err
	}
	a.setExecutionMode(crawlExecutionModeIdle)
	return marshalResult(map[string]any{"status": "already-stopped", "mode": "none"})
}

func (a *API) handleCrawlRestart(payload map[string]any) (string, error) {
	_ = a.stopAndWait()
	return a.handleCrawlStart(payload)
}
