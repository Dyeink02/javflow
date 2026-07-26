package avsubscriptionv2

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"javflow/internal/contracts/crawlartifact"
	runtimepaths "javflow/internal/runtime"
)

func TestImportFromOutputReadsAllCodesFromHiddenJSONBeforeVisibleJSON(t *testing.T) {
	userDataDir := t.TempDir()
	outputDir := t.TempDir()
	visibleJSON := `[
		{"title":"AAA-001 visible","actress":["演员一"]},
		{"title":"AAA-002 visible","actress":["演员一"]}
	]`
	if err := os.WriteFile(filepath.Join(outputDir, crawlartifact.CrawlFilmDataFile), []byte(visibleJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	internalPaths := crawlartifact.ResolveInternalArtifactPaths(userDataDir, outputDir)
	if err := os.MkdirAll(filepath.Dir(internalPaths.FilmDataPath), 0o755); err != nil {
		t.Fatal(err)
	}
	hiddenJSON := `[
		{"title":"AAA-001 hidden","actress":["演员一"]},
		{"title":"AAA-002 hidden","actress":["演员一"]},
		{"title":"AAA-003 hidden","actress":["演员一"]}
	]`
	if err := os.WriteFile(internalPaths.FilmDataPath, []byte(hiddenJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	service := NewService(runtimepaths.Paths{UserData: userDataDir}, nil)
	result, err := service.ImportFromOutput(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if result.FilmDataPath != internalPaths.FilmDataPath {
		t.Fatalf("expected hidden source path %q, got %q", internalPaths.FilmDataPath, result.FilmDataPath)
	}
	if len(result.Subscription.BaselineCodes) != 3 {
		t.Fatalf("expected all 3 hidden JSON codes, got %#v", result.Subscription.BaselineCodes)
	}

	if err := os.Remove(internalPaths.FilmDataPath); err != nil {
		t.Fatal(err)
	}
	fallbackCodes := extractCodesFromOutput(outputDir, userDataDir)
	if len(fallbackCodes) != 2 {
		t.Fatalf("expected 2 visible fallback codes, got %#v", fallbackCodes)
	}
}

func TestImportFromOutputPreservesProfileCountAsCurrentTotal(t *testing.T) {
	userDataDir := t.TempDir()
	outputDir := t.TempDir()

	filmData := `[
		{"title":"AAA-001","actress":["演员一"]},
		{"title":"AAA-002","actress":["演员一"]},
		{"title":"AAA-003","actress":["演员一"]}
	]`
	if err := os.WriteFile(filepath.Join(outputDir, crawlartifact.CrawlFilmDataFile), []byte(filmData), 0o644); err != nil {
		t.Fatal(err)
	}

	profile := map[string]any{
		"schemaVersion":  1,
		"runId":          "run-test-001",
		"completedAt":    "2026-01-01T00:00:00Z",
		"actressName":    "演员一",
		"crawlURL":       "https://example.com/star/abc",
		"targetCount":    2,
		"completedCount": 2,
		"itemsPerPage":   30,
		"totalPages":     1,
		"outputDir":      outputDir,
		"filmDataPath":   filepath.Join(outputDir, crawlartifact.CrawlFilmDataFile),
		"siteBase":       "https://example.com",
	}
	profileBytes, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, crawlartifact.CrawlProfileFile), profileBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	service := NewService(runtimepaths.Paths{UserData: userDataDir}, nil)
	result, err := service.ImportFromOutput(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if result.Subscription.BaselineCount != 3 {
		t.Fatalf("expected 3 baseline codes, got %d", result.Subscription.BaselineCount)
	}
	if result.Subscription.CurrentTotal != 2 {
		t.Fatalf("expected currentTotal=2 (profile count), got %d", result.Subscription.CurrentTotal)
	}
	if result.Subscription.CurrentObservedCount != 3 {
		t.Fatalf("expected currentObservedCount=3 (max of count and baseline), got %d", result.Subscription.CurrentObservedCount)
	}
}

func TestRepeatedImportsMergeEveryHistoricalJSONCode(t *testing.T) {
	service := NewService(runtimepaths.Paths{UserData: t.TempDir()}, nil)
	first, err := service.Upsert(Subscription{ActressName: "演员一", BaselineCodes: []string{"AAA-001", "AAA-002"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Upsert(Subscription{ID: first.ID, ActressName: "演员一", BaselineCodes: []string{"AAA-002", "AAA-003"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.BaselineCodes) != 3 {
		t.Fatalf("expected historical code union, got %#v", second.BaselineCodes)
	}
}
