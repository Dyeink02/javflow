package bridge

import (
	"fmt"
	"strings"
	"time"

	"javflow/internal/avsubscription"
)

// Manual subscription assembly is used by direct UI input and should stay
// separate from lookup-derived profile shaping.
//
// Ownership summary:
// 1) build persisted subscriptions from direct UI/manual payloads
// 2) keep manual validation separate from lookup/profile-derived defaults
// 3) centralize manual-input completeness checks and fallback shaping
//
// File map for maintainers:
// 1) manual payload completeness predicates
// 2) manual payload to subscription DTO builder

// hasSubscriptionCountInput keeps "manual payload is complete enough to build a
// subscription" checks in one place.
func hasSubscriptionCountInput(payload map[string]any) bool {
	if payload == nil {
		return false
	}
	rawValue, exists := payload["syncedCount"]
	if !exists || rawValue == nil {
		return false
	}
	switch typed := rawValue.(type) {
	case string:
		return strings.TrimSpace(typed) != ""
	default:
		return strings.TrimSpace(fmt.Sprint(typed)) != ""
	}
}

// buildManualSubscriptionFromPayload is the direct UI-input builder and should
// not absorb lookup/profile-derived defaults.
func buildManualSubscriptionFromPayload(payload map[string]any) (avsubscription.Subscription, error) {
	actressName := nonEmptyString(payload["actressName"])
	targetURL := nonEmptyString(payload["targetUrl"])
	if actressName == "" {
		return avsubscription.Subscription{}, fmt.Errorf("actress name is required")
	}
	if targetURL == "" {
		return avsubscription.Subscription{}, fmt.Errorf("target URL is required")
	}
	if !hasSubscriptionCountInput(payload) {
		return avsubscription.Subscription{}, fmt.Errorf("subscription count is required")
	}

	syncedCount := maxInt(0, intValue(payload["syncedCount"], 0))
	itemsPerPage := intValue(payload["itemsPerPage"], 0)
	if itemsPerPage <= 0 {
		itemsPerPage = 30
	}
	totalPages := intValue(payload["totalPages"], 0)
	if totalPages <= 0 {
		totalPages = maxInt(1, (maxInt(syncedCount, 1)+itemsPerPage-1)/itemsPerPage)
	}

	baselineCodes := stringSliceValue(payload["baselineCodes"])

	preferredBase := nonEmptyString(payload["preferredBase"])
	if preferredBase == "" {
		preferredBase = fallbackSubscriptionBase(targetURL)
	}

	now := time.Now().Format(time.RFC3339)
	return avsubscription.Subscription{
		ID:            nonEmptyString(payload["id"]),
		ActressName:   actressName,
		CrawlURL:      targetURL,
		PreferredBase: preferredBase,
		BaselineCodes: baselineCodes,
		Source:        nonEmptyString(payload["source"]),
		SyncedCount:   syncedCount,
		CurrentCount:  syncedCount,
		ItemsPerPage:  itemsPerPage,
		TotalPages:    totalPages,
		LastCheckedAt: "",
		LastSyncedAt:  map[bool]string{true: now, false: ""}[syncedCount >= 0],
		LastUpdatedAt: now,
		LastError:     "",
	}, nil
}
