package bridge

// Bridge payload helpers are intentionally split by concern:
// - api_value_scalar_helpers.go
// - api_value_slice_helpers.go
// - api_value_json_helpers.go
//
// Keep value helpers as leaf utilities only. When a caller starts needing
// workflow-specific fallback or interpretation, that logic should move up into
// the owning option builder/helper file instead of expanding these utilities.
//
// Ownership summary:
// 1) describe the bridge value-helper split by concern
// 2) keep helper intent visible without turning this file into another utility bag
// 3) remind maintainers to move workflow-specific logic upward
//
// File map for maintainers:
// 1) value-helper grouping note
