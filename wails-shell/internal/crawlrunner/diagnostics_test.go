package crawlrunner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"javflow/internal/contracts/crawlartifact"
	"javflow/internal/crawloutput"
	"javflow/internal/crawlparse"
	"javflow/internal/crawlqueue"
	"javflow/internal/crawlrequest"
	"javflow/internal/crawltaskstate"
)

func TestRestorePersistedOutputStateRestoresFilteredItems(t *testing.T) {
	outputDir := t.TempDir()

	writer, err := crawloutput.NewWriter(outputDir)
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}

	_, err = writer.WriteFilmData(crawloutput.FilmData{
		Title:                  "DAZD-277 Sample",
		SourceLink:             "https://example.com/DAZD-277",
		Magnet:                 "magnet:?xt=urn:btih:AAAA",
		ActressCount:           57,
		FilteredByActressCount: true,
		FilterRemark:           "actress count 57 >= threshold 5, skip magnet-links.txt output only",
	})
	if err != nil {
		t.Fatalf("write filtered film data: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush writer: %v", err)
	}

	runner, err := NewRunner(Config{Output: outputDir}, outputDir)
	if err != nil {
		t.Fatalf("create runner: %v", err)
	}

	runner.restorePersistedOutputState()

	filteredItems := runner.filteredActressItems()
	if len(filteredItems) != 1 || filteredItems[0] != "DAZD-277" {
		t.Fatalf("expected filtered items [DAZD-277], got %#v", filteredItems)
	}

	if got := runner.filmCount; got != 1 {
		t.Fatalf("expected filmCount 1, got %d", got)
	}
}

func TestBuildReconciliationIncludesDuplicatesWithoutNestedLocking(t *testing.T) {
	tracker := NewTracker()
	tracker.RecordExpectedPageLinks(1, []string{
		"https://example.com/ABP-001",
		"https://example.com/ABP-001",
	})

	recon := tracker.BuildReconciliation()
	if len(recon.DuplicateExpectedIDs) != 1 || recon.DuplicateExpectedIDs[0] != "ABP-001" {
		t.Fatalf("expected duplicate ABP-001, got %#v", recon.DuplicateExpectedIDs)
	}
	if recon.RawDuplicateEntryCount != 1 {
		t.Fatalf("expected one raw duplicate entry, got %d", recon.RawDuplicateEntryCount)
	}
}

func TestStateDetailsIncludesInferredFailuresForFinalStatus(t *testing.T) {
	outputDir := t.TempDir()
	runner, err := NewRunner(Config{Output: outputDir}, outputDir)
	if err != nil {
		t.Fatalf("create runner: %v", err)
	}

	runner.tracker.RecordExpectedPageLinks(1, []string{"https://example.com/ABP-001"})

	runningDetails := runner.stateDetails(StatusRunning)
	if got := runningDetails["failedDetailsTotal"].(int); got != 0 {
		t.Fatalf("expected running failedDetailsTotal 0, got %d", got)
	}

	finalDetails := runner.stateDetails(StatusIncomplete)
	if got := finalDetails["unfinishedItemsTotal"].(int); got != 1 {
		t.Fatalf("expected unfinished total 1, got %d", got)
	}
	if got := finalDetails["failedDetailsTotal"].(int); got != 1 {
		t.Fatalf("expected final failedDetailsTotal 1, got %d", got)
	}

	items, ok := finalDetails["failedDetails"].([]FailedDetail)
	if !ok {
		t.Fatalf("expected failedDetails to be []FailedDetail, got %T", finalDetails["failedDetails"])
	}
	if len(items) != 1 || items[0].Item != "ABP-001" {
		t.Fatalf("unexpected inferred failed details: %#v", items)
	}
}

func TestRestorePersistedOutputStateReadsFilmDataFile(t *testing.T) {
	outputDir := t.TempDir()
	filmDataPath := filepath.Join(outputDir, "filmData.json")
	payload := []byte(`[
  {
    "title": "PBD-512 Sample",
    "sourceLink": "https://example.com/PBD-512",
    "actressCount": 13,
    "filteredByActressCount": true
  }
]`)
	if err := os.WriteFile(filmDataPath, payload, 0644); err != nil {
		t.Fatalf("write filmData.json: %v", err)
	}

	runner, err := NewRunner(Config{Output: outputDir}, outputDir)
	if err != nil {
		t.Fatalf("create runner: %v", err)
	}

	runner.restorePersistedOutputState()

	filteredItems := runner.filteredActressItems()
	if len(filteredItems) != 1 || filteredItems[0] != "PBD-512" {
		t.Fatalf("expected filtered items [PBD-512], got %#v", filteredItems)
	}
}

func TestFinalizeOutputArtifactsWritesContractFiles(t *testing.T) {
	outputDir := t.TempDir()

	runner, err := NewRunner(Config{
		Output: outputDir,
		Base:   "https://www.javbus.com/star/test",
		Search: "结城りの",
		Limit:  246,
	}, outputDir)
	if err != nil {
		t.Fatalf("create runner: %v", err)
	}

	runner.startedAt = "2026-05-05T10:00:00Z"
	_, err = runner.writer.WriteFilmData(crawloutput.FilmData{
		Title:      "ABP-889 Sample",
		SourceLink: "https://example.com/ABP-889",
		Actress:    []string{"结城りの"},
		Magnet:     "magnet:?xt=urn:btih:AAAA",
	})
	if err != nil {
		t.Fatalf("write output record: %v", err)
	}
	runner.filmCount = 1
	runner.tracker.RecordExpectedPageLinks(1, []string{"https://example.com/ABP-889"})

	runner.finalizeOutputArtifacts(FinalStateOutput{
		Status:  StatusCompleted,
		Message: "crawl completed",
	})

	profilePath := filepath.Join(outputDir, crawlartifact.CrawlProfileFile)
	profileBytes, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("read crawl profile: %v", err)
	}
	var profile crawlartifact.CrawlProfileArtifact
	if err := json.Unmarshal(profileBytes, &profile); err != nil {
		t.Fatalf("unmarshal crawl profile: %v", err)
	}
	if profile.TargetCount != 246 {
		t.Fatalf("expected targetCount 246, got %#v", profile)
	}
	if profile.ActressName != "结城りの" {
		t.Fatalf("expected actressName 结城りの, got %#v", profile)
	}

	organizerPath := filepath.Join(outputDir, crawlartifact.OrganizerCodesFile)
	if _, err := os.Stat(organizerPath); err != nil {
		t.Fatalf("expected organizer codes artifact: %v", err)
	}
}

func TestProcessDetailTaskNomagStillFetchesMagnetAndPersistsMagneticRecord(t *testing.T) {
	outputDir := t.TempDir()
	runner, err := NewRunner(Config{
		Output: outputDir,
		Nomag:  true,
	}, outputDir)
	if err != nil {
		t.Fatalf("create runner: %v", err)
	}

	magnetFetched := false
	runner.fetchDetail = func(ctx context.Context, detailURL string) (crawlparse.Metadata, crawlrequest.PageResponse, error) {
		return crawlparse.Metadata{
			GID:   "1",
			UC:    "1",
			Img:   "cover.jpg",
			Title: "ABP-889 Sample",
		}, crawlrequest.PageResponse{}, nil
	}
	runner.fetchMagnetFn = func(ctx context.Context, gid string, uc string, img string, title string) (*crawlrequest.MagnetResult, error) {
		magnetFetched = true
		return &crawlrequest.MagnetResult{
			Magnet: "magnet:?xt=urn:btih:AAAA",
			MagnetLinks: []crawlrequest.MagnetLink{
				{Link: "magnet:?xt=urn:btih:AAAA", Size: "1.00GB"},
			},
		}, nil
	}

	if err := runner.processDetailTask(context.Background(), crawlqueue.DetailPageTask{Link: "https://example.com/ABP-889"}); err != nil {
		t.Fatalf("processDetailTask failed: %v", err)
	}
	if !magnetFetched {
		t.Fatal("expected nomag path to still fetch magnet")
	}
	if got := runner.writer.RecordCount(); got != 1 {
		t.Fatalf("expected one persisted record, got %d", got)
	}
	if err := runner.writer.Flush(); err != nil {
		t.Fatalf("flush writer: %v", err)
	}
	magnetBytes, err := os.ReadFile(filepath.Join(outputDir, crawlartifact.DefaultMagnetTxt))
	if err != nil {
		t.Fatalf("read magnet file: %v", err)
	}
	if string(magnetBytes) == "" {
		t.Fatal("expected magnet-links.txt to contain magnet when nomag item has magnet")
	}
}

func TestProcessDetailTaskNomagSkipsOnlyWhenMagnetMissingAfterLookup(t *testing.T) {
	outputDir := t.TempDir()
	runner, err := NewRunner(Config{
		Output: outputDir,
		Nomag:  true,
	}, outputDir)
	if err != nil {
		t.Fatalf("create runner: %v", err)
	}

	runner.fetchDetail = func(ctx context.Context, detailURL string) (crawlparse.Metadata, crawlrequest.PageResponse, error) {
		return crawlparse.Metadata{
			GID:   "1",
			UC:    "1",
			Img:   "cover.jpg",
			Title: "ABP-890 Sample",
		}, crawlrequest.PageResponse{}, nil
	}
	runner.fetchMagnetFn = func(ctx context.Context, gid string, uc string, img string, title string) (*crawlrequest.MagnetResult, error) {
		return nil, nil
	}

	if err := runner.processDetailTask(context.Background(), crawlqueue.DetailPageTask{Link: "https://example.com/ABP-890"}); err != nil {
		t.Fatalf("processDetailTask failed: %v", err)
	}
	if got := runner.writer.RecordCount(); got != 0 {
		t.Fatalf("expected no persisted record when nomag item has no magnet, got %d", got)
	}
	recon := runner.tracker.BuildReconciliation()
	if len(recon.SkippedItemIDs) != 1 {
		t.Fatalf("expected one skipped item, got %#v", recon.SkippedItemIDs)
	}
}

func TestProcessDetailTaskMetadataOnlyPersistsMetadataWithoutMagnetLookup(t *testing.T) {
	outputDir := t.TempDir()
	runner, err := NewRunner(Config{Output: outputDir, MetadataOnly: true}, outputDir)
	if err != nil {
		t.Fatalf("create runner: %v", err)
	}
	magnetFetched := false
	runner.fetchDetail = func(ctx context.Context, detailURL string) (crawlparse.Metadata, crawlrequest.PageResponse, error) {
		return crawlparse.Metadata{
			GID: "1", UC: "1", Img: "cover.jpg", Title: "ABP-891 Sample",
			Maker: "Studio Example", Label: "Example Label", Series: "Example Series",
		}, crawlrequest.PageResponse{}, nil
	}
	runner.fetchMagnetFn = func(ctx context.Context, gid string, uc string, img string, title string) (*crawlrequest.MagnetResult, error) {
		magnetFetched = true
		return nil, nil
	}

	if err := runner.processDetailTask(context.Background(), crawlqueue.DetailPageTask{Link: "https://example.com/ABP-891"}); err != nil {
		t.Fatalf("processDetailTask failed: %v", err)
	}
	if magnetFetched {
		t.Fatal("metadata-only mode must not call the magnet endpoint")
	}
	if got := runner.writer.RecordCount(); got != 1 {
		t.Fatalf("expected one persisted metadata record, got %d", got)
	}
	completed := runner.completedItemIDs()
	if len(completed) != 1 || completed[0] != "ABP-891" {
		t.Fatalf("metadata-only record should count as completed: %#v", completed)
	}
	if err := runner.writer.Flush(); err != nil {
		t.Fatalf("flush writer: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(outputDir, crawlartifact.CrawlFilmDataFile))
	if err != nil {
		t.Fatalf("read filmData.json: %v", err)
	}
	var records []crawloutput.FilmData
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatalf("unmarshal filmData.json: %v", err)
	}
	if len(records) != 1 || records[0].Maker != "Studio Example" || records[0].Series != "Example Series" {
		t.Fatalf("metadata fields were not persisted: %#v", records)
	}
}

func TestCompletedItemIDsExcludesFilteredAndSkipped(t *testing.T) {
	outputDir := t.TempDir()
	runner, err := NewRunner(Config{Output: outputDir}, outputDir)
	if err != nil {
		t.Fatalf("create runner: %v", err)
	}

	runner.config.FilmCodeFilterThreshold = "VR"

	// ABP-001: persisted with magnet, not filtered -> completed
	runner.tracker.MarkPersisted("https://example.com/ABP-001", "ABP-001")
	// VR-001: persisted but filtered by film code and actress count -> not completed
	runner.tracker.MarkPersisted("https://example.com/VR-001", "VR-001")
	runner.recordActressCountFiltered("https://example.com/VR-001")
	runner.recordFilmCodeFiltered("https://example.com/VR-001")
	// ABC-002: persisted but skipped (no magnet) -> not completed
	runner.tracker.MarkPersisted("https://example.com/ABC-002", "ABC-002")
	runner.tracker.MarkSkipped("ABC-002")

	completed := runner.completedItemIDs()
	if len(completed) != 1 || completed[0] != "ABP-001" {
		t.Fatalf("expected completed [ABP-001], got %#v", completed)
	}

	stats := runner.stats()
	if stats.CompletedItems != 1 {
		t.Fatalf("expected CompletedItems 1, got %d", stats.CompletedItems)
	}
	if len(stats.CompletedItemIDs) != 1 || stats.CompletedItemIDs[0] != "ABP-001" {
		t.Fatalf("expected CompletedItemIDs [ABP-001], got %#v", stats.CompletedItemIDs)
	}

	details := runner.stateDetails(StatusCompleted)
	if got := details["completedItemsTotal"].(int); got != 1 {
		t.Fatalf("expected completedItemsTotal 1, got %d", got)
	}
	ids, ok := details["completedItemIds"].([]string)
	if !ok || len(ids) != 1 || ids[0] != "ABP-001" {
		t.Fatalf("expected completedItemIds [ABP-001], got %#v", details["completedItemIds"])
	}
}

func TestTerminalStatsUseExplicitCompletionAccounting(t *testing.T) {
	outputDir := t.TempDir()
	runner, err := NewRunner(Config{Output: outputDir}, outputDir)
	if err != nil {
		t.Fatalf("create runner: %v", err)
	}

	runner.tracker.RecordExpectedPageLinks(1, []string{
		"https://example.com/ABP-001",
		"https://example.com/ABP-001",
		"https://example.com/ABP-002",
		"https://example.com/ABP-003",
		"https://example.com/ABP-004",
	})
	runner.tracker.MarkPersisted("https://example.com/ABP-001", "ABP-001")
	runner.tracker.MarkPersisted("https://example.com/ABP-002", "ABP-002")
	runner.recordActressCountFiltered("https://example.com/ABP-002")
	runner.recordDetailFailure("https://example.com/ABP-003", "详情页请求失败")

	stats := runner.statsForStatus(StatusCompleted)
	if stats.TotalItems != 5 || stats.FilteredItemsCount != 1 || stats.DuplicateItemsCount != 1 || stats.FailedItemsCount != 2 {
		t.Fatalf("unexpected completion breakdown: %+v", stats)
	}
	if stats.Completed != 1 {
		t.Fatalf("expected completed=5-1-1-2=1, got %d", stats.Completed)
	}

	details := runner.stateDetails(StatusCompleted)
	if got := details["completedCount"].(int); got != 1 {
		t.Fatalf("expected terminal completedCount 1, got %d", got)
	}
	duplicates := details["duplicateItems"].([]string)
	if len(duplicates) != 1 || duplicates[0] != "ABP-001" {
		t.Fatalf("expected duplicate ABP-001, got %#v", duplicates)
	}
}

func TestCalculateCompletedCountUsesDurableIDsWhileRunning(t *testing.T) {
	breakdown := completionBreakdown{
		Total:      8,
		Filtered:   1,
		Duplicates: 2,
		Failed:     1,
	}

	if got := calculateCompletedCount(StatusRunning, breakdown, []string{"ABP-001", "ABP-002"}); got != 2 {
		t.Fatalf("running count must use durable IDs, got %d", got)
	}
	if got := calculateCompletedCount(StatusCompleted, breakdown, []string{"ABP-001", "ABP-002"}); got != 4 {
		t.Fatalf("terminal count must use total-filtered-duplicate-failed, got %d", got)
	}
}

func TestRestoredFailureIDsNormalizeBeforeCompletionAccounting(t *testing.T) {
	outputDir := t.TempDir()
	runner, err := NewRunner(Config{Output: outputDir}, outputDir)
	if err != nil {
		t.Fatalf("create runner: %v", err)
	}
	runner.tracker.RecordExpectedPageLinks(1, []string{"https://example.com/ABA-250"})
	runner.tracker.MarkPersisted("https://example.com/ABA-250", "ABA-250")
	runner.restoreFailedDetails([]crawltaskstate.FailedDetailRecord{{
		Item:       "aba-250",
		SourceLink: "https://example.com/aba-250",
		Reason:     "详情页失败",
	}})

	failed := runner.failedItemIDs(true)
	if len(failed) != 1 || failed[0] != "ABA-250" {
		t.Fatalf("expected canonical failed ID, got %#v", failed)
	}
	completed := runner.completedItemIDsFor(true)
	if len(completed) != 0 {
		t.Fatalf("failed persisted item must be excluded from completed IDs, got %#v", completed)
	}
}

func TestDetectPrimaryActressPrefersExplicitSearchTarget(t *testing.T) {
	outputDir := t.TempDir()
	runner, err := NewRunner(Config{Output: outputDir, Search: "目标女优"}, outputDir)
	if err != nil {
		t.Fatalf("create runner: %v", err)
	}
	_, err = runner.writer.WriteFilmData(crawloutput.FilmData{
		Title:      "ABC-001 合集",
		SourceLink: "https://example.com/ABC-001",
		Actress:    []string{"其他女优", "目标女优"},
	})
	if err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, err = runner.writer.WriteFilmData(crawloutput.FilmData{
		Title:      "ABC-002 合集",
		SourceLink: "https://example.com/ABC-002",
		Actress:    []string{"其他女优"},
	})
	if err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if got := runner.detectPrimaryActressName(); got != "目标女优" {
		t.Fatalf("expected explicit search target, got %q", got)
	}
}
