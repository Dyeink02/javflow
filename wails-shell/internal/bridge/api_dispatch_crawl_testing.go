package bridge

import "fmt"

// handleCrawlTestingCommand owns fetch/page diagnostic probes. These commands
// should never be the first place to inspect when a live crawl start/stop flow
// regresses; they exist specifically to keep parser/network verification
// debuggable without reading lifecycle code.
//
// Probe rule:
// diagnostic commands may exercise fetch/parser code paths, but they should not
// enqueue task-controller state or mutate persistent run artifacts.
//
// Ownership summary:
// 1) route crawl testing/diagnostic probe commands
// 2) isolate probe-only flows from lifecycle and support commands
// 3) keep parser/network diagnostics from mutating persistent crawl state
//
// File map for maintainers:
// 1) crawl testing command dispatcher
// 2) probe command branch routing
func (a *API) handleCrawlTestingCommand(command string, payload map[string]any) (string, bool, error) {
	switch command {
	case "crawl:go-test-index-page":
		if a.crawl.crawlFetch == nil {
			return "", true, fmt.Errorf("Go crawl fetch service is not initialized")
		}
		result, err := a.handleGoCrawlTestIndex(payload)
		return result, true, err

	case "crawl:go-test-detail-page":
		if a.crawl.crawlFetch == nil {
			return "", true, fmt.Errorf("Go crawl fetch service is not initialized")
		}
		result, err := a.handleGoCrawlTestDetail(payload)
		return result, true, err
	}

	return "", false, nil
}
