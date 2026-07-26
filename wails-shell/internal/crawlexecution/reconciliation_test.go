package crawlexecution

import "testing"

func makeSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func TestBuildReconciliation(t *testing.T) {
	result := BuildReconciliation(ReconciliationInput{
		ExpectedItemIDs:      makeSet("ABC-001", "ABC-002", "ABC-003"),
		QueuedItemIDs:        makeSet("ABC-001", "ABC-002"),
		ProcessedItemIDs:     makeSet("ABC-001"),
		PersistedItemIDs:     makeSet("ABC-001"),
		DuplicateExpectedIDs: makeSet("ABC-002"),
		ExpectedItemLinkMap: map[string]string{
			"ABC-003": "https://example.com/abc-003",
		},
		ExpectedEntryCount:    3,
		RawDuplicateEntryCount: 1,
		RawDuplicateGroups: []RawDuplicateGroup{
			{ItemID: "ABC-002", Links: []string{"https://example.com/1", "https://example.com/1"}},
		},
	})

	if len(result.ExpectedButNotQueuedIDs) != 1 || result.ExpectedButNotQueuedIDs[0] != "ABC-003" {
		t.Fatalf("unexpected expected-but-not-queued ids: %#v", result.ExpectedButNotQueuedIDs)
	}
	if len(result.ExpectedButNotQueuedLinks) != 1 || result.ExpectedButNotQueuedLinks[0] != "https://example.com/abc-003" {
		t.Fatalf("unexpected expected-but-not-queued links: %#v", result.ExpectedButNotQueuedLinks)
	}
	if len(result.DuplicateExpectedIDs) != 1 || result.DuplicateExpectedIDs[0] != "ABC-002" {
		t.Fatalf("unexpected duplicate ids: %#v", result.DuplicateExpectedIDs)
	}
}
