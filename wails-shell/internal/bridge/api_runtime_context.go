package bridge

import (
	"javflow/internal/crawlresult"
	"javflow/internal/crawlreview"
	"javflow/internal/crawlruncontext"
	"javflow/internal/crawlstage"
)

// Runtime-facing panel and context builders are kept together so bootstrap,
// resume, review, and quality-summary queries can read stable crawl-facing
// payloads from one place.
//
// This file is the read-model side of crawl state: it should assemble derived
// UI payloads, not mutate crawl execution state.
//
// Read-model rule:
// keep all crawl panel/context assembly funneled through crawlRuntimeReadModel
// so startup hydration, refresh queries, and diagnostics share one shaping
// contract.
//
// Ownership summary:
// 1) expose read-only crawl panel/context builders from one bridge location
// 2) centralize crawl runtime read-model assembly for bootstrap and refresh
// 3) keep runtime query helpers separate from crawl mutations
//
// File map for maintainers:
// 1) shared crawl read-model constructor
// 2) read-only panel/context query helpers

// crawlRuntimeReadModel is the bridge-local constructor for the shared
// read-model assembler used by runtime-related queries.
func (a *API) crawlRuntimeReadModel() crawlRuntimeReadModel {
	return crawlRuntimeReadModel{
		runContext: a.crawl.crawlRunContext,
		stage:      a.crawl.crawlStage,
		result:     a.crawl.crawlResult,
		review:     a.crawl.crawlReview,
		quality:    a.crawl.crawlQuality,
	}
}

// getCrawlRunContext / getCrawlStagePanel / getCrawlResultPanel /
// getCrawlReviewPanel are pure query helpers. They must stay read-only.
func (a *API) getCrawlRunContext() (crawlruncontext.Context, error) {
	return a.crawlRuntimeReadModel().buildRunContext()
}

func (a *API) getCrawlStagePanel() (crawlstage.Panel, error) {
	return a.crawlRuntimeReadModel().buildStagePanel()
}

func (a *API) getCrawlResultPanel() (crawlresult.Panel, error) {
	return a.crawlRuntimeReadModel().buildResultPanel()
}

func (a *API) getCrawlReviewPanel() (crawlreview.Panel, error) {
	return a.crawlRuntimeReadModel().buildReviewPanel()
}
