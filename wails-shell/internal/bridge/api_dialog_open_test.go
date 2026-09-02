package bridge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExistingMagnetFileKeepsConcreteFilePath(t *testing.T) {
	pathValue := filepath.Join(t.TempDir(), "magnet-links.txt")
	if err := os.WriteFile(pathValue, []byte("magnet:?xt=urn:btih:test"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := existingMagnetFile(pathValue); got != pathValue {
		t.Fatalf("existingMagnetFile(%q) = %q, want original file path", pathValue, got)
	}
	if got := existingMagnetFile(filepath.Dir(pathValue)); got != "" {
		t.Fatalf("directory must not be treated as a magnet file: %q", got)
	}
}

func TestExistingMagnetFileRejectsMissingPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-magnet-links.txt")
	if got := existingMagnetFile(missing); got != "" {
		t.Fatalf("missing file unexpectedly resolved: %q", got)
	}
}
