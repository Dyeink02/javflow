package bridge

import (
	"fmt"
	"os"
	"strings"
	"time"

	"javflow/internal/antiblock"
	"javflow/internal/crawlfetch"
)

// Shared settings helpers keep low-level normalization in one place so crawl
// and organizer settings can evolve independently without duplicating parsing
// rules across the bridge.
//
// If a settings payload starts drifting, inspect this file before changing the
// domain-specific command handlers.
//
// Settings-helper rule:
// keep this file focused on bridge-wide normalization/persistence mechanics.
// Feature policy should stay in the specific option builder or service layer.
//
// Ownership summary:
// 1) provide bridge-wide settings load/mutate/save helpers
// 2) keep tolerant-vs-strict settings reads explicit
// 3) centralize shared normalization/persistence mechanics for bridge commands
//
// File map for maintainers:
// 1) tolerant vs strict settings load helpers
// 2) timeout/proxy/shared scalar normalization helpers
// 3) save-and-reload helpers used by multiple bridge command domains

// loadBridgeSettingsSnapshot is the tolerant read path for option builders and
// probes. Callers that only need best-effort defaults should use this helper
// so temporary settings-read issues do not block unrelated UI queries.
func (a *API) loadBridgeSettingsSnapshot() map[string]any {
	settings, err := a.loadBridgeSettingsSnapshotStrict()
	if err != nil {
		return map[string]any{}
	}
	return settings
}

// loadBridgeSettingsSnapshotStrict is the mutation/fallback path. These call
// sites should surface settings-read failures instead of silently drifting to
// empty defaults, because later troubleshooting depends on knowing the saved
// settings were actually available.
func (a *API) loadBridgeSettingsSnapshotStrict() (map[string]any, error) {
	if a == nil || a.runtime.store == nil {
		return nil, fmt.Errorf("settings store is not initialized")
	}
	return a.runtime.store.Load()
}

// mutateBridgeSettings centralizes the common atomic settings mutation used by
// bridge commands. The store lock spans the read, mutation, and replacement so
// two workspace commands cannot save stale full-map snapshots over each other.
func (a *API) mutateBridgeSettings(mutator func(map[string]any)) (map[string]any, error) {
	if a == nil || a.runtime.store == nil {
		return nil, fmt.Errorf("settings store is not initialized")
	}
	return a.runtime.store.Mutate(mutator)
}

// normalizeAdModelType keeps ad-model selection on the supported bridge values.
func normalizeAdModelType(rawValue string) string {
	value := strings.ToLower(strings.TrimSpace(rawValue))
	switch value {
	case "squeezenet-fast", "yolov8n-balanced", "mobile-net-v3-lite":
		return value
	default:
		return "mobile-net-v3-lite"
	}
}

// normalizeAdFileActionBridge keeps delete-policy values constrained.
func normalizeAdFileActionBridge(rawValue string) string {
	if strings.TrimSpace(rawValue) == "delete-directly" {
		return "delete-directly"
	}
	return "move-to-delete"
}

// normalizeKeywordList deduplicates and cleans free-form keyword input.
func normalizeKeywordList(rawValue string) []string {
	fields := strings.FieldsFunc(rawValue, func(r rune) bool {
		return r == '\r' || r == '\n' || r == ',' || r == ' ' || r == '\t'
	})
	seen := map[string]struct{}{}
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		keyword := strings.ToLower(strings.TrimSpace(field))
		if keyword == "" {
			continue
		}
		if _, exists := seen[keyword]; exists {
			continue
		}
		seen[keyword] = struct{}{}
		result = append(result, keyword)
	}
	return result
}

// stringMapValue normalizes loosely typed JSON maps into string maps.
func stringMapValue(value any) map[string]string {
	rawMap, ok := value.(map[string]any)
	if !ok {
		if typed, typedOk := value.(map[string]string); typedOk {
			result := map[string]string{}
			for key, item := range typed {
				if strings.TrimSpace(key) != "" && strings.TrimSpace(item) != "" {
					result[strings.TrimSpace(key)] = strings.TrimSpace(item)
				}
			}
			return result
		}
		return nil
	}

	result := map[string]string{}
	for key, item := range rawMap {
		cleanKey := strings.TrimSpace(key)
		cleanValue := strings.TrimSpace(fmt.Sprint(item))
		if cleanKey == "" || cleanValue == "" {
			continue
		}
		result[cleanKey] = cleanValue
	}
	return result
}

// resolveCrawlerBaseURL keeps the crawl entrypoint default in one place.
func resolveCrawlerBaseURL(payload map[string]any) string {
	baseURL := nonEmptyString(payload["baseUrl"])
	if baseURL == "" {
		baseURL = nonEmptyString(payload["base"])
	}
	if baseURL == "" {
		baseURL = "https://www.javbus.com/"
	}
	return baseURL
}

// crawlTimeoutFromPayload converts bridge timeout settings to duration.
func crawlTimeoutFromPayload(payload map[string]any) time.Duration {
	timeoutMS := intValue(payload["timeout"], 30000)
	if timeoutMS <= 0 {
		timeoutMS = 30000
	}
	return time.Duration(timeoutMS) * time.Millisecond
}

// buildCrawlFetchServiceFromPayload is the narrow bridge-to-fetch handoff.
func buildCrawlFetchServiceFromPayload(payload map[string]any) (*crawlfetch.Service, error) {
	headers := stringMapValue(payload["headers"])
	cookie := nonEmptyString(payload["cookie"])
	if cookie == "" && headers != nil {
		cookie = strings.TrimSpace(headers["Cookie"])
	}

	antiBlockURLs := []string(nil)
	if homeDir, err := os.UserHomeDir(); err == nil {
		antiBlockURLs, _ = antiblock.LoadURLs(homeDir)
	}
	if rawURLs, ok := payload["antiBlockUrls"].([]string); ok && len(rawURLs) > 0 {
		antiBlockURLs = append(antiBlockURLs, rawURLs...)
	}

	return crawlfetch.NewService(crawlfetch.ServiceOptions{
		Headers:           headers,
		ConfigCookie:      cookie,
		CloudflareCookies: nonEmptyString(payload["cloudflareCookies"]),
		Proxy:             nonEmptyString(payload["proxy"]),
		Timeout:           crawlTimeoutFromPayload(payload),
		UserAgent:         nonEmptyString(payload["userAgent"]),
		RetryCount:        intValue(payload["retryCount"], 3),
		RetryDelay:        time.Duration(intValue(payload["retryDelay"], 1000)) * time.Millisecond,
		AntiBlockURLs:     antiBlockURLs,
	})
}
