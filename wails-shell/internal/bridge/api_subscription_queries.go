package bridge

// listSubscriptionsResult is the pure read-model path for AV subscriptions.
// Keep it separate from artifact import and remote refresh so list-hydration
// bugs do not get mixed into fetch/update workflows.
//
// Ownership summary:
// 1) expose the pure read-model query for current AV subscriptions
// 2) keep subscription list hydration separate from import/refresh/mutations
// 3) centralize subscription query shaping in one helper
//
// File map for maintainers:
// 1) subscription list query helper
func (a *API) listSubscriptionsResult() (string, error) {
	items, err := a.lookup.avSubscriptions.List()
	if err != nil {
		return "", err
	}
	return marshalSubscriptionCollection(items, nil)
}
