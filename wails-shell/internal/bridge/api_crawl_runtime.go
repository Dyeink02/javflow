package bridge

import (
	"fmt"
	"time"

	"javflow/internal/common"
	"javflow/internal/crawlrunner"
)

// Crawl runtime helpers keep runner lifecycle and state emission out of the
// command entrypoints so bridge-level control flow stays small and readable.
//
// Ownership summary:
// 1) normalize crawl runtime payload fields used by runner startup
// 2) enforce bridge-side single-runner lifecycle guards
// 3) keep low-level runtime helper logic out of command entrypoints
//
// File map for maintainers:
// 1) payload-to-runtime field coercion helpers
// 2) bridge-side single-runner guard helpers
// 3) small startup/runtime support utilities

// crawlOutputDirFromPayload, crawlSearchFromPayload, and
// crawlRetryDelayFromPayload are bridge-owned payload normalization helpers for
// runner startup.
func crawlOutputDirFromPayload(payload map[string]any) (string, error) {
	outputDir := nonEmptyString(payload["outputDir"])
	if outputDir == "" {
		outputDir = nonEmptyString(payload["output"])
	}
	if outputDir == "" {
		return "", fmt.Errorf("missing required parameter: outputDir")
	}
	if err := common.ValidateWritableDirectoryPath(outputDir); err != nil {
		return "", fmt.Errorf("outputDir invalid: %w", err)
	}
	return outputDir, nil
}

func crawlSearchFromPayload(payload map[string]any) string {
	search := nonEmptyString(payload["search"])
	if search == "" {
		search = nonEmptyString(payload["keyword"])
	}
	return search
}

func crawlRetryDelayFromPayload(payload map[string]any) time.Duration {
	retryDelayMS := intValue(payload["retryDelay"], 1000)
	if retryDelayMS <= 0 {
		retryDelayMS = 1000
	}
	return time.Duration(retryDelayMS) * time.Millisecond
}

// ensureNoActiveRunner / hasActiveRunner guard the single-active-runner
// invariant at the bridge boundary.
func (a *API) ensureNoActiveRunner() error {
	// Active-runner ownership stays behind one lock/guard so start/restart/stop
	// entrypoints cannot each invent their own concurrent-run policy.
	a.runningMu.Lock()
	defer a.runningMu.Unlock()
	if a.activeRunner != nil {
		return fmt.Errorf("Go native crawl task is already running")
	}
	return nil
}

func (a *API) hasActiveRunner() bool {
	a.runningMu.Lock()
	defer a.runningMu.Unlock()
	return a.activeRunner != nil
}

// newGoNativeRunner is the bridge-side runner factory. Lifecycle control stays
// here, while crawl semantics remain in runner/task services.
func (a *API) newGoNativeRunner(payload map[string]any, baseURL string, outputDir string) (*crawlrunner.Runner, error) {
	// Runner construction is the last payload-to-config normalization step
	// before execution ownership leaves the bridge command layer.
	runner, err := crawlrunner.NewRunner(crawlrunner.Config{
		BaseURL:                     baseURL,
		Base:                        nonEmptyString(payload["base"]),
		Parallel:                    intValue(payload["parallel"], 2),
		Timeout:                     crawlTimeoutFromPayload(payload),
		Limit:                       intValue(payload["limit"], 240),
		TotalPages:                  intValue(payload["totalPages"], 0),
		ItemsPerPage:                intValue(payload["itemsPerPage"], 30),
		Delay:                       intValue(payload["delay"], 2),
		RetryCount:                  intValue(payload["retryCount"], 3),
		RetryDelay:                  crawlRetryDelayFromPayload(payload),
		Nomag:                       boolValue(payload["nomag"], false),
		MetadataOnly:                boolValue(payload["metadataOnly"], false),
		Allmag:                      boolValue(payload["allmag"], false),
		Nopic:                       boolValue(payload["nopic"], false),
		Proxy:                       nonEmptyString(payload["proxy"]),
		Output:                      outputDir,
		UserDataDir:                 a.runtime.store.UserDataDir(),
		Search:                      crawlSearchFromPayload(payload),
		SecondValidation:            true,
		MagnetExcludeKeywords:       nonEmptyString(payload["magnetExcludeKeywords"]),
		ActressCountFilterThreshold: intValue(payload["actressCountFilterThreshold"], 0),
		FilmCodeFilterThreshold:     nonEmptyString(payload["filmCodeFilterThreshold"]),
		MinReleaseDate:              nonEmptyString(payload["minReleaseDate"]),
		SupplementMagnetTopN:        intValue(payload["supplementMagnetTopN"], 3),
		RestoredState:               restoredTaskStateValue(payload["goRestoredTaskState"]),
	}, outputDir)
	if err != nil {
		return nil, fmt.Errorf("create Go native crawl runner: %w", err)
	}
	return runner, nil
}
