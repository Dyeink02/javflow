package crawltaskstate

import (
	"fmt"

	"javflow/internal/crawlidentity"
	"javflow/internal/crawlindex"
)

// RestoredState is the runner-facing resume payload derived from Snapshot.
//
// It rebuilds queue/progress/link sets into the exact shape the live runner
// needs, so restore callers do not have to understand persisted JSON details.
//
// Ownership summary:
// 1) translate persisted task-state snapshots back into runner-facing restore payloads
// 2) rebuild queue/progress/link sets from stored state
// 3) keep restore semantics separate from snapshot schema and storage layout
//
// File map for maintainers:
// 1) runner-facing restored-state DTO
// 2) snapshot-to-runtime reconstruction entrypoint
// 3) queue/link rebuild and duplicate-tracking helpers
type RestoredState struct {
	ShouldRestore        bool                    `json:"shouldRestore"`
	PageIndex            int                     `json:"pageIndex"`
	ExpectedItemsPerPage *int                    `json:"expectedItemsPerPage"`
	FilmsQueued          int                     `json:"filmsQueued"`
	FilmsAttempted       int                     `json:"filmsAttempted"`
	FilmCount            int                     `json:"filmCount"`
	PageAudits           []PageAuditRecord       `json:"pageAudits"`
	ValidationReport     *ResultValidationReport `json:"validationReport,omitempty"`
	FailedDetails        []FailedDetailRecord    `json:"failedDetails"`
	ExpectedLinks        []string                `json:"expectedLinks"`
	ExpectedItemIDs      []string                `json:"expectedItemIds"`
	QueuedLinks          []string                `json:"queuedLinks"`
	QueuedItemIDs        []string                `json:"queuedItemIds"`
	QueuedFilmIDs        []string                `json:"queuedFilmIds"`
	ProcessedLinks       []string                `json:"processedLinks"`
	ProcessedItemIDs     []string                `json:"processedItemIds"`
	PersistedLinks       []string                `json:"persistedLinks"`
	PersistedItemIDs     []string                `json:"persistedItemIds"`
	PersistedFilmIDs     []string                `json:"persistedFilmIds"`
	SkippedItemIDs       []string                `json:"skippedItemIds"`
	DuplicateExpectedIDs []string                `json:"duplicateExpectedIds"`
	PendingDetailLinks   []string                `json:"pendingDetailLinks"`
	LogMessage           string                  `json:"logMessage"`
}

// RestoreFromSnapshot converts persisted state back into runnable in-memory
// state.
//
// Completed snapshots deliberately do not restore: review can still read them,
// but the crawl should not resume once final output is considered done.
func RestoreFromSnapshot(snapshot *Snapshot) RestoredState {
	if snapshot == nil {
		return RestoredState{}
	}
	if snapshot.Status == "completed" {
		return RestoredState{}
	}

	state := RestoredState{
		ShouldRestore:        true,
		PageIndex:            maxInt(1, intValue(snapshot.Progress.NextPageIndex)),
		ExpectedItemsPerPage: cloneIntPointer(snapshot.Progress.ExpectedItemsPerPage),
		FilmsQueued:          maxInt(0, snapshot.Progress.Queued),
		FilmsAttempted:       maxInt(0, snapshot.Progress.Attempted),
		FilmCount:            maxInt(0, snapshot.Progress.Completed),
		PageAudits:           clonePageAudits(snapshot.PageAudits),
		ValidationReport:     cloneValidationReport(snapshot.ValidationReport),
		FailedDetails:        cloneFailedDetails(snapshot.FailedDetails),
		QueuedLinks:          cloneStringSliceNonNil(snapshot.Links.Queued),
		ProcessedLinks:       cloneStringSliceNonNil(snapshot.Links.Processed),
		PersistedLinks:       cloneStringSliceNonNil(snapshot.Links.Persisted),
		PersistedFilmIDs:     cloneStringSliceNonNil(snapshot.Links.PersistedFilmIDs),
		SkippedItemIDs:       cloneStringSliceNonNil(snapshot.Links.SkippedIDs),
	}

	expectedSourceLinks := snapshot.Links.Expected
	if len(expectedSourceLinks) == 0 {
		expectedSourceLinks = snapshot.Links.Queued
	}
	state.ExpectedLinks = cloneStringSliceNonNil(expectedSourceLinks)

	expectedItemIDs := orderedStringSet{}
	for _, link := range state.ExpectedLinks {
		expectedItemIDs.add(crawlindex.GetDetailItemID(link))
	}
	for _, itemID := range snapshot.Links.ExpectedIDs {
		expectedItemIDs.add(itemID)
	}
	state.ExpectedItemIDs = expectedItemIDs.values()

	queuedItemIDs := orderedStringSet{}
	queuedFilmIDs := orderedStringSet{}
	// Pending detail links are derived from queued items that never made it into
	// persisted output. That keeps resume work focused on actual gaps.
	for _, link := range state.QueuedLinks {
		queuedItemIDs.add(crawlindex.GetDetailItemID(link))
		queuedFilmIDs.add(crawlidentity.ExtractFilmID(link))
	}
	for _, itemID := range snapshot.Links.QueuedIDs {
		queuedItemIDs.add(itemID)
	}
	state.QueuedItemIDs = queuedItemIDs.values()
	state.QueuedFilmIDs = queuedFilmIDs.values()

	processedItemIDs := orderedStringSet{}
	for _, link := range state.ProcessedLinks {
		processedItemIDs.add(crawlindex.GetDetailItemID(link))
	}
	for _, itemID := range snapshot.Links.ProcessedIDs {
		processedItemIDs.add(itemID)
	}
	state.ProcessedItemIDs = processedItemIDs.values()

	persistedItemIDs := orderedStringSet{}
	for _, link := range state.PersistedLinks {
		persistedItemIDs.add(crawlindex.GetDetailItemID(link))
	}
	for _, itemID := range snapshot.Links.PersistedIDs {
		persistedItemIDs.add(itemID)
	}
	for _, filmID := range state.PersistedFilmIDs {
		persistedItemIDs.add(filmID)
	}
	state.PersistedItemIDs = persistedItemIDs.values()

	duplicateExpectedIDs := orderedStringSet{}
	if snapshot.Reconciliation != nil {
		for _, itemID := range snapshot.Reconciliation.DuplicateExpectedIDs {
			duplicateExpectedIDs.add(itemID)
		}
	}
	state.DuplicateExpectedIDs = duplicateExpectedIDs.values()

	for _, link := range state.QueuedLinks {
		itemID := crawlindex.GetDetailItemID(link)
		if !persistedItemIDs.has(itemID) {
			state.PendingDetailLinks = append(state.PendingDetailLinks, link)
		}
	}

	state.LogMessage = fmt.Sprintf("已从任务状态文件恢复：页码 %d，待补任务 %d 条。", state.PageIndex, len(state.PendingDetailLinks))
	return state
}

type orderedStringSet struct {
	items []string
	seen  map[string]struct{}
}

// add keeps resume/reconciliation order stable while deduplicating IDs.
func (s *orderedStringSet) add(value string) {
	if s.seen == nil {
		s.seen = map[string]struct{}{}
	}
	if value == "" {
		return
	}
	if _, exists := s.seen[value]; exists {
		return
	}
	s.seen[value] = struct{}{}
	s.items = append(s.items, value)
}

// has checks whether the resumed state already accounted for a given ID.
func (s *orderedStringSet) has(value string) bool {
	if s.seen == nil {
		return false
	}
	_, exists := s.seen[value]
	return exists
}

// values returns the stable deduplicated slice used by resume state.
func (s *orderedStringSet) values() []string {
	return cloneStringSliceNonNil(s.items)
}

// intValue keeps the restore math explicit at call sites.
func intValue(value int) int {
	return value
}

// maxInt keeps restored counters from drifting below expected minimums.
func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
