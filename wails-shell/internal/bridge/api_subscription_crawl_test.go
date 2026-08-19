package bridge

import (
	"testing"

	runtimepaths "javflow/internal/runtime"
	"javflow/internal/settings"
)

func TestResolveSubcrawlProxyPrefersRequestThenGlobalSetting(t *testing.T) {
	paths := runtimepaths.Paths{UserData: t.TempDir(), Documents: t.TempDir()}
	store := settings.NewStore(paths)
	if err := store.Save(map[string]any{"proxy": "http://global-proxy.example:7890"}); err != nil {
		t.Fatalf("save global proxy: %v", err)
	}
	api := &API{runtime: runtimeFacade{store: store}}

	if got := api.resolveSubcrawlProxy(map[string]any{"proxy": "socks5://manual-proxy.example:1080"}); got != "socks5://manual-proxy.example:1080" {
		t.Fatalf("request proxy = %q, want manual proxy", got)
	}
	if got := api.resolveSubcrawlProxy(map[string]any{}); got != "http://global-proxy.example:7890" {
		t.Fatalf("fallback proxy = %q, want saved global proxy", got)
	}
}

func TestResolveSubcrawlProxyHasNoFixedLoopbackFallback(t *testing.T) {
	api := &API{}
	if got := api.resolveSubcrawlProxy(map[string]any{}); got != "" {
		t.Fatalf("empty proxy fallback = %q, want empty", got)
	}
}
