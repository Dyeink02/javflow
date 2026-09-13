package actressranking

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestLiveBackfillAnnual2021to2023 fills the official ranking cache with the
// 2021-2023 annual actress rankings through the AVfan lane (the official
// rental pages are unreachable from non-JP exits). Opt-in: it contacts
// AVfan through the user's proxy and needs a local Chrome.
func TestLiveBackfillAnnual2021to2023(t *testing.T) {
	if os.Getenv("JAVFLOW_BACKFILL_LIVE") != "1" {
		t.Skip("set JAVFLOW_BACKFILL_LIVE=1 to run the AVfan annual backfill")
	}
	proxy := os.Getenv("JAVFLOW_LIVE_PROXY")
	if proxy == "" {
		proxy = "http://127.0.0.1:7897"
	}
	cachePath := os.Getenv("JAVFLOW_RANKING_CACHE_PATH")
	if cachePath == "" {
		t.Fatal("set JAVFLOW_RANKING_CACHE_PATH to the app cache file")
	}
	outputDir := os.Getenv("JAVFLOW_BACKFILL_OUTPUT")
	if outputDir == "" {
		t.Fatal("set JAVFLOW_BACKFILL_OUTPUT to a directory for JSON dumps")
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}

	service := NewService()
	cache := loadCache(cachePath)
	for _, year := range []int{2023, 2022, 2021} {
		data, _, err := service.fetchAVFanAnnualRanking(year, proxy)
		if err != nil {
			t.Errorf("%d 年 AVfan 年榜抓取失败：%v", year, err)
			continue
		}
		t.Logf("%d 年：%d 位（complete=%v，expected=%d，title=%s）", year, len(data.Items), data.Complete, data.ExpectedTotal, data.PeriodLabel)
		payload, marshalErr := json.MarshalIndent(data, "", "  ")
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		dumpPath := filepath.Join(outputDir, "avfan-annual-"+strconv.Itoa(year)+".json")
		if writeErr := os.WriteFile(dumpPath, payload, 0o644); writeErr != nil {
			t.Fatal(writeErr)
		}
		cache = service.commitRankingCache(cache, cachePath, "avfan", data)
	}
}
