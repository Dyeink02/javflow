// Package actressranking exposes one normalized ranking contract over official,
// AVfan, and local historical data without owning crawler workflow state.
//
// Maintenance boundary:
// - expose source-neutral ranking inputs and outputs
// - retain cache layout types local to this package
// - keep UI/bridge serialization outside this package
//
// Ownership summary:
// 1) define the stable vocabulary shared by parsers, caches, and bridge callers
// 2) limit synchronization to final cache commits, never network requests
// 3) keep upstream source identity separate from on-disk cache buckets
//
// File map for maintainers:
// 1) types.go: contracts, cache records, and service-local state
// 2) bundled_history.go: immutable verified offline snapshot
// 3) browser.go: browser/session transport used by source fetchers
// 4) service.go: source selection, cache policy, parse orchestration
package actressranking

// Public ranking contracts and service-local state live here so source
// parsers, cache persistence, and bridge callers share one small vocabulary.

import (
	"strings"
	"sync"
)

type rankingError struct {
	message string
	code    string
}

func (e *rankingError) Error() string {
	return e.message
}

func createRankingError(message string, code string) error {
	return &rankingError{message: strings.TrimSpace(message), code: strings.TrimSpace(code)}
}

type Options struct {
	Mode               string
	Year               int
	Month              int
	Source             string
	Proxy              string
	ForceRefresh       bool
	CacheFilePath      string
	HistoryDirectories []string
}

type RankingItem struct {
	Rank        int    `json:"rank"`
	ActressName string `json:"actressName"`
	ProfileURL  string `json:"profileUrl,omitempty"`
	ImageURL    string `json:"imageUrl,omitempty"`
	LatestTitle string `json:"latestTitle,omitempty"`
	LatestURL   string `json:"latestUrl,omitempty"`
	WorksCount  *int   `json:"worksCount,omitempty"`
}

type Result struct {
	Title                string        `json:"title"`
	SourceName           string        `json:"sourceName"`
	OriginSourceName     string        `json:"originSourceName,omitempty"`
	SourceURL            string        `json:"sourceUrl,omitempty"`
	Mode                 string        `json:"mode"`
	RequestedSource      string        `json:"requestedSource,omitempty"`
	RequestedSourceLabel string        `json:"requestedSourceLabel,omitempty"`
	ResolvedSource       string        `json:"resolvedSource,omitempty"`
	ResolvedSourceLabel  string        `json:"resolvedSourceLabel,omitempty"`
	PeriodLabel          string        `json:"periodLabel"`
	PeriodYear           int           `json:"periodYear"`
	PeriodMonth          int           `json:"periodMonth"`
	Total                int           `json:"total"`
	AvailableYears       []int         `json:"availableYears"`
	AvailableMonths      []int         `json:"availableMonths"`
	FetchedAt            string        `json:"fetchedAt"`
	Items                []RankingItem `json:"items"`
	FromCache            bool          `json:"fromCache"`
	Stale                bool          `json:"stale"`
	Notice               string        `json:"notice,omitempty"`
	ErrorMessage         string        `json:"errorMessage,omitempty"`
	FallbackUsed         bool          `json:"fallbackUsed"`
}

type cacheEntry struct {
	CachedAt string `json:"cachedAt"`
	Data     Result `json:"data"`
}

type sourceCache struct {
	MonthlyLatestKey string                `json:"monthlyLatestKey"`
	MonthlyByPeriod  map[string]cacheEntry `json:"monthlyByPeriod"`
	AnnualByYear     map[string]cacheEntry `json:"annualByYear"`
	AvailableYears   []int                 `json:"availableYears"`
}

type cacheFile struct {
	Version int                    `json:"version"`
	Sources map[string]sourceCache `json:"sources"`
}

type cachedMonthly struct {
	BucketID string
	Key      string
	Year     int
	Month    int
	CachedAt string
	Entry    cacheEntry
}

type cachedAnnual struct {
	BucketID string
	Year     int
	CachedAt string
	Entry    cacheEntry
}

type rankingContext struct {
	RequestedChannel string
	Mode             string
	Year             int
	Month            int
	ForceRefresh     bool
	Proxy            string
	Cache            cacheFile
	CacheFilePath    string
}

// Service is the public ranking facade. cacheMu protects only the short cache
// commit section, leaving slow remote fetches free to run concurrently.
type Service struct {
	browser *browserService
	cacheMu sync.Mutex
}

func NewService() *Service {
	return &Service{browser: newBrowserService()}
}
