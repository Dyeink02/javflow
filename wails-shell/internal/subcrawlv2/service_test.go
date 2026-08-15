package subcrawlv2

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"javflow/internal/crawloutput"
)

func TestClampConcurrency(t *testing.T) {
	tests := map[int]int{-1: 2, 0: 2, 1: 1, 2: 2, 3: 3, 4: 3, 99: 3}
	for input, expected := range tests {
		if actual := clampConcurrency(input); actual != expected {
			t.Fatalf("clampConcurrency(%d) = %d, expected %d", input, actual, expected)
		}
	}
}

func TestApplyActressCountFilter(t *testing.T) {
	film := crawloutput.FilmData{Title: "ABC-001", ActressCount: 12}
	if got := applyActressCountFilter(film, 0); got.FilteredByActressCount {
		t.Fatal("threshold 0 must leave filtering disabled")
	}
	got := applyActressCountFilter(film, 12)
	if !got.FilteredByActressCount || got.FilterRemark == "" {
		t.Fatalf("expected threshold match to mark collection film: %+v", got)
	}
	if below := applyActressCountFilter(film, 13); below.FilteredByActressCount {
		t.Fatal("film below threshold must remain visible")
	}
}

func TestWriteSubscriptionResultKeepsOnlyCountedTextFile(t *testing.T) {
	outputDir := t.TempDir()
	for _, name := range []string{"filmData.json", "magnet-links.txt", "crawl-profile.json"} {
		if err := os.WriteFile(filepath.Join(outputDir, name), []byte("temporary"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.Local)
	resultPath, err := writeSubscriptionResult(outputDir, []string{"magnet:?xt=urn:btih:AAAA"}, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(resultPath) != "2026\u5e747\u670822\u53f7\u66f4\u65b01\u90e8.txt" {
		t.Fatalf("unexpected result filename: %s", resultPath)
	}
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(resultPath) {
		t.Fatalf("expected one visible TXT result, got %#v", entries)
	}
}
