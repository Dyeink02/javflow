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

	case "app:resolve-actress-crawl-output":
		output, err := a.resolveActressCrawlerOutput(payload)
		if err != nil {
			return "", true, err
		}
		result, err := marshalResult(output)
		return result, true, err

	case "app:validate-proxy":
		return a.handleValidateProxyCommand(payload)

	case "app:save-actress-atlas-proxy":
		return a.handleSaveActressAtlasProxyCommand(payload)

	case "app:save-workspace-preferences":
		return a.handleSaveWorkspacePreferencesCommand(payload)

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

// handleSaveActressAtlasProxyCommand persists the Actor Atlas proxy through
// the shared settings store. The crawler, rankings and actress details then
// recover the same value after an application restart.
func (a *API) handleSaveActressAtlasProxyCommand(payload map[string]any) (string, bool, error) {
	proxyValue := nonEmptyString(payload["proxy"])
	settings, err := a.mutateBridgeSettings(func(current map[string]any) {
		current["proxy"] = proxyValue
	})
	if err != nil {
		return "", true, err
	}
	result, err := marshalResult(map[string]any{"proxy": nonEmptyString(settings["proxy"])})
	return result, true, err
}

// handleSaveWorkspacePreferences persists renderer-only choices that need to
// survive WebView storage resets as well as normal application restarts.
// Keep the accepted fields explicit: this command is not a generic settings
// writer and must not let arbitrary renderer payloads change crawler options.
func (a *API) handleSaveWorkspacePreferencesCommand(payload map[string]any) (string, bool, error) {
	settings, err := a.mutateBridgeSettings(func(current map[string]any) {
		libraryPreferencesSaved := false
		organizerPreferencesSaved := false
		if value, exists := payload["libraryCompactLayout"]; exists {
			current["libraryCompactLayout"] = boolValue(value, false)
			libraryPreferencesSaved = true
		}
		if value, exists := payload["libraryShowHiddenFiles"]; exists {
			current["libraryShowHiddenFiles"] = boolValue(value, true)
			libraryPreferencesSaved = true
		}
		if value, exists := payload["libraryScrapeConcurrency"]; exists {
			concurrency := intValue(value, 3)
			if concurrency < 1 {
				concurrency = 1
			}
			if concurrency > 5 {
				concurrency = 5
			}
			current["libraryScrapeConcurrency"] = concurrency
			libraryPreferencesSaved = true
		}
		if value, exists := payload["libraryAutoSubscribeFromOutput"]; exists {
			current["libraryAutoSubscribeFromOutput"] = boolValue(value, false)
			libraryPreferencesSaved = true
		}
		if value, exists := payload["organizerAutoSubscribeFromOutput"]; exists {
			current["organizerAutoSubscribeFromOutput"] = boolValue(value, false)
			organizerPreferencesSaved = true
		}
		if libraryPreferencesSaved {
			current["libraryWorkspacePreferencesVersion"] = 1
		}
		if organizerPreferencesSaved {
			current["organizerWorkspacePreferencesVersion"] = 1
		}
	})
	if err != nil {
		return "", true, err
	}
	result, err := marshalResult(map[string]any{
		"libraryCompactLayout":             boolValue(settings["libraryCompactLayout"], false),
		"libraryShowHiddenFiles":           boolValue(settings["libraryShowHiddenFiles"], true),
		"libraryScrapeConcurrency":         intValue(settings["libraryScrapeConcurrency"], 3),
		"libraryAutoSubscribeFromOutput":   boolValue(settings["libraryAutoSubscribeFromOutput"], false),
		"organizerAutoSubscribeFromOutput": boolValue(settings["organizerAutoSubscribeFromOutput"], false),
	})
	return result, true, err
}
