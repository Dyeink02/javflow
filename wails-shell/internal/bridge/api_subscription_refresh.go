package bridge

// api_subscription_refresh.go owns the bridge-side remote refresh path for AV
// subscriptions.
//
// Ownership summary:
// 1) refresh saved AV subscriptions against remote target pages
// 2) keep refresh result aggregation local to the subscription bridge path
// 3) stay independent from artifact import and live crawl UI/runtime state
//
// File map for maintainers:
// 1) refresh-summary DTO helpers
// 2) refresh result aggregation helpers
// 3) response payload shaping

import (
	"context"
	"fmt"
	"strings"
	"time"

	"javflow/internal/avsubscription"
)

type subscriptionRefreshSummary struct {
	subscriptions []avsubscription.Subscription
	updatedCount  int
	failedCount   int
	totalPending  int
}

func newSubscriptionRefreshSummary(capacity int) subscriptionRefreshSummary {
	if capacity < 0 {
		capacity = 0
	}
	return subscriptionRefreshSummary{
		subscriptions: make([]avsubscription.Subscription, 0, capacity),
	}
}

func (summary *subscriptionRefreshSummary) append(item avsubscription.Subscription, hasUpdate bool, refreshErr error) {
	if refreshErr != nil {
		summary.failedCount++
	}
	if hasUpdate {
		summary.updatedCount++
	}
	summary.totalPending += item.PendingCount
	summary.subscriptions = append(summary.subscriptions, item)
}

func (summary subscriptionRefreshSummary) responsePayload() map[string]any {
	return map[string]any{
		"subscriptions": summary.subscriptions,
		"checkedCount":  len(summary.subscriptions),
		"updatedCount":  summary.updatedCount,
		"failedCount":   summary.failedCount,
		"totalPending":  summary.totalPending,
	}
}

type singleSubscriptionRefreshPayload struct {
	Subscription avsubscription.Subscription `json:"subscription"`
	HasUpdate    bool                        `json:"hasUpdate"`
	CheckedCount int                         `json:"checkedCount"`
	UpdatedCount int                         `json:"updatedCount"`
	FailedCount  int                         `json:"failedCount"`
	TotalPending int                         `json:"totalPending"`
}

// refreshSubscriptionsResult is the remote-target refresh path for AV
// subscriptions. It should use dedicated subscription target inspection rather
// than routing through the JAV crawl UI workflow.
//
// Ownership summary:
// 1) refresh saved AV subscriptions against remote target pages
// 2) update subscription state without depending on live crawl UI/runtime state
// 3) keep refresh orchestration separate from artifact import and URL helpers
func (a *API) refreshSubscriptionsResult() (string, error) {
	items, err := a.lookup.avSubscriptions.List()
	if err != nil {
		return "", err
	}

	summary := newSubscriptionRefreshSummary(len(items))
	now := time.Now().Format(time.RFC3339)

	// Refresh is intentionally a pure "read remote target -> reshape subscription
	// state" pass. It should not mutate crawler settings or depend on live crawl
	// form/runtime state.
	for _, item := range items {
		next, hasUpdate, refreshErr := a.refreshSubscriptionItem(item, now)
		summary.append(next, hasUpdate, refreshErr)
	}

	if err := a.lookup.avSubscriptions.ReplaceAll(summary.subscriptions); err != nil {
		return "", err
	}
	return marshalResult(summary.responsePayload())
}

func (a *API) refreshSubscriptionsWithPayloadResult(payload map[string]any) (string, error) {
	items, err := a.lookup.avSubscriptions.List()
	if err != nil {
		return "", err
	}

	summary := newSubscriptionRefreshSummary(len(items))
	now := time.Now().Format(time.RFC3339)

	for _, item := range items {
		next, hasUpdate, refreshErr := a.refreshSubscriptionItemWithPayload(item, now, payload)
		summary.append(next, hasUpdate, refreshErr)
	}

	if err := a.lookup.avSubscriptions.ReplaceAll(summary.subscriptions); err != nil {
		return "", err
	}
	return marshalResult(summary.responsePayload())
}

// refreshSubscriptionResult refreshes one saved AV subscription against its
// remote target page. It exists so the subscription workspace can check one
// actress directly without routing through the crawler handoff path.
func (a *API) refreshSubscriptionResult(payload map[string]any) (string, error) {
	id := strings.TrimSpace(nonEmptyString(payload["id"]))
	if id == "" {
		return "", fmt.Errorf("subscription id is required")
	}

	items, err := a.lookup.avSubscriptions.List()
	if err != nil {
		return "", err
	}

	index := -1
	for currentIndex, item := range items {
		if strings.TrimSpace(item.ID) == id {
			index = currentIndex
			break
		}
	}
	if index < 0 {
		return "", fmt.Errorf("subscription not found: %s", id)
	}

	now := time.Now().Format(time.RFC3339)
	next, hasUpdate, refreshErr := a.refreshSubscriptionItem(items[index], now)
	items[index] = next

	if err := a.lookup.avSubscriptions.ReplaceAll(items); err != nil {
		return "", err
	}

	response := singleSubscriptionRefreshPayload{
		Subscription: next,
		HasUpdate:    hasUpdate,
		CheckedCount: 1,
		UpdatedCount: map[bool]int{true: 1, false: 0}[hasUpdate],
		FailedCount:  map[bool]int{true: 1, false: 0}[refreshErr != nil],
		TotalPending: next.PendingCount,
	}
	return marshalResult(response)
}

// refreshSubscriptionItem keeps the per-subscription remote inspection logic
// local so list/persistence endpoints remain pure state operations.
func (a *API) refreshSubscriptionItem(item avsubscription.Subscription, now string) (avsubscription.Subscription, bool, error) {
	return a.refreshSubscriptionItemWithPayload(item, now, nil)
}

func (a *API) refreshSubscriptionItemWithPayload(item avsubscription.Subscription, now string, payload map[string]any) (avsubscription.Subscription, bool, error) {
	if strings.TrimSpace(item.CrawlURL) == "" {
		item.LastCheckedAt = now
		item.LastUpdatedAt = now
		item.LastError = "subscription target URL is empty"
		item.Status = "error"
		return item, false, nil
	}

	lookupPayload := map[string]any{
		"actressName":   item.ActressName,
		"targetUrl":     item.CrawlURL,
		"preferredBase": item.PreferredBase,
	}
	proxyValue := ""
	if payload != nil {
		if value := nonEmptyString(payload["proxy"]); value != "" {
			lookupPayload["proxy"] = value
			proxyValue = value
		}
	}

	if len(item.BaselineCodes) > 0 {
		if next, hasUpdate, diffErr := a.refreshSubscriptionByBaselineDiff(item, now, proxyValue); diffErr == nil {
			return next, hasUpdate, nil
		} else {
			item.LastError = "baseline diff failed: " + diffErr.Error()
		}
	}

	profile, inspectErr := a.inspectSubscriptionTarget(lookupPayload)
	if inspectErr != nil {
		item.LastCheckedAt = now
		item.LastUpdatedAt = now
		item.LastError = "inspect failed: " + inspectErr.Error()
		item.Status = "error"
		return item, false, inspectErr
	}

	next := buildSubscriptionFromProfile(map[string]any{
		"id":            item.ID,
		"actressName":   item.ActressName,
		"targetUrl":     item.CrawlURL,
		"preferredBase": item.PreferredBase,
		"source":        item.Source,
		"syncedCount":   item.SyncedCount,
	}, profile)
	// A refresh updates current availability and pending-count projection, but it
	// must preserve explicit sync checkpoints until the user marks the item synced.
	next.LastSyncedAt = item.LastSyncedAt
	next.LastCheckedAt = now
	next.LastUpdatedAt = now
	if len(next.BaselineCodes) == 0 && len(item.BaselineCodes) > 0 {
		next.BaselineCodes = item.BaselineCodes
	}
	return next, next.PendingCount > 0, nil
}

func (a *API) refreshSubscriptionByBaselineDiff(item avsubscription.Subscription, now string, proxyValue string) (avsubscription.Subscription, bool, error) {
	diffResult, err := avsubscription.BuildRefreshDiff(context.Background(), item, proxyValue)
	if err != nil {
		return item, false, err
	}

	next := item
	next.CurrentCount = diffResult.CurrentCount
	next.ItemsPerPage = diffResult.ItemsPerPage
	next.TotalPages = diffResult.TotalPages
	next.LatestItemURL = diffResult.LatestItemURL
	next.LastCheckedAt = now
	next.LastUpdatedAt = now
	next.LastError = ""
	return next, len(diffResult.NewCodes) > 0, nil
}
