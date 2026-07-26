package crawltaskstate

import (
	"strings"
	"time"

	"javflow/internal/crawlexecution"
)

// builder.go owns translation from live runner state into persisted task-state
// snapshots used by resume and diagnostics.
//
// Ownership summary:
// 1) convert live runner state into persisted snapshots for resume and diagnostics
// 2) shape full-versus-light snapshot modes from one builder path
// 3) keep snapshot assembly separate from storage paths and restore logic
//
// File map for maintainers:
// 1) snapshot builder entrypoint
// 2) full-versus-light state selection rules
// 3) link-set/status/default value normalization helpers
//
// BuildSnapshot translates the live runner state into a persisted snapshot.
//
// "full" mode keeps extra reconciliation/validation detail for restore and
// operator review; lighter modes keep only the state needed for continuity.
// If recovery or validation output looks incomplete, start by checking which
// snapshot mode was written here.
func BuildSnapshot(params BuilderParams) Snapshot {
	includeFullState := params.Mode == crawlexecution.SnapshotModeFull
	updatedAt := params.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}

	links := SnapshotLinks{
		Expected:         cloneStringSliceNonNil(params.ExpectedDetailLinks),
		Queued:           cloneStringSliceNonNil(params.QueuedDetailLinks),
		Processed:        cloneStringSliceNonNil(params.ProcessedDetailLinks),
		Persisted:        cloneStringSliceNonNil(params.PersistedDetailLinks),
		PersistedFilmIDs: cloneStringSliceNonNil(params.PersistedFilmIDs),
		SkippedIDs:       cloneStringSliceNonNil(params.SkippedItemIDs),
	}
	if includeFullState {
		links.ExpectedIDs = cloneStringSlice(params.Reconciliation.ExpectedIDs)
		links.QueuedIDs = cloneStringSlice(params.Reconciliation.QueuedIDs)
		links.ProcessedIDs = cloneStringSlice(params.Reconciliation.ProcessedIDs)
		links.PersistedIDs = cloneStringSlice(params.Reconciliation.PersistedIDs)
	}

	itemsPerPage := params.Config.ItemsPerPage
	if itemsPerPage <= 0 && params.ExpectedItemsPerPage != nil {
		itemsPerPage = *params.ExpectedItemsPerPage
	}
	taskTemplate := strings.TrimSpace(params.Config.TaskTemplate)
	if taskTemplate == "" {
		taskTemplate = "balanced"
	}

	snapshot := Snapshot{
		SchemaVersion: 2,
		AppVersion:    strings.TrimSpace(params.AppVersion),
		Status:        strings.TrimSpace(params.Status),
		Message:       strings.TrimSpace(params.Message),
		UpdatedAt:     updatedAt.Format(time.RFC3339),
		StartedAt:     strings.TrimSpace(params.StartedAt),
		Config: SnapshotConfig{
			Base:             stringPointerOrNil(params.Config.Base),
			Output:           strings.TrimSpace(params.Config.Output),
			Limit:            params.Config.Limit,
			TotalPages:       params.Config.TotalPages,
			ItemsPerPage:     itemsPerPage,
			Parallel:         params.Config.Parallel,
			Delay:            params.Config.Delay,
			Timeout:          params.Config.Timeout,
			SecondValidation: params.Config.SecondValidation,
			TaskTemplate:     taskTemplate,
		},
		Progress: SnapshotProgress{
			NextPageIndex:        params.PageIndex,
			ExpectedItemsPerPage: cloneIntPointer(params.ExpectedItemsPerPage),
			Queued:               params.FilmsQueued,
			Attempted:            params.FilmsAttempted,
			Completed:            params.FilmCount,
			Skipped:              len(params.SkippedItemIDs),
		},
		Links:         links,
		FailedDetails: cloneFailedDetails(params.FailedDetails),
		PageAudits:    clonePageAudits(params.PageAudits),
	}

	if includeFullState {
		reconciliation := cloneReconciliation(params.Reconciliation)
		snapshot.Reconciliation = &reconciliation
		snapshot.MissingItems = cloneStringSlice(params.MissingItems)
		snapshot.ValidationReport = cloneValidationReport(params.ValidationReport)
	}

	return snapshot
}

// cloneStringSlice makes a defensive copy for persisted state fields.
func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, len(values))
	copy(result, values)
	return result
}

// cloneStringSliceNonNil keeps JSON arrays present even when empty.
func cloneStringSliceNonNil(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	return cloneStringSlice(values)
}

// cloneIntPointer avoids aliasing snapshot pointers back into live state.
func cloneIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	next := *value
	return &next
}

// stringPointerOrNil keeps empty strings out of persisted optional fields.
func stringPointerOrNil(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// cloneFailedDetails keeps failure records immutable across state writes.
func cloneFailedDetails(items []FailedDetailRecord) []FailedDetailRecord {
	if len(items) == 0 {
		return []FailedDetailRecord{}
	}
	result := make([]FailedDetailRecord, len(items))
	copy(result, items)
	return result
}

// clonePageAudits keeps audit rows stable between live and persisted state.
func clonePageAudits(items []PageAuditRecord) []PageAuditRecord {
	if len(items) == 0 {
		return []PageAuditRecord{}
	}
	result := make([]PageAuditRecord, len(items))
	copy(result, items)
	return result
}

// cloneValidationReport keeps the validation report detached from live state.
func cloneValidationReport(report *ResultValidationReport) *ResultValidationReport {
	if report == nil {
		return nil
	}
	next := *report
	next.MissingItems = cloneStringSliceNonNil(report.MissingItems)
	next.ExpectedButNotQueuedItems = cloneStringSliceNonNil(report.ExpectedButNotQueuedItems)
	next.ProcessedButNotPersistedItems = cloneStringSliceNonNil(report.ProcessedButNotPersistedItems)
	if len(report.LowConfidencePages) == 0 {
		next.LowConfidencePages = []int{}
	} else {
		next.LowConfidencePages = append([]int(nil), report.LowConfidencePages...)
	}
	return &next
}

// cloneDuplicateGroups normalizes duplicate groups for persisted snapshots.
func cloneDuplicateGroups(groups []DuplicateExpectedEntryGroup) []DuplicateExpectedEntryGroup {
	if len(groups) == 0 {
		return nil
	}
	result := make([]DuplicateExpectedEntryGroup, len(groups))
	for index, group := range groups {
		result[index] = DuplicateExpectedEntryGroup{
			ItemID: strings.TrimSpace(group.ItemID),
			Links:  cloneStringSliceNonNil(group.Links),
		}
	}
	return result
}

// cloneReconciliation keeps the resume math copied into the persisted contract.
func cloneReconciliation(snapshot ReconciliationSnapshot) ReconciliationSnapshot {
	return ReconciliationSnapshot{
		ExpectedIDs:                 cloneStringSliceNonNil(snapshot.ExpectedIDs),
		QueuedIDs:                   cloneStringSliceNonNil(snapshot.QueuedIDs),
		ProcessedIDs:                cloneStringSliceNonNil(snapshot.ProcessedIDs),
		PersistedIDs:                cloneStringSliceNonNil(snapshot.PersistedIDs),
		ExpectedButNotQueuedIDs:     cloneStringSlice(snapshot.ExpectedButNotQueuedIDs),
		ExpectedButNotPersistedIDs:  cloneStringSlice(snapshot.ExpectedButNotPersistedIDs),
		ProcessedButNotPersistedIDs: cloneStringSlice(snapshot.ProcessedButNotPersistedIDs),
		DuplicateExpectedIDs:        cloneStringSlice(snapshot.DuplicateExpectedIDs),
		ExpectedEntryCount:          snapshot.ExpectedEntryCount,
		RawDuplicateEntryCount:      snapshot.RawDuplicateEntryCount,
		RawDuplicateGroups:          cloneDuplicateGroups(snapshot.RawDuplicateGroups),
	}
}
