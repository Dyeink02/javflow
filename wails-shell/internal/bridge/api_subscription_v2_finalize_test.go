package bridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"javflow/internal/contracts/crawlartifact"
	"javflow/internal/crawloutput"
)

func TestFinalizeSubscriptionOutputByCodesKeepsOnlyPendingCodes(t *testing.T) {
	outputDir := t.TempDir()
	records := []map[string]any{
		{
			"title":      "MIDA-616 old item full title",
			"sourceLink": "https://www.javbus.com/mida-616",
			"magnet":     "magnet:?xt=urn:btih:old",
		},
		{
			"title":      "MIDA-438 pending item full title",
			"sourceLink": "https://www.javbus.com/mida-438",
			"magnet":     "magnet:?xt=urn:btih:new",
		},
	}
	payload, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, crawlartifact.CrawlFilmDataFile), payload, 0o644); err != nil {
		t.Fatal(err)
	}

	finalized, err := finalizeSubscriptionOutputByCodes(outputDir, []string{"MIDA-438"})
	if err != nil {
		t.Fatalf("finalizeSubscriptionOutputByCodes returned error: %v", err)
	}
	if len(finalized.KeptCodes) != 1 || finalized.KeptCodes[0] != "MIDA-438" {
		t.Fatalf("expected to keep only MIDA-438, got %#v", finalized.KeptCodes)
	}

	_, filteredRecords, err := crawlartifact.ReadFilmDataRecords(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(filteredRecords) != 1 {
		t.Fatalf("expected one filtered record, got %d", len(filteredRecords))
	}
	if code := resolveSubscriptionRecordCode(filteredRecords[0], 0); code != "MIDA-438" {
		t.Fatalf("expected kept filmData record MIDA-438, got %q", code)
	}

	magnetText := strings.Join(finalized.MagnetLines, "\n")
	if !strings.Contains(magnetText, "btih:new") || strings.Contains(magnetText, "btih:old") {
		t.Fatalf("unexpected filtered magnet output: %q", magnetText)
	}
}

func TestSubscriptionFinalizeKeepsOnlyDatedTXTAndHiddenJSON(t *testing.T) {
	outputDir := t.TempDir()
	userDataDir := t.TempDir()
	records := []map[string]any{{
		"title":      "CJOB-209 pending item",
		"sourceLink": "https://www.javbus.com/CJOB-209",
		"magnet":     "magnet:?xt=urn:btih:cjob209",
	}}
	payload, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, crawlartifact.CrawlFilmDataFile), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(outputDir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "logs", "latest-log.txt"), []byte("old log"), 0o644); err != nil {
		t.Fatal(err)
	}

	finalized, err := finalizeSubscriptionOutputByCodes(outputDir, []string{"CJOB-209"})
	if err != nil {
		t.Fatal(err)
	}
	if err := crawloutput.SyncInternalArtifactsFromVisible(userDataDir, outputDir, crawloutput.ArtifactMetadata{
		ActressName:    "松本いちか",
		TargetCount:    1,
		CompletedCount: 1,
	}); err != nil {
		t.Fatal(err)
	}
	visibleFile, err := writeSubscriptionDatedMagnetFile(
		outputDir,
		finalized.MagnetLines,
		len(finalized.KeptCodes),
		time.Date(2026, 7, 19, 16, 0, 0, 0, time.Local),
	)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(visibleFile) != "2026\u5e747\u670819\u53f7\u66f4\u65b01\u90e8.txt" {
		t.Fatalf("unexpected visible filename: %s", visibleFile)
	}
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "2026\u5e747\u670819\u53f7\u66f4\u65b01\u90e8.txt" {
		t.Fatalf("visible output must contain only dated TXT, got %#v", entries)
	}
	if _, hiddenRecords, err := crawlartifact.ReadFilmDataRecordsWithUserData(outputDir, userDataDir); err != nil {
		t.Fatalf("hidden JSON must remain readable: %v", err)
	} else if len(hiddenRecords) != 1 || resolveSubscriptionRecordCode(hiddenRecords[0], 0) != "CJOB-209" {
		t.Fatalf("unexpected hidden records: %#v", hiddenRecords)
	}
}

func TestDiffSubscriptionTargetCodesReportsMissing(t *testing.T) {
	missing := diffSubscriptionTargetCodes([]string{"FWAY-087", "MIDA-438"}, []string{"FWAY-87"})
	if len(missing) != 1 || missing[0] != "MIDA-438" {
		t.Fatalf("expected only MIDA-438 missing, got %#v", missing)
	}
}

func TestResolveSubscriptionFinalizeOutputDirPrefersCurrentTaskRunDir(t *testing.T) {
	got := resolveSubscriptionFinalizeOutputDir(map[string]any{
		"outputDir":            `C:\AV订阅`,
		"currentTaskOutputDir": `C:\AV订阅\run-20260524-163431`,
		"lastTaskOutputDir":    `C:\AV订阅\old-run`,
	})

	if got != `C:\AV订阅\run-20260524-163431` {
		t.Fatalf("expected current task run dir, got %q", got)
	}
}
