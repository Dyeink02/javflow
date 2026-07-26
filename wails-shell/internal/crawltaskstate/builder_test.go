package crawltaskstate

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"javflow/internal/crawlexecution"
)

func intPointer(value int) *int {
	return &value
}

func TestBuildSnapshotLightModeOmitsFullState(t *testing.T) {
	snapshot := BuildSnapshot(BuilderParams{
		AppVersion:           "0.27.0",
		Status:               "running",
		Message:              "testing",
		StartedAt:            "2026-05-01T10:00:00Z",
		PageIndex:            2,
		ExpectedItemsPerPage: intPointer(30),
		FilmsQueued:          10,
		FilmsAttempted:       6,
		FilmCount:            5,
		ExpectedDetailLinks:  []string{"https://www.javbus.com/ABP-001"},
		QueuedDetailLinks:    []string{"https://www.javbus.com/ABP-001"},
		ProcessedDetailLinks: []string{},
		PersistedDetailLinks: []string{},
		PersistedFilmIDs:     []string{"ABP-001"},
		SkippedItemIDs:       []string{"ABP-002"},
		Reconciliation: ReconciliationSnapshot{
			ExpectedIDs: []string{"ABP-001"},
		},
		Mode:      crawlexecution.SnapshotModeLight,
		UpdatedAt: time.Date(2026, 5, 1, 10, 5, 0, 0, time.UTC),
	})

	if snapshot.Progress.Skipped != 1 {
		t.Fatalf("expected skipped=1, got %d", snapshot.Progress.Skipped)
	}
	if snapshot.Reconciliation != nil {
		t.Fatalf("expected reconciliation to be omitted in light mode")
	}
	if snapshot.Links.ExpectedIDs != nil {
		t.Fatalf("expected expectedIds to be omitted in light mode")
	}
	if len(snapshot.Links.Expected) != 1 || snapshot.Links.Expected[0] != "https://www.javbus.com/ABP-001" {
		t.Fatalf("unexpected expected links: %#v", snapshot.Links.Expected)
	}

	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	jsonText := string(encoded)
	if strings.Contains(jsonText, "reconciliation") {
		t.Fatalf("light snapshot should not contain reconciliation: %s", jsonText)
	}
	if strings.Contains(jsonText, "expectedIds") {
		t.Fatalf("light snapshot should not contain expectedIds: %s", jsonText)
	}
}

func TestBuildSnapshotFullModeIncludesFullState(t *testing.T) {
	report := &ResultValidationReport{
		GeneratedAt:                   "2026-05-01T10:10:00Z",
		MissingItems:                  []string{"ABP-003"},
		ExpectedButNotQueuedItems:     []string{},
		ProcessedButNotPersistedItems: []string{},
		LowConfidencePages:            []int{2},
		Summary:                       "summary",
	}
	snapshot := BuildSnapshot(BuilderParams{
		AppVersion: "0.27.0",
		Status:     "completed",
		Message:    "done",
		StartedAt:  "2026-05-01T10:00:00Z",
		Config: BuilderConfigInput{
			Base:         "https://www.javbus.com",
			Output:       `C:\test\output`,
			ItemsPerPage: 30,
		},
		PageIndex:            9,
		ExpectedItemsPerPage: intPointer(30),
		FilmsQueued:          30,
		FilmsAttempted:       30,
		FilmCount:            28,
		ExpectedDetailLinks:  []string{"a"},
		QueuedDetailLinks:    []string{"a"},
		ProcessedDetailLinks: []string{"a"},
		PersistedDetailLinks: []string{"a"},
		PersistedFilmIDs:     []string{"ABP-001"},
		SkippedItemIDs:       []string{"ABP-002"},
		Reconciliation: ReconciliationSnapshot{
			ExpectedIDs:            []string{"ABP-001"},
			QueuedIDs:              []string{"ABP-001"},
			ProcessedIDs:           []string{"ABP-001"},
			PersistedIDs:           []string{"ABP-001"},
			DuplicateExpectedIDs:   []string{"ABP-009"},
			ExpectedEntryCount:     30,
			RawDuplicateEntryCount: 2,
			RawDuplicateGroups: []DuplicateExpectedEntryGroup{
				{ItemID: "ABP-009", Links: []string{"x", "y"}},
			},
		},
		MissingItems:     []string{"ABP-003"},
		ValidationReport: report,
		Mode:             crawlexecution.SnapshotModeFull,
		UpdatedAt:        time.Date(2026, 5, 1, 10, 10, 0, 0, time.UTC),
	})

	if snapshot.Reconciliation == nil {
		t.Fatal("expected reconciliation in full mode")
	}
	if snapshot.ValidationReport == nil || snapshot.ValidationReport.Summary != "summary" {
		t.Fatalf("unexpected validation report: %#v", snapshot.ValidationReport)
	}
	if len(snapshot.MissingItems) != 1 || snapshot.MissingItems[0] != "ABP-003" {
		t.Fatalf("unexpected missing items: %#v", snapshot.MissingItems)
	}
	if snapshot.Config.Base == nil || *snapshot.Config.Base != "https://www.javbus.com" {
		t.Fatalf("unexpected base: %#v", snapshot.Config.Base)
	}
}
