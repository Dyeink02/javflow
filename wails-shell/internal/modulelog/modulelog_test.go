package modulelog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBeginRunCreatesOneFilePerRunWithModulePrefix(t *testing.T) {
	appPath := t.TempDir()
	now := time.Date(2026, time.September, 13, 12, 34, 56, 0, time.Local)

	runPath, err := BeginRun(appPath, Subscription, now)
	if err != nil {
		t.Fatal(err)
	}
	if base := filepath.Base(runPath); !strings.HasPrefix(base, "订阅-") || !strings.HasSuffix(base, ".txt") {
		t.Fatalf("unexpected run file name: %s", base)
	}
	if err := Append(appPath, Subscription, "info", "hello", now); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(runPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hello") {
		t.Fatalf("run file missing appended line: %q", string(data))
	}

	// 单份日志策略：同目录不应再出现当日汇总或固定名指针文件。
	entries, err := os.ReadDir(filepath.Join(appPath, "log", Subscription))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 log file, got %d", len(entries))
	}
}

func TestPruneKeepsOnlyNewestRetentionFiles(t *testing.T) {
	appPath := t.TempDir()
	base := time.Date(2026, time.September, 1, 8, 0, 0, 0, time.Local)
	for i := 0; i < logRetentionCount+3; i++ {
		if _, err := BeginRun(appPath, Organizer, base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(appPath, "log", Organizer))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != logRetentionCount {
		t.Fatalf("expected retention %d files, got %d", logRetentionCount, len(entries))
	}
}
