package bridge

import "fmt"

// Crawler settings helpers own payload fallback and persistent crawler config.
// When crawl behavior differs between runs, debug here before checking the
// runner itself.
//
// Ownership summary:
// 1) normalize persisted crawler settings from incoming bridge payloads
// 2) keep crawler settings save/merge policy centralized
// 3) separate crawl settings persistence from runner execution behavior
//
// File map for maintainers:
// 1) crawler setting field/default tables
// 2) payload-to-settings merge helpers
// 3) strict save/validation entrypoints

type crawlerSettingField struct {
	key          string
	defaultValue any
}

var crawlerSettingFields = []crawlerSettingField{
	{key: "base", defaultValue: ""},
	{key: "output", defaultValue: ""},
	{key: "limit", defaultValue: 0},
	{key: "totalPages", defaultValue: 0},
	{key: "itemsPerPage", defaultValue: 30},
	{key: "parallel", defaultValue: 2},
	{key: "delay", defaultValue: 2},
	{key: "timeout", defaultValue: 30000},
	{key: "proxy", defaultValue: ""},
	{key: "cookie", defaultValue: ""},
	{key: "cloudflareCookies", defaultValue: ""},
	{key: "userAgent", defaultValue: ""},
	{key: "magnetExcludeKeywords", defaultValue: ""},
	{key: "actressCountFilterThreshold", defaultValue: 0},
	{key: "filmCodeFilterThreshold", defaultValue: ""},
	{key: "taskTemplate", defaultValue: ""},
	{key: "cloudflare", defaultValue: true},
	{key: "secondValidation", defaultValue: true},
	{key: "nomag", defaultValue: false},
	{key: "allmag", defaultValue: false},
	{key: "magnetContentValidation", defaultValue: false},
	{key: "nopic", defaultValue: false},
}

func applyCrawlerSettingsPayload(currentSettings map[string]any, payload map[string]any) {
	for _, field := range crawlerSettingFields {
		switch fallback := field.defaultValue.(type) {
		case string:
			currentSettings[field.key] = nonEmptyString(payload[field.key])
		case int:
			currentSettings[field.key] = intValue(payload[field.key], fallback)
		case bool:
			currentSettings[field.key] = boolValue(payload[field.key], fallback)
		}
	}
}

func (a *API) saveCrawlerSettings(payload map[string]any) error {
	a.emitLogEntry("info", fmt.Sprintf(
		"[diagnostic] crawl settings payload actressCountFilterThreshold=%v filmCodeFilterThreshold=%v cloudflare=%v goTaskController=%v output=%v",
		payload["actressCountFilterThreshold"],
		payload["filmCodeFilterThreshold"],
		payloadBool(payload["cloudflare"]),
		payloadBool(payload["goTaskController"]),
		payload["output"],
	))

	_, err := a.mutateBridgeSettings(func(currentSettings map[string]any) {
		applyCrawlerSettingsPayload(currentSettings, payload)
	})
	return err
}

func (a *API) normalizeActressThresholdPayload(payload map[string]any) map[string]any {
	if payload == nil {
		return map[string]any{}
	}

	normalized := cloneMap(payload)
	if a.runtime.store == nil {
		return normalized
	}

	currentSettings, err := a.loadBridgeSettingsSnapshotStrict()
	if err != nil {
		a.emitLogEntry("warn", fmt.Sprintf("[diagnostic] failed to load saved threshold fallback: %v", err))
		return normalized
	}

	if _, exists := normalized["actressCountFilterThreshold"]; !exists {
		fallbackThreshold := intValue(currentSettings["actressCountFilterThreshold"], 0)
		normalized["actressCountFilterThreshold"] = fallbackThreshold
		a.emitLogEntry("warn", fmt.Sprintf(
			"[diagnostic] crawl payload missing actressCountFilterThreshold; fallback to saved value %d (frontendPayloadVersion=%v)",
			fallbackThreshold,
			normalized["frontendPayloadVersion"],
		))
	}

	if _, exists := normalized["filmCodeFilterThreshold"]; !exists {
		fallbackFilmCodeFilter := nonEmptyString(currentSettings["filmCodeFilterThreshold"])
		normalized["filmCodeFilterThreshold"] = fallbackFilmCodeFilter
		a.emitLogEntry("warn", fmt.Sprintf(
			"[diagnostic] crawl payload missing filmCodeFilterThreshold; fallback to saved value %q (frontendPayloadVersion=%v)",
			fallbackFilmCodeFilter,
			normalized["frontendPayloadVersion"],
		))
	}

	return normalized
}
