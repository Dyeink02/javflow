package avsubscription

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	storageDirName  = "subscriptions"
	storageFileName = "av-subscriptions.json"
)

// storage.go owns persistence and ordering rules for subscriptions. If a bug
// is about saved state, stale counts, or ordering, debug here first.
//
// Ownership summary:
// 1) persist AV subscription state to one stable storage file
// 2) normalize ordering and derived counters during load/save
// 3) keep subscription storage policy separate from refresh/import workflows
//
// File map for maintainers:
// 1) storage path/hash helpers
// 2) load/save and state normalization helpers
// 3) ordering and derived counter refresh helpers

// storagePath is the single persisted location for AV subscription state. Keep
// that path stable so artifact-import and future remote-refresh logic share one
// small state file instead of scattering cache fragments.
func (s *Service) storagePath() string {
	return filepath.Join(s.paths.UserData, storageDirName, storageFileName)
}

// loadLocked reads persisted subscription state and immediately re-normalizes
// counters/status so old files can still be upgraded in memory without a manual
// migration step.
func (s *Service) loadLocked() ([]Subscription, error) {
	filePath := s.storagePath()
	contents, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []Subscription{}, nil
		}
		return nil, err
	}

	items := []Subscription{}
	if err := json.Unmarshal(contents, &items); err != nil {
		return []Subscription{}, nil
	}

	now := time.Now().Format(time.RFC3339)
	normalized := make([]Subscription, 0, len(items))
	for _, item := range items {
		normalized = append(normalized, normalizeSubscription(item, now))
	}
	sortSubscriptionsByPriority(normalized)
	return normalized, nil
}

// saveLocked writes the already-normalized list back as the sole persisted
// source of truth for subscription state.
func (s *Service) saveLocked(items []Subscription) error {
	filePath := s.storagePath()
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return err
	}

	payload, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, payload, 0o644)
}

// buildSubscriptionIdentityHash prefers actress identity over crawl URL so
// artifact-import updates and manual edits converge on the same record when the
// actress is known.
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

// sortSubscriptionsByPriority keeps "needs attention first" ordering stable for
// UI consumers while still remaining deterministic for equal pending counts.
func sortSubscriptionsByPriority(items []Subscription) {
	sort.SliceStable(items, func(i int, j int) bool {
		if items[i].PendingCount != items[j].PendingCount {
			return items[i].PendingCount > items[j].PendingCount
		}

		leftTime := normalizeOrderingTimestamp(items[i].LastUpdatedAt)
		rightTime := normalizeOrderingTimestamp(items[j].LastUpdatedAt)
		if leftTime != rightTime {
			return leftTime > rightTime
		}

		if normalizeName(items[i].ActressName) != normalizeName(items[j].ActressName) {
			return normalizeName(items[i].ActressName) < normalizeName(items[j].ActressName)
		}
		return items[i].ID < items[j].ID
	})
}

func normalizeOrderingTimestamp(value string) string {
	return strings.TrimSpace(value)
}
