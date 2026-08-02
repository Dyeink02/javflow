package organizer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverLocalCodesWritesHiddenSnapshotAndRejectsAmbiguousNames(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := []string{
		filepath.Join(root, "ABA-250.mp4"),
		filepath.Join(root, "nested", "1818.com@TL1.mkv"),
		filepath.Join(root, "nested", "ABA-250-TL-001.mp4"),
		filepath.Join(root, "readme.txt"),
	}
	for _, path := range files {
		if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	service := NewService()
	result, err := service.DiscoverLocalCodes(DiscoverOptions{
		RootPath:              root,
		IncludeSubdirectories: true,
		VideoExtensions:       "mp4,mkv",
	})
	if err != nil {
		t.Fatalf("DiscoverLocalCodes returned error: %v", err)
	}
	if result.VideoFiles != 3 || result.IdentifiedFiles != 2 || result.UnidentifiedFiles != 1 {
		t.Fatalf("unexpected discovery counters: %#v", result)
	}
	if len(result.Codes) != 2 || result.Codes[0] != "ABA-250" || result.Codes[1] != "TL-001" {
		t.Fatalf("unexpected codes: %#v", result.Codes)
	}
	if _, err := os.Stat(result.StatePath); err != nil {
		t.Fatalf("hidden snapshot was not written: %v", err)
	}
	if result.ReportPath == "" {
		t.Fatal("expected an unmatched report for the ambiguous local video")
	}
	if hidden, err := service.loadDiscoveredCodesArtifact(root); err != nil {
		t.Fatalf("hidden snapshot could not be loaded directly: %v", err)
	} else if hidden.CodeCount != 2 {
		t.Fatalf("unexpected direct hidden snapshot: %#v", hidden)
	}

	loaded, err := service.LoadCrawlFilmCodes(root)
	if err != nil {
		t.Fatalf("LoadCrawlFilmCodes did not read hidden snapshot: %v", err)
	}
	if loaded.SourceType != "local-discovery" || loaded.CodeCount != 2 {
		t.Fatalf("unexpected hidden snapshot load result: %#v", loaded)
	}
}
