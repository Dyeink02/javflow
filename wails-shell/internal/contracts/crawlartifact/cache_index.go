// Ownership summary:
//   This file implements the crawl-cache snapshot index used across crawler, subscription, and organizer.
//
// File map for maintainers:
//   1) CacheSnapshot type and index file constants.
//   2) Snapshot discovery, read, and write operations.
//   3) Index maintenance helpers.
//
package crawlartifact

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	CrawlCacheIndexFile = "crawl-cache-index.json"
)

// CacheSnapshot is the lightweight internal crawl snapshot index entry shared
// by crawler, organizer, and subscription preload flows.
//
// This is intentionally smaller than filmData.json and organizer-codes.json:
// it exists so UI selectors and cross-module preload logic can list "which
// actress/result snapshots do we currently remember?" without reopening large
// artifacts or depending on live runtime state.
type CacheSnapshot struct {
	SchemaVersion      int    `json:"schemaVersion"`
	CacheKey           string `json:"cacheKey"`
	Source             string `json:"source"`
	ActressName        string `json:"actressName,omitempty"`
	CrawlURL           string `json:"crawlUrl,omitempty"`
	SiteBase           string `json:"siteBase,omitempty"`
	OutputDir          string `json:"outputDir"`
	FilmDataPath       string `json:"filmDataPath,omitempty"`
	CrawlProfilePath   string `json:"crawlProfilePath,omitempty"`
	OrganizerCodesPath string `json:"organizerCodesPath,omitempty"`
	TargetCount        int    `json:"targetCount"`
	CompletedCount     int    `json:"completedCount"`
	ItemsPerPage       int    `json:"itemsPerPage,omitempty"`
	TotalPages         int    `json:"totalPages,omitempty"`
	UpdatedAt          string `json:"updatedAt,omitempty"`
	DisplayName        string `json:"displayName,omitempty"`
}

var cacheIndexMu sync.Mutex

// BackfillResult reports one bounded visible-to-hidden cache migration.
type BackfillResult struct {
	MigratedFilmDataCount int `json:"migratedFilmDataCount"`
	CopiedProfileCount    int `json:"copiedProfileCount"`
	CopiedOrganizerCount  int `json:"copiedOrganizerCount"`
}

// CacheIndexPath returns the shared internal snapshot-index path.
func CacheIndexPath(userDataDir string) string {
	normalizedUserData := NormalizeRootPath(userDataDir)
	if normalizedUserData == "" {
		return ""
	}
	return filepath.Join(normalizedUserData, "crawl-artifacts", CrawlCacheIndexFile)
}

// InferUserDataDirFromArtifactPath reconstructs the app-managed user-data root
// from one internal artifact path when the writer only knows where the
// redirected crawl-profile.json was written.
func InferUserDataDirFromArtifactPath(artifactPath string) string {
	normalizedPath := NormalizeRootPath(artifactPath)
	if normalizedPath == "" {
		return ""
	}

	artifactDir := filepath.Dir(normalizedPath)
	parent := filepath.Dir(artifactDir)
	if strings.EqualFold(filepath.Base(parent), "crawl-artifacts") {
		return filepath.Dir(parent)
	}
	return ""
}

// BuildCacheSnapshot shapes one stable cache entry from the shared crawl
// artifacts that were just written.
func BuildCacheSnapshot(paths CrawlOutputPaths, profile CrawlProfileArtifact, source string) CacheSnapshot {
	return CacheSnapshot{
		SchemaVersion:      CurrentSchemaVersion,
		CacheKey:           stableOutputDirKey(paths.OutputDir),
		Source:             strings.TrimSpace(source),
		ActressName:        strings.TrimSpace(profile.ActressName),
		CrawlURL:           strings.TrimSpace(profile.CrawlURL),
		SiteBase:           strings.TrimSpace(profile.SiteBase),
		OutputDir:          strings.TrimSpace(paths.OutputDir),
		FilmDataPath:       strings.TrimSpace(paths.FilmDataPath),
		CrawlProfilePath:   strings.TrimSpace(paths.CrawlProfilePath),
		OrganizerCodesPath: strings.TrimSpace(paths.OrganizerCodesPath),
		TargetCount:        profile.TargetCount,
		CompletedCount:     profile.CompletedCount,
		ItemsPerPage:       profile.ItemsPerPage,
		TotalPages:         profile.TotalPages,
		UpdatedAt:          strings.TrimSpace(profile.CompletedAt),
	}
}

// ListCacheSnapshots reads the internal crawl snapshot index and returns it in
// newest-first order for UI selectors and preload helpers.
func ListCacheSnapshots(userDataDir string) ([]CacheSnapshot, error) {
	indexPath := CacheIndexPath(userDataDir)
	if indexPath == "" {
		return []CacheSnapshot{}, nil
	}

	payload, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []CacheSnapshot{}, nil
		}
		return nil, err
	}

	items := []CacheSnapshot{}
	if err := json.Unmarshal(payload, &items); err != nil {
		return nil, fmt.Errorf("缓存索引损坏或格式错误：%w", err)
	}
	normalizeCacheSnapshots(items)
	preferInternalSnapshotPaths(userDataDir, items)
	return items, nil
}

// DiscoverCacheSnapshots repairs a missing or empty cache index from persisted
// crawl artifacts. Roots are deliberately bounded by depth and entry count so
// a UI refresh cannot turn into an unbounded disk scan.
func DiscoverCacheSnapshots(userDataDir string, roots []string) ([]CacheSnapshot, error) {
	existing, indexErr := ListCacheSnapshots(userDataDir)
	if indexErr != nil {
		existing = []CacheSnapshot{}
	}

	scanRoots := append([]string{}, roots...)
	if artifactRoot := filepath.Join(NormalizeRootPath(userDataDir), "crawl-artifacts"); strings.TrimSpace(userDataDir) != "" {
		scanRoots = append(scanRoots, artifactRoot)
	}

	candidates := map[string]*discoveredArtifactSet{}
	seenRoots := map[string]struct{}{}
	for _, root := range scanRoots {
		normalized := NormalizeRootPath(root)
		if normalized == "" {
			continue
		}
		if info, err := os.Stat(normalized); err == nil && !info.IsDir() {
			normalized = filepath.Dir(normalized)
		}
		rootKey := strings.ToLower(normalized)
		if _, seen := seenRoots[rootKey]; seen {
			continue
		}
		seenRoots[rootKey] = struct{}{}
		discoverArtifactsUnderRoot(normalized, candidates)
	}

	merged := make(map[string]CacheSnapshot, len(existing)+len(candidates))
	for _, item := range existing {
		if key := strings.TrimSpace(item.CacheKey); key != "" {
			merged[key] = item
		}
	}
	for _, candidate := range candidates {
		snapshot, ok := buildDiscoveredCacheSnapshot(candidate)
		if !ok {
			continue
		}
		key := strings.TrimSpace(snapshot.CacheKey)
		if key == "" {
			continue
		}
		if current, exists := merged[key]; !exists || snapshot.UpdatedAt >= current.UpdatedAt {
			merged[key] = snapshot
		}
	}

	items := make([]CacheSnapshot, 0, len(merged))
	for _, item := range merged {
		items = append(items, item)
	}
	items = filterUsableCacheSnapshots(userDataDir, items)
	var backfillErr error
	items, _, backfillErr = BackfillVisibleFilmDataSnapshots(userDataDir, items)
	items = filterUsableCacheSnapshots(userDataDir, items)
	normalizeCacheSnapshots(items)

	// A successful discovery is authoritative enough to repair a corrupt,
	// missing, or stale index. Visible crawl output files are never modified.
	if err := writeCacheIndex(userDataDir, items); err != nil {
		return items, err
	}
	if backfillErr != nil {
		return items, backfillErr
	}
	return items, nil
}

// BackfillVisibleFilmDataSnapshots preserves older public crawl outputs in the
// app-managed cache. Existing hidden files always win and are never overwritten.
func BackfillVisibleFilmDataSnapshots(userDataDir string, items []CacheSnapshot) ([]CacheSnapshot, BackfillResult, error) {
	result := BackfillResult{}
	var firstErr error
	for index := range items {
		item := &items[index]
		if strings.EqualFold(strings.TrimSpace(item.Source), "crawler-history") {
			continue
		}
		if strings.TrimSpace(item.OutputDir) == "" {
			continue
		}

		visiblePaths := ResolveCrawlOutputPaths(item.OutputDir)
		internalPaths := ResolveInternalArtifactPaths(userDataDir, item.OutputDir)
		if internalPaths.OutputDir == "" {
			continue
		}

		filmSource := firstExistingFile(item.FilmDataPath, visiblePaths.FilmDataPath)
		if copied, err := copyArtifactIfMissing(filmSource, internalPaths.FilmDataPath); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("迁移隐藏 filmData.json 失败：%w", err)
			}
		} else if copied {
			result.MigratedFilmDataCount++
			item.Source = "migrated"
		}

		profileSource := firstExistingFile(item.CrawlProfilePath, visiblePaths.CrawlProfilePath)
		if copied, err := copyArtifactIfMissing(profileSource, internalPaths.CrawlProfilePath); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("迁移隐藏 crawl-profile.json 失败：%w", err)
			}
		} else if copied {
			result.CopiedProfileCount++
		}

		organizerSource := firstExistingFile(item.OrganizerCodesPath, visiblePaths.OrganizerCodesPath)
		if copied, err := copyArtifactIfMissing(organizerSource, internalPaths.OrganizerCodesPath); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("迁移隐藏 organizer-codes.json 失败：%w", err)
			}
		} else if copied {
			result.CopiedOrganizerCount++
		}
	}
	preferInternalSnapshotPaths(userDataDir, items)
	return items, result, firstErr
}

func copyArtifactIfMissing(sourcePath string, targetPath string) (bool, error) {
	source := firstExistingFile(sourcePath)
	target := strings.TrimSpace(targetPath)
	if source == "" || target == "" || strings.EqualFold(NormalizeRootPath(source), NormalizeRootPath(target)) {
		return false, nil
	}
	if firstExistingFile(target) != "" {
		return false, nil
	}
	payload, err := os.ReadFile(source)
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(target, payload, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func preferInternalSnapshotPaths(userDataDir string, items []CacheSnapshot) {
	for index := range items {
		if strings.EqualFold(strings.TrimSpace(items[index].Source), "crawler-history") {
			continue
		}
		visiblePaths := ResolveCrawlOutputPaths(items[index].OutputDir)
		internalPaths := ResolveInternalArtifactPaths(userDataDir, items[index].OutputDir)
		items[index].FilmDataPath = firstExistingFile(internalPaths.FilmDataPath, items[index].FilmDataPath, visiblePaths.FilmDataPath)
		items[index].CrawlProfilePath = firstExistingFile(internalPaths.CrawlProfilePath, items[index].CrawlProfilePath, visiblePaths.CrawlProfilePath)
		items[index].OrganizerCodesPath = firstExistingFile(internalPaths.OrganizerCodesPath, items[index].OrganizerCodesPath, visiblePaths.OrganizerCodesPath)
	}
}

func filterUsableCacheSnapshots(userDataDir string, items []CacheSnapshot) []CacheSnapshot {
	filtered := make([]CacheSnapshot, 0, len(items))
	artifactRoot := NormalizeRootPath(filepath.Join(userDataDir, "crawl-artifacts"))
	for _, item := range items {
		outputDir := NormalizeRootPath(item.OutputDir)
		if artifactRoot != "" {
			if relative, err := filepath.Rel(artifactRoot, outputDir); err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				continue
			}
		}
		if firstExistingFile(item.FilmDataPath, item.OrganizerCodesPath) == "" {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

type discoveredArtifactSet struct {
	dir            string
	filmData       string
	crawlProfile   string
	organizerCodes string
	newestModTime  time.Time
}

func discoverArtifactsUnderRoot(root string, candidates map[string]*discoveredArtifactSet) {
	const maxDepth = 4
	const maxEntries = 20000
	entriesVisited := 0

	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		entriesVisited++
		if entriesVisited > maxEntries {
			return filepath.SkipAll
		}

		relative, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		depth := 0
		if relative != "." {
			depth = len(strings.Split(relative, string(os.PathSeparator)))
		}
		if entry.IsDir() {
			name := strings.ToLower(entry.Name())
			if relative != "." && (name == ".git" || name == "node_modules" || name == "runtime") {
				return filepath.SkipDir
			}
			if depth >= maxDepth {
				return filepath.SkipDir
			}
			return nil
		}

		name := strings.ToLower(entry.Name())
		if name != strings.ToLower(CrawlFilmDataFile) &&
			name != strings.ToLower(CrawlProfileFile) &&
			name != strings.ToLower(OrganizerCodesFile) {
			return nil
		}

		dir := filepath.Dir(path)
		key := strings.ToLower(dir)
		candidate := candidates[key]
		if candidate == nil {
			candidate = &discoveredArtifactSet{dir: dir}
			candidates[key] = candidate
		}
		switch name {
		case strings.ToLower(CrawlFilmDataFile):
			candidate.filmData = path
		case strings.ToLower(CrawlProfileFile):
			candidate.crawlProfile = path
		case strings.ToLower(OrganizerCodesFile):
			candidate.organizerCodes = path
		}
		if info, statErr := entry.Info(); statErr == nil && info.ModTime().After(candidate.newestModTime) {
			candidate.newestModTime = info.ModTime()
		}
		return nil
	})
}

func buildDiscoveredCacheSnapshot(candidate *discoveredArtifactSet) (CacheSnapshot, bool) {
	if candidate == nil {
		return CacheSnapshot{}, false
	}

	var profile CrawlProfileArtifact
	if candidate.crawlProfile != "" {
		if payload, err := os.ReadFile(candidate.crawlProfile); err == nil {
			_ = json.Unmarshal(payload, &profile)
		}
	}
	var organizer OrganizerCodesArtifact
	if candidate.organizerCodes != "" {
		if payload, err := os.ReadFile(candidate.organizerCodes); err == nil {
			_ = json.Unmarshal(payload, &organizer)
		}
	}

	outputDir := NormalizeRootPath(profile.OutputDir)
	if outputDir == "" {
		outputDir = NormalizeRootPath(organizer.OutputDir)
	}
	if outputDir == "" {
		if strings.EqualFold(filepath.Base(filepath.Dir(candidate.dir)), "crawl-artifacts") {
			return CacheSnapshot{}, false
		}
		outputDir = NormalizeRootPath(candidate.dir)
	}
	if outputDir == "" {
		return CacheSnapshot{}, false
	}

	filmDataPath := strings.TrimSpace(candidate.filmData)
	if filmDataPath == "" {
		filmDataPath = strings.TrimSpace(profile.FilmDataPath)
	}
	if filmDataPath == "" {
		filmDataPath = strings.TrimSpace(organizer.FilmDataPath)
	}
	completedCount := profile.CompletedCount
	if organizer.UniqueCodeCount > completedCount {
		completedCount = organizer.UniqueCodeCount
	}
	if completedCount == 0 && filmDataPath != "" {
		if payload, err := os.ReadFile(filmDataPath); err == nil {
			var parsed any
			if json.Unmarshal(payload, &parsed) == nil {
				completedCount = len(NormalizeFilmDataRecords(parsed))
			}
		}
	}

	updatedAt := strings.TrimSpace(profile.CompletedAt)
	if strings.TrimSpace(organizer.CompletedAt) > updatedAt {
		updatedAt = strings.TrimSpace(organizer.CompletedAt)
	}
	if updatedAt == "" && !candidate.newestModTime.IsZero() {
		updatedAt = candidate.newestModTime.UTC().Format(time.RFC3339)
	}
	actressName := strings.TrimSpace(profile.ActressName)
	if actressName == "" {
		actressName = strings.TrimSpace(organizer.ActressName)
	}
	if actressName == "" {
		actressName = filepath.Base(outputDir)
	}

	cacheKey := stableOutputDirKey(outputDir)
	source := "discovered"
	if expectedRoot := ResolveInternalArtifactRoot(InferUserDataDirFromArtifactPath(candidate.crawlProfile), outputDir); expectedRoot != "" && !strings.EqualFold(NormalizeRootPath(expectedRoot), NormalizeRootPath(candidate.dir)) {
		cacheKey = stableOutputDirKey(candidate.dir)
		source = "crawler-history"
	}
	return CacheSnapshot{
		SchemaVersion:      CurrentSchemaVersion,
		CacheKey:           cacheKey,
		Source:             source,
		ActressName:        actressName,
		CrawlURL:           strings.TrimSpace(profile.CrawlURL),
		SiteBase:           strings.TrimSpace(profile.SiteBase),
		OutputDir:          outputDir,
		FilmDataPath:       filmDataPath,
		CrawlProfilePath:   strings.TrimSpace(candidate.crawlProfile),
		OrganizerCodesPath: strings.TrimSpace(candidate.organizerCodes),
		TargetCount:        profile.TargetCount,
		CompletedCount:     completedCount,
		ItemsPerPage:       profile.ItemsPerPage,
		TotalPages:         profile.TotalPages,
		UpdatedAt:          updatedAt,
	}, true
}

func normalizedSnapshotPathKey(outputDir string) string {
	return strings.ToLower(NormalizeRootPath(outputDir))
}

// UpsertCacheSnapshot keeps the shared snapshot index aligned with the latest
// persisted crawl artifacts.
func UpsertCacheSnapshot(userDataDir string, snapshot CacheSnapshot) error {
	cacheIndexMu.Lock()
	defer cacheIndexMu.Unlock()
	return upsertCacheSnapshotUnlocked(userDataDir, snapshot)
}

func upsertCacheSnapshotUnlocked(userDataDir string, snapshot CacheSnapshot) error {
	indexPath := CacheIndexPath(userDataDir)
	if indexPath == "" {
		return nil
	}

	items, err := ListCacheSnapshots(userDataDir)
	if err != nil {
		return err
	}

	snapshot.SchemaVersion = CurrentSchemaVersion
	if strings.TrimSpace(snapshot.CacheKey) == "" {
		snapshot.CacheKey = stableOutputDirKey(snapshot.OutputDir)
	}
	if snapshot.CacheKey == "" {
		return nil
	}

	filtered := make([]CacheSnapshot, 0, len(items)+1)
	for _, item := range items {
		if strings.TrimSpace(item.CacheKey) == strings.TrimSpace(snapshot.CacheKey) {
			continue
		}
		if strings.EqualFold(snapshot.Source, "crawler-history") && strings.TrimSpace(item.CacheKey) == stableOutputDirKey(snapshot.OutputDir) {
			continue
		}
		filtered = append(filtered, item)
	}
	filtered = append(filtered, snapshot)
	normalizeCacheSnapshots(filtered)

	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		return err
	}

	payload, err := json.MarshalIndent(filtered, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(indexPath, payload, 0o644)
}

// ArchiveCompletedCacheSnapshot preserves one immutable completed run while
// leaving the stable hidden alias in place for automatic cross-module reads.
func ArchiveCompletedCacheSnapshot(userDataDir string, paths CrawlOutputPaths, profile CrawlProfileArtifact, source string) error {
	if strings.TrimSpace(userDataDir) == "" || strings.TrimSpace(profile.CompletedAt) == "" {
		return nil
	}
	completedAt, err := time.Parse(time.RFC3339, profile.CompletedAt)
	if err != nil {
		completedAt = time.Now()
	}
	name := sanitizeSnapshotName(profile.ActressName)
	if name == "" {
		name = "\u672a\u547d\u540d"
	}
	displayName := fmt.Sprintf("%d\u6708%d\u65e5 %s %d\u90e8", int(completedAt.Month()), completedAt.Day(), name, profile.CompletedCount)
	dirLabel := fmt.Sprintf("%d\u6708%d\u65e5-%s-%d\u90e8", int(completedAt.Month()), completedAt.Day(), name, profile.CompletedCount)
	seed := strings.NewReplacer("|", "_", "/", "_", "\\", "_").Replace(strings.Join([]string{profile.RunID, profile.CompletedAt, paths.OutputDir, name, fmt.Sprint(profile.CompletedCount)}, "|"))
	suffix := stableOutputDirKey(seed)
	if len(suffix) > 8 {
		suffix = suffix[len(suffix)-8:]
	}
	archiveDir := filepath.Join(NormalizeRootPath(userDataDir), "crawl-artifacts", dirLabel+"-"+suffix)
	archivePaths := CrawlOutputPaths{
		OutputDir:          paths.OutputDir,
		FilmDataPath:       filepath.Join(archiveDir, CrawlFilmDataFile),
		CrawlProfilePath:   filepath.Join(archiveDir, CrawlProfileFile),
		OrganizerCodesPath: filepath.Join(archiveDir, OrganizerCodesFile),
	}
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return err
	}
	for _, pair := range [][2]string{{paths.FilmDataPath, archivePaths.FilmDataPath}, {paths.CrawlProfilePath, archivePaths.CrawlProfilePath}, {paths.OrganizerCodesPath, archivePaths.OrganizerCodesPath}} {
		payload, readErr := os.ReadFile(pair[0])
		if readErr != nil {
			if os.IsNotExist(readErr) {
				continue
			}
			return readErr
		}
		if writeErr := os.WriteFile(pair[1], payload, 0o644); writeErr != nil {
			return writeErr
		}
	}
	// Repoint metadata inside the archived copy so opening an old snapshot can
	// never silently fall back to the mutable stable/latest alias.
	if payload, readErr := os.ReadFile(archivePaths.CrawlProfilePath); readErr == nil {
		var archivedProfile CrawlProfileArtifact
		if json.Unmarshal(payload, &archivedProfile) == nil {
			archivedProfile.FilmDataPath = archivePaths.FilmDataPath
			if rewritten, marshalErr := json.MarshalIndent(archivedProfile, "", "  "); marshalErr == nil {
				if writeErr := os.WriteFile(archivePaths.CrawlProfilePath, rewritten, 0o644); writeErr != nil {
					return writeErr
				}
			}
		}
	}
	if payload, readErr := os.ReadFile(archivePaths.OrganizerCodesPath); readErr == nil {
		var archivedOrganizer OrganizerCodesArtifact
		if json.Unmarshal(payload, &archivedOrganizer) == nil {
			archivedOrganizer.FilmDataPath = archivePaths.FilmDataPath
			if rewritten, marshalErr := json.MarshalIndent(archivedOrganizer, "", "  "); marshalErr == nil {
				if writeErr := os.WriteFile(archivePaths.OrganizerCodesPath, rewritten, 0o644); writeErr != nil {
					return writeErr
				}
			}
		}
	}
	snapshot := BuildCacheSnapshot(archivePaths, profile, "crawler-history")
	snapshot.CacheKey = stableOutputDirKey(archiveDir)
	snapshot.DisplayName = displayName
	return UpsertCacheSnapshot(userDataDir, snapshot)
}

func sanitizeSnapshotName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_").Replace(value)
	return strings.TrimRight(value, ". ")
}

// RemoveCacheSnapshot deletes one snapshot entry and its internal artifact
// directory. Visible user output files are left untouched.
func RemoveCacheSnapshot(userDataDir string, cacheKey string) (int, error) {
	items, err := ListCacheSnapshots(userDataDir)
	if err != nil {
		return 0, err
	}

	trimmedKey := strings.TrimSpace(cacheKey)
	if trimmedKey == "" {
		return 0, nil
	}

	removed := 0
	filtered := make([]CacheSnapshot, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.CacheKey) == trimmedKey {
			removed++
			root := ResolveInternalArtifactRoot(userDataDir, item.OutputDir)
			if strings.EqualFold(strings.TrimSpace(item.Source), "crawler-history") {
				root = filepath.Dir(firstExistingFile(item.FilmDataPath, item.CrawlProfilePath, item.OrganizerCodesPath))
			}
			if root != "" {
				if err := os.RemoveAll(root); err != nil {
					return 0, fmt.Errorf("删除快照产物目录失败 %s: %w", root, err)
				}
			}
			continue
		}
		filtered = append(filtered, item)
	}

	if err := writeCacheIndex(userDataDir, filtered); err != nil {
		return 0, err
	}
	return removed, nil
}

// ClearAllCacheSnapshots removes the whole internal snapshot index and all
// per-output internal artifact directories, but does not touch user-chosen
// visible output folders.
func ClearAllCacheSnapshots(userDataDir string) (int, error) {
	items, err := ListCacheSnapshots(userDataDir)
	if err != nil {
		return 0, err
	}

	var removeErr error
	artifactRoot := filepath.Join(NormalizeRootPath(userDataDir), "crawl-artifacts")
	if strings.TrimSpace(artifactRoot) != "" {
		entries, readErr := os.ReadDir(artifactRoot)
		if readErr == nil {
			for _, entry := range entries {
				if strings.EqualFold(entry.Name(), CrawlCacheIndexFile) {
					continue
				}
				if err := os.RemoveAll(filepath.Join(artifactRoot, entry.Name())); err != nil && removeErr == nil {
					removeErr = fmt.Errorf("清理快照目录失败 %s: %w", entry.Name(), err)
				}
			}
		}
	}

	if err := writeCacheIndex(userDataDir, []CacheSnapshot{}); err != nil {
		return 0, err
	}
	if removeErr != nil {
		return len(items), removeErr
	}
	return len(items), nil
}

func writeCacheIndex(userDataDir string, items []CacheSnapshot) error {
	indexPath := CacheIndexPath(userDataDir)
	if indexPath == "" {
		return nil
	}
	normalizeCacheSnapshots(items)
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		return err
	}

	payload, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(indexPath, payload, 0o644)
}

func normalizeCacheSnapshots(items []CacheSnapshot) {
	for index := range items {
		items[index].SchemaVersion = CurrentSchemaVersion
		items[index].CacheKey = strings.TrimSpace(items[index].CacheKey)
		if items[index].CacheKey == "" {
			items[index].CacheKey = stableOutputDirKey(items[index].OutputDir)
		}
		items[index].Source = strings.TrimSpace(items[index].Source)
		items[index].ActressName = strings.TrimSpace(items[index].ActressName)
		items[index].CrawlURL = strings.TrimSpace(items[index].CrawlURL)
		items[index].SiteBase = strings.TrimSpace(items[index].SiteBase)
		items[index].OutputDir = NormalizeRootPath(items[index].OutputDir)
		items[index].FilmDataPath = strings.TrimSpace(items[index].FilmDataPath)
		items[index].CrawlProfilePath = strings.TrimSpace(items[index].CrawlProfilePath)
		items[index].OrganizerCodesPath = strings.TrimSpace(items[index].OrganizerCodesPath)
		items[index].UpdatedAt = strings.TrimSpace(items[index].UpdatedAt)
		items[index].DisplayName = strings.TrimSpace(items[index].DisplayName)
		if items[index].CompletedCount < 0 {
			items[index].CompletedCount = 0
		}
		if items[index].TargetCount < 0 {
			items[index].TargetCount = 0
		}
		if items[index].ItemsPerPage < 0 {
			items[index].ItemsPerPage = 0
		}
		if items[index].TotalPages < 0 {
			items[index].TotalPages = 0
		}
	}

	sort.SliceStable(items, func(i int, j int) bool {
		if items[i].UpdatedAt != items[j].UpdatedAt {
			return items[i].UpdatedAt > items[j].UpdatedAt
		}
		if items[i].ActressName != items[j].ActressName {
			return items[i].ActressName < items[j].ActressName
		}
		return items[i].CacheKey < items[j].CacheKey
	})
}

// CacheSnapshotLabel returns one compact UI-facing label for selectors.
func CacheSnapshotLabel(snapshot CacheSnapshot) string {
	if displayName := strings.TrimSpace(snapshot.DisplayName); displayName != "" {
		return displayName
	}
	name := strings.TrimSpace(snapshot.ActressName)
	if name == "" {
		name = filepath.Base(strings.TrimSpace(snapshot.OutputDir))
	}
	if name == "" {
		name = "未命名快照"
	}
	if updated := strings.TrimSpace(snapshot.UpdatedAt); updated != "" {
		if parsed, err := time.Parse(time.RFC3339, updated); err == nil {
			return fmt.Sprintf("%d\u6708%d\u65e5 %s %d\u90e8", int(parsed.Month()), parsed.Day(), name, snapshot.CompletedCount)
		}
		return fmt.Sprintf("%s | %s | %d \u90e8", name, updated, snapshot.CompletedCount)
	}
	return fmt.Sprintf("%s | %d \u90e8", name, snapshot.CompletedCount)
}
