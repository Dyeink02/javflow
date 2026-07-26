// Package crawlexecution owns Go-side runtime planning and execution summaries
// for the crawler.
//
// Boundary rule:
// - runtime summary text that depends on crawl state may live here
// - ordinary static desktop UI wording should stay in the frontend text modules
// - if a label is wrong before any crawl begins, do not start debugging here
//
// Ownership summary:
// 1) build one index-page execution plan from discovered link counts and limits
// 2) centralize page-level queue decisions and log messages
// 3) keep per-page planning separate from outer index loop orchestration
//
// File map for maintainers:
// 1) page execution-plan input/output DTOs
// 2) pre/post queue decision assembly
// 3) expected-items-per-page and log message resolution
package crawlexecution

import "javflow/internal/crawlindex"

type IndexPageExecutionPlanInput struct {
	CurrentPage                 int  `json:"currentPage"`
	TargetTotalPages            int  `json:"targetTotalPages"`
	ExpectedCount               *int `json:"expectedCount"`
	LinksCount                  int  `json:"linksCount"`
	TrackedLinksCount           int  `json:"trackedLinksCount"`
	NewLinksCount               int  `json:"newLinksCount"`
	FilmsQueued                 int  `json:"filmsQueued"`
	FilmLimit                   int  `json:"filmLimit"`
	ResumeExisting              bool `json:"resumeExisting"`
	CurrentExpectedItemsPerPage *int `json:"currentExpectedItemsPerPage"`
}

type IndexPageExecutionPlan struct {
	ShouldSetExpectedItemsPerPage bool                                `json:"shouldSetExpectedItemsPerPage"`
	ExpectedItemsPerPageValue     *int                                `json:"expectedItemsPerPageValue"`
	LogMessages                   []string                            `json:"logMessages"`
	PreQueueDecision              *crawlindex.IndexProcessingDecision `json:"preQueueDecision,omitempty"`
	QueueCount                    int                                 `json:"queueCount"`
	PostQueueDecision             *crawlindex.IndexProcessingDecision `json:"postQueueDecision,omitempty"`
}

func ResolveIndexPageExecutionPlan(input IndexPageExecutionPlanInput) IndexPageExecutionPlan {
	logMessages := []string{}
	shouldSetExpectedItemsPerPage := input.CurrentExpectedItemsPerPage == nil && input.LinksCount > 0

	skippedPersistedCount := input.TrackedLinksCount - input.NewLinksCount
	if skippedPersistedCount > 0 {
		logMessages = append(logMessages, "已跳过 "+itoa(skippedPersistedCount)+" 个已完成影片，仅补抓未完成内容。")
	}
	if input.TrackedLinksCount > 0 && input.TrackedLinksCount < input.LinksCount {
		logMessages = append(logMessages, "当前为限量模式，本页仅追踪前 "+itoa(input.TrackedLinksCount)+" 个链接，剩余 "+itoa(input.LinksCount-input.TrackedLinksCount)+" 个链接不计入本次任务。")
	}

	expectedItemsPerPageValue := input.CurrentExpectedItemsPerPage
	if shouldSetExpectedItemsPerPage {
		expectedItemsPerPageValue = &input.LinksCount
	}

	buildDecision := func(newLinksCount int, filmsQueued int) *crawlindex.IndexProcessingDecision {
		decision := crawlindex.ResolveIndexProcessingDecision(crawlindex.IndexProcessingDecisionInput{
			CurrentPage:      input.CurrentPage,
			TargetTotalPages: input.TargetTotalPages,
			ExpectedCount:    input.ExpectedCount,
			LinksCount:       input.LinksCount,
			NewLinksCount:    newLinksCount,
			ResumeExisting:   input.ResumeExisting,
			FilmLimit:        input.FilmLimit,
			FilmsQueued:      filmsQueued,
		})
		return &decision
	}

	if input.LinksCount == 0 {
		return IndexPageExecutionPlan{
			ShouldSetExpectedItemsPerPage: false,
			ExpectedItemsPerPageValue:     expectedItemsPerPageValue,
			LogMessages:                   logMessages,
			PreQueueDecision:              buildDecision(0, input.FilmsQueued),
			QueueCount:                    0,
			PostQueueDecision:             nil,
		}
	}

	if input.NewLinksCount == 0 {
		return IndexPageExecutionPlan{
			ShouldSetExpectedItemsPerPage: shouldSetExpectedItemsPerPage,
			ExpectedItemsPerPageValue:     expectedItemsPerPageValue,
			LogMessages:                   logMessages,
			PreQueueDecision:              buildDecision(0, input.FilmsQueued),
			QueueCount:                    0,
			PostQueueDecision:             nil,
		}
	}

	if input.FilmLimit > 0 && input.FilmsQueued >= input.FilmLimit {
		return IndexPageExecutionPlan{
			ShouldSetExpectedItemsPerPage: shouldSetExpectedItemsPerPage,
			ExpectedItemsPerPageValue:     expectedItemsPerPageValue,
			LogMessages:                   logMessages,
			PreQueueDecision:              buildDecision(input.NewLinksCount, input.FilmsQueued),
			QueueCount:                    0,
			PostQueueDecision:             nil,
		}
	}

	queueDecision := crawlindex.ResolveIndexQueueLimitDecision(input.FilmLimit, input.FilmsQueued, input.NewLinksCount)
	queueCount := queueDecision.QueueCount
	filmsQueuedAfterQueue := input.FilmsQueued + queueCount

	return IndexPageExecutionPlan{
		ShouldSetExpectedItemsPerPage: shouldSetExpectedItemsPerPage,
		ExpectedItemsPerPageValue:     expectedItemsPerPageValue,
		LogMessages:                   logMessages,
		PreQueueDecision:              nil,
		QueueCount:                    queueCount,
		PostQueueDecision:             buildDecision(queueCount, filmsQueuedAfterQueue),
	}
}
