// Ownership summary:
//   This file implements domain logic for the javflow backend.
//
// File map for maintainers:
//   1) Domain-specific types and helpers.
//   2) Internal service implementation.
//

package bridge

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"javflow/internal/avsubscriptionv2"
	"javflow/internal/modulelog"
)

const subscriptionV2LogEventName = "avsubscriptionv2.log"

func (a *API) emitSubscriptionV2Log(level string, message string) {
	if a == nil {
		return
	}
	now := time.Now()
	timestamp := now.Format(time.RFC3339)
	normalizedLevel := strings.TrimSpace(level)
	normalizedMessage := strings.TrimSpace(message)
	_ = modulelog.Append(a.runtime.paths.AppPath, modulelog.Subscription, normalizedLevel, normalizedMessage, now)
	if a.runtime.bus == nil {
		return
	}
	a.runtime.bus.Emit(subscriptionV2LogEventName, map[string]any{
		"level":     normalizedLevel,
		"message":   normalizedMessage,
		"timestamp": timestamp,
	})
}

func (a *API) subscriptionV2RefreshLogger() func(level string, message string) {
	return func(level string, message string) {
		a.emitSubscriptionV2Log(level, message)
	}
}

func (a *API) buildSubscriptionV2RuntimeOptions(payload map[string]any) avsubscriptionv2.ScanRuntimeOptions {
	settingsMap := a.loadBridgeSettingsSnapshot()

	proxyValue := nonEmptyString(payload["proxy"])
	if proxyValue == "" {
		proxyValue = nonEmptyString(settingsMap["proxy"])
	}

	timeout := crawlTimeoutFromPayload(settingsMap)
	if _, exists := payload["timeout"]; exists {
		timeout = crawlTimeoutFromPayload(payload)
	}

	headers := stringMapValue(settingsMap["headers"])
	if payloadHeaders := stringMapValue(payload["headers"]); payloadHeaders != nil {
		headers = payloadHeaders
	}

	configCookie := nonEmptyString(payload["cookie"])
	if configCookie == "" {
		configCookie = nonEmptyString(settingsMap["cookie"])
	}

	cloudflareCookies := nonEmptyString(payload["cloudflareCookies"])
	if cloudflareCookies == "" {
		cloudflareCookies = nonEmptyString(settingsMap["cloudflareCookies"])
	}

	userAgent := nonEmptyString(payload["userAgent"])
	if userAgent == "" {
		userAgent = nonEmptyString(settingsMap["userAgent"])
	}

	return avsubscriptionv2.ScanRuntimeOptions{
		Proxy:             proxyValue,
		Timeout:           timeout,
		ConfigCookie:      configCookie,
		CloudflareCookies: cloudflareCookies,
		UserAgent:         userAgent,
		Headers:           headers,
		RetryCount:        intValue(payload["retryCount"], intValue(settingsMap["retryCount"], 3)),
		RetryDelay:        time.Duration(intValue(payload["retryDelay"], intValue(settingsMap["retryDelay"], 1000))) * time.Millisecond,
		Cloudflare:        boolValue(payload["cloudflare"], boolValue(settingsMap["cloudflare"], false)),
		SecondValidation:  boolValue(payload["secondValidation"], boolValue(settingsMap["secondValidation"], true)),
		PageFetcher:       a.subscriptionV2SidecarPageFetcher(),
	}
}

func describeSubscriptionV2RuntimeOptions(options avsubscriptionv2.ScanRuntimeOptions) string {
	return fmt.Sprintf(
		"proxy=%s timeout=%d cloudflare=%v secondValidation=%v retry=%d",
		firstNonEmpty(options.Proxy, "none"),
		int(options.Timeout/time.Millisecond),
		options.Cloudflare,
		options.SecondValidation,
		options.RetryCount,
	)
}

func (a *API) refreshSubscriptionsV2Result(payload map[string]any) (string, error) {
	if a.lookup.avSubscriptionsV2 == nil {
		return "", fmt.Errorf("AV subscription V2 service is not initialized")
	}
	runtimeOptions := a.buildSubscriptionV2RuntimeOptions(payload)
	a.emitSubscriptionV2Log("info", "[diagnostic] AV订阅检测桥接主爬虫请求配置 "+describeSubscriptionV2RuntimeOptions(runtimeOptions))
	summary, err := a.lookup.avSubscriptionsV2.RefreshAll(
		a.applicationContext(),
		runtimeOptions,
		a.subscriptionV2RefreshLogger(),
	)
	if err != nil {
		return "", err
	}
	return marshalResult(summary)
}

func (a *API) subscriptionV2SidecarPageFetcher() avsubscriptionv2.ScanPageFetcher {
	return func(pageURL string, runtimeOptions avsubscriptionv2.ScanRuntimeOptions) (avsubscriptionv2.ScanPageFetchResult, error) {
		a.emitSubscriptionV2Log("info", fmt.Sprintf("[diagnostic] AV订阅检测使用 JAV 爬虫兼容页面获取 page=%s cloudflare=%v proxy=%s", pageURL, runtimeOptions.Cloudflare, firstNonEmpty(runtimeOptions.Proxy, "none")))
		if !runtimeOptions.Cloudflare {
			return avsubscriptionv2.ScanPageFetchResult{}, fmt.Errorf("Cloudflare compatibility is not enabled")
		}
		if err := a.ensureSidecarStarted(); err != nil {
			return avsubscriptionv2.ScanPageFetchResult{}, err
		}
		timeout := runtimeOptions.Timeout
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		payload := map[string]any{
			"pageUrl":          pageURL,
			"base":             pageURL,
			"baseUrl":          pageURL,
			"proxy":            runtimeOptions.Proxy,
			"timeout":          int(timeout / time.Millisecond),
			"cookie":           runtimeOptions.ConfigCookie,
			"cloudflare":       runtimeOptions.Cloudflare,
			"secondValidation": runtimeOptions.SecondValidation,
			"retryCount":       runtimeOptions.RetryCount,
			"retryDelay":       int(runtimeOptions.RetryDelay / time.Millisecond),
			"parallel":         2,
			"delay":            2,
		}

		// The compatibility fetcher remains the existing JAV crawler route. Its
		// only lifecycle policy here is cancellation when the desktop closes.
		raw, err := a.runtime.manager.Call(a.applicationContext(), "crawl", "fetch-index-page", payload)
		if err != nil {
			return avsubscriptionv2.ScanPageFetchResult{}, err
		}
		var decoded struct {
			PageURL    string   `json:"pageUrl"`
			StatusCode int      `json:"statusCode"`
			Links      []string `json:"links"`
			LinkCount  int      `json:"linkCount"`
			BodyLength int      `json:"bodyLength"`
		}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return avsubscriptionv2.ScanPageFetchResult{}, err
		}
		a.emitSubscriptionV2Log("info", fmt.Sprintf("[diagnostic] JAV 爬虫兼容页面获取完成 links=%d bodyLength=%d status=%d", len(decoded.Links), decoded.BodyLength, decoded.StatusCode))
		return avsubscriptionv2.ScanPageFetchResult{
			URL:        firstNonEmpty(decoded.PageURL, pageURL),
			StatusCode: decoded.StatusCode,
			Links:      decoded.Links,
			LinkCount:  decoded.LinkCount,
		}, nil
	}
}

func (a *API) refreshSubscriptionV2Result(payload map[string]any) (string, error) {
	if a.lookup.avSubscriptionsV2 == nil {
		return "", fmt.Errorf("AV subscription V2 service is not initialized")
	}
	id := strings.TrimSpace(nonEmptyString(payload["id"]))
	if id == "" {
		return "", fmt.Errorf("subscription id is required")
	}
	runtimeOptions := a.buildSubscriptionV2RuntimeOptions(payload)
	a.emitSubscriptionV2Log("info", "[diagnostic] AV订阅检测桥接主爬虫请求配置 "+describeSubscriptionV2RuntimeOptions(runtimeOptions))
	result, err := a.lookup.avSubscriptionsV2.RefreshOne(
		a.applicationContext(),
		id,
		runtimeOptions,
		a.subscriptionV2RefreshLogger(),
	)
	if err != nil {
		return "", err
	}
	return marshalResult(result)
}
