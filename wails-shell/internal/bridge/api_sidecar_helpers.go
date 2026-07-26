package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Sidecar compatibility entry helpers stay separate so the legacy bridge path
// remains explicit while ordinary Go-native work continues to shrink.
//
// Ownership summary:
// 1) centralize bridge-side sidecar startup and raw compatibility calls
// 2) keep remaining legacy command mappings explicit
// 3) separate sidecar helper plumbing from ordinary Go-native command flow
//
// Boundary rule:
// bridge files may depend on this helper for the active crawl compatibility
// lane, but organizer / subscription / lookup feature ownership must not drift
// back into sidecar-only transports.
//
// File map for maintainers:
// 1) legacy command mapping helpers
// 2) sidecar startup lifecycle helper
// 3) raw bridge-to-sidecar call adapter

func legacySidecarCommand(command string) (string, string, bool) {
	switch command {
	case "app:restart-crawl":
		return "crawl", "restart", true
	default:
		return "", "", false
	}
}

func (a *API) ensureSidecarStarted() error {
	if a.runtime.manager == nil {
		return fmt.Errorf("Node sidecar manager is not initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	return a.runtime.manager.Start(ctx)
}

// callSidecar keeps the remaining compatibility boundary uniform. Bridge files
// should explain why a request still needs the Node path, while this helper
// owns startup, raw call execution, and empty-result normalization.
func (a *API) callSidecar(domain string, action string, payload map[string]any) (string, error) {
	if err := a.ensureSidecarStarted(); err != nil {
		return "", err
	}

	raw, err := a.runtime.manager.Call(context.Background(), domain, action, payload)
	if err != nil {
		return "", err
	}
	if len(raw) == 0 {
		return "null", nil
	}
	return string(raw), nil
}

// callSidecarJSON is the typed variant for compatibility lanes that still need
// a structured JSON response from the sidecar.
func (a *API) callSidecarJSON(domain string, action string, payload map[string]any, target any) error {
	raw, err := a.callSidecar(domain, action, payload)
	if err != nil {
		return err
	}
	if raw == "" || raw == "null" {
		return nil
	}
	return json.Unmarshal([]byte(raw), target)
}
