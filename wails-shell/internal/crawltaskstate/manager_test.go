package crawltaskstate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagerSnapshotBackupBehavior(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "output")
	stateRoot := filepath.Join(t.TempDir(), "runtime-state")

	manager, err := NewManager(outputDir, ManagerOptions{StateRoot: stateRoot})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	taskStatePath := manager.Paths().TaskStatePath
	backupPath := filepath.Join(manager.Paths().BackupDir, "task-state.json.bak")

	baseSnapshot := Snapshot{
		SchemaVersion: 2,
		AppVersion:    "0.27.0",
		Status:        "running",
		Message:       "testing",
		Config: SnapshotConfig{
			Output:       outputDir,
			ItemsPerPage: 30,
			TaskTemplate: "balanced",
		},
		Progress: SnapshotProgress{
			NextPageIndex: 1,
			Queued:        1,
		},
		Links: SnapshotLinks{
			Expected:         []string{"https://www.javbus.com/ABP-001"},
			Queued:           []string{"https://www.javbus.com/ABP-001"},
			Processed:        []string{},
			Persisted:        []string{},
			PersistedFilmIDs: []string{},
			SkippedIDs:       []string{},
		},
		FailedDetails: []FailedDetailRecord{},
		PageAudits:    []PageAuditRecord{},
	}

	if err := manager.SaveSnapshot(baseSnapshot, true); err != nil {
		t.Fatalf("save first snapshot: %v", err)
	}
	if !fileExists(taskStatePath) {
		t.Fatalf("expected task state file to exist: %s", taskStatePath)
	}
	if fileExists(backupPath) {
		t.Fatalf("backup should not exist after first save: %s", backupPath)
	}

	baseSnapshot.Progress.Queued = 2
	if err := manager.SaveSnapshot(baseSnapshot, false); err != nil {
		t.Fatalf("save second snapshot: %v", err)
	}
	if fileExists(backupPath) {
		t.Fatalf("backup should still be absent after lightweight save: %s", backupPath)
	}

	baseSnapshot.Progress.Queued = 3
	if err := manager.SaveSnapshot(baseSnapshot, true); err != nil {
		t.Fatalf("save third snapshot: %v", err)
	}
	if !fileExists(backupPath) {
		t.Fatalf("expected backup after full save: %s", backupPath)
	}
}

func TestManagerLoadSaveAndCleanup(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "output")
	stateRoot := filepath.Join(t.TempDir(), "runtime-state")

	manager, err := NewManager(outputDir, ManagerOptions{StateRoot: stateRoot})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	report := ResultValidationReport{
		GeneratedAt:                   "2026-05-01T10:00:00Z",
		MissingItems:                  []string{"ABP-003"},
		ExpectedButNotQueuedItems:     []string{},
		ProcessedButNotPersistedItems: []string{},
		LowConfidencePages:            []int{2},
		Summary:                       "ok",
	}
	if err := manager.SaveValidationReport(report); err != nil {
		t.Fatalf("save validation report: %v", err)
	}

	loadedReport, err := manager.LoadValidationReport()
	if err != nil {
		t.Fatalf("load validation report: %v", err)
	}
	if loadedReport == nil || loadedReport.Summary != "ok" {
		t.Fatalf("unexpected loaded report: %#v", loadedReport)
	}

	snapshot := Snapshot{
		SchemaVersion: 2,
		AppVersion:    "0.27.0",
		Status:        "running",
		Message:       "restoring",
		Config: SnapshotConfig{
			Output:       outputDir,
			TaskTemplate: "balanced",
		},
		Progress: SnapshotProgress{
			NextPageIndex: 5,
		},
		Links: SnapshotLinks{
			Expected:         []string{},
			Queued:           []string{},
			Processed:        []string{},
			Persisted:        []string{},
			PersistedFilmIDs: []string{},
			SkippedIDs:       []string{},
		},
		FailedDetails: []FailedDetailRecord{},
		PageAudits:    []PageAuditRecord{},
	}
	if err := manager.SaveSnapshot(snapshot, true); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	loadedSnapshot, err := manager.LoadSnapshot()
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if loadedSnapshot == nil || loadedSnapshot.Progress.NextPageIndex != 5 {
		t.Fatalf("unexpected loaded snapshot: %#v", loadedSnapshot)
	}

	if err := manager.CleanupRuntimeState(); err != nil {
		t.Fatalf("cleanup runtime state: %v", err)
	}
	if _, err := os.Stat(manager.Paths().RuntimeDir); !os.IsNotExist(err) {
		t.Fatalf("expected runtime dir to be removed, stat err=%v", err)
	}
}

func TestResolvePathsUsesStateRootOutsideOutputDirectory(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "output")
	stateRoot := filepath.Join(t.TempDir(), "runtime-state")

	paths, err := ResolvePaths(outputDir, ManagerOptions{StateRoot: stateRoot})
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}

	if paths.StateRoot != stateRoot {
		t.Fatalf("expected state root %q, got %q", stateRoot, paths.StateRoot)
	}
	if filepath.Dir(paths.RuntimeDir) != stateRoot {
		t.Fatalf("runtime dir should be under state root: %#v", paths)
	}
	if filepath.Dir(paths.TaskStatePath) != paths.RuntimeDir {
		t.Fatalf("task-state path should be inside runtime dir: %#v", paths)
	}
	if filepath.Base(paths.ValidationReportPath) != "validation-report.json" {
		t.Fatalf("unexpected validation report path: %q", paths.ValidationReportPath)
	}
}
