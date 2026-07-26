package bridge

import "strings"

// Payload coercion helpers stay in one place so command handlers do not
// duplicate UI transport conversions.
//
// Coercion rule:
// helpers here should only normalize transport shapes, not encode domain
// defaults that belong in option builders or services.
//
// Ownership summary:
// 1) normalize transport-level payload booleans and similar coarse helpers
// 2) keep command handlers free of repeated UI transport coercion
// 3) separate low-level payload coercion from domain defaults
//
// File map for maintainers:
// 1) transport-level payload boolean helper
func payloadBool(val any) bool {
	switch v := val.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true") || v == "1"
	case float64:
		return v != 0
	default:
		return false
	}
}
