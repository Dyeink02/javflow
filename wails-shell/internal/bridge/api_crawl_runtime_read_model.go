// Package bridge is the Wails command boundary for current desktop features.
//
// This file owns crawl runtime read-model assembly helpers used by runtime
// query commands.
//
// Ownership summary:
// 1) build crawl runtime read-model projections one by one
// 2) hide service-by-service read-model assembly behind a small helper surface
// 3) keep query callers free from raw service dependency knowledge
//
// File map for maintainers:
// 1) read-model service bag DTO
// 2) per-panel/per-summary builder helpers
package bridge

import (
	"fmt"

	"javflow/internal/crawlquality"
	"javflow/internal/crawlresult"
	"javflow/internal/crawlreview"
	"javflow/internal/crawlruncontext"
	"javflow/internal/crawlstage"
)

// crawlRuntimeReadModel centralizes read-only crawler runtime projections used
// by bootstrap hydration, result/quality panels, and diagnostics queries.
//
// It must not mutate crawl execution state; anything that changes runner state
// belongs in the active command or lifecycle path instead.
type crawlRuntimeReadModel struct {
	runContext *crawlruncontext.Service
	stage      *crawlstage.Service
	result     *crawlresult.Service
	review     *crawlreview.Service
	quality    *crawlquality.Service
}

// buildRunContext / buildStagePanel / buildResultPanel / buildReviewPanel /
// summarizeQuality are read-only projections over the underlying crawl
// read-model services.
func (m crawlRuntimeReadModel) buildRunContext() (crawlruncontext.Context, error) {
	// Read-model builders intentionally expose fully shaped projections one by
	// one so bootstrap/query callers do not need to know underlying services.
	if m.runContext == nil {
		return crawlruncontext.Context{}, fmt.Errorf("crawl run context service is not initialized")
	}
	return m.runContext.Build(), nil
}

func (m crawlRuntimeReadModel) buildStagePanel() (crawlstage.Panel, error) {
	if m.stage == nil {
		return crawlstage.Panel{}, fmt.Errorf("crawl stage service is not initialized")
	}
	return m.stage.Build(), nil
}

func (m crawlRuntimeReadModel) buildResultPanel() (crawlresult.Panel, error) {
	if m.result == nil {
		return crawlresult.Panel{}, fmt.Errorf("crawl result service is not initialized")
	}
	return m.result.Build(), nil
}

func (m crawlRuntimeReadModel) buildReviewPanel() (crawlreview.Panel, error) {
	if m.review == nil {
		return crawlreview.Panel{}, fmt.Errorf("crawl review service is not initialized")
	}
	return m.review.Build(), nil
}

func (m crawlRuntimeReadModel) summarizeQuality(options crawlquality.Options) (crawlquality.Summary, error) {
	if m.quality == nil {
		return crawlquality.Summary{}, fmt.Errorf("crawl quality service is not initialized")
	}
	// Quality summarization stays in the read-model lane: it may inspect output
	// artifacts and logs, but it must not mutate runner/task state.
	return m.quality.Summarize(options)
}
