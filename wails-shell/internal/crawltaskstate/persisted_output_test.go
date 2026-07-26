package crawltaskstate

import (
	"os"
	"path/filepath"
	"testing"

	"javflow/internal/contracts/crawlartifact"
)

func TestInspectPersistedOutputLoadsFilmDataRecords(t *testing.T) {
	outputDir := t.TempDir()
	filmDataPath := filepath.Join(outputDir, crawlartifact.CrawlFilmDataFile)
	content := `[
  {"title":"ABP-001 title","sourceLink":"https://www.javbus.com/ABP-001"},
  {"title":"ABP-002 title","sourceLink":"https://www.javbus.com/ABP-002"},
  {"title":"","sourceLink":""}
]`
	if err := os.WriteFile(filmDataPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write filmData.json: %v", err)
	}

	state, err := InspectPersistedOutput(outputDir)
	if err != nil {
		t.Fatalf("inspect persisted output: %v", err)
	}

	if !state.FilmDataExists {
		t.Fatal("expected filmDataExists=true")
	}
	if state.RecordCount != 3 {
		t.Fatalf("expected record count 3, got %d", state.RecordCount)
	}
	if len(state.Records) != 2 {
		t.Fatalf("expected normalized records 2, got %d", len(state.Records))
	}
	if state.Records[0].SourceLink != "https://www.javbus.com/ABP-001" {
		t.Fatalf("unexpected first source link: %#v", state.Records[0])
	}
	if state.LogMessage == "" {
		t.Fatal("expected log message")
	}
}
