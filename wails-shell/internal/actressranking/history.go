// Package actressranking keeps historical ranking import separate from online
// source fetching so legacy JSON compatibility cannot complicate live requests.
//
// Maintenance boundary:
// - discover and normalize user-supplied historical JSON files
// - merge valid records into the local-history cache bucket
// - ignore malformed import files without affecting online source caches
//
// Ownership summary:
// 1) support the legacy history JSON shapes accepted by previous releases
// 2) convert arbitrary JSON values into the package's stable Result contract
// 3) keep imported records in the dedicated localHistory cache bucket
//
// File map for maintainers:
// 1) history.go: directory discovery, compatibility parsing, local-history merge
// 2) types.go: Result and cache contracts
// 3) service.go: source selection and caller orchestration
package actressranking

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"javflow/internal/common"
)

const recordedMonthlyHistoryFilename = "recorded-monthly-rankings.json"

type recordedMonthlyHistoryFile struct {
	Version int      `json:"version"`
	Records []Result `json:"records"`
}

func listJSONFiles(directoryPath string) []string {
	normalized := strings.TrimSpace(directoryPath)
	if normalized == "" {
		return nil
	}
	info, err := os.Stat(normalized)
	if err != nil || !info.IsDir() {
		return nil
	}

	queue := []string{normalized}
	results := make([]string, 0)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		entries, err := os.ReadDir(current)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			entryPath := filepath.Join(current, entry.Name())
			if entry.IsDir() {
				queue = append(queue, entryPath)
				continue
			}
			if strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
				results = append(results, entryPath)
			}
		}
	}
	return results
}

func toIntValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(typed))
		return parsed
	default:
		return 0
	}
}

func toStringValue(value any) string {
	return strings.TrimSpace(fmt.Sprint(value))
}

func normalizeRankingItems(items any) []RankingItem {
	rawItems, ok := items.([]any)
	if !ok {
		return nil
	}
	result := make([]RankingItem, 0, len(rawItems))
	for index, rawItem := range rawItems {
		itemMap, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		actressName := strings.TrimSpace(toStringValue(itemMap["actressName"]))
		if actressName == "" {
			continue
		}
		rank := toIntValue(itemMap["rank"])
		if rank <= 0 {
			rank = index + 1
		}
		result = append(result, RankingItem{
			Rank:        rank,
			ActressName: actressName,
			ProfileURL:  strings.TrimSpace(toStringValue(itemMap["profileUrl"])),
			ImageURL:    strings.TrimSpace(toStringValue(itemMap["imageUrl"])),
		})
	}
	return result
}

func buildPeriodLabel(mode string, year int, month int) string {
	if mode == "annual" {
		return fmt.Sprintf("%d年", year)
	}
	return fmt.Sprintf("%d年%02d月", year, month)
}

func normalizeHistoryRecord(record map[string]any, filePath string) *Result {
	mode := "monthly"
	if strings.TrimSpace(toStringValue(record["mode"])) == "annual" {
		mode = "annual"
	}
	periodYear := toIntValue(record["periodYear"])
	periodMonth := toIntValue(record["periodMonth"])
	if mode == "monthly" && (periodMonth < 1 || periodMonth > 12) {
		return nil
	}
	items := normalizeRankingItems(record["items"])
	if periodYear <= 0 || len(items) == 0 {
		return nil
	}

	fetchedAt := strings.TrimSpace(toStringValue(record["fetchedAt"]))
	if fetchedAt == "" {
		if info, err := os.Stat(filePath); err == nil {
			fetchedAt = info.ModTime().Format(time.RFC3339)
		}
	}

	sourceName := strings.TrimSpace(toStringValue(record["sourceName"]))
	if sourceName == "" {
		sourceName = "本地历史导入"
	}
	title := strings.TrimSpace(toStringValue(record["title"]))
	if title == "" {
		title = fmt.Sprintf("本地历史榜单 %s", buildPeriodLabel(mode, periodYear, periodMonth))
	}
	periodLabel := strings.TrimSpace(toStringValue(record["periodLabel"]))
	if periodLabel == "" {
		periodLabel = buildPeriodLabel(mode, periodYear, periodMonth)
	}

	availableYears := []int{periodYear}
	if rawYears, ok := record["availableYears"].([]any); ok {
		for _, item := range rawYears {
			availableYears = append(availableYears, toIntValue(item))
		}
	}

	return &Result{
		Mode:           mode,
		SourceName:     sourceName,
		SourceURL:      strings.TrimSpace(toStringValue(record["sourceUrl"])),
		Title:          title,
		PeriodLabel:    periodLabel,
		PeriodYear:     periodYear,
		PeriodMonth:    periodMonth,
		Total:          maxInt(toIntValue(record["total"]), len(items)),
		AvailableYears: normalizeYearList(availableYears),
		AvailableMonths: func() []int {
			if mode == "monthly" {
				return []int{periodMonth}
			}
			return []int{}
		}(),
		FetchedAt: fetchedAt,
		Items:     items,
	}
}

func toHistoryRecords(payload any) []map[string]any {
	switch typed := payload.(type) {
	case []any:
		result := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if record, ok := item.(map[string]any); ok {
				result = append(result, record)
			}
		}
		return result
	case map[string]any:
		if records, ok := typed["records"].([]any); ok {
			result := make([]map[string]any, 0, len(records))
			for _, item := range records {
				if record, ok := item.(map[string]any); ok {
					result = append(result, record)
				}
			}
			return result
		}
		return []map[string]any{typed}
	default:
		return nil
	}
}

func historyWriteDirectory(directories []string) string {
	for _, directoryPath := range directories {
		if normalized := strings.TrimSpace(directoryPath); normalized != "" {
			return normalized
		}
	}
	return ""
}

// SaveMonthlyHistory keeps a user-confirmed monthly snapshot outside the
// replaceable source cache. An existing period is deliberately never
// overwritten: the first saved record is the user's historical evidence when
// the upstream site later removes that month.
func (s *Service) SaveMonthlyHistory(data Result, directories []string) (Result, bool, error) {
	directoryPath := historyWriteDirectory(directories)
	if directoryPath == "" {
		return Result{}, false, fmt.Errorf("本地榜单历史目录不可用")
	}

	snapshot := normalizeResultMetadata(data)
	if snapshot.Mode != "monthly" || snapshot.PeriodYear <= 0 || snapshot.PeriodMonth < 1 || snapshot.PeriodMonth > 12 {
		return Result{}, false, fmt.Errorf("只能保存有效的月榜")
	}
	if !snapshot.Complete {
		return Result{}, false, fmt.Errorf("当前月榜仅有 %d/%d 位，未保存不完整榜单", snapshot.Total, snapshot.ExpectedTotal)
	}

	originalSource := strings.TrimSpace(snapshot.OriginSourceName)
	if originalSource == "" {
		originalSource = strings.TrimSpace(snapshot.SourceName)
	}
	if originalSource == "" {
		originalSource = "未知来源"
	}
	snapshot.SourceName = fmt.Sprintf("用户保存月榜（原始来源：%s）", originalSource)
	snapshot.OriginSourceName = originalSource
	snapshot.Mode = "monthly"
	snapshot.AvailableYears = []int{snapshot.PeriodYear}
	snapshot.AvailableMonths = []int{snapshot.PeriodMonth}
	snapshot.RequestedSource = ""
	snapshot.RequestedSourceLabel = ""
	snapshot.ResolvedSource = ""
	snapshot.ResolvedSourceLabel = ""
	snapshot.FromCache = false
	snapshot.Stale = false
	snapshot.Notice = ""
	snapshot.ErrorMessage = ""
	snapshot.FallbackUsed = false
	if strings.TrimSpace(snapshot.FetchedAt) == "" {
		snapshot.FetchedAt = time.Now().Format(time.RFC3339)
	}

	if err := os.MkdirAll(directoryPath, 0o755); err != nil {
		return Result{}, false, err
	}
	filePath := filepath.Join(directoryPath, recordedMonthlyHistoryFilename)

	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	payload := recordedMonthlyHistoryFile{Version: 1, Records: []Result{}}
	if raw, err := os.ReadFile(filePath); err == nil {
		if err := json.Unmarshal(raw, &payload); err != nil {
			return Result{}, false, fmt.Errorf("无法读取已保存的月榜历史：%w", err)
		}
	} else if !os.IsNotExist(err) {
		return Result{}, false, err
	}
	if payload.Version <= 0 {
		payload.Version = 1
	}

	for _, existing := range payload.Records {
		if existing.Mode == "monthly" && existing.PeriodYear == snapshot.PeriodYear && existing.PeriodMonth == snapshot.PeriodMonth {
			return existing, true, nil
		}
	}
	payload.Records = append(payload.Records, snapshot)
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return Result{}, false, err
	}
	if err := common.WriteFileAtomic(filePath, encoded, 0o644); err != nil {
		return Result{}, false, err
	}
	return snapshot, false, nil
}

func mergeHistoryDirectoriesIntoCache(cache *cacheFile, directories []string) {
	if cache == nil {
		return
	}

	bucket := getSourceBucket(*cache, "localHistory")
	visited := map[string]struct{}{}
	files := make([]string, 0)
	for _, directoryPath := range directories {
		for _, filePath := range listJSONFiles(directoryPath) {
			if _, ok := visited[filePath]; ok {
				continue
			}
			visited[filePath] = struct{}{}
			files = append(files, filePath)
		}
	}

	for _, filePath := range files {
		payloadBytes, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}
		var payload any
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			continue
		}
		for _, record := range toHistoryRecords(payload) {
			normalized := normalizeHistoryRecord(record, filePath)
			if normalized == nil {
				continue
			}
			if normalized.Mode == "annual" {
				bucket.AnnualByYear[strconv.Itoa(normalized.PeriodYear)] = buildCachePayload(*normalized)
				bucket.AvailableYears = normalizeYearList(append(bucket.AvailableYears, append(normalized.AvailableYears, normalized.PeriodYear)...))
				continue
			}

			monthKey := getMonthKey(normalized.PeriodYear, normalized.PeriodMonth)
			if monthKey == "" {
				continue
			}
			bucket.MonthlyByPeriod[monthKey] = buildCachePayload(*normalized)
			if bucket.MonthlyLatestKey == "" || monthKey > bucket.MonthlyLatestKey {
				bucket.MonthlyLatestKey = monthKey
			}
			bucket.AvailableYears = normalizeYearList(append(bucket.AvailableYears, append(normalized.AvailableYears, normalized.PeriodYear)...))
		}
	}

	cache.Sources["localHistory"] = bucket
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
