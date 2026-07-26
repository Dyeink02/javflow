package crawltask

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"javflow/internal/contracts/crawlartifact"
)

func TestResolveRunOutputDirectoryKeepsBaseDirectoryWhenResuming(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, crawlartifact.CrawlFilmDataFile), []byte("[]"), 0o644); err != nil {
		t.Fatalf("write filmData.json: %v", err)
	}

	result := ResolveRunOutputDirectory(tempDir, true, time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC))
	if result.OutputDir != tempDir {
		t.Fatalf("expected output dir %q, got %q", tempDir, result.OutputDir)
	}
	if result.CreatedRunDir {
		t.Fatalf("expected CreatedRunDir=false")
	}
	if result.Reason != "resume-existing" {
		t.Fatalf("expected reason resume-existing, got %q", result.Reason)
	}
}

func TestResolveRunOutputDirectoryCreatesRunDirectoryWhenArtifactsExist(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, crawlartifact.CrawlFilmDataFile), []byte("[]"), 0o644); err != nil {
		t.Fatalf("write filmData.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, crawlartifact.DefaultMagnetTxt), []byte("magnet:?xt=urn:btih:test"), 0o644); err != nil {
		t.Fatalf("write magnet-links.txt: %v", err)
	}

	result := ResolveRunOutputDirectory(tempDir, false, time.Date(2026, 4, 12, 16, 0, 0, 0, time.Local))
	expected := filepath.Join(tempDir, "run-20260412-160000")

	if result.BaseOutputDir != tempDir {
		t.Fatalf("expected base output dir %q, got %q", tempDir, result.BaseOutputDir)
	}
	if result.OutputDir != expected {
		t.Fatalf("expected output dir %q, got %q", expected, result.OutputDir)
	}
	if !result.CreatedRunDir {
		t.Fatalf("expected CreatedRunDir=true")
	}
	if result.Reason != "isolated-existing-output" {
		t.Fatalf("expected reason isolated-existing-output, got %q", result.Reason)
	}
}
