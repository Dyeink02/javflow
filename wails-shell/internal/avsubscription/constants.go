package avsubscription

// Subscription sources are persisted and returned to the frontend. Keep them
// centralized so artifact-import flow, manual edits, and future refresh logic
// use the same stable vocabulary.
//
// Ownership summary:
// 1) centralize persisted AV subscription source labels
// 2) centralize subscription status vocabulary used by UI and storage
// 3) keep scan/import source constants stable across subscription flows
//
// File map for maintainers:
// 1) subscription source constants
// 2) subscription status constants
// 3) import scan-source constants
const (
	sourceManual = "manual"
	sourceScan   = "scan"
)

// Subscription statuses are derived summary labels, not crawler runtime
// statuses. They should stay coarse and stable for UI display and local
// persistence.
const (
	statusIdle    = "idle"
	statusUpdated = "updated"
	statusError   = "error"
)

// Scan source types describe which persisted crawl artifact produced the
// current import result. Keep these stable because they are also surfaced in
// operator-facing diagnostics and future subscription cache traces.
const (
	scanSourceFilmData     = "filmData"
	scanSourceCrawlProfile = "crawlProfile"
)
