package bridge

import "fmt"

// Ownership summary:
// 1) route the read-only proxy region check command
// 2) keep payload shaping at the bridge boundary
// 3) ensure region information cannot become a workflow-blocking policy
//
// File map for maintainers:
// 1) proxy region command handler

// Proxy region detection is a read-only runtime probe. It reports the current
// exit location for the renderer and deliberately has no policy that can stop
// ranking, crawling, searching, or subscription workflows.
func (a *API) handleCheckProxyRegionCommand(payload map[string]any) (string, bool, error) {
	if a.runtime.proxyService == nil {
		return "", true, fmt.Errorf("代理服务未初始化")
	}
	result, err := marshalResult(a.runtime.proxyService.CheckProxyRegion(stringValue(payload["proxyValue"])))
	return result, true, err
}
