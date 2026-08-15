package avsubscription

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"javflow/internal/contracts/crawlartifact"
	runtimepaths "javflow/internal/runtime"
)

func TestScanOutputKeepsPrimaryActressOnly(t *testing.T) {
	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "Mikami Yua6")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output: %v", err)
	}

	records := []map[string]any{
		{
			"title":      "AAA-001",
			"sourceLink": "https://www.javbus.com/AAA-001",
			"actress":    []string{"Mikami Yua"},
		},
		{
			"title":      "AAA-002",
			"sourceLink": "https://www.javbus.com/AAA-002",
			"actress":    []string{"Mikami Yua", "Guest Actress A", "Guest Actress B"},
		},
		{
			"title":      "AAA-003",
			"sourceLink": "https://www.javbus.com/AAA-003",
			"actress":    []string{"Mikami Yua", "Guest Actress C"},
		},
	}

	writeFilmData(t, outputDir, records)

	service := NewService(runtimepaths.Paths{
		UserData:  filepath.Join(tempDir, "user-data"),
		Documents: tempDir,
	})

	result, err := service.ScanOutput(outputDir)
	if err != nil {
		t.Fatalf("scan output: %v", err)
	}

	if result.ScannedActresses != 1 {
		t.Fatalf("expected 1 primary actress, got %d", result.ScannedActresses)
	}
	if len(result.ScannedActressList) != 1 || result.ScannedActressList[0] != "Mikami Yua" {
		t.Fatalf("unexpected scanned actress list: %#v", result.ScannedActressList)
	}

	subs, err := service.List()
	if err != nil {
		t.Fatalf("list subscriptions: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription after scan, got %d", len(subs))
	}
	if subs[0].ActressName != "Mikami Yua" {
		t.Fatalf("expected primary actress subscription, got %+v", subs[0])
	}
	if subs[0].SyncedCount != 3 || subs[0].CurrentCount != 3 || subs[0].PendingCount != 0 {
		t.Fatalf("unexpected counts for primary actress: %+v", subs[0])
	}
}

func TestScanOutputFallsBackToDominantActress(t *testing.T) {
	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "run-20260430-213011")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output: %v", err)
	}

	records := []map[string]any{
		{
			"title":      "BBB-001",
			"sourceLink": "https://www.javbus.com/BBB-001",
			"actress":    []string{"Primary Actress", "Guest Actress"},
		},
		{
			"title":      "BBB-002",
			"sourceLink": "https://www.javbus.com/BBB-002",
			"actress":    []string{"Primary Actress"},
		},
		{
			"title":      "BBB-003",
			"sourceLink": "https://www.javbus.com/BBB-003",
			"actress":    []string{"Guest Actress"},
		},
	}

	writeFilmData(t, outputDir, records)

	service := NewService(runtimepaths.Paths{
		UserData:  filepath.Join(tempDir, "user-data"),
		Documents: tempDir,
	})

	result, err := service.ScanOutput(outputDir)
	if err != nil {
		t.Fatalf("scan output: %v", err)
	}

	if len(result.ScannedActressList) != 1 || result.ScannedActressList[0] != "Primary Actress" {
		t.Fatalf("unexpected scanned actress list: %#v", result.ScannedActressList)
	}
}

func TestScanOutputPrefersCrawlProfileArtifact(t *testing.T) {
	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "结城りの")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output: %v", err)
	}

	profile := crawlartifact.CrawlProfileArtifact{
		SchemaVersion:  crawlartifact.CurrentSchemaVersion,
		RunID:          "crawl-test-run",
		CompletedAt:    "2026-05-05T12:00:00Z",
		ActressName:    "结城りの",
		CrawlURL:       "https://www.javbus.com/star/okq?page=1",
		TargetCount:    246,
		CompletedCount: 243,
		OutputDir:      outputDir,
		FilmDataPath:   filepath.Join(outputDir, crawlartifact.CrawlFilmDataFile),
		SiteBase:       "https://www.javbus.com",
	}
	payload, err := json.Marshal(profile)
	if err != nil {
		t.Fatalf("marshal profile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, crawlartifact.CrawlProfileFile), payload, 0o644); err != nil {
		t.Fatalf("write crawl-profile.json: %v", err)
	}

	service := NewService(runtimepaths.Paths{
		UserData:  filepath.Join(tempDir, "user-data"),
		Documents: tempDir,
	})

	result, err := service.ScanOutput(outputDir)
	if err != nil {
		t.Fatalf("scan output: %v", err)
	}
	if result.SourceType != scanSourceCrawlProfile {
		t.Fatalf("expected sourceType crawlProfile, got %q", result.SourceType)
	}
	if len(result.ScannedActressList) != 1 || result.ScannedActressList[0] != "结城りの" {
		t.Fatalf("unexpected scanned actress list: %#v", result.ScannedActressList)
	}

	subs, err := service.List()
	if err != nil {
		t.Fatalf("list subscriptions: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(subs))
	}
	if subs[0].ActressName != "结城りの" || subs[0].CrawlURL != "https://www.javbus.com/star/okq?page=1" {
		t.Fatalf("unexpected subscription: %+v", subs[0])
	}
	if subs[0].SyncedCount != 243 || subs[0].CurrentCount != 243 {
		t.Fatalf("unexpected counts: %+v", subs[0])
	}
}

func TestScanOutputProfileImportKeepsExistingTargetMetadataWhenProfileOmitsIt(t *testing.T) {
	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "Yuki Rino")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output: %v", err)
	}

	service := NewService(runtimepaths.Paths{
		UserData:  filepath.Join(tempDir, "user-data"),
		Documents: tempDir,
	})

	if _, err := service.Upsert(Subscription{
		ActressName:   "Yuki Rino",
		CrawlURL:      "https://www.javbus.com/star/okq?page=1",
		PreferredBase: "https://www.javbus.com",
		SyncedCount:   200,
		CurrentCount:  200,
		ItemsPerPage:  60,
	}); err != nil {
		t.Fatalf("seed subscription: %v", err)
	}

	profile := crawlartifact.CrawlProfileArtifact{
		SchemaVersion:  crawlartifact.CurrentSchemaVersion,
		RunID:          "crawl-test-run",
		CompletedAt:    "2026-05-05T12:00:00Z",
		ActressName:    "Yuki Rino",
		CompletedCount: 243,
		OutputDir:      outputDir,
		FilmDataPath:   filepath.Join(outputDir, crawlartifact.CrawlFilmDataFile),
	}
	payload, err := json.Marshal(profile)
	if err != nil {
		t.Fatalf("marshal profile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, crawlartifact.CrawlProfileFile), payload, 0o644); err != nil {
		t.Fatalf("write crawl-profile.json: %v", err)
	}

	result, err := service.ScanOutput(outputDir)
	if err != nil {
		t.Fatalf("scan output: %v", err)
	}
	if result.UpdatedCount != 1 || result.AddedCount != 0 {
		t.Fatalf("unexpected scan update counts: %+v", result)
	}

	subs, err := service.List()
	if err != nil {
		t.Fatalf("list subscriptions: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(subs))
	}
	if subs[0].CrawlURL != "https://www.javbus.com/star/okq?page=1" {
		t.Fatalf("expected existing crawl url to stay, got %+v", subs[0])
	}
	if subs[0].PreferredBase != "https://www.javbus.com" {
		t.Fatalf("expected existing preferred base to stay, got %+v", subs[0])
	}
	if subs[0].ItemsPerPage != 60 {
		t.Fatalf("expected existing itemsPerPage to stay, got %+v", subs[0])
	}
	if subs[0].SyncedCount != 243 || subs[0].CurrentCount != 243 {
		t.Fatalf("expected updated counts from profile import, got %+v", subs[0])
	}
}

func TestScanOutputAcceptsLegacyCrawlProfileWithoutSchemaVersion(t *testing.T) {
	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "结城りの")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output: %v", err)
	}

	normalizedOutputDir := filepath.ToSlash(outputDir)
	normalizedFilmDataPath := filepath.ToSlash(filepath.Join(outputDir, crawlartifact.CrawlFilmDataFile))
	payload := []byte(`{
  "runId": "legacy-run",
  "completedAt": "2026-05-05T12:00:00Z",
  "actressName": "结城りの",
  "crawlURL": "https://www.javbus.com/star/okq?page=1",
  "targetCount": 246,
  "completedCount": 243,
  "outputDir": "` + normalizedOutputDir + `",
  "filmDataPath": "` + normalizedFilmDataPath + `",
  "siteBase": "https://www.javbus.com"
}`)

	if err := os.WriteFile(filepath.Join(outputDir, crawlartifact.CrawlProfileFile), payload, 0o644); err != nil {
		t.Fatalf("write legacy crawl-profile.json: %v", err)
	}

	service := NewService(runtimepaths.Paths{
		UserData:  filepath.Join(tempDir, "user-data"),
		Documents: tempDir,
	})

	result, err := service.ScanOutput(outputDir)
	if err != nil {
		t.Fatalf("scan output: %v", err)
	}
	if result.SourceType != scanSourceCrawlProfile {
		t.Fatalf("expected sourceType crawlProfile, got %q", result.SourceType)
	}
	if len(result.ScannedActressList) != 1 || result.ScannedActressList[0] != "结城りの" {
		t.Fatalf("unexpected scanned actress list: %#v", result.ScannedActressList)
	}
	subs, err := service.List()
	if err != nil {
		t.Fatalf("list subscriptions: %v", err)
	}
	if len(subs) != 1 || subs[0].SyncedCount != 243 || subs[0].CurrentCount != 243 {
		t.Fatalf("unexpected subscriptions from legacy crawl profile: %#v", subs)
	}
}

func TestScanOutputPrefersInternalCrawlProfileArtifactWhenOutputDirHasOnlyFilmData(t *testing.T) {
	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "Seto Kanna")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output: %v", err)
	}

	writeFilmData(t, outputDir, []map[string]any{
		{
			"title":      "AAA-001",
			"sourceLink": "https://www.javbus.com/AAA-001",
			"actress":    []string{"Fallback Actress"},
		},
	})

	userDataDir := filepath.Join(tempDir, "user-data")
	internalPaths := crawlartifact.ResolveInternalArtifactPaths(userDataDir, outputDir)
	if err := os.MkdirAll(filepath.Dir(internalPaths.CrawlProfilePath), 0o755); err != nil {
		t.Fatalf("mkdir internal artifact dir: %v", err)
	}

	profile := crawlartifact.CrawlProfileArtifact{
		SchemaVersion:  crawlartifact.CurrentSchemaVersion,
		RunID:          "crawl-test-run",
		CompletedAt:    "2026-05-11T12:00:00Z",
		ActressName:    "Seto Kanna",
		CrawlURL:       "https://www.javbus.com/star/138y",
		TargetCount:    1,
		CompletedCount: 1,
		OutputDir:      outputDir,
		FilmDataPath:   filepath.Join(outputDir, crawlartifact.CrawlFilmDataFile),
		SiteBase:       "https://www.javbus.com",
	}
	payload, err := json.Marshal(profile)
	if err != nil {
		t.Fatalf("marshal profile: %v", err)
	}
	if err := os.WriteFile(internalPaths.CrawlProfilePath, payload, 0o644); err != nil {
		t.Fatalf("write internal crawl-profile: %v", err)
	}

	service := NewService(runtimepaths.Paths{
		UserData:  userDataDir,
		Documents: tempDir,
	})

	result, err := service.ScanOutput(outputDir)
	if err != nil {
		t.Fatalf("scan output: %v", err)
	}
	if result.SourceType != scanSourceCrawlProfile {
		t.Fatalf("expected internal crawlProfile sourceType, got %q", result.SourceType)
	}
	if result.CrawlProfilePath != internalPaths.CrawlProfilePath {
		t.Fatalf("expected internal crawl profile path %q, got %q", internalPaths.CrawlProfilePath, result.CrawlProfilePath)
	}
	if len(result.ScannedActressList) != 1 || result.ScannedActressList[0] != "Seto Kanna" {
		t.Fatalf("unexpected scanned actress list: %#v", result.ScannedActressList)
	}
}

func TestScanOutputRecoversTargetURLFromLatestLogWhenProfileIsMissing(t *testing.T) {
	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "Fukuda Yua")
	if err := os.MkdirAll(filepath.Join(outputDir, crawlartifact.DefaultLogDirName), 0o755); err != nil {
		t.Fatalf("mkdir log dir: %v", err)
	}

	writeFilmData(t, outputDir, []map[string]any{
		{
			"title":      "MIDA-616",
			"sourceLink": "https://www.javbus.com/MIDA-616",
			"actress":    []string{"Fukuda Yua"},
		},
	})

	logText := "JavFlow 任务日志\r\n起始地址: https://www.javbus.com/star/1498\r\n"
	if err := os.WriteFile(filepath.Join(outputDir, crawlartifact.DefaultLogDirName, crawlartifact.DefaultLatestLogTxt), []byte(logText), 0o644); err != nil {
		t.Fatalf("write latest-log: %v", err)
	}

	service := NewService(runtimepaths.Paths{
		UserData:  filepath.Join(tempDir, "user-data"),
		Documents: tempDir,
	})

	result, err := service.ScanOutput(outputDir)
	if err != nil {
		t.Fatalf("scan output: %v", err)
	}
	if result.SourceType != scanSourceFilmData {
		t.Fatalf("expected sourceType filmData, got %q", result.SourceType)
	}

	subs, err := service.List()
	if err != nil {
		t.Fatalf("list subscriptions: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(subs))
	}
	if subs[0].CrawlURL != "https://www.javbus.com/star/1498" {
		t.Fatalf("expected recovered crawl url, got %+v", subs[0])
	}
	if subs[0].PreferredBase != "https://www.javbus.com" {
		t.Fatalf("expected recovered preferred base, got %+v", subs[0])
	}
}

func TestScanOutputRecoversTargetURLFromQualitySummaryWhenProfileIsMissing(t *testing.T) {
	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "Fukuda Yua 2")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output: %v", err)
	}

	writeFilmData(t, outputDir, []map[string]any{
		{
			"title":      "FWAY-087",
			"sourceLink": "https://www.javbus.com/FWAY-087",
			"actress":    []string{"Fukuda Yua"},
		},
	})

	reportText := "JAV 自动化爬虫 - 运行质量摘要\r\n任务信息：\r\n起始地址：https://www.javbus.com/star/1498\r\n"
	if err := os.WriteFile(filepath.Join(outputDir, crawlartifact.DefaultQualitySummaryTxt), []byte(reportText), 0o644); err != nil {
		t.Fatalf("write crawl-quality-summary: %v", err)
	}

	service := NewService(runtimepaths.Paths{
		UserData:  filepath.Join(tempDir, "user-data"),
		Documents: tempDir,
	})

	if _, err := service.ScanOutput(outputDir); err != nil {
		t.Fatalf("scan output: %v", err)
	}

	subs, err := service.List()
	if err != nil {
		t.Fatalf("list subscriptions: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(subs))
	}
	if subs[0].CrawlURL != "https://www.javbus.com/star/1498" {
		t.Fatalf("expected recovered crawl url from quality summary, got %+v", subs[0])
	}
}

func TestClearRemovesAllSubscriptions(t *testing.T) {
	service := NewService(runtimepaths.Paths{
		UserData:  filepath.Join(t.TempDir(), "user-data"),
		Documents: t.TempDir(),
	})

	if _, err := service.Upsert(Subscription{ActressName: "Actress A", CurrentCount: 10, SyncedCount: 10}); err != nil {
		t.Fatalf("upsert actress A: %v", err)
	}
	if _, err := service.Upsert(Subscription{ActressName: "Actress B", CurrentCount: 20, SyncedCount: 20}); err != nil {
		t.Fatalf("upsert actress B: %v", err)
	}

	clearedCount, err := service.Clear()
	if err != nil {
		t.Fatalf("clear subscriptions: %v", err)
	}
	if clearedCount != 2 {
		t.Fatalf("expected cleared count 2, got %d", clearedCount)
	}

	subs, err := service.List()
	if err != nil {
		t.Fatalf("list subscriptions: %v", err)
	}
	if len(subs) != 0 {
		t.Fatalf("expected 0 subscriptions after clear, got %d", len(subs))
	}
}

func TestMarkSyncedClearsPendingCount(t *testing.T) {
	service := NewService(runtimepaths.Paths{
		UserData:  filepath.Join(t.TempDir(), "user-data"),
		Documents: t.TempDir(),
	})

	saved, err := service.Upsert(Subscription{
		ActressName:  "Actress X",
		CurrentCount: 480,
		SyncedCount:  470,
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if saved.PendingCount != 10 {
		t.Fatalf("expected pending 10, got %d", saved.PendingCount)
	}

	synced, err := service.MarkSynced(saved.ID)
	if err != nil {
		t.Fatalf("mark synced: %v", err)
	}
	if synced.PendingCount != 0 || synced.SyncedCount != 480 {
		t.Fatalf("expected synced subscription to clear pending count, got %+v", synced)
	}
}

func TestListOrdersByPendingFirst(t *testing.T) {
	service := NewService(runtimepaths.Paths{
		UserData:  filepath.Join(t.TempDir(), "user-data"),
		Documents: t.TempDir(),
	})
	var err error

	_, err = service.Upsert(Subscription{
		ActressName:  "Actress A",
		CurrentCount: 20,
		SyncedCount:  15,
	})
	if err != nil {
		t.Fatalf("upsert actress A: %v", err)
	}

	_, err = service.Upsert(Subscription{
		ActressName:  "Actress B",
		CurrentCount: 18,
		SyncedCount:  18,
	})
	if err != nil {
		t.Fatalf("upsert actress B: %v", err)
	}

	items, err := service.List()
	if err != nil {
		t.Fatalf("list subscriptions: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].ActressName != "Actress A" {
		t.Fatalf("expected pending item first, got %#v", items)
	}
}

func TestListPreservesUnreadableStateInsteadOfTreatingItAsEmpty(t *testing.T) {
	userData := filepath.Join(t.TempDir(), "user-data")
	service := NewService(runtimepaths.Paths{UserData: userData, Documents: t.TempDir()})
	storagePath := filepath.Join(userData, storageDirName, storageFileName)
	if err := os.MkdirAll(filepath.Dir(storagePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storagePath, []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := service.List(); err == nil {
		t.Fatal("expected unreadable subscription state to be reported")
	}
	backups, err := filepath.Glob(storagePath + ".corrupt-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected one preserved corrupt state file, got %v", backups)
	}
	if _, err := os.Stat(storagePath); !os.IsNotExist(err) {
		t.Fatalf("unreadable state should not remain active, stat err=%v", err)
	}
}

func writeFilmData(t *testing.T, outputDir string, records []map[string]any) {
	t.Helper()

	payload, err := json.Marshal(records)
	if err != nil {
		t.Fatalf("marshal records: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, crawlartifact.CrawlFilmDataFile), payload, 0o644); err != nil {
		t.Fatalf("write filmData: %v", err)
	}
}
