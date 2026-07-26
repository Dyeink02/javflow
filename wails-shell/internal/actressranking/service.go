// Package actressranking serves normalized actress ranking data from online and
// local sources.
//
// Maintenance boundary:
// - own source selection/fallback policy
// - own ranking cache normalization and persistence
// - return one normalized result shape to the bridge/UI
// - keep crawl/subscription workflow state outside this package
//
// Ownership summary:
// 1) expose the normalized actress-ranking facade over online and local sources
// 2) keep source fallback, cache persistence, and result normalization in one package
// 3) return stable ranking outputs without absorbing crawl/subscription workflow state
package actressranking

import (
	"encoding/json"
	"fmt"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/html"
)

// File map for maintainers:
// 1) source/cache constants and option/result contracts
// 2) cache/history load + persist helpers
// 3) source-specific fetch/parse orchestration for avfan / official ranking data
// 4) normalization helpers that collapse all source outputs into one Result shape
//
// Troubleshooting rule:
// - cache staleness / source fallback issues should start in this file
// - browser/session specifics should start in the source/browser helpers
// - UI/bridge concerns should stay outside this package

const (
	avfanMonthlyURL        = "https://av-fan.tokyo/ranking/fanza-dvd-actress-monthly.php"
	avfanYearlyURL         = "https://av-fan.tokyo/ranking/fanza-rental-dvd-actress-top100.php"
	officialMonthlyURL     = "https://www.dmm.co.jp/mono/dvd/-/ranking/=/mode=actress/term=monthly/"
	monthlyCacheMaxAgeMS   = 12 * 60 * 60 * 1000
	yearlyCacheMaxAgeMS    = 7 * 24 * 60 * 60 * 1000
	cacheVersion           = 2
	defaultOfficialTimeout = 45 * time.Second
)

var (
	monthlyPeriodPattern = regexp.MustCompile(`(\d{4})\.(\d{2})`)
	yearPattern          = regexp.MustCompile(`(\d{4})`)
	worksCountPattern    = regexp.MustCompile(`商品数\s*[:：]\s*(\d+)`)
	digitsPattern        = regexp.MustCompile(`\d+`)
)

type rankingError struct {
	message string
	code    string
}

func (e *rankingError) Error() string {
	return e.message
}

func createRankingError(message string, code string) error {
	return &rankingError{
		message: strings.TrimSpace(message),
		code:    strings.TrimSpace(code),
	}
}

type Options struct {
	Mode               string
	Year               int
	Month              int
	Source             string
	Proxy              string
	ForceRefresh       bool
	CacheFilePath      string
	HistoryDirectories []string
}

type RankingItem struct {
	Rank        int    `json:"rank"`
	ActressName string `json:"actressName"`
	ProfileURL  string `json:"profileUrl,omitempty"`
	ImageURL    string `json:"imageUrl,omitempty"`
	LatestTitle string `json:"latestTitle,omitempty"`
	LatestURL   string `json:"latestUrl,omitempty"`
	WorksCount  *int   `json:"worksCount,omitempty"`
}

type Result struct {
	Title                string        `json:"title"`
	SourceName           string        `json:"sourceName"`
	OriginSourceName     string        `json:"originSourceName,omitempty"`
	SourceURL            string        `json:"sourceUrl,omitempty"`
	Mode                 string        `json:"mode"`
	RequestedSource      string        `json:"requestedSource,omitempty"`
	RequestedSourceLabel string        `json:"requestedSourceLabel,omitempty"`
	ResolvedSource       string        `json:"resolvedSource,omitempty"`
	ResolvedSourceLabel  string        `json:"resolvedSourceLabel,omitempty"`
	PeriodLabel          string        `json:"periodLabel"`
	PeriodYear           int           `json:"periodYear"`
	PeriodMonth          int           `json:"periodMonth"`
	Total                int           `json:"total"`
	AvailableYears       []int         `json:"availableYears"`
	AvailableMonths      []int         `json:"availableMonths"`
	FetchedAt            string        `json:"fetchedAt"`
	Items                []RankingItem `json:"items"`
	FromCache            bool          `json:"fromCache"`
	Stale                bool          `json:"stale"`
	Notice               string        `json:"notice,omitempty"`
	ErrorMessage         string        `json:"errorMessage,omitempty"`
	FallbackUsed         bool          `json:"fallbackUsed"`
}

type cacheEntry struct {
	CachedAt string `json:"cachedAt"`
	Data     Result `json:"data"`
}

type sourceCache struct {
	MonthlyLatestKey string                `json:"monthlyLatestKey"`
	MonthlyByPeriod  map[string]cacheEntry `json:"monthlyByPeriod"`
	AnnualByYear     map[string]cacheEntry `json:"annualByYear"`
	AvailableYears   []int                 `json:"availableYears"`
}

type cacheFile struct {
	Version int                    `json:"version"`
	Sources map[string]sourceCache `json:"sources"`
}

type cachedMonthly struct {
	BucketID string
	Key      string
	Year     int
	Month    int
	CachedAt string
	Entry    cacheEntry
}

type cachedAnnual struct {
	BucketID string
	Year     int
	CachedAt string
	Entry    cacheEntry
}

type rankingContext struct {
	RequestedChannel string
	Mode             string
	Year             int
	Month            int
	ForceRefresh     bool
	Proxy            string
	Cache            cacheFile
	CacheFilePath    string
}

type Service struct {
	browser *browserService
}

func NewService() *Service {
	return &Service{
		browser: newBrowserService(),
	}
}

// sourceChannel keeps UI-facing source identity separate from cache-bucket
// identity so fallback policy can change without rewriting cache layout.
type sourceChannel struct {
	ID          string
	Label       string
	CacheBucket string
}

// sourceChannels is the normalized routing table from user-selected channel to
// fetch/cache behavior.
var sourceChannels = map[string]sourceChannel{
	"smart": {ID: "smart", Label: "智能推荐", CacheBucket: "smart"},
	"fanza": {ID: "fanza", Label: "FANZA 官方", CacheBucket: "official"},
	"dmm":   {ID: "dmm", Label: "DMM 官方", CacheBucket: "official"},
	"avfan": {ID: "avfan", Label: "AVfan 在线", CacheBucket: "avfan"},
	"local": {ID: "local", Label: "本地历史", CacheBucket: "local"},
}

// messages centralizes user-facing fallback text so source policy changes do
// not scatter wording edits through fetch branches.
var messages = struct {
	LocalCacheMissing             string
	OfficialMonthlyOnly           string
	RequestedMonthFallbackToCache string
	SmartOfficialNotice           string
	AVFanDirectRetryNotice        string
	LocalMonthlyMissing           func(string) string
	LocalAnnualMissing            func(int) string
	FallbackTo                    func(string) string
	OfficialFallbackTo            func(string) string
	OfficialAnnualFallbackTo      func(string) string
}{
	LocalCacheMissing:             "本地历史暂无可用榜单缓存，请先成功抓取一次在线榜单。",
	OfficialMonthlyOnly:           "当前 DMM/FANZA 官方渠道仅提供当前月度女优榜单。",
	RequestedMonthFallbackToCache: "所选月份暂时无稳定在线源，已回退到本地缓存。",
	SmartOfficialNotice:           "智能模式将优先使用官方当前月榜，不可用时自动回退。",
	AVFanDirectRetryNotice:        "检测到当前代理无法访问 AVfan，已自动切换为直连模式继续获取榜单。",
	LocalMonthlyMissing: func(key string) string {
		return fmt.Sprintf("本地历史暂无 %s 的月榜缓存。", strings.TrimSpace(key))
	},
	LocalAnnualMissing: func(year int) string {
		return fmt.Sprintf("本地历史暂无 %d年 的年榜缓存。", year)
	},
	FallbackTo: func(targetName string) string {
		return fmt.Sprintf("已自动切换至 %s。", strings.TrimSpace(targetName))
	},
	OfficialFallbackTo: func(targetName string) string {
		return fmt.Sprintf("官方渠道暂时不可用，已自动切换至 %s。", strings.TrimSpace(targetName))
	},
	OfficialAnnualFallbackTo: func(targetName string) string {
		return fmt.Sprintf("官方渠道暂仅支持月榜，已自动切换至 %s。", strings.TrimSpace(targetName))
	},
}

func buildSourceCache() sourceCache {
	return sourceCache{
		MonthlyByPeriod: map[string]cacheEntry{},
		AnnualByYear:    map[string]cacheEntry{},
		AvailableYears:  []int{},
	}
}

func getCacheSkeleton() cacheFile {
	return cacheFile{
		Version: cacheVersion,
		Sources: map[string]sourceCache{
			"avfan":        buildSourceCache(),
			"official":     buildSourceCache(),
			"localHistory": buildSourceCache(),
		},
	}
}

func normalizeSourceCache(source sourceCache) sourceCache {
	normalized := buildSourceCache()
	normalized.MonthlyLatestKey = strings.TrimSpace(source.MonthlyLatestKey)
	for key, entry := range source.MonthlyByPeriod {
		normalized.MonthlyByPeriod[strings.TrimSpace(key)] = entry
	}
	for key, entry := range source.AnnualByYear {
		normalized.AnnualByYear[strings.TrimSpace(key)] = entry
	}
	normalized.AvailableYears = normalizeYearList(source.AvailableYears)
	return normalized
}

func loadCache(filePath string) cacheFile {
	// Cache loading accepts both current and legacy layouts so history imports do
	// not break when maintainers tighten the main cache schema.
	skeleton := getCacheSkeleton()
	trimmedPath := strings.TrimSpace(filePath)
	if trimmedPath == "" {
		return skeleton
	}

	payload, err := os.ReadFile(trimmedPath)
	if err != nil {
		return skeleton
	}

	var current cacheFile
	if err := json.Unmarshal(payload, &current); err == nil && len(current.Sources) > 0 {
		current.Version = cacheVersion
		skeleton.Sources["avfan"] = normalizeSourceCache(current.Sources["avfan"])
		skeleton.Sources["official"] = normalizeSourceCache(current.Sources["official"])
		skeleton.Sources["localHistory"] = normalizeSourceCache(current.Sources["localHistory"])
		return skeleton
	}

	var legacy sourceCache
	if err := json.Unmarshal(payload, &legacy); err == nil {
		skeleton.Sources["avfan"] = normalizeSourceCache(legacy)
	}
	return skeleton
}

func writeCache(filePath string, cache cacheFile) error {
	trimmedPath := strings.TrimSpace(filePath)
	if trimmedPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(trimmedPath), 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(trimmedPath, payload, 0o644)
}

func normalizeRankingChannel(channel string) string {
	normalized := strings.ToLower(strings.TrimSpace(channel))
	if _, ok := sourceChannels[normalized]; ok {
		return normalized
	}
	return "smart"
}

func getChannelLabel(channel string) string {
	return sourceChannels[normalizeRankingChannel(channel)].Label
}

// Availability normalization keeps UI choice lists stable even when upstream
// sources expose slightly different year/month metadata shapes.
func normalizeYearList(values []int) []int {
	seen := map[int]struct{}{}
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value < 2000 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i] > result[j]
	})
	return result
}

func normalizeMonthList(values []int) []int {
	seen := map[int]struct{}{}
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value < 1 || value > 12 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i] > result[j]
	})
	return result
}

func getMonthKey(year int, month int) string {
	if year <= 0 || month < 1 || month > 12 {
		return ""
	}
	return fmt.Sprintf("%04d-%02d", year, month)
}

func getCurrentJapanYearMonth() (int, int) {
	location, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		now := time.Now()
		return now.Year(), int(now.Month())
	}
	now := time.Now().In(location)
	return now.Year(), int(now.Month())
}

func absoluteURL(href string, baseURL string) string {
	normalizedHref := strings.TrimSpace(href)
	if normalizedHref == "" {
		return ""
	}
	parsed, err := neturl.Parse(normalizedHref)
	if err == nil && parsed.IsAbs() {
		return parsed.String()
	}
	base, err := neturl.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return normalizedHref
	}
	return base.ResolveReference(parsed).String()
}

func buildCachePayload(data Result) cacheEntry {
	return cacheEntry{
		CachedAt: time.Now().Format(time.RFC3339),
		Data:     data,
	}
}

func isFresh(entry cacheEntry, maxAgeMS int64) bool {
	if strings.TrimSpace(entry.CachedAt) == "" {
		return false
	}
	cachedAt, err := time.Parse(time.RFC3339, entry.CachedAt)
	if err != nil {
		return false
	}
	return time.Since(cachedAt) <= time.Duration(maxAgeMS)*time.Millisecond
}

func mergeNotice(parts ...string) string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		text := strings.TrimSpace(part)
		if text == "" {
			continue
		}
		if _, ok := seen[text]; ok {
			continue
		}
		seen[text] = struct{}{}
		result = append(result, text)
	}
	return strings.Join(result, " ")
}

func getSourceBucket(cache cacheFile, bucketID string) sourceCache {
	bucket, ok := cache.Sources[bucketID]
	if !ok {
		return buildSourceCache()
	}
	return normalizeSourceCache(bucket)
}

// Cache readers below intentionally work in source-bucket terms so upstream
// source fetchers and downstream UI callers do not need to share cache layout
// assumptions.
func listMonthlyPeriods(cache cacheFile, bucketIDs []string) []cachedMonthly {
	result := make([]cachedMonthly, 0)
	for _, bucketID := range bucketIDs {
		bucket := getSourceBucket(cache, bucketID)
		for key, entry := range bucket.MonthlyByPeriod {
			match := regexp.MustCompile(`^(\d{4})-(\d{2})$`).FindStringSubmatch(key)
			if len(match) != 3 || strings.TrimSpace(entry.Data.Title) == "" {
				continue
			}
			year, _ := strconv.Atoi(match[1])
			month, _ := strconv.Atoi(match[2])
			result = append(result, cachedMonthly{
				BucketID: bucketID,
				Key:      key,
				Year:     year,
				Month:    month,
				CachedAt: entry.CachedAt,
				Entry:    entry,
			})
		}
	}
	return result
}

func listAnnualEntries(cache cacheFile, bucketIDs []string) []cachedAnnual {
	result := make([]cachedAnnual, 0)
	for _, bucketID := range bucketIDs {
		bucket := getSourceBucket(cache, bucketID)
		for yearKey, entry := range bucket.AnnualByYear {
			year, err := strconv.Atoi(strings.TrimSpace(regexp.MustCompile(`[^\d]`).ReplaceAllString(yearKey, "")))
			if err != nil || strings.TrimSpace(entry.Data.Title) == "" {
				continue
			}
			result = append(result, cachedAnnual{
				BucketID: bucketID,
				Year:     year,
				CachedAt: entry.CachedAt,
				Entry:    entry,
			})
		}
	}
	return result
}

func getMonthlyAvailability(cache cacheFile, bucketIDs []string, selectedYear int) ([]int, []int) {
	periods := listMonthlyPeriods(cache, bucketIDs)
	yearValues := make([]int, 0, len(periods))
	for _, item := range periods {
		yearValues = append(yearValues, item.Year)
	}
	availableYears := normalizeYearList(yearValues)
	effectiveYear := selectedYear
	if effectiveYear <= 0 && len(availableYears) > 0 {
		effectiveYear = availableYears[0]
	}
	monthValues := make([]int, 0, len(periods))
	for _, item := range periods {
		if item.Year == effectiveYear {
			monthValues = append(monthValues, item.Month)
		}
	}
	return availableYears, normalizeMonthList(monthValues)
}

func getAnnualAvailability(cache cacheFile, bucketIDs []string) []int {
	values := make([]int, 0)
	for _, item := range listAnnualEntries(cache, bucketIDs) {
		values = append(values, item.Year)
	}
	for _, bucketID := range bucketIDs {
		values = append(values, getSourceBucket(cache, bucketID).AvailableYears...)
	}
	return normalizeYearList(values)
}

func resolveCachedMonthlyEntry(cache cacheFile, bucketIDs []string, year int, month int, exactOnly bool) *cachedMonthly {
	requestedKey := getMonthKey(year, month)
	if requestedKey != "" {
		for _, item := range listMonthlyPeriods(cache, bucketIDs) {
			if item.Key == requestedKey {
				entry := item
				return &entry
			}
		}
	}
	if exactOnly {
		return nil
	}

	periods := listMonthlyPeriods(cache, bucketIDs)
	sort.Slice(periods, func(i, j int) bool {
		left, _ := time.Parse(time.RFC3339, periods[i].CachedAt)
		right, _ := time.Parse(time.RFC3339, periods[j].CachedAt)
		return right.After(left)
	})
	if len(periods) == 0 {
		return nil
	}
	entry := periods[0]
	return &entry
}

func resolveCachedAnnualEntry(cache cacheFile, bucketIDs []string, year int, exactOnly bool) *cachedAnnual {
	if year > 0 {
		for _, item := range listAnnualEntries(cache, bucketIDs) {
			if item.Year == year {
				entry := item
				return &entry
			}
		}
	}
	if exactOnly {
		return nil
	}

	entries := listAnnualEntries(cache, bucketIDs)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Year > entries[j].Year
	})
	if len(entries) == 0 {
		return nil
	}
	entry := entries[0]
	return &entry
}

func decorateMonthlyResult(cache cacheFile, bucketIDs []string, data Result, requestedChannel string, resolvedChannel string, fromCache bool, stale bool, notice string, errorMessage string, fallbackUsed bool) Result {
	availableYears, availableMonths := getMonthlyAvailability(cache, bucketIDs, data.PeriodYear)
	result := data
	if normalizeRankingChannel(resolvedChannel) == "local" {
		result.SourceName = fmt.Sprintf("%s · %s", getChannelLabel("local"), strings.TrimSpace(data.SourceName))
	}
	result.OriginSourceName = data.SourceName
	result.Mode = "monthly"
	result.RequestedSource = requestedChannel
	result.RequestedSourceLabel = getChannelLabel(requestedChannel)
	result.ResolvedSource = resolvedChannel
	result.ResolvedSourceLabel = getChannelLabel(resolvedChannel)
	result.AvailableYears = availableYears
	result.AvailableMonths = availableMonths
	result.FromCache = fromCache
	result.Stale = stale
	result.Notice = strings.TrimSpace(notice)
	result.ErrorMessage = strings.TrimSpace(errorMessage)
	result.FallbackUsed = fallbackUsed
	return result
}

func decorateAnnualResult(cache cacheFile, bucketIDs []string, data Result, requestedChannel string, resolvedChannel string, fromCache bool, stale bool, notice string, errorMessage string, fallbackUsed bool) Result {
	result := data
	if normalizeRankingChannel(resolvedChannel) == "local" {
		result.SourceName = fmt.Sprintf("%s · %s", getChannelLabel("local"), strings.TrimSpace(data.SourceName))
	}
	result.OriginSourceName = data.SourceName
	result.Mode = "annual"
	result.RequestedSource = requestedChannel
	result.RequestedSourceLabel = getChannelLabel(requestedChannel)
	result.ResolvedSource = resolvedChannel
	result.ResolvedSourceLabel = getChannelLabel(resolvedChannel)
	result.AvailableYears = getAnnualAvailability(cache, bucketIDs)
	result.AvailableMonths = []int{}
	result.FromCache = fromCache
	result.Stale = stale
	result.Notice = strings.TrimSpace(notice)
	result.ErrorMessage = strings.TrimSpace(errorMessage)
	result.FallbackUsed = fallbackUsed
	return result
}

func persistMonthly(cache *cacheFile, bucketID string, data Result) {
	if cache == nil {
		return
	}
	bucket := getSourceBucket(*cache, bucketID)
	key := getMonthKey(data.PeriodYear, data.PeriodMonth)
	if key == "" {
		return
	}
	bucket.MonthlyLatestKey = key
	bucket.MonthlyByPeriod[key] = buildCachePayload(data)
	cache.Sources[bucketID] = bucket
}

func persistAnnual(cache *cacheFile, bucketID string, data Result) {
	if cache == nil {
		return
	}
	bucket := getSourceBucket(*cache, bucketID)
	if data.PeriodYear <= 0 {
		return
	}
	bucket.AnnualByYear[strconv.Itoa(data.PeriodYear)] = buildCachePayload(data)
	bucket.AvailableYears = normalizeYearList(append(bucket.AvailableYears, append(data.AvailableYears, data.PeriodYear)...))
	cache.Sources[bucketID] = bucket
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

func stripControlChars(value string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value))
}

func decodeActressNameFromProfileURL(profileURL string) string {
	parsed, err := neturl.Parse(strings.TrimSpace(profileURL))
	if err != nil {
		return ""
	}
	slug := filepath.Base(parsed.Path)
	slug = strings.TrimSuffix(slug, filepath.Ext(slug))
	if decoded, err := neturl.QueryUnescape(slug); err == nil {
		return stripControlChars(decoded)
	}
	return stripControlChars(slug)
}

func buildAVFanTitle(mode string, periodYear int, periodMonth int) string {
	if mode == "annual" {
		return fmt.Sprintf("%d AVfan FANZA DVD Actress Annual Ranking", periodYear)
	}
	return fmt.Sprintf("%d.%02d AVfan FANZA DVD Actress Monthly Ranking", periodYear, periodMonth)
}

func buildAVFanPeriodLabel(mode string, periodYear int, periodMonth int) string {
	if mode == "annual" {
		return fmt.Sprintf("%d年", periodYear)
	}
	return fmt.Sprintf("%d年%02d月", periodYear, periodMonth)
}

func parsePeriodParts(mode string, title string, fallbackYear int) (int, int, string) {
	if match := monthlyPeriodPattern.FindStringSubmatch(title); len(match) == 3 {
		year, _ := strconv.Atoi(match[1])
		month, _ := strconv.Atoi(match[2])
		return year, month, fmt.Sprintf("%d年%02d月", year, month)
	}

	if mode == "annual" {
		if match := yearPattern.FindStringSubmatch(title); len(match) == 2 {
			year, _ := strconv.Atoi(match[1])
			return year, 0, fmt.Sprintf("%d年", year)
		}
		if fallbackYear > 0 {
			return fallbackYear, 0, fmt.Sprintf("%d年", fallbackYear)
		}
	}

	year, month := getCurrentJapanYearMonth()
	if mode == "annual" {
		return year, 0, fmt.Sprintf("%d年", year)
	}
	return year, month, fmt.Sprintf("%d年%02d月", year, month)
}

func findElementsWithClass(root *html.Node, tagName string, classNames ...string) []*html.Node {
	return findAll(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode &&
			strings.EqualFold(node.Data, tagName) &&
			hasAllClasses(node, classNames...)
	})
}

func parseAVFanRankingHTML(htmlSource string, mode string, sourceURL string, fallbackYear int) (Result, error) {
	root, err := parseHTMLDocument(htmlSource)
	if err != nil {
		return Result{}, err
	}

	titleNode := findFirst(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode && strings.EqualFold(node.Data, "title")
	})
	pageTitle := stripControlChars(nodeText(titleNode))
	pageTitle = strings.TrimSpace(pageTitle)
	if pageTitle == "" {
		pageTitle = "AVfan 榜单"
	}

	periodYear, periodMonth, periodLabel := parsePeriodParts(mode, pageTitle, fallbackYear)
	items := make([]RankingItem, 0)

	for _, listNode := range findElementsWithClass(root, "li") {
		rankNode := findFirst(listNode, func(node *html.Node) bool {
			return node.Type == html.ElementNode && strings.EqualFold(node.Data, "b")
		})
		rankValue, _ := strconv.Atoi(strings.TrimSpace(nodeText(rankNode)))

		anchors := findAll(listNode, func(node *html.Node) bool {
			return node.Type == html.ElementNode &&
				strings.EqualFold(node.Data, "a") &&
				strings.Contains(getAttribute(node, "href"), "/actress/")
		})
		var actressAnchor *html.Node
		if len(anchors) > 0 {
			actressAnchor = anchors[len(anchors)-1]
		}
		var imageNode *html.Node
		imageNode = findFirst(listNode, func(node *html.Node) bool {
			return node.Type == html.ElementNode && strings.EqualFold(node.Data, "img")
		})

		actressName := ""
		profileURL := ""
		if actressAnchor != nil {
			actressName = stripControlChars(nodeText(actressAnchor))
			profileURL = absoluteURL(getAttribute(actressAnchor, "href"), sourceURL)
		}
		if actressName == "" && imageNode != nil {
			actressName = stripControlChars(getAttribute(imageNode, "alt"))
		}
		decodedName := decodeActressNameFromProfileURL(profileURL)
		if decodedName != "" {
			actressName = decodedName
		}
		if actressName == "" || rankValue <= 0 {
			continue
		}

		items = append(items, RankingItem{
			Rank:        rankValue,
			ActressName: actressName,
			ProfileURL:  profileURL,
			ImageURL:    absoluteURL(getAttribute(imageNode, "src"), sourceURL),
		})
	}

	yearValues := make([]int, 0)
	for _, yearLink := range findElementsWithClass(root, "a") {
		if parent := yearLink.Parent; parent != nil && hasClass(parent, "ranking-year-link") {
			yearValues = append(yearValues, toIntValue(nodeText(yearLink)))
		}
	}
	if len(items) == 0 {
		return Result{}, createRankingError("未从 AVfan 榜单页解析到有效内容。", "avfan_parse_empty")
	}

	return Result{
		Mode:           mode,
		SourceName:     "AVfan 在线",
		SourceURL:      sourceURL,
		Title:          buildAVFanTitle(mode, periodYear, periodMonth),
		PeriodLabel:    periodLabel,
		PeriodYear:     periodYear,
		PeriodMonth:    periodMonth,
		Total:          len(items),
		AvailableYears: normalizeYearList(yearValues),
		AvailableMonths: func() []int {
			if mode == "monthly" && periodMonth > 0 {
				return []int{periodMonth}
			}
			return []int{}
		}(),
		FetchedAt: time.Now().Format(time.RFC3339),
		Items:     items,
	}, nil
}

func getOfficialSourceName(requestedChannel string) string {
	switch requestedChannel {
	case "dmm":
		return "DMM 官方"
	case "fanza":
		return "FANZA 官方"
	default:
		return "DMM/FANZA 官方"
	}
}

func isAgeCheckPage(pageURL string, htmlSource string, title string) bool {
	return strings.Contains(pageURL, "/age_check/") ||
		strings.Contains(title, "年齢認証") ||
		strings.Contains(htmlSource, "/age_check/")
}

func parseOfficialMonthlyRankingHTML(htmlSource string, requestedChannel string) (Result, error) {
	root, err := parseHTMLDocument(htmlSource)
	if err != nil {
		return Result{}, err
	}

	titleNode := findFirst(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode && strings.EqualFold(node.Data, "title")
	})
	pageTitle := strings.TrimSpace(nodeText(titleNode))
	if pageTitle == "" {
		pageTitle = "官方女优月榜"
	}
	year, month := getCurrentJapanYearMonth()

	rows := findAll(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode && hasClass(node, "bd-b")
	})
	items := make([]RankingItem, 0)
	for _, row := range rows {
		rankNode := findFirst(row, func(node *html.Node) bool {
			return node.Type == html.ElementNode && hasClass(node, "rank")
		})
		rankValue, _ := strconv.Atoi(strings.TrimSpace(nodeText(rankNode)))

		var actressAnchor *html.Node
		dataNode := findFirst(row, func(node *html.Node) bool {
			return node.Type == html.ElementNode && hasClass(node, "data")
		})
		if dataNode != nil {
			pNode := firstElementChild(dataNode, func(node *html.Node) bool {
				return node.Type == html.ElementNode && strings.EqualFold(node.Data, "p")
			})
			if pNode != nil {
				actressAnchor = findFirst(pNode, func(node *html.Node) bool {
					return node.Type == html.ElementNode && strings.EqualFold(node.Data, "a")
				})
			}
		}

		latestWorkLink := findFirst(row, func(node *html.Node) bool {
			return node.Type == html.ElementNode &&
				strings.EqualFold(node.Data, "a") &&
				strings.Contains(getAttribute(node, "href"), "/detail/")
		})
		imageNode := findFirst(row, func(node *html.Node) bool {
			return node.Type == html.ElementNode && strings.EqualFold(node.Data, "img")
		})
		actressName := ""
		if actressAnchor != nil {
			actressName = strings.TrimSpace(nodeText(actressAnchor))
		}
		if actressName == "" && imageNode != nil {
			actressName = strings.TrimSpace(getAttribute(imageNode, "alt"))
		}
		if actressName == "" || rankValue <= 0 {
			continue
		}

		rowText := nodeText(row)
		var worksCount *int
		if match := worksCountPattern.FindStringSubmatch(rowText); len(match) == 2 {
			if parsed, err := strconv.Atoi(match[1]); err == nil {
				worksCount = &parsed
			}
		}

		items = append(items, RankingItem{
			Rank:        rankValue,
			ActressName: actressName,
			ProfileURL:  absoluteURL(getAttribute(actressAnchor, "href"), officialMonthlyURL),
			ImageURL:    absoluteURL(getAttribute(imageNode, "src"), officialMonthlyURL),
			LatestTitle: strings.TrimSpace(nodeText(latestWorkLink)),
			LatestURL:   absoluteURL(getAttribute(latestWorkLink, "href"), officialMonthlyURL),
			WorksCount:  worksCount,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Rank < items[j].Rank
	})
	if len(items) == 0 {
		return Result{}, createRankingError("未从 DMM/FANZA 官方月榜解析到女优列表。", "official_parse_empty")
	}

	return Result{
		Mode:            "monthly",
		SourceName:      getOfficialSourceName(requestedChannel),
		SourceURL:       officialMonthlyURL,
		Title:           pageTitle,
		PeriodLabel:     fmt.Sprintf("%d年%02d月（官方当前月榜）", year, month),
		PeriodYear:      year,
		PeriodMonth:     month,
		Total:           len(items),
		AvailableYears:  []int{year},
		AvailableMonths: []int{month},
		FetchedAt:       time.Now().Format(time.RFC3339),
		Items:           items,
	}, nil
}

func (s *Service) fetchLatestAVFanMonthlyRanking(proxyValue string) (Result, string, error) {
	htmlSource, sourceURL, _, err := s.browser.fetchAVFanHTML(avfanMonthlyURL, proxyValue)
	if err != nil {
		if strings.TrimSpace(proxyValue) != "" && isBrowserProxyError(err) {
			htmlSource, sourceURL, _, err = s.browser.fetchAVFanHTML(avfanMonthlyURL, "")
			if err == nil {
				result, parseErr := parseAVFanRankingHTML(htmlSource, "monthly", sourceURL, 0)
				if parseErr != nil {
					return Result{}, "", parseErr
				}
				return result, messages.AVFanDirectRetryNotice, nil
			}
		}
		return Result{}, "", err
	}
	result, err := parseAVFanRankingHTML(htmlSource, "monthly", sourceURL, 0)
	return result, "", err
}

func (s *Service) fetchAVFanAnnualRanking(year int, proxyValue string) (Result, string, error) {
	preferredYear := year
	if preferredYear <= 0 {
		preferredYear = time.Now().Year() - 1
	}
	landingURL := fmt.Sprintf("%s?year=%d", avfanYearlyURL, preferredYear)
	htmlSource, _, _, err := s.browser.fetchAVFanHTML(landingURL, proxyValue)
	notice := ""
	if err != nil {
		if strings.TrimSpace(proxyValue) != "" && isBrowserProxyError(err) {
			htmlSource, _, _, err = s.browser.fetchAVFanHTML(landingURL, "")
			if err == nil {
				notice = messages.AVFanDirectRetryNotice
			}
		}
		if err != nil {
			return Result{}, "", err
		}
	}

	root, parseErr := parseHTMLDocument(htmlSource)
	if parseErr != nil {
		return Result{}, notice, parseErr
	}
	availableYears := make([]int, 0)
	for _, yearLink := range findElementsWithClass(root, "a") {
		if parent := yearLink.Parent; parent != nil && hasClass(parent, "ranking-year-link") {
			availableYears = append(availableYears, toIntValue(nodeText(yearLink)))
		}
	}
	availableYears = normalizeYearList(availableYears)

	initial, err := parseAVFanRankingHTML(htmlSource, "annual", landingURL, preferredYear)
	if err == nil {
		initial.AvailableYears = normalizeYearList(append(availableYears, initial.AvailableYears...))
		return initial, notice, nil
	}

	fallbackYear := 0
	for _, value := range availableYears {
		if value <= preferredYear {
			fallbackYear = value
			break
		}
	}
	if fallbackYear == 0 && len(availableYears) > 0 {
		fallbackYear = availableYears[0]
	}
	if fallbackYear == 0 {
		return Result{}, notice, createRankingError("未找到可用的 AVfan 年榜年份。", "avfan_annual_year_missing")
	}

	fallbackURL := fmt.Sprintf("%s?year=%d", avfanYearlyURL, fallbackYear)
	if fallbackYear != preferredYear {
		htmlSource, _, _, err = s.browser.fetchAVFanHTML(fallbackURL, proxyValue)
		if err != nil {
			return Result{}, notice, err
		}
	}
	result, err := parseAVFanRankingHTML(htmlSource, "annual", fallbackURL, fallbackYear)
	if err != nil {
		return Result{}, notice, err
	}
	result.AvailableYears = normalizeYearList(append(availableYears, result.AvailableYears...))
	return result, notice, nil
}

func (s *Service) fetchOfficialMonthlyRanking(proxyValue string, requestedChannel string) (Result, error) {
	htmlSource, pageURL, title, err := s.browser.fetchOfficialMonthlyHTML(proxyValue)
	if err != nil {
		return Result{}, createRankingError("官方榜单暂时不可用，请确认日本地区代理 / VPN 和网络连接状态。", "official_unavailable")
	}

	if strings.Contains(pageURL, "not-available-in-your-region") {
		return Result{}, createRankingError("当前线路被 DMM/FANZA 限制，请确认已开启日本地区代理或 VPN。", "official_region_blocked")
	}
	if isAgeCheckPage(pageURL, htmlSource, title) {
		return Result{}, createRankingError("当前线路未通过 DMM/FANZA 年龄验证，请确认已开启日本地区代理或 VPN。", "official_age_check_required")
	}
	return parseOfficialMonthlyRankingHTML(htmlSource, requestedChannel)
}

func getErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}

// getAVFanResult owns AVfan fetch/cache/fallback policy and returns a fully
// decorated result shape for the bridge/UI.
func (s *Service) getAVFanResult(context rankingContext) (Result, error) {
	bucketID := "avfan"
	requestedMonthKey := getMonthKey(context.Year, context.Month)

	if context.Mode == "monthly" {
		cached := resolveCachedMonthlyEntry(context.Cache, []string{bucketID}, context.Year, context.Month, requestedMonthKey != "")
		if !context.ForceRefresh && cached != nil && isFresh(cached.Entry, monthlyCacheMaxAgeMS) {
			return decorateMonthlyResult(context.Cache, []string{bucketID}, cached.Entry.Data, context.RequestedChannel, "avfan", true, false, "", "", false), nil
		}

		data, notice, err := s.fetchLatestAVFanMonthlyRanking(context.Proxy)
		if err == nil {
			persistMonthly(&context.Cache, bucketID, data)
			_ = writeCache(context.CacheFilePath, context.Cache)

			requestedKey := getMonthKey(context.Year, context.Month)
			latestKey := getMonthKey(data.PeriodYear, data.PeriodMonth)
			if requestedKey != "" && requestedKey != latestKey {
				bucket := getSourceBucket(context.Cache, bucketID)
				if requestedCached, ok := bucket.MonthlyByPeriod[requestedKey]; ok {
					return decorateMonthlyResult(context.Cache, []string{bucketID}, requestedCached.Data, context.RequestedChannel, "avfan", true, true, mergeNotice(messages.RequestedMonthFallbackToCache, notice), messages.RequestedMonthFallbackToCache, false), nil
				}
				return Result{}, createRankingError(fmt.Sprintf("AVfan 暂未提供 %s 的稳定历史月榜。", requestedKey), "avfan_month_history_missing")
			}

			return decorateMonthlyResult(context.Cache, []string{bucketID}, data, context.RequestedChannel, "avfan", false, false, notice, "", false), nil
		}

		if cached != nil {
			return decorateMonthlyResult(context.Cache, []string{bucketID}, cached.Entry.Data, context.RequestedChannel, "avfan", true, true, notice, getErrorMessage(err), false), nil
		}
		return Result{}, err
	}

	cachedAnnual := resolveCachedAnnualEntry(context.Cache, []string{bucketID}, context.Year, context.Year > 0)
	if !context.ForceRefresh && cachedAnnual != nil && isFresh(cachedAnnual.Entry, yearlyCacheMaxAgeMS) {
		return decorateAnnualResult(context.Cache, []string{bucketID}, cachedAnnual.Entry.Data, context.RequestedChannel, "avfan", true, false, "", "", false), nil
	}

	data, notice, err := s.fetchAVFanAnnualRanking(context.Year, context.Proxy)
	if err == nil {
		persistAnnual(&context.Cache, bucketID, data)
		_ = writeCache(context.CacheFilePath, context.Cache)
		return decorateAnnualResult(context.Cache, []string{bucketID}, data, context.RequestedChannel, "avfan", false, false, notice, "", false), nil
	}

	if cachedAnnual != nil {
		return decorateAnnualResult(context.Cache, []string{bucketID}, cachedAnnual.Entry.Data, context.RequestedChannel, "avfan", true, true, notice, getErrorMessage(err), false), nil
	}
	return Result{}, err
}

// getOfficialResult owns official monthly-source policy and keeps regional/age
// gate handling local to the official source branch.
func (s *Service) getOfficialResult(context rankingContext) (Result, error) {
	bucketID := "official"
	effectiveRequestedChannel := context.RequestedChannel
	if effectiveRequestedChannel == "smart" {
		effectiveRequestedChannel = "fanza"
	}

	requestedKey := getMonthKey(context.Year, context.Month)
	cached := resolveCachedMonthlyEntry(context.Cache, []string{bucketID}, context.Year, context.Month, requestedKey != "")
	if context.Mode != "monthly" {
		return Result{}, createRankingError(messages.OfficialMonthlyOnly, "official_annual_unsupported")
	}

	if !context.ForceRefresh && cached != nil && isFresh(cached.Entry, monthlyCacheMaxAgeMS) {
		return decorateMonthlyResult(context.Cache, []string{bucketID}, cached.Entry.Data, context.RequestedChannel, effectiveRequestedChannel, true, false, "", "", false), nil
	}

	data, err := s.fetchOfficialMonthlyRanking(context.Proxy, effectiveRequestedChannel)
	if err == nil {
		persistMonthly(&context.Cache, bucketID, data)
		_ = writeCache(context.CacheFilePath, context.Cache)
		return decorateMonthlyResult(context.Cache, []string{bucketID}, data, context.RequestedChannel, effectiveRequestedChannel, false, false, "", "", false), nil
	}

	if cached != nil {
		return decorateMonthlyResult(context.Cache, []string{bucketID}, cached.Entry.Data, context.RequestedChannel, effectiveRequestedChannel, true, true, "", getErrorMessage(err), false), nil
	}
	return Result{}, err
}

// getLocalResult serves cache-only history views and deliberately avoids online
// fetch work so "local history" remains deterministic during debugging.
func getLocalResult(context rankingContext) (Result, error) {
	monthlyBuckets := []string{"localHistory", "official", "avfan"}
	annualBuckets := []string{"localHistory", "avfan"}

	if context.Mode == "monthly" {
		requestedKey := getMonthKey(context.Year, context.Month)
		cached := resolveCachedMonthlyEntry(context.Cache, monthlyBuckets, context.Year, context.Month, requestedKey != "")
		if cached == nil && requestedKey == "" {
			cached = resolveCachedMonthlyEntry(context.Cache, monthlyBuckets, context.Year, context.Month, false)
		}
		if cached == nil {
			if requestedKey != "" {
				return Result{}, createRankingError(messages.LocalMonthlyMissing(requestedKey), "local_month_missing")
			}
			return Result{}, createRankingError(messages.LocalCacheMissing, "local_cache_missing")
		}

		notice := "本地历史当前优先展示最近一次 AVfan 缓存。"
		if cached.BucketID == "localHistory" {
			notice = "本地历史当前优先展示你手动写入或导入的榜单。"
		} else if cached.BucketID == "official" {
			notice = "本地历史当前优先展示最近一次官方月榜缓存。"
		}

		return decorateMonthlyResult(context.Cache, monthlyBuckets, cached.Entry.Data, context.RequestedChannel, "local", true, true, notice, "", false), nil
	}

	cachedAnnual := resolveCachedAnnualEntry(context.Cache, annualBuckets, context.Year, context.Year > 0)
	if cachedAnnual == nil && context.Year <= 0 {
		cachedAnnual = resolveCachedAnnualEntry(context.Cache, annualBuckets, context.Year, false)
	}
	if cachedAnnual == nil {
		if context.Year > 0 {
			return Result{}, createRankingError(messages.LocalAnnualMissing(context.Year), "local_annual_missing")
		}
		return Result{}, createRankingError(messages.LocalCacheMissing, "local_cache_missing")
	}

	notice := "本次仅使用本地历史榜单缓存。"
	if cachedAnnual.BucketID == "localHistory" {
		notice = "本次优先展示你手动写入或导入的本地历史榜单。"
	}

	return decorateAnnualResult(context.Cache, annualBuckets, cachedAnnual.Entry.Data, context.RequestedChannel, "local", true, true, notice, "", false), nil
}

// buildSourcePlan is the single fallback-policy entry point. Downstream callers
// should not re-create source ordering on their own.
func buildSourcePlan(requestedChannel string, mode string) []string {
	switch requestedChannel {
	case "local":
		return []string{"local"}
	case "avfan":
		return []string{"avfan", "local"}
	case "fanza", "dmm":
		if mode == "annual" {
			return []string{"avfan", "local"}
		}
		return []string{"official", "avfan", "local"}
	default:
		if mode == "monthly" {
			return []string{"official", "avfan", "local"}
		}
		return []string{"avfan", "local"}
	}
}

// enrichFallbackNotice adds user-facing explanation after the actual source has
// already been resolved, keeping wording separate from source execution.
func enrichFallbackNotice(requestedChannel string, attemptedSource string, result Result) string {
	if requestedChannel == "smart" && attemptedSource == "official" {
		return mergeNotice(messages.OfficialFallbackTo(result.ResolvedSourceLabel), result.Notice)
	}
	if (requestedChannel == "fanza" || requestedChannel == "dmm") && attemptedSource == "official" {
		return mergeNotice(messages.OfficialFallbackTo(result.ResolvedSourceLabel), result.Notice)
	}
	if (requestedChannel == "fanza" || requestedChannel == "dmm") && result.Mode == "annual" {
		return mergeNotice(messages.OfficialAnnualFallbackTo(result.ResolvedSourceLabel), result.Notice)
	}
	return mergeNotice(messages.FallbackTo(result.ResolvedSourceLabel), result.Notice)
}

// trySource routes one normalized source ID to its source-specific policy
// branch.
func (s *Service) trySource(sourceID string, context rankingContext) (Result, error) {
	switch sourceID {
	case "official":
		return s.getOfficialResult(context)
	case "avfan":
		return s.getAVFanResult(context)
	default:
		return getLocalResult(context)
	}
}

// GetActressRankings is the public facade: normalize request, hydrate cache,
// execute fallback plan, and return one stable result contract.
func (s *Service) GetActressRankings(options Options) (Result, error) {
	requestedChannel := normalizeRankingChannel(options.Source)
	mode := "monthly"
	if strings.TrimSpace(options.Mode) == "annual" {
		mode = "annual"
	}
	cache := loadCache(options.CacheFilePath)
	mergeHistoryDirectoriesIntoCache(&cache, options.HistoryDirectories)

	context := rankingContext{
		RequestedChannel: requestedChannel,
		Mode:             mode,
		Year:             options.Year,
		Month:            options.Month,
		ForceRefresh:     options.ForceRefresh,
		Proxy:            strings.TrimSpace(options.Proxy),
		Cache:            cache,
		CacheFilePath:    strings.TrimSpace(options.CacheFilePath),
	}

	failures := make([]struct {
		SourceID string
		Message  string
	}, 0)
	for _, sourceID := range buildSourcePlan(requestedChannel, mode) {
		result, err := s.trySource(sourceID, context)
		if err != nil {
			failures = append(failures, struct {
				SourceID string
				Message  string
			}{
				SourceID: sourceID,
				Message:  getErrorMessage(err),
			})
			continue
		}

		if len(failures) > 0 {
			result.Notice = enrichFallbackNotice(requestedChannel, failures[0].SourceID, result)
			result.FallbackUsed = true
		} else if (requestedChannel == "fanza" || requestedChannel == "dmm") && mode == "annual" {
			result.Notice = mergeNotice(messages.OfficialAnnualFallbackTo(result.ResolvedSourceLabel), result.Notice)
			result.FallbackUsed = true
		} else if requestedChannel == "smart" && sourceID == "official" && strings.TrimSpace(result.Notice) == "" {
			result.Notice = messages.SmartOfficialNotice
		}

		return result, nil
	}

	details := make([]string, 0, len(failures))
	for _, failure := range failures {
		details = append(details, fmt.Sprintf("%s: %s", getChannelLabel(failure.SourceID), failure.Message))
	}
	return Result{}, createRankingError(strings.Join(details, " | "), "ranking_all_sources_failed")
}
