package organizer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"javflow/internal/contracts/crawlartifact"
)

func TestResolveTargetPath(t *testing.T) {
	service := NewService()
	rootDir := t.TempDir()

	if got := service.ResolveTargetPath(rootDir, "waiting"); got != filepath.Join(rootDir, "\u5f85\u6574\u7406") {
		t.Fatalf("unexpected waiting path: %s", got)
	}

	if got := service.ResolveTargetPath(rootDir, "intro-ad"); got != filepath.Join(rootDir, "\u542b\u5f00\u5934\u5e7f\u544a") {
		t.Fatalf("unexpected intro-ad path: %s", got)
	}
}

func TestRunOrganizerRejectsFilesystemRoot(t *testing.T) {
	service := NewService()
	rootPath := filepath.VolumeName(t.TempDir()) + string(os.PathSeparator)
	if rootPath == string(os.PathSeparator) {
		// The POSIX root is always present; on Windows the temp directory volume
		// gives the active drive root such as C:\\.
		rootPath = string(os.PathSeparator)
	}
	_, err := service.RunOrganizer(RunOptions{RootPath: rootPath})
	if err == nil || !strings.Contains(err.Error(), "\u4e0d\u80fd\u76f4\u63a5\u9009\u62e9") {
		t.Fatalf("expected filesystem root rejection, got %v", err)
	}
}

func TestParseConflictSuffixStrategyUsesOnlyFixedOptions(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{input: "-A", want: []string{"-A", "-B"}},
		{input: "-1", want: []string{"-1", "-2"}},
		{input: "_DUP1", want: []string{"_DUP1", "_DUP2"}},
		{input: "-C", want: []string{"-A", "-B"}},
		{input: "custom", want: []string{"-A", "-B"}},
	}

	for _, tc := range cases {
		strategy, err := parseConflictSuffixStrategy(tc.input)
		if err != nil {
			t.Fatalf("parseConflictSuffixStrategy(%q) failed: %v", tc.input, err)
		}
		for index, want := range tc.want {
			if got := formatSuffix(strategy, index); got != want {
				t.Errorf("formatSuffix(%q, %d) = %q, want %q", tc.input, index, got, want)
			}
		}
	}
}

func writeSparseFile(t *testing.T, filePath string, size int64) {
	t.Helper()
	file, err := os.Create(filePath)
	if err != nil {
		t.Fatalf("create sparse file: %v", err)
	}
	if err := file.Truncate(size); err != nil {
		_ = file.Close()
		t.Fatalf("truncate sparse file: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close sparse file: %v", err)
	}
}

func TestRunOrganizerMovesQualifiedVideoAndDeletesSourceFolder(t *testing.T) {
	service := NewService()
	rootDir := t.TempDir()
	sourceDir := filepath.Join(rootDir, "ABP-889")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	largeVideo := filepath.Join(sourceDir, "domain@ABP-889.mp4")
	smallVideo := filepath.Join(sourceDir, "ad.mp4")
	writeSparseFile(t, largeVideo, 2*1024*1024)
	if err := os.WriteFile(smallVideo, []byte("ad"), 0o644); err != nil {
		t.Fatalf("write small video: %v", err)
	}

	result, err := service.RunOrganizer(RunOptions{
		RootPath:              rootDir,
		MinSizeMB:             1,
		VideoExtensions:       "mp4, iso",
		AdFileAction:          adFileActionDeleteDirectly,
		IncludeSubdirectories: true,
		StrictExpectedCodes:   true,
		ExpectedCodes:         []string{"ABP-889"},
		ExpectedCodeEntries:   []CodeEntry{{Code: "ABP-889", Magnets: []MagnetEntry{{Link: "magnet:?xt=urn:btih:AAA"}}}},
		AdDetectionEnabled:    false,
	})
	if err != nil {
		t.Fatalf("RunOrganizer returned error: %v", err)
	}

	if result.Summary.MovedToWaiting != 1 {
		t.Fatalf("expected 1 moved to waiting, got %d", result.Summary.MovedToWaiting)
	}
	if result.Summary.DeletedDirectly != 1 {
		t.Fatalf("expected 1 direct delete, got %d", result.Summary.DeletedDirectly)
	}
	if _, err := os.Stat(filepath.Join(rootDir, "\u5f85\u6574\u7406", "ABP-889.mp4")); err != nil {
		t.Fatalf("expected renamed waiting video: %v", err)
	}
	if _, err := os.Stat(sourceDir); !os.IsNotExist(err) {
		t.Fatalf("expected source folder to be removed, stat err=%v", err)
	}
	for _, reportPath := range result.ReportFiles {
		if _, err := os.Stat(reportPath); err != nil {
			t.Fatalf("expected report file %s: %v", reportPath, err)
		}
	}
}

func TestRunOrganizerDirectDeletePreservesSoftwareArtifactInSourceFolder(t *testing.T) {
	service := NewService()
	rootDir := t.TempDir()
	sourceDir := filepath.Join(rootDir, "ABP-990")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	writeSparseFile(t, filepath.Join(sourceDir, "ABP-990.mp4"), 2*1024*1024)
	if err := os.WriteFile(filepath.Join(sourceDir, "ad.mp4"), []byte("ad"), 0o644); err != nil {
		t.Fatalf("write small video: %v", err)
	}
	artifactPath := filepath.Join(sourceDir, "filmData.json")
	if err := os.WriteFile(artifactPath, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write crawler artifact: %v", err)
	}

	result, err := service.RunOrganizer(RunOptions{
		RootPath:              rootDir,
		MinSizeMB:             1,
		VideoExtensions:       "mp4",
		AdFileAction:          adFileActionDeleteDirectly,
		IncludeSubdirectories: true,
		StrictExpectedCodes:   true,
		ExpectedCodes:         []string{"ABP-990"},
		AdDetectionEnabled:    false,
	})
	if err != nil {
		t.Fatalf("RunOrganizer returned error: %v", err)
	}
	if result.Summary.DeletedDirectly != 1 {
		t.Fatalf("expected only the classified ad to be deleted, got %d", result.Summary.DeletedDirectly)
	}
	if _, err := os.Stat(artifactPath); err != nil {
		t.Fatalf("expected crawler artifact to be preserved: %v", err)
	}
	if _, err := os.Stat(sourceDir); err != nil {
		t.Fatalf("expected source folder to remain for preserved artifact: %v", err)
	}
}

func TestRunOrganizerMatchesMagnetDisplayNameAliasInStrictMode(t *testing.T) {
	service := NewService()
	rootDir := t.TempDir()
	sourceDir := filepath.Join(rootDir, "MXGS1121")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	videoPath := filepath.Join(sourceDir, "mxgs01121.mp4")
	writeSparseFile(t, videoPath, 2*1024*1024)

	result, err := service.RunOrganizer(RunOptions{
		RootPath:              rootDir,
		MinSizeMB:             1,
		VideoExtensions:       "mp4",
		AdFileAction:          adFileActionDeleteDirectly,
		IncludeSubdirectories: true,
		StrictExpectedCodes:   true,
		ExpectedCodes:         []string{"MXGS-112"},
		ExpectedCodeEntries: []CodeEntry{{
			Code:    "MXGS-112",
			Magnets: []MagnetEntry{{Link: "magnet:?xt=urn:btih:AAA&dn=MXGS1121"}},
		}},
		AdDetectionEnabled: false,
	})
	if err != nil {
		t.Fatalf("RunOrganizer returned error: %v", err)
	}
	if result.Summary.MatchedToCrawlCode != 1 {
		t.Fatalf("expected one canonical crawl match, got %d", result.Summary.MatchedToCrawlCode)
	}
	if result.Summary.MovedToWaiting != 1 {
		t.Fatalf("expected one waiting move, got %d", result.Summary.MovedToWaiting)
	}
	if _, err := os.Stat(filepath.Join(rootDir, "待整理", "MXGS-112.mp4")); err != nil {
		t.Fatalf("expected canonical renamed video: %v", err)
	}
	if len(result.Preview.UnmatchedRecords) != 0 {
		t.Fatalf("expected no unmatched records, got %+v", result.Preview.UnmatchedRecords)
	}
}

func TestRunOrganizerPreservesRootLevelSmallVideo(t *testing.T) {
	service := NewService()
	rootDir := t.TempDir()
	rootSmallVideo := filepath.Join(rootDir, "small-ad.mp4")
	if err := os.WriteFile(rootSmallVideo, []byte("ad"), 0o644); err != nil {
		t.Fatalf("write root small video: %v", err)
	}

	result, err := service.RunOrganizer(RunOptions{
		RootPath:              rootDir,
		MinSizeMB:             1,
		VideoExtensions:       "mp4",
		AdFileAction:          adFileActionDeleteDirectly,
		IncludeSubdirectories: true,
		StrictExpectedCodes:   false,
		AdDetectionEnabled:    false,
	})
	if err != nil {
		t.Fatalf("RunOrganizer returned error: %v", err)
	}

	if result.Summary.DeletedDirectly != 0 {
		t.Fatalf("expected no direct delete for root-level file, got %d", result.Summary.DeletedDirectly)
	}
	if _, err := os.Stat(rootSmallVideo); err != nil {
		t.Fatalf("expected root-level small video to be preserved: %v", err)
	}
}

func TestRunOrganizerMovesIntroAdAfterWaitingRename(t *testing.T) {
	service := NewService()
	rootDir := t.TempDir()
	sourceDir := filepath.Join(rootDir, "ABF-055")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	video := filepath.Join(sourceDir, "ABF-055.mp4")
	writeSparseFile(t, video, 2*1024*1024)

	result, err := service.RunOrganizer(RunOptions{
		RootPath:              rootDir,
		MinSizeMB:             1,
		VideoExtensions:       "mp4",
		AdFileAction:          adFileActionMoveToDelete,
		IncludeSubdirectories: true,
		StrictExpectedCodes:   true,
		ExpectedCodes:         []string{"ABF-055"},
		AdDetectionEnabled:    true,
		AdThreshold:           60,
		EvaluateAdRisk: func(request AdRiskRequest) (AdRiskResult, error) {
			if filepath.Base(request.VideoPath) != "ABF-055.mp4" {
				t.Fatalf("expected evaluator to receive renamed waiting file, got %s", request.VideoPath)
			}
			return AdRiskResult{
				IsAd:      true,
				Score:     88,
				Threshold: 60,
				Reasons:   []string{"测试命中广告样本"},
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("RunOrganizer returned error: %v", err)
	}

	if result.Summary.MovedToWaiting != 0 {
		t.Fatalf("expected waiting count to be decremented after intro ad move, got %d", result.Summary.MovedToWaiting)
	}
	if result.Summary.MovedToIntroAd != 1 {
		t.Fatalf("expected 1 intro ad, got %d", result.Summary.MovedToIntroAd)
	}
	if _, err := os.Stat(filepath.Join(rootDir, "\u542b\u5f00\u5934\u5e7f\u544a", "ABF-055.mp4")); err != nil {
		t.Fatalf("expected intro ad file: %v", err)
	}
}

func TestRunOrganizerNonBatchMoveToDeleteMovesOnlyClassifiedFiles(t *testing.T) {
	service := NewService()
	rootDir := t.TempDir()
	sourceDir := filepath.Join(rootDir, "MIRD-237")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}

	largeVideo := filepath.Join(sourceDir, "MIRD-237.mp4")
	smallVideo := filepath.Join(sourceDir, "promo.mp4")
	if err := os.WriteFile(smallVideo, []byte("ad"), 0o644); err != nil {
		t.Fatalf("write small video: %v", err)
	}
	writeSparseFile(t, largeVideo, 2*1024*1024)

	result, err := service.RunOrganizer(RunOptions{
		RootPath:              rootDir,
		MinSizeMB:             1,
		VideoExtensions:       "mp4",
		AdFileAction:          adFileActionMoveToDelete,
		IncludeSubdirectories: true,
		StrictExpectedCodes:   true,
		ExpectedCodes:         []string{"MIRD-237"},
		AdDetectionEnabled:    false,
	})
	if err != nil {
		t.Fatalf("RunOrganizer returned error: %v", err)
	}

	if result.Summary.MovedToWaiting != 1 {
		t.Fatalf("expected 1 moved to waiting, got %d", result.Summary.MovedToWaiting)
	}
	if result.Summary.MovedToDelete != 1 {
		t.Fatalf("expected 1 moved to delete, got %d", result.Summary.MovedToDelete)
	}
	if _, err := os.Stat(filepath.Join(rootDir, "待整理", "MIRD-237.mp4")); err != nil {
		t.Fatalf("expected waiting video to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rootDir, "待删除", "promo.mp4")); err != nil {
		t.Fatalf("expected classified ad file to move individually in non-batch mode: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rootDir, "待删除", "MIRD-237")); !os.IsNotExist(err) {
		t.Fatalf("non-batch mode must not move the whole source folder, stat err=%v", err)
	}
	if _, err := os.Stat(sourceDir); !os.IsNotExist(err) {
		t.Fatalf("expected original source folder to be moved away, stat err=%v", err)
	}
}

func TestRunOrganizerRenamesDetectedCodeOutsideExpectedListWhenStrictMatchingDisabled(t *testing.T) {
	service := NewService()
	rootDir := t.TempDir()
	sourceDir := filepath.Join(rootDir, "[FHD]FSET-739")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}

	videoA := filepath.Join(sourceDir, "1fset00739hhb1.mp4")
	videoB := filepath.Join(sourceDir, "1fset00739hhb2.mp4")
	writeSparseFile(t, videoA, 2*1024*1024)
	writeSparseFile(t, videoB, 2*1024*1024)

	result, err := service.RunOrganizer(RunOptions{
		RootPath:              rootDir,
		MinSizeMB:             1,
		VideoExtensions:       "mp4",
		AdFileAction:          adFileActionMoveToDelete,
		IncludeSubdirectories: true,
		StrictExpectedCodes:   false,
		ExpectedCodes:         []string{"ABP-889"},
		AdDetectionEnabled:    false,
		Suffix:                "-A",
	})
	if err != nil {
		t.Fatalf("RunOrganizer returned error: %v", err)
	}

	if result.Summary.MovedToWaiting != 2 {
		t.Fatalf("expected 2 moved to waiting, got %d", result.Summary.MovedToWaiting)
	}
	if _, err := os.Stat(filepath.Join(rootDir, "待整理", "FSET-739-A.mp4")); err != nil {
		t.Fatalf("expected first renamed waiting file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rootDir, "待整理", "FSET-739-B.mp4")); err != nil {
		t.Fatalf("expected second renamed waiting file: %v", err)
	}
	if len(result.Preview.RenameRecords) < 2 {
		t.Fatalf("expected rename preview records, got %d", len(result.Preview.RenameRecords))
	}
	for _, record := range result.Preview.RenameRecords {
		if record.FilmCode != "FSET-739" {
			t.Fatalf("expected normalized film code FSET-739, got %+v", record)
		}
		if !record.RenameApplied {
			t.Fatalf("expected renameApplied=true for detected code outside expected list, got %+v", record)
		}
	}
}

func TestRunOrganizerBatchDeleteRunsAfterStrictMatchingAndPreservesManagedOutputs(t *testing.T) {
	service := NewService()
	rootDir := t.TempDir()

	validDir := filepath.Join(rootDir, "DASS-287-source")
	trashDir := filepath.Join(rootDir, "small-trash")
	unlistedDir := filepath.Join(rootDir, "FSET-739-unlisted")
	for _, dir := range []string{validDir, trashDir, unlistedDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeSparseFile(t, filepath.Join(validDir, "DASS-287-one.mp4"), 2*1024*1024)
	writeSparseFile(t, filepath.Join(validDir, "DASS-287-two.mp4"), 2*1024*1024)
	writeSparseFile(t, filepath.Join(unlistedDir, "FSET-739.mp4"), 2*1024*1024)
	if err := os.WriteFile(filepath.Join(validDir, "ad.txt"), []byte("ad"), 0o644); err != nil {
		t.Fatal(err)
	}
	managedNestedDir := filepath.Join(validDir, ".video-organizer-cache")
	if err := os.MkdirAll(managedNestedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	emptyNestedDir := filepath.Join(validDir, "empty", "deeper")
	if err := os.MkdirAll(emptyNestedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(managedNestedDir, "state.json"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	logNestedDir := filepath.Join(validDir, "diagnostics")
	if err := os.MkdirAll(logNestedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logNestedPath := filepath.Join(logNestedDir, "organizer-run.log")
	if err := os.WriteFile(logNestedPath, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	doubleExtensionLogPath := filepath.Join(logNestedDir, "task-run.log.txt")
	if err := os.WriteFile(doubleExtensionLogPath, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(validDir, "filmData.json"), []byte("software artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(trashDir, "promo.mp4"), []byte("ad"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootJunk := filepath.Join(rootDir, "root-junk.txt")
	if err := os.WriteFile(rootJunk, []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, artifactName := range []string{"filmData.json", "crawl-profile.json", "magnet-links.txt"} {
		if err := os.WriteFile(filepath.Join(rootDir, artifactName), []byte("software artifact"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	softwareStateDir := filepath.Join(rootDir, ".video-organizer-generated")
	if err := os.MkdirAll(softwareStateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(softwareStateDir, "state.json"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	paths := service.ResolvePaths(rootDir)
	for _, dir := range []string{paths.LogsDir, paths.StateDir, paths.ToDeleteDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	logPath := filepath.Join(paths.LogsDir, "organizer.log")
	if err := os.WriteFile(logPath, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	toDeleteMarker := filepath.Join(paths.ToDeleteDir, "keep.txt")
	if err := os.WriteFile(toDeleteMarker, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := service.RunOrganizer(RunOptions{
		RootPath:              rootDir,
		MinSizeMB:             1,
		VideoExtensions:       "mp4",
		AdFileAction:          adFileActionDeleteDirectly,
		BatchDelete:           true,
		IncludeSubdirectories: true,
		StrictExpectedCodes:   true,
		ExpectedCodes:         []string{"DASS-287"},
		Suffix:                "-A",
		AdDetectionEnabled:    false,
	})
	if err != nil {
		t.Fatalf("RunOrganizer returned error: %v", err)
	}

	if result.Summary.MovedToWaiting != 2 {
		t.Fatalf("expected both matching videos in waiting, got %d", result.Summary.MovedToWaiting)
	}
	if result.Summary.DeletedDirectly != 2 {
		t.Fatalf("expected 2 classified trash files deleted in one batch, got %d", result.Summary.DeletedDirectly)
	}
	for _, name := range []string{"DASS-287-A.mp4", "DASS-287-B.mp4"} {
		if _, err := os.Stat(filepath.Join(paths.WaitingDir, name)); err != nil {
			t.Fatalf("expected split output %s: %v", name, err)
		}
	}
	if _, err := os.Stat(validDir); err != nil {
		t.Fatalf("expected source directory to remain because it contains a software cache: %v", err)
	}
	if _, err := os.Stat(filepath.Join(validDir, "ad.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected explicitly classified ad file to be removed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(managedNestedDir, "state.json")); err != nil {
		t.Fatalf("expected nested software cache to be preserved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(validDir, "filmData.json")); err != nil {
		t.Fatalf("expected nested crawler artifact to be preserved: %v", err)
	}
	if _, err := os.Stat(logNestedPath); err != nil {
		t.Fatalf("expected nested log file to be preserved: %v", err)
	}
	if _, err := os.Stat(doubleExtensionLogPath); err != nil {
		t.Fatalf("expected .log.txt task log to be preserved: %v", err)
	}
	if _, err := os.Stat(emptyNestedDir); !os.IsNotExist(err) {
		t.Fatalf("expected nested empty directory to be cleaned, stat err=%v", err)
	}
	for _, removedPath := range []string{trashDir} {
		if _, err := os.Stat(removedPath); !os.IsNotExist(err) {
			t.Fatalf("expected batch-deleted path %s, stat err=%v", removedPath, err)
		}
	}
	if _, err := os.Stat(unlistedDir); !os.IsNotExist(err) {
		t.Fatalf("expected empty unmatched source directory to be cleaned, stat err=%v", err)
	}
	unmatchedVideo := filepath.Join(paths.UnmatchedDir, "FSET-739.mp4")
	preservedArtifacts := []string{
		rootJunk,
		filepath.Join(rootDir, "filmData.json"),
		filepath.Join(rootDir, "crawl-profile.json"),
		filepath.Join(rootDir, "magnet-links.txt"),
		softwareStateDir,
		filepath.Join(softwareStateDir, "state.json"),
	}
	for _, preservedPath := range append([]string{paths.WaitingDir, paths.UnmatchedDir, unmatchedVideo, paths.IntroAdDir, paths.LogsDir, paths.StateDir, paths.ToDeleteDir, logPath, toDeleteMarker}, preservedArtifacts...) {
		if _, err := os.Stat(preservedPath); err != nil {
			t.Fatalf("expected preserved path %s: %v", preservedPath, err)
		}
	}
	for _, reportPath := range result.ReportFiles {
		if _, err := os.Stat(reportPath); err != nil {
			t.Fatalf("expected preserved report %s: %v", reportPath, err)
		}
	}
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), batchDeleteStagingPrefix) {
			t.Fatalf("batch-delete staging directory leaked after success: %s", entry.Name())
		}
	}
	for _, record := range result.Preview.RenameRecords {
		if record.FilmCode == "FSET-739" {
			t.Fatalf("strictly unmatched video must not be organized: %+v", record)
		}
	}
}

func TestRunOrganizerStrictFuzzyMatchesParentAndRoutesNearMissToUnmatched(t *testing.T) {
	service := NewService()
	rootDir := t.TempDir()
	matchedDir := filepath.Join(rootDir, "1818@tl1")
	nearMissDir := filepath.Join(rootDir, "TL-0010")
	for _, dir := range []string{matchedDir, nearMissDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeSparseFile(t, filepath.Join(matchedDir, "video.mp4"), 2*1024*1024)
	writeSparseFile(t, filepath.Join(nearMissDir, "other.mp4"), 2*1024*1024)

	result, err := service.RunOrganizer(RunOptions{
		RootPath:              rootDir,
		MinSizeMB:             1,
		VideoExtensions:       "mp4",
		AdFileAction:          adFileActionMoveToDelete,
		IncludeSubdirectories: true,
		StrictExpectedCodes:   true,
		ExpectedCodes:         []string{"TL-001"},
		Suffix:                "-A",
	})
	if err != nil {
		t.Fatalf("RunOrganizer returned error: %v", err)
	}
	paths := service.ResolvePaths(rootDir)
	if _, err := os.Stat(filepath.Join(paths.WaitingDir, "TL-001.mp4")); err != nil {
		t.Fatalf("expected parent-folder fuzzy match to use TL-001: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.UnmatchedDir, "other.mp4")); err != nil {
		t.Fatalf("expected TL-0010 near miss in unmatched: %v", err)
	}
	if result.Summary.MovedToWaiting != 1 || result.Summary.MovedToUnmatched != 1 {
		t.Fatalf("unexpected strict fuzzy summary: %+v", result.Summary)
	}
	if _, err := os.Stat(paths.RenameHistoryPath); err != nil {
		t.Fatalf("expected persistent hidden rename history: %v", err)
	}
}

func TestLoadCrawlFilmCodes(t *testing.T) {
	service := NewService()
	outputDir := t.TempDir()

	payload := map[string]any{
		"records": []map[string]any{
			{
				"filmCode":    "abp889",
				"magnetLinks": []map[string]any{{"link": "magnet:?xt=urn:btih:AAA", "size": "2.1GB"}},
			},
			{
				"sourceLink": "https://www.javbus.com/ABP-889",
				"magnets":    []string{"magnet:?xt=urn:btih:AAA", "magnet:?xt=urn:btih:BBB"},
			},
			{
				"title":  "SSIS-123 sample",
				"magnet": "magnet:?xt=urn:btih:CCC",
			},
		},
	}

	contents, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	filmDataPath := filepath.Join(outputDir, crawlartifact.CrawlFilmDataFile)
	if err := os.WriteFile(filmDataPath, contents, 0o644); err != nil {
		t.Fatalf("write filmData.json: %v", err)
	}
	magnetPath := filepath.Join(outputDir, crawlartifact.DefaultMagnetTxt)
	if err := os.WriteFile(magnetPath, []byte("magnet:?xt=urn:btih:AAA\r\nmagnet:?xt=urn:btih:BBB\r\nfiltered-code\r\nmagnet:?xt=urn:btih:AAA\r\n"), 0o644); err != nil {
		t.Fatalf("write magnet-links.txt: %v", err)
	}

	result, err := service.LoadCrawlFilmCodes(outputDir)
	if err != nil {
		t.Fatalf("LoadCrawlFilmCodes returned error: %v", err)
	}

	if result.CodeCount != 2 {
		t.Fatalf("expected 2 codes, got %d", result.CodeCount)
	}
	if result.ActualMagnetCount != 2 {
		t.Fatalf("expected 2 actual magnet outputs, got %d", result.ActualMagnetCount)
	}
	if result.MagnetPath != magnetPath {
		t.Fatalf("expected magnet path %q, got %q", magnetPath, result.MagnetPath)
	}

	if len(result.CodeEntries) != 2 {
		t.Fatalf("expected 2 code entries, got %d", len(result.CodeEntries))
	}

	if result.CodeEntries[0].Code != "ABP-889" {
		t.Fatalf("expected first code ABP-889, got %s", result.CodeEntries[0].Code)
	}

	if len(result.CodeEntries[0].Magnets) != 2 {
		t.Fatalf("expected merged magnets for ABP-889, got %d", len(result.CodeEntries[0].Magnets))
	}
	if result.PreloadedExpected.SourceType != codeSourceFilmData {
		t.Fatalf("expected preloaded sourceType filmData, got %q", result.PreloadedExpected.SourceType)
	}
	if result.PreloadedExpected.SourcePath != filmDataPath {
		t.Fatalf("expected preloaded sourcePath %q, got %q", filmDataPath, result.PreloadedExpected.SourcePath)
	}
}

func TestLoadCrawlFilmCodesPrefersOrganizerCodesArtifact(t *testing.T) {
	service := NewService()
	outputDir := t.TempDir()

	artifact := crawlartifact.OrganizerCodesArtifact{
		SchemaVersion:   crawlartifact.CurrentSchemaVersion,
		RunID:           "crawl-test-run",
		CompletedAt:     "2026-05-05T10:20:30Z",
		ActressName:     "结城りの",
		OutputDir:       outputDir,
		FilmDataPath:    filepath.Join(outputDir, crawlartifact.CrawlFilmDataFile),
		TotalRecords:    243,
		UniqueCodeCount: 2,
		Codes:           []string{"ABP-889", "SSIS-123"},
		CodeEntries: []crawlartifact.CodeEntry{
			{
				Code:  "ABP-889",
				Title: "ABP-889 sample",
				Magnets: []crawlartifact.MagnetEntry{
					{Link: "magnet:?xt=urn:btih:AAA", Size: "2.1GB"},
				},
			},
			{
				Code: "SSIS-123",
			},
		},
	}

	payload, err := json.Marshal(artifact)
	if err != nil {
		t.Fatalf("marshal artifact: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, crawlartifact.OrganizerCodesFile), payload, 0o644); err != nil {
		t.Fatalf("write organizer-codes.json: %v", err)
	}

	result, err := service.LoadCrawlFilmCodes(outputDir)
	if err != nil {
		t.Fatalf("LoadCrawlFilmCodes returned error: %v", err)
	}
	if result.SourceType != codeSourceOrganizerCodes {
		t.Fatalf("expected sourceType organizerCodes, got %q", result.SourceType)
	}
	if result.ActressName != "结城りの" {
		t.Fatalf("expected actressName 结城りの, got %q", result.ActressName)
	}
	if result.OrganizerCodesPath == "" {
		t.Fatalf("expected organizerCodesPath to be populated")
	}
	if result.CodeCount != 2 || len(result.CodeEntries) != 2 {
		t.Fatalf("unexpected code load result: %#v", result)
	}
	if result.PreloadedExpected.SourceType != codeSourceOrganizerCodes {
		t.Fatalf("expected preloaded sourceType organizerCodes, got %q", result.PreloadedExpected.SourceType)
	}
	if result.PreloadedExpected.SourcePath != result.OrganizerCodesPath {
		t.Fatalf("expected preloaded sourcePath %q, got %q", result.OrganizerCodesPath, result.PreloadedExpected.SourcePath)
	}
}

func TestLoadCrawlFilmCodesFallsBackToFilmDataMagnetLinks(t *testing.T) {
	service := NewService()
	outputDir := t.TempDir()
	payload := []map[string]any{
		{
			"title":             "ABP-001 sample",
			"magnetLinks":       []map[string]any{{"link": "magnet:?xt=urn:btih:AAA"}},
			"backupMagnetLinks": []map[string]any{{"link": "magnet:?xt=urn:btih:BACKUP"}},
		},
		{
			"title":                  "ABP-002 filtered",
			"filteredByActressCount": true,
			"magnetLinks":            []map[string]any{{"link": "magnet:?xt=urn:btih:FILTERED"}},
		},
		{
			"title":       "ABP-003 sample",
			"magnetLinks": []map[string]any{{"link": "magnet:?xt=urn:btih:AAA"}},
			"magnet":      "magnet:?xt=urn:btih:CCC",
		},
	}
	contents, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal filmData fallback payload: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, crawlartifact.CrawlFilmDataFile), contents, 0o644); err != nil {
		t.Fatalf("write filmData fallback payload: %v", err)
	}

	result, err := service.LoadCrawlFilmCodes(outputDir)
	if err != nil {
		t.Fatalf("LoadCrawlFilmCodes fallback returned error: %v", err)
	}
	if result.ActualMagnetCount != 2 {
		t.Fatalf("expected filtered and duplicate-safe fallback count 2, got %d", result.ActualMagnetCount)
	}
}

func TestLoadCrawlFilmCodesAcceptsLegacyOrganizerArtifactWithoutSchemaVersion(t *testing.T) {
	service := NewService()
	outputDir := t.TempDir()
	normalizedOutputDir := filepath.ToSlash(outputDir)
	normalizedFilmDataPath := filepath.ToSlash(filepath.Join(outputDir, crawlartifact.CrawlFilmDataFile))

	payload := []byte(`{
  "runId": "legacy-run",
  "completedAt": "2026-05-05T10:20:30Z",
  "actressName": "结城りの",
  "outputDir": "` + normalizedOutputDir + `",
  "filmDataPath": "` + normalizedFilmDataPath + `",
  "totalRecords": 243,
  "uniqueCodeCount": 1,
  "codes": ["ABP-889"],
  "codeEntries": [
    {
      "code": "ABP-889",
      "title": "ABP-889 sample"
    }
  ]
}`)

	if err := os.WriteFile(filepath.Join(outputDir, crawlartifact.OrganizerCodesFile), payload, 0o644); err != nil {
		t.Fatalf("write legacy organizer-codes.json: %v", err)
	}

	result, err := service.LoadCrawlFilmCodes(outputDir)
	if err != nil {
		t.Fatalf("LoadCrawlFilmCodes returned error for legacy artifact: %v", err)
	}
	if result.SourceType != codeSourceOrganizerCodes {
		t.Fatalf("expected sourceType organizerCodes, got %q", result.SourceType)
	}
	if result.CodeCount != 1 || len(result.Codes) != 1 || result.Codes[0] != "ABP-889" {
		t.Fatalf("unexpected legacy artifact result: %#v", result)
	}
}

func TestResolvePreloadedExpectedCodesFallsBackToCrawlOutputDir(t *testing.T) {
	service := NewService()
	outputDir := t.TempDir()

	payload := map[string]any{
		"records": []map[string]any{
			{
				"filmCode":    "dazd277",
				"magnetLinks": []map[string]any{{"link": "magnet:?xt=urn:btih:AAA", "size": "3.1GB"}},
			},
		},
	}

	contents, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	filmDataPath := filepath.Join(outputDir, crawlartifact.CrawlFilmDataFile)
	if err := os.WriteFile(filmDataPath, contents, 0o644); err != nil {
		t.Fatalf("write filmData.json: %v", err)
	}

	resolved, err := service.ResolvePreloadedExpectedCodes(RunOptions{
		CrawlOutputDir: outputDir,
	})
	if err != nil {
		t.Fatalf("ResolvePreloadedExpectedCodes returned error: %v", err)
	}
	if resolved.SourceType != codeSourceFilmData {
		t.Fatalf("expected sourceType filmData, got %q", resolved.SourceType)
	}
	if resolved.CodeCount != 1 || len(resolved.Codes) != 1 || resolved.Codes[0] != "DAZD-277" {
		t.Fatalf("unexpected resolved codes: %#v", resolved)
	}
	if resolved.SourcePath != filmDataPath {
		t.Fatalf("expected sourcePath %q, got %q", filmDataPath, resolved.SourcePath)
	}
}

func TestResolvePreloadedExpectedCodesKeepsExplicitPayloadSnapshot(t *testing.T) {
	service := NewService()

	resolved, err := service.ResolvePreloadedExpectedCodes(RunOptions{
		CrawlOutputDir: "C:\\crawl-output",
		PreloadedExpected: PreloadedExpectedCodes{
			SourceType:         codeSourcePayload,
			SourcePath:         "ui-cache",
			FilmDataPath:       "C:\\crawl-output\\filmData.json",
			OrganizerCodesPath: "C:\\crawl-output\\organizer-codes.json",
			Codes:              []string{"abp889", "ABP-889"},
			CodeEntries: []CodeEntry{
				{Code: "abp889", Magnets: []MagnetEntry{{Link: "magnet:?xt=urn:btih:AAA"}}},
			},
		},
	})
	if err != nil {
		t.Fatalf("ResolvePreloadedExpectedCodes returned error: %v", err)
	}
	if resolved.SourceType != codeSourcePayload {
		t.Fatalf("expected sourceType payload, got %q", resolved.SourceType)
	}
	if resolved.CodeCount != 1 || len(resolved.Codes) != 1 || resolved.Codes[0] != "ABP-889" {
		t.Fatalf("unexpected normalized payload result: %#v", resolved)
	}
	if resolved.OutputDir != "C:\\crawl-output" {
		t.Fatalf("expected outputDir to be preserved, got %q", resolved.OutputDir)
	}
	if resolved.SourcePath != "ui-cache" {
		t.Fatalf("expected explicit payload sourcePath to be preserved, got %q", resolved.SourcePath)
	}
}

func TestResolvePreloadedExpectedCodesUsesLegacyFieldsOnlyAsSupplement(t *testing.T) {
	service := NewService()

	resolved, err := service.ResolvePreloadedExpectedCodes(RunOptions{
		CrawlOutputDir: "C:\\crawl-output",
		ExpectedCodes:  []string{"SSIS-123"},
		PreloadedExpected: PreloadedExpectedCodes{
			SourceType:         codeSourceFilmData,
			SourcePath:         "C:\\crawl-output\\filmData.json",
			FilmDataPath:       "C:\\crawl-output\\filmData.json",
			OrganizerCodesPath: "C:\\crawl-output\\organizer-codes.json",
			Codes:              []string{"ABP-889"},
		},
	})
	if err != nil {
		t.Fatalf("ResolvePreloadedExpectedCodes returned error: %v", err)
	}
	if resolved.SourceType != codeSourceFilmData {
		t.Fatalf("expected sourceType filmData, got %q", resolved.SourceType)
	}
	if resolved.SourcePath != "C:\\crawl-output\\filmData.json" {
		t.Fatalf("expected explicit filmData sourcePath to stay primary, got %q", resolved.SourcePath)
	}
	if resolved.OutputDir != "C:\\crawl-output" {
		t.Fatalf("expected outputDir to be preserved, got %q", resolved.OutputDir)
	}
	if resolved.CodeCount != 2 || len(resolved.Codes) != 2 {
		t.Fatalf("expected merged codes, got %#v", resolved)
	}
	if resolved.Codes[0] != "ABP-889" || resolved.Codes[1] != "SSIS-123" {
		t.Fatalf("expected sorted merged codes, got %#v", resolved.Codes)
	}
}

func TestResolvePreloadedExpectedCodesDoesNotReReadArtifactsWhenPreloadedAlreadyPresent(t *testing.T) {
	service := NewService()
	outputDir := t.TempDir()

	resolved, err := service.ResolvePreloadedExpectedCodes(RunOptions{
		CrawlOutputDir: outputDir,
		PreloadedExpected: PreloadedExpectedCodes{
			SourceType: codeSourcePayload,
			SourcePath: "ui-cache",
			Codes:      []string{"ABP-889"},
			CodeEntries: []CodeEntry{
				{Code: "ABP-889", Magnets: []MagnetEntry{{Link: "magnet:?xt=urn:btih:AAA"}}},
			},
		},
	})
	if err != nil {
		t.Fatalf("ResolvePreloadedExpectedCodes returned error: %v", err)
	}
	if resolved.SourceType != codeSourcePayload {
		t.Fatalf("expected sourceType payload, got %q", resolved.SourceType)
	}
	if resolved.SourcePath != "ui-cache" {
		t.Fatalf("expected explicit sourcePath ui-cache, got %q", resolved.SourcePath)
	}
	if resolved.CodeCount != 1 || len(resolved.Codes) != 1 || resolved.Codes[0] != "ABP-889" {
		t.Fatalf("unexpected preloaded codes: %#v", resolved)
	}
}

func TestResolvePreloadedExpectedCodesRepairsFilmDataSourcePathPreference(t *testing.T) {
	service := NewService()

	resolved, err := service.ResolvePreloadedExpectedCodes(RunOptions{
		PreloadedExpected: PreloadedExpectedCodes{
			SourceType:         codeSourceFilmData,
			SourcePath:         "C:\\crawl-output\\organizer-codes.json",
			FilmDataPath:       "C:\\crawl-output\\filmData.json",
			OrganizerCodesPath: "C:\\crawl-output\\organizer-codes.json",
			Codes:              []string{"ABP-889"},
		},
	})
	if err != nil {
		t.Fatalf("ResolvePreloadedExpectedCodes returned error: %v", err)
	}
	if resolved.SourceType != codeSourceFilmData {
		t.Fatalf("expected sourceType filmData, got %q", resolved.SourceType)
	}
	if resolved.SourcePath != "C:\\crawl-output\\filmData.json" {
		t.Fatalf("expected repaired filmData sourcePath, got %q", resolved.SourcePath)
	}
}
