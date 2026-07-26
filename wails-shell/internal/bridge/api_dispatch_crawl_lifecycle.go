package bridge

import "fmt"

// handleCrawlLifecycleCommand owns commands that can start, stop, or restart a
// live crawl execution lane. Keep this separate from probes and support
// mutations so runtime regressions can be isolated from diagnostic tooling.
//
// Lifecycle rule:
// any command that changes runner/task-controller execution state belongs here,
// even if the UI button lives in a settings-oriented panel.
//
// Ownership summary:
// 1) route crawl lifecycle mutation commands
// 2) keep start/stop/restart dispatch separate from read-only runtime queries
// 3) preserve one bridge entry for live crawl-state mutations
//
// File map for maintainers:
// 1) top-level crawl lifecycle dispatcher
// 2) command-to-controller/runner routing branches
func (a *API) handleCrawlLifecycleCommand(command string, payload map[string]any) (string, bool, error) {
	switch command {
	case "crawl:go-native-start":
		if a.crawl.crawlRunner == nil {
			return "", true, fmt.Errorf("Go native crawl runner is not initialized")
		}
		result, err := a.handleGoCrawlNativeStart(payload)
		return result, true, err

	case "app:start-crawl":
		result, err := a.handleTaskControlledCrawlStart(payload)
		return result, true, err

	case "app:restart-crawl":
		result, err := a.handleTaskControlledCrawlRestart(payload)
		return result, true, err

	case "app:stop-crawl":
		result, err := a.handleTaskControlledCrawlStop()
		return result, true, err
	}

	return "", false, nil
}
