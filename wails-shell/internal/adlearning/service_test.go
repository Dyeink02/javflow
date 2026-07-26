package adlearning

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	runtimepaths "javflow/internal/runtime"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	root := t.TempDir()
	return &Service{
		paths: runtimepaths.Paths{
			UserData: root,
		},
		now: func() time.Time {
			return time.Date(2026, 5, 1, 17, 0, 0, 0, time.Local)
		},
	}
}

func TestGetSummaryReturnsDefaultModelWhenFileMissing(t *testing.T) {
	service := newTestService(t)

	summary := service.GetSummary()

	if summary.Version != modelVersion {
		t.Fatalf("expected version %d, got %d", modelVersion, summary.Version)
	}
	if summary.ActiveModel != defaultAdModelType {
		t.Fatalf("expected active model %q, got %q", defaultAdModelType, summary.ActiveModel)
	}
	if summary.KeywordCount != 0 || summary.AdSampleCount != 0 || summary.IntroTemplateCount != 0 {
		t.Fatalf("unexpected default summary: %#v", summary)
	}
}

func TestUpdateModelMergesKeywordsAndPersistsSummary(t *testing.T) {
	service := newTestService(t)

	adScore := 72
	high := 12
	summary, err := service.UpdateModel(UpdateModelOptions{
		Keywords:               []string{"Promo", "promo", "Teaser"},
		AdScore:                &adScore,
		HighSimilarityDistance: &high,
		ModelType:              "yolov8n-balanced",
	})
	if err != nil {
		t.Fatalf("update model: %v", err)
	}

	if summary.KeywordCount != 2 {
		t.Fatalf("expected 2 unique keywords, got %d", summary.KeywordCount)
	}
	if summary.Thresholds.AdScore != 72 {
		t.Fatalf("expected ad score 72, got %d", summary.Thresholds.AdScore)
	}
	if summary.Thresholds.HighSimilarityDistance != 12 {
		t.Fatalf("expected high similarity distance 12, got %d", summary.Thresholds.HighSimilarityDistance)
	}
	if summary.ActiveModel != "yolov8n-balanced" {
		t.Fatalf("expected active model yolov8n-balanced, got %q", summary.ActiveModel)
	}

	modelPath := filepath.Join(service.paths.UserData, "ad-learning-model.json")
	if _, err := os.Stat(modelPath); err != nil {
		t.Fatalf("expected model file to exist: %v", err)
	}
}

func TestGetSummaryBuildsIntroTemplatesFromLegacyAdSamples(t *testing.T) {
	service := newTestService(t)
	modelPath := filepath.Join(service.paths.UserData, "ad-learning-model.json")
	if err := os.MkdirAll(filepath.Dir(modelPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	payload := `{
  "version": 1,
  "keywords": ["ad"],
  "thresholds": {
    "adScore": 60,
    "highSimilarityDistance": 10,
    "mediumSimilarityDistance": 16,
    "lowSimilarityDistance": 22
  },
  "adSamples": [
    {"id":"sample-1","hashBits":"1010","sourcePath":"C:\\\\a.jpg"},
    {"id":"sample-2","hashBits":"1010","sourcePath":"C:\\\\b.jpg"},
    {"id":"sample-3","hashBits":"1111","sourcePath":"C:\\\\c.jpg"}
  ],
  "normalSamples": [],
  "introTemplates": [],
  "meta": {"activeModel":"mobile-net-v3-lite"},
  "metrics": {"totalLearningRuns": 0}
}`
	if err := os.WriteFile(modelPath, []byte(payload), 0o644); err != nil {
		t.Fatalf("write model: %v", err)
	}

	summary := service.GetSummary()
	if summary.IntroTemplateCount != 2 {
		t.Fatalf("expected intro template count 2, got %d", summary.IntroTemplateCount)
	}
}

func TestEvaluateRiskWithHashesReturnsSafeWhenCoarseStageMisses(t *testing.T) {
	service := newTestService(t)
	model := defaultModel()

	result := service.evaluateRiskWithHashes(
		`C:\videos\ABP-001.mp4`,
		model,
		"mobile-net-v3-lite",
		60,
		true,
		[]string{strings.Repeat("0", 64)},
		false,
	)

	if result.IsAd {
		t.Fatalf("expected non-ad result, got ad: %#v", result)
	}
	if result.Score != 0 {
		t.Fatalf("expected score 0, got %v", result.Score)
	}
	coarseStage, ok := result.Evidence["coarseStage"].(map[string]any)
	if !ok {
		t.Fatalf("expected coarseStage evidence, got %#v", result.Evidence["coarseStage"])
	}
	if passed, _ := coarseStage["passed"].(bool); passed {
		t.Fatalf("expected coarse stage miss, got %#v", coarseStage)
	}
}

func TestEvaluateRiskWithHashesFlagsExactAdTemplateMatch(t *testing.T) {
	service := newTestService(t)
	model := defaultModel()
	hashBits := strings.Repeat("1", 64)
	frameSecond := 3
	model.AdSamples = []SampleRecord{
		{
			ID:          "ad-1",
			HashBits:    hashBits,
			SourcePath:  `C:\samples\ad-1.mp4`,
			FilmCode:    "ABF-055",
			FrameSecond: &frameSecond,
		},
	}
	model.IntroTemplates = []SampleRecord{
		{
			ID:          "tpl-1",
			HashBits:    hashBits,
			SourcePath:  `C:\samples\tpl-1.mp4`,
			FrameSecond: &frameSecond,
		},
	}

	result := service.evaluateRiskWithHashes(
		`C:\videos\ABF-055.mp4`,
		model,
		"mobile-net-v3-lite",
		60,
		true,
		[]string{hashBits},
		true,
	)

	if !result.IsAd {
		t.Fatalf("expected ad result, got non-ad: %#v", result)
	}
	if result.Score != 100 {
		t.Fatalf("expected clamped score 100, got %v", result.Score)
	}
	if result.Evidence["bestAdSampleMatch"] == nil {
		t.Fatalf("expected bestAdSampleMatch evidence, got %#v", result.Evidence)
	}
	if result.Evidence["bestTemplateMatch"] == nil {
		t.Fatalf("expected bestTemplateMatch evidence, got %#v", result.Evidence)
	}
}

func TestNormalizeCodeListAcceptsCommaAndLineSeparatedCodes(t *testing.T) {
	codes := normalizeCodeList([]string{"abf055, ABF-179\nabw_006，ppt116"})

	expected := []string{"ABF-055", "ABF-179", "ABW-006", "PPT-116"}
	if strings.Join(codes, "|") != strings.Join(expected, "|") {
		t.Fatalf("unexpected codes: %#v", codes)
	}
}

func TestDetectFilmCodeFromPathUsesLongestTokenFirst(t *testing.T) {
	pairs := buildTokenPairs([]string{"ABF-055", "ABF-055A"})
	code := detectFilmCodeFromPath(`C:\videos\[site]ABF055A.mp4`, pairs)

	if code != "ABF-055A" {
		t.Fatalf("expected ABF-055A, got %q", code)
	}
}

func TestCollectVideoFilesWithManagedFallbackScansWaitingDirectory(t *testing.T) {
	root := t.TempDir()
	waitingDir := filepath.Join(root, "待整理")
	if err := os.MkdirAll(waitingDir, 0o755); err != nil {
		t.Fatalf("mkdir waiting dir: %v", err)
	}
	videoPath := filepath.Join(waitingDir, "ABF-055.mp4")
	if err := os.WriteFile(videoPath, []byte("test"), 0o644); err != nil {
		t.Fatalf("write video: %v", err)
	}

	result := collectVideoFilesWithManagedFallback(root, true, nil)

	if !result.UsedManagedFallback {
		t.Fatalf("expected managed fallback, got %#v", result)
	}
	if len(result.VideoFiles) != 1 || result.VideoFiles[0] != videoPath {
		t.Fatalf("unexpected video files: %#v", result.VideoFiles)
	}
}
