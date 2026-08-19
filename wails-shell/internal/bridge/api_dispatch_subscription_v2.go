// Ownership summary:
//
//	This file dispatches V2 subscription bridge commands to the avsubscriptionv2 service.
//
// File map for maintainers:
//  1. Command switch and parameter extraction.
//  2. Result wrappers for list, scan, add, refresh, and crawl commands.
//  3. Error marshalling back to the renderer.
package bridge

// V2 subscription commands intentionally live beside the old subscription
// domain, but on a separate command family. This keeps the rebuilt workflow
// independently debuggable while the rest of the desktop shell stays stable.
func (a *API) handleSubscriptionV2Command(command string, payload map[string]any) (string, bool, error) {
	switch command {
	case "app:list-av-subscriptions-v2":
		result, err := a.listSubscriptionsV2Result()
		return result, true, err
	case "app:scan-av-subscriptions-v2-from-output":
		result, err := a.scanSubscriptionsV2FromOutputResult(payload)
		return result, true, err
	case "app:add-av-subscription-v2-manual":
		result, err := a.addSubscriptionV2ManualResult(payload)
		return result, true, err
	case "app:refresh-av-subscriptions-v2":
		result, err := a.refreshSubscriptionsV2Result(payload)
		return result, true, err
	case "app:refresh-av-subscription-v2":
		result, err := a.refreshSubscriptionV2Result(payload)
		return result, true, err
	case "app:prepare-av-subscription-v2-crawl":
		result, err := a.prepareSubscriptionV2CrawlResult(payload)
		return result, true, err
	case "app:start-av-subscription-v2-crawl":
		result, err := a.startSubscriptionV2CrawlResult(payload)
		return result, true, err
	case "app:start-av-subscription-v2-batch-crawl":
		result, err := a.startSubscriptionV2BatchCrawlResult(payload)
		return result, true, err
	case "app:finalize-av-subscription-v2-crawl":
		result, err := a.finalizeSubscriptionV2CrawlResult(payload)
		return result, true, err
	case "app:stop-av-subscription-v2-crawl":
		result, err := a.stopSubscriptionV2CrawlResult()
		return result, true, err
	case "app:av-subscription-v2-crawl-status":
		result, err := a.subscriptionV2CrawlStatusResult()
		return result, true, err
	case "app:remove-av-subscription-v2":
		result, err := a.removeSubscriptionV2Result(payload)
		return result, true, err
	case "app:clear-av-subscriptions-v2":
		result, err := a.clearSubscriptionsV2Result()
		return result, true, err
	case "app:mark-av-subscription-v2-synced":
		result, err := a.markSubscriptionV2SyncedResult(payload)
		return result, true, err
	case "app:patch-av-subscription-v2":
		result, err := a.patchSubscriptionV2Result(payload)
		return result, true, err
	case "app:patch-all-av-subscriptions-actress-filter-v2":
		result, err := a.patchAllSubscriptionActressFilterV2Result(payload)
		return result, true, err
	case "app:reorder-av-subscriptions-v2":
		result, err := a.reorderSubscriptionsV2Result(payload)
		return result, true, err
	case "app:enrich-av-subscription-v2-media":
		result, err := a.enrichSubscriptionV2MediaResult(payload)
		return result, true, err
	}
	return "", false, nil
}
