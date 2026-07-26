package bridge

import "javflow/internal/avsubscription"

// api_subscription_response_builder.go owns small bridge-side payload builders
// for AV subscription list-style responses.
//
// Ownership summary:
// 1) keep repeated subscription list response envelopes in one helper
// 2) separate response shaping from mutation/query orchestration
// 3) avoid duplicating "subscriptions + total + extras" maps across handlers
//
// File map for maintainers:
// 1) subscription collection response builder

func marshalSubscriptionCollection(items []avsubscription.Subscription, extras map[string]any) (string, error) {
	payload := map[string]any{
		"subscriptions": items,
		"total":         len(items),
	}
	for key, value := range extras {
		payload[key] = value
	}
	return marshalResult(payload)
}
