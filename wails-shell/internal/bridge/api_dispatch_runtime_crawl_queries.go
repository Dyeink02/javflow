package bridge

import "fmt"

// Runtime crawl queries expose read-only crawl state to the UI. They assemble
// panels, summaries, and task snapshots, but must not own crawl mutations.
//
// Query boundary:
// these commands may aggregate multiple runtime/read-model sources into one UI
// payload, but they should never "fix" crawl state by mutating task/runner
// services during a query.
//
// Ownership summary:
// 1) route read-only crawl runtime query commands
// 2) aggregate runtime/read-model projections for UI consumption
// 3) keep query assembly separate from crawl mutations
//
// File map for maintainers:
// 1) runtime crawl query dispatcher
// 2) per-panel/per-summary query entrypoints
func (a *API) handleRuntimeCrawlQueryCommand(command string, payload map[string]any) (string, bool, error) {
	switch command {
	case "app:get-crawl-run-context":
		return a.handleGetCrawlRunContextCommand()

	case "app:get-crawl-stage-panel":
		return a.handleGetCrawlStagePanelCommand()

	case "app:get-crawl-result-panel":
		return a.handleGetCrawlResultPanelCommand()

	case "app:get-crawl-review-panel":
		return a.handleGetCrawlReviewPanelCommand()

	case "app:get-run-quality-summary":
		return a.handleGetRunQualitySummaryCommand(payload)

	case "app:get-crawl-task-snapshot":
		return a.handleGetCrawlTaskSnapshotCommand()
	}

	return "", false, nil
}

func (a *API) handleGetCrawlRunContextCommand() (string, bool, error) {
	runContext, err := a.getCrawlRunContext()
	if err != nil {
		return "", true, err
	}
	result, err := marshalResult(runContext)
	return result, true, err
}

func (a *API) handleGetCrawlStagePanelCommand() (string, bool, error) {
	stagePanel, err := a.getCrawlStagePanel()
	if err != nil {
		return "", true, err
	}
	result, err := marshalResult(stagePanel)
	return result, true, err
}

func (a *API) handleGetCrawlResultPanelCommand() (string, bool, error) {
	resultPanel, err := a.getCrawlResultPanel()
	if err != nil {
		return "", true, err
	}
	result, err := marshalResult(resultPanel)
	return result, true, err
}

func (a *API) handleGetCrawlReviewPanelCommand() (string, bool, error) {
	reviewPanel, err := a.getCrawlReviewPanel()
	if err != nil {
		return "", true, err
	}
	result, err := marshalResult(reviewPanel)
	return result, true, err
}

func (a *API) handleGetRunQualitySummaryCommand(payload map[string]any) (string, bool, error) {
	options, err := a.buildCrawlQualityOptions(payload)
	if err != nil {
		return "", true, err
	}
	summary, err := a.crawlRuntimeReadModel().summarizeQuality(options)
	if err != nil {
		return "", true, err
	}
	result, err := marshalResult(summary)
	return result, true, err
}

func (a *API) handleGetCrawlTaskSnapshotCommand() (string, bool, error) {
	if a.crawl.crawlTask == nil {
		return "", true, fmt.Errorf("crawl task service is not initialized")
	}
	result, err := marshalResult(a.crawl.crawlTask.Snapshot())
	return result, true, err
}
