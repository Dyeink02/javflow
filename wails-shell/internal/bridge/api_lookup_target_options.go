package bridge

import (
	"fmt"

	"javflow/internal/actresslookup"
	"javflow/internal/contracts/subscriptiontarget"
)

// Target lookup options are shared by actress target resolution and future
// lightweight subscription refresh flows. Keep them separate from rankings and
// anti-block shaping so target parsing issues have one obvious boundary.
//
// Ownership summary:
// 1) build normalized actress/subscription target lookup options
// 2) combine payload values with settings-derived fallback bases/proxy
// 3) keep target option shaping separate from ranking and anti-block helpers
//
// File map for maintainers:
// 1) lookup option builder entrypoints
// 2) payload/settings fallback merge helpers
func (a *API) buildActressLookupOptions(payload map[string]any) actresslookup.ResolveOptions {
	currentSettings := a.loadBridgeSettingsSnapshot()

	// Renderer payloads may carry a previously resolved profile URL. Keep that
	// convenience, but never let an arbitrary address become an actor lookup
	// source through this bridge path.
	targetURL := nonEmptyString(payload["targetUrl"])
	if !actresslookup.IsAllowedActressLookupTargetURL(targetURL) {
		targetURL = ""
	}
	preferredBase := nonEmptyString(payload["preferredBase"])
	if preferredBase == "" {
		preferredBase = nonEmptyString(currentSettings["base"])
	}
	if !actresslookup.IsAllowedActressLookupBaseURL(preferredBase) {
		preferredBase = ""
	}

	proxyValue := nonEmptyString(payload["proxy"])
	if proxyValue == "" {
		proxyValue = nonEmptyString(currentSettings["proxy"])
	}
	enrichProfile, _ := payload["includeProfile"].(bool)
	basicProfileOnly, _ := payload["basicProfileOnly"].(bool)

	return actresslookup.ResolveOptions{
		ActressName:      nonEmptyString(payload["actressName"]),
		TargetURL:        targetURL,
		PreferredBase:    preferredBase,
		FallbackBases:    buildSubscriptionFallbackBases(targetURL, preferredBase),
		Proxy:            proxyValue,
		EnrichProfile:    enrichProfile,
		BasicProfileOnly: basicProfileOnly,
	}
}

func (a *API) inspectSubscriptionTarget(payload map[string]any) (subscriptiontarget.TargetProfile, error) {
	if a.lookup.actressLookup == nil {
		return subscriptiontarget.TargetProfile{}, fmt.Errorf("actress lookup service is not initialized")
	}
	return a.lookup.actressLookup.InspectTarget(a.buildActressLookupOptions(payload))
}
