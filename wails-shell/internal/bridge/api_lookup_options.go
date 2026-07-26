package bridge

// Lookup option builders are the bridge boundary between saved desktop
// settings and lookup-related services.
//
// The file set is intentionally split by problem domain:
// - target resolution / inspection
// - ranking helpers
// - anti-block operational helpers
//
// If lookup behavior changes, start from the matching option builder before
// debugging the underlying service implementation.
//
// Ownership summary:
// 1) define the bridge-side grouping for lookup option builders
// 2) explain the split between target, ranking, and anti-block option helpers
// 3) keep lookup option intent visible before diving into service code
//
// File map for maintainers:
// 1) lookup option-builder grouping note
