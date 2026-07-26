package bridge

import (
	"fmt"
	"time"

	"javflow/internal/organizer"
)

const (
	organizerDefaultVideoExtensions = "mp4, mkv, avi, mov, flv, wmv, ts, m4v, iso"
	organizerDefaultSuffix          = "-A"
)

// Organizer runtime helpers keep bridge lifecycle plumbing away from the
// command entrypoint, which makes future organizer changes much easier to
// review and isolate.
//
// Ownership summary:
// 1) normalize organizer start payloads into runtime options
// 2) create bridge-owned organizer lifecycle/task metadata
// 3) keep organizer runtime helper logic out of command entrypoints
//
// File map for maintainers:
// 1) organizer default constants
// 2) payload-to-run-options normalization helpers
// 3) bridge-owned organizer lifecycle/task helper logic

// prepareOrganizerRunOptions is the bridge-owned normalization layer:
// defaults, persisted UI settings, and payload-to-service option translation
// belong here, while organizer execution semantics belong in the service.
func (a *API) prepareOrganizerRunOptions(payload map[string]any) (organizer.RunOptions, error) {
	options := a.buildOrganizerRunOptions(payload)
	if options.VideoExtensions == "" {
		options.VideoExtensions = organizerDefaultVideoExtensions
	}
	if options.Suffix == "" {
		options.Suffix = organizerDefaultSuffix
	}
	if err := a.saveOrganizerSettings(options); err != nil {
		return organizer.RunOptions{}, err
	}
	return options, nil
}

// newOrganizerTaskID creates the bridge-owned organizer lifecycle correlation
// key used by organizer state/log events.
func newOrganizerTaskID() string {
	return fmt.Sprintf("organizer-go-%d", time.Now().UnixMilli())
}

func organizerStartMessage(dryRun bool) string {
	if dryRun {
		return "organizer dry run is starting"
	}
	return "organizer task is starting"
}

func organizerCompletionMessage(result organizer.RunResult) string {
	if result.DryRun {
		return fmt.Sprintf("dry run completed: qualified videos %d", result.Summary.QualifiedVideo)
	}
	return fmt.Sprintf(
		"organizer completed: waiting %d, delete %d, intro ads %d, deleted %d, missing code %d",
		result.Summary.MovedToWaiting,
		result.Summary.MovedToDelete,
		result.Summary.MovedToIntroAd,
		result.Summary.DeletedDirectly,
		result.Summary.MissingCodeCount,
	)
}

// bindOrganizerRuntimeHandlers adapts organizer log/progress callbacks into the
// shared bridge event shape. Keep payload translation here so organizer service
// code does not need to learn renderer event conventions.
func (a *API) bindOrganizerRuntimeHandlers(options *organizer.RunOptions, taskID string) {
	options.OnLog = func(entry organizer.LogEntry) {
		a.emitOrganizerLog(entry, taskID)
	}
	options.OnProgress = func(progress organizer.ProgressEntry) {
		a.emitOrganizerState(map[string]any{
			"status":   "running",
			"mode":     "organizer-progress",
			"message":  organizerProgressMessage(progress),
			"progress": progress,
		}, taskID)
	}
}

// emitOrganizerStartState, emitOrganizerFailureState, and
// emitOrganizerCompletionState keep organizer lifecycle payload wording stable
// at the bridge boundary.
func (a *API) emitOrganizerStartState(taskID string, dryRun bool) {
	a.emitOrganizerState(map[string]any{
		"status":  "starting",
		"mode":    "organizer",
		"message": organizerStartMessage(dryRun),
	}, taskID)
}

func (a *API) emitOrganizerFailureState(taskID string, err error) {
	_ = err
	a.emitOrganizerState(map[string]any{
		"status":  "error",
		"mode":    "organizer",
		"message": "整理任务失败，请查看日志了解详情",
	}, taskID)
}

func (a *API) emitOrganizerCompletionState(taskID string, result organizer.RunResult) {
	a.emitOrganizerState(map[string]any{
		"status":          "completed",
		"mode":            "organizer",
		"message":         organizerCompletionMessage(result),
		"summary":         result.Summary,
		"reportMap":       result.ReportMap,
		"reportFiles":     result.ReportFiles,
		"missingDownload": result.MissingDownload,
		"adRisk":          result.AdRisk,
	}, taskID)
}
