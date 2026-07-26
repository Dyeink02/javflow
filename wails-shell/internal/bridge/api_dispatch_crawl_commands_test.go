package bridge

import (
	"strings"
	"testing"
)

func TestHandleCrawlTestingCommandClaimsIndexProbeCommands(t *testing.T) {
	api := &API{}

	_, handled, err := api.handleCrawlTestingCommand("crawl:go-test-index-page", map[string]any{})
	if !handled {
		t.Fatalf("expected crawl testing command to claim index probe command")
	}
	if err == nil || !strings.Contains(err.Error(), "Go crawl fetch service is not initialized") {
		t.Fatalf("expected missing fetch service error, got %v", err)
	}
}

func TestHandleCrawlLifecycleCommandClaimsStopCommand(t *testing.T) {
	api := &API{}

	result, handled, err := api.handleCrawlLifecycleCommand("app:stop-crawl", map[string]any{})
	if !handled {
		t.Fatalf("expected crawl lifecycle command to claim stop command")
	}
	if err != nil {
		t.Fatalf("expected stop command to succeed without active runner, got %v", err)
	}
	if !strings.Contains(result, `"status":"already-stopped"`) {
		t.Fatalf("expected already-stopped result, got %s", result)
	}
}

func TestHandleCrawlLifecycleCommandClaimsNativeStartCommand(t *testing.T) {
	api := &API{}

	_, handled, err := api.handleCrawlLifecycleCommand("crawl:go-native-start", map[string]any{})
	if !handled {
		t.Fatalf("expected crawl lifecycle command to claim native start command")
	}
	if err == nil || !strings.Contains(err.Error(), "Go native crawl runner is not initialized") {
		t.Fatalf("expected missing native runner error, got %v", err)
	}
}

func TestHandleCrawlSupportCommandClaimsAntiBlockUpdate(t *testing.T) {
	api := &API{}

	_, handled, err := api.handleCrawlSupportCommand("app:update-antiblock", map[string]any{})
	if !handled {
		t.Fatalf("expected crawl support command to claim anti-block update")
	}
	if err == nil || !strings.Contains(err.Error(), "anti-block service is not initialized") {
		t.Fatalf("expected missing anti-block service error, got %v", err)
	}
}

func TestHandleCrawlCommandLeavesUnknownCommandUnclaimed(t *testing.T) {
	api := &API{}

	_, handled, err := api.handleCrawlCommand("app:unknown-crawl-command", map[string]any{})
	if handled {
		t.Fatalf("expected unknown crawl command to stay unclaimed")
	}
	if err != nil {
		t.Fatalf("expected no error for unclaimed crawl command, got %v", err)
	}
}
