package actressranking

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGetActressRankingsReturnsStaleOfficialCacheWithoutOnlineRefresh(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "cache.json")
	cache := cacheFile{Version: cacheVersion, Sources: map[string]sourceCache{"official": buildSourceCache()}}
	cache.Sources["official"] = sourceCache{
		MonthlyLatestKey: "2026-07",
		MonthlyByPeriod: map[string]cacheEntry{"2026-07": {
			CachedAt: time.Now().Add(-48 * time.Hour).Format(time.RFC3339),
			Data:     Result{Mode: "monthly", SourceName: "FANZA 官方", PeriodYear: 2026, PeriodMonth: 7, Total: 1, Items: []RankingItem{{Rank: 1, ActressName: "三上悠亚"}}},
		}},
		AnnualByYear: map[string]cacheEntry{},
	}
	payload, err := json.Marshal(cache)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	result, err := NewService().GetActressRankings(Options{Source: "fanza", Mode: "monthly", CacheFilePath: cachePath})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("stale cache should not wait for online fetch: %s", elapsed)
	}
	if !result.FromCache || !result.Stale || len(result.Items) != 1 || result.Complete {
		t.Fatalf("official cache must remain its own source instead of being upgraded from AVfan: %#v", result)
	}
}

func TestGetActressRankingsUsesBundledJulySnapshotOnFirstRun(t *testing.T) {
	started := time.Now()
	result, err := NewService().GetActressRankings(Options{Source: "fanza", Mode: "monthly"})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("bundled snapshot should not wait for online fetch: %s", elapsed)
	}
	if !result.FromCache || result.PeriodYear != 2026 || result.PeriodMonth != 7 || len(result.Items) != 20 {
		t.Fatalf("unexpected bundled snapshot: period=%d-%d cache=%t items=%d", result.PeriodYear, result.PeriodMonth, result.FromCache, len(result.Items))
	}
	if result.SourceName != "FANZA 官方" || result.Total != len(result.Items) || result.Items[0].Rank != 1 || result.Items[0].ActressName != "瀬戸環奈" || result.Items[len(result.Items)-1].Rank != 20 {
		t.Fatalf("bundled official snapshot must retain its exact official rows: %#v", result)
	}
	if result.FetchedAt != "2026-07-31T01:07:19+08:00" {
		t.Fatalf("snapshot must expose its source capture time, got %q", result.FetchedAt)
	}
	if result.ExpectedTotal != 100 || result.Complete {
		t.Fatalf("partial bundled official snapshot must not be marked complete, got expected=%d complete=%t", result.ExpectedTotal, result.Complete)
	}
}

func TestAVFanVerificationPageIsNotTreatedAsAnEmptyRanking(t *testing.T) {
	if !isAVFanVerificationPage("<title>Just a moment...</title><div id='cf-chl-widget'></div>", "Just a moment...") {
		t.Fatal("expected Cloudflare challenge markup to be detected")
	}
	if isAVFanVerificationPage("<title>2026.07 AVfan Ranking</title><ul class='ranking-list'></ul>", "2026.07 AVfan Ranking") {
		t.Fatal("ordinary ranking markup must not be treated as a verification page")
	}
}

func TestLoadCachePersistsBundledSnapshotForFirstRun(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "actress-ranking-cache.json")
	cache := loadCache(cachePath)
	if len(listMonthlyPeriods(cache, []string{"official"})) == 0 {
		t.Fatal("first-run cache should include the bundled monthly snapshot")
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("first-run cache should be persisted: %v", err)
	}
}

func TestLoadCacheRecoversFromDamagedJSON(t *testing.T) {
	directory := t.TempDir()
	cachePath := filepath.Join(directory, "actress-ranking-cache.json")
	if err := os.WriteFile(cachePath, []byte(`{not-json`), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := loadCache(cachePath)
	if len(listMonthlyPeriods(cache, []string{"official"})) == 0 {
		t.Fatal("recovered cache must keep the bundled first-run snapshot")
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("recovered cache missing: %v", err)
	}
	backups, err := filepath.Glob(cachePath + ".corrupt-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected one corrupt-cache backup, got %v", backups)
	}
}

func TestLoadCacheRemovesOnlyMislabelledOfficialAVFanSnapshots(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "cache.json")
	cache := getCacheSkeleton()
	cache.Version = cacheVersion - 1
	cache.Sources["official"] = sourceCache{
		MonthlyLatestKey: "2026-08",
		MonthlyByPeriod: map[string]cacheEntry{
			"2026-08": {Data: Result{Mode: "monthly", SourceName: "FANZA 官方（本地 Top100 快照）", PeriodYear: 2026, PeriodMonth: 8, Items: []RankingItem{{Rank: 1, ActressName: "错误复制"}}}},
			"2025-12": {Data: Result{Mode: "monthly", SourceName: "FANZA 官方", PeriodYear: 2025, PeriodMonth: 12, Items: []RankingItem{{Rank: 1, ActressName: "真实官方"}}}},
		},
		AnnualByYear: map[string]cacheEntry{},
	}
	cache.Sources["avfan"] = sourceCache{
		MonthlyLatestKey: "2026-08",
		MonthlyByPeriod: map[string]cacheEntry{
			"2026-08": {Data: Result{Mode: "monthly", SourceName: "AVfan 在线", PeriodYear: 2026, PeriodMonth: 8, Items: []RankingItem{{Rank: 1, ActressName: "保留的 AVfan"}}}},
		},
		AnnualByYear: map[string]cacheEntry{},
	}
	payload, err := json.Marshal(cache)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	loaded := loadCache(cachePath)
	if _, exists := loaded.Sources["official"].MonthlyByPeriod["2026-08"]; exists {
		t.Fatalf("mislabelled AVfan snapshot must be removed from official cache: %#v", loaded.Sources["official"])
	}
	if got := loaded.Sources["official"].MonthlyByPeriod["2025-12"].Data.Items[0].ActressName; got != "真实官方" {
		t.Fatalf("real official entry must be preserved, got %q", got)
	}
	if got := loaded.Sources["avfan"].MonthlyByPeriod["2026-08"].Data.Items[0].ActressName; got != "保留的 AVfan" {
		t.Fatalf("AVfan source record must remain available, got %q", got)
	}
}

func TestCommitRankingCacheKeepsConcurrentPeriods(t *testing.T) {
	service := NewService()
	cachePath := filepath.Join(t.TempDir(), "cache.json")
	initial := loadCache(cachePath)
	monthly := Result{Mode: "monthly", PeriodYear: 2026, PeriodMonth: 8, Items: []RankingItem{{Rank: 1, ActressName: "actor-month"}}}
	annual := Result{Mode: "annual", PeriodYear: 2025, Items: []RankingItem{{Rank: 1, ActressName: "actor-year"}}}
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		service.commitRankingCache(initial, cachePath, "official", monthly)
	}()
	go func() {
		defer workers.Done()
		service.commitRankingCache(initial, cachePath, "official", annual)
	}()
	workers.Wait()
	final := loadCache(cachePath)
	if _, ok := final.Sources["official"].MonthlyByPeriod["2026-08"]; !ok {
		t.Fatal("concurrent monthly cache entry was lost")
	}
	if _, ok := final.Sources["official"].AnnualByYear["2025"]; !ok {
		t.Fatal("concurrent annual cache entry was lost")
	}
}

func TestBundledHistoryContainsOnlyVerifiedMonthlyPeriods(t *testing.T) {
	cache := getBundledInitialCache()
	periods := listMonthlyPeriods(cache, []string{"avfan"})
	seen := make(map[string]int, len(periods))
	for _, period := range periods {
		seen[period.Key] = len(period.Entry.Data.Items)
	}
	for _, key := range []string{"2026-03", "2026-04", "2026-05", "2026-06", "2026-07"} {
		if seen[key] != 100 {
			t.Fatalf("bundled AVfan period %s must contain 100 verified rows, got %d", key, seen[key])
		}
	}
	for _, key := range []string{"2026-01", "2026-02"} {
		if _, ok := seen[key]; ok {
			t.Fatalf("bundled history must not invent unavailable period %s", key)
		}
	}
}

func TestBundledFANZAVideoArchiveKeepsItsOwnSourceAndPeriods(t *testing.T) {
	cache := getBundledInitialCache()
	periods := listMonthlyPeriods(cache, []string{"localHistory"})
	seen := make(map[string]Result, len(periods))
	for _, period := range periods {
		seen[period.Key] = normalizeResultMetadata(period.Entry.Data)
	}

	for _, key := range []string{"2026-01", "2026-02"} {
		data, ok := seen[key]
		if !ok {
			t.Fatalf("missing bundled FANZA Video archive period %s", key)
		}
		if data.Total != expectedRankingTotal || !data.Complete {
			t.Fatalf("bundled FANZA Video archive %s must be a complete Top100: %#v", key, data)
		}
		if data.SourceName != "Proclivity-DB（FANZA 视频月榜归档）" || !strings.Contains(data.SourceURL, key) {
			t.Fatalf("bundled archive must preserve its source identity: %#v", data)
		}
	}

	if _, exists := cache.Sources["official"].MonthlyByPeriod["2026-01"]; exists {
		t.Fatal("FANZA Video archive must not be written into the official DVD bucket")
	}
	if _, exists := cache.Sources["avfan"].MonthlyByPeriod["2026-02"]; exists {
		t.Fatal("FANZA Video archive must not be written into the AVfan DVD bucket")
	}
}

func TestBundledFullHistoryCarriesCompleteTop100Metadata(t *testing.T) {
	result, err := NewService().GetActressRankings(Options{Source: "smart", Mode: "monthly", Year: 2026, Month: 3})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 100 || result.ExpectedTotal != 100 || !result.Complete {
		t.Fatalf("bundled Top 100 metadata mismatch: total=%d expected=%d complete=%t", result.Total, result.ExpectedTotal, result.Complete)
	}
}

func TestRankingCacheLogsItsHitWithoutNetwork(t *testing.T) {
	logs := make([]string, 0)
	_, err := NewService().GetActressRankings(Options{
		Source: "smart",
		Mode:   "monthly",
		Year:   2026,
		Month:  3,
		Log: func(level string, message string, details map[string]any) {
			logs = append(logs, level+":"+message)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) < 3 {
		t.Fatalf("expected cache lifecycle logs, got %v", logs)
	}
}

func TestSourceHealthEntersShortCooldownAfterRepeatedFailures(t *testing.T) {
	service := NewService()
	service.recordSourceResult("official", true)
	if _, cooling := service.sourceIsCoolingDown("official"); cooling {
		t.Fatal("first source failure should remain retryable")
	}
	service.recordSourceResult("official", true)
	if _, cooling := service.sourceIsCoolingDown("official"); !cooling {
		t.Fatal("repeated source failures should enter cooldown")
	}
	service.recordSourceResult("official", false)
	if _, cooling := service.sourceIsCoolingDown("official"); cooling {
		t.Fatal("successful source response should clear cooldown")
	}
}

func TestSmartHistoricalMonthUsesBundledSnapshotBeforeOnlineFetch(t *testing.T) {
	started := time.Now()
	result, err := NewService().GetActressRankings(Options{
		Source: "smart",
		Mode:   "monthly",
		Year:   2026,
		Month:  3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("historical bundled snapshot should not wait for online fetch: %s", elapsed)
	}
	if !result.FromCache || result.ResolvedSource != "avfan" || result.PeriodYear != 2026 || result.PeriodMonth != 3 || len(result.Items) != 100 {
		t.Fatalf("unexpected bundled historical result: %+v", result)
	}
}

func TestFANZAHistoricalMonthUsesLocalHistoryBeforeOnlineFetch(t *testing.T) {
	started := time.Now()
	result, err := NewService().GetActressRankings(Options{
		Source: "fanza",
		Mode:   "monthly",
		Year:   2026,
		Month:  3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("FANZA historical fallback must not wait for the current official page: %s", elapsed)
	}
	if !result.FromCache || !result.FallbackUsed || result.ResolvedSource != "local" || result.PeriodYear != 2026 || result.PeriodMonth != 3 || len(result.Items) != 100 {
		t.Fatalf("unexpected FANZA local-history fallback: %+v", result)
	}
	if !strings.Contains(result.SourceName, "本地历史") || !strings.Contains(result.Notice, "FANZA 官方暂未保存") {
		t.Fatalf("FANZA historical fallback must remain truthfully labeled: %+v", result)
	}
}

func TestListMonthlyPeriodsAcceptsLegacyCacheWithoutTitle(t *testing.T) {
	cache := getCacheSkeleton()
	cache.Sources["official"] = sourceCache{
		MonthlyLatestKey: "2026-07",
		MonthlyByPeriod: map[string]cacheEntry{
			"2026-07": {
				CachedAt: time.Now().Add(-48 * time.Hour).Format(time.RFC3339),
				Data: Result{
					Mode: "monthly", PeriodYear: 2026, PeriodMonth: 7,
					Items: []RankingItem{{Rank: 1, ActressName: "三上悠亚"}},
				},
			},
		},
		AnnualByYear: map[string]cacheEntry{},
	}

	periods := listMonthlyPeriods(cache, []string{"official"})
	if len(periods) != 1 {
		t.Fatalf("legacy cache should expose one period, got %d", len(periods))
	}
	if periods[0].Key != "2026-07" || periods[0].Entry.Data.Items[0].ActressName != "三上悠亚" {
		t.Fatalf("unexpected legacy cache period: %#v", periods[0])
	}
}

func TestBuildSourcePlan(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name             string
		requestedChannel string
		mode             string
		expected         []string
	}{
		{name: "local", requestedChannel: "local", mode: "monthly", expected: []string{"local"}},
		{name: "avfan", requestedChannel: "avfan", mode: "monthly", expected: []string{"avfan", "local"}},
		{name: "official monthly", requestedChannel: "fanza", mode: "monthly", expected: []string{"official", "local"}},
		{name: "official annual", requestedChannel: "dmm", mode: "annual", expected: []string{"official", "local"}},
		{name: "smart annual", requestedChannel: "smart", mode: "annual", expected: []string{"official", "avfan", "local"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			actual := buildSourcePlan(testCase.requestedChannel, testCase.mode)
			if len(actual) != len(testCase.expected) {
				t.Fatalf("buildSourcePlan() length = %d, expected %d", len(actual), len(testCase.expected))
			}
			for index := range actual {
				if actual[index] != testCase.expected[index] {
					t.Fatalf("buildSourcePlan()[%d] = %q, expected %q", index, actual[index], testCase.expected[index])
				}
			}
		})
	}
}

func TestParseAVFanRankingHTML(t *testing.T) {
	t.Parallel()

	htmlSource := `
<html>
  <head><title>2026.04 AVfan FANZA DVD Actress Monthly Ranking</title></head>
  <body>
    <div class="ranking-year-link"><a href="?year=2026">2026</a></div>
    <div class="ranking-year-link"><a href="?year=2025">2025</a></div>
    <ul class="ranking-list">
      <li>
        <div class="ranking-cnt"><b>1</b></div>
        <a href="/actress/%E4%B8%89%E4%B8%8A%E6%82%A0%E4%BA%9C.html"><img src="/images/a.jpg" alt="备用名"></a>
      </li>
      <li>
        <div class="ranking-cnt"><b>2</b></div>
        <a href="/actress/%E9%80%A2%E6%B2%A2%E3%81%BF%E3%82%86.html"><img src="/images/b.jpg" alt=""></a>
      </li>
    </ul>
  </body>
</html>`

	result, err := parseAVFanRankingHTML(htmlSource, "monthly", avfanMonthlyURL, 0)
	if err != nil {
		t.Fatalf("parseAVFanRankingHTML returned error: %v", err)
	}
	if result.PeriodYear != 2026 || result.PeriodMonth != 4 {
		t.Fatalf("unexpected period: %d-%d", result.PeriodYear, result.PeriodMonth)
	}
	if result.Total != 2 {
		t.Fatalf("result.Total = %d, expected 2", result.Total)
	}
	if result.Items[0].ActressName != "三上悠亜" {
		t.Fatalf("first actress = %q", result.Items[0].ActressName)
	}
	if len(result.AvailableYears) != 2 || result.AvailableYears[0] != 2026 {
		t.Fatalf("available years = %#v", result.AvailableYears)
	}
}

func TestParseAVFanAvailableYearsSupportsNestedNavigation(t *testing.T) {
	root, err := parseHTMLDocument(`
<div class="ranking-year-link">
  <ul><li><a href="?year=2025">2025</a></li><li><a href="?year=2024">2024</a></li></ul>
</div>`)
	if err != nil {
		t.Fatal(err)
	}
	years := parseAVFanAvailableYears(root)
	if len(years) != 2 || years[0] != 2025 || years[1] != 2024 {
		t.Fatalf("unexpected AVfan available years: %#v", years)
	}
}

func TestParseOfficialMonthlyRankingHTML(t *testing.T) {
	t.Parallel()

	htmlSource := `
<html>
  <head><title>DMM 月榜</title></head>
  <body>
    <div class="area-rank">
      <table>
        <tr class="bd-b">
          <td><span class="rank">1</span></td>
          <td class="data">
            <p><a href="/mono/dvd/-/list/=/article=actress/id=123/">三上悠亜</a></p>
            <a href="/mono/dvd/-/detail/=/cid=abc123/">最新作品</a>
            商品数：12
          </td>
          <td><img src="/image/a.jpg" alt="三上悠亜"></td>
        </tr>
      </table>
    </div>
  </body>
</html>`

	result, err := parseOfficialMonthlyRankingHTML(htmlSource, "fanza")
	if err != nil {
		t.Fatalf("parseOfficialMonthlyRankingHTML returned error: %v", err)
	}
	if result.SourceName != "FANZA 官方" {
		t.Fatalf("result.SourceName = %q", result.SourceName)
	}
	if result.Total != 1 {
		t.Fatalf("result.Total = %d", result.Total)
	}
	if result.Items[0].LatestTitle != "最新作品" {
		t.Fatalf("latest title = %q", result.Items[0].LatestTitle)
	}
	if result.Items[0].WorksCount == nil || *result.Items[0].WorksCount != 12 {
		t.Fatalf("works count = %#v", result.Items[0].WorksCount)
	}
}

func TestParseOfficialAnnualRentalRankingHTML(t *testing.T) {
	t.Parallel()

	htmlSource := `
<html>
  <head><title>AV女優ランキング ベスト100</title></head>
  <body>
    <div class="area-rank">
      <a href="/rental/-/ranking/=/article=actress/t=year_2025/">2025年</a>
      <a href="/rental/-/ranking/=/article=actress/t=year_2024/">2024年</a>
      <table><tr><td class="bd-b">
        <span class="rank">21</span>
        <a href="/rental/-/list/=/article=actress/id=123/"><img src="/images/a.jpg" alt="演员甲"></a>
        <div class="data"><p><a href="/rental/-/list/=/article=actress/id=123/">演员甲</a></p></div>
      </td></tr></table>
    </div>
  </body>
</html>`

	result, err := parseOfficialAnnualRentalRankingHTML(htmlSource, "fanza", "https://www.dmm.co.jp/rental/-/ranking/=/article=actress/t=year_2025/", 2025)
	if err != nil {
		t.Fatalf("parseOfficialAnnualRentalRankingHTML returned error: %v", err)
	}
	if result.Mode != "annual" || result.PeriodYear != 2025 || result.PeriodMonth != 0 {
		t.Fatalf("unexpected annual period: %#v", result)
	}
	if result.Total != 1 || result.Items[0].Rank != 21 || result.Items[0].ActressName != "演员甲" {
		t.Fatalf("unexpected annual items: %#v", result.Items)
	}
	if len(result.AvailableYears) != 2 || result.AvailableYears[0] != 2025 || result.AvailableYears[1] != 2024 {
		t.Fatalf("unexpected official annual years: %#v", result.AvailableYears)
	}
}

func TestGetActressRankingsFromLocalHistory(t *testing.T) {
	tempDir := t.TempDir()
	cachePath := filepath.Join(tempDir, "cache.json")
	historyDir := filepath.Join(tempDir, "history")
	if err := os.MkdirAll(historyDir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	historyPath := filepath.Join(historyDir, "2026-04-monthly.json")
	historyPayload := `{
  "mode": "monthly",
  "sourceName": "本地历史导入",
  "title": "2026年04月 本地历史月榜",
  "periodLabel": "2026年04月",
  "periodYear": 2026,
  "periodMonth": 4,
  "total": 1,
  "items": [
    { "rank": 1, "actressName": "三上悠亜", "profileUrl": "https://example.com/a" }
  ]
}`
	if err := os.WriteFile(historyPath, []byte(historyPayload), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	service := NewService()
	result, err := service.GetActressRankings(Options{
		Mode:               "monthly",
		Year:               2026,
		Month:              4,
		Source:             "local",
		CacheFilePath:      cachePath,
		HistoryDirectories: []string{historyDir},
	})
	if err != nil {
		t.Fatalf("GetActressRankings returned error: %v", err)
	}
	if result.ResolvedSource != "local" {
		t.Fatalf("result.ResolvedSource = %q", result.ResolvedSource)
	}
	if result.Total != 1 || len(result.Items) != 1 {
		t.Fatalf("unexpected local result size: total=%d items=%d", result.Total, len(result.Items))
	}
	if result.Items[0].ActressName != "三上悠亜" {
		t.Fatalf("first actress = %q", result.Items[0].ActressName)
	}
	if !result.FromCache || !result.Stale {
		t.Fatalf("expected local result to be cache-backed and stale")
	}
}

func TestSaveMonthlyHistoryPreservesFirstCompleteSnapshot(t *testing.T) {
	tempDir := t.TempDir()
	historyDir := filepath.Join(tempDir, "ranking-history")
	items := make([]RankingItem, 0, expectedRankingTotal)
	for index := 1; index <= expectedRankingTotal; index++ {
		items = append(items, RankingItem{Rank: index, ActressName: fmt.Sprintf("演员-%03d", index)})
	}
	service := NewService()
	first, alreadySaved, err := service.SaveMonthlyHistory(Result{
		Mode: "monthly", SourceName: "FANZA 官方", PeriodYear: 2026, PeriodMonth: 2,
		PeriodLabel: "2026年02月", Items: items,
	}, []string{historyDir})
	if err != nil || alreadySaved || first.SourceName != "用户保存月榜（原始来源：FANZA 官方）" {
		t.Fatalf("first monthly snapshot should be saved once: saved=%+v already=%t err=%v", first, alreadySaved, err)
	}

	changed := append([]RankingItem(nil), items...)
	changed[0].ActressName = "不应覆盖的演员"
	second, alreadySaved, err := service.SaveMonthlyHistory(Result{
		Mode: "monthly", SourceName: "FANZA 官方", PeriodYear: 2026, PeriodMonth: 2,
		PeriodLabel: "2026年02月", Items: changed,
	}, []string{historyDir})
	if err != nil || !alreadySaved || second.Items[0].ActressName != items[0].ActressName {
		t.Fatalf("existing monthly snapshot must be preserved: saved=%+v already=%t err=%v", second, alreadySaved, err)
	}

	restored, err := service.GetActressRankings(Options{
		Mode: "monthly", Source: "local", Year: 2026, Month: 2,
		CacheFilePath: filepath.Join(tempDir, "cache.json"), HistoryDirectories: []string{historyDir},
	})
	if err != nil || restored.Total != expectedRankingTotal || restored.Items[0].ActressName != items[0].ActressName {
		t.Fatalf("saved snapshot must be available as local history: result=%+v err=%v", restored, err)
	}
}

func TestSaveMonthlyHistoryRejectsIncompleteResult(t *testing.T) {
	_, _, err := NewService().SaveMonthlyHistory(Result{
		Mode: "monthly", PeriodYear: 2026, PeriodMonth: 1,
		Items: []RankingItem{{Rank: 1, ActressName: "不完整演员"}},
	}, []string{filepath.Join(t.TempDir(), "ranking-history")})
	if err == nil || !strings.Contains(err.Error(), "未保存不完整榜单") {
		t.Fatalf("incomplete monthly ranking must not be recorded, got %v", err)
	}
}

func TestAnnualQueryYearsOnlyExposeRealSourceOrCacheYears(t *testing.T) {
	cache := getCacheSkeleton()
	cache.Sources["avfan"] = sourceCache{
		MonthlyByPeriod: map[string]cacheEntry{},
		AnnualByYear: map[string]cacheEntry{
			"2025": {Data: Result{Title: "真实缓存年榜", Mode: "annual", PeriodYear: 2025, Total: 1, Items: []RankingItem{{Rank: 1, ActressName: "真实缓存演员"}}}},
		},
	}

	years := getAnnualQueryYears(cache, []string{"avfan"})
	if len(years) != 1 || years[0] != 2025 {
		t.Fatalf("query years must only expose cached/source years: %#v", years)
	}

	if len(cache.Sources["avfan"].AnnualByYear) != 1 {
		t.Fatalf("query year helper must not add cached ranking records: %#v", cache.Sources["avfan"].AnnualByYear)
	}
	if cache.Sources["avfan"].AnnualByYear["2025"].Data.Items[0].ActressName != "真实缓存演员" {
		t.Fatal("query year helper must not alter real cached ranking data")
	}
}

func TestAnnualAvailabilityExcludesTheCurrentCalendarYear(t *testing.T) {
	currentYear := time.Now().Year()
	publishedYear := currentYear - 1
	cache := getCacheSkeleton()
	cache.Sources["official"] = sourceCache{
		// Monthly availability must never create an annual selector option.
		AvailableYears: []int{currentYear},
		AnnualByYear: map[string]cacheEntry{
			strconv.Itoa(currentYear):   {Data: Result{Title: "未发布年榜", Mode: "annual", PeriodYear: currentYear, AvailableYears: []int{currentYear, publishedYear}, Items: []RankingItem{{Rank: 1, ActressName: "不应显示"}}}},
			strconv.Itoa(publishedYear): {Data: Result{Title: "已发布年榜", Mode: "annual", PeriodYear: publishedYear, AvailableYears: []int{currentYear, publishedYear}, Items: []RankingItem{{Rank: 1, ActressName: "应显示"}}}},
		},
	}

	years := getAnnualQueryYears(cache, []string{"official"})
	if len(years) != 1 || years[0] != publishedYear {
		t.Fatalf("annual picker must exclude current year, got %#v", years)
	}
	if cached := resolveCachedAnnualEntry(cache, []string{"official"}, currentYear, true); cached != nil {
		t.Fatalf("unpublished current annual cache must not resolve: %#v", cached)
	}
	_, err := NewService().GetActressRankings(Options{Source: "fanza", Mode: "annual", Year: currentYear})
	if err == nil || !strings.Contains(err.Error(), "尚未发布") {
		t.Fatalf("current annual request must be rejected before fetch, got %v", err)
	}
}

func TestOfficialRentalAnnualCandidateYears(t *testing.T) {
	years := officialRentalAnnualCandidateYears(2025)
	if len(years) != 2 || years[0] != 2025 || years[1] != 2024 {
		t.Fatalf("unexpected verified rental candidate years: %#v", years)
	}
}

func TestSmartMonthlyAvailabilityMergesRealSourcePeriods(t *testing.T) {
	cache := getCacheSkeleton()
	cache.Sources["official"] = sourceCache{
		MonthlyByPeriod: map[string]cacheEntry{
			"2026-08": {Data: Result{PeriodYear: 2026, PeriodMonth: 8, Items: []RankingItem{{Rank: 1, ActressName: "官方八月"}}}},
		},
	}
	cache.Sources["avfan"] = sourceCache{
		MonthlyByPeriod: map[string]cacheEntry{
			"2026-07": {Data: Result{PeriodYear: 2026, PeriodMonth: 7, Items: []RankingItem{{Rank: 1, ActressName: "AVfan七月"}}}},
			"2025-12": {Data: Result{PeriodYear: 2025, PeriodMonth: 12, Items: []RankingItem{{Rank: 1, ActressName: "AVfan十二月"}}}},
		},
	}
	years, months := getMonthlyAvailability(cache, rankingAvailabilityBuckets("smart", "official", []string{"official"}), 2026)
	if len(years) != 2 || years[0] != 2026 || years[1] != 2025 {
		t.Fatalf("unexpected merged years: %#v", years)
	}
	if len(months) != 2 || months[0] != 8 || months[1] != 7 {
		t.Fatalf("unexpected merged months: %#v", months)
	}
}

func TestMonthlyAvailabilityStaysNavigableAfterSwitchingToFANZA(t *testing.T) {
	cache := getCacheSkeleton()
	cache.Sources["official"] = sourceCache{
		MonthlyByPeriod: map[string]cacheEntry{
			"2026-07": {Data: Result{PeriodYear: 2026, PeriodMonth: 7, Items: []RankingItem{{Rank: 1, ActressName: "FANZA七月"}}}},
		},
	}
	cache.Sources["avfan"] = sourceCache{
		MonthlyByPeriod: map[string]cacheEntry{
			"2026-06": {Data: Result{PeriodYear: 2026, PeriodMonth: 6, Items: []RankingItem{{Rank: 1, ActressName: "AVfan六月"}}}},
		},
	}
	cache.Sources["localHistory"] = sourceCache{
		MonthlyByPeriod: map[string]cacheEntry{
			"2026-02": {Data: Result{PeriodYear: 2026, PeriodMonth: 2, Items: []RankingItem{{Rank: 1, ActressName: "本地二月"}}}},
		},
	}

	years, months := getMonthlyAvailability(cache, rankingAvailabilityBuckets("fanza", "fanza", []string{"official"}), 2026)
	if len(years) != 1 || years[0] != 2026 {
		t.Fatalf("unexpected available years after FANZA switch: %#v", years)
	}
	if len(months) != 3 || months[0] != 7 || months[1] != 6 || months[2] != 2 {
		t.Fatalf("FANZA switch must retain every verified local month, got %#v", months)
	}
}

func TestDMMChannelMigratesToFANZA(t *testing.T) {
	if got := normalizeRankingChannel("dmm"); got != "fanza" {
		t.Fatalf("legacy DMM selection must migrate to FANZA, got %q", got)
	}
	if got := getChannelLabel("dmm"); got != "FANZA" {
		t.Fatalf("legacy DMM label must resolve to FANZA, got %q", got)
	}
}

func TestOfficialMonthlyPageURLFollowsVerifiedPagination(t *testing.T) {
	if got := officialMonthlyPageURL(1); got != officialMonthlyURL {
		t.Fatalf("page 1 should stay on the base URL, got %s", got)
	}
	if got := officialMonthlyPageURL(3); got != "https://www.dmm.co.jp/mono/dvd/-/ranking/=/mode=actress/term=monthly/page=3/" {
		t.Fatalf("unexpected page URL: %s", got)
	}
}

func TestParseAVFanAnnualSupportsTableRowLayout(t *testing.T) {
	htmlSource := `
<html>
  <head><title>【2023年】【FANZA】年間AV女優ランキング | AVfan</title></head>
  <body>
    <div class="ranking-year-link"><a href="?year=2023">2023</a></div>
    <table>
      <tr><td><b>导航表格不得计入</b></td><td><a href="/ranking/monthly.html">月榜</a></td></tr>
      <tr>
        <td valign="middle">
          <div class="ranking-cnt"><span><b>1</b>位</span></div>
        </td>
        <td valign="middle">
          <a href="https://av-fan.tokyo/actress/%E6%A5%AA%E3%82%AB%E3%83%AC%E3%83%B3.html"><img src="https://pics.dmm.co.jp/mono/actjpgs/yuzuriha_karen.jpg" alt="楪カレン" align="left"></a>
        </td>
        <td valign="middle">
          <a href="https://av-fan.tokyo/actress/%E6%A5%AA%E3%82%AB%E3%83%AC%E3%83%B3.html">楪カレン</a>
        </td>
      </tr>
      <tr>
        <td valign="middle">
          <div class="ranking-cnt"><span><b>2</b>位</span></div>
        </td>
        <td valign="middle">
          <a href="https://av-fan.tokyo/actress/test2.html"><img src="https://pics.dmm.co.jp/mono/actjpgs/test2.jpg" alt="" align="left"></a>
        </td>
        <td valign="middle">
          <a href="https://av-fan.tokyo/actress/test2.html">另一名演员</a>
        </td>
      </tr>
    </table>
  </body>
</html>`

	result, err := parseAVFanRankingHTML(htmlSource, "annual", "https://av-fan.tokyo/ranking/fanza-rental-dvd-actress-top100.php?year=2023", 2023)
	if err != nil {
		t.Fatalf("parseAVFanRankingHTML returned error: %v", err)
	}
	if result.PeriodYear != 2023 {
		t.Fatalf("PeriodYear = %d, expected 2023", result.PeriodYear)
	}
	if result.Total != 2 {
		t.Fatalf("result.Total = %d, expected 2（导航表格行必须被排除）", result.Total)
	}
	if result.Items[0].Rank != 1 || result.Items[0].ActressName != "楪カレン" {
		t.Fatalf("first item = rank %d name %q", result.Items[0].Rank, result.Items[0].ActressName)
	}
	if result.Items[0].ImageURL == "" {
		t.Fatalf("first item image URL is empty")
	}
}

func TestBundledAnnualSnapshotContainsVerifiedYears(t *testing.T) {
	compressed, err := base64.StdEncoding.DecodeString(bundledAnnualHistoryBase64)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer reader.Close()
	payload, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("gzip read: %v", err)
	}
	var bundled cacheFile
	if err := json.Unmarshal(payload, &bundled); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}

	expected := map[string]map[string]int{
		"official": {"2024": 100, "2025": 100},
		"avfan":    {"2021": 90, "2022": 90, "2023": 90},
	}
	for bucketID, years := range expected {
		source, ok := bundled.Sources[bucketID]
		if !ok {
			t.Fatalf("snapshot missing bucket %s", bucketID)
		}
		for year, wantCount := range years {
			entry, ok := source.AnnualByYear[year]
			if !ok {
				t.Fatalf("%s missing annual year %s", bucketID, year)
			}
			if len(entry.Data.Items) != wantCount {
				t.Fatalf("%s %s: items = %d, want %d", bucketID, year, len(entry.Data.Items), wantCount)
			}
			seen := map[int]bool{}
			for _, item := range entry.Data.Items {
				if seen[item.Rank] {
					t.Fatalf("%s %s: duplicate rank %d", bucketID, year, item.Rank)
				}
				seen[item.Rank] = true
			}
		}
	}
}

func TestGetBundledInitialCacheIncludesAnnualYears(t *testing.T) {
	cache := getBundledInitialCache()
	official := cache.Sources["official"]
	for _, year := range []string{"2024", "2025"} {
		if entry, ok := official.AnnualByYear[year]; !ok || len(entry.Data.Items) != 100 {
			t.Fatalf("official bundled annual %s missing or incomplete", year)
		}
	}
	avfan := cache.Sources["avfan"]
	for _, year := range []string{"2021", "2022", "2023"} {
		entry, ok := avfan.AnnualByYear[year]
		if !ok || len(entry.Data.Items) != 90 {
			t.Fatalf("avfan bundled annual %s missing or incomplete", year)
		}
	}
}
