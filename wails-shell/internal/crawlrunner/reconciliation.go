package crawlrunner

import (
	"sort"
	"strings"
	"sync"
)

// reconciliation.go owns the runner's live bookkeeping of expected/queued/
// processed/persisted crawl items.
//
// If item-level mismatch or resume shortfall looks wrong, inspect this tracker
// before going into output formatting or bridge code.
//
// Ownership summary:
// 1) track expected, queued, attempted, processed, and persisted crawl items
// 2) derive mismatch sets for resume/quality/report consumers
// 3) keep item-level reconciliation separate from output and UI projection
//
// File map for maintainers:
// 1) tracker and duplicate-group DTOs
// 2) mark/add helpers for lifecycle bookkeeping
// 3) snapshot/reconciliation derivation helpers for reporting callers

type Reconciliation struct {
	ExpectedIDs                 []string `json:"expectedIds"`
	QueuedIDs                   []string `json:"queuedIds"`
	PersistedIDs                []string `json:"persistedIds"`
	ExpectedButNotQueuedIDs     []string `json:"expectedButNotQueuedIds"`
	ExpectedButNotPersistedIDs  []string `json:"expectedButNotPersistedIds"`
	ProcessedButNotPersistedIDs []string `json:"processedButNotPersistedIds"`
	DuplicateExpectedIDs        []string `json:"duplicateExpectedIds"`
	ExpectedEntryCount          int      `json:"expectedEntryCount"`
	RawDuplicateEntryCount      int      `json:"rawDuplicateEntryCount"`
	SkippedItemIDs              []string `json:"skippedItemIds"`
}

type Tracker struct {
	mu sync.RWMutex

	queuedDetailLinks        map[string]struct{}
	queuedItemIDs            map[string]struct{}
	expectedItemIDs          map[string]struct{}
	attemptedItemIDs         map[string]struct{}
	processedItemIDs         map[string]struct{}
	persistedItemIDs         map[string]struct{}
	skippedItemIDs           map[string]struct{}
	expectedItemLinkMap      map[string]string
	expectedDetailLinks      map[string]struct{}
	expectedDetailLinkKeys   map[string]struct{}
	expectedEntryCountRaw    int
	expectedItemVariantLinks map[string][]string
	duplicateExpectedIDs     map[string]struct{}
	attemptedDetailLinks     map[string]struct{}
	processedDetailLinks     map[string]struct{}
	persistedDetailLinks     map[string]struct{}
	persistedFilmIDs         map[string]struct{}
}

type DuplicateGroup struct {
	ItemID string   `json:"itemId"`
	Links  []string `json:"links"`
}

// NewTracker creates the empty bookkeeping maps for one crawl run.
func NewTracker() *Tracker {
	return &Tracker{
		queuedDetailLinks:        map[string]struct{}{},
		queuedItemIDs:            map[string]struct{}{},
		expectedItemIDs:          map[string]struct{}{},
		attemptedItemIDs:         map[string]struct{}{},
		processedItemIDs:         map[string]struct{}{},
		persistedItemIDs:         map[string]struct{}{},
		skippedItemIDs:           map[string]struct{}{},
		expectedItemLinkMap:      map[string]string{},
		expectedDetailLinks:      map[string]struct{}{},
		expectedDetailLinkKeys:   map[string]struct{}{},
		expectedItemVariantLinks: map[string][]string{},
		duplicateExpectedIDs:     map[string]struct{}{},
		attemptedDetailLinks:     map[string]struct{}{},
		processedDetailLinks:     map[string]struct{}{},
		persistedDetailLinks:     map[string]struct{}{},
		persistedFilmIDs:         map[string]struct{}{},
	}
}

func (t *Tracker) IsAlreadyPersisted(link string, resumeExisting bool) bool {
	if !resumeExisting {
		return false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	itemID := getDetailItemIDFromLink(link)
	if _, ok := t.persistedDetailLinks[link]; ok {
		return true
	}
	if _, ok := t.persistedItemIDs[itemID]; ok {
		return true
	}
	filmID := extractFilmIDFromLink(link)
	if filmID != "" {
		if _, ok := t.persistedFilmIDs[filmID]; ok {
			return true
		}
	}
	return false
}

func (t *Tracker) RememberExpectedDetailLink(link string) {
	raw := strings.TrimSpace(link)
	if raw == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rememberExpectedDetailLinkLocked(raw)
}

func (t *Tracker) rememberExpectedDetailLinkLocked(raw string) {
	t.expectedEntryCountRaw++
	normalized := normalizeDetailLink(raw)
	if normalized != "" {
		if _, ok := t.expectedDetailLinkKeys[normalized]; !ok {
			t.expectedDetailLinkKeys[normalized] = struct{}{}
			t.expectedDetailLinks[raw] = struct{}{}
		}
	}
	itemID := getDetailItemIDFromLink(raw)
	t.expectedItemVariantLinks[itemID] = append(t.expectedItemVariantLinks[itemID], raw)
}

func (t *Tracker) RecordExpectedPageLinks(pageNumber int, links []string) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	duplicateIDs := make([]string, 0)
	for _, link := range links {
		t.rememberExpectedDetailLinkLocked(link)
		itemID := getDetailItemIDFromLink(link)
		if _, ok := t.expectedItemIDs[itemID]; ok {
			t.duplicateExpectedIDs[itemID] = struct{}{}
			duplicateIDs = append(duplicateIDs, itemID)
			continue
		}
		t.expectedItemIDs[itemID] = struct{}{}
		t.expectedItemLinkMap[itemID] = link
	}
	return duplicateIDs
}

func (t *Tracker) MarkQueued(link string, itemID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.queuedDetailLinks[link] = struct{}{}
	t.queuedItemIDs[itemID] = struct{}{}
}

func (t *Tracker) IsQueued(itemID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	_, ok := t.queuedItemIDs[itemID]
	return ok
}

func (t *Tracker) MarkAttempted(link string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.attemptedDetailLinks[link] = struct{}{}
	t.attemptedItemIDs[getDetailItemIDFromLink(link)] = struct{}{}
}

func (t *Tracker) MarkProcessed(link string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.processedDetailLinks[link] = struct{}{}
	t.processedItemIDs[getDetailItemIDFromLink(link)] = struct{}{}
}

func (t *Tracker) MarkPersisted(link string, filmID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if link != "" {
		t.persistedDetailLinks[link] = struct{}{}
	}
	itemID := getDetailItemIDFromLink(link)
	if itemID != "" {
		t.persistedItemIDs[itemID] = struct{}{}
	}
	if filmID != "" {
		t.persistedFilmIDs[filmID] = struct{}{}
		t.persistedItemIDs[filmID] = struct{}{}
	}
}

func (t *Tracker) MarkSkipped(itemID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if itemID != "" {
		t.skippedItemIDs[itemID] = struct{}{}
	}
}

func (t *Tracker) RestoreExpectedLinks(links []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, link := range links {
		t.rememberExpectedDetailLinkLocked(link)
		itemID := getDetailItemIDFromLink(link)
		if itemID == "" {
			continue
		}
		if _, exists := t.expectedItemIDs[itemID]; exists {
			t.duplicateExpectedIDs[itemID] = struct{}{}
			continue
		}
		t.expectedItemIDs[itemID] = struct{}{}
		t.expectedItemLinkMap[itemID] = link
	}
}

func (t *Tracker) RestoreQueuedLinks(links []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, link := range links {
		itemID := getDetailItemIDFromLink(link)
		if itemID == "" {
			continue
		}
		t.queuedDetailLinks[link] = struct{}{}
		t.queuedItemIDs[itemID] = struct{}{}
	}
}

func (t *Tracker) RestoreProcessedLinks(links []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, link := range links {
		itemID := getDetailItemIDFromLink(link)
		if itemID == "" {
			continue
		}
		t.processedDetailLinks[link] = struct{}{}
		t.processedItemIDs[itemID] = struct{}{}
	}
}

func (t *Tracker) RestorePersistedLinks(links []string, filmIDs []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, link := range links {
		itemID := getDetailItemIDFromLink(link)
		if itemID != "" {
			t.persistedDetailLinks[link] = struct{}{}
			t.persistedItemIDs[itemID] = struct{}{}
		}
	}
	for _, filmID := range filmIDs {
		if filmID != "" {
			t.persistedFilmIDs[filmID] = struct{}{}
			t.persistedItemIDs[filmID] = struct{}{}
		}
	}
}

func (t *Tracker) MarkPersistedFilmID(filmID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if filmID != "" {
		t.persistedFilmIDs[filmID] = struct{}{}
		t.persistedItemIDs[filmID] = struct{}{}
	}
}

// ExpectedEntryCount keeps the original index-entry volume, including raw
// duplicates. This is useful when investigating "why did page N only queue X"
// because the final unique item count alone hides duplicate discoveries.
func (t *Tracker) ExpectedEntryCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.expectedEntryCountLocked()
}

func (t *Tracker) expectedEntryCountLocked() int {
	if t.expectedEntryCountRaw > 0 {
		return t.expectedEntryCountRaw
	}
	if len(t.expectedDetailLinks) > 0 {
		return len(t.expectedDetailLinks)
	}
	return len(t.expectedItemIDs)
}

func (t *Tracker) RawDuplicateEntryCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.rawDuplicateEntryCountLocked()
}

func (t *Tracker) rawDuplicateEntryCountLocked() int {
	total := 0
	for _, group := range t.rawDuplicateGroupsLocked() {
		if len(group.Links) > 1 {
			total += len(group.Links) - 1
		}
	}
	return total
}

func (t *Tracker) RawDuplicateGroups() []DuplicateGroup {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.rawDuplicateGroupsLocked()
}

func (t *Tracker) rawDuplicateGroupsLocked() []DuplicateGroup {
	result := make([]DuplicateGroup, 0)
	for itemID, links := range t.expectedItemVariantLinks {
		if len(links) <= 1 {
			continue
		}
		sorted := make([]string, len(links))
		copy(sorted, links)
		sort.Strings(sorted)
		result = append(result, DuplicateGroup{ItemID: itemID, Links: sorted})
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].ItemID) < strings.ToLower(result[j].ItemID)
	})
	return result
}

func (t *Tracker) DuplicateItemIDs() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.duplicateItemIDsLocked()
}

// duplicateItemIDsLocked is the lock-free implementation used by callers
// that already hold Tracker.mu. Keeping the public wrapper above separate is
// important: recursively acquiring an RWMutex read lock can deadlock when a
// writer is waiting between the two read-lock attempts.
func (t *Tracker) duplicateItemIDsLocked() []string {
	result := make([]string, 0, len(t.duplicateExpectedIDs))
	for id := range t.duplicateExpectedIDs {
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i]) < strings.ToLower(result[j])
	})
	return result
}

func (t *Tracker) ExpectedButNotQueuedIDs() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.expectedButNotQueuedIDsLocked()
}

func (t *Tracker) expectedButNotQueuedIDsLocked() []string {
	result := make([]string, 0)
	for id := range t.expectedItemIDs {
		if _, ok := t.queuedItemIDs[id]; !ok {
			if _, ok := t.persistedItemIDs[id]; !ok {
				result = append(result, id)
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i]) < strings.ToLower(result[j])
	})
	return result
}

func (t *Tracker) ExpectedButNotQueuedLinks() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]string, 0)
	for _, id := range t.expectedButNotQueuedIDsLocked() {
		if link, ok := t.expectedItemLinkMap[id]; ok {
			result = append(result, link)
		}
	}
	return result
}

func (t *Tracker) ExpectedButNotPersistedIDs() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.expectedButNotPersistedIDsLocked()
}

func (t *Tracker) expectedButNotPersistedIDsLocked() []string {
	result := make([]string, 0)
	for id := range t.expectedItemIDs {
		if _, ok := t.persistedItemIDs[id]; ok {
			continue
		}
		if _, ok := t.skippedItemIDs[id]; ok {
			continue
		}
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i]) < strings.ToLower(result[j])
	})
	return result
}

func (t *Tracker) ProcessedButNotPersistedIDs() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.processedButNotPersistedIDsLocked()
}

func (t *Tracker) processedButNotPersistedIDsLocked() []string {
	result := make([]string, 0)
	for id := range t.processedItemIDs {
		if _, ok := t.persistedItemIDs[id]; !ok {
			if _, ok := t.skippedItemIDs[id]; !ok {
				result = append(result, id)
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i]) < strings.ToLower(result[j])
	})
	return result
}

func (t *Tracker) BuildReconciliation() Reconciliation {
	t.mu.RLock()
	defer t.mu.RUnlock()
	expectedNotQueued := t.expectedButNotQueuedIDsLocked()
	expectedNotPersisted := t.expectedButNotPersistedIDsLocked()
	processedNotPersisted := t.processedButNotPersistedIDsLocked()
	expectedIDs := sortedMapKeys(t.expectedItemIDs)
	queuedIDs := sortedMapKeys(t.queuedItemIDs)
	persistedIDs := sortedMapKeys(t.persistedItemIDs)
	duplicateIDs := t.duplicateItemIDsLocked()
	skippedIDs := sortedMapKeys(t.skippedItemIDs)

	// Reconciliation is the canonical "gap view" used by resume, final review,
	// and quality summaries. Any new missing-item explanation should be derived
	// here first instead of adding ad hoc counters elsewhere.
	return Reconciliation{
		ExpectedIDs:                 expectedIDs,
		QueuedIDs:                   queuedIDs,
		PersistedIDs:                persistedIDs,
		ExpectedButNotQueuedIDs:     expectedNotQueued,
		ExpectedButNotPersistedIDs:  expectedNotPersisted,
		ProcessedButNotPersistedIDs: processedNotPersisted,
		DuplicateExpectedIDs:        duplicateIDs,
		ExpectedEntryCount:          t.expectedEntryCountLocked(),
		RawDuplicateEntryCount:      t.rawDuplicateEntryCountLocked(),
		SkippedItemIDs:              skippedIDs,
	}
}

func (t *Tracker) GetPersistedItemIDs() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return sortedMapKeys(t.persistedItemIDs)
}

func (t *Tracker) GetUncapturedItems() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	// Uncaptured items intentionally include both:
	// 1. attempted but not persisted
	// 2. expected but never persisted
	// This makes the unfinished list useful both during active drain and after
	// a stop/restart, where some items may never have entered the worker queue.
	items := map[string]struct{}{}
	for id := range t.attemptedItemIDs {
		if _, ok := t.persistedItemIDs[id]; !ok {
			items[id] = struct{}{}
		}
	}
	for _, id := range t.expectedButNotPersistedIDsLocked() {
		items[id] = struct{}{}
	}
	result := make([]string, 0, len(items))
	for id := range items {
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i]) < strings.ToLower(result[j])
	})
	return result
}

func (t *Tracker) GetMissingDetailLinks() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]string, 0)
	for link := range t.queuedDetailLinks {
		itemID := getDetailItemIDFromLink(link)
		if _, ok := t.persistedItemIDs[itemID]; !ok {
			result = append(result, link)
		}
	}
	return result
}

func (t *Tracker) GetExpectedLink(itemID string) string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.expectedItemLinkMap[itemID]
}

func (t *Tracker) ExpectedDetailLinks() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]string, 0, len(t.expectedDetailLinks))
	for link := range t.expectedDetailLinks {
		result = append(result, link)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i]) < strings.ToLower(result[j])
	})
	return result
}

func (t *Tracker) QueuedDetailLinks() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]string, 0, len(t.queuedDetailLinks))
	for link := range t.queuedDetailLinks {
		result = append(result, link)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i]) < strings.ToLower(result[j])
	})
	return result
}

func (t *Tracker) ProcessedDetailLinks() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]string, 0, len(t.processedDetailLinks))
	for link := range t.processedDetailLinks {
		result = append(result, link)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i]) < strings.ToLower(result[j])
	})
	return result
}

func (t *Tracker) PersistedDetailLinks() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]string, 0, len(t.persistedDetailLinks))
	for link := range t.persistedDetailLinks {
		result = append(result, link)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i]) < strings.ToLower(result[j])
	})
	return result
}

func (t *Tracker) PersistedFilmIDs() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return sortedMapKeys(t.persistedFilmIDs)
}

func (t *Tracker) ResolveFull() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for id, link := range t.expectedItemLinkMap {
		t.expectedItemIDs[id] = struct{}{}
		_ = link
	}
}

func sortedMapKeys(m map[string]struct{}) []string {
	result := make([]string, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i]) < strings.ToLower(result[j])
	})
	return result
}
