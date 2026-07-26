package modulelog

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAppendCreatesChineseModuleDailySessionAndCurrentLogs(t *testing.T) {
	appPath := t.TempDir()
	now := time.Date(2026, time.July, 22, 12, 34, 56, 0, time.Local)
	if err := Append(appPath, Subscription, "info", "test", now); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(appPath, "log", Subscription)
	for _, path := range []string{
		filepath.Join(dir, "AV\u8ba2\u9605-2026\u5e747\u670822\u65e5.txt"),
		filepath.Join(dir, "\u8fd0\u884c\u65e5\u5fd7-20260722-123456.txt"),
		filepath.Join(dir, "\u8fd0\u884c\u65e5\u5fd7.txt"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected log file %s: %v", path, err)
		}
	}
}
