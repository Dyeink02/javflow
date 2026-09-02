package organizer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"javflow/internal/contracts/crawlartifact"
)

// crawl_artifacts.go owns organizer-side consumption of persisted crawl output
// artifacts such as filmData.json, crawl-profile.json, and organizer-codes.json.
//
// Ownership summary:
// 1) normalize crawl artifact records into organizer-friendly code/magnet shapes
// 2) keep artifact-source precedence and source-type/source-path resolution local
// 3) provide the read-only handoff from crawler outputs into organizer preload
//    state
//
// This file does not participate in live crawl runtime control.
//
// File map for maintainers:
// 1) organizer-facing artifact DTOs
// 2) magnet/code normalization helpers
// 3) crawl artifact import/load routines
// 4) conversion into organizer preload/runtime contracts

// MagnetEntry and CodeEntry are organizer-facing normalized crawl artifacts.
// Keeping their normalization logic in one file makes it easier to diagnose
// "crawl data looks fine, organizer consumed it wrong" style bugs.
//
// Organizer intentionally consumes persisted crawl artifacts, primarily
// filmData.json and derived magnet fields. It should not depend on the crawler
// controller/runtime to organize files, because that coupling slows down
// post-download troubleshooting and increases cross-module breakage.
//
// Practical split:
// - artifact lookup and precedence stay here
// - organizer execution and file moves stay in `run*.go`
// - subscription import logic stays in `internal/avsubscriptionv2`
type MagnetEntry struct {
	Link        string `json:"link"`
	Size        string `json:"size"`
	DisplayName string `json:"displayName,omitempty"`
}

type CodeEntry struct {
	Code    string        `json:"code"`
	Title   string        `json:"title,omitempty"`
	Maker   string        `json:"maker,omitempty"`
	Label   string        `json:"label,omitempty"`
	Series  string        `json:"series,omitempty"`
	Magnets []MagnetEntry `json:"magnets"`
}

type LoadCrawlFilmCodesResult struct {
	OutputDir          string                                `json:"outputDir"`
	FilmDataPath       string                                `json:"filmDataPath"`
	OrganizerCodesPath string                                `json:"organizerCodesPath,omitempty"`
	SourceType         string                                `json:"sourceType"`
	ActressName        string                                `json:"actressName,omitempty"`
	TotalRecords       int                                   `json:"totalRecords"`
	CodeCount          int                                   `json:"codeCount"`
	ActualMagnetCount  int                                   `json:"actualMagnetCount"`
	MagnetPath         string                                `json:"magnetPath,omitempty"`
	Codes              []string                              `json:"codes"`
	CodeEntries        []CodeEntry                           `json:"codeEntries"`
	FilteredCodes      []crawlartifact.FilteredFilmCodeEntry `json:"filteredCodes,omitempty"`
	PreloadedExpected  PreloadedExpectedCodes                `json:"preloadedExpected"`
}

// ToPreloadedExpectedCodes converts organizer artifact imports into the
// runtime preload shape used by RunOrganizer. This keeps the read-only crawl
// artifact contract identical whether the data is loaded in the UI first or
// lazily resolved by the organizer service itself.
func (r LoadCrawlFilmCodesResult) ToPreloadedExpectedCodes() PreloadedExpectedCodes {
	sourceType := resolveExpectedSourceType(
		r.SourceType,
		"",
		r.FilmDataPath,
		r.OrganizerCodesPath,
		len(r.Codes) > 0 || len(r.CodeEntries) > 0,
		"",
	)
	sourcePath := resolveExpectedSourcePath(sourceType, "", r.FilmDataPath, r.OrganizerCodesPath)

	return PreloadedExpectedCodes{
		SourceType:         sourceType,
		SourcePath:         sourcePath,
		OutputDir:          strings.TrimSpace(r.OutputDir),
		FilmDataPath:       strings.TrimSpace(r.FilmDataPath),
		OrganizerCodesPath: strings.TrimSpace(r.OrganizerCodesPath),
		ActressName:        strings.TrimSpace(r.ActressName),
		TotalRecords:       r.TotalRecords,
		CodeCount:          r.CodeCount,
		ActualMagnetCount:  r.ActualMagnetCount,
		MagnetPath:         strings.TrimSpace(r.MagnetPath),
		Codes:              append([]string(nil), r.Codes...),
		CodeEntries:        append([]CodeEntry(nil), r.CodeEntries...),
	}
}

func normalizeMagnetEntry(rawValue any) *MagnetEntry {
	switch value := rawValue.(type) {
	case string:
		link := strings.TrimSpace(value)
		if link == "" {
			return nil
		}
		return &MagnetEntry{Link: link, Size: ""}
	case map[string]any:
		link := strings.TrimSpace(crawlartifact.AnyToString(value["link"]))
		if link == "" {
			link = strings.TrimSpace(crawlartifact.AnyToString(value["magnet"]))
		}
		if link == "" {
			return nil
		}
		return &MagnetEntry{
			Link:        link,
			Size:        strings.TrimSpace(crawlartifact.AnyToString(value["size"])),
			DisplayName: strings.TrimSpace(crawlartifact.AnyToString(value["displayName"])),
		}
	default:
		return nil
	}
}

func normalizeMagnetEntries(rawValue any) []MagnetEntry {
	var list []any
	switch value := rawValue.(type) {
	case []any:
		list = value
	case []MagnetEntry:
		for _, item := range value {
			list = append(list, map[string]any{
				"link":        item.Link,
				"size":        item.Size,
				"displayName": item.DisplayName,
			})
		}
	case string:
		for _, item := range strings.Split(value, "\n") {
			trimmed := strings.TrimSpace(item)
			if trimmed != "" {
				list = append(list, trimmed)
			}
		}
	default:
		return nil
	}

	output := make([]MagnetEntry, 0, len(list))
	seen := map[string]struct{}{}
	for _, item := range list {
		entry := normalizeMagnetEntry(item)
		if entry == nil {
			continue
		}

		key := strings.ToLower(entry.Link)
		if _, exists := seen[key]; exists {
			continue
		}

		seen[key] = struct{}{}
		output = append(output, *entry)
	}

	return output
}

func mergeMagnetEntries(groups ...any) []MagnetEntry {
	merged := make([]MagnetEntry, 0)
	seen := map[string]struct{}{}

	for _, group := range groups {
		for _, entry := range normalizeMagnetEntries(group) {
			key := strings.ToLower(entry.Link)
			if _, exists := seen[key]; exists {
				continue
			}

			seen[key] = struct{}{}
			merged = append(merged, entry)
		}
	}

	return merged
}

func extractRecordCode(record map[string]any) string {
	candidates := []string{
		crawlartifact.AnyToString(record["filmCode"]),
		crawlartifact.AnyToString(record["sourceLink"]),
		crawlartifact.AnyToString(record["code"]),
		crawlartifact.AnyToString(record["title"]),
		crawlartifact.AnyToString(record["fileName"]),
	}

	for _, candidate := range candidates {
		if filmID := extractFilmID(candidate); filmID != "" {
			return filmID
		}
	}

	return ""
}

func extractRecordMagnetEntries(record map[string]any) []MagnetEntry {
	return mergeMagnetEntries(record["backupMagnetLinks"], record["magnetLinks"], record["magnet"], record["magnets"])
}

func firstRecordString(record map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(crawlartifact.AnyToString(record[key])); value != "" {
			return value
		}
	}
	return ""
}

// LoadCrawlFilmCodes is organizer's read-only adapter over crawl outputs. It
// prefers the derived organizer-codes artifact when available, then falls back
// to filmData normalization for older runs.
func (s *Service) LoadCrawlFilmCodes(outputDir string) (LoadCrawlFilmCodesResult, error) {
	outputDir = crawlartifact.ResolveEffectiveCrawlOutputDir(outputDir)
	internalPaths := crawlartifact.ResolveInternalArtifactPaths(s.paths.UserData, outputDir)
	if pathIsFile(internalPaths.OrganizerCodesPath) {
		if loaded, err := s.loadOrganizerCodesArtifact(outputDir); err == nil {
			return s.withMagnetStats(outputDir, loaded), nil
		}
	}
	if pathIsFile(internalPaths.FilmDataPath) {
		paths, records, err := crawlartifact.ReadFilmDataRecordsWithUserData(outputDir, s.paths.UserData)
		if err == nil {
			return s.withMagnetStats(outputDir, s.withFilteredCodes(outputDir, buildFilmDataCodeResult(paths, records))), nil
		}
	}

	visiblePaths := crawlartifact.ResolveCrawlOutputPaths(outputDir)
	if pathIsFile(visiblePaths.OrganizerCodesPath) {
		if loaded, err := s.loadOrganizerCodesArtifact(outputDir); err == nil {
			return s.withMagnetStats(outputDir, loaded), nil
		}
	}

	// A local discovery snapshot is the final safe fallback for old libraries
	// where the user no longer has either public or app-managed crawler JSON. It
	// is intentionally checked after crawler artifacts so an older local scan
	// can never override a newer authoritative crawl list.
	if discovered, err := s.loadDiscoveredCodesArtifact(outputDir); err == nil && discovered.CodeCount > 0 {
		return s.withMagnetStats(outputDir, discovered), nil
	}

	paths, records, err := crawlartifact.ReadFilmDataRecordsWithUserData(outputDir, s.paths.UserData)
	if err != nil {
		return LoadCrawlFilmCodesResult{}, fmt.Errorf("在 %s 中未找到可用的番号名单（需要 filmData.json 或 organizer-codes.json），请确认已选择正确的爬虫结果目录或整理快照：%w", outputDir, err)
	}
	return s.withMagnetStats(outputDir, s.withFilteredCodes(outputDir, buildFilmDataCodeResult(paths, records))), nil
}

// withMagnetStats prefers the final magnet TXT, then reconstructs the same
// effective output count from filmData.json when the TXT is absent from a
// hidden artifact cache. The organizer UI must show actual magnet output, not
// completed film records or code-entry count.
func (s *Service) withMagnetStats(outputDir string, result LoadCrawlFilmCodesResult) LoadCrawlFilmCodesResult {
	magnetPath := crawlartifact.ResolveCrawlRunPaths(outputDir).MagnetPath
	result.MagnetPath = magnetPath
	result.ActualMagnetCount = -1
	if pathIsFile(magnetPath) {
		if count, err := countMagnetLinksInTextFile(magnetPath); err == nil {
			result.ActualMagnetCount = count
			result.PreloadedExpected = result.ToPreloadedExpectedCodes()
			return result
		}
	}

	if count, found := s.countMagnetLinksFromFilmData(outputDir); found {
		result.ActualMagnetCount = count
		result.PreloadedExpected = result.ToPreloadedExpectedCodes()
		return result
	}

	// organizer-codes.json is a smaller compatibility artifact. It is only the
	// last fallback because older versions may merge backup candidates into its
	// normalized magnet list.
	seen := map[string]struct{}{}
	for _, entry := range result.CodeEntries {
		for _, magnet := range entry.Magnets {
			link := strings.TrimSpace(magnet.Link)
			if link == "" || !strings.HasPrefix(strings.ToLower(link), "magnet:") {
				continue
			}
			seen[strings.ToLower(link)] = struct{}{}
		}
	}
	if len(result.CodeEntries) > 0 {
		result.ActualMagnetCount = len(seen)
	}
	result.PreloadedExpected = result.ToPreloadedExpectedCodes()
	return result
}

func countMagnetLinksInTextFile(path string) (int, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return -1, err
	}
	seen := map[string]struct{}{}
	for _, line := range strings.Split(string(contents), "\n") {
		link := strings.TrimSpace(line)
		if link == "" || !strings.HasPrefix(strings.ToLower(link), "magnet:") {
			continue
		}
		seen[strings.ToLower(link)] = struct{}{}
	}
	return len(seen), nil
}

func (s *Service) countMagnetLinksFromFilmData(outputDir string) (int, bool) {
	_, records, err := crawlartifact.ReadFilmDataRecordsWithUserData(outputDir, s.paths.UserData)
	if err != nil {
		return -1, false
	}

	seen := map[string]struct{}{}
	for _, record := range records {
		if boolValue(record["filteredByActressCount"]) ||
			boolValue(record["filteredByFilmCode"]) ||
			boolValue(record["filteredByReleaseDate"]) {
			continue
		}
		for _, magnet := range mergeMagnetEntries(record["magnetLinks"], record["magnet"]) {
			link := strings.TrimSpace(magnet.Link)
			if link == "" || !strings.HasPrefix(strings.ToLower(link), "magnet:") {
				continue
			}
			seen[strings.ToLower(link)] = struct{}{}
		}
	}
	return len(seen), true
}

func boolValue(value any) bool {
	typed, ok := value.(bool)
	return ok && typed
}

func (s *Service) loadDiscoveredCodesArtifact(outputDir string) (LoadCrawlFilmCodesResult, error) {
	path := filepath.Join(strings.TrimSpace(outputDir), stateDirName, discoveredCodesFileName)
	payload, err := os.ReadFile(path)
	if err != nil {
		return LoadCrawlFilmCodesResult{}, err
	}
	var artifact localDiscoveryArtifact
	if err := json.Unmarshal(payload, &artifact); err != nil {
		return LoadCrawlFilmCodesResult{}, err
	}
	if len(artifact.Codes) == 0 {
		return LoadCrawlFilmCodesResult{}, fmt.Errorf("本地识别快照没有可用番号")
	}
	codes := make([]string, 0, len(artifact.Codes))
	entries := make([]CodeEntry, 0, len(artifact.Codes))
	for _, item := range artifact.Codes {
		code := normalizeFilmID(item.Code)
		if code == "" {
			continue
		}
		codes = append(codes, code)
		entries = append(entries, CodeEntry{Code: code})
	}
	sort.Strings(codes)
	result := LoadCrawlFilmCodesResult{
		OutputDir:    outputDir,
		FilmDataPath: path,
		SourceType:   "local-discovery",
		TotalRecords: len(codes),
		CodeCount:    len(codes),
		Codes:        codes,
		CodeEntries:  entries,
	}
	result.PreloadedExpected = result.ToPreloadedExpectedCodes()
	return result, nil
}

// withFilteredCodes supplements a filmData-derived result with the structured
// filtered-code ledger when it exists. Organizer-codes artifact results already
// carry this field, so this helper only fills the fallback path.
func (s *Service) withFilteredCodes(outputDir string, result LoadCrawlFilmCodesResult) LoadCrawlFilmCodesResult {
	result.FilteredCodes = s.loadFilteredCodesFromOutput(outputDir)
	return result
}

// loadFilteredCodesFromOutput reads the structured filtered-code ledger written
// by the crawler alongside filmData.json and magnet-links.txt.
func (s *Service) loadFilteredCodesFromOutput(outputDir string) []crawlartifact.FilteredFilmCodeEntry {
	if strings.TrimSpace(outputDir) == "" {
		return nil
	}

	artifactPath := crawlartifact.ResolveCrawlRunPaths(outputDir).FilteredFilmCodesPath
	if strings.TrimSpace(s.paths.UserData) != "" {
		internalPaths := crawlartifact.ResolveInternalArtifactPaths(s.paths.UserData, outputDir)
		if internalPaths.FilmDataPath != "" {
			artifactPath = filepath.Join(filepath.Dir(internalPaths.FilmDataPath), crawlartifact.FilteredFilmCodesFile)
		}
	}
	if artifactPath == "" {
		return nil
	}
	info, err := os.Stat(artifactPath)
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}

	payload, err := os.ReadFile(artifactPath)
	if err != nil {
		return nil
	}

	var artifact crawlartifact.FilteredCodesArtifact
	if err := json.Unmarshal(payload, &artifact); err != nil {
		return nil
	}

	result := make([]crawlartifact.FilteredFilmCodeEntry, 0, len(artifact.FilteredCodes))
	for _, entry := range artifact.FilteredCodes {
		result = append(result, crawlartifact.FilteredFilmCodeEntry{
			Code:   strings.TrimSpace(entry.Code),
			Reason: strings.TrimSpace(entry.Reason),
			Remark: strings.TrimSpace(entry.Remark),
		})
	}
	return result
}

func pathIsFile(path string) bool {
	info, err := os.Stat(strings.TrimSpace(path))
	return err == nil && info.Mode().IsRegular()
}

func buildFilmDataCodeResult(paths crawlartifact.CrawlOutputPaths, records []map[string]any) LoadCrawlFilmCodesResult {
	codeEntryMap := map[string]*CodeEntry{}
	for _, record := range records {
		code := extractRecordCode(record)
		if code == "" {
			continue
		}

		entry := codeEntryMap[code]
		if entry == nil {
			entry = &CodeEntry{Code: code}
			codeEntryMap[code] = entry
		}
		if entry.Title == "" {
			entry.Title = strings.TrimSpace(crawlartifact.AnyToString(record["title"]))
		}
		if entry.Maker == "" {
			entry.Maker = firstRecordString(record, "maker", "studio", "manufacturer")
		}
		if entry.Label == "" {
			entry.Label = firstRecordString(record, "label")
		}
		if entry.Series == "" {
			entry.Series = firstRecordString(record, "series")
		}
		entry.Magnets = mergeMagnetEntries(entry.Magnets, extractRecordMagnetEntries(record))
	}

	codes := make([]string, 0, len(codeEntryMap))
	for code := range codeEntryMap {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	codeEntries := make([]CodeEntry, 0, len(codes))
	for _, code := range codes {
		entry := *codeEntryMap[code]
		entry.Magnets = mergeMagnetEntries(entry.Magnets)
		codeEntries = append(codeEntries, entry)
	}

	result := LoadCrawlFilmCodesResult{
		OutputDir:          paths.OutputDir,
		FilmDataPath:       paths.FilmDataPath,
		OrganizerCodesPath: paths.OrganizerCodesPath,
		SourceType:         codeSourceFilmData,
		TotalRecords:       len(records),
		CodeCount:          len(codes),
		Codes:              codes,
		CodeEntries:        codeEntries,
	}
	result.PreloadedExpected = result.ToPreloadedExpectedCodes()
	return result
}

// loadOrganizerCodesArtifact reads the derived code snapshot written by the
// crawler. This path is preferred because it already encodes the crawler's
// unique-code view and avoids organizer re-deriving it from loose filmData.
func (s *Service) loadOrganizerCodesArtifact(outputDir string) (LoadCrawlFilmCodesResult, error) {
	paths, artifact, err := crawlartifact.ReadOrganizerCodesArtifactWithUserData(outputDir, s.paths.UserData)
	if err != nil {
		return LoadCrawlFilmCodesResult{}, err
	}

	codeEntries := make([]CodeEntry, 0, len(artifact.CodeEntries))
	for _, entry := range artifact.CodeEntries {
		magnets := make([]MagnetEntry, 0, len(entry.Magnets))
		for _, magnet := range entry.Magnets {
			magnets = append(magnets, MagnetEntry{
				Link:        strings.TrimSpace(magnet.Link),
				Size:        strings.TrimSpace(magnet.Size),
				DisplayName: strings.TrimSpace(magnet.DisplayName),
			})
		}
		codeEntries = append(codeEntries, CodeEntry{
			Code:    strings.TrimSpace(entry.Code),
			Title:   strings.TrimSpace(entry.Title),
			Maker:   strings.TrimSpace(entry.Maker),
			Label:   strings.TrimSpace(entry.Label),
			Series:  strings.TrimSpace(entry.Series),
			Magnets: magnets,
		})
	}

	filteredCodes := make([]crawlartifact.FilteredFilmCodeEntry, 0, len(artifact.FilteredCodes))
	for _, entry := range artifact.FilteredCodes {
		filteredCodes = append(filteredCodes, crawlartifact.FilteredFilmCodeEntry{
			Code:   strings.TrimSpace(entry.Code),
			Reason: strings.TrimSpace(entry.Reason),
			Remark: strings.TrimSpace(entry.Remark),
		})
	}

	result := LoadCrawlFilmCodesResult{
		OutputDir:          paths.OutputDir,
		FilmDataPath:       firstNonEmpty(artifact.FilmDataPath, paths.FilmDataPath),
		OrganizerCodesPath: paths.OrganizerCodesPath,
		SourceType:         codeSourceOrganizerCodes,
		ActressName:        strings.TrimSpace(artifact.ActressName),
		TotalRecords:       artifact.TotalRecords,
		CodeCount:          len(artifact.Codes),
		Codes:              append([]string(nil), artifact.Codes...),
		CodeEntries:        codeEntries,
		FilteredCodes:      filteredCodes,
	}
	result.PreloadedExpected = result.ToPreloadedExpectedCodes()
	return result, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func sameExpectedSourcePath(left string, right string) bool {
	normalizedLeft := strings.TrimSpace(left)
	normalizedRight := strings.TrimSpace(right)
	if normalizedLeft == "" || normalizedRight == "" {
		return false
	}

	return strings.EqualFold(filepath.Clean(normalizedLeft), filepath.Clean(normalizedRight))
}

// Organizer shows crawl artifact provenance in UI/logs. Keep the mapping from
// source type to preferred source path centralized so compatibility paths do
// not drift into contradictory metadata.
func resolveExpectedSourceType(sourceType string, sourcePath string, filmDataPath string, organizerCodesPath string, hasCodes bool, preferredType string) string {
	normalizedSourceType := strings.TrimSpace(sourceType)
	if normalizedSourceType != "" {
		return normalizedSourceType
	}

	normalizedSourcePath := strings.TrimSpace(sourcePath)
	normalizedFilmDataPath := strings.TrimSpace(filmDataPath)
	normalizedOrganizerCodesPath := strings.TrimSpace(organizerCodesPath)
	normalizedPreferredType := strings.TrimSpace(preferredType)

	if sameExpectedSourcePath(normalizedSourcePath, normalizedOrganizerCodesPath) {
		return codeSourceOrganizerCodes
	}
	if sameExpectedSourcePath(normalizedSourcePath, normalizedFilmDataPath) {
		return codeSourceFilmData
	}
	if normalizedOrganizerCodesPath != "" && normalizedFilmDataPath == "" {
		return codeSourceOrganizerCodes
	}
	if normalizedFilmDataPath != "" && normalizedOrganizerCodesPath == "" {
		return codeSourceFilmData
	}
	if normalizedPreferredType != "" && normalizedPreferredType != codeSourcePayload {
		return normalizedPreferredType
	}
	if normalizedSourcePath != "" || hasCodes || normalizedPreferredType == codeSourcePayload {
		return codeSourcePayload
	}

	return ""
}

// resolveExpectedSourcePath pairs with resolveExpectedSourceType so logs, UI,
// and lazy organizer loads all point at the same artifact path choice.
func resolveExpectedSourcePath(sourceType string, sourcePath string, filmDataPath string, organizerCodesPath string) string {
	normalizedSourceType := strings.TrimSpace(sourceType)
	normalizedSourcePath := strings.TrimSpace(sourcePath)
	normalizedFilmDataPath := strings.TrimSpace(filmDataPath)
	normalizedOrganizerCodesPath := strings.TrimSpace(organizerCodesPath)

	switch normalizedSourceType {
	case codeSourceOrganizerCodes:
		return firstNonEmpty(normalizedOrganizerCodesPath, normalizedSourcePath, normalizedFilmDataPath)
	case codeSourceFilmData:
		return firstNonEmpty(normalizedFilmDataPath, normalizedSourcePath, normalizedOrganizerCodesPath)
	case codeSourcePayload:
		return firstNonEmpty(normalizedSourcePath, normalizedFilmDataPath, normalizedOrganizerCodesPath)
	default:
		return firstNonEmpty(normalizedSourcePath, normalizedOrganizerCodesPath, normalizedFilmDataPath)
	}
}

// ResolvePreloadedExpectedCodes normalizes organizer expected-code inputs.
// Callers may provide:
// 1) explicit preloaded data from a prior artifact import
// 2) legacy expectedCodes/expectedCodeEntries payload fields
// 3) only crawlOutputDir, in which case organizer lazily loads artifacts itself
//
// This keeps the organizer execution path read-only and artifact-based without
// forcing the frontend to be the only place that can hydrate expected codes.
func (s *Service) ResolvePreloadedExpectedCodes(options RunOptions) (PreloadedExpectedCodes, error) {
	resolved := ComposePreloadedExpectedCodes(
		options.PreloadedExpected,
		options.CrawlOutputDir,
		options.ExpectedCodes,
		options.ExpectedCodeEntries,
	)

	if len(resolved.Codes) == 0 && len(resolved.CodeEntries) == 0 {
		outputDir := strings.TrimSpace(options.CrawlOutputDir)
		if outputDir == "" {
			// Strict matching must never fall back to free-form filename guesses.
			// When no crawler list is supplied, create a read-only local discovery
			// snapshot and use only its unambiguous codes.
			if options.StrictExpectedCodes && strings.TrimSpace(options.RootPath) != "" {
				discovered, discoverErr := s.DiscoverLocalCodes(DiscoverOptions{
					RootPath:              options.RootPath,
					IncludeSubdirectories: options.IncludeSubdirectories,
					VideoExtensions:       options.VideoExtensions,
				})
				if discoverErr != nil || discovered.CodeCount == 0 {
					return resolved, nil
				}
				return PreloadedExpectedCodes{
					SourceType:   "local-discovery",
					SourcePath:   discovered.StatePath,
					OutputDir:    discovered.RootPath,
					FilmDataPath: discovered.StatePath,
					CodeCount:    discovered.CodeCount,
					Codes:        discovered.Codes,
					CodeEntries:  discovered.CodeEntries,
					TotalRecords: discovered.VideoFiles,
				}, nil
			}
			return resolved, nil
		}

		loaded, err := s.LoadCrawlFilmCodes(outputDir)
		if err != nil {
			return PreloadedExpectedCodes{}, err
		}
		return loaded.PreloadedExpected, nil
	}

	return resolved, nil
}

// ComposePreloadedExpectedCodes collapses organizer crawl-code inputs into one
// normalized read-only snapshot without touching the filesystem.
//
// Preferred input:
// - explicit PreloadedExpected snapshot
//
// Compatibility supplement:
// - legacy ExpectedCodes / ExpectedCodeEntries payload fields
//
// Keeping this helper exported lets the Wails bridge reuse the exact same
// normalization rules as the organizer service, instead of maintaining a second
// parallel interpretation of what the expected-code payload means.
func ComposePreloadedExpectedCodes(preloaded PreloadedExpectedCodes, crawlOutputDir string, legacyCodes []string, legacyEntries []CodeEntry) PreloadedExpectedCodes {
	return mergePreloadedExpectedCodes(
		normalizePreloadedExpectedCodes(preloaded),
		PreloadedExpectedCodes{
			SourceType:  codeSourcePayload,
			OutputDir:   strings.TrimSpace(crawlOutputDir),
			Codes:       append([]string(nil), legacyCodes...),
			CodeEntries: append([]CodeEntry(nil), legacyEntries...),
		},
	)
}

// normalizePreloadedExpectedCodes is the canonical in-memory cleanup pass for
// organizer expected-code snapshots, regardless of whether they came from
// artifacts, legacy payloads, or bridge-preloaded data.
func normalizePreloadedExpectedCodes(input PreloadedExpectedCodes) PreloadedExpectedCodes {
	normalized := PreloadedExpectedCodes{
		SourceType:         strings.TrimSpace(input.SourceType),
		SourcePath:         strings.TrimSpace(input.SourcePath),
		OutputDir:          strings.TrimSpace(input.OutputDir),
		FilmDataPath:       strings.TrimSpace(input.FilmDataPath),
		OrganizerCodesPath: strings.TrimSpace(input.OrganizerCodesPath),
		ActressName:        strings.TrimSpace(input.ActressName),
		TotalRecords:       input.TotalRecords,
		ActualMagnetCount:  input.ActualMagnetCount,
		MagnetPath:         strings.TrimSpace(input.MagnetPath),
	}

	codeSet, _ := buildExpectedCodeSets(input.Codes)
	codeEntryMap := buildExpectedCodeEntryMap(input.CodeEntries)
	metadataByCode := map[string]CodeEntry{}
	for _, entry := range input.CodeEntries {
		code := normalizeFilmID(entry.Code)
		if code == "" {
			continue
		}
		current := metadataByCode[code]
		if current.Code == "" {
			current.Code = code
		}
		if current.Title == "" {
			current.Title = strings.TrimSpace(entry.Title)
		}
		if current.Maker == "" {
			current.Maker = strings.TrimSpace(entry.Maker)
		}
		if current.Label == "" {
			current.Label = strings.TrimSpace(entry.Label)
		}
		if current.Series == "" {
			current.Series = strings.TrimSpace(entry.Series)
		}
		metadataByCode[code] = current
	}
	for code := range codeEntryMap {
		codeSet[code] = struct{}{}
	}

	codes := make([]string, 0, len(codeSet))
	for code := range codeSet {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	codeEntries := make([]CodeEntry, 0, len(codeEntryMap))
	entryCodes := make([]string, 0, len(codeEntryMap))
	for code := range codeEntryMap {
		entryCodes = append(entryCodes, code)
	}
	sort.Strings(entryCodes)
	for _, code := range entryCodes {
		metadata := metadataByCode[code]
		codeEntries = append(codeEntries, CodeEntry{
			Code:    code,
			Title:   metadata.Title,
			Maker:   metadata.Maker,
			Label:   metadata.Label,
			Series:  metadata.Series,
			Magnets: mergeMagnetEntries(codeEntryMap[code]),
		})
	}

	normalized.Codes = codes
	normalized.CodeEntries = codeEntries
	normalized.CodeCount = len(codes)
	normalized.SourceType = resolveExpectedSourceType(
		normalized.SourceType,
		normalized.SourcePath,
		normalized.FilmDataPath,
		normalized.OrganizerCodesPath,
		normalized.CodeCount > 0,
		"",
	)
	normalized.SourcePath = resolveExpectedSourcePath(
		normalized.SourceType,
		normalized.SourcePath,
		normalized.FilmDataPath,
		normalized.OrganizerCodesPath,
	)

	return normalized
}

// mergePreloadedExpectedCodes gives explicit preloaded data priority while
// still accepting compatibility supplements from older payload fields.
func mergePreloadedExpectedCodes(primary PreloadedExpectedCodes, fallback PreloadedExpectedCodes) PreloadedExpectedCodes {
	merged := PreloadedExpectedCodes{
		SourceType:         firstNonEmpty(primary.SourceType, fallback.SourceType),
		SourcePath:         firstNonEmpty(primary.SourcePath, fallback.SourcePath),
		OutputDir:          firstNonEmpty(primary.OutputDir, fallback.OutputDir),
		FilmDataPath:       firstNonEmpty(primary.FilmDataPath, fallback.FilmDataPath),
		OrganizerCodesPath: firstNonEmpty(primary.OrganizerCodesPath, fallback.OrganizerCodesPath),
		ActressName:        firstNonEmpty(primary.ActressName, fallback.ActressName),
		TotalRecords:       organizerMaxInt(primary.TotalRecords, fallback.TotalRecords),
		ActualMagnetCount:  resolveMagnetCount(primary.ActualMagnetCount, fallback.ActualMagnetCount),
		MagnetPath:         firstNonEmpty(primary.MagnetPath, fallback.MagnetPath),
		Codes:              append(append([]string(nil), primary.Codes...), fallback.Codes...),
		CodeEntries:        append(append([]CodeEntry(nil), primary.CodeEntries...), fallback.CodeEntries...),
	}

	return normalizePreloadedExpectedCodes(merged)
}

func organizerMaxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func resolveMagnetCount(left int, right int) int {
	if left >= 0 {
		return left
	}
	return right
}
