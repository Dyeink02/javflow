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
	targetURL := os.Getenv("JAVFLOW_RANKING_LIVE_URL")
	if targetURL == "" {
		targetURL = officialMonthlyURL
	}
	htmlSource, pageURL, title, err := service.browser.fetchOfficialRankingHTML(targetURL, "http://127.0.0.1:7897")
	t.Logf("elapsed=%s err=%v pageURL=%s title=%s htmlLen=%d", time.Since(started), err, pageURL, title, len(htmlSource))
	if err != nil {
		t.Fatalf("real function failed: %v", err)
	}
	if outputPath := os.Getenv("JAVFLOW_RANKING_LIVE_HTML_OUTPUT"); outputPath != "" {
		if err := os.WriteFile(outputPath, []byte(htmlSource), 0o644); err != nil {
			t.Fatalf("write captured HTML: %v", err)
		}
		t.Logf("captured HTML -> %s", outputPath)
	}
}
