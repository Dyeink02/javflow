package crawltaskstate

import (
	"strings"
	"testing"
)

func TestRestoreFromSnapshotIgnoresCompletedSnapshot(t *testing.T) {
	restored := RestoreFromSnapshot(&Snapshot{
		Status: "completed",
	})
	if restored.ShouldRestore {
		t.Fatalf("completed snapshot should not restore: %#v", restored)
	}
}

func TestRestoreFromSnapshotBuildsPendingLinksAndIdentities(t *testing.T) {
	restored := RestoreFromSnapshot(&Snapshot{
		Status: "running",
		Progress: SnapshotProgress{
			NextPageIndex: 8,
			Queued:        12,
			Attempted:     10,
			Completed:     9,
		},
		Links: SnapshotLinks{
			Expected:         []string{"https://www.javbus.com/ABP-001", "https://www.javbus.com/ABP-002"},
			ExpectedIDs:      []string{"ABP-003"},
			Queued:           []string{"https://www.javbus.com/ABP-001", "https://www.javbus.com/ABP-002"},
			QueuedIDs:        []string{"ABP-004"},
			Processed:        []string{"https://www.javbus.com/ABP-001"},
			Persisted:        []string{"https://www.javbus.com/ABP-001"},
			PersistedFilmIDs: []string{"ABP-001"},
			SkippedIDs:       []string{"ABP-099"},
		},
		Reconciliation: &ReconciliationSnapshot{
			DuplicateExpectedIDs: []string{"ABP-777"},
		},
		FailedDetails: []FailedDetailRecord{
			{Item: "ABP-002", SourceLink: "https://www.javbus.com/ABP-002", Reason: "failed"},
		},
	})

	if !restored.ShouldRestore {
		t.Fatalf("expected restore to be enabled: %#v", restored)
	}
	if restored.PageIndex != 8 {
		t.Fatalf("expected page index 8, got %d", restored.PageIndex)
	}
	if len(restored.PendingDetailLinks) != 1 || restored.PendingDetailLinks[0] != "https://www.javbus.com/ABP-002" {
		t.Fatalf("unexpected pending detail links: %#v", restored.PendingDetailLinks)
	}
	if len(restored.ExpectedItemIDs) != 3 {
		t.Fatalf("unexpected expected item ids: %#v", restored.ExpectedItemIDs)
	}
	if len(restored.QueuedFilmIDs) != 2 {
		t.Fatalf("unexpected queued film ids: %#v", restored.QueuedFilmIDs)
	}
	if len(restored.DuplicateExpectedIDs) != 1 || restored.DuplicateExpectedIDs[0] != "ABP-777" {
		t.Fatalf("unexpected duplicate expected ids: %#v", restored.DuplicateExpectedIDs)
	}
	if !strings.Contains(restored.LogMessage, "待补任务 1 条") {
		t.Fatalf("unexpected log message: %q", restored.LogMessage)
	}
}
