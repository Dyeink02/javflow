package common

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteUTF8TextFileAddsBOM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.txt")
	if err := WriteUTF8TextFile(path, "line-1\r\n"); err != nil {
		t.Fatalf("WriteUTF8TextFile failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if !bytes.HasPrefix(data, utf8BOM) {
		t.Fatalf("expected UTF-8 BOM prefix")
	}
	if string(data[len(utf8BOM):]) != "line-1\r\n" {
		t.Fatalf("unexpected content: %q", string(data[len(utf8BOM):]))
	}
}

func TestAppendUTF8TextFileAddsBOMOnlyOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "latest-log.txt")
	if err := AppendUTF8TextFile(path, "line-1\r\n"); err != nil {
		t.Fatalf("first AppendUTF8TextFile failed: %v", err)
	}
	if err := AppendUTF8TextFile(path, "line-2\r\n"); err != nil {
		t.Fatalf("second AppendUTF8TextFile failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if !bytes.HasPrefix(data, utf8BOM) {
		t.Fatalf("expected UTF-8 BOM prefix")
	}
	if bytes.Count(data, utf8BOM) != 1 {
		t.Fatalf("expected a single BOM, got %d", bytes.Count(data, utf8BOM))
	}
	if string(data[len(utf8BOM):]) != "line-1\r\nline-2\r\n" {
		t.Fatalf("unexpected appended content: %q", string(data[len(utf8BOM):]))
	}
}

func TestWriteFileAtomicReplacesCompleteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	if err := os.WriteFile(path, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(path, []byte(`{"version":2}`), 0o600); err != nil {
		t.Fatalf("WriteFileAtomic failed: %v", err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != `{"version":2}` {
		t.Fatalf("unexpected atomic payload: %q", payload)
	}
	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".cache.json-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("atomic write left temporary files: %v", leftovers)
	}
}
