package bridge

// handleCrawlCommand is the crawl-domain aggregator used by top-level bridge
// dispatch. Active crawl traffic now fans out by responsibility:
// 1) fetch-page diagnostics
// 2) lifecycle start/stop ownership
// 3) crawl-adjacent support commands
//
// That split keeps future triage narrower. If a command fails, first identify
// whether it was a probe, a live runner mutation, or a support mutation before
// inspecting runner internals or sidecar compatibility.
//
// Crawl rule:
// keep live crawl mutations in lifecycle/support handlers instead of letting
// diagnostics or read-model queries grow side effects.
//
// Ownership summary:
// 1) aggregate crawl-domain dispatch across testing, lifecycle, and support lanes
// 2) keep crawl command routing out of the top-level bridge switch
// 3) preserve the split between diagnostics, mutations, and support helpers
//
// File map for maintainers:
// 1) crawl-domain aggregate dispatcher
// 2) testing/lifecycle/support branch routing
func (a *API) handleCrawlCommand(command string, payload map[string]any) (string, bool, error) {
	if result, handled, err := a.handleCrawlTestingCommand(command, payload); handled {
		return result, true, err
	}
	if result, handled, err := a.handleCrawlLifecycleCommand(command, payload); handled {
		return result, true, err
	}
	if result, handled, err := a.handleCrawlSupportCommand(command, payload); handled {
		return result, true, err
	}

	return "", false, nil
}
