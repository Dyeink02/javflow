// Package avsubscription is the current Go-native AV subscription domain.
//
// It intentionally separates:
// 1) artifact import from persisted crawl output
// 2) later remote refresh against saved actress targets
//
// It must not depend on live JAV crawler UI state. That boundary is what keeps
// subscription evolution from repeatedly breaking the crawler workflow.
//
// Ownership summary:
// 1) expose the persisted AV subscription state-management facade
// 2) keep artifact import and remote refresh as independent strategies behind it
// 3) present one stable contract to bridge/UI callers
//
// File map for maintainers:
// 1) facade-level contracts and defaults
// 2) persisted subscription state-management methods
// 3) artifact import entrypoints delegated to scan/persistence helpers
// 4) remote refresh entrypoints delegated to target/fetch helpers
package avsubscription

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"javflow/internal/contracts/crawlartifact"
	"javflow/internal/contracts/subscriptiontarget"
	runtimepaths "javflow/internal/runtime"
)

const defaultItemsPerPage = 30

// Service is the avsubscription facade.
//
// There are two independent paths behind this facade:
// 1) artifact import from persisted crawl output such as filmData.json
// 2) remote target refresh based on saved subscription URLs
//
// Public callers should stay here, while persistence and artifact-import
// scanning live in dedicated files.
//
// Maintenance rule:
//   - keep state storage semantics stable here
//   - keep artifact import independent from remote refresh logic
//   - do not let subscription behavior depend on live crawl UI state
//   - if subscription data looks wrong, inspect the persistence/scan files
//     before touching the bridge or crawler flow
type Service struct {
	paths runtimepaths.Paths
	mu    sync.Mutex
}

type Subscription struct {
	ID            string   `json:"id"`
	ActressName   string   `json:"actressName"`
	CrawlURL      string   `json:"crawlUrl"`
	PreferredBase string   `json:"preferredBase,omitempty"`
	LatestItemURL string   `json:"latestItemUrl,omitempty"`
	BaselineCodes []string `json:"baselineCodes,omitempty"`
	Source        string   `json:"source"`
	SyncedCount   int      `json:"syncedCount"`
	CurrentCount  int      `json:"currentCount"`
	PendingCount  int      `json:"pendingCount"`
	ItemsPerPage  int      `json:"itemsPerPage"`
	TotalPages    int      `json:"totalPages"`
	Status        string   `json:"status"`
	LastCheckedAt string   `json:"lastCheckedAt,omitempty"`
	LastSyncedAt  string   `json:"lastSyncedAt,omitempty"`
	LastUpdatedAt string   `json:"lastUpdatedAt,omitempty"`
	LastError     string   `json:"lastError,omitempty"`
}

type TargetProfile = subscriptiontarget.TargetProfile

type ScanResult struct {
	OutputDir          string                                `json:"outputDir"`
	FilmDataPath       string                                `json:"filmDataPath"`
	CrawlProfilePath   string                                `json:"crawlProfilePath,omitempty"`
	SourceType         string                                `json:"sourceType"`
	AddedCount         int                                   `json:"addedCount"`
	UpdatedCount       int                                   `json:"updatedCount"`
	SubscriptionCount  int                                   `json:"subscriptionCount"`
	ScannedActresses   int                                   `json:"scannedActresses"`
	Subscriptions      []Subscription                        `json:"subscriptions"`
	ScannedActressList []string                              `json:"scannedActressList"`
	FilteredCodes      []crawlartifact.FilteredFilmCodeEntry `json:"filteredCodes,omitempty"`
}

func NewService(paths runtimepaths.Paths) *Service {
	return &Service{paths: paths}
}

// List, Upsert, ReplaceAll, Remove, Clear, and MarkSynced are the persisted
// subscription-state management surface. They should stay independent from the
// artifact-import path and from the remote refresh path so future decoupling
// can move those strategies without changing basic state storage semantics.
func (s *Service) List() ([]Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.loadLocked()
}

func (s *Service) Upsert(next Subscription) (Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items, err := s.loadLocked()
	if err != nil {
		return Subscription{}, err
	}

	now := time.Now().Format(time.RFC3339)
	normalized := normalizeSubscription(next, now)
	normalized.LastUpdatedAt = now
	if normalized.LastCheckedAt == "" {
		normalized.LastCheckedAt = now
	}

	matchIndex := findSubscriptionIndex(items, normalized)
	if matchIndex >= 0 {
		current := items[matchIndex]
		if normalized.Source == "" {
			normalized.Source = current.Source
		}
		if normalized.CrawlURL == "" {
			normalized.CrawlURL = current.CrawlURL
		}
		if normalized.PreferredBase == "" {
			normalized.PreferredBase = current.PreferredBase
		}
		if normalized.ItemsPerPage <= 0 {
			normalized.ItemsPerPage = current.ItemsPerPage
		}
		if normalized.SyncedCount == 0 && current.SyncedCount > 0 {
			normalized.SyncedCount = current.SyncedCount
		}
		if normalized.CurrentCount == 0 && current.CurrentCount > 0 {
			normalized.CurrentCount = current.CurrentCount
		}
		if normalized.LastSyncedAt == "" {
			normalized.LastSyncedAt = current.LastSyncedAt
		}
		if normalized.LastCheckedAt == "" {
			normalized.LastCheckedAt = current.LastCheckedAt
		}
		if normalized.LastError == "" {
			normalized.LastError = current.LastError
		}
		items[matchIndex] = normalizeSubscription(normalized, now)
	} else {
		items = append(items, normalized)
	}

	sortSubscriptionsByPriority(items)
	if err := s.saveLocked(items); err != nil {
		return Subscription{}, err
	}

	savedIndex := findSubscriptionIndex(items, normalized)
	if savedIndex >= 0 {
		return items[savedIndex], nil
	}
	return normalized, nil
}

func (s *Service) ReplaceAll(items []Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Format(time.RFC3339)
	normalized := make([]Subscription, 0, len(items))
	for _, item := range items {
		normalized = append(normalized, normalizeSubscription(item, now))
	}
	sortSubscriptionsByPriority(normalized)
	return s.saveLocked(normalized)
}

func (s *Service) Remove(id string) ([]Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items, err := s.loadLocked()
	if err != nil {
		return nil, err
	}

	filtered := make([]Subscription, 0, len(items))
	for _, item := range items {
		if item.ID == strings.TrimSpace(id) {
			continue
		}
		filtered = append(filtered, item)
	}

	sortSubscriptionsByPriority(filtered)
	if err := s.saveLocked(filtered); err != nil {
		return nil, err
	}
	return filtered, nil
}

func (s *Service) Clear() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items, err := s.loadLocked()
	if err != nil {
		return 0, err
	}

	if err := s.saveLocked([]Subscription{}); err != nil {
		return 0, err
	}
	return len(items), nil
}

func (s *Service) MarkSynced(id string) (Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items, err := s.loadLocked()
	if err != nil {
		return Subscription{}, err
	}

	now := time.Now().Format(time.RFC3339)
	for index, item := range items {
		if item.ID != strings.TrimSpace(id) {
			continue
		}
		item.SyncedCount = maxInt(item.SyncedCount, item.CurrentCount)
		item.LastSyncedAt = now
		item.LastUpdatedAt = now
		item.LastError = ""
		items[index] = normalizeSubscription(item, now)
		sortSubscriptionsByPriority(items)
		if err := s.saveLocked(items); err != nil {
			return Subscription{}, err
		}
		return items[findSubscriptionIndexByID(items, item.ID)], nil
	}

	return Subscription{}, fmt.Errorf("\u672a\u627e\u5230\u8ba2\u9605\uff1a%s", id)
}

// MarkSubscriptionCrawlCompleted merges the newest crawled baseline codes from
// one subscription-side crawl output, then marks the subscription synced.
func (s *Service) MarkSubscriptionCrawlCompleted(id string, outputDir string) (Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items, err := s.loadLocked()
	if err != nil {
		return Subscription{}, err
	}

	nextBaseline := []string{}
	if strings.TrimSpace(outputDir) != "" {
		if _, records, readErr := crawlartifact.ReadFilmDataRecordsWithUserData(outputDir, s.paths.UserData); readErr == nil {
			for index, record := range records {
				identity := extractRecordIdentity(record, index)
				code := normalizeFilmCode(identity)
				if code == "" {
					code = normalizeFilmCode(recordFieldText(record["title"]))
				}
				if code == "" {
					code = normalizeFilmCode(recordFieldText(record["sourceLink"]))
				}
				if code != "" {
					nextBaseline = append(nextBaseline, code)
				}
			}
		}
	}

	now := time.Now().Format(time.RFC3339)
	for index, item := range items {
		if item.ID != strings.TrimSpace(id) {
			continue
		}
		mergedBaseline := append([]string{}, item.BaselineCodes...)
		mergedBaseline = append(mergedBaseline, nextBaseline...)
		item.BaselineCodes = normalizeBaselineCodes(mergedBaseline)
		item.SyncedCount = maxInt(item.SyncedCount, maxInt(item.CurrentCount, len(item.BaselineCodes)))
		item.CurrentCount = maxInt(item.CurrentCount, item.SyncedCount)
		item.LastSyncedAt = now
		item.LastUpdatedAt = now
		item.LastError = ""
		items[index] = normalizeSubscription(item, now)
		sortSubscriptionsByPriority(items)
		if err := s.saveLocked(items); err != nil {
			return Subscription{}, err
		}
		return items[findSubscriptionIndexByID(items, item.ID)], nil
	}

	return Subscription{}, fmt.Errorf("未找到订阅：%s", id)
}

// ScanOutput imports subscription candidates from persisted crawl artifacts
// only. It should remain independent from the remote subscription refresh path.
func (s *Service) ScanOutput(outputDir string) (ScanResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Public scan entry stays intentionally narrow: normalize the user-supplied
	// artifact root once, then delegate the actual import strategy to scan_output.
	return s.scanOutputLocked(strings.TrimSpace(outputDir))
}

// normalizeSubscription is the single place that derives pending counts,
// pages, ids, and status fields from stored subscription data. When debugging
// inconsistent subscription counters, start here before checking UI code.
func normalizeSubscription(item Subscription, now string) Subscription {
	// This normalization function is intentionally the one place that derives
	// counters/status/pages. UI bugs around pending counts should be checked here
	// before changing renderer formatting or persistence code.
	item.ActressName = strings.TrimSpace(item.ActressName)
	item.CrawlURL = strings.TrimSpace(item.CrawlURL)
	item.PreferredBase = strings.TrimSpace(item.PreferredBase)
	item.LatestItemURL = strings.TrimSpace(item.LatestItemURL)
	item.BaselineCodes = normalizeBaselineCodes(item.BaselineCodes)
	item.Source = normalizeSource(item.Source)
	item.ItemsPerPage = maxInt(defaultItemsPerPage, item.ItemsPerPage)
	item.SyncedCount = maxInt(0, item.SyncedCount)
	item.CurrentCount = maxInt(item.SyncedCount, item.CurrentCount)
	item.PendingCount = maxInt(0, item.CurrentCount-item.SyncedCount)
	item.TotalPages = calcPages(maxInt(item.PendingCount, item.CurrentCount), item.ItemsPerPage)
	if item.ID == "" {
		item.ID = buildSubscriptionIdentityHash(item.ActressName, item.CrawlURL)
	}
	if item.LastUpdatedAt == "" {
		item.LastUpdatedAt = now
	}
	if item.PendingCount > 0 {
		item.Status = statusUpdated
	} else if strings.TrimSpace(item.LastError) != "" {
		item.Status = statusError
	} else {
		item.Status = statusIdle
	}
	return item
}

func normalizeSource(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case sourceManual, sourceScan:
		return strings.TrimSpace(strings.ToLower(value))
	default:
		return sourceManual
	}
}

func findSubscriptionIndex(items []Subscription, next Subscription) int {
	if next.ID != "" {
		if index := findSubscriptionIndexByID(items, next.ID); index >= 0 {
			return index
		}
	}

	nextName := normalizeName(next.ActressName)
	nextURL := strings.ToLower(strings.TrimSpace(next.CrawlURL))
	for index, item := range items {
		if nextName != "" && normalizeName(item.ActressName) == nextName {
			return index
		}
		if nextURL != "" && strings.ToLower(strings.TrimSpace(item.CrawlURL)) == nextURL {
			return index
		}
	}
	return -1
}

func normalizeBaselineCodes(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}

	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		normalized := normalizeFilmCode(value)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func findSubscriptionIndexByID(items []Subscription, id string) int {
	for index, item := range items {
		if item.ID == strings.TrimSpace(id) {
			return index
		}
	}
	return -1
}

func calcPages(total int, itemsPerPage int) int {
	if total <= 0 {
		return 1
	}
	if itemsPerPage <= 0 {
		itemsPerPage = defaultItemsPerPage
	}
	pages := total / itemsPerPage
	if total%itemsPerPage != 0 {
		pages++
	}
	if pages < 1 {
		return 1
	}
	return pages
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func normalizeName(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), ""))
}
