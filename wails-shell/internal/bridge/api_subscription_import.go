package bridge

// scanSubscriptionsFromOutputResult is the artifact-import path for AV
// subscriptions. It reads persisted crawl artifacts and infers a candidate
// subscription state, but it is not the long-term subscription crawl engine.
//
// Boundary rule:
// - this path accepts artifact files or output directories only
// - it must stay independent from live crawler controller/runtime state
//
// Accepted input, in preferred order:
// 1) crawl-profile.json
// 2) filmData.json
// 3) crawl output directory
//
// Preferred payload field:
// - artifactInput
//
// Legacy compatibility field:
// - outputDir
//
// Ownership summary:
// 1) expose bridge-side AV subscription artifact import entrypoints
// 2) keep persisted-crawl import separate from remote refresh logic
// 3) centralize artifact-input alias resolution for subscription import
//
// File map for maintainers:
// 1) artifact-import result entrypoint
// 2) artifactInput/outputDir resolution helper
func (a *API) scanSubscriptionsFromOutputResult(payload map[string]any) (string, error) {
	scanned, err := a.lookup.avSubscriptions.ScanOutput(a.resolveSubscriptionOutputDir(payload))
	if err != nil {
		return "", err
	}
	return marshalResult(scanned)
}

// resolveSubscriptionOutputDir keeps the subscription artifact-import
// compatibility boundary readable at the call site while delegating the actual
// alias priority plus artifact-path normalization to the shared bridge helper.
// The current Wails path should send artifactInput only; outputDir remains a
// fallback alias for older callers and historical desktop paths.
//
// Debugging rule:
// this entrypoint should only answer "which persisted crawl run do we import
// from?" It must not peek at live crawler form/runtime state, because AV
// subscription import is meant to stay usable even when crawler UI state has
// already changed to a different actress/run.
//
// Remote refresh belongs to the subscription service/query path, not here.
// Keep this helper focused on persisted artifact resolution only so failures
// can be separated into:
// 1) wrong artifact selected
// 2) refresh/list mutation logic
func (a *API) resolveSubscriptionOutputDir(payload map[string]any) string {
	return a.resolveArtifactInputOutputDir(payload, "outputDir")
}
