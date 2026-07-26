package crawltask

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"javflow/internal/contracts/crawlartifact"
)

// Output resolution owns the task-controller rule for choosing a fresh run
// directory versus resuming an existing artifact directory.
//
// Ownership summary:
// 1) decide whether to reuse an existing output directory or create a fresh run dir
// 2) centralize artifact-presence checks used by task-controller output resolution
// 3) keep output-dir policy separate from runner execution and artifact writes
//
// File map for maintainers:
// 1) output-resolution DTOs and artifact-name tables
// 2) artifact-presence detection helpers
// 3) fresh-run vs reuse decision helpers

type OutputDirectoryResolution struct {
	BaseOutputDir        string
	OutputDir            string
	CreatedRunDir        bool
	ReusedExistingDir    bool
	HasExistingArtifacts bool
	Reason               string
}

var coreOutputArtifacts = []string{
	crawlartifact.CrawlFilmDataFile,
	crawlartifact.DefaultMagnetTxt,
	crawlartifact.DefaultUnfinishedTxt,
}

func ResolveRunOutputDirectory(outputDir string, resumeExisting bool, now time.Time) OutputDirectoryResolution {
	baseOutputDir := normalizePath(outputDir)
	if baseOutputDir == "" {
		baseOutputDir = normalizePath(".")
	}

	if resumeExisting {
		return OutputDirectoryResolution{
			BaseOutputDir:        baseOutputDir,
			OutputDir:            baseOutputDir,
			CreatedRunDir:        false,
			ReusedExistingDir:    true,
			HasExistingArtifacts: false,
			Reason:               "resume-existing",
		}
	}

	info, err := os.Stat(baseOutputDir)
	if err != nil || !info.IsDir() {
		return OutputDirectoryResolution{
			BaseOutputDir:        baseOutputDir,
			OutputDir:            baseOutputDir,
			CreatedRunDir:        false,
			ReusedExistingDir:    true,
			HasExistingArtifacts: false,
			Reason:               "base-dir-missing",
		}
	}

	if !hasHistoricalArtifacts(baseOutputDir) {
		return OutputDirectoryResolution{
			BaseOutputDir:        baseOutputDir,
			OutputDir:            baseOutputDir,
			CreatedRunDir:        false,
			ReusedExistingDir:    true,
			HasExistingArtifacts: false,
			Reason:               "base-dir-empty",
		}
	}

	stamp := formatRunStamp(now)
	candidate := filepath.Join(baseOutputDir, "run-"+stamp)
	suffix := 0
	for pathExists(candidate) {
		suffix++
		candidate = filepath.Join(baseOutputDir, "run-"+stamp+"-"+strconv.Itoa(suffix))
	}

	return OutputDirectoryResolution{
		BaseOutputDir:        baseOutputDir,
		OutputDir:            candidate,
		CreatedRunDir:        true,
		ReusedExistingDir:    false,
		HasExistingArtifacts: true,
		Reason:               "isolated-existing-output",
	}
}

func hasHistoricalArtifacts(outputDir string) bool {
	for _, fileName := range coreOutputArtifacts {
		if pathExists(filepath.Join(outputDir, fileName)) {
			return true
		}
	}

	return pathExists(crawlartifact.DefaultLogDirPath(outputDir))
}

func pathExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func formatRunStamp(now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}

	return now.Format("20060102-150405")
}
