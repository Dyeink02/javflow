package avsubscriptionv2

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

func TestImportFromOutputPreservesRawTargetCountAsBaseline(t *testing.T) {
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
		"targetCount":    251,
		"completedCount": 249,
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
	if result.Subscription.BaselineCount != 251 {
		t.Fatalf("expected raw target baseline 251, got %d", result.Subscription.BaselineCount)
	}
	if len(result.Subscription.BaselineCodes) != 3 {
		t.Fatalf("expected 3 unique baseline codes, got %#v", result.Subscription.BaselineCodes)
	}
	if result.Subscription.CurrentTotal != 249 {
		t.Fatalf("expected currentTotal=249 (completed count), got %d", result.Subscription.CurrentTotal)
	}
	if result.Subscription.CurrentObservedCount != 251 {
		t.Fatalf("expected currentObservedCount=251 (raw baseline), got %d", result.Subscription.CurrentObservedCount)
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

func TestImportFromFilmDataPreservesRawDuplicateRecordCount(t *testing.T) {
	userDataDir := t.TempDir()
	outputDir := t.TempDir()
	var payload strings.Builder
	payload.WriteString("[")
	for index := 0; index < 251; index++ {
		if index > 0 {
			payload.WriteString(",")
		}
		code := "AAA-001"
		if index == 249 {
			code = "AAA-002"
		}
		if index == 250 {
			code = "AAA-003"
		}
		payload.WriteString(`{"title":"` + code + `","actress":["演员一"],"sourceLink":"https://example.test/` + code + `-` + strconv.Itoa(index) + `"}`)
	}
	payload.WriteString("]")
	if err := os.WriteFile(filepath.Join(outputDir, crawlartifact.CrawlFilmDataFile), []byte(payload.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	service := NewService(runtimepaths.Paths{UserData: userDataDir}, nil)
	result, err := service.ImportFromOutput(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if result.Subscription.BaselineCount != 251 {
		t.Fatalf("expected raw baseline count 251, got %d", result.Subscription.BaselineCount)
	}
	if len(result.Subscription.BaselineCodes) != 3 {
		t.Fatalf("expected 3 unique comparison codes, got %#v", result.Subscription.BaselineCodes)
	}
}
