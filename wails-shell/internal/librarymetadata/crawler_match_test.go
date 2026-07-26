package librarymetadata

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"javflow/internal/contracts/crawlartifact"
)

func TestBuildCrawlSource_FromFilmData_BBAN452(t *testing.T) {
	tmpDir := t.TempDir()
	filmData := `[
  {
    "title": "BBAN-452 - 测试标题",
    "sourceLink": "https://www.javbus.com/BBAN-452",
    "actress": ["Actor A", "Actor B"],
    "category": ["Lesbian", "HD"],
    "coverImage": "https://example.com/cover.jpg"
  }
]`
	if err := os.WriteFile(filepath.Join(tmpDir, "filmData.json"), []byte(filmData), 0o644); err != nil {
		t.Fatalf("write filmData: %v", err)
	}

	source, err := BuildCrawlSource(tmpDir, "")
	if err != nil {
		t.Fatalf("BuildCrawlSource failed: %v", err)
	}

	record, ok := source.Lookup("BBAN-452")
	if !ok {
		t.Fatal("expected BBAN-452 in crawl source")
	}
	if record.Title != "BBAN-452 - 测试标题" {
		t.Errorf("title mismatch: %s", record.Title)
	}
	if record.CoverURL != "https://example.com/cover.jpg" {
		t.Errorf("cover mismatch: %s", record.CoverURL)
	}
	if len(record.Actors) != 2 {
		t.Errorf("expected 2 actors, got %d", len(record.Actors))
	}

	info := BuildMovieInfoFromCrawlerRecord(record)
	if info == nil {
		t.Fatal("expected MovieInfo")
	}
	if info.Title != "测试标题" {
		t.Errorf("expected cleaned title '测试标题', got %s", info.Title)
	}
	if info.CoverURL != "https://example.com/cover.jpg" {
		t.Errorf("expected cover URL, got %s", info.CoverURL)
	}
}

func TestBuildCrawlSource_ExplicitSelectionDoesNotMergeHistory(t *testing.T) {
	selectedDir := t.TempDir()
	otherDir := t.TempDir()
	userDataDir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(selectedDir, "filmData.json"),
		[]byte(`[{"title":"BBAN-452 - selected"}]`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	otherFilmDataPath := filepath.Join(otherDir, "filmData.json")
	if err := os.WriteFile(otherFilmDataPath, []byte(`[{"title":"SSIS-999 - historical"}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	indexPath := crawlartifact.CacheIndexPath(userDataDir)
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		t.Fatal(err)
	}
	indexPayload, err := json.Marshal([]crawlartifact.CacheSnapshot{{
		CacheKey:     "history",
		OutputDir:    otherDir,
		FilmDataPath: otherFilmDataPath,
		UpdatedAt:    "2026-07-20T10:00:00+08:00",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(indexPath, indexPayload, 0o644); err != nil {
		t.Fatal(err)
	}

	selected, err := BuildCrawlSource(filepath.Join(selectedDir, "filmData.json"), userDataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected.Records()) != 1 {
		t.Fatalf("explicit JSON must stay bounded to one record, got %d", len(selected.Records()))
	}
	if _, ok := selected.Lookup("BBAN-452"); !ok {
		t.Fatal("selected JSON record is missing")
	}
	if _, ok := selected.Lookup("SSIS-999"); ok {
		t.Fatal("historical snapshot leaked into explicit JSON selection")
	}

	history, err := BuildCrawlSource("", userDataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := history.Lookup("SSIS-999"); !ok {
		t.Fatal("history fallback should remain available without an explicit selection")
	}
}

func TestBuildMovieInfoFromCrawlerRecord_NormalizesRelativeCoverURLs(t *testing.T) {
	tests := []struct {
		name       string
		coverURL   string
		sourceLink string
		want       string
	}{
		{
			name:       "protocol relative",
			coverURL:   "//pics.example.com/covers/DASS-287.jpg",
			sourceLink: "https://www.javbus.com/DASS-287",
			want:       "https://pics.example.com/covers/DASS-287.jpg",
		},
		{
			name:       "root relative",
			coverURL:   "/pics/cover/DASS-287.jpg",
			sourceLink: "https://www.javbus.com/DASS-287",
			want:       "https://www.javbus.com/pics/cover/DASS-287.jpg",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := BuildMovieInfoFromCrawlerRecord(CrawlerRecord{
				Code:       "DASS-287",
				CoverURL:   test.coverURL,
				SourceLink: test.sourceLink,
			})
			if info == nil {
				t.Fatal("expected movie info")
			}
			if info.CoverURL != test.want || info.ThumbURL != test.want || info.BackdropURL != test.want {
				t.Fatalf("normalized cover = %q / %q / %q, want %q", info.CoverURL, info.ThumbURL, info.BackdropURL, test.want)
			}
		})
	}
}

// TestBuildMovieInfoFromCrawlerRecord_ProvidesBackdropFromCover verifies that
// local crawler records, which usually only contain a coverImage, expose the
// same URL as backdrop so media-library scraping does not skip the background
// download entirely.
func TestBuildMovieInfoFromCrawlerRecord_ProvidesBackdropFromCover(t *testing.T) {
	info := BuildMovieInfoFromCrawlerRecord(CrawlerRecord{
		Code:       "IPZZ-688",
		Title:      "IPZZ-688 - FIRST IMPRESSION 185",
		CoverURL:   "https://example.com/cover.jpg",
		SourceLink: "https://www.javbus.com/IPZZ-688",
	})
	if info == nil {
		t.Fatal("expected movie info")
	}
	if info.BackdropURL != "https://example.com/cover.jpg" {
		t.Errorf("expected backdrop URL to fall back to cover, got %s", info.BackdropURL)
	}
	if info.ThumbURL != "https://example.com/cover.jpg" {
		t.Errorf("expected thumb URL to fall back to cover, got %s", info.ThumbURL)
	}
}

func TestBuildMovieInfoFromCrawlerRecord_EmptyCoverLeavesEmptyBackdrop(t *testing.T) {
	info := BuildMovieInfoFromCrawlerRecord(CrawlerRecord{
		Code:       "IPZZ-688",
		Title:      "IPZZ-688 - FIRST IMPRESSION 185",
		SourceLink: "https://www.javbus.com/IPZZ-688",
	})
	if info == nil {
		t.Fatal("expected movie info")
	}
	if info.BackdropURL != "" {
		t.Errorf("expected empty backdrop URL when cover is missing, got %s", info.BackdropURL)
	}
}

// TestBuildMovieInfoFromCrawlerRecord_JSONRoundTrip verifies that the backdrop
// URL survives the JSON encode/decode cycle used by the Wails bridge between
// resolveLibraryMetadata and writeLibraryMetadata.
func TestBuildMovieInfoFromCrawlerRecord_JSONRoundTrip(t *testing.T) {
	info := BuildMovieInfoFromCrawlerRecord(CrawlerRecord{
		Code:       "IPZZ-688",
		Title:      "IPZZ-688 - FIRST IMPRESSION 185",
		CoverURL:   "https://example.com/cover.jpg",
		SourceLink: "https://www.javbus.com/IPZZ-688",
	})
	if info == nil {
		t.Fatal("expected movie info")
	}

	// Simulate the Wails bridge: Go struct -> JSON -> JS object -> JSON -> Go struct.
	payload, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal info: %v", err)
	}

	var jsObject map[string]any
	if err := json.Unmarshal(payload, &jsObject); err != nil {
		t.Fatalf("unmarshal to js object: %v", err)
	}

	backFromJS, err := json.Marshal(jsObject)
	if err != nil {
		t.Fatalf("marshal js object: %v", err)
	}

	var decoded MovieInfo
	if err := json.Unmarshal(backFromJS, &decoded); err != nil {
		t.Fatalf("unmarshal to MovieInfo: %v", err)
	}

	if decoded.BackdropURL != "https://example.com/cover.jpg" {
		t.Errorf("expected backdrop URL to survive round trip, got %q", decoded.BackdropURL)
	}
	if decoded.CoverURL != "https://example.com/cover.jpg" {
		t.Errorf("expected cover URL to survive round trip, got %q", decoded.CoverURL)
	}
	if decoded.ThumbURL != "https://example.com/cover.jpg" {
		t.Errorf("expected thumb URL to survive round trip, got %q", decoded.ThumbURL)
	}
}

// TestBuildCrawlSource_FromFilePath verifies that passing a path to an artifact
// file (e.g. organizer-codes.json) automatically resolves to its parent directory.
func TestBuildCrawlSource_FromFilePath(t *testing.T) {
	tmpDir := t.TempDir()
	filmData := `[{"title": "BBAN-452 - 测试标题", "sourceLink": "https://www.javbus.com/BBAN-452"}]`
	if err := os.WriteFile(filepath.Join(tmpDir, "filmData.json"), []byte(filmData), 0o644); err != nil {
		t.Fatalf("write filmData: %v", err)
	}

	source, err := BuildCrawlSource(filepath.Join(tmpDir, "organizer-codes.json"), "")
	if err != nil {
		t.Fatalf("BuildCrawlSource from file path failed: %v", err)
	}

	if _, ok := source.Lookup("BBAN-452"); !ok {
		t.Fatal("expected BBAN-452 when input is an artifact file path")
	}
	if source.outputDir != tmpDir {
		t.Errorf("expected outputDir to be parent directory, got %s", source.outputDir)
	}
}
