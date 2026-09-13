// Package crawloutput owns the persisted crawler artifacts written during and
// after a run. If filmData.json or magnet TXT output is wrong, start here
// before inspecting bridge/UI code.
//
// Boundary:
// - this package writes persisted crawl artifacts and derived snapshots
// - it does not decide crawl execution order, review panel semantics, or UI text
//
// Ownership summary:
// 1) persist filmData, magnet output, and derived crawl artifact snapshots
// 2) apply output-specific filtering/write rules without owning crawl flow
// 3) keep artifact storage semantics separate from runner and renderer code
//
// File map for maintainers:
// 1) persisted artifact DTOs and metadata contracts
// 2) writer bootstrap / on-disk preload
// 3) filmData mutation + dedupe path
// 4) flush/write helpers for filmData, magnet txt, and crawl-profile artifacts
package crawloutput

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"javflow/internal/common"
	"javflow/internal/contracts/crawlartifact"
)

type FilmData struct {
	Title       string   `json:"title"`
	SourceLink  string   `json:"sourceLink"`
	Category    []string `json:"category,omitempty"`
	Actress     []string `json:"actress,omitempty"`
	CoverImage  string   `json:"coverImage,omitempty"`
	ReleaseDate string   `json:"releaseDate,omitempty"`
	Maker       string   `json:"maker,omitempty"`
	Label       string   `json:"label,omitempty"`
	Series      string   `json:"series,omitempty"`
	MagnetLinks []struct {
		Link        string `json:"link"`
		Size        string `json:"size"`
		DisplayName string `json:"displayName,omitempty"`
	} `json:"magnetLinks,omitempty"`
	Magnet                 string `json:"magnet,omitempty"`
	ActressCount           int    `json:"actressCount,omitempty"`
	FilteredByActressCount bool   `json:"filteredByActressCount,omitempty"`
	FilteredByFilmCode     bool   `json:"filteredByFilmCode,omitempty"`
	FilteredByReleaseDate  bool   `json:"filteredByReleaseDate,omitempty"`
	FilterRemark           string `json:"filterRemark,omitempty"`
}

func (f FilmData) IdentityKey() string {
	id := extractFilmID(f.Title)
	if id == "" {
		id = normalizeSourceLink(f.SourceLink)
	}
	return strings.ToLower(id)
}

type Writer struct {
	outputDir      string
	artifactPaths  crawlartifact.CrawlOutputPaths
	mu             sync.Mutex
	records        []FilmData
	filmIDIndex    map[string]int
	dirty          bool
	metadataDirty  bool
	writeCount     int
	lastFlush      int
	flushEvery     int
	metadata       ArtifactMetadata
	minReleaseDate string
	// outputDirsEnsured marks that the output/internal directories have been
	// created. Directory creation is deferred to the first write so that the
	// app-startup standby runner no longer creates folders for merely having a
	// saved output path in settings.
	outputDirsEnsured bool
}

// ArtifactMetadata captures the minimum stable crawl context needed to derive
// post-run artifacts without coupling organizer/subscription to the runner.
// Keep this intentionally smaller than runner state: it is the persisted handoff
// context, not a serialization of the whole crawl runtime.
type ArtifactMetadata struct {
	RunID          string
	CompletedAt    string
	ActressName    string
	CrawlURL       string
	TargetCount    int
	CompletedCount int
	ItemsPerPage   int
	TotalPages     int
	SiteBase       string
}

// NewWriter owns the artifact directory and existing filmData preload. Keep
// artifact bootstrap centralized here so flush/write behavior stays consistent
// across full runs, restore flows, and retries.
func NewWriter(outputDir string) (*Writer, error) {
	return NewWriterWithArtifactPaths(outputDir, crawlartifact.ResolveCrawlOutputPaths(outputDir))
}

// NewWriterWithArtifactPaths allows the crawler to keep user-visible deliverables
// in the chosen output directory while redirecting bridge-only artifacts such as
// crawl-profile.json / organizer-codes.json to an internal cache location.
func NewWriterWithArtifactPaths(outputDir string, artifactPaths crawlartifact.CrawlOutputPaths) (*Writer, error) {
	// 审计 H-16：目录创建推迟到首次写盘（ensureOutputDirsLocked）。
	// 应用启动时会构造待命 Runner（设置里保存过输出目录），若在构造期建目录，
	// 会出现“只是打开软件就生成输出文件夹”的现象。
	if strings.TrimSpace(artifactPaths.OutputDir) == "" {
		artifactPaths = crawlartifact.ResolveCrawlOutputPaths(outputDir)
	}
	w := &Writer{
		outputDir:     outputDir,
		artifactPaths: artifactPaths,
		filmIDIndex:   map[string]int{},
		flushEvery:    100,
	}
	w.loadFromDisk()
	return w, nil
}

// SyncInternalArtifactsFromVisible rebuilds the hidden snapshot after another
// workflow intentionally rewrites the public filmData file, such as the AV
// subscription finalizer filtering a crawl to requested codes.
func SyncInternalArtifactsFromVisible(userDataDir string, outputDir string, metadata ArtifactMetadata) error {
	paths := crawlartifact.ResolveInternalArtifactPaths(userDataDir, outputDir)
	writer, err := NewWriterWithArtifactPaths(outputDir, paths)
	if err != nil {
		return err
	}
	// This operation is deliberately different from a resume: subscription
	// finalization has just rewritten the public filmData.json and must rebuild
	// the hidden snapshot from that visible, filtered view. NewWriter normally
	// prefers the hidden snapshot, so explicitly replace it here when the public
	// file is readable (including a valid empty array).
	visiblePath := crawlartifact.ResolveCrawlOutputPaths(outputDir).FilmDataPath
	writer.loadRecordsFromPath(visiblePath)
	writer.SetArtifactMetadata(metadata)
	return writer.Flush()
}

func (w *Writer) loadFromDisk() {
	// Resume prefers the app-managed complete snapshot. If it was removed by
	// the user, fall back to the public file so old portable runs remain usable.
	// The order is important: the public file may intentionally contain only a
	// subscription subset while the hidden copy retains the complete crawl.
	candidates := make([]string, 0, 2)
	if path := strings.TrimSpace(w.artifactPaths.FilmDataPath); path != "" {
		candidates = append(candidates, path)
	}
	if path := strings.TrimSpace(crawlartifact.ResolveCrawlOutputPaths(w.outputDir).FilmDataPath); path != "" {
		if len(candidates) == 0 || !strings.EqualFold(candidates[0], path) {
			candidates = append(candidates, path)
		}
	}
	for _, path := range candidates {
		if w.loadRecordsFromPath(path) {
			return
		}
	}
}

// loadRecordsFromPath replaces the in-memory index only when path contains a
// valid filmData array. Returning false lets callers try a lower-priority
// compatibility path without destroying a successfully loaded snapshot.
func (w *Writer) loadRecordsFromPath(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var records []FilmData
	if err := json.Unmarshal(data, &records); err != nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.records = records
	w.filmIDIndex = map[string]int{}
	for i, r := range records {
		key := r.IdentityKey()
		if key != "" {
			w.filmIDIndex[key] = i
		}
	}
	return true
}

// WriteFilmData is the one mutation path for filmData records. Deduplication,
// actress-filter flags, and magnet enrichment should converge here before disk
// flush logic runs.
func (w *Writer) WriteFilmData(data FilmData) (bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	key := data.IdentityKey()
	if key == "" {
		return false, nil
	}

	if idx, exists := w.filmIDIndex[key]; exists {
		existing := w.records[idx]
		changed := false
		if data.ActressCount > 0 && existing.ActressCount != data.ActressCount {
			w.records[idx].ActressCount = data.ActressCount
			changed = true
		}
		if existing.FilteredByActressCount != data.FilteredByActressCount {
			w.records[idx].FilteredByActressCount = data.FilteredByActressCount
			changed = true
		}
		if existing.FilteredByFilmCode != data.FilteredByFilmCode {
			w.records[idx].FilteredByFilmCode = data.FilteredByFilmCode
			changed = true
		}
		if existing.FilterRemark != data.FilterRemark {
			w.records[idx].FilterRemark = data.FilterRemark
			changed = true
		}
		if existing.Maker == "" && data.Maker != "" {
			w.records[idx].Maker = strings.TrimSpace(data.Maker)
			changed = true
		}
		if existing.Label == "" && data.Label != "" {
			w.records[idx].Label = strings.TrimSpace(data.Label)
			changed = true
		}
		if existing.Series == "" && data.Series != "" {
			w.records[idx].Series = strings.TrimSpace(data.Series)
			changed = true
		}
		if existing.Magnet == "" && data.Magnet != "" {
			w.records[idx].Magnet = data.Magnet
			changed = true
		}
		// Older filmData records may contain the same magnet without the
		// displayName field. Enrich those records on a later crawl so the
		// original case/suffix remains available to organizer and audit views.
		// Merge backup candidates instead of discarding links discovered by a
		// later retry. Keep the first selected `Magnet` stable, but retain every
		// unique backup with any newly available display name/size enrichment.
		storedLinks := w.records[idx].MagnetLinks
		for _, incoming := range data.MagnetLinks {
			incomingLink := strings.TrimSpace(incoming.Link)
			if incomingLink == "" {
				continue
			}
			found := false
			for magnetIndex, stored := range storedLinks {
				if !strings.EqualFold(strings.TrimSpace(stored.Link), incomingLink) {
					continue
				}
				found = true
				if stored.DisplayName == "" && incoming.DisplayName != "" {
					w.records[idx].MagnetLinks[magnetIndex].DisplayName = incoming.DisplayName
					changed = true
				}
				if stored.Size == "" && incoming.Size != "" {
					w.records[idx].MagnetLinks[magnetIndex].Size = incoming.Size
					changed = true
				}
				break
			}
			if !found {
				incoming.Link = incomingLink
				w.records[idx].MagnetLinks = append(w.records[idx].MagnetLinks, incoming)
				storedLinks = w.records[idx].MagnetLinks
				changed = true
			}
		}
		if changed {
			w.dirty = true
			return true, nil
		}
		return false, nil
	}

	w.records = append(w.records, data)
	w.filmIDIndex[key] = len(w.records) - 1
	w.dirty = true
	w.writeCount++

	needFlush := w.flushEvery > 0 && w.writeCount-w.lastFlush >= w.flushEvery
	if needFlush {
		if flushErr := w.flushLocked(); flushErr != nil {
			return true, flushErr
		}
		w.lastFlush = w.writeCount
	}

	return true, nil
}

func (w *Writer) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.flushLocked()
}

// SetArtifactMetadata updates the post-crawl metadata written alongside
// filmData.json. Callers may invoke it multiple times as final counters become
// clearer near task completion.
func (w *Writer) SetArtifactMetadata(metadata ArtifactMetadata) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.metadata == metadata {
		return
	}
	w.metadata = metadata
	w.metadataDirty = true
}

// SetMinReleaseDate sets the earliest release date a record may have to be
// included in the public filmData.json and magnet-links.txt outputs. Records
// with an earlier date are excluded from persisted artifacts. Empty value
// disables the filter.
func (w *Writer) SetMinReleaseDate(date string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.minReleaseDate = strings.TrimSpace(date)
}

// isReleaseDateFiltered reports whether a record's release date is before the
// configured minimum. Empty record date or empty minimum date disables filtering
// for that record.
func (w *Writer) isReleaseDateFiltered(recordDate string) bool {
	minDate := strings.TrimSpace(w.minReleaseDate)
	if minDate == "" {
		return false
	}
	recordDate = strings.TrimSpace(recordDate)
	if recordDate == "" {
		return false
	}
	return strings.Compare(recordDate, minDate) < 0
}

// visibleRecordsLocked returns records that should be exported, split into
// visible output records and entries filtered by release date. The returned
// slice is sorted by title and is safe to use for both file writes and live
// counter reporting.
func (w *Writer) visibleRecordsLocked() ([]FilmData, []crawlartifact.FilteredFilmCodeEntry) {
	sortedRecords := make([]FilmData, len(w.records))
	copy(sortedRecords, w.records)
	sort.Slice(sortedRecords, func(i, j int) bool {
		return strings.ToLower(sortedRecords[i].Title) < strings.ToLower(sortedRecords[j].Title)
	})

	visibleRecords := make([]FilmData, 0, len(sortedRecords))
	filteredEntries := make([]crawlartifact.FilteredFilmCodeEntry, 0)
	filteredSeen := map[string]struct{}{}
	addFilteredEntry := func(record FilmData, reason string, remark string) {
		code := extractFilmID(record.Title)
		if code == "" {
			code = extractFilmID(record.SourceLink)
		}
		code = strings.TrimSpace(strings.ToUpper(code))
		if code == "" {
			return
		}
		if _, exists := filteredSeen[code]; exists {
			return
		}
		filteredSeen[code] = struct{}{}
		filteredEntries = append(filteredEntries, crawlartifact.FilteredFilmCodeEntry{
			Code:   code,
			Reason: reason,
			Remark: remark,
		})
	}
	for _, record := range sortedRecords {
		if w.isReleaseDateFiltered(record.ReleaseDate) {
			remark := ""
			if strings.TrimSpace(record.ReleaseDate) != "" && strings.TrimSpace(w.minReleaseDate) != "" {
				remark = "release date " + strings.TrimSpace(record.ReleaseDate) + " before " + strings.TrimSpace(w.minReleaseDate)
			}
			addFilteredEntry(record, "releaseDate", remark)
			continue
		}
		visibleRecords = append(visibleRecords, record)
		if record.FilteredByActressCount || record.FilteredByFilmCode {
			reasons := make([]string, 0, 2)
			if record.FilteredByActressCount {
				reasons = append(reasons, "actressCount")
			}
			if record.FilteredByFilmCode {
				reasons = append(reasons, "filmCode")
			}
			addFilteredEntry(record, strings.Join(reasons, "+"), strings.TrimSpace(record.FilterRemark))
		}
	}
	return visibleRecords, filteredEntries
}

// buildMagnetLinesLocked derives the exact lines that will be written to
// magnet-links.txt from the visible records. It respects actress-count,
// film-code, no-magnet, and duplicate-link rules so live counters stay in sync
// with the final file.
func (w *Writer) buildMagnetLinesLocked(visibleRecords []FilmData) []string {
	var magnetLines []string
	seen := map[string]struct{}{}
	for _, record := range visibleRecords {
		if record.FilteredByActressCount || record.FilteredByFilmCode {
			continue
		}
		if record.Magnet == "" {
			continue
		}
		for _, link := range strings.Split(record.Magnet, "\n") {
			link = strings.TrimSpace(link)
			if link == "" {
				continue
			}
			lower := strings.ToLower(link)
			if _, ok := seen[lower]; ok {
				continue
			}
			seen[lower] = struct{}{}
			magnetLines = append(magnetLines, link)
		}
	}
	return magnetLines
}

// OutputMagnetCount returns the number of unique magnet links that would be
// written to magnet-links.txt right now. This is the value the UI should show
// as "completed" output count.
func (w *Writer) OutputMagnetCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	visibleRecords, _ := w.visibleRecordsLocked()
	return len(w.buildMagnetLinesLocked(visibleRecords))
}

// flushLocked persists the core artifacts plus derived cross-module handoff
// files. If filmData, magnet-links, crawl-profile, and organizer-codes disagree,
// inspect this write boundary first.
// ensureOutputDirsLocked creates the output/internal artifact directories on
// first write. Callers must hold w.mu.
func (w *Writer) ensureOutputDirsLocked() error {
	if w.outputDirsEnsured {
		return nil
	}
	if err := os.MkdirAll(w.outputDir, 0755); err != nil {
		return err
	}
	for _, key := range []string{"CrawlProfilePath", "FilmDataPath", "OrganizerCodesPath"} {
		var artifactDir string
		switch key {
		case "CrawlProfilePath":
			artifactDir = filepath.Dir(strings.TrimSpace(w.artifactPaths.CrawlProfilePath))
		case "FilmDataPath":
			artifactDir = filepath.Dir(strings.TrimSpace(w.artifactPaths.FilmDataPath))
		case "OrganizerCodesPath":
			artifactDir = filepath.Dir(strings.TrimSpace(w.artifactPaths.OrganizerCodesPath))
		}
		if artifactDir == "" {
			continue
		}
		if err := os.MkdirAll(artifactDir, 0755); err != nil {
			return err
		}
	}
	w.outputDirsEnsured = true
	return nil
}

func (w *Writer) flushLocked() error {
	if !w.dirty && !w.metadataDirty {
		return nil
	}
	if err := w.ensureOutputDirsLocked(); err != nil {
		return err
	}

	runPaths := crawlartifact.ResolveCrawlRunPaths(w.outputDir)
	jsonPath := runPaths.FilmDataPath
	visibleRecords, filteredEntries := w.visibleRecordsLocked()

	if w.dirty {
		jsonBytes, err := json.MarshalIndent(visibleRecords, "", "  ")
		if err != nil {
			return err
		}
		if err := common.WriteFileAtomic(jsonPath, jsonBytes, 0o644); err != nil {
			return err
		}
	}

	magnetLines := w.buildMagnetLinesLocked(visibleRecords)
	magnetPath := runPaths.MagnetPath
	if w.dirty {
		if err := common.WriteFileAtomic(magnetPath, []byte(strings.Join(magnetLines, "\r\n")), 0o644); err != nil {
			return err
		}
	}

	// Filtered-code export is a user-facing review aid. Keep it derived from the
	// persisted filmData snapshot so UI state, live logs, and final files stay in sync.
	filteredCodesPath := runPaths.FilteredCodesPath
	filteredFilmCodesPath := runPaths.FilteredFilmCodesPath
	if len(filteredEntries) == 0 {
		_ = os.Remove(filteredCodesPath)
		_ = os.Remove(filteredFilmCodesPath)
	} else if w.dirty {
		sort.Slice(filteredEntries, func(i, j int) bool {
			return strings.ToLower(filteredEntries[i].Code) < strings.ToLower(filteredEntries[j].Code)
		})
		filteredCodes := make([]string, 0, len(filteredEntries))
		for _, entry := range filteredEntries {
			filteredCodes = append(filteredCodes, entry.Code)
		}
		if err := common.WriteUTF8TextFile(filteredCodesPath, strings.Join(filteredCodes, "、")+"\r\n"); err != nil {
			return err
		}
		filteredArtifact := crawlartifact.FilteredCodesArtifact{
			SchemaVersion: crawlartifact.CurrentSchemaVersion,
			RunID:         strings.TrimSpace(w.metadata.RunID),
			CompletedAt:   strings.TrimSpace(w.metadata.CompletedAt),
			FilteredCodes: filteredEntries,
		}
		if err := writeJSONFile(filteredFilmCodesPath, filteredArtifact); err != nil {
			return err
		}
	}

	if err := w.writeDerivedArtifactsLocked(visibleRecords, filteredEntries); err != nil {
		return err
	}

	w.dirty = false
	w.metadataDirty = false
	return nil
}

func (w *Writer) RecordCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.records)
}

// MagnetOutputCount returns the exact number of magnet lines the current
// records produce (the line count of magnet-links.txt).
func (w *Writer) MagnetOutputCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	visible, _ := w.visibleRecordsLocked()
	return len(w.buildMagnetLinesLocked(visible))
}

// FilteredFilmCodeEntries returns the operator-filter exclusions currently in
// effect, classified by reason (film-code / actress-count / release-date) so
// review surfaces can show them as separate categories.
func (w *Writer) FilteredFilmCodeEntries() []crawlartifact.FilteredFilmCodeEntry {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, entries := w.visibleRecordsLocked()
	return entries
}

func (w *Writer) WriteUnfinishedReport(lines []string) error {
	path := crawlartifact.DefaultUnfinishedReportPath(w.outputDir)
	if len(lines) == 0 {
		_ = os.Remove(path)
		return nil
	}
	if err := w.ensureOutputDirsLocked(); err != nil {
		return err
	}
	seen := map[string]struct{}{}
	unique := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		unique = append(unique, trimmed)
	}
	return common.WriteUTF8TextFile(path, strings.Join(unique, "\r\n")+"\r\n")
}

func (w *Writer) CleanupLegacyArtifacts() {
	artifacts := []string{
		filepath.Join(w.outputDir, "task-state.json"),
		filepath.Join(w.outputDir, "validation-report.json"),
	}
	for _, target := range artifacts {
		_ = os.Remove(target)
	}
}

func extractFilmID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	upper := strings.ToUpper(value)
	dashIdx := strings.Index(upper, "-")
	if dashIdx <= 0 || dashIdx >= len(upper)-1 {
		return ""
	}
	prefix := upper[:dashIdx]
	suffix := upper[dashIdx+1:]

	hasLetter := strings.ContainsAny(prefix, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	if !hasLetter {
		return ""
	}
	if len(suffix) == 0 || suffix[0] < '0' || suffix[0] > '9' {
		return ""
	}
	digitEnd := 0
	for digitEnd < len(suffix) && suffix[digitEnd] >= '0' && suffix[digitEnd] <= '9' {
		digitEnd++
	}
	if digitEnd == 0 {
		return ""
	}
	return prefix + "-" + suffix[:digitEnd]
}

func normalizeSourceLink(link string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(link), "/")
	idx := strings.LastIndex(trimmed, "/")
	if idx >= 0 && idx < len(trimmed)-1 {
		return strings.ToLower(trimmed[idx+1:])
	}
	return strings.ToLower(trimmed)
}

// writeDerivedArtifactsLocked is the handoff point from crawler-owned records to
// cross-module artifacts consumed by organizer and subscription flows.
func (w *Writer) writeDerivedArtifactsLocked(sortedRecords []FilmData, filteredEntries []crawlartifact.FilteredFilmCodeEntry) error {
	paths := w.artifactPaths
	if strings.TrimSpace(paths.OutputDir) == "" {
		paths = crawlartifact.ResolveCrawlOutputPaths(w.outputDir)
	}
	if paths.OutputDir == "" {
		return nil
	}
	if strings.TrimSpace(paths.FilmDataPath) != "" {
		if err := writeJSONFile(paths.FilmDataPath, sortedRecords); err != nil {
			return err
		}
	}

	profile := crawlartifact.CrawlProfileArtifact{
		SchemaVersion:  crawlartifact.CurrentSchemaVersion,
		RunID:          strings.TrimSpace(w.metadata.RunID),
		CompletedAt:    strings.TrimSpace(w.metadata.CompletedAt),
		ActressName:    strings.TrimSpace(w.metadata.ActressName),
		CrawlURL:       strings.TrimSpace(w.metadata.CrawlURL),
		TargetCount:    maxInt(w.metadata.TargetCount, 0),
		CompletedCount: maxInt(completedCountOrRecordCount(w.metadata.CompletedCount, len(sortedRecords)), 0),
		ItemsPerPage:   maxInt(w.metadata.ItemsPerPage, 0),
		TotalPages:     maxInt(w.metadata.TotalPages, 0),
		OutputDir:      paths.OutputDir,
		FilmDataPath:   paths.FilmDataPath,
		SiteBase:       strings.TrimSpace(w.metadata.SiteBase),
		FilteredCodes:  append([]crawlartifact.FilteredFilmCodeEntry(nil), filteredEntries...),
	}
	if err := writeJSONFile(paths.CrawlProfilePath, profile); err != nil {
		return err
	}

	codes, codeEntries := buildOrganizerArtifactData(sortedRecords)
	organizerArtifact := crawlartifact.OrganizerCodesArtifact{
		SchemaVersion:   crawlartifact.CurrentSchemaVersion,
		RunID:           profile.RunID,
		CompletedAt:     profile.CompletedAt,
		ActressName:     firstNonEmpty(profile.ActressName, detectPrimaryActressName(sortedRecords)),
		OutputDir:       paths.OutputDir,
		FilmDataPath:    paths.FilmDataPath,
		TotalRecords:    len(sortedRecords),
		UniqueCodeCount: len(codes),
		Codes:           codes,
		CodeEntries:     codeEntries,
		FilteredCodes:   append([]crawlartifact.FilteredFilmCodeEntry(nil), filteredEntries...),
	}
	if err := writeJSONFile(paths.OrganizerCodesPath, organizerArtifact); err != nil {
		return err
	}

	userDataDir := crawlartifact.InferUserDataDirFromArtifactPath(paths.CrawlProfilePath)
	if strings.TrimSpace(userDataDir) != "" {
		if err := crawlartifact.UpsertCacheSnapshot(userDataDir, crawlartifact.BuildCacheSnapshot(paths, profile, "crawler")); err != nil {
			return err
		}
		if err := crawlartifact.ArchiveCompletedCacheSnapshot(userDataDir, paths, profile, "crawler"); err != nil {
			return err
		}
	}
	return nil
}

func writeJSONFile(filePath string, value any) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return common.WriteFileAtomic(filePath, payload, 0o644)
}

func completedCountOrRecordCount(completedCount int, recordCount int) int {
	if completedCount > 0 {
		return completedCount
	}
	return recordCount
}

// buildOrganizerArtifactData derives the organizer-facing unique-code snapshot
// from persisted crawl records only.
func buildOrganizerArtifactData(records []FilmData) ([]string, []crawlartifact.CodeEntry) {
	codeMap := map[string]*crawlartifact.CodeEntry{}
	for _, record := range records {
		code := extractFilmID(record.Title)
		if code == "" {
			code = extractFilmID(record.SourceLink)
		}
		if code == "" {
			continue
		}

		entry, exists := codeMap[code]
		if !exists {
			entry = &crawlartifact.CodeEntry{
				Code:   code,
				Title:  strings.TrimSpace(record.Title),
				Maker:  strings.TrimSpace(record.Maker),
				Label:  strings.TrimSpace(record.Label),
				Series: strings.TrimSpace(record.Series),
			}
			codeMap[code] = entry
		}
		if entry.Title == "" {
			entry.Title = strings.TrimSpace(record.Title)
		}
		if entry.Maker == "" {
			entry.Maker = strings.TrimSpace(record.Maker)
		}
		if entry.Label == "" {
			entry.Label = strings.TrimSpace(record.Label)
		}
		if entry.Series == "" {
			entry.Series = strings.TrimSpace(record.Series)
		}

		appendUniqueMagnetEntries(entry, record)
	}

	codes := make([]string, 0, len(codeMap))
	for code := range codeMap {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	codeEntries := make([]crawlartifact.CodeEntry, 0, len(codes))
	for _, code := range codes {
		entry := codeMap[code]
		sort.Slice(entry.Magnets, func(i, j int) bool {
			return strings.ToLower(entry.Magnets[i].Link) < strings.ToLower(entry.Magnets[j].Link)
		})
		codeEntries = append(codeEntries, *entry)
	}
	return codes, codeEntries
}

func appendUniqueMagnetEntries(entry *crawlartifact.CodeEntry, record FilmData) {
	if entry == nil {
		return
	}

	seen := map[string]struct{}{}
	for _, item := range entry.Magnets {
		seen[strings.ToLower(strings.TrimSpace(item.Link))] = struct{}{}
	}

	appendMagnet := func(link string, size string, displayName string) {
		trimmed := strings.TrimSpace(link)
		if trimmed == "" {
			return
		}
		key := strings.ToLower(trimmed)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		entry.Magnets = append(entry.Magnets, crawlartifact.MagnetEntry{
			Link:        trimmed,
			Size:        strings.TrimSpace(size),
			DisplayName: strings.TrimSpace(displayName),
		})
	}

	for _, item := range record.MagnetLinks {
		appendMagnet(item.Link, item.Size, item.DisplayName)
	}
	for _, line := range strings.Split(record.Magnet, "\n") {
		appendMagnet(line, "", "")
	}
}

func detectPrimaryActressName(records []FilmData) string {
	type actressStat struct {
		Name  string
		Count int
		Order int
	}

	stats := map[string]*actressStat{}
	nextOrder := 0
	for _, record := range records {
		seenInRecord := map[string]struct{}{}
		for _, actress := range record.Actress {
			name := strings.TrimSpace(actress)
			key := strings.ToLower(strings.Join(strings.Fields(name), ""))
			if key == "" {
				continue
			}
			if _, exists := seenInRecord[key]; exists {
				continue
			}
			seenInRecord[key] = struct{}{}
			stat, exists := stats[key]
			if !exists {
				stat = &actressStat{Name: name, Order: nextOrder}
				stats[key] = stat
				nextOrder++
			}
			stat.Count++
		}
	}

	best := actressStat{Order: int(^uint(0) >> 1)}
	found := false
	for _, stat := range stats {
		if !found || stat.Count > best.Count || (stat.Count == best.Count && stat.Order < best.Order) || (stat.Count == best.Count && stat.Order == best.Order && stat.Name < best.Name) {
			best = *stat
			found = true
		}
	}
	if !found {
		return ""
	}
	return best.Name
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
