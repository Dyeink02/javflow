package librarymetadata

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanLibrary_TestLibrary(t *testing.T) {
	root := filepath.Join("..", "..", "..", "test-library")
	if _, err := os.Stat(root); err != nil {
		t.Skipf("test-library not found: %v", err)
	}

	result := ScanLibrary(ScanOptions{Root: root})
	if result.Error != "" {
		t.Fatalf("scan failed: %s", result.Error)
	}

	codes := make(map[string]bool)
	for _, item := range result.Items {
		codes[item.Code] = true
		t.Logf("scanned: code=%s path=%s stem=%s status=%s", item.Code, item.MediaPath, item.MediaStem, item.Status)
	}

	expected := []string{"BBAN-452", "IPZZ-123", "FC2-PPV-1234567", "SSIS-999"}
	for _, code := range expected {
		if !codes[code] {
			t.Errorf("expected code %s not found in scan result", code)
		}
	}
}

func TestScanLibrary_WithCrawlSource_BBAN452(t *testing.T) {
	tmpDir := t.TempDir()
	mediaPath := filepath.Join(tmpDir, "BBAN-452.mp4.strm")
	if err := os.WriteFile(mediaPath, []byte("dummy"), 0o644); err != nil {
		t.Fatalf("write media: %v", err)
	}

	crawlDir := t.TempDir()
	filmData := `[{"title":"BBAN-452 - 本地标题","coverImage":"https://example.com/cover.jpg"}]`
	if err := os.WriteFile(filepath.Join(crawlDir, "filmData.json"), []byte(filmData), 0o644); err != nil {
		t.Fatalf("write filmData: %v", err)
	}

	result := ScanLibrary(ScanOptions{Root: tmpDir, CrawlOutputDir: crawlDir})
	if result.Error != "" {
		t.Fatalf("scan failed: %s", result.Error)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result.Items))
	}
	item := result.Items[0]
	if item.Code != "BBAN-452" {
		t.Errorf("expected BBAN-452, got %s", item.Code)
	}
	if item.MediaStem != "BBAN-452.mp4" {
		t.Errorf("expected media stem BBAN-452.mp4, got %s", item.MediaStem)
	}
	if !item.CrawlMatch {
		t.Error("expected CrawlMatch true")
	}
	if item.Title != "本地标题" {
		t.Errorf("expected title '本地标题', got %s", item.Title)
	}
	if !strings.Contains(item.NfoPath, "BBAN-452.mp4.nfo") {
		t.Errorf("expected NFO path to contain BBAN-452.mp4.nfo, got %s", item.NfoPath)
	}
}

func TestScanLibrary_ExplicitCrawlSourceLimitsVisibleCodes(t *testing.T) {
	mediaRoot := t.TempDir()
	for _, name := range []string{"BBAN-452.mp4", "SSIS-999.mp4"} {
		if err := os.WriteFile(filepath.Join(mediaRoot, name), []byte("dummy"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	crawlDir := t.TempDir()
	filmData := `[
  {"title":"BBAN-452 - selected local item"},
  {"title":"IPZZ-123 - selected missing item"}
]`
	filmDataPath := filepath.Join(crawlDir, "filmData.json")
	if err := os.WriteFile(filmDataPath, []byte(filmData), 0o644); err != nil {
		t.Fatal(err)
	}

	result := ScanLibrary(ScanOptions{Root: mediaRoot, CrawlOutputDir: filmDataPath})
	if result.Error != "" {
		t.Fatalf("scan failed: %s", result.Error)
	}
	if len(result.Items) != 1 || result.Items[0].Code != "BBAN-452" {
		t.Fatalf("local list must be restricted to selected JSON codes, got %#v", result.Items)
	}
	if len(result.MissingItems) != 1 || result.MissingItems[0].Code != "IPZZ-123" {
		t.Fatalf("missing list must come only from selected JSON, got %#v", result.MissingItems)
	}
	for _, item := range append(append([]LibraryMediaItem{}, result.Items...), result.MissingItems...) {
		if item.Code == "SSIS-999" {
			t.Fatal("unselected local code leaked into the visible list")
		}
	}
}

func TestScanLibrary_AlphabeticPartsRemainSeparate(t *testing.T) {
	mediaRoot := t.TempDir()
	for _, name := range []string{"DASS-287-A.mp4.strm", "DASS-287-B.mp4.strm"} {
		if err := os.WriteFile(filepath.Join(mediaRoot, name), []byte("dummy"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	crawlDir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(crawlDir, "filmData.json"),
		[]byte(`[{"title":"DASS-287 - split release"}]`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	result := ScanLibrary(ScanOptions{Root: mediaRoot, OutputMode: "inplace", CrawlOutputDir: crawlDir})
	if result.Error != "" {
		t.Fatalf("scan failed: %s", result.Error)
	}
	if len(result.Items) != 2 {
		t.Fatalf("expected both split files in the list, got %d: %#v", len(result.Items), result.Items)
	}

	byDisplayCode := make(map[string]LibraryMediaItem, len(result.Items))
	for _, item := range result.Items {
		if item.Code != "DASS-287" {
			t.Errorf("metadata lookup code must be DASS-287, got %q", item.Code)
		}
		if !item.CrawlMatch {
			t.Errorf("split item %q must match the base code in selected JSON", item.DisplayCode)
		}
		byDisplayCode[item.DisplayCode] = item
	}

	partA, ok := byDisplayCode["DASS-287-A"]
	if !ok {
		t.Fatal("DASS-287-A is missing from the scan list")
	}
	partB, ok := byDisplayCode["DASS-287-B"]
	if !ok {
		t.Fatal("DASS-287-B is missing from the scan list")
	}
	if !strings.HasSuffix(partA.NfoPath, "DASS-287-A.mp4.nfo") {
		t.Errorf("part A output path lost its suffix: %s", partA.NfoPath)
	}
	if !strings.HasSuffix(partB.NfoPath, "DASS-287-B.mp4.nfo") {
		t.Errorf("part B output path lost its suffix: %s", partB.NfoPath)
	}
	if partA.NfoPath == partB.NfoPath {
		t.Fatal("part A and part B must not share an output path")
	}
}

func TestScanLibrary_AlphabeticPartsUseSeparateSubfolders(t *testing.T) {
	mediaRoot := t.TempDir()
	for _, name := range []string{"DASS-287-A.strm", "DASS-287-B.strm"} {
		if err := os.WriteFile(filepath.Join(mediaRoot, name), []byte("dummy"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result := ScanLibrary(ScanOptions{Root: mediaRoot, OutputMode: "subfolder"})
	if result.Error != "" {
		t.Fatalf("scan failed: %s", result.Error)
	}
	if len(result.Items) != 2 {
		t.Fatalf("expected 2 split items, got %d", len(result.Items))
	}
	if filepath.Dir(result.Items[0].NfoPath) == filepath.Dir(result.Items[1].NfoPath) {
		t.Fatalf("split files must use separate output folders: %#v", result.Items)
	}
}

func TestScanLibrary_FixedNumericAndDupPartsRemainSeparate(t *testing.T) {
	mediaRoot := t.TempDir()
	for _, name := range []string{"MIDD-820-1.mp4", "MIDD-820-2.mp4", "MIDD-820_DUP1.mp4", "MIDD-820_DUP2.mp4"} {
		if err := os.WriteFile(filepath.Join(mediaRoot, name), []byte("dummy"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result := ScanLibrary(ScanOptions{Root: mediaRoot, OutputMode: "inplace"})
	if result.Error != "" {
		t.Fatalf("scan failed: %s", result.Error)
	}
	if len(result.Items) != 4 {
		t.Fatalf("expected all fixed-suffix files in the list, got %d: %#v", len(result.Items), result.Items)
	}

	byDisplayCode := make(map[string]LibraryMediaItem, len(result.Items))
	for _, item := range result.Items {
		if item.Code != "MIDD-820" {
			t.Errorf("metadata lookup code must be MIDD-820, got %q", item.Code)
		}
		byDisplayCode[item.DisplayCode] = item
	}
	for _, displayCode := range []string{"MIDD-820-1", "MIDD-820-2", "MIDD-820_DUP1", "MIDD-820_DUP2"} {
		item, ok := byDisplayCode[displayCode]
		if !ok {
			t.Fatalf("missing fixed-suffix item %q: %#v", displayCode, byDisplayCode)
		}
		if !strings.Contains(item.NfoPath, displayCode+".nfo") {
			t.Errorf("NFO path lost suffix for %q: %s", displayCode, item.NfoPath)
		}
	}
}

func TestScanLibrary_ResolvesCrawlerMagnetAliasesToCanonicalCode(t *testing.T) {
	mediaRoot := t.TempDir()
	for _, name := range []string{"GOMK-051.avi.strm", "MXGS-1183.mp4.strm", "UNSELECTED-001.mp4.strm"} {
		if err := os.WriteFile(filepath.Join(mediaRoot, name), []byte("dummy"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	crawlDir := filepath.Join(mediaRoot, "佐山愛-抓取")
	if err := os.MkdirAll(crawlDir, 0o755); err != nil {
		t.Fatal(err)
	}
	filmData := `[
  {"title":"GOMK-51 ミス・マーキュリー 佐山愛","actress":["佐山愛"],"magnetLinks":[{"link":"magnet:?xt=urn:btih:1&dn=GOMK-51"}]},
  {"title":"MXGS-118 【AIリマスター版】SUMMER GIRL 佐山愛","actress":["佐山愛"],"magnetLinks":[{"link":"magnet:?xt=urn:btih:2&dn=MXGS-1183"}]}
]`
	if err := os.WriteFile(filepath.Join(crawlDir, "filmData.json"), []byte(filmData), 0o644); err != nil {
		t.Fatal(err)
	}

	result := ScanLibrary(ScanOptions{Root: mediaRoot, CrawlOutputDir: crawlDir})
	if result.Error != "" {
		t.Fatalf("scan failed: %s", result.Error)
	}
	if len(result.Items) != 2 {
		t.Fatalf("expected only selected alias files, got %d: %#v", len(result.Items), result.Items)
	}
	byDisplay := map[string]LibraryMediaItem{}
	for _, item := range result.Items {
		byDisplay[item.DisplayCode] = item
	}
	if item := byDisplay["GOMK-051"]; item.Code != "GOMK-51" || !item.CrawlMatch {
		t.Fatalf("GOMK alias did not resolve canonically: %#v", item)
	}
	if item := byDisplay["MXGS-1183"]; item.Code != "MXGS-118" || !item.CrawlMatch {
		t.Fatalf("MXGS magnet alias did not resolve canonically: %#v", item)
	}
	for _, item := range result.Items {
		if strings.Contains(item.MediaPath, "UNSELECTED") {
			t.Fatal("unselected file leaked into explicit crawl selection")
		}
	}
}

func TestScanLibrary_SelectedArtifactFailureDoesNotScanEverything(t *testing.T) {
	mediaRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(mediaRoot, "BBAN-452.mp4"), []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := ScanLibrary(ScanOptions{Root: mediaRoot, CrawlOutputDir: filepath.Join(mediaRoot, "missing-filmData.json")})
	if result.Error == "" {
		t.Fatal("expected selected artifact failure")
	}
	if len(result.Items) != 0 {
		t.Fatalf("must not scan files after selected artifact failure: %#v", result.Items)
	}
}
