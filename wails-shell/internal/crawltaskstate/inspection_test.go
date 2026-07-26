package crawltaskstate

import (
	"path/filepath"
	"testing"
)

func TestInspectReturnsRestoreSummary(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "runtime-state")
	outputDir := filepath.Join(t.TempDir(), "output")

	manager, err := NewManager(outputDir, ManagerOptions{StateRoot: stateRoot})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	snapshot := Snapshot{
		Status: "running",
		Progress: SnapshotProgress{
			NextPageIndex: 7,
			Queued:        12,
			Attempted:     9,
			Completed:     8,
		},
		Links: SnapshotLinks{
			Queued:           []string{"https://www.javbus.com/ABP-001", "https://www.javbus.com/ABP-002"},
			Persisted:        []string{"https://www.javbus.com/ABP-001"},
			PersistedFilmIDs: []string{"ABP-001"},
			Expected:         []string{"https://www.javbus.com/ABP-001", "https://www.javbus.com/ABP-002"},
			SkippedIDs:       []string{},
		},
		FailedDetails: []FailedDetailRecord{
			{Item: "ABP-002", SourceLink: "https://www.javbus.com/ABP-002", Reason: "failed"},
		},
		PageAudits: []PageAuditRecord{},
	}
	if err := manager.SaveSnapshot(snapshot, true); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	if err := manager.SaveValidationReport(ResultValidationReport{Summary: "ok"}); err != nil {
		t.Fatalf("save validation report: %v", err)
	}

	inspection, err := Inspect(outputDir, ManagerOptions{StateRoot: stateRoot})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}

	if !inspection.TaskStateExists {
		t.Fatal("expected task state to exist")
	}
	if !inspection.ValidationReportExists {
		t.Fatal("expected validation report to exist")
	}
	if !inspection.CanRestore {
		t.Fatalf("expected can restore: %#v", inspection)
	}
	if inspection.RestorePageIndex != 7 {
		t.Fatalf("expected page index 7, got %d", inspection.RestorePageIndex)
	}
	if inspection.RestorePendingDetailCount != 1 {
		t.Fatalf("expected pending count 1, got %d", inspection.RestorePendingDetailCount)
	}
	if inspection.RestoreFailedDetailCount != 1 {
		t.Fatalf("expected failed count 1, got %d", inspection.RestoreFailedDetailCount)
	}
}
