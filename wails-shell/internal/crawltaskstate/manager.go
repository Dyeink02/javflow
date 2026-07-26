// Package crawltaskstate owns persisted runtime state used by resume,
// validation, and post-run diagnostics.
//
// manager.go owns the persisted task-state directory layout and file handles
// used by resume, validation, and post-run diagnostics.
//
// Ownership summary:
// 1) resolve on-disk task-state paths and identity keys
// 2) keep resume/validation persistence layout stable across runs
// 3) separate runtime-state storage from user-facing crawl artifacts
//
// File map for maintainers:
// 1) task-state path and identity DTOs
// 2) output-to-state-root resolution helpers
// 3) snapshot/report load-save utilities
package crawltaskstate

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Practical split:
// - this file resolves where crawl state should live on disk
// - `types.go` defines the persisted shape
// - `persisted_output.go` inspects what the crawl actually wrote
// - keep state identity logic here so resume bugs have one obvious place to inspect
//
// This package is runtime-state persistence only. User-facing crawl artifacts
// remain in crawloutput/contracts packages.
//
const StateRootEnvName = "JAV_SCRAPY_STATE_DIR"

type Paths struct {
	OutputDir            string `json:"outputDir"`
	StateRoot            string `json:"stateRoot"`
	RuntimeDir           string `json:"runtimeDir"`
	TaskStatePath        string `json:"taskStatePath"`
	ValidationReportPath string `json:"validationReportPath"`
	BackupDir            string `json:"backupDir"`
}

type ManagerOptions struct {
	StateRoot        string
	WorkingDirectory string
	HomeDirectory    string
}

type Manager struct {
	paths Paths
}

// NewManager resolves the canonical state/runtime paths for one crawl output
// directory and ensures the backing directories exist before any save/load.
// It should stay side-effect-light beyond directory creation so restore
// decisions remain explicit in callers.
func NewManager(outputDir string, options ManagerOptions) (*Manager, error) {
	paths, err := ResolvePaths(outputDir, options)
	if err != nil {
		return nil, err
	}
	manager := &Manager{paths: paths}
	if err := manager.EnsureDirectories(); err != nil {
		return nil, err
	}
	return manager, nil
}

// ResolvePaths turns a crawl output identity into the runtime-state storage
// contract used by resume and validation.
func ResolvePaths(outputDir string, options ManagerOptions) (Paths, error) {
	// ResolvePaths turns an output directory identity into the on-disk contract
	// used by resume, validation, and backup files.
	resolvedOutputDir, err := resolveOutputDir(outputDir, options)
	if err != nil {
		return Paths{}, err
	}

	stateRoot, err := resolveStateRoot(options)
	if err != nil {
		return Paths{}, err
	}

	stateID := buildStateID(resolvedOutputDir)
	runtimeDir := filepath.Join(stateRoot, stateID)
	return Paths{
		OutputDir:            resolvedOutputDir,
		StateRoot:            stateRoot,
		RuntimeDir:           runtimeDir,
		TaskStatePath:        filepath.Join(runtimeDir, "task-state.json"),
		ValidationReportPath: filepath.Join(runtimeDir, "validation-report.json"),
		BackupDir:            filepath.Join(runtimeDir, "backups"),
	}, nil
}

func (m *Manager) Paths() Paths {
	if m == nil {
		return Paths{}
	}
	return m.paths
}

// EnsureDirectories materializes the runtime tree before any read/write call.
func (m *Manager) EnsureDirectories() error {
	if m == nil {
		return errors.New("task state manager is nil")
	}
	if err := os.MkdirAll(m.paths.OutputDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(m.paths.RuntimeDir, 0o755); err != nil {
		return err
	}
	return os.MkdirAll(m.paths.BackupDir, 0o755)
}

// LoadSnapshot reads the last persisted crawl state, if any. The caller owns
// all policy decisions about whether that snapshot is still safe to restore.
func (m *Manager) LoadSnapshot() (*Snapshot, error) {
	// LoadSnapshot is the restore entrypoint; it should stay read-only and let
	// callers decide whether the snapshot is still safe to trust.
	if m == nil {
		return nil, errors.New("task state manager is nil")
	}
	if !fileExists(m.paths.TaskStatePath) {
		return nil, nil
	}

	payload, err := os.ReadFile(m.paths.TaskStatePath)
	if err != nil {
		return nil, err
	}

	var snapshot Snapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

// SaveSnapshot writes the current runtime snapshot and optionally refreshes a
// single-file backup of the previous version. This keeps restore state local to
// the output directory identity while still giving operators a last-known copy.
func (m *Manager) SaveSnapshot(snapshot Snapshot, withBackup bool) error {
	// SaveSnapshot keeps the persisted runtime state in sync with the live run.
	// The optional backup preserves the previous version for rollback/review.
	if m == nil {
		return errors.New("task state manager is nil")
	}
	return m.writeJSON(m.paths.TaskStatePath, snapshot, withBackup)
}

// LoadValidationReport is the read-only entrypoint for post-run validation
// artifacts.
func (m *Manager) LoadValidationReport() (*ResultValidationReport, error) {
	if m == nil {
		return nil, errors.New("task state manager is nil")
	}
	if !fileExists(m.paths.ValidationReportPath) {
		return nil, nil
	}

	payload, err := os.ReadFile(m.paths.ValidationReportPath)
	if err != nil {
		return nil, err
	}

	var report ResultValidationReport
	if err := json.Unmarshal(payload, &report); err != nil {
		return nil, err
	}
	return &report, nil
}

// SaveValidationReport persists the post-run validation artifact.
func (m *Manager) SaveValidationReport(report ResultValidationReport) error {
	if m == nil {
		return errors.New("task state manager is nil")
	}
	return m.writeJSON(m.paths.ValidationReportPath, report, true)
}

// CleanupRuntimeState removes the persisted runtime directory after the run no
// longer needs resume data. Review artifacts in the user output directory are
// intentionally outside this cleanup scope.
// CleanupRuntimeState removes only the runtime-state bucket, not user output.
func (m *Manager) CleanupRuntimeState() error {
	if m == nil {
		return errors.New("task state manager is nil")
	}
	if !dirExists(m.paths.RuntimeDir) {
		return nil
	}
	return os.RemoveAll(m.paths.RuntimeDir)
}

// writeJSON is the shared persistence helper for snapshot/report files.
func (m *Manager) writeJSON(filePath string, data any, withBackup bool) error {
	if err := m.EnsureDirectories(); err != nil {
		return err
	}
	if withBackup {
		if err := m.createLatestBackup(filePath); err != nil {
			return err
		}
	}

	payload, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, payload, 0o644)
}

// createLatestBackup keeps one previous copy for rollback review.
func (m *Manager) createLatestBackup(filePath string) error {
	// Backups are a single-file safety net, not a second state tree.
	if !fileExists(filePath) {
		return nil
	}
	backupPath := filepath.Join(m.paths.BackupDir, filepath.Base(filePath)+".bak")
	return copyFile(filePath, backupPath)
}

// resolveOutputDir resolves the artifact directory identity before any state
// path is derived. If a restore path looks wrong, inspect this first.
func resolveOutputDir(outputDir string, options ManagerOptions) (string, error) {
	// Output directory resolution prefers explicit inputs first, then the caller
	// working directory, and only falls back to the current process root last.
	candidate := strings.TrimSpace(outputDir)
	if candidate == "" {
		candidate = strings.TrimSpace(options.WorkingDirectory)
	}
	if candidate == "" {
		workingDirectory, err := os.Getwd()
		if err != nil {
			return "", err
		}
		candidate = workingDirectory
	}
	return filepath.Abs(candidate)
}

// resolveStateRoot keeps resume state storage separate from the output tree so
// operators can relocate runtime state without rewriting artifacts.
func resolveStateRoot(options ManagerOptions) (string, error) {
	// State root resolution stays separate from output directory resolution so
	// resume state can be moved deliberately without changing artifact output.
	if candidate := strings.TrimSpace(options.StateRoot); candidate != "" {
		return filepath.Abs(candidate)
	}
	if envRoot := strings.TrimSpace(os.Getenv(StateRootEnvName)); envRoot != "" {
		return filepath.Abs(envRoot)
	}

	homeDirectory := strings.TrimSpace(options.HomeDirectory)
	if homeDirectory == "" {
		var err error
		homeDirectory, err = os.UserHomeDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Abs(filepath.Join(homeDirectory, ".jav-scrapy", "runtime-state"))
}

// buildStateID collapses one output directory into one runtime bucket.
func buildStateID(outputDir string) string {
	// buildStateID converts one output directory identity into one runtime bucket
	// so repeated runs do not collide in the persisted-state tree.
	normalizedOutput := strings.ToLower(strings.TrimSpace(outputDir))
	sum := sha1.Sum([]byte(normalizedOutput))
	return hex.EncodeToString(sum[:])[:16]
}

// fileExists is a narrow probe for persisted task-state files and backups.
func fileExists(targetPath string) bool {
	// Keep these tiny path probes local so state-manager behavior is easy to
	// inspect when a restore path goes wrong.
	info, err := os.Stat(strings.TrimSpace(targetPath))
	return err == nil && !info.IsDir()
}

// dirExists keeps directory cleanup checks local to this package.
func dirExists(targetPath string) bool {
	info, err := os.Stat(strings.TrimSpace(targetPath))
	return err == nil && info.IsDir()
}

// copyFile is used only for the latest single-file backup path.
func copyFile(srcPath string, targetPath string) error {
	sourceFile, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}

	targetFile, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	defer targetFile.Close()

	if _, err := targetFile.ReadFrom(sourceFile); err != nil {
		return err
	}
	return targetFile.Close()
}
