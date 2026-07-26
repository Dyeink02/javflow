package bridge

// This file is now reserved for shared subscription-builder notes.
// Concrete builders live in:
// - api_subscription_profile_builder.go
// - api_subscription_manual_builder.go
//
// The placeholder remains deliberate: it marks the stable bridge boundary for
// subscription payload assembly without forcing future contributors to rediscover
// where profile-based and manual builder responsibilities diverge.
//
// Ownership summary:
// 1) mark the stable bridge boundary for subscription payload builders
// 2) keep profile/manual builder split discoverable
// 3) avoid re-merging builder responsibilities into one mixed file
//
// File map for maintainers:
// 1) subscription builder split note
