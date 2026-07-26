package bridge

// handleSubscriptionCommand keeps AV subscription flows local so they stop
// sharing one branch table with crawl and organizer controls.
// The file-level split below follows three lanes:
// 1) read-only list queries
// 2) artifact import from filmData.json
// 3) remote target refresh and local persistence mutations
//
// Ownership summary:
// 1) route AV subscription commands to the correct read/import/write helper
// 2) keep subscription dispatch separate from crawl and organizer command tables
// 3) preserve the split between queries, import, refresh, and mutations
//
// Boundary rule:
// bridge routing stays at verb ownership only. Subscription import/refresh
// policy belongs in the result helpers and underlying subscription service.
//
// File map for maintainers:
// 1) subscription command dispatcher
// 2) query/import/mutation branch routing
func (a *API) handleSubscriptionCommand(command string, payload map[string]any) (string, bool, error) {
	// Dispatch stays at verb-routing level only. Subscription import/refresh/
	// persistence rules must remain in the dedicated result helpers and service.
	switch command {
	case "app:list-av-subscriptions":
		result, err := a.listSubscriptionsResult()
		return result, true, err

	case "app:scan-av-subscriptions-from-output":
		result, err := a.scanSubscriptionsFromOutputResult(payload)
		return result, true, err

	case "app:add-av-subscription":
		result, err := a.addSubscriptionResult(payload)
		return result, true, err

	case "app:refresh-av-subscriptions":
		result, err := a.refreshSubscriptionsWithPayloadResult(payload)
		return result, true, err

	case "app:refresh-av-subscription":
		result, err := a.refreshSubscriptionResult(payload)
		return result, true, err

	case "app:remove-av-subscription":
		result, err := a.removeSubscriptionResult(payload)
		return result, true, err

	case "app:clear-av-subscriptions":
		result, err := a.clearSubscriptionsResult()
		return result, true, err

	case "app:mark-av-subscription-synced":
		result, err := a.markSubscriptionSyncedResult(payload)
		return result, true, err

	case "app:start-subscription-crawl":
		result, err := a.startSubscriptionCrawlResult(payload)
		return result, true, err

	case "app:start-subscription-batch-crawl":
		result, err := a.startSubscriptionBatchCrawlResult(payload)
		return result, true, err

	case "app:stop-subscription-crawl":
		result, err := a.stopSubscriptionCrawlResult()
		return result, true, err

	case "app:subscription-crawl-status":
		result, err := a.subscriptionCrawlStatusResult()
		return result, true, err
	}

	return "", false, nil
}
