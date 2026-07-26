package bridge

// handleRuntimeCommand owns read-mostly bridge commands used by UI bootstrap,
// status hydration, and lightweight diagnostics. Keep runtime command routing
// separate from crawl/organizer/subscription domains so startup read models and
// lightweight app diagnostics can evolve without touching product command lanes.
//
// Runtime rule:
// if a command starts mutating feature state or launching a workflow, it no
// longer belongs here even if the UI calls it during bootstrap.
//
// Ownership summary:
// 1) route read-mostly runtime commands across bootstrap and runtime-query lanes
// 2) keep startup hydration separate from product workflow dispatch
// 3) centralize runtime command routing away from feature domains
//
// File map for maintainers:
// 1) runtime command aggregate dispatcher
// 2) bootstrap vs runtime-query branch routing
func (a *API) handleRuntimeCommand(command string, payload map[string]any) (string, bool, error) {
	if result, handled, err := a.handleRuntimeBootstrapCommand(command, payload); handled {
		return result, true, err
	}
	if result, handled, err := a.handleRuntimeCrawlQueryCommand(command, payload); handled {
		return result, true, err
	}

	return "", false, nil
}
