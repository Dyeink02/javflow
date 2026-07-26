package bridge

import "context"

// Runtime-state helpers are the bridge-owned synchronization point between the
// current execution lane and the renderer/bootstrap read models. Keep mode
// mutation here so crawl lifecycle changes do not scatter runtime-state writes.
//
// Ownership summary:
// 1) synchronize bridge-side runtime state and Wails context wiring
// 2) centralize execution/controller mode updates
// 3) keep runtime-state mutation separate from crawl command logic
//
// File map for maintainers:
// 1) Wails context wiring helper
// 2) execution/controller mode update helpers

// SetWailsContext wires the active desktop context into the shared bus/runtime
// synchronization path.
func (a *API) SetWailsContext(ctx context.Context) {
	a.wailsCtx = ctx
}

// updateCrawlModes keeps execution/controller mode changes together so crawl
// lifecycle paths do not drift into partially-updated runtime state.
func (a *API) updateCrawlModes(executionMode string, controllerMode string) {
	if a == nil || a.runtime.runtimeState == nil {
		return
	}
	if executionMode != "" {
		a.runtime.runtimeState.SetExecutionMode(executionMode)
	}
	if controllerMode != "" {
		a.runtime.runtimeState.SetControllerMode(controllerMode)
	}
}

func (a *API) setExecutionMode(mode string) {
	a.updateCrawlModes(mode, "")
}

func (a *API) setControllerMode(mode string) {
	a.updateCrawlModes("", mode)
}

// setGoTaskExecutionMode applies the bridge's canonical "Go task controller"
// execution/controller pairing.
func (a *API) setGoTaskExecutionMode(mode string) {
	// Go-task mode is the canonical "Go owns task lifecycle" switch used by
	// current crawl orchestration. Keep the dual update in one helper so
	// execution/controller mode cannot drift apart.
	a.updateCrawlModes(mode, crawlControllerModeGoTask)
}

func (a *API) currentExecutionMode() string {
	if a == nil || a.runtime.runtimeState == nil {
		return ""
	}
	return a.runtime.runtimeState.ExecutionMode()
}

func (a *API) currentControllerMode() string {
	if a == nil || a.runtime.runtimeState == nil {
		return ""
	}
	return a.runtime.runtimeState.ControllerMode()
}
