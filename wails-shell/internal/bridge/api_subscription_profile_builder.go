package bridge

import (
	"strings"
	"time"

	"javflow/internal/avsubscription"
	"javflow/internal/contracts/subscriptiontarget"
)

// Profile-based subscription assembly is used by lookup/refresh flows and is
// the right place to debug count/page drift after inspecting a remote target.
//
// Ownership summary:
// 1) build persisted subscriptions from resolved remote target profiles
// 2) keep profile-derived defaults separate from manual UI input assembly
// 3) centralize count/page/base fallback rules for profile-based subscription flows
//
// File map for maintainers:
// 1) profile-to-subscription field derivation helpers
// 2) fallback count/page/base/name assembly rules

// buildSubscriptionFromProfile converts a resolved target profile into the
// persisted subscription contract. Bridge callers should assemble through this
// helper rather than reimplementing count/page defaults in multiple commands.
func buildSubscriptionFromProfile(payload map[string]any, profile subscriptiontarget.TargetProfile) avsubscription.Subscription {
	actressName := nonEmptyString(payload["actressName"])
	if actressName == "" {
		actressName = strings.TrimSpace(profile.ResolvedActressName)
	}
	if actressName == "" {
		actressName = strings.TrimSpace(profile.ActressName)
	}

	crawlURL := nonEmptyString(payload["targetUrl"])
	if crawlURL == "" {
		crawlURL = strings.TrimSpace(profile.ResolvedBase)
	}
	if actressName == "" {
		actressName = fallbackSubscriptionNameFromURL(crawlURL)
	}

	preferredBase := nonEmptyString(payload["preferredBase"])
	if preferredBase == "" {
		preferredBase = strings.TrimSpace(profile.LookupBaseOrigin)
	}
	if preferredBase == "" {
		preferredBase = fallbackSubscriptionBase(crawlURL)
	}

	manualSyncedCount := intValue(payload["syncedCount"], 0)
	// Subscription currentCount should follow the directly actionable count
	// first, not the broader actress total-film count. The subscription module
	// uses this number to decide "有无新增/需抓多少", so we prioritize the
	// current crawlable/magnet-backed count and only fall back to total-film
	// count when the actionable count is unavailable.
	currentCount := maxInt(profile.FillCount, profile.PreferredCount)
	if currentCount <= 0 {
		currentCount = maxInt(profile.AllCount, manualSyncedCount)
	}

	syncedCount := manualSyncedCount
	if syncedCount <= 0 {
		syncedCount = currentCount
	}
	if currentCount < syncedCount {
		currentCount = syncedCount
	}

	itemsPerPage := intValue(payload["itemsPerPage"], profile.ItemsPerPage)
	if itemsPerPage <= 0 {
		itemsPerPage = 30
	}
	totalPages := intValue(payload["totalPages"], profile.TotalPages)
	if totalPages <= 0 {
		totalPages = maxInt(1, (maxInt(currentCount, syncedCount)+itemsPerPage-1)/itemsPerPage)
	}

	now := time.Now().Format(time.RFC3339)
	return avsubscription.Subscription{
		ID:            nonEmptyString(payload["id"]),
		ActressName:   actressName,
		CrawlURL:      crawlURL,
		PreferredBase: preferredBase,
		LatestItemURL: strings.TrimSpace(profile.LatestItemURL),
		Source:        nonEmptyString(payload["source"]),
		SyncedCount:   syncedCount,
		CurrentCount:  currentCount,
		ItemsPerPage:  itemsPerPage,
		TotalPages:    totalPages,
		LastCheckedAt: now,
		LastSyncedAt:  map[bool]string{true: now, false: ""}[syncedCount == currentCount],
		LastUpdatedAt: now,
		LastError:     "",
	}
}
