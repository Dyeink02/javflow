package bridge

import "javflow/internal/contracts/crawlartifact"

// Integration context is the cross-module handoff payload shared with
// organizer/subscription flows. It should stay narrowly focused on stable
// output paths so downstream modules do not depend on crawl internals.
//
// Handoff rule:
// only expose stable artifact/output pointers here. If another module needs
// detailed crawler execution state, that should come from its own explicit
// query path rather than silently broadening integration context.
// getIntegrationContext is the narrow cross-module handoff query for stable
// crawl artifact/output pointers.
//
// Ownership summary:
// 1) assemble the narrow cross-module integration context payload
// 2) expose stable crawl artifact/output pointers only
// 3) keep integration handoff separate from detailed runtime-state queries
//
// File map for maintainers:
// 1) cross-module integration-context query helper
func (a *API) getIntegrationContext() (map[string]any, error) {
	runContext, err := a.getCrawlRunContext()
	if err != nil {
		return nil, err
	}

	cacheSnapshots := []crawlartifact.CacheSnapshot{}
	if a.runtime.store != nil {
		cacheSnapshots, _ = crawlartifact.DiscoverCacheSnapshots(a.runtime.store.UserDataDir(), nil)
	}

	return map[string]any{
		"currentTaskOutputDir":        runContext.CurrentTaskOutputDir,
		"lastTaskOutputDir":           runContext.LastTaskOutputDir,
		"preferredOutputDir":          runContext.PreferredOutputDir,
		"appPath":                     a.runtime.paths.AppPath,
		"preferredFilmDataPath":       runContext.PreferredFilmDataPath,
		"preferredCrawlProfilePath":   runContext.PreferredCrawlProfilePath,
		"preferredOrganizerCodesPath": runContext.PreferredOrganizerCodesPath,
		"crawlCacheSnapshots":         cacheSnapshots,
	}, nil
}
