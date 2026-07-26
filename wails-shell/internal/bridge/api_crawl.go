package bridge

import (
	"fmt"
	"strings"

	"javflow/internal/crawlrunner"
	"javflow/internal/modulelog"
)

// Native crawl start remains in its own file because it is the bridge point
// where payload normalization ends and long-running runner ownership begins.
//
// Keep this file narrow: it should only validate payload, build runner inputs,
// bind events, and hand off execution.
//
// Ownership summary:
// 1) validate native crawl start payloads at the final bridge boundary
// 2) build/bind the Go runner and fetch service for one crawl start
// 3) hand off long-running ownership to the runner without keeping command logic here
//
// File map for maintainers:
// 1) native crawl start bridge entrypoint
// 2) runner/fetch build-and-bind handoff
func (a *API) handleGoCrawlNativeStart(payload map[string]any) (string, error) {
	if err := a.ensureNoActiveRunner(); err != nil {
		return "", err
	}

	baseURL := resolveCrawlerBaseURL(payload)
	outputDir, err := crawlOutputDirFromPayload(payload)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(fmt.Sprint(payload["logDir"])) == "" {
		payload["logDir"] = modulelog.Directory(a.runtime.paths.AppPath, modulelog.JAVCrawl)
	}

	fetchService, err := buildCrawlFetchServiceFromPayload(payload)
	if err != nil {
		return "", fmt.Errorf("failed to build crawl fetch service: %w", err)
	}
	a.crawl.crawlFetch = fetchService

	runner, err := a.newGoNativeRunner(payload, baseURL, outputDir)
	if err != nil {
		return "", err
	}

	crawlrunner.BindFetchService(runner, fetchService)
	// The lines below are the runtime-ownership handoff point: once the runner
	// is activated, bridge command handling should step back and rely on
	// lifecycle/read-model services for subsequent state exposure.
	a.bindGoRunnerEvents(runner, outputDir)
	doneCh := a.activateRunner(runner)
	a.initTaskLog(outputDir, payload)
	a.runActiveRunnerAsync(runner, doneCh)

	return marshalResult(map[string]any{"status": "started", "mode": "go-native"})
}
