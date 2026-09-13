package bridge

import (
	"encoding/json"
	"testing"

	"javflow/internal/proxy"
)

func TestCheckProxyRegionCommandRoutesToProxyService(t *testing.T) {
	api := &API{runtime: runtimeFacade{proxyService: proxy.NewService()}}
	raw, handled, err := api.handleRuntimeBootstrapCommand("app:check-proxy-region", map[string]any{
		"proxyValue": "bad proxy",
	})
	if err != nil {
		t.Fatalf("check proxy region command returned error: %v", err)
	}
	if !handled {
		t.Fatal("check proxy region command was not handled")
	}
	var result proxy.RegionCheckResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("decode region result: %v", err)
	}
	if result.Status != "invalid" || !result.ViaProxy {
		t.Fatalf("unexpected region result: %#v", result)
	}
}
