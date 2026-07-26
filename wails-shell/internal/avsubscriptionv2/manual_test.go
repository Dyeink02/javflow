package avsubscriptionv2

import (
	"context"
	"testing"

	runtimepaths "javflow/internal/runtime"
)

func TestCreateManualPreservesMetadataAutoSource(t *testing.T) {
	service := NewService(runtimepaths.Paths{UserData: t.TempDir()}, nil)
	saved, err := service.CreateManual(context.Background(), ManualCreateRequest{
		ActressName:     "松本いちか",
		CrawlURL:        "https://example.com/star/abc",
		PreferredBase:   "https://example.com",
		DeclaredTotal:   100,
		DeclaredPages:   4,
		DeclaredPerPage: 30,
		SourceType:      "metadata-auto",
		RuntimeOptions: ScanRuntimeOptions{
			Cloudflare: true,
			PageFetcher: func(pageURL string, _ ScanRuntimeOptions) (ScanPageFetchResult, error) {
				return ScanPageFetchResult{
					URL:        pageURL,
					StatusCode: 200,
					Links: []string{
						"https://example.com/ABP-001",
						"https://example.com/ABP-002",
					},
				}, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if saved.SourceType != sourceTypeMetadataAuto {
		t.Fatalf("expected %s source, got %s", sourceTypeMetadataAuto, saved.SourceType)
	}
	if saved.BaselineCount != 2 {
		t.Fatalf("expected 2 baseline codes, got %d", saved.BaselineCount)
	}
	if saved.CurrentTotal != 100 {
		t.Fatalf("expected currentTotal=100, got %d", saved.CurrentTotal)
	}
	if saved.CurrentObservedCount != 100 {
		t.Fatalf("expected currentObservedCount=100, got %d", saved.CurrentObservedCount)
	}
}
