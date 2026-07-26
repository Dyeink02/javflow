package subcrawlv2

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClampConcurrency(t *testing.T) {
	tests := map[int]int{-1: 2, 0: 2, 1: 1, 2: 2, 3: 3, 4: 3, 99: 3}
	for input, expected := range tests {
		if actual := clampConcurrency(input); actual != expected {
			t.Fatalf("clampConcurrency(%d) = %d, expected %d", input, actual, expected)
		}
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
