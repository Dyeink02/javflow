package actressranking

import (
	"os"
	"testing"
	"time"
)

// TestLiveAnnualRankingAvailability probes which past calendar years still
// expose an official FANZA rental annual actress ranking on the live source.
// Opt-in: it contacts FANZA through the user's proxy and needs a local Chrome.
func TestLiveAnnualRankingAvailability(t *testing.T) {
	if os.Getenv("JAVFLOW_ANNUAL_LIVE_FETCH") != "1" {
		t.Skip("set JAVFLOW_ANNUAL_LIVE_FETCH=1 to probe live annual ranking availability")
	}
	proxy := os.Getenv("JAVFLOW_LIVE_PROXY")
	if proxy == "" {
		proxy = "http://127.0.0.1:7897"
	}

	service := NewService()
	currentYear := time.Now().Year()
	for offset := 1; offset <= 5; offset++ {
		year := currentYear - offset
		result, err := service.fetchOfficialRentalAnnualRanking(year, proxy, "fanza")
		if err != nil {
			t.Logf("%d 年官方年榜不可用：%v", year, err)
			continue
		}
		t.Logf("%d 年官方年榜可用：%d 位（period=%s, source=%s）", year, len(result.Items), result.PeriodLabel, result.SourceName)
	}
}
