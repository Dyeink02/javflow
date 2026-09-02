package organizer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePathsForInputSeparatesMediaDirectoriesFromArtifactFiles(t *testing.T) {
	mediaRoot := t.TempDir()
	artifactRoot := t.TempDir()
	service := NewService()
	paths := service.ResolvePathsForInput(mediaRoot, artifactRoot)

	if paths.WaitingDir != filepath.Join(mediaRoot, waitingDirName) {
		t.Fatalf("waiting directory escaped media root: %s", paths.WaitingDir)
	}
	if paths.ToDeleteDir != filepath.Join(mediaRoot, toDeleteDirName) {
		t.Fatalf("delete directory escaped media root: %s", paths.ToDeleteDir)
	}
	if paths.ArtifactRootPath != artifactRoot {
		t.Fatalf("artifact root = %q, want %q", paths.ArtifactRootPath, artifactRoot)
	}
	if paths.LogsDir != filepath.Join(mediaRoot, logsDirName) {
		t.Fatalf("logs directory = %q, want media-root-owned logs", paths.LogsDir)
	}
	if paths.StateDir != filepath.Join(artifactRoot, stateDirName) {
		t.Fatalf("state directory = %q, want input-owned state", paths.StateDir)
	}
	for _, reportPath := range []string{
		paths.RenameMapPath,
		paths.UnmatchedPath,
		paths.AdRiskCodesPath,
		paths.AdRiskDetailPath,
		paths.AdRiskMagnetsPath,
		paths.MissingMagnetsPath,
	} {
		if filepath.Dir(reportPath) != paths.LogsDir {
			t.Fatalf("report path %q is not under logs directory %q", reportPath, paths.LogsDir)
		}
	}
}

func TestMigrateLegacyOrganizerArtifactsMovesOnlyKnownOutputs(t *testing.T) {
	mediaRoot := t.TempDir()
	artifactRoot := t.TempDir()
	service := NewService()
	paths := service.ResolvePathsForInput(mediaRoot, artifactRoot)

	legacyReport := filepath.Join(mediaRoot, "更新前后对照.txt")
	legacyMagnetReport := filepath.Join(mediaRoot, "合并奶头磁力.txt")
	userText := filepath.Join(mediaRoot, "用户笔记.txt")
	oldStateFile := filepath.Join(mediaRoot, stateDirName, "discovered-codes.json")
	for path, contents := range map[string]string{
		legacyReport:       "report",
		legacyMagnetReport: "legacy magnet report",
		userText:           "keep",
		oldStateFile:       "state",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := migrateLegacyOrganizerArtifacts(mediaRoot, paths, nil); err != nil {
		t.Fatalf("migrateLegacyOrganizerArtifacts returned error: %v", err)
	}
	if _, err := os.Stat(legacyReport); !os.IsNotExist(err) {
		t.Fatalf("legacy report remained at media root, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.LogsDir, "更新前后对照.txt")); err != nil {
		t.Fatalf("legacy report was not moved to logs: %v", err)
	}
	if _, err := os.Stat(legacyMagnetReport); !os.IsNotExist(err) {
		t.Fatalf("legacy magnet report remained at media root, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.LogsDir, "合并奶头磁力.txt")); err != nil {
		t.Fatalf("legacy magnet report was not moved to logs: %v", err)
	}
	if _, err := os.Stat(oldStateFile); !os.IsNotExist(err) {
		t.Fatalf("old state remained at media root, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.StateDir, "discovered-codes.json")); err != nil {
		t.Fatalf("state was not moved beside input: %v", err)
	}
	if _, err := os.Stat(userText); err != nil {
		t.Fatalf("unrelated user text was moved or removed: %v", err)
	}
}

func TestMigrateLegacyOrganizerArtifactsDoesNotOverwriteInputLog(t *testing.T) {
	mediaRoot := t.TempDir()
	artifactRoot := t.TempDir()
	service := NewService()
	paths := service.ResolvePathsForInput(mediaRoot, artifactRoot)
	if err := os.MkdirAll(paths.LogsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyReport := filepath.Join(mediaRoot, "更新前后对照.txt")
	destinationReport := filepath.Join(paths.LogsDir, "更新前后对照.txt")
	if err := os.WriteFile(legacyReport, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destinationReport, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := migrateLegacyOrganizerArtifacts(mediaRoot, paths, nil); err != nil {
		t.Fatalf("migrateLegacyOrganizerArtifacts returned error: %v", err)
	}
	contents, err := os.ReadFile(destinationReport)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "new" {
		t.Fatalf("existing input log was overwritten: %q", contents)
	}
	if _, err := os.Stat(legacyReport); !os.IsNotExist(err) {
		t.Fatalf("legacy report should be moved away from media root, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.LogsDir, "更新前后对照_DUP1.txt")); err != nil {
		t.Fatalf("legacy report was not retained under a unique name: %v", err)
	}
}

func TestCleanupLegacyReportFilesArchivesInsteadOfDeleting(t *testing.T) {
	root := t.TempDir()
	legacyPath := filepath.Join(root, legacyReportFileNames[0])
	if err := os.WriteFile(legacyPath, []byte("legacy report"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := cleanupLegacyReportFiles(root, nil); got != 1 {
		t.Fatalf("cleanupLegacyReportFiles moved %d files, want 1", got)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy report remained at root, stat err=%v", err)
	}
	archivedPath := filepath.Join(root, logsDirName, legacyReportFileNames[0])
	contents, err := os.ReadFile(archivedPath)
	if err != nil {
		t.Fatalf("archived report missing: %v", err)
	}
	if string(contents) != "legacy report" {
		t.Fatalf("archived report contents = %q, want original contents", contents)
	}
}
