package bridge

import (
	"context"
	"encoding/json"
	"fmt"
)

// These entrypoints are owned by the Go task controller. They are intentionally
// small wrappers so crawl lifecycle bugs can be separated into:
// 1) command dispatch,
// 2) mode selection,
// 3) runner execution.
//
// Ownership summary:
// 1) expose crawl lifecycle entrypoints for the Go task controller
// 2) normalize/save start payloads before dispatch
// 3) keep controller-mode selection separate from runner execution internals
//
// File map for maintainers:
// 1) native controller start/stop entrypoints
// 2) shared crawl-start payload normalization/persistence helper
func (a *API) startCrawlNativeViaTaskController(ctx context.Context, payload map[string]any) (json.RawMessage, error) {
	result, err := a.dispatchCrawlStart(payload)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(result), nil
}

func (a *API) stopCrawlNativeViaTaskController(ctx context.Context) (json.RawMessage, error) {
	result, err := a.handleCrawlStop()
	if err != nil {
		return nil, err
	}
	return json.RawMessage(result), nil
}

// prepareCrawlStartPayload is the shared normalization/persistence boundary for
// any crawl-start path. Keeping it in one place prevents new crawl settings from
// being saved in one controller path but silently missed in another.
func (a *API) prepareCrawlStartPayload(payload map[string]any) (map[string]any, error) {
	payload = a.normalizeActressThresholdPayload(payload)
	// Content validation is opt-in. Older renderer/CLI callers may omit the
	// field entirely; normalize that omission before mode selection so the
	// default stays consistent on the Go-task-controller path.
	if _, exists := payload["magnetContentValidation"]; !exists {
		payload["magnetContentValidation"] = false
	}
	if err := a.saveCrawlerSettings(payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func (a *API) handleTaskControlledCrawlStart(payload map[string]any) (string, error) {
	prepared, err := a.prepareCrawlStartPayload(payload)
	if err != nil {
		return "", err
	}
	mode := a.resolveCrawlExecutionMode(prepared)
	a.setGoTaskExecutionMode(mode)
	a.emitLogEntry("info", fmt.Sprintf(
		"[diagnostic] task-controller crawl start actressCountFilterThreshold=%v filmCodeFilterThreshold=%v output=%v cloudflare=%v executionMode=%s controllerMode=%s",
		prepared["actressCountFilterThreshold"],
		prepared["filmCodeFilterThreshold"],
		prepared["output"],
		payloadBool(prepared["cloudflare"]),
		mode,
		crawlControllerModeGoTask,
	))
	if a.crawl.crawlTask == nil {
		return a.dispatchPreparedCrawlStart(prepared)
	}
	raw, err := a.crawl.crawlTask.Start(context.Background(), prepared)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (a *API) handleTaskControlledCrawlRestart(payload map[string]any) (string, error) {
	prepared, err := a.prepareCrawlStartPayload(payload)
	if err != nil {
		return "", err
	}
	mode := a.resolveCrawlExecutionMode(prepared)
	a.setGoTaskExecutionMode(mode)
	a.emitLogEntry("info", fmt.Sprintf(
		"[diagnostic] task-controller crawl restart output=%v cloudflare=%v executionMode=%s controllerMode=%s",
		prepared["output"],
		payloadBool(prepared["cloudflare"]),
		mode,
		crawlControllerModeGoTask,
	))
	if a.crawl.crawlTask == nil {
		_ = a.stopAndWait()
		return a.dispatchPreparedCrawlStart(prepared)
	}
	raw, err := a.crawl.crawlTask.Restart(context.Background(), prepared)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (a *API) handleTaskControlledCrawlStop() (string, error) {
	a.setGoTaskExecutionMode(a.currentExecutionMode())
	if a.crawl.crawlTask == nil {
		return a.handleCrawlStop()
	}
	raw, err := a.crawl.crawlTask.Stop(context.Background())
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (a *API) handleCrawlStart(payload map[string]any) (string, error) {
	prepared, err := a.prepareCrawlStartPayload(payload)
	if err != nil {
		return "", err
	}
	return a.dispatchPreparedCrawlStart(prepared)
}

func (a *API) dispatchPreparedCrawlStart(payload map[string]any) (string, error) {
	a.emitLogEntry("info", fmt.Sprintf(
		"[diagnostic] crawl start dispatch actressCountFilterThreshold=%v filmCodeFilterThreshold=%v output=%v cloudflare=%v",
		payload["actressCountFilterThreshold"],
		payload["filmCodeFilterThreshold"],
		payload["output"],
		payloadBool(payload["cloudflare"]),
	))
	return a.dispatchCrawlStart(payload)
}
