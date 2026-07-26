// Ownership summary:
//   This file implements domain logic for the javflow backend.
//
// File map for maintainers:
//   1) Domain-specific types and helpers.
//   2) Internal service implementation.
//

package bridge

import "javflow/internal/avsubscriptionv2"

func marshalSubscriptionCollectionV2(items []avsubscriptionv2.Subscription, extras map[string]any) (string, error) {
	payload := map[string]any{
		"subscriptions": items,
		"total":         len(items),
	}
	for key, value := range extras {
		payload[key] = value
	}
	return marshalResult(payload)
}

func (a *API) listSubscriptionsV2Result() (string, error) {
	items, err := a.lookup.avSubscriptionsV2.List()
	if err != nil {
		return "", err
	}
	return marshalSubscriptionCollectionV2(items, nil)
}

