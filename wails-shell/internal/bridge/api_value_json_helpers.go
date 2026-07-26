package bridge

import "encoding/json"

// Small JSON bridge helpers stay isolated because they are used across log,
// state, and result emission paths.
//
// Ownership summary:
// 1) provide tiny JSON/map helpers reused across bridge emission paths
// 2) centralize clone/raw-json helpers away from callers
// 3) keep JSON utility behavior separate from domain-specific payload shaping
//
// File map for maintainers:
// 1) map clone helper
// 2) raw-json marshal helper

func cloneMap(source map[string]any) map[string]any {
	next := make(map[string]any, len(source))
	for key, value := range source {
		next[key] = value
	}
	return next
}

func mustRawJSON(value any) json.RawMessage {
	payload, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return payload
}
