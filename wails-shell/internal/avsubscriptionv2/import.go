// Ownership summary:
//   This file implements V2 subscription baseline import from crawler artifacts.
//
// File map for maintainers:
//   1) ImportFromOutput entry and artifact-root resolution.
//   2) crawl-profile.json and filmData.json importers.
//   3) Import helpers and validation.
//
package avsubscriptionv2

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"javflow/internal/contracts/crawlartifact"
)

// ImportFromOutput builds one V2 baseline from persisted crawl artifacts.
func (s *Service) ImportFromOutput(outputDir string) (ImportResult, error) {
	cleanOutputDir := strings.TrimSpace(outputDir)
	if cleanOutputDir == "" {
		return ImportResult{}, fmt.Errorf("请先选择抓取结果目录。")
	}

	artifactRoot := s.resolveArtifactRoot(cleanOutputDir)
	if result, err := s.importFromProfile(artifactRoot); err == nil {
		return result, nil
	}
	return s.importFromFilmData(cleanOutputDir)
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

func (s *Service) importFromProfile(outputDir string) (ImportResult, error) {
	paths, profile, err := crawlartifact.ReadCrawlProfileArtifactWithUserData(outputDir, s.paths.UserData)
	if err != nil {
		return ImportResult{}, err
	}

	actressName := strings.TrimSpace(profile.ActressName)
	if actressName == "" {
		return ImportResult{}, fmt.Errorf("crawl-profile.json 中未识别到主女优名称。")
	}

	baselineCodes := extractCodesFromOutput(outputDir, s.paths.UserData)
	if len(baselineCodes) == 0 {
		return ImportResult{}, fmt.Errorf("未能从隐藏或公开 filmData.json 读取完整番号基线。")
	}
	count := profile.CompletedCount
	if count <= 0 {
		count = profile.TargetCount
	}
	if count <= 0 {
		count = len(baselineCodes)
	}
	if count <= 0 {
		return ImportResult{}, fmt.Errorf("crawl-profile.json 中缺少有效影片数量。")
	}

	crawlURL := strings.TrimSpace(profile.CrawlURL)
	preferredBase := strings.TrimSpace(profile.SiteBase)
	if crawlURL == "" || preferredBase == "" {
		recoveredURL, recoveredBase := s.recoverSubscriptionTargetMetadata(outputDir)
		if crawlURL == "" {
			crawlURL = recoveredURL
		}
		if preferredBase == "" {
			preferredBase = recoveredBase
		}
	}

	now := time.Now().Format(time.RFC3339)
	next := Subscription{
		ActressName:          actressName,
		CrawlURL:             crawlURL,
		PreferredBase:        preferredBase,
		SourceType:           sourceTypeCrawlImport,
		BaselineCodes:        baselineCodes,
		BaselineCount:        len(baselineCodes),
		CurrentObservedCount: maxInt(count, len(baselineCodes)),
		CurrentTotal:         count,
		ItemsPerPage:         maxInt(defaultItemsPerPage, profile.ItemsPerPage),
		TotalPages:           maxInt(calcPages(count, maxInt(defaultItemsPerPage, profile.ItemsPerPage)), profile.TotalPages),
		CreatedAt:            now,
		BaselineSnapshotAt:   strings.TrimSpace(profile.CompletedAt),
		LastUpdatedAt:        now,
		PreferredOutputDir:   paths.OutputDir,
	}
	saved, added, updated, err := s.upsertWithChangeState(next)
	if err != nil {
		return ImportResult{}, err
	}

	return ImportResult{
		Subscription: saved,
		Added:        added,
		Updated:      updated,
		SourceType:   sourceTypeCrawlImport,
		OutputDir:    paths.OutputDir,
		FilmDataPath: firstNonEmpty(profile.FilmDataPath, paths.FilmDataPath),
	}, nil
}

func (s *Service) importFromFilmData(outputDir string) (ImportResult, error) {
	paths, records, err := crawlartifact.ReadFilmDataRecordsWithUserData(outputDir, s.paths.UserData)
	if err != nil {
		return ImportResult{}, err
	}
	if len(records) == 0 {
		return ImportResult{}, fmt.Errorf("filmData.json 中没有可识别的影片记录。")
	}

	actressFilmSet, actressNames, actressOrder := buildActressFilmSet(records)
	primaryKey := detectPrimaryActressKey(actressFilmSet, actressNames, actressOrder, outputDir)
	if primaryKey == "" {
		return ImportResult{}, fmt.Errorf("filmData.json 中未能识别到主女优。")
	}

	actressName := actressNames[primaryKey]
	if actressName == "" {
		return ImportResult{}, fmt.Errorf("filmData.json 中未能识别到主女优名称。")
	}

	recoveredURL, recoveredBase := s.recoverSubscriptionTargetMetadata(outputDir)
	baselineCodes := sortedFilmSetKeys(actressFilmSet[primaryKey])
	now := time.Now().Format(time.RFC3339)
	next := Subscription{
		ActressName:          actressName,
		CrawlURL:             recoveredURL,
		PreferredBase:        recoveredBase,
		SourceType:           sourceTypeCrawlImport,
		BaselineCodes:        baselineCodes,
		BaselineCount:        len(baselineCodes),
		CurrentObservedCount: len(baselineCodes),
		CurrentTotal:         len(baselineCodes),
		ItemsPerPage:         defaultItemsPerPage,
		TotalPages:           calcPages(len(baselineCodes), defaultItemsPerPage),
		CreatedAt:            now,
		BaselineSnapshotAt:   now,
		LastUpdatedAt:        now,
		PreferredOutputDir:   paths.OutputDir,
	}
	saved, added, updated, err := s.upsertWithChangeState(next)
	if err != nil {
		return ImportResult{}, err
	}

	return ImportResult{
		Subscription: saved,
		Added:        added,
		Updated:      updated,
		SourceType:   sourceTypeCrawlImport,
		OutputDir:    paths.OutputDir,
		FilmDataPath: paths.FilmDataPath,
	}, nil
}

func (s *Service) upsertWithChangeState(next Subscription) (Subscription, bool, bool, error) {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	items, err := s.load()
	if err != nil {
		return Subscription{}, false, false, err
	}

	now := time.Now().Format(time.RFC3339)
	next = normalizeSubscription(next, now)
	index := findSubscriptionIndex(items, next)
	added := index < 0
	updated := false
	if index >= 0 {
		current := items[index]
		next = mergeSubscriptionState(current, next, now)
		next.LastUpdatedAt = now
		updated = current.BaselineCount != next.BaselineCount ||
			current.CrawlURL != next.CrawlURL ||
			current.PreferredBase != next.PreferredBase ||
			current.CurrentObservedCount != next.CurrentObservedCount
		items[index] = normalizeSubscription(next, now)
	} else {
		items = append(items, normalizeSubscription(next, now))
	}

	sortSubscriptions(items)
	if err := s.save(items); err != nil {
		return Subscription{}, false, false, err
	}
	savedIndex := findSubscriptionIndex(items, next)
	if savedIndex >= 0 {
		return items[savedIndex], added, updated, nil
	}
	return next, added, updated, nil
}

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
		hintKey := normalizeFolderHint(hint)
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
			return r == ',' || r == '、' || r == '/' || r == '\n' || r == '\r'
		}) {
			appendName(item)
		}
	}
	return output
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
	return normalizeCodes(items)
}

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
