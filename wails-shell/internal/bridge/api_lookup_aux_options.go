package bridge

import (
	"javflow/internal/actressranking"
	"javflow/internal/antiblock"
)

// Auxiliary lookup option builders cover non-target features such as rankings
// and anti-block updates. These are operational helpers, not crawl-target
// parsers.
//
// Ownership summary:
// 1) build normalized anti-block and ranking option payloads
// 2) reuse current settings snapshots for auxiliary lookup helpers
// 3) keep non-target lookup option shaping separate from target parsing
//
// File map for maintainers:
// 1) anti-block option builder
// 2) actress ranking option builder
func (a *API) buildAntiBlockOptions(payload map[string]any) antiblock.UpdateOptions {
	currentSettings := a.loadBridgeSettingsSnapshot()

	baseValue := nonEmptyString(payload["base"])
	if baseValue == "" {
		baseValue = nonEmptyString(currentSettings["base"])
	}

	proxyValue := nonEmptyString(payload["proxy"])
	if proxyValue == "" {
		proxyValue = nonEmptyString(currentSettings["proxy"])
	}

	return antiblock.UpdateOptions{
		Base:  baseValue,
		Proxy: proxyValue,
	}
}

func (a *API) buildActressRankingOptions(payload map[string]any) actressranking.Options {
	currentSettings := a.loadBridgeSettingsSnapshot()

	proxyValue := nonEmptyString(payload["proxy"])
	if proxyValue == "" {
		proxyValue = nonEmptyString(currentSettings["proxy"])
	}

	cacheFilePath := ""
	historyDirectories := []string(nil)
	if a.runtime.store != nil {
		cacheFilePath = a.runtime.store.GetRankingCachePath()
		historyDirectories = a.runtime.store.GetRankingHistoryDirectories()
	}

	return actressranking.Options{
		Mode:               nonEmptyString(payload["mode"]),
		Year:               intValue(payload["year"], 0),
		Month:              intValue(payload["month"], 0),
		Source:             nonEmptyString(payload["source"]),
		Proxy:              proxyValue,
		ForceRefresh:       boolValue(payload["forceRefresh"], false),
		CacheFilePath:      cacheFilePath,
		HistoryDirectories: historyDirectories,
	}
}
