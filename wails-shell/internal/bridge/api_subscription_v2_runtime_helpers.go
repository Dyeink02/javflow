package bridge

// Ownership summary:
// 1) resolve persisted proxy and cookie settings for subscription V2 crawls
// 2) keep request defaults consistent with the V2 refresh lane
// 3) prevent retired fixed loopback defaults from returning to active code
//
// File map for maintainers:
// 1) proxy resolution
// 2) cookie resolution

import "javflow/internal/common"

// Subscription V2 crawl requests use the same persisted proxy and cookie
// settings as the refresh path. An empty proxy means direct access; it must
// never silently become a hard-coded loopback proxy.
func (a *API) resolveSubcrawlProxy(payload map[string]any) string {
	proxyValue := common.CleanString(payload["proxy"])
	if proxyValue != "" {
		return proxyValue
	}
	return common.CleanString(a.loadBridgeSettingsSnapshot()["proxy"])
}

func (a *API) resolveSubcrawlCookie(payload map[string]any) string {
	cookieValue := common.CleanString(payload["configCookie"])
	if cookieValue == "" {
		cookieValue = common.CleanString(payload["cookie"])
	}
	if cookieValue != "" {
		return cookieValue
	}
	return common.CleanString(a.loadBridgeSettingsSnapshot()["cookie"])
}
