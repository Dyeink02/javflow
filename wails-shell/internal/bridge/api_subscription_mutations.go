package bridge

import "javflow/internal/avsubscription"

// Write-oriented subscription helpers are isolated so local persistence/sync
// changes can be reviewed separately from artifact import and remote refresh
// behavior.
//
// Ownership summary:
// 1) handle write-oriented AV subscription mutations
// 2) keep manual add/remove/clear/sync operations separate from import/refresh
// 3) centralize subscription write helpers away from query paths
//
// File map for maintainers:
// 1) add/remove/clear/sync mutation helpers
// 2) persisted-state result shaping helpers

// addSubscriptionResult is the manual-write path only. Artifact import and
// remote refresh should continue to flow through their dedicated entrypoints.
func (a *API) addSubscriptionResult(payload map[string]any) (string, error) {
	next, err := buildManualSubscriptionFromPayload(payload)
	if err != nil {
		return "", err
	}
	saved, err := a.lookup.avSubscriptions.Upsert(next)
	if err != nil {
		return "", err
	}
	return marshalResult(saved)
}

// removeSubscriptionResult, clearSubscriptionsResult, and
// markSubscriptionSyncedResult are pure persisted-state mutations. They must
// stay separate from artifact import and remote inspection flows.
func (a *API) removeSubscriptionResult(payload map[string]any) (string, error) {
	items, err := a.lookup.avSubscriptions.Remove(nonEmptyString(payload["id"]))
	if err != nil {
		return "", err
	}
	return marshalSubscriptionCollection(items, nil)
}

func (a *API) clearSubscriptionsResult() (string, error) {
	clearedCount, err := a.lookup.avSubscriptions.Clear()
	if err != nil {
		return "", err
	}
	return marshalSubscriptionCollection([]avsubscription.Subscription{}, map[string]any{
		"clearedCount": clearedCount,
	})
}

func (a *API) markSubscriptionSyncedResult(payload map[string]any) (string, error) {
	item, err := a.lookup.avSubscriptions.MarkSynced(nonEmptyString(payload["id"]))
	if err != nil {
		return "", err
	}
	return marshalResult(item)
}
