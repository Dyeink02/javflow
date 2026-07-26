package crawlartifact

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// This file owns the canonical crawl artifact path contract used by Go-side
// crawler, organizer, subscription, and diagnostics flows. When output files or
// report locations drift, fix the shared path rules here before patching
// downstream call sites independently.
//
// Scope:
// - canonical artifact/report filenames and path helpers live here
// - artifact data shapes live in artifacts.go
// - feature-specific interpretation stays in crawler/organizer/subscription code
//
// Ownership summary:
// 1) define the shared canonical crawl artifact/report path contract
// 2) keep path derivation stable across crawler, organizer, subscription, and diagnostics
// 3) separate shared artifact path rules from feature-specific interpretation
//
// File map for maintainers:
// 1) canonical artifact/report filename constants
// 2) crawl output path DTOs
// 3) path derivation/normalize helpers

const (
	CrawlFilmDataFile         = "filmData.json"
	CrawlProfileFile          = "crawl-profile.json"
	OrganizerCodesFile        = "organizer-codes.json"
	FilteredFilmCodesFile     = "filtered-film-codes.json"
	DefaultMagnetTxt          = "magnet-links.txt"
	DefaultFilteredCodesTxt   = "filtered-codes.txt"
	DefaultLogDirName         = "logs"
	DefaultLatestLogTxt       = "latest-log.txt"
	DefaultUnfinishedTxt      = "unfinished-codes.txt"
	DefaultQualitySummaryTxt  = "crawl-quality-summary.txt"
)

// CrawlOutputPaths holds the canonical locations for crawl artifacts.
// This is the shared minimal path bundle for cross-module handoff.
type CrawlOutputPaths struct {
	OutputDir          string `json:"outputDir"`
	FilmDataPath       string `json:"filmDataPath"`
	CrawlProfilePath   string `json:"crawlProfilePath"`
	OrganizerCodesPath string `json:"organizerCodesPath"`
}

// CrawlRunPaths extends artifact paths with runtime-facing files that live next
// to a crawl run, such as logs and magnet output. Read-model builders should
// prefer this helper instead of re-joining the same default paths locally.
type CrawlRunPaths struct {
	CrawlOutputPaths
	MagnetPath            string `json:"magnetPath"`
	FilteredCodesPath     string `json:"filteredCodesPath"`
	FilteredFilmCodesPath string `json:"filteredFilmCodesPath"`
	LogDir                string `json:"logDir"`
	LatestLogPath         string `json:"latestLogPath"`
}

// NormalizeRootPath converts a user-provided output root into an absolute path
// when possible. It is shared by modules that only need artifact locations.
func NormalizeRootPath(rootPath string) string {
	trimmed := strings.TrimSpace(rootPath)
	if trimmed == "" {
		return ""
	}

	if absolutePath, err := filepath.Abs(trimmed); err == nil {
		return absolutePath
	}

	return filepath.Clean(trimmed)
}

// hasCrawlArtifacts reports whether a directory contains at least one
// recognizable crawl artifact. It is used to decide whether a candidate
// directory is a concrete crawl run or merely a parent/root that holds
// multiple run subdirectories.
func hasCrawlArtifacts(dir string) bool {
	for _, name := range []string{CrawlFilmDataFile, OrganizerCodesFile, CrawlProfileFile} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

// ResolveEffectiveCrawlOutputDir returns the concrete crawl output directory
// backed by actual artifacts. If the candidate already contains artifacts it
// is returned as-is. Otherwise the immediate subdirectories are scanned and
// the newest one containing artifacts is returned. This handles UI/organizer
// inputs that point at a parent directory while the real runs live in
// timestamped subfolders.
func ResolveEffectiveCrawlOutputDir(candidateDir string) string {
	normalized := NormalizeRootPath(candidateDir)
	if normalized == "" {
		return ""
	}

	if hasCrawlArtifacts(normalized) {
		return normalized
	}

	entries, err := os.ReadDir(normalized)
	if err != nil {
		return normalized
	}

	var bestDir string
	var bestModTime int64
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		subDir := filepath.Join(normalized, entry.Name())
		if !hasCrawlArtifacts(subDir) {
			continue
		}
		info, err := os.Stat(subDir)
		if err != nil {
			continue
		}
		if bestDir == "" || info.ModTime().Unix() > bestModTime {
			bestDir = subDir
			bestModTime = info.ModTime().Unix()
		}
	}

	if bestDir != "" {
		return bestDir
	}
	return normalized
}

// ResolveOutputPaths returns the canonical crawl artifact paths for a crawl run.
func ResolveOutputPaths(outputDir string) (normalizedOutputDir string, filmDataPath string) {
	paths := ResolveCrawlOutputPaths(outputDir)
	return paths.OutputDir, paths.FilmDataPath
}

// ResolveCrawlOutputPaths returns the canonical crawl artifact paths as a struct.
func ResolveCrawlOutputPaths(outputDir string) CrawlOutputPaths {
	normalizedOutputDir := NormalizeRootPath(outputDir)
	if normalizedOutputDir == "" {
		return CrawlOutputPaths{}
	}
	return CrawlOutputPaths{
		OutputDir:          normalizedOutputDir,
		FilmDataPath:       filepath.Join(normalizedOutputDir, CrawlFilmDataFile),
		CrawlProfilePath:   filepath.Join(normalizedOutputDir, CrawlProfileFile),
		OrganizerCodesPath: filepath.Join(normalizedOutputDir, OrganizerCodesFile),
	}
}

// ResolveInternalArtifactPaths maps one visible crawl output directory to the
// hidden application-data artifact cache. The hidden cache contains a complete
// filmData snapshot as well as the smaller cross-module handoff files.
func ResolveInternalArtifactPaths(userDataDir string, outputDir string) CrawlOutputPaths {
	normalizedUserData := NormalizeRootPath(userDataDir)
	normalizedOutputDir := NormalizeRootPath(outputDir)
	if normalizedUserData == "" || normalizedOutputDir == "" {
		return CrawlOutputPaths{}
	}

	artifactRoot := ResolveInternalArtifactRoot(normalizedUserData, normalizedOutputDir)
	return CrawlOutputPaths{
		OutputDir:          normalizedOutputDir,
		FilmDataPath:       filepath.Join(artifactRoot, CrawlFilmDataFile),
		CrawlProfilePath:   filepath.Join(artifactRoot, CrawlProfileFile),
		OrganizerCodesPath: filepath.Join(artifactRoot, OrganizerCodesFile),
	}
}

// ResolveInternalArtifactRoot returns the internal artifact directory backing
// one visible crawl output directory. Cross-module caches that need to live
// next to crawl-profile.json / organizer-codes.json should reuse this helper
// instead of rebuilding the same path shape locally.
func ResolveInternalArtifactRoot(userDataDir string, outputDir string) string {
	normalizedUserData := NormalizeRootPath(userDataDir)
	normalizedOutputDir := NormalizeRootPath(outputDir)
	if normalizedUserData == "" || normalizedOutputDir == "" {
		return ""
	}
	return filepath.Join(normalizedUserData, "crawl-artifacts", stableOutputDirKey(normalizedOutputDir))
}

// ResolveCrawlRunPaths returns canonical crawl artifact paths together with the
// default log and magnet locations for a run output directory.
// Read-only consumers should use this helper instead of rebuilding filenames
// locally, so log/report naming stays consistent across modules.
func ResolveCrawlRunPaths(outputDir string) CrawlRunPaths {
	outputPaths := ResolveCrawlOutputPaths(outputDir)
	if outputPaths.OutputDir == "" {
		return CrawlRunPaths{}
	}

	logDir := DefaultLogDirPath(outputPaths.OutputDir)
	return CrawlRunPaths{
		CrawlOutputPaths:      outputPaths,
		MagnetPath:            DefaultMagnetFilePath(outputPaths.OutputDir),
		FilteredCodesPath:     filepath.Join(outputPaths.OutputDir, DefaultFilteredCodesTxt),
		FilteredFilmCodesPath: filepath.Join(outputPaths.OutputDir, FilteredFilmCodesFile),
		LogDir:                logDir,
		LatestLogPath:         DefaultLatestLogPath(logDir),
	}
}

// ReadFilmDataRecords reads filmData.json and normalizes the historical shapes
// used by earlier crawl outputs into a simple slice of object records.
// Consumers that still need filmData should come through this helper rather
// than decoding ad hoc, so old payload variants stay centralized.
func ReadFilmDataRecords(outputDir string) (CrawlOutputPaths, []map[string]any, error) {
	paths := ResolveCrawlOutputPaths(outputDir)
	return readFilmDataRecordsAtPaths(paths)
}

// ReadFilmDataRecordsWithUserData prefers the app-managed complete snapshot and
// falls back to the user-visible filmData.json when the hidden copy is missing.
func ReadFilmDataRecordsWithUserData(outputDir string, userDataDir string) (CrawlOutputPaths, []map[string]any, error) {
	paths := ResolveCrawlOutputPaths(outputDir)
	if strings.TrimSpace(userDataDir) != "" {
		internalPaths := ResolveInternalArtifactPaths(userDataDir, outputDir)
		paths.FilmDataPath = firstExistingFile(internalPaths.FilmDataPath, paths.FilmDataPath)
	}
	return readFilmDataRecordsAtPaths(paths)
}

func readFilmDataRecordsAtPaths(paths CrawlOutputPaths) (CrawlOutputPaths, []map[string]any, error) {
	if paths.OutputDir == "" {
		return CrawlOutputPaths{}, nil, fmt.Errorf("please select a crawl output directory")
	}

	filmDataPath := firstExistingFile(paths.FilmDataPath)
	fileInfo, err := os.Stat(filmDataPath)
	if err != nil || !fileInfo.Mode().IsRegular() {
		return CrawlOutputPaths{}, nil, fmt.Errorf("filmData.json not found: %s", paths.FilmDataPath)
	}

	contents, err := os.ReadFile(filmDataPath)
	if err != nil {
		return CrawlOutputPaths{}, nil, err
	}

	var parsed any
	if err := json.Unmarshal(contents, &parsed); err != nil {
		return CrawlOutputPaths{}, nil, fmt.Errorf("failed to parse filmData.json: %w", err)
	}

	paths.FilmDataPath = filmDataPath
	return paths, NormalizeFilmDataRecords(parsed), nil
}

// NormalizeFilmDataRecords accepts the loose JSON shapes that appeared in old
// crawl outputs and returns only object records.
// This compatibility layer is intentionally here so organizer/subscription do
// not each maintain their own legacy filmData parsers.
func NormalizeFilmDataRecords(parsed any) []map[string]any {
	switch value := parsed.(type) {
	case []any:
		return NormalizeObjectSlice(value)
	case map[string]any:
		if records, ok := value["records"].([]any); ok {
			return NormalizeObjectSlice(records)
		}
		if filmData, ok := value["filmData"].([]any); ok {
			return NormalizeObjectSlice(filmData)
		}

		values := make([]any, 0, len(value))
		for _, item := range value {
			values = append(values, item)
		}

		allObjects := len(values) > 0
		for _, item := range values {
			if _, ok := item.(map[string]any); !ok {
				allObjects = false
				break
			}
		}

		if allObjects {
			return NormalizeObjectSlice(values)
		}

		return []map[string]any{value}
	default:
		return nil
	}
}

// NormalizeObjectSlice keeps only map-shaped JSON items and drops anything else.
func NormalizeObjectSlice(values []any) []map[string]any {
	records := make([]map[string]any, 0, len(values))
	for _, item := range values {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		records = append(records, record)
	}
	return records
}

// AnyToString keeps the old permissive conversion behavior for shared readers.
func AnyToString(value any) string {
	return fmt.Sprint(value)
}

// DefaultLatestLogPath returns the default latest log path inside a log dir.
func DefaultLatestLogPath(logDir string) string {
	if strings.TrimSpace(logDir) == "" {
		return ""
	}
	return filepath.Join(logDir, DefaultLatestLogTxt)
}

// DefaultLogDirPath returns the conventional log directory for a crawl run.
func DefaultLogDirPath(outputDir string) string {
	if strings.TrimSpace(outputDir) == "" {
		return ""
	}
	return filepath.Join(outputDir, DefaultLogDirName)
}

// DefaultMagnetFilePath returns the default magnet output path for a run dir.
func DefaultMagnetFilePath(outputDir string) string {
	if strings.TrimSpace(outputDir) == "" {
		return ""
	}
	return filepath.Join(outputDir, DefaultMagnetTxt)
}

// DefaultUnfinishedReportPath returns the canonical unfinished-item report path.
func DefaultUnfinishedReportPath(outputDir string) string {
	if strings.TrimSpace(outputDir) == "" {
		return ""
	}
	return filepath.Join(outputDir, DefaultUnfinishedTxt)
}

// DefaultQualitySummaryPath returns the canonical human-readable quality report path.
func DefaultQualitySummaryPath(outputDir string) string {
	if strings.TrimSpace(outputDir) == "" {
		return ""
	}
	return filepath.Join(outputDir, DefaultQualitySummaryTxt)
}

// DefaultCrawlProfilePath returns the default crawl profile artifact path.
func DefaultCrawlProfilePath(outputDir string) string {
	if strings.TrimSpace(outputDir) == "" {
		return ""
	}
	return filepath.Join(outputDir, CrawlProfileFile)
}

// DefaultOrganizerCodesPath returns the default organizer snapshot path.
func DefaultOrganizerCodesPath(outputDir string) string {
	if strings.TrimSpace(outputDir) == "" {
		return ""
	}
	return filepath.Join(outputDir, OrganizerCodesFile)
}

func stableOutputDirKey(outputDir string) string {
	normalized := strings.ToLower(strings.TrimSpace(filepath.Clean(outputDir)))
	if normalized == "" {
		return ""
	}
	replacer := strings.NewReplacer(":", "", "\\", "_", "/", "_", " ", "_")
	prefix := replacer.Replace(filepath.Base(normalized))
	if prefix == "" {
		prefix = "crawl-output"
	}
	if len(prefix) > 48 {
		prefix = prefix[:48]
	}
	return prefix + "-" + shortHash(normalized)
}

func shortHash(value string) string {
	hash := uint32(2166136261)
	for i := 0; i < len(value); i++ {
		hash ^= uint32(value[i])
		hash *= 16777619
	}
	return fmt.Sprintf("%08x", hash)
}

func firstExistingFile(paths ...string) string {
	for _, candidate := range paths {
		trimmed := strings.TrimSpace(candidate)
		if trimmed == "" {
			continue
		}
		info, err := os.Stat(trimmed)
		if err == nil && info.Mode().IsRegular() {
			return trimmed
		}
	}
	return ""
}

// ReadOrganizerCodesArtifact reads organizer-codes.json when available.
// Prefer this over filmData when a caller needs the crawler's normalized unique
// code snapshot.
func ReadOrganizerCodesArtifact(outputDir string) (CrawlOutputPaths, OrganizerCodesArtifact, error) {
	paths := ResolveCrawlOutputPaths(outputDir)
	if paths.OutputDir == "" {
		return CrawlOutputPaths{}, OrganizerCodesArtifact{}, fmt.Errorf("please select a crawl output directory")
	}

	artifactPath := firstExistingFile(paths.OrganizerCodesPath)
	if artifactPath == "" {
		return CrawlOutputPaths{}, OrganizerCodesArtifact{}, os.ErrNotExist
	}

	payload, err := os.ReadFile(artifactPath)
	if err != nil {
		return CrawlOutputPaths{}, OrganizerCodesArtifact{}, err
	}

	var artifact OrganizerCodesArtifact
	if err := json.Unmarshal(payload, &artifact); err != nil {
		return CrawlOutputPaths{}, OrganizerCodesArtifact{}, fmt.Errorf("failed to parse organizer-codes.json: %w", err)
	}
	normalizeOrganizerCodesArtifact(&artifact)
	paths.OrganizerCodesPath = artifactPath
	return paths, artifact, nil
}

// ReadCrawlProfileArtifact reads crawl-profile.json when available.
// Prefer this over filmData when a caller only needs the recent crawl summary.
func ReadCrawlProfileArtifact(outputDir string) (CrawlOutputPaths, CrawlProfileArtifact, error) {
	paths := ResolveCrawlOutputPaths(outputDir)
	if paths.OutputDir == "" {
		return CrawlOutputPaths{}, CrawlProfileArtifact{}, fmt.Errorf("please select a crawl output directory")
	}

	artifactPath := firstExistingFile(paths.CrawlProfilePath)
	if artifactPath == "" {
		return CrawlOutputPaths{}, CrawlProfileArtifact{}, os.ErrNotExist
	}

	payload, err := os.ReadFile(artifactPath)
	if err != nil {
		return CrawlOutputPaths{}, CrawlProfileArtifact{}, err
	}

	var artifact CrawlProfileArtifact
	if err := json.Unmarshal(payload, &artifact); err != nil {
		return CrawlOutputPaths{}, CrawlProfileArtifact{}, fmt.Errorf("failed to parse crawl-profile.json: %w", err)
	}
	normalizeCrawlProfileArtifact(&artifact)
	paths.CrawlProfilePath = artifactPath
	return paths, artifact, nil
}

func ReadOrganizerCodesArtifactWithUserData(outputDir string, userDataDir string) (CrawlOutputPaths, OrganizerCodesArtifact, error) {
	paths := ResolveCrawlOutputPaths(outputDir)
	if strings.TrimSpace(userDataDir) != "" {
		internalPaths := ResolveInternalArtifactPaths(userDataDir, outputDir)
		paths.OrganizerCodesPath = firstExistingFile(internalPaths.OrganizerCodesPath, paths.OrganizerCodesPath)
	}
	return readOrganizerCodesArtifactAtPaths(paths)
}

func ReadCrawlProfileArtifactWithUserData(outputDir string, userDataDir string) (CrawlOutputPaths, CrawlProfileArtifact, error) {
	paths := ResolveCrawlOutputPaths(outputDir)
	if strings.TrimSpace(userDataDir) != "" {
		internalPaths := ResolveInternalArtifactPaths(userDataDir, outputDir)
		paths.CrawlProfilePath = firstExistingFile(internalPaths.CrawlProfilePath, paths.CrawlProfilePath)
	}
	return readCrawlProfileArtifactAtPaths(paths)
}

func readOrganizerCodesArtifactAtPaths(paths CrawlOutputPaths) (CrawlOutputPaths, OrganizerCodesArtifact, error) {
	if paths.OutputDir == "" {
		return CrawlOutputPaths{}, OrganizerCodesArtifact{}, fmt.Errorf("please select a crawl output directory")
	}

	artifactPath := firstExistingFile(paths.OrganizerCodesPath)
	if artifactPath == "" {
		return CrawlOutputPaths{}, OrganizerCodesArtifact{}, os.ErrNotExist
	}

	payload, err := os.ReadFile(artifactPath)
	if err != nil {
		return CrawlOutputPaths{}, OrganizerCodesArtifact{}, err
	}

	var artifact OrganizerCodesArtifact
	if err := json.Unmarshal(payload, &artifact); err != nil {
		return CrawlOutputPaths{}, OrganizerCodesArtifact{}, fmt.Errorf("failed to parse organizer-codes.json: %w", err)
	}
	normalizeOrganizerCodesArtifact(&artifact)
	paths.OrganizerCodesPath = artifactPath
	return paths, artifact, nil
}

func readCrawlProfileArtifactAtPaths(paths CrawlOutputPaths) (CrawlOutputPaths, CrawlProfileArtifact, error) {
	if paths.OutputDir == "" {
		return CrawlOutputPaths{}, CrawlProfileArtifact{}, fmt.Errorf("please select a crawl output directory")
	}

	artifactPath := firstExistingFile(paths.CrawlProfilePath)
	if artifactPath == "" {
		return CrawlOutputPaths{}, CrawlProfileArtifact{}, os.ErrNotExist
	}

	payload, err := os.ReadFile(artifactPath)
	if err != nil {
		return CrawlOutputPaths{}, CrawlProfileArtifact{}, err
	}

	var artifact CrawlProfileArtifact
	if err := json.Unmarshal(payload, &artifact); err != nil {
		return CrawlOutputPaths{}, CrawlProfileArtifact{}, fmt.Errorf("failed to parse crawl-profile.json: %w", err)
	}
	normalizeCrawlProfileArtifact(&artifact)
	paths.CrawlProfilePath = artifactPath
	return paths, artifact, nil
}

func normalizeCrawlProfileArtifact(artifact *CrawlProfileArtifact) {
	if artifact == nil {
		return
	}
	if artifact.SchemaVersion <= 0 {
		artifact.SchemaVersion = CurrentSchemaVersion
	}
}

func normalizeOrganizerCodesArtifact(artifact *OrganizerCodesArtifact) {
	if artifact == nil {
		return
	}
	if artifact.SchemaVersion <= 0 {
		artifact.SchemaVersion = CurrentSchemaVersion
	}
}
