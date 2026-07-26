package bridge

import "fmt"

// Lookup target commands resolve or inspect actress/subscription targets.
// Keep them separate from ranking and anti-block helpers so target-shaping
// problems can be debugged without scanning unrelated lookup features.
//
// Target rule:
// these helpers may normalize actress target inputs/outputs, but they should
// stop short of launching crawls or mutating subscription state.
//
// Ownership summary:
// 1) route actress/subscription target lookup commands
// 2) keep target inspection/resolution separate from rankings and anti-block helpers
// 3) preserve the read-only boundary for lookup target helpers
//
// File map for maintainers:
// 1) lookup target command dispatcher
// 2) resolve/inspect target branches
func (a *API) handleLookupTargetCommand(command string, payload map[string]any) (string, bool, error) {
	switch command {
	case "app:resolve-actress-crawl-target":
		if a.lookup.actressLookup == nil {
			return "", true, fmt.Errorf("actress lookup service is not initialized")
		}
		target, err := a.lookup.actressLookup.ResolveTarget(a.buildActressLookupOptions(payload))
		if err != nil {
			return "", true, err
		}
		result, err := marshalResult(target)
		return result, true, err

	case "app:inspect-actress-target":
		if a.lookup.actressLookup == nil {
			return "", true, fmt.Errorf("actress lookup service is not initialized")
		}
		profile, err := a.lookup.actressLookup.InspectTarget(a.buildActressLookupOptions(payload))
		if err != nil {
			return "", true, err
		}
		result, err := marshalResult(profile)
		return result, true, err
	}

	return "", false, nil
}
