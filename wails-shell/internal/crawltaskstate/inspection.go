package crawltaskstate

import "strings"

// Inspection is the lightweight "can I resume and what is left" view used by
// operators and diagnostics.
//
// It summarizes runtime-state presence without forcing callers to manually load
// and interpret Snapshot/validation JSON.
//
// Ownership summary:
// 1) expose a lightweight resume/remaining-work inspection read model
// 2) summarize persisted task-state health without full runner restoration
// 3) keep support inspection separate from runtime execution logic
//
// File map for maintainers:
// 1) inspection read-model DTO
// 2) persisted state presence and restore-summary helpers
// 3) operator-facing lightweight inspection entrypoint
type Inspection struct {
	Paths                     Paths          `json:"paths"`
	TaskStateExists           bool           `json:"taskStateExists"`
	ValidationReportExists    bool           `json:"validationReportExists"`
	SnapshotStatus            string         `json:"snapshotStatus"`
	CanRestore                bool           `json:"canRestore"`
	RestorePageIndex          int            `json:"restorePageIndex"`
	RestorePendingDetailCount int            `json:"restorePendingDetailCount"`
	RestoreFailedDetailCount  int            `json:"restoreFailedDetailCount"`
	RestoreQueuedCount        int            `json:"restoreQueuedCount"`
	RestoreAttemptedCount     int            `json:"restoreAttemptedCount"`
	RestoreCompletedCount     int            `json:"restoreCompletedCount"`
	RestoreMessage            string         `json:"restoreMessage"`
	RestoredState             *RestoredState `json:"restoredState,omitempty"`
}

// Inspect is the read-only entry point for support tooling.
//
// It resolves the task-state paths, loads persisted state when present, and
// derives a restore summary that is cheap to surface in UI or maintenance
// commands.
func Inspect(outputDir string, options ManagerOptions) (Inspection, error) {
	paths, err := ResolvePaths(outputDir, options)
	if err != nil {
		return Inspection{}, err
	}

	inspection := Inspection{
		Paths:                  paths,
		TaskStateExists:        fileExists(paths.TaskStatePath),
		ValidationReportExists: fileExists(paths.ValidationReportPath),
	}
	if !inspection.TaskStateExists {
		return inspection, nil
	}

	manager := &Manager{paths: paths}
	snapshot, err := manager.LoadSnapshot()
	if err != nil {
		return inspection, err
	}
	if snapshot == nil {
		return inspection, nil
	}

	inspection.SnapshotStatus = strings.ToLower(strings.TrimSpace(snapshot.Status))
	restoredState := RestoreFromSnapshot(snapshot)
	if restoredState.ShouldRestore {
		inspection.CanRestore = true
		inspection.RestorePageIndex = restoredState.PageIndex
		inspection.RestorePendingDetailCount = len(restoredState.PendingDetailLinks)
		inspection.RestoreFailedDetailCount = len(restoredState.FailedDetails)
		inspection.RestoreQueuedCount = restoredState.FilmsQueued
		inspection.RestoreAttemptedCount = restoredState.FilmsAttempted
		inspection.RestoreCompletedCount = restoredState.FilmCount
		inspection.RestoreMessage = strings.TrimSpace(restoredState.LogMessage)
		inspection.RestoredState = &restoredState
	}

	return inspection, nil
}
