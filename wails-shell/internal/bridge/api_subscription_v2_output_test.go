package bridge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareSubscriptionV2OutputDirUsesAppActressFolder(t *testing.T) {
	appPath := t.TempDir()
	root := filepath.Join(appPath, "AV订阅")
	if err := os.MkdirAll(filepath.Join(root, "run-old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "filmData.json"), []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	actressDir := filepath.Join(root, "大槻ひびき")
	if err := os.MkdirAll(filepath.Join(actressDir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(actressDir, "2026.7.18.txt"), []byte("previous"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(actressDir, "magnet-links.txt"), []byte("obsolete"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := prepareSubscriptionV2OutputDir(appPath, "大槻ひびき")
	if err != nil {
		t.Fatal(err)
	}
	if got != actressDir {
		t.Fatalf("expected %q, got %q", actressDir, got)
	}
	if _, err := os.Stat(filepath.Join(root, "filmData.json")); !os.IsNotExist(err) {
		t.Fatalf("root-level filmData should be removed, stat error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "run-old")); !os.IsNotExist(err) {
		t.Fatalf("obsolete run directory should be removed, stat error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(actressDir, "magnet-links.txt")); !os.IsNotExist(err) {
		t.Fatalf("generic magnet file should be removed, stat error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(actressDir, "logs")); !os.IsNotExist(err) {
		t.Fatalf("actor output logs should be removed, stat error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(actressDir, "2026.7.18.txt")); err != nil {
		t.Fatalf("previous dated output should survive until successful update: %v", err)
	}
	if _, err := os.Stat(filepath.Join(appPath, "log", "AV订阅")); err != nil {
		t.Fatalf("central subscription log directory should exist: %v", err)
	}
}

func TestLatestSubscriptionDatedTextFileUsesCalendarOrder(t *testing.T) {
	outputDir := t.TempDir()
	for _, name := range []string{"2026.7.9.txt", "2026.7.19.txt", "magnet-links.txt"} {
		if err := os.WriteFile(filepath.Join(outputDir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := filepath.Base(latestSubscriptionDatedTextFile(outputDir)); got != "2026.7.19.txt" {
		t.Fatalf("expected latest dated file, got %q", got)
	}
	if !isSubscriptionDatedTextFile("2026.7.19.txt") || isSubscriptionDatedTextFile("latest-log.txt") {
		t.Fatal("dated subscription file recognition is incorrect")
	}
	if _, ok := parseSubscriptionDateFileName("2026.2.30.txt"); ok {
		t.Fatal("invalid calendar date must be rejected")
	}
}
