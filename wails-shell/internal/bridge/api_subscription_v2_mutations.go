// Ownership summary:
//   This file implements domain logic for the javflow backend.
//
// File map for maintainers:
//   1) Domain-specific types and helpers.
//   2) Internal service implementation.
//

package bridge

import (
	"fmt"

	"javflow/internal/avsubscriptionv2"
)

func (a *API) removeSubscriptionV2Result(payload map[string]any) (string, error) {
	if a.lookup.avSubscriptionsV2 == nil {
		return "", fmt.Errorf("AV subscription V2 service is not initialized")
	}
	items, err := a.lookup.avSubscriptionsV2.Remove(nonEmptyString(payload["id"]))
	if err != nil {
		return "", err
	}
	return marshalSubscriptionCollectionV2(items, nil)
}

func (a *API) clearSubscriptionsV2Result() (string, error) {
	if a.lookup.avSubscriptionsV2 == nil {
		return "", fmt.Errorf("AV subscription V2 service is not initialized")
	}
	clearedCount, err := a.lookup.avSubscriptionsV2.Clear()
	if err != nil {
		return "", err
	}
	return marshalSubscriptionCollectionV2([]avsubscriptionv2.Subscription{}, map[string]any{
		"clearedCount": clearedCount,
	})
}

func (a *API) markSubscriptionV2SyncedResult(payload map[string]any) (string, error) {
	if a.lookup.avSubscriptionsV2 == nil {
		return "", fmt.Errorf("AV subscription V2 service is not initialized")
	}
	item, err := a.lookup.avSubscriptionsV2.MarkSynced(nonEmptyString(payload["id"]))
	if err != nil {
		return "", err
	}
	return marshalResult(item)
}

func (a *API) patchSubscriptionV2Result(payload map[string]any) (string, error) {
	if a.lookup.avSubscriptionsV2 == nil {
		return "", fmt.Errorf("AV subscription V2 service is not initialized")
	}
	id := nonEmptyString(payload["id"])
	if id == "" {
		return "", fmt.Errorf("subscription id is required")
	}
	item, err := a.lookup.avSubscriptionsV2.Patch(id, payload)
	if err != nil {
		return "", err
	}
	return marshalResult(item)
}

func (a *API) patchAllSubscriptionActressFilterV2Result(payload map[string]any) (string, error) {
	if a.lookup.avSubscriptionsV2 == nil {
		return "", fmt.Errorf("AV subscription V2 service is not initialized")
	}
	threshold := intValue(payload["actressCountFilterThreshold"], 0)
	items, err := a.lookup.avSubscriptionsV2.PatchActressCountFilterThresholdForAll(threshold)
	if err != nil {
		return "", err
	}
	return marshalSubscriptionCollectionV2(items, map[string]any{
		"actressCountFilterThreshold": threshold,
	})
}
