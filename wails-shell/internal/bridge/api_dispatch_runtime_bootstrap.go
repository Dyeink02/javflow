package bridge

// Runtime bootstrap commands hydrate settings, diagnostics, and lightweight
// environment checks needed before or alongside a crawl run. Keeping them away
// from crawl panels makes startup/support issues easier to isolate.
//
// These commands may be called early and often during startup. Keep them
// idempotent/read-mostly so renderer bootstrap retries do not accidentally
// create side effects.
//
// Ownership summary:
// 1) route runtime bootstrap/read-mostly commands during startup
// 2) centralize settings/log-context/diagnostic query entrypoints
// 3) keep startup helpers separate from crawl execution commands
//
// File map for maintainers:
// 1) runtime bootstrap command dispatcher
// 2) read-mostly bootstrap query entrypoints
func (a *API) handleRuntimeBootstrapCommand(command string, payload map[string]any) (string, bool, error) {
	switch command {
	case "app:get-settings":
		return a.handleGetSettingsCommand()

	case "app:get-log-context":
		return a.handleGetLogContextCommand()

	case "app:get-integration-context":
		return a.handleGetIntegrationContextCommand()

	case "app:validate-proxy":
		return a.handleValidateProxyCommand(payload)

	case "app:list-crawl-cache-snapshots":
		result, err := a.listCrawlCacheSnapshotsResult()
		return result, true, err

	case "app:remove-crawl-cache-snapshot":
		result, err := a.removeCrawlCacheSnapshotResult(payload)
		return result, true, err

	case "app:clear-crawl-cache-snapshots":
		result, err := a.clearCrawlCacheSnapshotsResult()
		return result, true, err
	}

	return "", false, nil
}

func (a *API) handleGetSettingsCommand() (string, bool, error) {
	settingsMap, err := a.runtime.store.LoadWithBackground()
	if err != nil {
		return "", true, err
	}
	result, err := marshalResult(settingsMap)
	return result, true, err
}

func (a *API) handleGetLogContextCommand() (string, bool, error) {
	logContext, err := a.getLogContext()
	if err != nil {
		return "", true, err
	}
	result, err := marshalResult(logContext)
	return result, true, err
}

func (a *API) handleGetIntegrationContextCommand() (string, bool, error) {
	integrationContext, err := a.getIntegrationContext()
	if err != nil {
		return "", true, err
	}
	result, err := marshalResult(integrationContext)
	return result, true, err
}

func (a *API) handleValidateProxyCommand(payload map[string]any) (string, bool, error) {
	options, _ := payload["options"].(map[string]any)
	targetURL := ""
	if options != nil {
		targetURL = stringValue(options["targetUrl"])
	}
	result, err := marshalResult(a.runtime.proxyService.ValidateProxy(stringValue(payload["proxyValue"]), targetURL))
	return result, true, err
}
