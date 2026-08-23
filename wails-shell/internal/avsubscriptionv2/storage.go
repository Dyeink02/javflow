// Ownership summary:
//   This file implements domain logic for the javflow backend.
//
// File map for maintainers:
//   1) Domain-specific types and helpers.
//   2) Internal service implementation.
//

package avsubscriptionv2

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"javflow/internal/common"
	"javflow/internal/crawlfetch"
	runtimepaths "javflow/internal/runtime"
)

const (
	storageDirName  = "subscriptions-v2"
	storageFileName = "av-subscriptions-v2.json"
)

// Service is the AV-subscription V2 facade.
//
// The service exposes one stable surface for:
// 1) import/manual baseline creation
// 2) refresh diff detection
// 3) persisted state mutation
type Service struct {
	paths     runtimepaths.Paths
	fetch     *crawlfetch.Service
	storageMu sync.Mutex
}

func NewService(paths runtimepaths.Paths, fetch *crawlfetch.Service) *Service {
	return &Service{paths: paths, fetch: fetch}
}

func (s *Service) storagePath() string {
	return filepath.Join(s.paths.UserData, storageDirName, storageFileName)
}

func (s *Service) load() ([]Subscription, error) {
	payload, err := os.ReadFile(s.storagePath())
	if err != nil {
		if os.IsNotExist(err) {
			return []Subscription{}, nil
		}
		return nil, err
	}

	items := []Subscription{}
	if err := json.Unmarshal(payload, &items); err != nil {
		backupPath, backupErr := common.PreserveCorruptFile(s.storagePath())
		if backupErr != nil {
			return nil, fmt.Errorf("subscription state is unreadable and could not be preserved: %w", backupErr)
		}
		return nil, fmt.Errorf("subscription state is unreadable; preserved original file at %s", backupPath)
	}

	now := time.Now().Format(time.RFC3339)
	normalized := make([]Subscription, 0, len(items))
	for _, item := range items {
		normalized = append(normalized, normalizeSubscription(item, now))
	}
	ensureSortOrders(normalized)
	sortSubscriptions(normalized)
	return normalized, nil
}

func (s *Service) save(items []Subscription) error {
	ensureSortOrders(items)
	sortSubscriptions(items)
	payload, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	return common.WriteFileAtomic(s.storagePath(), payload, 0o644)
}

// ApplyRefreshResults merges a completed refresh into the latest persisted
// list. Network scans intentionally happen without storageMu; reading the
// current list again here prevents their stale pre-scan snapshot from erasing
// a user's concurrent add, remove, reorder, media update, or crawl handoff.
func (s *Service) ApplyRefreshResults(results []Subscription) ([]Subscription, error) {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()

	items, err := s.load()
	if err != nil {
		return nil, err
	}
	now := time.Now().Format(time.RFC3339)
	for _, refreshed := range results {
		index := findSubscriptionIndex(items, refreshed)
		if index < 0 {
			// The user may have removed this subscription while it was being
			// refreshed. Do not resurrect it from a stale network result.
			continue
		}
		current := items[index]
		merged := mergeRefreshResult(current, refreshed, now)
		items[index] = normalizeSubscription(merged, now)
	}
	ensureSortOrders(items)
	if err := s.save(items); err != nil {
		return nil, err
	}
	return append([]Subscription(nil), items...), nil
}

func (s *Service) List() ([]Subscription, error) {
	return s.load()
}

func (s *Service) Upsert(next Subscription) (Subscription, error) {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	items, err := s.load()
	if err != nil {
		return Subscription{}, err
	}

	now := time.Now().Format(time.RFC3339)
	next = normalizeSubscription(next, now)
	index := findSubscriptionIndex(items, next)
	if index >= 0 {
		current := items[index]
		next = mergeSubscriptionState(current, next, now)
		items[index] = normalizeSubscription(next, now)
	} else {
		if next.SortOrder <= 0 {
			next.SortOrder = nextSortOrder(items)
		}
		items = append(items, normalizeSubscription(next, now))
	}

	sortSubscriptions(items)
	if err := s.save(items); err != nil {
		return Subscription{}, err
	}

	savedIndex := findSubscriptionIndex(items, next)
	if savedIndex >= 0 {
		return items[savedIndex], nil
	}
	return next, nil
}

func (s *Service) ReplaceAll(items []Subscription) error {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	now := time.Now().Format(time.RFC3339)
	normalized := make([]Subscription, 0, len(items))
	for _, item := range items {
		normalized = append(normalized, normalizeSubscription(item, now))
	}
	sortSubscriptions(normalized)
	return s.save(normalized)
}

func (s *Service) Remove(id string) ([]Subscription, error) {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	items, err := s.load()
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
	sortSubscriptions(filtered)
	if err := s.save(filtered); err != nil {
		return nil, err
	}
	return filtered, nil
}

func (s *Service) Clear() (int, error) {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	items, err := s.load()
	if err != nil {
		return 0, err
	}
	if err := s.save([]Subscription{}); err != nil {
		return 0, err
	}
	return len(items), nil
}

func (s *Service) MarkSynced(id string) (Subscription, error) {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	items, err := s.load()
	if err != nil {
		return Subscription{}, err
	}

	now := time.Now().Format(time.RFC3339)
	for index, item := range items {
		if item.ID != strings.TrimSpace(id) {
			continue
		}
		merged := append([]string{}, item.BaselineCodes...)
		merged = append(merged, item.PendingCodes...)
		item.BaselineCodes = normalizeCodes(merged)
		item.BaselineCount = len(item.BaselineCodes)
		item.CurrentObservedCount = maxInt(item.CurrentObservedCount, item.BaselineCount)
		item.PendingCodes = []string{}
		item.PendingCount = 0
		item.LastCrawlAt = now
		item.LastUpdatedAt = now
		item.LastError = ""
		item.Status = statusIdle
		items[index] = normalizeSubscription(item, now)
		sortSubscriptions(items)
		if err := s.save(items); err != nil {
			return Subscription{}, err
		}
		return items[findSubscriptionIndexByID(items, item.ID)], nil
	}

	return Subscription{}, os.ErrNotExist
}

// Patch applies partial updates to a subscription (e.g. correcting baseline count or URL).
func (s *Service) Patch(id string, patch map[string]any) (Subscription, error) {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	items, err := s.load()
	if err != nil {
		return Subscription{}, err
	}

	now := time.Now().Format(time.RFC3339)
	for index, item := range items {
		if item.ID != strings.TrimSpace(id) {
			continue
		}
		if v, ok := patch["actressName"].(string); ok && strings.TrimSpace(v) != "" {
			item.ActressName = strings.TrimSpace(v)
		}
		if v, ok := patch["crawlUrl"].(string); ok && strings.TrimSpace(v) != "" {
			item.CrawlURL = strings.TrimSpace(v)
		}
		if v, ok := patch["preferredBase"].(string); ok && strings.TrimSpace(v) != "" {
			item.PreferredBase = strings.TrimSpace(v)
		}
		if v, ok := patch["itemsPerPage"]; ok {
			if n := toInt(v); n > 0 {
				item.ItemsPerPage = n
			}
		}
		if v, ok := patch["actressCountFilterThreshold"]; ok {
			item.ActressCountFilterThreshold = maxInt(0, toInt(v))
		}
		if v, ok := patch["totalPages"]; ok {
			if n := toInt(v); n > 0 {
				item.TotalPages = n
			}
		}
		if v, ok := patch["preferredOutputDir"].(string); ok {
			item.PreferredOutputDir = strings.TrimSpace(v)
		}
		item.LastUpdatedAt = now
		items[index] = normalizeSubscription(item, now)
		sortSubscriptions(items)
		if err := s.save(items); err != nil {
			return Subscription{}, err
		}
		return items[findSubscriptionIndexByID(items, item.ID)], nil
	}

	return Subscription{}, os.ErrNotExist
}

// PatchActressCountFilterThresholdForAll applies one actor-count filter to
// every subscription and saves the collection once, keeping the bulk UI
// action atomic from the user's perspective.
func (s *Service) PatchActressCountFilterThresholdForAll(threshold int) ([]Subscription, error) {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	items, err := s.load()
	if err != nil {
		return nil, err
	}

	now := time.Now().Format(time.RFC3339)
	for index, item := range items {
		item.ActressCountFilterThreshold = maxInt(0, threshold)
		item.LastUpdatedAt = now
		items[index] = normalizeSubscription(item, now)
	}
	sortSubscriptions(items)
	if err := s.save(items); err != nil {
		return nil, err
	}
	return items, nil
}

// Reorder persists the exact user-facing order. Unknown IDs are ignored and
// any subscriptions omitted by the caller are appended in their current order.
func (s *Service) Reorder(orderedIDs []string) ([]Subscription, error) {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	items, err := s.load()
	if err != nil {
		return nil, err
	}

	byID := make(map[string]Subscription, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}

	reordered := make([]Subscription, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, rawID := range orderedIDs {
		id := strings.TrimSpace(rawID)
		item, ok := byID[id]
		if !ok {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		reordered = append(reordered, item)
	}
	for _, item := range items {
		if _, exists := seen[item.ID]; exists {
			continue
		}
		reordered = append(reordered, item)
	}
	for index := range reordered {
		reordered[index].SortOrder = index + 1
	}
	if err := s.save(reordered); err != nil {
		return nil, err
	}
	return reordered, nil
}

// SetMedia records locally cached actress media without changing crawl state.
func (s *Service) SetMedia(id string, avatarURL string, photoURLs []string, checkedAt string) (Subscription, error) {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	items, err := s.load()
	if err != nil {
		return Subscription{}, err
	}

	now := time.Now().Format(time.RFC3339)
	for index, item := range items {
		if item.ID != strings.TrimSpace(id) {
			continue
		}
		item.AvatarURL = strings.TrimSpace(avatarURL)
		item.PhotoURLs = normalizeMediaURLs(photoURLs)
		item.MediaUpdatedAt = ensureTimestamp(checkedAt, now)
		item.LastUpdatedAt = now
		items[index] = normalizeSubscription(item, now)
		if err := s.save(items); err != nil {
			return Subscription{}, err
		}
		return items[findSubscriptionIndexByID(items, item.ID)], nil
	}

	return Subscription{}, os.ErrNotExist
}

func toInt(v any) int {
	switch val := v.(type) {
	case int:
		return val
	case float64:
		return int(val)
	case float32:
		return int(val)
	case int64:
		return int(val)
	default:
		return 0
	}
}

func (s *Service) MarkCrawlCompleted(id string, outputDir string) (Subscription, error) {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	items, err := s.load()
	if err != nil {
		return Subscription{}, err
	}

	nextBaseline := extractCodesFromOutput(outputDir, s.paths.UserData)
	now := time.Now().Format(time.RFC3339)
	for index, item := range items {
		if item.ID != strings.TrimSpace(id) {
			continue
		}
		merged := append([]string{}, item.BaselineCodes...)
		merged = append(merged, nextBaseline...)
		item.BaselineCodes = normalizeCodes(merged)
		item.BaselineCount = len(item.BaselineCodes)
		item.CurrentObservedCount = maxInt(item.CurrentObservedCount, item.BaselineCount)
		item.PendingCodes = diffCodes(item.PendingCodes, nextBaseline)
		item.PendingCount = len(item.PendingCodes)
		item.LastCrawlAt = now
		item.LastUpdatedAt = now
		item.PreferredOutputDir = strings.TrimSpace(outputDir)
		item.LastCrawlOutputDir = strings.TrimSpace(outputDir)
		item.LastError = ""
		if item.PendingCount > 0 {
			item.Status = statusUpdated
		} else {
			item.Status = statusIdle
		}
		items[index] = normalizeSubscription(item, now)
		sortSubscriptions(items)
		if err := s.save(items); err != nil {
			return Subscription{}, err
		}
		return items[findSubscriptionIndexByID(items, item.ID)], nil
	}

	return Subscription{}, os.ErrNotExist
}

func buildSubscriptionIdentityHash(actressName string, crawlURL string) string {
	key := normalizeName(actressName)
	if key == "" {
		key = strings.ToLower(strings.TrimSpace(crawlURL))
	}
	if key == "" {
		key = time.Now().UTC().Format(time.RFC3339Nano)
	}
	sum := sha1.Sum([]byte(key))
	return hex.EncodeToString(sum[:8])
}

func sortSubscriptions(items []Subscription) {
	sort.SliceStable(items, func(i int, j int) bool {
		if items[i].SortOrder > 0 || items[j].SortOrder > 0 {
			if items[i].SortOrder <= 0 {
				return false
			}
			if items[j].SortOrder <= 0 {
				return true
			}
			if items[i].SortOrder != items[j].SortOrder {
				return items[i].SortOrder < items[j].SortOrder
			}
		}
		if items[i].PendingCount != items[j].PendingCount {
			return items[i].PendingCount > items[j].PendingCount
		}
		if strings.TrimSpace(items[i].LastUpdatedAt) != strings.TrimSpace(items[j].LastUpdatedAt) {
			return strings.TrimSpace(items[i].LastUpdatedAt) > strings.TrimSpace(items[j].LastUpdatedAt)
		}
		if normalizeName(items[i].ActressName) != normalizeName(items[j].ActressName) {
			return normalizeName(items[i].ActressName) < normalizeName(items[j].ActressName)
		}
		return items[i].ID < items[j].ID
	})
}

func normalizeSubscription(item Subscription, now string) Subscription {
	// Older V2 records may have only counts (no per-film code arrays). Preserve
	// those values during lazy normalization so opening the newer subscription
	// page never makes an existing baseline or pending badge appear as zero.
	persistedBaselineCount := maxInt(0, item.BaselineCount)
	persistedPendingCount := maxInt(0, item.PendingCount)
	item.ID = strings.TrimSpace(item.ID)
	item.ActressName = strings.TrimSpace(item.ActressName)
	item.CrawlURL = strings.TrimSpace(item.CrawlURL)
	item.PreferredBase = strings.TrimSpace(item.PreferredBase)
	item.SourceType = normalizeSourceType(item.SourceType)
	item.BaselineCodes = normalizeCodes(item.BaselineCodes)
	item.PendingCodes = normalizeCodes(item.PendingCodes)
	if len(item.BaselineCodes) > 0 {
		item.BaselineCount = len(item.BaselineCodes)
	} else {
		item.BaselineCount = persistedBaselineCount
	}
	item.PendingCodes = diffCodes(item.PendingCodes, item.BaselineCodes)
	if len(item.PendingCodes) > 0 {
		item.PendingCount = len(item.PendingCodes)
	} else {
		item.PendingCount = persistedPendingCount
	}
	item.ActressCountFilterThreshold = maxInt(0, item.ActressCountFilterThreshold)
	item.ItemsPerPage = maxInt(defaultItemsPerPage, item.ItemsPerPage)
	if item.CurrentObservedCount < item.BaselineCount {
		item.CurrentObservedCount = item.BaselineCount
	}
	if item.CurrentObservedCount < item.BaselineCount+item.PendingCount {
		item.CurrentObservedCount = item.BaselineCount + item.PendingCount
	}
	if item.ManualDeclaredTotal > 0 && item.CurrentObservedCount < item.ManualDeclaredTotal {
		item.CurrentObservedCount = item.ManualDeclaredTotal
	}
	if item.CurrentTotal <= 0 {
		if item.CurrentObservedCount > 0 {
			item.CurrentTotal = item.CurrentObservedCount
		} else {
			item.CurrentTotal = item.BaselineCount
		}
	}
	if item.TotalPages <= 0 {
		item.TotalPages = calcPages(maxInt(item.CurrentObservedCount, item.BaselineCount), item.ItemsPerPage)
	}
	item.LatestItemURL = strings.TrimSpace(item.LatestItemURL)
	item.PreferredOutputDir = strings.TrimSpace(item.PreferredOutputDir)
	item.LastCrawlOutputDir = strings.TrimSpace(item.LastCrawlOutputDir)
	item.AvatarURL = normalizeMediaURL(item.AvatarURL)
	item.PhotoURLs = normalizeMediaURLs(item.PhotoURLs)
	item.MediaUpdatedAt = strings.TrimSpace(item.MediaUpdatedAt)
	item.CreatedAt = ensureTimestamp(item.CreatedAt, now)
	item.BaselineSnapshotAt = ensureTimestamp(item.BaselineSnapshotAt, now)
	item.LastUpdatedAt = ensureTimestamp(item.LastUpdatedAt, now)
	item.LastCheckedAt = strings.TrimSpace(item.LastCheckedAt)
	item.LastUpdateDetectedAt = strings.TrimSpace(item.LastUpdateDetectedAt)
	item.LastCrawlAt = strings.TrimSpace(item.LastCrawlAt)
	item.LastError = strings.TrimSpace(item.LastError)
	if item.ID == "" {
		item.ID = buildSubscriptionIdentityHash(item.ActressName, item.CrawlURL)
	}
	if item.PendingCount > 0 {
		item.Status = statusUpdated
	} else if item.LastError != "" {
		item.Status = statusError
	} else if strings.TrimSpace(item.Status) == statusRunning {
		item.Status = statusRunning
	} else {
		item.Status = statusIdle
	}
	return item
}

func normalizeSourceType(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case sourceTypeCrawlImport:
		return sourceTypeCrawlImport
	case sourceTypeMetadataAuto:
		return sourceTypeMetadataAuto
	default:
		return sourceTypeManual
	}
}

func ensureTimestamp(value string, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(fallback)
}

func mergeSubscriptionState(current Subscription, next Subscription, now string) Subscription {
	if next.ID == "" {
		next.ID = current.ID
	}
	if current.CrawlURL != "" {
		next.CrawlURL = current.CrawlURL
	}
	if current.PreferredBase != "" {
		next.PreferredBase = current.PreferredBase
	}
	if next.PreferredOutputDir == "" {
		next.PreferredOutputDir = current.PreferredOutputDir
	}
	if next.CreatedAt == "" {
		next.CreatedAt = current.CreatedAt
	}
	if next.BaselineSnapshotAt == "" {
		next.BaselineSnapshotAt = current.BaselineSnapshotAt
	}
	if next.LastCrawlAt == "" {
		next.LastCrawlAt = current.LastCrawlAt
	}
	if next.LastCrawlOutputDir == "" {
		next.LastCrawlOutputDir = current.LastCrawlOutputDir
	}
	if next.SortOrder <= 0 {
		next.SortOrder = current.SortOrder
	}
	if next.AvatarURL == "" {
		next.AvatarURL = current.AvatarURL
	}
	if len(next.PhotoURLs) == 0 {
		next.PhotoURLs = current.PhotoURLs
	}
	if next.MediaUpdatedAt == "" {
		next.MediaUpdatedAt = current.MediaUpdatedAt
	}
	if next.ManualDeclaredTotal == 0 {
		next.ManualDeclaredTotal = current.ManualDeclaredTotal
	}
	if next.ManualDeclaredPages == 0 {
		next.ManualDeclaredPages = current.ManualDeclaredPages
	}
	if next.ManualDeclaredPerPage == 0 {
		next.ManualDeclaredPerPage = current.ManualDeclaredPerPage
	}
	if next.ActressCountFilterThreshold == 0 {
		next.ActressCountFilterThreshold = current.ActressCountFilterThreshold
	}
	if len(next.BaselineCodes) == 0 {
		next.BaselineCodes = current.BaselineCodes
	} else {
		next.BaselineCodes = normalizeCodes(append(append([]string{}, current.BaselineCodes...), next.BaselineCodes...))
	}
	if len(next.PendingCodes) == 0 && len(current.PendingCodes) > 0 {
		next.PendingCodes = current.PendingCodes
	}
	if next.CurrentObservedCount < current.CurrentObservedCount {
		next.CurrentObservedCount = current.CurrentObservedCount
	}
	if next.CurrentTotal <= 0 {
		next.CurrentTotal = current.CurrentTotal
	}
	if next.LastCheckedAt == "" {
		next.LastCheckedAt = current.LastCheckedAt
	}
	if next.LastUpdateDetectedAt == "" {
		next.LastUpdateDetectedAt = current.LastUpdateDetectedAt
	}
	if next.LastError == "" {
		next.LastError = current.LastError
	}
	next.LastUpdatedAt = now
	return next
}

// mergeRefreshResult owns the narrow refresh-write contract. User-owned
// fields (target, order, local media, output preference, and crawl baseline)
// come from the latest storage record; remote-observed counts and refresh
// diagnostics come from the completed scan.
func mergeRefreshResult(current Subscription, refreshed Subscription, now string) Subscription {
	merged := current
	merged.CurrentObservedCount = refreshed.CurrentObservedCount
	merged.CurrentTotal = refreshed.CurrentTotal
	merged.PendingCodes = normalizeCodes(refreshed.PendingCodes)
	merged.PendingCount = len(merged.PendingCodes)
	merged.ItemsPerPage = refreshed.ItemsPerPage
	merged.TotalPages = refreshed.TotalPages
	merged.LastScanPages = refreshed.LastScanPages
	merged.LastStoppedOnPage = refreshed.LastStoppedOnPage
	merged.LatestItemURL = refreshed.LatestItemURL
	merged.LastCheckedAt = refreshed.LastCheckedAt
	merged.LastUpdateDetectedAt = refreshed.LastUpdateDetectedAt
	if merged.LastUpdateDetectedAt == "" {
		merged.LastUpdateDetectedAt = current.LastUpdateDetectedAt
	}
	merged.LastError = refreshed.LastError
	merged.Status = refreshed.Status
	merged.LastUpdatedAt = now
	return merged
}

func nextSortOrder(items []Subscription) int {
	maxOrder := 0
	for _, item := range items {
		if item.SortOrder > maxOrder {
			maxOrder = item.SortOrder
		}
	}
	if maxOrder == 0 {
		maxOrder = len(items)
	}
	return maxOrder + 1
}

func ensureSortOrders(items []Subscription) {
	if len(items) == 0 {
		return
	}
	hasExplicitOrder := false
	for _, item := range items {
		if item.SortOrder > 0 {
			hasExplicitOrder = true
			break
		}
	}
	if !hasExplicitOrder {
		// Preserve the legacy pending-count ordering as the initial user order.
		for index := range items {
			items[index].SortOrder = 0
		}
		sortSubscriptions(items)
		for index := range items {
			items[index].SortOrder = index + 1
		}
		return
	}

	sortSubscriptions(items)
	for index := range items {
		items[index].SortOrder = index + 1
	}
}

func normalizeMediaURLs(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := normalizeMediaURL(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func normalizeMediaURL(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.HasPrefix(trimmed, "/subscription-media/") {
		return trimmed
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || !strings.EqualFold(parsed.Scheme, "file") {
		return trimmed
	}
	pathValue, err := url.PathUnescape(parsed.Path)
	if err != nil {
		return trimmed
	}
	slashed := filepath.ToSlash(pathValue)
	lower := strings.ToLower(slashed)
	marker := "/subscriptions-v2/media/"
	markerIndex := strings.Index(lower, marker)
	if markerIndex < 0 {
		return trimmed
	}
	relative := strings.TrimPrefix(slashed[markerIndex+len(marker):], "/")
	if relative == "" {
		return trimmed
	}
	return "/subscription-media/" + relative
}

func findSubscriptionIndex(items []Subscription, next Subscription) int {
	if strings.TrimSpace(next.ID) != "" {
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

func findSubscriptionIndexByID(items []Subscription, id string) int {
	for index, item := range items {
		if item.ID == strings.TrimSpace(id) {
			return index
		}
	}
	return -1
}
