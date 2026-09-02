package bridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"javflow/internal/avsubscriptionv2"
	"javflow/internal/contracts/crawlartifact"
	"javflow/internal/organizer"
	runtimepaths "javflow/internal/runtime"
)

func TestLoadOrganizerCrawlFilmCodesResultAcceptsArtifactPath(t *testing.T) {
	outputDir := t.TempDir()
	artifact := crawlartifact.OrganizerCodesArtifact{
		RunID:           "crawl-test-run",
		CompletedAt:     "2026-05-06T11:20:00Z",
		ActressName:     "Yuki Rino",
		OutputDir:       outputDir,
		FilmDataPath:    filepath.Join(outputDir, crawlartifact.CrawlFilmDataFile),
		TotalRecords:    243,
		UniqueCodeCount: 2,
		Codes:           []string{"ABP-889", "SSIS-123"},
		CodeEntries: []crawlartifact.CodeEntry{
			{
				Code:  "ABP-889",
				Title: "ABP-889 sample",
				Magnets: []crawlartifact.MagnetEntry{
					{Link: "magnet:?xt=urn:btih:AAA", Size: "2.1GB"},
				},
			},
			{Code: "SSIS-123"},
		},
	}

	payload, err := json.Marshal(artifact)
	if err != nil {
		t.Fatalf("marshal organizer artifact: %v", err)
	}
	artifactPath := filepath.Join(outputDir, crawlartifact.OrganizerCodesFile)
	if err := writeBridgeTestFile(artifactPath, payload); err != nil {
		t.Fatalf("write organizer artifact: %v", err)
	}

	api := &API{
		organizer: organizerFacade{
			organizerService: organizer.NewService(),
		},
	}

	raw, err := api.loadOrganizerCrawlFilmCodesResult(map[string]any{
		"artifactInput": artifactPath,
	})
	if err != nil {
		t.Fatalf("loadOrganizerCrawlFilmCodesResult returned error: %v", err)
	}

	var result organizer.LoadCrawlFilmCodesResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal organizer result: %v", err)
	}
	if result.SourceType != "organizerCodes" {
		t.Fatalf("expected organizerCodes sourceType, got %q", result.SourceType)
	}
	if result.OutputDir != outputDir {
		t.Fatalf("expected outputDir %q, got %q", outputDir, result.OutputDir)
	}
	if result.ActressName != "Yuki Rino" {
		t.Fatalf("expected actressName Yuki Rino, got %q", result.ActressName)
	}
	if result.OrganizerCodesPath != artifactPath {
		t.Fatalf("expected organizer artifact path %q, got %q", artifactPath, result.OrganizerCodesPath)
	}
}

func TestLoadOrganizerCrawlFilmCodesResultAcceptsFilmDataPathAsArtifactInput(t *testing.T) {
	outputDir := t.TempDir()
	records := []map[string]any{
		{
			"title":      "ABP-889 sample",
			"sourceLink": "https://www.javbus.com/ABP-889",
			"actress":    []string{"Yuki Rino"},
			"magnetLinks": []map[string]any{
				{"link": "magnet:?xt=urn:btih:AAA", "size": "2.1GB"},
			},
		},
		{
			"title":      "SSIS-123 sample",
			"sourceLink": "https://www.javbus.com/SSIS-123",
			"actress":    []string{"Yuki Rino"},
		},
	}
	payload, err := json.Marshal(records)
	if err != nil {
		t.Fatalf("marshal filmData records: %v", err)
	}
	filmDataPath := filepath.Join(outputDir, crawlartifact.CrawlFilmDataFile)
	if err := writeBridgeTestFile(filmDataPath, payload); err != nil {
		t.Fatalf("write filmData artifact: %v", err)
	}

	api := &API{
		organizer: organizerFacade{
			organizerService: organizer.NewService(),
		},
	}

	raw, err := api.loadOrganizerCrawlFilmCodesResult(map[string]any{
		"artifactInput": filmDataPath,
	})
	if err != nil {
		t.Fatalf("loadOrganizerCrawlFilmCodesResult returned error: %v", err)
	}

	var result organizer.LoadCrawlFilmCodesResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal organizer result: %v", err)
	}
	if result.SourceType != "filmData" {
		t.Fatalf("expected filmData sourceType, got %q", result.SourceType)
	}
	if result.OutputDir != outputDir {
		t.Fatalf("expected outputDir %q, got %q", outputDir, result.OutputDir)
	}
	if result.FilmDataPath != filmDataPath {
		t.Fatalf("expected filmDataPath %q, got %q", filmDataPath, result.FilmDataPath)
	}
	if result.CodeCount != 2 || len(result.Codes) != 2 {
		t.Fatalf("expected two codes from filmData artifact, got %#v", result)
	}
}

func TestLoadOrganizerCrawlFilmCodesResultPrefersArtifactInputOverLegacyOutputDir(t *testing.T) {
	tempDir := t.TempDir()
	activeOutputDir := filepath.Join(tempDir, "active-run")
	legacyOutputDir := filepath.Join(tempDir, "legacy-run")

	activeArtifact := crawlartifact.OrganizerCodesArtifact{
		RunID:           "crawl-test-run",
		CompletedAt:     "2026-05-06T11:40:00Z",
		ActressName:     "Primary Target",
		OutputDir:       activeOutputDir,
		FilmDataPath:    filepath.Join(activeOutputDir, crawlartifact.CrawlFilmDataFile),
		TotalRecords:    243,
		UniqueCodeCount: 1,
		Codes:           []string{"ABP-889"},
		CodeEntries: []crawlartifact.CodeEntry{
			{Code: "ABP-889"},
		},
	}
	legacyArtifact := crawlartifact.OrganizerCodesArtifact{
		RunID:           "crawl-test-run-legacy",
		CompletedAt:     "2026-05-06T11:41:00Z",
		ActressName:     "Legacy Target",
		OutputDir:       legacyOutputDir,
		FilmDataPath:    filepath.Join(legacyOutputDir, crawlartifact.CrawlFilmDataFile),
		TotalRecords:    98,
		UniqueCodeCount: 1,
		Codes:           []string{"SSIS-123"},
		CodeEntries: []crawlartifact.CodeEntry{
			{Code: "SSIS-123"},
		},
	}

	activePayload, err := json.Marshal(activeArtifact)
	if err != nil {
		t.Fatalf("marshal active organizer artifact: %v", err)
	}
	legacyPayload, err := json.Marshal(legacyArtifact)
	if err != nil {
		t.Fatalf("marshal legacy organizer artifact: %v", err)
	}

	activeArtifactPath := filepath.Join(activeOutputDir, crawlartifact.OrganizerCodesFile)
	if err := writeBridgeTestFile(activeArtifactPath, activePayload); err != nil {
		t.Fatalf("write active organizer artifact: %v", err)
	}
	if err := writeBridgeTestFile(filepath.Join(legacyOutputDir, crawlartifact.OrganizerCodesFile), legacyPayload); err != nil {
		t.Fatalf("write legacy organizer artifact: %v", err)
	}

	api := &API{
		organizer: organizerFacade{
			organizerService: organizer.NewService(),
		},
	}

	raw, err := api.loadOrganizerCrawlFilmCodesResult(map[string]any{
		"artifactInput": activeArtifactPath,
		"outputDir":     legacyOutputDir,
	})
	if err != nil {
		t.Fatalf("loadOrganizerCrawlFilmCodesResult returned error: %v", err)
	}

	var result organizer.LoadCrawlFilmCodesResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal organizer result: %v", err)
	}
	if result.OutputDir != activeOutputDir {
		t.Fatalf("expected artifactInput outputDir %q, got %q", activeOutputDir, result.OutputDir)
	}
	if result.ActressName != "Primary Target" {
		t.Fatalf("expected artifactInput actressName Primary Target, got %q", result.ActressName)
	}
	if result.CodeCount != 1 || len(result.Codes) != 1 || result.Codes[0] != "ABP-889" {
		t.Fatalf("expected artifactInput code snapshot, got %#v", result)
	}
}

func TestBuildOrganizerRunOptionsCollapsesLegacyExpectedPayloadIntoPreloadedSnapshot(t *testing.T) {
	api := &API{}

	options := api.buildOrganizerRunOptions(map[string]any{
		"crawlOutputDir": "C:\\crawl-output",
		"expectedCodes":  []any{"abp889", "ABP-889"},
		"expectedCodeEntries": []any{
			map[string]any{
				"code": "ABP-889",
				"magnets": []any{
					map[string]any{"link": "magnet:?xt=urn:btih:AAA", "size": "2.1GB"},
				},
			},
		},
	})

	if len(options.ExpectedCodes) != 0 {
		t.Fatalf("expected legacy expectedCodes to be collapsed before organizer service call, got %#v", options.ExpectedCodes)
	}
	if len(options.ExpectedCodeEntries) != 0 {
		t.Fatalf("expected legacy expectedCodeEntries to be collapsed before organizer service call, got %#v", options.ExpectedCodeEntries)
	}
	if options.PreloadedExpected.SourceType != "payload" {
		t.Fatalf("expected payload sourceType, got %q", options.PreloadedExpected.SourceType)
	}
	if options.PreloadedExpected.OutputDir != "C:\\crawl-output" {
		t.Fatalf("expected outputDir C:\\crawl-output, got %q", options.PreloadedExpected.OutputDir)
	}
	if options.PreloadedExpected.CodeCount != 1 || len(options.PreloadedExpected.Codes) != 1 || options.PreloadedExpected.Codes[0] != "ABP-889" {
		t.Fatalf("unexpected preloaded expected codes: %#v", options.PreloadedExpected)
	}
	if len(options.PreloadedExpected.CodeEntries) != 1 || options.PreloadedExpected.CodeEntries[0].Code != "ABP-889" {
		t.Fatalf("unexpected preloaded expected code entries: %#v", options.PreloadedExpected.CodeEntries)
	}
}

func TestBuildOrganizerRunOptionsKeepsExplicitPreloadedMetadataAndUsesLegacyOnlyAsSupplement(t *testing.T) {
	api := &API{}

	options := api.buildOrganizerRunOptions(map[string]any{
		"crawlOutputDir": "C:\\crawl-output",
		"expectedCodes":  []any{"SSIS-123"},
		"preloadedExpected": map[string]any{
			"sourceType":         "filmData",
			"sourcePath":         "C:\\crawl-output\\filmData.json",
			"filmDataPath":       "C:\\crawl-output\\filmData.json",
			"organizerCodesPath": "C:\\crawl-output\\organizer-codes.json",
			"codes":              []any{"ABP-889"},
		},
	})

	if len(options.ExpectedCodes) != 0 || len(options.ExpectedCodeEntries) != 0 {
		t.Fatalf("expected only preloadedExpected to be forwarded on current Wails path, got %#v / %#v", options.ExpectedCodes, options.ExpectedCodeEntries)
	}
	if options.PreloadedExpected.SourceType != "filmData" {
		t.Fatalf("expected explicit sourceType filmData, got %q", options.PreloadedExpected.SourceType)
	}
	if options.PreloadedExpected.SourcePath != "C:\\crawl-output\\filmData.json" {
		t.Fatalf("expected explicit filmData sourcePath to stay primary, got %q", options.PreloadedExpected.SourcePath)
	}
	if options.PreloadedExpected.OutputDir != "C:\\crawl-output" {
		t.Fatalf("expected outputDir C:\\crawl-output, got %q", options.PreloadedExpected.OutputDir)
	}
	if options.PreloadedExpected.CodeCount != 2 || len(options.PreloadedExpected.Codes) != 2 {
		t.Fatalf("expected merged code snapshot, got %#v", options.PreloadedExpected)
	}
	if options.PreloadedExpected.Codes[0] != "ABP-889" || options.PreloadedExpected.Codes[1] != "SSIS-123" {
		t.Fatalf("expected sorted merged codes, got %#v", options.PreloadedExpected.Codes)
	}
}

func TestScanSubscriptionsV2FromOutputResultAcceptsArtifactPath(t *testing.T) {
	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "Yuki Rino")
	profile := crawlartifact.CrawlProfileArtifact{
		RunID:          "crawl-test-run",
		CompletedAt:    "2026-05-06T11:25:00Z",
		ActressName:    "Yuki Rino",
		CrawlURL:       "https://www.javbus.com/star/okq?page=1",
		TargetCount:    246,
		CompletedCount: 243,
		OutputDir:      outputDir,
		FilmDataPath:   filepath.Join(outputDir, crawlartifact.CrawlFilmDataFile),
		SiteBase:       "https://www.javbus.com",
	}

	payload, err := json.Marshal(profile)
	if err != nil {
		t.Fatalf("marshal crawl profile: %v", err)
	}
	artifactPath := filepath.Join(outputDir, crawlartifact.CrawlProfileFile)
	if err := writeBridgeTestFile(artifactPath, payload); err != nil {
		t.Fatalf("write crawl profile: %v", err)
	}
	if err := writeBridgeTestFile(filepath.Join(outputDir, crawlartifact.CrawlFilmDataFile), []byte(`[{"title":"AAA-001","actress":["Yuki Rino"]}]`)); err != nil {
		t.Fatalf("write filmData artifact: %v", err)
	}

	api := &API{
		lookup: lookupFacade{
			avSubscriptionsV2: avsubscriptionv2.NewService(runtimepaths.Paths{
				UserData:  filepath.Join(tempDir, "user-data"),
				Documents: tempDir,
			}, nil),
		},
	}

	raw, err := api.scanSubscriptionsV2FromOutputResult(map[string]any{
		"artifactInput": artifactPath,
	})
	if err != nil {
		t.Fatalf("scanSubscriptionsV2FromOutputResult returned error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal subscription result: %v", err)
	}
	if result["sourceType"] != "crawl-import" {
		t.Fatalf("expected crawl-import sourceType, got %#v", result["sourceType"])
	}
	if result["outputDir"] != outputDir {
		t.Fatalf("expected outputDir %q, got %#v", outputDir, result["outputDir"])
	}
	if actresses, ok := result["scannedActressList"].([]any); !ok || len(actresses) != 1 || actresses[0] != "Yuki Rino" {
		t.Fatalf("unexpected scanned actress list: %#v", result["scannedActressList"])
	}
}

func TestScanSubscriptionsV2FromOutputResultAcceptsFilmDataPathAsArtifactInput(t *testing.T) {
	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "Yuki Rino")
	records := []map[string]any{
		{
			"title":      "AAA-001",
			"sourceLink": "https://www.javbus.com/AAA-001",
			"actress":    []string{"Yuki Rino"},
		},
		{
			"title":      "AAA-002",
			"sourceLink": "https://www.javbus.com/AAA-002",
			"actress":    []string{"Yuki Rino", "Guest Actress"},
		},
	}
	payload, err := json.Marshal(records)
	if err != nil {
		t.Fatalf("marshal filmData records: %v", err)
	}
	filmDataPath := filepath.Join(outputDir, crawlartifact.CrawlFilmDataFile)
	if err := writeBridgeTestFile(filmDataPath, payload); err != nil {
		t.Fatalf("write filmData artifact: %v", err)
	}

	api := &API{
		lookup: lookupFacade{
			avSubscriptionsV2: avsubscriptionv2.NewService(runtimepaths.Paths{
				UserData:  filepath.Join(tempDir, "user-data"),
				Documents: tempDir,
			}, nil),
		},
	}

	raw, err := api.scanSubscriptionsV2FromOutputResult(map[string]any{
		"artifactInput": filmDataPath,
	})
	if err != nil {
		t.Fatalf("scanSubscriptionsV2FromOutputResult returned error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal subscription result: %v", err)
	}
	if result["sourceType"] != "crawl-import" {
		t.Fatalf("expected crawl-import sourceType, got %#v", result["sourceType"])
	}
	if result["outputDir"] != outputDir {
		t.Fatalf("expected outputDir %q, got %#v", outputDir, result["outputDir"])
	}
	if result["filmDataPath"] != filmDataPath {
		t.Fatalf("expected filmDataPath %q, got %#v", filmDataPath, result["filmDataPath"])
	}
	if actresses, ok := result["scannedActressList"].([]any); !ok || len(actresses) != 1 || actresses[0] != "Yuki Rino" {
		t.Fatalf("unexpected scanned actress list: %#v", result["scannedActressList"])
	}
}

func TestScanSubscriptionsV2FromOutputResultPrefersArtifactInputOverLegacyOutputDir(t *testing.T) {
	tempDir := t.TempDir()
	activeOutputDir := filepath.Join(tempDir, "active-run")
	legacyOutputDir := filepath.Join(tempDir, "legacy-run")

	activeProfile := crawlartifact.CrawlProfileArtifact{
		RunID:          "crawl-test-run",
		CompletedAt:    "2026-05-06T11:30:00Z",
		ActressName:    "Primary Target",
		CrawlURL:       "https://www.javbus.com/star/okq?page=1",
		TargetCount:    246,
		CompletedCount: 243,
		OutputDir:      activeOutputDir,
		FilmDataPath:   filepath.Join(activeOutputDir, crawlartifact.CrawlFilmDataFile),
		SiteBase:       "https://www.javbus.com",
	}
	legacyProfile := crawlartifact.CrawlProfileArtifact{
		RunID:          "crawl-test-run-legacy",
		CompletedAt:    "2026-05-06T11:31:00Z",
		ActressName:    "Legacy Target",
		CrawlURL:       "https://www.javbus.com/star/legacy?page=1",
		TargetCount:    99,
		CompletedCount: 98,
		OutputDir:      legacyOutputDir,
		FilmDataPath:   filepath.Join(legacyOutputDir, crawlartifact.CrawlFilmDataFile),
		SiteBase:       "https://www.javbus.com",
	}

	activePayload, err := json.Marshal(activeProfile)
	if err != nil {
		t.Fatalf("marshal active crawl profile: %v", err)
	}
	legacyPayload, err := json.Marshal(legacyProfile)
	if err != nil {
		t.Fatalf("marshal legacy crawl profile: %v", err)
	}

	activeArtifactPath := filepath.Join(activeOutputDir, crawlartifact.CrawlProfileFile)
	if err := writeBridgeTestFile(activeArtifactPath, activePayload); err != nil {
		t.Fatalf("write active crawl profile: %v", err)
	}
	if err := writeBridgeTestFile(filepath.Join(legacyOutputDir, crawlartifact.CrawlProfileFile), legacyPayload); err != nil {
		t.Fatalf("write legacy crawl profile: %v", err)
	}
	if err := writeBridgeTestFile(filepath.Join(activeOutputDir, crawlartifact.CrawlFilmDataFile), []byte(`[{"title":"AAA-001","actress":["Primary Target"]}]`)); err != nil {
		t.Fatalf("write active filmData artifact: %v", err)
	}
	if err := writeBridgeTestFile(filepath.Join(legacyOutputDir, crawlartifact.CrawlFilmDataFile), []byte(`[{"title":"BBB-001","actress":["Legacy Target"]}]`)); err != nil {
		t.Fatalf("write legacy filmData artifact: %v", err)
	}

	api := &API{
		lookup: lookupFacade{
			avSubscriptionsV2: avsubscriptionv2.NewService(runtimepaths.Paths{
				UserData:  filepath.Join(tempDir, "user-data"),
				Documents: tempDir,
			}, nil),
		},
	}

	raw, err := api.scanSubscriptionsV2FromOutputResult(map[string]any{
		"artifactInput": activeArtifactPath,
		"outputDir":     legacyOutputDir,
	})
	if err != nil {
		t.Fatalf("scanSubscriptionsV2FromOutputResult returned error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal subscription result: %v", err)
	}
	if result["outputDir"] != activeOutputDir {
		t.Fatalf("expected artifactInput outputDir %q, got %#v", activeOutputDir, result["outputDir"])
	}
	if actresses, ok := result["scannedActressList"].([]any); !ok || len(actresses) != 1 || actresses[0] != "Primary Target" {
		t.Fatalf("expected artifactInput actress selection, got %#v", result["scannedActressList"])
	}
}

func writeBridgeTestFile(filePath string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filePath, contents, 0o644)
}
