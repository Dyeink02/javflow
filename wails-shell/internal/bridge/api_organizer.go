package bridge

import "javflow/internal/organizer"

// runOrganizer is intentionally kept thin. Organizer execution now reads as a
// single bridge workflow while sidecar/ad-risk wiring and lifecycle events live
// in dedicated helpers.
//
// This file owns organizer execution only. Crawl-artifact import for expected
// codes lives in a separate bridge helper so future troubleshooting can tell
// apart:
// - "organizer consumed crawl data wrong"
// - "organizer execution failed after inputs were ready"
//
// Ownership summary:
// 1) expose bridge-side organizer run entrypoints
// 2) keep organizer execution lifecycle separate from artifact import
// 3) isolate organizer runtime binding/emission from the service internals
//
// File map for maintainers:
// 1) organizer run entrypoint wrappers
// 2) prepared-run lifecycle/binding helpers
func (a *API) runOrganizer(payload map[string]any) (organizer.RunResult, error) {
	options, err := a.prepareOrganizerRunOptions(payload)
	if err != nil {
		return organizer.RunResult{}, err
	}
	return a.runPreparedOrganizer(options)
}

// runPreparedOrganizer owns only bridge-side lifecycle emission and runtime
// wiring. Actual organizer file decisions must stay inside organizerService so
// bridge changes do not silently fork organizer behavior.
func (a *API) runPreparedOrganizer(options organizer.RunOptions) (organizer.RunResult, error) {
	taskID := newOrganizerTaskID()
	a.emitOrganizerStartState(taskID, options.DryRun)
	a.configureOrganizerAdRisk(&options, taskID)
	a.bindOrganizerRuntimeHandlers(&options, taskID)

	result, err := a.organizer.organizerService.RunOrganizer(options)
	if err != nil {
		a.emitOrganizerFailureState(taskID, err)
		return organizer.RunResult{}, err
	}

	a.emitOrganizerCompletionState(taskID, result)
	return result, nil
}
