package librarymetadata

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteMetadata_CreatesBackdropFromLocalCrawlerRecord(t *testing.T) {
	imageBytes := makeTestJPEG(t)

	tmpDir := t.TempDir()
	mediaPath := filepath.Join(tmpDir, "IPZZ-688.mkv")
	if err := os.WriteFile(mediaPath, []byte("fake"), 0o644); err != nil {
		t.Fatalf("write media: %v", err)
	}

	info := BuildMovieInfoFromCrawlerRecord(CrawlerRecord{
		Code:       "IPZZ-688",
		Title:      "IPZZ-688 - FIRST IMPRESSION 185",
		CoverURL:   "https://example.com/cover.png",
		SourceLink: "https://www.javbus.com/IPZZ-688",
		Source:     "JavBus",
	})
	if info == nil {
		t.Fatal("expected movie info")
	}
	if info.BackdropURL == "" {
		t.Fatal("expected non-empty backdrop URL from crawler record")
	}

	item := LibraryMediaItem{
		Code:          "IPZZ-688",
		MediaPath:     mediaPath,
		MediaStem:     "IPZZ-688",
		NfoPath:       filepath.Join(tmpDir, "IPZZ-688.nfo"),
		PosterPath:    filepath.Join(tmpDir, "IPZZ-688-poster.jpg"),
		BackdropPath:  filepath.Join(tmpDir, "IPZZ-688-backdrop.jpg"),
		LandscapePath: filepath.Join(tmpDir, "IPZZ-688-landscape.jpg"),
	}

	result := WriteMetadata(WriteOptions{
		Item:         item,
		Info:         info,
		ImageFetcher: func(_, _ string) ([]byte, error) { return imageBytes, nil },
	})

	if result.Error != "" {
		t.Fatalf("write metadata failed: %s", result.Error)
	}
	if result.BackdropPath == "" {
		t.Fatalf("expected backdrop path to be set, got empty; poster=%q landscape=%q", result.PosterPath, result.LandscapePath)
	}
	if _, err := os.Stat(result.BackdropPath); err != nil {
		t.Fatalf("backdrop file not created: %v", err)
	}
	fmt.Printf("backdrop=%s poster=%s landscape=%s\n", result.BackdropPath, result.PosterPath, result.LandscapePath)
}
