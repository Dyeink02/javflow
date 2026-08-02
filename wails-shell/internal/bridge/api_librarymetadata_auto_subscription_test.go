package bridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"javflow/internal/contracts/crawlartifact"
)

func TestSameLibraryMetadataActressNormalizesSpacing(t *testing.T) {
	if !sameLibraryMetadataActress("松本 いちか", "松本いちか") {
		t.Fatal("expected normalized actress names to match")
	}
	if sameLibraryMetadataActress("松本いちか", "深田えいみ") {
		t.Fatal("different actress names must not match")
	}
}

func TestAutoSubscriptionOutputDirUsesSiblingForDifferentActress(t *testing.T) {
	current := filepath.Join(t.TempDir(), "新松本いちか")
	if got := autoSubscriptionOutputDir(current, "松本いちか"); got != current {
		t.Fatalf("matching output should be reused, got %s", got)
	}
	want := filepath.Join(filepath.Dir(current), "深田えいみ")
	if got := autoSubscriptionOutputDir(current, "深田えいみ"); got != want {
		t.Fatalf("expected sibling output %s, got %s", want, got)
	}
}

func TestCrawlProfileActressNamePrefersHiddenSnapshot(t *testing.T) {
	userDataDir := t.TempDir()
	outputDir := t.TempDir()

	visiblePath := filepath.Join(outputDir, crawlartifact.CrawlProfileFile)
	writeProfile := func(path, actress string) {
		t.Helper()
		payload, err := json.Marshal(crawlartifact.CrawlProfileArtifact{ActressName: actress})
		if err != nil {
			t.Fatalf("marshal profile: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create profile directory: %v", err)
		}
		if err := os.WriteFile(path, payload, 0o644); err != nil {
			t.Fatalf("write profile: %v", err)
		}
	}

	writeProfile(visiblePath, "公开文件演员")
	hiddenPath := crawlartifact.ResolveInternalArtifactPaths(userDataDir, outputDir).CrawlProfilePath
	writeProfile(hiddenPath, "隐藏快照演员")

	if got := crawlProfileActressName(userDataDir, outputDir); got != "隐藏快照演员" {
		t.Fatalf("expected hidden profile actress, got %q", got)
	}
	if got := crawlProfileActressName(userDataDir, visiblePath); got != "隐藏快照演员" {
		t.Fatalf("expected hidden profile actress for artifact-file input, got %q", got)
	}

	if err := os.Remove(hiddenPath); err != nil {
		t.Fatalf("remove hidden profile: %v", err)
	}
	if got := crawlProfileActressName(userDataDir, outputDir); got != "公开文件演员" {
		t.Fatalf("expected visible profile fallback, got %q", got)
	}
}

func TestResolveAutoSubscriptionActressNameRequiresSnapshotTarget(t *testing.T) {
	userDataDir := t.TempDir()
	outputDir := t.TempDir()

	if _, err := resolveAutoSubscriptionActressName("影片首位演员", userDataDir, outputDir); err == nil {
		t.Fatal("selected crawl output without a profile must not fall back to a movie actor")
	}
	if got, err := resolveAutoSubscriptionActressName("影片首位演员", userDataDir, ""); err != nil || got != "影片首位演员" {
		t.Fatalf("empty crawl output should preserve explicit fallback actor, got %q, err=%v", got, err)
	}
}
