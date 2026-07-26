package bridge

// Organizer is allowed to consume crawl artifacts, but that cross-module read
// should stay behind one bridge helper so future organizer work does not
// re-implement crawl-output fallback rules in multiple places.
//
// Important boundary:
// - organizer reads stable crawl artifacts such as filmData.json
// - organizer should not depend on live crawler controller/runtime state
// This keeps file-organization bugs isolated from crawl-controller bugs.
//
// Preferred payload field:
// - artifactInput
//
// Legacy compatibility fields:
// - outputDir
// - crawlOutputDir
//
// Ownership summary:
// 1) resolve organizer-facing crawl artifact input/output directory pointers
// 2) preserve one compatibility boundary for organizer consumption of crawl artifacts
// 3) keep organizer crawl-input fallback rules out of multiple call sites
//
// File map for maintainers:
// 1) organizer crawl-artifact input resolution helper
func (a *API) resolveOrganizerCrawlOutputDir(payload map[string]any) string {
	return a.resolveArtifactInputOutputDir(payload, "outputDir", "crawlOutputDir")
}
