package librarymetadata

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestScrapeByNumber_BBAN452(t *testing.T) {
	if os.Getenv("RUN_NETWORK_TESTS") != "1" {
		t.Skip("network scrape test disabled; set RUN_NETWORK_TESTS=1 to enable")
	}
	svc := NewService()

	providers := svc.SearchProviders()
	if len(providers) == 0 {
		t.Fatal("expected at least one provider")
	}
	t.Logf("available providers: %v", providers)

	proxy := os.Getenv("TEST_PROXY")
	if proxy != "" {
		t.Logf("using test proxy: %s", proxy)
	}

	result, err := svc.ScrapeByNumber(context.Background(), ScrapeOptions{
		Number: "BBAN-452",
		Proxy:  proxy,
	})
	if err != nil {
		t.Logf("scrape error: %v", err)
	}
	if result.Info == nil {
		t.Skip("no network result; set TEST_PROXY env to run live scrape verification")
	}
	if !strings.EqualFold(result.Info.Number, "BBAN-452") {
		t.Fatalf("expected BBAN-452, got %s", result.Info.Number)
	}
	if result.Info.Title == "" {
		t.Fatal("expected non-empty title")
	}
	t.Logf("BBAN-452 title=%s provider=%s cover=%s", result.Info.Title, result.Info.Provider, result.Info.CoverURL)
}
