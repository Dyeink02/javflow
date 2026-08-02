// Ownership summary:
//
//	This file matches local videos against crawler artifact records for metadata fallback.
//
// File map for maintainers:
//  1. CrawlerRecord and CrawlSource types.
//  2. Artifact loading and code-keyed lookup.
//  3. Match helpers used by the metadata builder.
package librarymetadata

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

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
	// Aliases contains noisy code tokens seen in magnet names. They are used
	// only to map a local filename back to this canonical crawler record.
	Aliases []string `json:"-"`
}

// CrawlSource is a read-only lookup table built from one or more crawl
// artifacts. It is safe to use from multiple goroutines after construction.
type CrawlSource struct {
	records       map[string]CrawlerRecord
	aliases       map[string]string
	ambiguous     map[string]struct{}
	outputDir     string
	expectedActor string
}

// Lookup returns a crawler record by normalized code.
func (s *CrawlSource) Lookup(code string) (CrawlerRecord, bool) {
	if s == nil || s.records == nil {
		return CrawlerRecord{}, false
	}
	key := strings.ToUpper(strings.TrimSpace(code))
	record, ok := s.records[key]
	if ok {
		return cloneCrawlerRecord(record), true
	}
	if canonical, exists := s.aliases[key]; exists {
		record, ok = s.records[canonical]
		if ok {
			return cloneCrawlerRecord(record), true
		}
		return CrawlerRecord{}, false
	}
	// Older snapshots may not persist magnet links, so also accept a unique
	// numeric spelling such as GOMK-051 for canonical GOMK-51. Ambiguous
	// candidates are rejected instead of guessing between records.
	var matched CrawlerRecord
	matchCount := 0
	for canonical, candidate := range s.records {
		if !likelyCrawlerAlias(key, canonical) {
			continue
		}
		matched = candidate
		matchCount++
		if matchCount > 1 {
			return CrawlerRecord{}, false
		}
	}
	if matchCount == 1 {
		return cloneCrawlerRecord(matched), true
	}
	return CrawlerRecord{}, false
}

// Records returns all loaded records.
func (s *CrawlSource) Records() []CrawlerRecord {
	if s == nil || s.records == nil {
		return nil
	}
	result := make([]CrawlerRecord, 0, len(s.records))
	for _, record := range s.records {
		result = append(result, cloneCrawlerRecord(record))
	}
	// Map iteration order is deliberately random in Go. Keep the read model
	// deterministic so missing-item lists, diagnostics, and tests do not change
	// order between runs when the underlying snapshot is unchanged.
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].Code) < strings.ToLower(result[j].Code)
	})
	return result
}

// cloneCrawlerRecord protects the source's internal maps and slices from
// callers that only need a read model. Returning a shallow struct copy would
// still let a caller mutate Actors, Genres, or Aliases and affect later
// lookups, which would violate CrawlSource's read-only contract.
func cloneCrawlerRecord(record CrawlerRecord) CrawlerRecord {
	clone := record
	clone.Actors = append([]string(nil), record.Actors...)
	clone.Genres = append([]string(nil), record.Genres...)
	clone.Aliases = append([]string(nil), record.Aliases...)
	return clone
}

// BuildCrawlSource loads the selected output's hidden/visible artifacts. Other
// historical snapshots are merged only when no explicit output was selected;
// a file picker selection must remain a strict, bounded source for the list.
func BuildCrawlSource(outputDir, userDataDir string) (*CrawlSource, error) {
	normalizedOutput := normalizeCrawlOutputDir(outputDir)
	normalizedUserData := strings.TrimSpace(userDataDir)

	source := &CrawlSource{
		records:   map[string]CrawlerRecord{},
		aliases:   map[string]string{},
		ambiguous: map[string]struct{}{},
		outputDir: normalizedOutput,
	}
	source.expectedActor = expectedActorForCrawlSource(normalizedOutput, normalizedUserData)

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

	// Apply actor ownership only after all records have been loaded. This keeps
	// a selected snapshot bounded to its actress while retaining records whose
	// legacy payload did not persist actor names at all.
	source.filterByExpectedActor()
	source.rebuildAliases()

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
	for _, field := range []string{"magnetLinks", "backupMagnetLinks"} {
		if links, ok := raw[field].([]any); ok {
			for _, item := range links {
				entry, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if link, ok := entry["link"].(string); ok {
					record.Aliases = append(record.Aliases, extractMagnetCodeCandidates(link)...)
				}
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
	if len(incoming.Aliases) > 0 {
		existing.Aliases = appendUniqueStrings(existing.Aliases, incoming.Aliases...)
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

var (
	// Crawler titles commonly put a plain space before the title, e.g.
	// "DV-803 【AIリマスター版】...". The old pattern required punctuation
	// and silently dropped those otherwise valid records.
	titleCodePrefixPattern = regexp.MustCompile(`(?i)^([A-Z]{2,8})[-_]?([0-9]{1,8})(?:\s+|[-_:：]|$)`)
	urlCodePrefixPattern   = regexp.MustCompile(`(?i)^([A-Z]{2,8})[-_]?([0-9]{1,8})(?:[_-][0-9]{4}(?:[-_][0-9]{2}){0,2})?$`)
	textCodePattern        = regexp.MustCompile(`(?i)([A-Z]{2,12}[-_]?\d{1,8})`)
	compactCodePattern     = regexp.MustCompile(`(?i)^([A-Z]{2,12})(\d{1,8})$`)
	separatedCodePattern   = regexp.MustCompile(`(?i)^([A-Z]{2,12})[-_](\d{1,8})$`)
)

func extractCodeFromTitle(title string) string {
	upper := strings.ToUpper(strings.TrimSpace(title))
	if match := titleCodePrefixPattern.FindStringSubmatch(upper); match != nil {
		return normalizeCode(match[1] + "-" + match[2])
	}
	return ""
}

func extractCodeFromURL(rawURL string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	idx := strings.LastIndex(trimmed, "/")
	if idx < 0 || idx >= len(trimmed)-1 {
		return ""
	}
	candidate := trimmed[idx+1:]
	candidate, _ = url.PathUnescape(candidate)
	if match := urlCodePrefixPattern.FindStringSubmatch(strings.ToUpper(candidate)); match != nil {
		return normalizeCode(match[1] + "-" + match[2])
	}
	return ""
}

// expectedActorForCrawlSource obtains the ownership marker from the persisted
// profile first, then from the organizer snapshot, and finally from an output
// directory whose basename is itself present in the record actor list.
func expectedActorForCrawlSource(outputDir, userDataDir string) string {
	if strings.TrimSpace(outputDir) == "" {
		return ""
	}
	if strings.TrimSpace(userDataDir) != "" {
		if _, profile, err := crawlartifact.ReadCrawlProfileArtifactWithUserData(outputDir, userDataDir); err == nil {
			if name := strings.TrimSpace(profile.ActressName); name != "" {
				return name
			}
		}
		if _, artifact, err := crawlartifact.ReadOrganizerCodesArtifactWithUserData(outputDir, userDataDir); err == nil {
			if name := strings.TrimSpace(artifact.ActressName); name != "" {
				return name
			}
		}
	}
	return strings.TrimSpace(filepath.Base(outputDir))
}

func (s *CrawlSource) filterByExpectedActor() {
	if s == nil || strings.TrimSpace(s.expectedActor) == "" {
		return
	}
	target := normalizeActorName(s.expectedActor)
	if target == "" {
		return
	}
	// A temporary directory used by tests or by a user-selected arbitrary path
	// may have a basename that is not an actress name. Only enforce the filter
	// when the candidate is actually present in at least one record.
	seenTarget := false
	for _, record := range s.records {
		if crawlerRecordHasActor(record, target) {
			seenTarget = true
			break
		}
	}
	if !seenTarget {
		return
	}
	for code, record := range s.records {
		if len(record.Actors) == 0 || crawlerRecordHasActor(record, target) {
			continue
		}
		delete(s.records, code)
	}
}

func crawlerRecordHasActor(record CrawlerRecord, target string) bool {
	for _, actor := range record.Actors {
		if normalizeActorName(actor) == target {
			return true
		}
	}
	return false
}

func normalizeActorName(value string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsSpace(r) || strings.ContainsRune("・·,，、()（）[]【】-_", r) {
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func (s *CrawlSource) rebuildAliases() {
	if s == nil {
		return
	}
	s.aliases = map[string]string{}
	s.ambiguous = map[string]struct{}{}
	for canonical, record := range s.records {
		for _, alias := range record.Aliases {
			alias = strings.ToUpper(strings.TrimSpace(alias))
			if alias == "" || alias == canonical || !likelyCrawlerAlias(alias, canonical) {
				continue
			}
			if _, blocked := s.ambiguous[alias]; blocked {
				continue
			}
			if existing, exists := s.aliases[alias]; exists && existing != canonical {
				delete(s.aliases, alias)
				s.ambiguous[alias] = struct{}{}
				continue
			}
			s.aliases[alias] = canonical
		}
	}
}

func extractMagnetCodeCandidates(rawLink string) []string {
	value := strings.TrimSpace(rawLink)
	if value == "" {
		return nil
	}
	if parsed, err := url.Parse(value); err == nil {
		if dn := strings.TrimSpace(parsed.Query().Get("dn")); dn != "" {
			value = dn
		}
	}
	matches := textCodePattern.FindAllStringIndex(strings.ToUpper(value), -1)
	result := make([]string, 0, len(matches))
	for _, span := range matches {
		start, end := span[0], span[1]
		if start > 0 && isCodeWordRune(value[start-1]) {
			continue
		}
		if end < len(value) && isCodeWordRune(value[end]) {
			continue
		}
		candidate := normalizeMagnetCode(value[start:end])
		if candidate != "" {
			result = appendUniqueStrings(result, candidate)
		}
	}
	return result
}

func isCodeWordRune(value byte) bool {
	return (value >= 'A' && value <= 'Z') || (value >= 'a' && value <= 'z') || (value >= '0' && value <= '9')
}

func normalizeMagnetCode(value string) string {
	upper := strings.ToUpper(strings.TrimSpace(value))
	if match := compactCodePattern.FindStringSubmatch(strings.ReplaceAll(strings.ReplaceAll(upper, "-", ""), "_", "")); match != nil {
		return normalizeCode(match[1] + "-" + match[2])
	}
	parts := separatedCodePattern.FindStringSubmatch(upper)
	if len(parts) == 3 {
		return normalizeCode(parts[1] + "-" + parts[2])
	}
	return ""
}

func likelyCrawlerAlias(alias, canonical string) bool {
	aliasPrefix, aliasNumber, ok := splitCrawlerCode(alias)
	if !ok {
		return false
	}
	canonicalPrefix, canonicalNumber, ok := splitCrawlerCode(canonical)
	if !ok || aliasPrefix != canonicalPrefix {
		return false
	}
	aliasNumber = strings.TrimLeft(aliasNumber, "0")
	canonicalNumber = strings.TrimLeft(canonicalNumber, "0")
	if aliasNumber == "" {
		aliasNumber = "0"
	}
	if canonicalNumber == "" {
		canonicalNumber = "0"
	}
	if aliasNumber == canonicalNumber {
		return true
	}
	// Some magnet providers append one or two digits to the canonical code
	// (MXGS-118 -> MXGS-1183). Accept this only in the same studio namespace.
	return len(aliasNumber) > len(canonicalNumber) &&
		len(aliasNumber)-len(canonicalNumber) <= 2 &&
		strings.HasPrefix(aliasNumber, canonicalNumber)
}

func splitCrawlerCode(code string) (string, string, bool) {
	compact := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(code), "-", ""), "_", ""))
	match := compactCodePattern.FindStringSubmatch(compact)
	if len(match) != 3 {
		return "", "", false
	}
	return match[1], match[2], true
}

func appendUniqueStrings(values []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(values)+len(additions))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	for _, value := range additions {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
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
