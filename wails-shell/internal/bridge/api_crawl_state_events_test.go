package bridge

import (
	"testing"

	"javflow/internal/events"
	runtimepaths "javflow/internal/runtime"
	"javflow/internal/settings"
)

func TestResolveCrawlExecutionModeRoutesMagnetValidationToCompatibilityLane(t *testing.T) {
	api := &API{}

	mode := api.resolveCrawlExecutionMode(map[string]any{
		"magnetContentValidation": true,
	})

	if mode != crawlExecutionModeCloudflareCompat {
		t.Fatalf("expected magnet validation to use compatibility lane, got %q", mode)
	}
}

func TestPrepareCrawlStartPayloadDefaultsMissingMagnetValidationToDisabled(t *testing.T) {
	tempDir := t.TempDir()
	api := &API{
		runtime: runtimeFacade{
			store: settings.NewStore(runtimepaths.Paths{UserData: tempDir, Documents: tempDir}),
			bus:   events.NewBus(),
		},
	}

	prepared, err := api.prepareCrawlStartPayload(map[string]any{})
	if err != nil {
		t.Fatalf("prepare crawl start payload: %v", err)
	}
	if got := prepared["magnetContentValidation"]; got != false {
		t.Fatalf("prepared magnetContentValidation = %v, want false", got)
	}
}
