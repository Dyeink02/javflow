package actressranking

import (
	"encoding/json"
	"os"
	"testing"
)

// TestLiveFetchOfficialMonthlyRanking captures the full five-page FANZA
// monthly ranking through the real browser path so the verified 100-row
// result can be baked into the bundled offline cache. Opt-in: it contacts
// FANZA through the user's proxy and needs a local Chrome.
func TestLiveFetchOfficialMonthlyRanking(t *testing.T) {
	if os.Getenv("JAVFLOW_RANKING_LIVE_FETCH") != "1" {
		t.Skip("set JAVFLOW_RANKING_LIVE_FETCH=1 to run the live monthly ranking capture")
	}

	service := NewService()
	result, err := service.fetchOfficialMonthlyRanking("http://127.0.0.1:7897", "fanza")
	if err != nil {
		t.Fatalf("live monthly fetch failed: %v", err)
	}
	if len(result.Items) != 100 {
		t.Fatalf("expected the verified 100-row ranking, got %d", len(result.Items))
	}

	payload, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	outputPath := os.Getenv("JAVFLOW_RANKING_LIVE_OUTPUT")
	if outputPath == "" {
		outputPath = "live-monthly-ranking.json"
	}
	if err := os.WriteFile(outputPath, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("captured %d rows -> %s (fetchedAt=%s)", len(result.Items), outputPath, result.FetchedAt)
}
