package crawlexecution

import (
	"sort"
	"strconv"
	"strings"
)

// Reconciliation models the gap analysis between expected, queued, processed,
// and persisted crawl items. When quality summary numbers disagree, this is the
// first file to inspect.
//
// Ownership summary:
// 1) compute execution-layer reconciliation between expected/queued/processed/persisted items
// 2) preserve duplicate/raw-gap analysis inputs for downstream summaries
// 3) keep reconciliation math separate from runner storage and UI projection
//
// File map for maintainers:
// 1) reconciliation and duplicate-group DTOs
// 2) gap and duplicate computation helpers
// 3) normalized summary and item-set merge helpers

type RawDuplicateGroup struct {
	ItemID string
	Links  []string
}

type Reconciliation struct {
	ExpectedIDs             []string            `json:"expectedIds"`
	QueuedIDs               []string            `json:"queuedIds"`
	ProcessedIDs            []string            `json:"processedIds"`
	PersistedIDs            []string            `json:"persistedIds"`
	ExpectedButNotQueuedIDs []string            `json:"expectedButNotQueuedIds"`
	ExpectedButNotQueuedLinks []string          `json:"expectedButNotQueuedLinks"`
	ExpectedButNotPersistedIDs []string         `json:"expectedButNotPersistedIds"`
	ProcessedButNotPersistedIDs []string        `json:"processedButNotPersistedIds"`
	DuplicateExpectedIDs    []string            `json:"duplicateExpectedIds"`
	ExpectedEntryCount      int                 `json:"expectedEntryCount"`
	RawDuplicateEntryCount  int                 `json:"rawDuplicateEntryCount"`
	RawDuplicateGroups      []RawDuplicateGroup `json:"rawDuplicateGroups"`
}

type ReconciliationInput struct {
	ExpectedItemIDs      map[string]struct{}
	QueuedItemIDs        map[string]struct{}
	ProcessedItemIDs     map[string]struct{}
	PersistedItemIDs     map[string]struct{}
	SkippedItemIDs       map[string]struct{}
	DuplicateExpectedIDs map[string]struct{}
	ExpectedItemLinkMap  map[string]string
	ExpectedEntryCount   int
	RawDuplicateEntryCount int
	RawDuplicateGroups   []RawDuplicateGroup
}

func GetExpectedButNotQueuedIDs(expectedItemIDs map[string]struct{}, queuedItemIDs map[string]struct{}, persistedItemIDs map[string]struct{}) []string {
	result := make([]string, 0)
	for _, itemID := range sortStrings(expectedItemIDs) {
		if !containsID(queuedItemIDs, itemID) && !containsID(persistedItemIDs, itemID) {
			result = append(result, itemID)
		}
	}
	return result
}

func GetExpectedButNotQueuedLinks(expectedItemIDs map[string]struct{}, queuedItemIDs map[string]struct{}, persistedItemIDs map[string]struct{}, expectedItemLinkMap map[string]string) []string {
	result := make([]string, 0)
	for _, itemID := range GetExpectedButNotQueuedIDs(expectedItemIDs, queuedItemIDs, persistedItemIDs) {
		link := strings.TrimSpace(expectedItemLinkMap[itemID])
		if link == "" {
			link = itemID
		}
		if link != "" {
			result = append(result, link)
		}
	}
	return result
}

func GetExpectedButNotPersistedIDs(expectedItemIDs map[string]struct{}, queuedItemIDs map[string]struct{}, persistedItemIDs map[string]struct{}, skippedItemIDs map[string]struct{}) []string {
	baselineIDs := expectedItemIDs
	if len(baselineIDs) == 0 {
		baselineIDs = queuedItemIDs
	}

	result := make([]string, 0)
	for _, itemID := range sortStrings(baselineIDs) {
		if !containsID(persistedItemIDs, itemID) && !containsID(skippedItemIDs, itemID) {
			result = append(result, itemID)
		}
	}
	return result
}

func GetProcessedButNotPersistedIDs(processedItemIDs map[string]struct{}, persistedItemIDs map[string]struct{}, skippedItemIDs map[string]struct{}) []string {
	result := make([]string, 0)
	for _, itemID := range sortStrings(processedItemIDs) {
		if !containsID(persistedItemIDs, itemID) && !containsID(skippedItemIDs, itemID) {
			result = append(result, itemID)
		}
	}
	return result
}

func BuildReconciliation(input ReconciliationInput) Reconciliation {
	return Reconciliation{
		ExpectedIDs:                 sortStrings(input.ExpectedItemIDs),
		QueuedIDs:                   sortStrings(input.QueuedItemIDs),
		ProcessedIDs:                sortStrings(input.ProcessedItemIDs),
		PersistedIDs:                sortStrings(input.PersistedItemIDs),
		ExpectedButNotQueuedIDs:     GetExpectedButNotQueuedIDs(input.ExpectedItemIDs, input.QueuedItemIDs, input.PersistedItemIDs),
		ExpectedButNotQueuedLinks:   GetExpectedButNotQueuedLinks(input.ExpectedItemIDs, input.QueuedItemIDs, input.PersistedItemIDs, input.ExpectedItemLinkMap),
		ExpectedButNotPersistedIDs:  GetExpectedButNotPersistedIDs(input.ExpectedItemIDs, input.QueuedItemIDs, input.PersistedItemIDs, input.SkippedItemIDs),
		ProcessedButNotPersistedIDs: GetProcessedButNotPersistedIDs(input.ProcessedItemIDs, input.PersistedItemIDs, input.SkippedItemIDs),
		DuplicateExpectedIDs:        sortStrings(input.DuplicateExpectedIDs),
		ExpectedEntryCount:          input.ExpectedEntryCount,
		RawDuplicateEntryCount:      input.RawDuplicateEntryCount,
		RawDuplicateGroups:          append([]RawDuplicateGroup(nil), input.RawDuplicateGroups...),
	}
}

func BuildRawDuplicateSummary(groups []RawDuplicateGroup, limit int) string {
	if len(groups) == 0 {
		return ""
	}
	if limit <= 0 {
		limit = 4
	}

	previewItems := make([]string, 0, minInt(limit, len(groups)))
	for _, group := range groups[:minInt(limit, len(groups))] {
		if itemID := strings.TrimSpace(group.ItemID); itemID != "" {
			previewItems = append(previewItems, itemID)
		}
	}

	if len(groups) > limit {
		return strings.Join(previewItems, "、") + " 等 " + itoa(len(groups)) + " 个番号"
	}
	return strings.Join(previewItems, "、")
}

func BuildRawDuplicateReportLines(groups []RawDuplicateGroup) []string {
	lines := make([]string, 0, len(groups))
	for _, group := range groups {
		linkCounts := map[string]int{}
		orderedLinks := make([]string, 0)
		for _, link := range group.Links {
			normalizedLink := strings.TrimSpace(link)
			if normalizedLink == "" {
				continue
			}
			if linkCounts[normalizedLink] == 0 {
				orderedLinks = append(orderedLinks, normalizedLink)
			}
			linkCounts[normalizedLink]++
		}

		parts := make([]string, 0, len(orderedLinks))
		for _, link := range orderedLinks {
			if linkCounts[link] > 1 {
				parts = append(parts, link+" (x"+itoa(linkCounts[link])+")")
			} else {
				parts = append(parts, link)
			}
		}

		lines = append(lines, strings.TrimSpace(group.ItemID)+" | 出现 "+itoa(len(group.Links))+" 次 | "+strings.Join(parts, " | "))
	}
	return lines
}

func BuildRawDuplicateItemIDs(groups []RawDuplicateGroup) []string {
	result := make([]string, 0, len(groups))
	for _, group := range groups {
		itemID := strings.TrimSpace(group.ItemID)
		if itemID != "" {
			result = append(result, itemID)
		}
	}
	sort.Strings(result)
	return result
}

func containsID(values map[string]struct{}, target string) bool {
	if len(values) == 0 {
		return false
	}
	_, ok := values[target]
	return ok
}

func sortStrings(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	sort.Strings(result)
	return result
}

func itoa(value int) string {
	return strconv.Itoa(value)
}
