// Package avsubscriptionv2 owns the rebuilt AV-subscription workflow.
//
// This V2 lane intentionally separates:
// 1) baseline creation/import
// 2) update detection
// 3) incremental crawl execution handoff
//
// It does not reuse the old "count-only" subscription semantics.
//
// Ownership summary:
//
//	This file defines V2 subscription domain types and constants.
//
// File map for maintainers:
//  1. Subscription source/status constants.
//  2. Core Subscription, ScanRuntimeOptions, and refresh result types.
//  3. Display and sorting helpers.
package avsubscriptionv2

import "time"

const (
	defaultItemsPerPage = 30
	defaultMaxScanPages = 50

	sourceTypeCrawlImport  = "crawl-import"
	sourceTypeManual       = "manual"
	sourceTypeMetadataAuto = "metadata-auto"

	statusIdle    = "idle"
	statusUpdated = "updated"
	statusRunning = "running"
	statusError   = "error"
)

// Subscription is the persisted AV-subscription V2 record.
//
// Keep this shape smaller than crawler runtime state. It is the durable,
// cross-run subscription baseline and refresh result only.
type Subscription struct {
	ID                          string   `json:"id"`
	ActressName                 string   `json:"actressName"`
	SortOrder                   int      `json:"sortOrder"`
	CrawlURL                    string   `json:"crawlUrl"`
	PreferredBase               string   `json:"preferredBase,omitempty"`
	SourceType                  string   `json:"sourceType"`
	BaselineCodes               []string `json:"baselineCodes,omitempty"`
	BaselineCount               int      `json:"baselineCount"`
	CurrentObservedCount        int      `json:"currentObservedCount"`
	CurrentTotal                int      `json:"currentTotal"`
	PendingCodes                []string `json:"pendingCodes,omitempty"`
	PendingCount                int      `json:"pendingCount"`
	ActressCountFilterThreshold int      `json:"actressCountFilterThreshold,omitempty"`
	ItemsPerPage                int      `json:"itemsPerPage"`
	TotalPages                  int      `json:"totalPages"`
	LastScanPages               int      `json:"lastScanPages,omitempty"`
	LastStoppedOnPage           int      `json:"lastStoppedOnPage,omitempty"`
	LatestItemURL               string   `json:"latestItemUrl,omitempty"`
	PreferredOutputDir          string   `json:"preferredOutputDir,omitempty"`
	LastCrawlOutputDir          string   `json:"lastCrawlOutputDir,omitempty"`
	CreatedAt                   string   `json:"createdAt"`
	BaselineSnapshotAt          string   `json:"baselineSnapshotAt"`
	LastCheckedAt               string   `json:"lastCheckedAt,omitempty"`
	LastUpdatedAt               string   `json:"lastUpdatedAt,omitempty"`
	LastUpdateDetectedAt        string   `json:"lastUpdateDetectedAt,omitempty"`
	LastCrawlAt                 string   `json:"lastCrawlAt,omitempty"`
	AvatarURL                   string   `json:"avatarUrl,omitempty"`
	PhotoURLs                   []string `json:"photoUrls,omitempty"`
	MediaUpdatedAt              string   `json:"mediaUpdatedAt,omitempty"`
	LastError                   string   `json:"lastError,omitempty"`
	Status                      string   `json:"status"`
	ManualDeclaredTotal         int      `json:"manualDeclaredTotal,omitempty"`
	ManualDeclaredPages         int      `json:"manualDeclaredPages,omitempty"`
	ManualDeclaredPerPage       int      `json:"manualDeclaredItemsPerPage,omitempty"`
}

// ImportResult is the source-import result surface used by the bridge/UI.
type ImportResult struct {
	Subscription Subscription `json:"subscription"`
	Added        bool         `json:"added"`
	Updated      bool         `json:"updated"`
	SourceType   string       `json:"sourceType"`
	OutputDir    string       `json:"outputDir"`
	FilmDataPath string       `json:"filmDataPath,omitempty"`
}

// ManualCreateRequest is the V2 manual subscription baseline-init contract.
type ManualCreateRequest struct {
	ActressName     string
	CrawlURL        string
	PreferredBase   string
	DeclaredTotal   int
	DeclaredPages   int
	DeclaredPerPage int
	PreferredOutDir string
	Proxy           string
	RuntimeOptions  ScanRuntimeOptions
	SourceType      string
}

// ScanRuntimeOptions carries the main JAV crawler request settings into AV
// subscription detection. V2 owns the diff logic, but page access must stay on
// the shared crawler request lane so Cloudflare/age-check behavior is not
// reimplemented here.
type ScanRuntimeOptions struct {
	Proxy             string
	Timeout           time.Duration
	ConfigCookie      string
	CloudflareCookies string
	UserAgent         string
	Headers           map[string]string
	RetryCount        int
	RetryDelay        time.Duration
	Cloudflare        bool
	SecondValidation  bool
	PageFetcher       ScanPageFetcher
}

// ScanPageFetcher is a narrow page-access seam for update detection. AV
// subscription owns diffing, while callers may provide the main JAV crawler
// Cloudflare/age-check page lane when Cloudflare compatibility is enabled.
type ScanPageFetcher func(pageURL string, runtimeOptions ScanRuntimeOptions) (ScanPageFetchResult, error)

type ScanPageFetchResult struct {
	URL        string
	StatusCode int
	Body       string
	Links      []string
	LinkCount  int
}

// RefreshSummary is the batch update-detection response for one refresh pass.
type RefreshSummary struct {
	Subscriptions []Subscription `json:"subscriptions"`
	CheckedCount  int            `json:"checkedCount"`
	UpdatedCount  int            `json:"updatedCount"`
	DetectedCount int            `json:"detectedCount"`
	FailedCount   int            `json:"failedCount"`
	TotalPending  int            `json:"totalPending"`
}

// RefreshPageSnapshot is one page-level diagnostic fragment produced during
// V2 detection. It stays lightweight so logs and future UI review panels can
// explain why the detector stopped.
type RefreshPageSnapshot struct {
	Page           int      `json:"page"`
	ObservedCodes  []string `json:"observedCodes,omitempty"`
	NewCodes       []string `json:"newCodes,omitempty"`
	RepeatedCodes  []string `json:"repeatedCodes,omitempty"`
	FullPageRepeat bool     `json:"fullPageRepeat"`
	ContinueScan   bool     `json:"continueScan"`
	StopReason     string   `json:"stopReason,omitempty"`
	ObservedCount  int      `json:"observedCount"`
	NewCount       int      `json:"newCount"`
	RepeatedCount  int      `json:"repeatedCount"`
	RawLinkCount   int      `json:"rawLinkCount,omitempty"`
	PageURL        string   `json:"pageUrl,omitempty"`
	ResponseURL    string   `json:"responseUrl,omitempty"`
}

// RefreshResult is the per-subscription page-scan diff result.
type RefreshResult struct {
	Subscription      Subscription          `json:"subscription"`
	HasUpdate         bool                  `json:"hasUpdate"`
	NewUpdateDetected bool                  `json:"newUpdateDetected"`
	ObservedCount     int                   `json:"observedCount"`
	PendingCodes      []string              `json:"pendingCodes,omitempty"`
	ScannedPages      int                   `json:"scannedPages"`
	StoppedOnPage     int                   `json:"stoppedOnPage"`
	PageSnapshots     []RefreshPageSnapshot `json:"pageSnapshots,omitempty"`
}
