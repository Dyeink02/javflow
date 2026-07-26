package bridge

import "fmt"

// handleCrawlSupportCommand covers crawl-adjacent maintenance mutations that
// are not themselves runner lifecycle controls. Keeping them apart prevents the
// crawl lifecycle lane from turning back into a mixed bag of unrelated actions.
//
// Support rule:
// these commands may update crawl-adjacent configuration/services, but they
// should not directly drive runner state transitions.
//
// Ownership summary:
// 1) route crawl-adjacent support and maintenance commands
// 2) keep non-lifecycle crawl helpers away from runner mutation paths
// 3) centralize crawl support dispatch in one narrow lane
//
// File map for maintainers:
// 1) crawl support command dispatcher
// 2) support-command branch routing
func (a *API) handleCrawlSupportCommand(command string, payload map[string]any) (string, bool, error) {
	switch command {
	case "app:update-antiblock":
		if a.lookup.antiBlock == nil {
			return "", true, fmt.Errorf("anti-block service is not initialized")
		}
		updated, err := a.lookup.antiBlock.Update(a.buildAntiBlockOptions(payload))
		if err != nil {
			return "", true, err
		}
		result, err := marshalResult(updated)
		return result, true, err
	}

	return "", false, nil
}
