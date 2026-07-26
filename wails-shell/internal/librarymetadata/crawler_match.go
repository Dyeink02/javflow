// Ownership summary:
//   This file matches local videos against crawler artifact records for metadata fallback.
//
// File map for maintainers:
//   1) CrawlerRecord and CrawlSource types.
//   2) Artifact loading and code-keyed lookup.
//   3) Match helpers used by the metadata builder.
//
package librarymetadata

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"javflow/internal/contracts/crawlartifact"
)

// CrawlerRecord holds the minimum metadata we can recover from crawler
// artifacts for a single code. It is intentionally smaller than the full
// filmData shape so librarymetadata does not depend on crawler runtime types.
type CrawlerRecord struct {
	Code       string   `json:"code"`
	Title      string   `json:"title,omitempty"`
	Actors     []string `json:"actors,omitempty"`
	Genres     []string `json:"genres,omitempty"`
	CoverURL   string   `json:"coverUrl,omitempty"`
	SourceLink string   `json:"sourceLink,omitempty"`
	Magnet     string   `json:"magnet,omitempty"`
	OutputDir  string   `json:"outputDir,omitempty"`
	Source     string   `json:"source,omitempty"`
}

// CrawlSource is a read-only lookup table built from one or more crawl
// artifacts. It is safe to use from multiple goroutines after construction.
type CrawlSource struct {
	records   map[string]CrawlerRecord
	outputDir string
}

// Lookup returns a crawler record by normalized code.
func (s *CrawlSource) Lookup(code string) (CrawlerRecord, bool) {
	if s == nil || s.records == nil {
		return CrawlerRecord{}, false
	}
	key := strings.ToUpper(strings.TrimSpace(code))
	record, ok := s.records[key]
	return record, ok
}

// Records returns all loaded records.
func (s *CrawlSource) Records() []CrawlerRecord {
	if s == nil || s.records == nil {
		return nil
	}
	result := make([]CrawlerRecord, 0, len(s.records))
	for _, record := range s.records {
		result = append(result, record)
	}
	return result
}

// BuildCrawlSource loads the selected output's hidden/visible artifacts. Other
// historical snapshots are merged only when no explicit output was selected;
// a file picker selection must remain a strict, bounded source for the list.
func BuildCrawlSource(outputDir, userDataDir string) (*CrawlSource, error) {
	normalizedOutput := normalizeCrawlOutputDir(outputDir)
	normalizedUserData := strings.TrimSpace(userDataDir)

	source := &CrawlSource{
		records:   map[string]CrawlerRecord{},
		outputDir: normalizedOutput,
	}

	// 1) Prefer the complete hidden snapshot, then the hidden code artifact.
	if normalizedOutput != "" {
		internalPaths := crawlartifact.ResolveInternalArtifactPaths(normalizedUserData, normalizedOutput)
		switch {
		case crawlArtifactFileExists(internalPaths.FilmDataPath):
			if err := loadFilmDataRecordsWithUserData(source, normalizedOutput, normalizedUserData); err != nil {
				return source, fmt.Errorf("读取隐藏 filmData.json 失败：%w", err)
			}
		case crawlArtifactFileExists(internalPaths.OrganizerCodesPath):
			if err := loadOrganizerCodesArtifactAt(source, internalPaths.OrganizerCodesPath, normalizedOutput); err != nil {
				return source, fmt.Errorf("读取隐藏 organizer-codes.json 失败：%w", err)
			}
		default:
			// 2) Hidden artifacts are absent: fall back to user-visible output.
			visiblePaths := crawlartifact.ResolveCrawlOutputPaths(normalizedOutput)
			if crawlArtifactFileExists(visiblePaths.FilmDataPath) {
				if err := loadFilmDataRecords(source, normalizedOutput); err != nil {
					return source, fmt.Errorf("读取 filmData.json 失败：%w", err)
				}
			} else {
				if err := loadOrganizerCodesArtifact(source, normalizedOutput, ""); err != nil && !os.IsNotExist(err) {
					return source, fmt.Errorf("读取 organizer-codes.json 失败：%w", err)
				}
			}
		}
	}

	// 3) With no explicit source, use all historical snapshots as a fallback.
	// Never merge them into an explicitly selected JSON/directory: that would
	// make an 800-code selection expand into the complete application history.
	if normalizedOutput == "" && normalizedUserData != "" {
		if err := loadFromCacheSnapshots(source, normalizedUserData); err != nil && !os.IsNotExist(err) {
			return source, fmt.Errorf("读取历史抓取快照失败：%w", err)
		}
	}

	if len(source.records) == 0 {
		return source, fmt.Errorf("未找到任何爬虫产物")
	}
	return source, nil
}

func loadFilmDataRecords(source *CrawlSource, outputDir string) error {
	_, records, err := crawlartifact.ReadFilmDataRecords(outputDir)
	return mergeFilmDataRecords(source, records, outputDir, err)
}

func loadFilmDataRecordsWithUserData(source *CrawlSource, outputDir, userDataDir string) error {
	_, records, err := crawlartifact.ReadFilmDataRecordsWithUserData(outputDir, userDataDir)
	return mergeFilmDataRecords(source, records, outputDir, err)
}

func mergeFilmDataRecords(source *CrawlSource, records []map[string]any, outputDir string, err error) error {
	if err != nil {
		return err
	}

	for _, raw := range records {
		record := filmDataRecordToCrawlerRecord(raw, outputDir)
		if record.Code == "" {
			continue
		}
		mergeCrawlerRecord(source, record)
	}
	return nil
}

func crawlArtifactFileExists(path string) bool {
	info, err := os.Stat(strings.TrimSpace(path))
	return err == nil && info.Mode().IsRegular()
}

func loadOrganizerCodesArtifact(source *CrawlSource, outputDir, userDataDir string) error {
	var artifact crawlartifact.OrganizerCodesArtifact
	var err error
	if userDataDir != "" {
		_, artifact, err = crawlartifact.ReadOrganizerCodesArtifactWithUserData(outputDir, userDataDir)
	} else {
		_, artifact, err = crawlartifact.ReadOrganizerCodesArtifact(outputDir)
	}
	if err != nil {
		return err
	}

	for _, entry := range artifact.CodeEntries {
		code := strings.ToUpper(strings.TrimSpace(entry.Code))
		if code == "" {
			continue
		}
		record := CrawlerRecord{
			Code:      code,
			Title:     strings.TrimSpace(entry.Title),
			OutputDir: outputDir,
			Source:    "organizer-codes",
		}
		if len(entry.Magnets) > 0 {
			record.Magnet = entry.Magnets[0].Link
		}
		mergeCrawlerRecord(source, record)
	}
	return nil
}

func loadOrganizerCodesArtifactAt(source *CrawlSource, path, outputDir string) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var artifact crawlartifact.OrganizerCodesArtifact
	if err := json.Unmarshal(payload, &artifact); err != nil {
		return err
	}
	for _, entry := range artifact.CodeEntries {
		code := strings.ToUpper(strings.TrimSpace(entry.Code))
		if code == "" {
			continue
		}
		record := CrawlerRecord{
			Code:      code,
			Title:     strings.TrimSpace(entry.Title),
			OutputDir: outputDir,
			Source:    "organizer-codes-hidden",
		}
		if len(entry.Magnets) > 0 {
			record.Magnet = entry.Magnets[0].Link
		}
		mergeCrawlerRecord(source, record)
	}
	return nil
}

func loadFromCacheSnapshots(source *CrawlSource, userDataDir string) error {
	snapshots, err := crawlartifact.ListCacheSnapshots(userDataDir)
	if err != nil {
		return err
	}
	for _, snapshot := range snapshots {
		outputDir := strings.TrimSpace(snapshot.OutputDir)
		if outputDir == "" {
			continue
		}
		// Avoid re-loading the same output directory twice.
		if outputDir == source.outputDir {
			continue
		}
		internalPaths := crawlartifact.ResolveInternalArtifactPaths(userDataDir, outputDir)
		if crawlArtifactFileExists(internalPaths.FilmDataPath) {
			_ = loadFilmDataRecordsWithUserData(source, outputDir, userDataDir)
		} else if crawlArtifactFileExists(internalPaths.OrganizerCodesPath) {
			_ = loadOrganizerCodesArtifact(source, outputDir, userDataDir)
		} else {
			_ = loadFilmDataRecords(source, outputDir)
			_ = loadOrganizerCodesArtifact(source, outputDir, "")
		}
	}
	return nil
}

func filmDataRecordToCrawlerRecord(raw map[string]any, outputDir string) CrawlerRecord {
	record := CrawlerRecord{
		OutputDir: outputDir,
		Source:    "filmData",
	}

	if title, ok := raw["title"].(string); ok {
		record.Title = strings.TrimSpace(title)
		record.Code = extractCodeFromTitle(record.Title)
	}
	if sourceLink, ok := raw["sourceLink"].(string); ok {
		record.SourceLink = strings.TrimSpace(sourceLink)
		if record.Code == "" {
			record.Code = extractCodeFromURL(record.SourceLink)
		}
	}
	if coverImage, ok := raw["coverImage"].(string); ok {
		record.CoverURL = strings.TrimSpace(coverImage)
	}
	if category, ok := raw["category"].([]any); ok {
		for _, item := range category {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				record.Genres = append(record.Genres, strings.TrimSpace(s))
			}
		}
	}
	if actresses, ok := raw["actress"].([]any); ok {
		for _, item := range actresses {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				record.Actors = append(record.Actors, strings.TrimSpace(s))
			}
		}
	}
	if magnet, ok := raw["magnet"].(string); ok && strings.TrimSpace(magnet) != "" {
		record.Magnet = strings.TrimSpace(magnet)
	} else if magnetLinks, ok := raw["magnetLinks"].([]any); ok && len(magnetLinks) > 0 {
		if first, ok := magnetLinks[0].(map[string]any); ok {
			if link, ok := first["link"].(string); ok {
				record.Magnet = strings.TrimSpace(link)
			}
		}
	}

	return record
}

func mergeCrawlerRecord(source *CrawlSource, incoming CrawlerRecord) {
	code := strings.ToUpper(strings.TrimSpace(incoming.Code))
	if code == "" {
		return
	}
	incoming.Code = code

	existing, exists := source.records[code]
	if !exists {
		source.records[code] = incoming
		return
	}

	if existing.Title == "" && incoming.Title != "" {
		existing.Title = incoming.Title
	}
	if existing.CoverURL == "" && incoming.CoverURL != "" {
		existing.CoverURL = incoming.CoverURL
	}
	if existing.SourceLink == "" && incoming.SourceLink != "" {
		existing.SourceLink = incoming.SourceLink
	}
	if existing.Magnet == "" && incoming.Magnet != "" {
		existing.Magnet = incoming.Magnet
	}
	if len(existing.Actors) == 0 && len(incoming.Actors) > 0 {
		existing.Actors = incoming.Actors
	}
	if len(existing.Genres) == 0 && len(incoming.Genres) > 0 {
		existing.Genres = incoming.Genres
	}
	if existing.OutputDir == "" && incoming.OutputDir != "" {
		existing.OutputDir = incoming.OutputDir
	}
	if incoming.Source != "" {
		if existing.Source == "" {
			existing.Source = incoming.Source
		} else if !strings.Contains(existing.Source, incoming.Source) {
			existing.Source = existing.Source + "+" + incoming.Source
		}
	}
	source.records[code] = existing
}

var titleCodePrefixPattern = regexp.MustCompile(`^([A-Z]{2,8}-?\d{2,8})\s*[\-_:：]\s*`)

func extractCodeFromTitle(title string) string {
	upper := strings.ToUpper(strings.TrimSpace(title))
	if match := titleCodePrefixPattern.FindStringSubmatch(upper); match != nil {
		return match[1]
	}
	return ""
}

func extractCodeFromURL(url string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(url), "/")
	idx := strings.LastIndex(trimmed, "/")
	if idx < 0 || idx >= len(trimmed)-1 {
		return ""
	}
	candidate := trimmed[idx+1:]
	// Accept only candidate that looks like a code.
	if matched, _ := regexp.MatchString(`^[A-Z]{2,8}-?\d{2,8}$`, strings.ToUpper(candidate)); matched {
		return strings.ToUpper(candidate)
	}
	return ""
}

// CountCrawlArtifacts returns the number of distinct film codes available in
// the selected crawl output directory. It only reads the visible artifacts
// (filmData.json and organizer-codes.json) for the given directory, without
// falling back to historical snapshots, so the directory-picker feedback stays
// fast.
func CountCrawlArtifacts(outputDir string) int {
	normalizedOutput := normalizeCrawlOutputDir(outputDir)
	if normalizedOutput == "" {
		return 0
	}

	source := &CrawlSource{
		records:   map[string]CrawlerRecord{},
		outputDir: normalizedOutput,
	}

	_ = loadFilmDataRecords(source, normalizedOutput)
	_ = loadOrganizerCodesArtifact(source, normalizedOutput, "")

	return len(source.records)
}

// normalizeCrawlOutputDir converts an artifact file path (e.g. a selected
// organizer-codes.json or filmData.json) into its parent directory. This makes
// the "crawl output directory" field forgiving when users paste or pick a file
// instead of the containing folder.
func normalizeCrawlOutputDir(outputDir string) string {
	normalized := strings.TrimSpace(outputDir)
	if normalized == "" {
		return ""
	}

	info, err := os.Stat(normalized)
	if err == nil && info.Mode().IsRegular() {
		return filepath.Dir(normalized)
	}

	// If the path points to a likely artifact filename that does not exist,
	// still treat it as a file selection and resolve to the parent directory.
	base := strings.ToLower(filepath.Base(normalized))
	if err != nil && (base == "filmdata.json" ||
		base == "organizer-codes.json" || base == "crawl-profile.json" ||
		base == "magnet-links.txt") {
		return filepath.Dir(normalized)
	}

	return normalized
}
