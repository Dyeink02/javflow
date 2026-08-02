package avsubscription

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"javflow/internal/contracts/crawlartifact"
	"javflow/internal/crawlidentity"
)

// scan_output.go owns crawl-output artifact import for AV subscriptions.
//
// Ownership summary:
// 1) read crawl-profile.json / filmData.json import candidates
// 2) infer the primary actress subscription candidate from persisted artifacts
// 3) merge artifact-derived candidates into stored subscription state
//
// File map for maintainers:
// 1) artifact-import candidate/apply DTOs
// 2) filmData/profile scan and precedence helpers
// 3) primary-actress detection helpers
// 4) merge/apply path into stored subscription state
//
// Boundary reminder:
// this file only turns persisted crawl outputs into subscription state inputs.
// Remote refresh and target fetching belong to the separate subscription fetch
// path, so import bugs and refresh bugs can be isolated quickly.

var filmCodePattern = regexp.MustCompile(`([A-Z]{2,12})-?(\d{1,8}[A-Z]*)`)

// scanImportCandidate is the normalized handoff from one artifact-reader path
// into the shared subscription-merge logic. Profile-driven and filmData-driven
// imports differ in how they discover actress metadata, but they should update
// stored subscriptions through the same code path.
type scanImportCandidate struct {
	ActressName   string
	CrawlURL      string
	PreferredBase string
	Count         int
	ItemsPerPage  int
	TotalPages    int
	BaselineCodes []string
}

type scanImportApplyResult struct {
	AddedCount    int
	UpdatedCount  int
	Subscriptions []Subscription
}

func (s *Service) scanOutputLocked(cleanOutputDir string) (ScanResult, error) {
	if cleanOutputDir == "" {
		return ScanResult{}, fmt.Errorf("\u8bf7\u5148\u9009\u62e9\u6293\u53d6\u7ed3\u679c\u76ee\u5f55\u3002")
	}

	artifactRoot := s.resolveArtifactRoot(cleanOutputDir)

	// Prefer the compact crawl-profile artifact when available because it is the
	// least ambiguous import source. Fall back to filmData.json only when profile
	// metadata is absent or incomplete.
	if scanned, err := s.scanOutputFromProfileLocked(artifactRoot); err == nil {
		return scanned, nil
	}

	paths, records, err := crawlartifact.ReadFilmDataRecordsWithUserData(cleanOutputDir, s.paths.UserData)
	if err != nil {
		return ScanResult{}, err
	}
	if len(records) == 0 {
		return ScanResult{}, fmt.Errorf("filmData.json \u4e2d\u6ca1\u6709\u53ef\u8bc6\u522b\u7684\u5f71\u7247\u8bb0\u5f55\u3002")
	}

	actressFilmSet, actressNames, actressOrder := buildActressFilmSet(records)
	primaryActressKey := detectPrimaryActressKey(actressFilmSet, actressNames, actressOrder, cleanOutputDir)
	if primaryActressKey == "" {
		return ScanResult{}, fmt.Errorf("filmData.json \u4e2d\u672a\u80fd\u8bc6\u522b\u5230\u4e3b\u5973\u4f18\u3002")
	}

	actressName := actressNames[primaryActressKey]
	if actressName == "" {
		return ScanResult{}, fmt.Errorf("filmData.json \u4e2d\u672a\u80fd\u8bc6\u522b\u5230\u4e3b\u5973\u4f18\u540d\u79f0\u3002")
	}

	recoveredURL, recoveredBase := s.recoverSubscriptionTargetMetadata(cleanOutputDir)

	applied, err := s.applyScanImportCandidateLocked(scanImportCandidate{
		ActressName:   actressName,
		CrawlURL:      recoveredURL,
		PreferredBase: recoveredBase,
		Count:         len(actressFilmSet[primaryActressKey]),
		BaselineCodes: sortedFilmSetKeys(actressFilmSet[primaryActressKey]),
	})
	if err != nil {
		return ScanResult{}, err
	}

	scannedNames := []string{actressName}

	sort.Strings(scannedNames)
	return ScanResult{
		OutputDir:          paths.OutputDir,
		FilmDataPath:       paths.FilmDataPath,
		CrawlProfilePath:   paths.CrawlProfilePath,
		SourceType:         scanSourceFilmData,
		AddedCount:         applied.AddedCount,
		UpdatedCount:       applied.UpdatedCount,
		SubscriptionCount:  len(applied.Subscriptions),
		ScannedActresses:   len(scannedNames),
		Subscriptions:      applied.Subscriptions,
		ScannedActressList: scannedNames,
		FilteredCodes:      s.loadFilteredCodesFromOutput(cleanOutputDir),
	}, nil
}

func (s *Service) resolveArtifactRoot(outputDir string) string {
	normalizedOutputDir := strings.TrimSpace(outputDir)
	if normalizedOutputDir == "" {
		return ""
	}
	if strings.TrimSpace(s.paths.UserData) == "" {
		return normalizedOutputDir
	}

	internalPaths := crawlartifact.ResolveInternalArtifactPaths(s.paths.UserData, normalizedOutputDir)
	if strings.TrimSpace(internalPaths.CrawlProfilePath) != "" {
		if _, err := os.Stat(internalPaths.CrawlProfilePath); err == nil {
			return normalizedOutputDir
		}
	}
	return normalizedOutputDir
}

// scanOutputFromProfileLocked is the preferred artifact import path because the
// profile already records actress identity, crawl URL, and counts explicitly.
// It should stay small and explicit so profile-vs-filmData precedence is easy
// to audit during future AV subscription decoupling work.
func (s *Service) scanOutputFromProfileLocked(cleanOutputDir string) (ScanResult, error) {
	paths, profile, err := crawlartifact.ReadCrawlProfileArtifactWithUserData(cleanOutputDir, s.paths.UserData)
	if err != nil {
		return ScanResult{}, err
	}

	actressName := strings.TrimSpace(profile.ActressName)
	if actressName == "" {
		return ScanResult{}, fmt.Errorf("crawl-profile.json 中未识别到主女优名称。")
	}

	count := profile.CompletedCount
	if count <= 0 {
		count = profile.TargetCount
	}
	if count <= 0 {
		return ScanResult{}, fmt.Errorf("crawl-profile.json 中缺少有效影片数量。")
	}

	crawlURL := strings.TrimSpace(profile.CrawlURL)
	preferredBase := strings.TrimSpace(profile.SiteBase)
	if crawlURL == "" || preferredBase == "" {
		recoveredURL, recoveredBase := s.recoverSubscriptionTargetMetadata(cleanOutputDir)
		if crawlURL == "" {
			crawlURL = recoveredURL
		}
		if preferredBase == "" {
			preferredBase = recoveredBase
		}
	}

	scannedNames := []string{actressName}
	applied, err := s.applyScanImportCandidateLocked(scanImportCandidate{
		ActressName:   actressName,
		CrawlURL:      crawlURL,
		PreferredBase: preferredBase,
		Count:         count,
		ItemsPerPage:  profile.ItemsPerPage,
		TotalPages:    profile.TotalPages,
		BaselineCodes: extractBaselineCodesFromOutput(cleanOutputDir, s.paths.UserData),
	})
	if err != nil {
		return ScanResult{}, err
	}

	filteredCodes := make([]crawlartifact.FilteredFilmCodeEntry, 0, len(profile.FilteredCodes))
	for _, entry := range profile.FilteredCodes {
		filteredCodes = append(filteredCodes, crawlartifact.FilteredFilmCodeEntry{
			Code:   strings.TrimSpace(entry.Code),
			Reason: strings.TrimSpace(entry.Reason),
			Remark: strings.TrimSpace(entry.Remark),
		})
	}

	return ScanResult{
		OutputDir:          paths.OutputDir,
		FilmDataPath:       firstNonEmpty(profile.FilmDataPath, paths.FilmDataPath),
		CrawlProfilePath:   paths.CrawlProfilePath,
		SourceType:         scanSourceCrawlProfile,
		AddedCount:         applied.AddedCount,
		UpdatedCount:       applied.UpdatedCount,
		SubscriptionCount:  len(applied.Subscriptions),
		ScannedActresses:   len(scannedNames),
		Subscriptions:      applied.Subscriptions,
		ScannedActressList: scannedNames,
		FilteredCodes:      filteredCodes,
	}, nil
}

// applyScanImportCandidateLocked owns the shared "artifact candidate ->
// persisted subscription state" rules for scan imports. Both profile-driven
// and filmData-driven imports should reuse this function so metadata merge
// behavior stays identical across artifact source types.
func (s *Service) applyScanImportCandidateLocked(candidate scanImportCandidate) (scanImportApplyResult, error) {
	// This merge point is intentionally source-agnostic. Once profile/filmData
	// import paths produce a candidate, persisted subscription updates should
	// follow one shared rule set.
	items, err := s.loadLocked()
	if err != nil {
		return scanImportApplyResult{}, err
	}

	now := time.Now().Format(time.RFC3339)
	next := normalizeSubscription(Subscription{
		ActressName:   strings.TrimSpace(candidate.ActressName),
		CrawlURL:      strings.TrimSpace(candidate.CrawlURL),
		PreferredBase: strings.TrimSpace(candidate.PreferredBase),
		BaselineCodes: normalizeBaselineCodes(candidate.BaselineCodes),
		Source:        sourceScan,
		SyncedCount:   candidate.Count,
		CurrentCount:  candidate.Count,
		ItemsPerPage:  maxInt(defaultItemsPerPage, candidate.ItemsPerPage),
		TotalPages:    calcPages(candidate.Count, maxInt(defaultItemsPerPage, candidate.ItemsPerPage)),
		LastCheckedAt: now,
		LastSyncedAt:  now,
		LastUpdatedAt: now,
	}, now)

	result := scanImportApplyResult{}
	matchIndex := findSubscriptionIndex(items, next)
	if matchIndex >= 0 {
		current := items[matchIndex]
		if next.CrawlURL == "" {
			next.CrawlURL = current.CrawlURL
		}
		if next.PreferredBase == "" {
			next.PreferredBase = current.PreferredBase
		}
		if current.ItemsPerPage > 0 {
			next.ItemsPerPage = current.ItemsPerPage
			next.TotalPages = calcPages(candidate.Count, current.ItemsPerPage)
		}
		if len(next.BaselineCodes) == 0 && len(current.BaselineCodes) > 0 {
			next.BaselineCodes = normalizeBaselineCodes(current.BaselineCodes)
		}
		items[matchIndex] = normalizeSubscription(next, now)
		result.UpdatedCount = 1
	} else {
		items = append(items, next)
		result.AddedCount = 1
	}

	sortSubscriptionsByPriority(items)
	if err := s.saveLocked(items); err != nil {
		return scanImportApplyResult{}, err
	}

	result.Subscriptions = items
	return result, nil
}

// buildActressFilmSet transforms filmData records into a deduplicated
// actress-to-film map plus stable name/order metadata used for primary-target
// selection.
func buildActressFilmSet(records []map[string]any) (map[string]map[string]struct{}, map[string]string, map[string]int) {
	actressFilmSet := map[string]map[string]struct{}{}
	actressNames := map[string]string{}
	actressOrder := map[string]int{}
	nextOrder := 0

	for index, record := range records {
		identity := extractRecordIdentity(record, index)
		for _, actressName := range extractActressNames(record) {
			key := normalizeName(actressName)
			if key == "" {
				continue
			}
			if _, ok := actressFilmSet[key]; !ok {
				actressFilmSet[key] = map[string]struct{}{}
			}
			actressFilmSet[key][identity] = struct{}{}
			if actressNames[key] == "" {
				actressNames[key] = strings.TrimSpace(actressName)
			}
			if _, exists := actressOrder[key]; !exists {
				actressOrder[key] = nextOrder
				nextOrder++
			}
		}
	}

	return actressFilmSet, actressNames, actressOrder
}

// detectPrimaryActressKey applies a deterministic tie-break order:
// 1) folder-name hints near the crawl output directory
// 2) highest distinct film count
// 3) earliest appearance order
// 4) stable lexical actress-name order
func detectPrimaryActressKey(
	actressFilmSet map[string]map[string]struct{},
	actressNames map[string]string,
	actressOrder map[string]int,
	outputDir string,
) string {
	if len(actressFilmSet) == 0 {
		return ""
	}

	for _, hint := range outputDirHints(outputDir) {
		hintKey := normalizeSubscriptionFolderHint(hint)
		if hintKey == "" {
			continue
		}
		if _, exists := actressFilmSet[hintKey]; exists {
			return hintKey
		}
	}

	bestKey := ""
	bestCount := -1
	bestOrder := int(^uint(0) >> 1)
	for actressKey, filmSet := range actressFilmSet {
		count := len(filmSet)
		order := actressOrder[actressKey]
		if count > bestCount {
			bestKey = actressKey
			bestCount = count
			bestOrder = order
			continue
		}
		if count == bestCount && order < bestOrder {
			bestKey = actressKey
			bestOrder = order
			continue
		}
		if count == bestCount && order == bestOrder && actressNames[actressKey] < actressNames[bestKey] {
			bestKey = actressKey
		}
	}
	return bestKey
}

// outputDirHints lets the artifact-import path reuse user-visible folder names
// as a weak signal when filmData contains multi-actress compilation noise.
func outputDirHints(outputDir string) []string {
	cleanPath := filepath.Clean(strings.TrimSpace(outputDir))
	if cleanPath == "" || cleanPath == "." {
		return nil
	}

	hints := []string{filepath.Base(cleanPath)}
	parent := filepath.Base(filepath.Dir(cleanPath))
	if parent != "" && parent != "." && parent != hints[0] {
		hints = append(hints, parent)
	}
	return hints
}

// normalizeSubscriptionFolderHint trims run-folder suffix noise such as
// counters or punctuation so actress folder names still match normalized
// actress keys when the output directory includes trailing numbers.
func normalizeSubscriptionFolderHint(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "run-") {
		return ""
	}

	runes := []rune(trimmed)
	end := len(runes)
	for end > 0 {
		last := runes[end-1]
		if unicode.IsDigit(last) || unicode.IsSpace(last) || strings.ContainsRune("-_()[]{}\uFF0C\u3002\uFF08\uFF09", last) {
			end--
			continue
		}
		break
	}

	normalized := strings.TrimSpace(string(runes[:end]))
	if normalized == "" || strings.HasPrefix(strings.ToLower(normalized), "run-") {
		return ""
	}
	return normalizeName(normalized)
}

// extractActressNames accepts the historical filmData shape variations and
// deduplicates names before primary-actress inference uses them.
func extractActressNames(record map[string]any) []string {
	output := make([]string, 0)
	seen := map[string]struct{}{}
	appendName := func(value string) {
		name := strings.TrimSpace(value)
		if name == "" {
			return
		}
		key := normalizeName(name)
		if key == "" {
			return
		}
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		output = append(output, name)
	}

	switch value := record["actress"].(type) {
	case []any:
		for _, item := range value {
			appendName(fmt.Sprint(item))
		}
	case []string:
		for _, item := range value {
			appendName(item)
		}
	case string:
		for _, item := range strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == '\u3001' || r == '/' || r == '\n' || r == '\r'
		}) {
			appendName(item)
		}
	}

	return output
}

// extractRecordIdentity collapses historical filmData variations into one
// dedupe key so actress counts remain stable across legacy exports.
func extractRecordIdentity(record map[string]any, index int) string {
	candidates := []string{
		recordFieldText(record["filmCode"]),
		recordFieldText(record["code"]),
		recordFieldText(record["sourceLink"]),
		recordFieldText(record["title"]),
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		upper := strings.ToUpper(candidate)
		if matches := filmCodePattern.FindStringSubmatch(upper); len(matches) == 3 {
			return crawlidentity.NormalizeFilmID(matches[1] + "-" + matches[2])
		}
		if strings.TrimSpace(candidate) != "" {
			return candidate
		}
	}

	return fmt.Sprintf("record-%d", index)
}

func recordFieldText(value any) string {
	if value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || strings.EqualFold(text, "<nil>") {
		return ""
	}
	return text
}

func normalizeFilmCode(value string) string {
	upper := strings.ToUpper(strings.TrimSpace(value))
	if upper == "" {
		return ""
	}
	if matches := filmCodePattern.FindStringSubmatch(upper); len(matches) == 3 {
		return crawlidentity.NormalizeFilmID(matches[1] + "-" + matches[2])
	}
	return ""
}

func sortedFilmSetKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return []string{}
	}
	items := make([]string, 0, len(values))
	for key := range values {
		normalized := normalizeFilmCode(key)
		if normalized == "" {
			normalized = strings.ToUpper(strings.TrimSpace(key))
		}
		if normalized != "" {
			items = append(items, normalized)
		}
	}
	sort.Strings(items)
	return normalizeBaselineCodes(items)
}

func extractBaselineCodesFromOutput(outputDir string, userDataDir string) []string {
	_, records, err := crawlartifact.ReadFilmDataRecordsWithUserData(outputDir, userDataDir)
	if err != nil || len(records) == 0 {
		return []string{}
	}

	codes := make([]string, 0, len(records))
	for index, record := range records {
		identity := extractRecordIdentity(record, index)
		normalized := normalizeFilmCode(identity)
		if normalized == "" {
			normalized = normalizeFilmCode(recordFieldText(record["title"]))
		}
		if normalized == "" {
			normalized = normalizeFilmCode(recordFieldText(record["sourceLink"]))
		}
		if normalized != "" {
			codes = append(codes, normalized)
		}
	}
	return normalizeBaselineCodes(codes)
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

// recoverSubscriptionTargetMetadata is the compatibility bridge for historical
// crawl outputs that only left filmData.json in the visible output folder.
//
// Lookup order is intentionally stable:
// 1) internal cache snapshot written by newer Go output paths
// 2) crawl-quality-summary.txt
// 3) logs/latest-log.txt
//
// This keeps AV subscription import usable even when a compat/sidecar crawl
// did not persist crawl-profile.json into the internal artifact cache.
func (s *Service) recoverSubscriptionTargetMetadata(outputDir string) (string, string) {
	crawlURL, preferredBase := s.recoverFromCacheSnapshot(outputDir)
	if crawlURL != "" || preferredBase != "" {
		return crawlURL, preferredBase
	}

	runPaths := crawlartifact.ResolveCrawlRunPaths(outputDir)
	crawlURL, preferredBase = recoverTargetMetadataFromTextFile(crawlartifact.DefaultQualitySummaryPath(runPaths.OutputDir))
	if crawlURL != "" || preferredBase != "" {
		return crawlURL, preferredBase
	}

	return recoverTargetMetadataFromTextFile(runPaths.LatestLogPath)
}

func (s *Service) recoverFromCacheSnapshot(outputDir string) (string, string) {
	if strings.TrimSpace(s.paths.UserData) == "" {
		return "", ""
	}

	items, err := crawlartifact.ListCacheSnapshots(s.paths.UserData)
	if err != nil || len(items) == 0 {
		return "", ""
	}

	normalizedOutputDir := crawlartifact.NormalizeRootPath(outputDir)
	for _, item := range items {
		if !strings.EqualFold(crawlartifact.NormalizeRootPath(item.OutputDir), normalizedOutputDir) {
			continue
		}
		crawlURL := strings.TrimSpace(item.CrawlURL)
		preferredBase := strings.TrimSpace(item.SiteBase)
		if preferredBase == "" {
			preferredBase = inferBaseFromURL(crawlURL)
		}
		return crawlURL, preferredBase
	}
	return "", ""
}

func recoverTargetMetadataFromTextFile(filePath string) (string, string) {
	trimmedPath := strings.TrimSpace(filePath)
	if trimmedPath == "" {
		return "", ""
	}

	file, err := os.Open(trimmedPath)
	if err != nil {
		return "", ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		crawlURL := extractSourceURLFromLine(line)
		if crawlURL == "" {
			continue
		}
		return crawlURL, inferBaseFromURL(crawlURL)
	}
	return "", ""
}

func extractSourceURLFromLine(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ""
	}

	lower := strings.ToLower(trimmed)
	if !strings.Contains(lower, "起始地址") && !strings.Contains(lower, "source url") {
		return ""
	}

	for _, separator := range []string{"：", ":"} {
		if idx := strings.Index(trimmed, separator); idx >= 0 {
			value := strings.TrimSpace(trimmed[idx+len(separator):])
			if strings.HasPrefix(strings.ToLower(value), "http://") || strings.HasPrefix(strings.ToLower(value), "https://") {
				return value
			}
		}
	}
	return ""
}

func inferBaseFromURL(crawlURL string) string {
	trimmed := strings.TrimSpace(crawlURL)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://") {
		parts := strings.SplitN(trimmed, "/", 4)
		if len(parts) >= 3 {
			return parts[0] + "//" + parts[2]
		}
	}
	return ""
}

// loadFilteredCodesFromOutput reads the structured filtered-code ledger when
// the scan path does not already have it from a crawl-profile artifact. This
// keeps the filmData fallback path consistent with the profile-driven path.
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
