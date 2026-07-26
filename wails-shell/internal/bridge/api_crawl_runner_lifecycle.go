package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"javflow/internal/crawlrunner"
)

// Runner lifecycle helpers are separated from payload parsing so crawl control
// flow issues can be debugged without scanning unrelated configuration code.
//
// Ownership summary:
// 1) wire Go runner events into bridge/runtime fanout channels
// 2) own bridge-side runner lifecycle start/stop helper behavior
// 3) keep runner event mirroring separate from payload parsing and mode selection
//
// File map for maintainers:
// 1) Go runner event binding helpers
// 2) runner lifecycle start/stop bridge helpers
// 3) compatibility fanout and log/state emission helpers

func (a *API) bindGoRunnerEvents(runner *crawlrunner.Runner, outputDir string) {
	// The Go runner still mirrors a compact raw feed onto `crawl.state` /
	// `crawl.log` for compatibility consumers. Richer stage/result/review panels
	// are emitted elsewhere; keep this bridge focused on stable baseline signals.
	runner.On(crawlrunner.EventState, func(event crawlrunner.PhaseEvent) {
		statePayload := map[string]any{
			"status":               string(event.Status),
			"message":              event.Message,
			"stats":                event.Stats,
			"outputDir":            outputDir,
			"currentTaskOutputDir": outputDir,
		}
		if data, ok := event.Data.(map[string]any); ok {
			for key, value := range data {
				statePayload[key] = value
			}
		}
		raw, marshalErr := json.Marshal(statePayload)
		if marshalErr != nil {
			log.Printf("state event marshal error: %v", marshalErr)
			return
		}
		a.runtime.bus.Publish("", "state", "crawl.state", "", "", "", time.Now().Format(time.RFC3339), raw)
	})

	runner.On(crawlrunner.EventLog, func(event crawlrunner.PhaseEvent) {
		a.emitLogEntry("info", event.Message)
	})
}

func (a *API) activateRunner(runner *crawlrunner.Runner) chan struct{} {
	a.runningMu.Lock()
	doneCh := make(chan struct{})
	a.activeRunner = runner
	a.runnerDone = doneCh
	a.runningMu.Unlock()

	a.setGoTaskExecutionMode(crawlExecutionModeGoNative)
	return doneCh
}

func (a *API) clearActiveRunner(doneCh chan struct{}) {
	a.runningMu.Lock()
	a.activeRunner = nil
	a.runnerDone = nil
	a.runningMu.Unlock()
	a.setExecutionMode(crawlExecutionModeIdle)
	close(doneCh)
}

func (a *API) runActiveRunnerAsync(runner *crawlrunner.Runner, doneCh chan struct{}) {
	go func() {
		defer a.clearActiveRunner(doneCh)
		if runErr := runner.Run(context.Background()); runErr != nil {
			a.emitLogEntry("error", "Go native crawl runner failed: "+runErr.Error())
		}
	}()
}

func (a *API) stopAndWait() error {
	a.runningMu.Lock()
	runner := a.activeRunner
	doneCh := a.runnerDone
	a.runningMu.Unlock()

	if runner == nil {
		return nil
	}

	runner.Stop()

	// Stop waits only for the currently activated Go runner. Legacy sidecar stop
	// remains a separate compatibility path handled by the higher-level controller.
	if doneCh != nil {
		select {
		case <-doneCh:
		case <-time.After(30 * time.Second):
			return fmt.Errorf("timeout waiting for crawl runner to stop")
		}
	}

	return nil
}

func (a *API) ShutdownRunner() {
	// App shutdown intentionally collapses to the same Go-runner stop path used by
	// explicit stop/restart commands so teardown semantics stay uniform.
	a.setGoTaskExecutionMode(a.currentExecutionMode())
	_ = a.stopAndWait()
	a.setExecutionMode(crawlExecutionModeIdle)
}
