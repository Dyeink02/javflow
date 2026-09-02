package actressranking

import (
	"os"
	"testing"
	"time"
)

func TestLiveDiagRealFunction(t *testing.T) {
	if os.Getenv("JAVFLOW_RANKING_LIVE_FETCH") != "1" {
		t.Skip("opt-in")
	}
	service := NewService()
	started := time.Now()
	htmlSource, pageURL, title, err := service.browser.fetchOfficialRankingHTML(officialMonthlyURL, "http://127.0.0.1:7897")
	t.Logf("elapsed=%s err=%v pageURL=%s title=%s htmlLen=%d", time.Since(started), err, pageURL, title, len(htmlSource))
	if err != nil {
		t.Fatalf("real function failed: %v", err)
	}
}
