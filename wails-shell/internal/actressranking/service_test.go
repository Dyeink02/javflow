package actressranking

import (
	"os"
	"path/filepath"
	"testing"
)

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
		{name: "official monthly", requestedChannel: "fanza", mode: "monthly", expected: []string{"official", "avfan", "local"}},
		{name: "official annual", requestedChannel: "dmm", mode: "annual", expected: []string{"avfan", "local"}},
		{name: "smart annual", requestedChannel: "smart", mode: "annual", expected: []string{"avfan", "local"}},
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
