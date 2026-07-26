// Package bridge is the current Wails desktop command boundary.
//
// It is the narrow integration layer between:
// 1) frontend commands/events
// 2) Go domain services
// 3) Node sidecar routing
//
// When a desktop bug crosses module boundaries, start here to answer one
// question first: which domain owned the request.
//
// Ownership summary:
// 1) expose the root bridge constructor and shared marshal helper
// 2) wire coarse-grained domain facades into one command boundary
// 3) keep startup-time ownership layout visible before command handlers fan out
//
// File map for maintainers:
// 1) bridge package-wide purpose header
// 2) root API constructor and native-controller hook wiring
// 3) shared bridge result marshal helper
package bridge

import (
	"encoding/json"
)

// NewAPI wires domain facades once at startup from one explicit dependency bag.
// The bridge keeps only the coarse-grained handles it needs so later bug triage
// can start from a smaller, domain-shaped surface instead of one flat service
// bag.
//
// Facade grouping rule:
// - runtime: dialogs, settings, proxy, sidecar/runtime state
// - lookup: actress lookup/ranking + subscription target helpers
// - organizer: organizer + ad-learning
// - crawl: normal crawler controller/runner/read-model services
//
// Keep new bridge methods inside the correct facade bucket so "which module
// owned this request?" remains answerable from the constructor shape alone.
//
// Startup wiring rule:
// NewAPI may connect coarse-grained bridge callbacks such as task-controller
// hooks, but detailed workflow policy must stay inside the underlying domain
// services.
func NewAPI(deps Dependencies) *API {
	api := deps.buildAPI()
	if deps.CrawlTask != nil {
		deps.CrawlTask.SetNativeController(api.startCrawlNativeViaTaskController, api.stopCrawlNativeViaTaskController)
	}
	return api
}

func marshalResult(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}
