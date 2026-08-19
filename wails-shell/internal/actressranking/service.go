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

	"javflow/internal/common"

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
	avfanMonthlyURL         = "https://av-fan.tokyo/ranking/fanza-dvd-actress-monthly.php"
	avfanYearlyURL          = "https://av-fan.tokyo/ranking/fanza-rental-dvd-actress-top100.php"
	officialMonthlyURL      = "https://www.dmm.co.jp/mono/dvd/-/ranking/=/mode=actress/term=monthly/"
	officialRentalAnnualURL = "https://www.dmm.co.jp/rental/-/ranking/=/article=actress/t=year_%d/"
	monthlyCacheMaxAgeMS    = 12 * 60 * 60 * 1000
	yearlyCacheMaxAgeMS     = 7 * 24 * 60 * 60 * 1000
	cacheVersion            = 2
	defaultOfficialTimeout  = 45 * time.Second
)

var (
	monthlyPeriodPattern  = regexp.MustCompile(`(\d{4})\.(\d{2})`)
	yearPattern           = regexp.MustCompile(`(\d{4})`)
	officialAnnualPattern = regexp.MustCompile(`t=year_(\d{4})`)
	worksCountPattern     = regexp.MustCompile(`商品数\s*[:：]\s*(\d+)`)
	digitsPattern         = regexp.MustCompile(`\d+`)
)

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
	"fanza": {ID: "fanza", Label: "FANZA", CacheBucket: "official"},
	"dmm":   {ID: "dmm", Label: "DMM", CacheBucket: "official"},
	"avfan": {ID: "avfan", Label: "AVfan", CacheBucket: "avfan"},
	"local": {ID: "local", Label: "本地历史", CacheBucket: "local"},
}

// messages centralizes user-facing fallback text so source policy changes do
// not scatter wording edits through fetch branches.
var messages = struct {
	LocalCacheMissing             string
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
		return fmt.Sprintf("官方年榜暂时不可用，已自动切换至 %s。", strings.TrimSpace(targetName))
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

// getBundledInitialCache is the offline-first bootstrap dataset for the Actor
// Atlas. The official lane keeps the exact 20 rows FANZA returned on
// 2026-07-31; the AVfan lane is merged from separately captured, verified
// 100-row snapshots for 2026-03 through 2026-07. Missing months remain
// missing: snapshots are never padded, recycled, or relabeled.
func getBundledInitialCache() cacheFile {
	cache := getCacheSkeleton()
	works := []RankingItem{
		{Rank: 1, ActressName: "瀬戸環奈", ProfileURL: "https://www.dmm.co.jp/mono/dvd/-/list/=/article=actress/id=1099472/", ImageURL: "https://pics.dmm.co.jp/mono/actjpgs/medium/seto_kanna.jpg"},
		{Rank: 2, ActressName: "彩月七緒", ProfileURL: "https://www.dmm.co.jp/mono/dvd/-/list/=/article=actress/id=1089946/", ImageURL: "https://pics.dmm.co.jp/mono/actjpgs/medium/satuki_nao.jpg"},
		{Rank: 3, ActressName: "河北彩花（河北彩伽）", ProfileURL: "https://www.dmm.co.jp/mono/dvd/-/list/=/article=actress/id=1044864/", ImageURL: "https://pics.dmm.co.jp/mono/actjpgs/medium/kawakita_saika.jpg"},
		{Rank: 4, ActressName: "石川澪", ProfileURL: "https://www.dmm.co.jp/mono/dvd/-/list/=/article=actress/id=1072127/", ImageURL: "https://pics.dmm.co.jp/mono/actjpgs/medium/isikawa_mio.jpg"},
		{Rank: 5, ActressName: "小野坂ゆいか", ProfileURL: "https://www.dmm.co.jp/mono/dvd/-/list/=/article=actress/id=1093790/", ImageURL: "https://pics.dmm.co.jp/mono/actjpgs/medium/onosaka_yuika.jpg"},
		{Rank: 6, ActressName: "逢沢みゆ", ProfileURL: "https://www.dmm.co.jp/mono/dvd/-/list/=/article=actress/id=1088602/", ImageURL: "https://pics.dmm.co.jp/mono/actjpgs/medium/aizawa_miyu.jpg"},
		{Rank: 7, ActressName: "田野憂", ProfileURL: "https://www.dmm.co.jp/mono/dvd/-/list/=/article=actress/id=1093791/", ImageURL: "https://pics.dmm.co.jp/mono/actjpgs/medium/tano_yuu.jpg"},
		{Rank: 8, ActressName: "長浜みつり", ProfileURL: "https://www.dmm.co.jp/mono/dvd/-/list/=/article=actress/id=1089578/", ImageURL: "https://pics.dmm.co.jp/mono/actjpgs/medium/nagahama_mituri.jpg"},
		{Rank: 9, ActressName: "神宮寺ナオ", ProfileURL: "https://www.dmm.co.jp/mono/dvd/-/list/=/article=actress/id=1041897/", ImageURL: "https://pics.dmm.co.jp/mono/actjpgs/medium/zinguuzi_nao.jpg"},
		{Rank: 10, ActressName: "森沢かな（飯岡かなこ）", ProfileURL: "https://www.dmm.co.jp/mono/dvd/-/list/=/article=actress/id=1020685/", ImageURL: "https://pics.dmm.co.jp/mono/actjpgs/medium/iioka_kanako.jpg"},
		{Rank: 11, ActressName: "波多野結衣", ProfileURL: "https://www.dmm.co.jp/mono/dvd/-/list/=/article=actress/id=26225/", ImageURL: "https://pics.dmm.co.jp/mono/actjpgs/medium/hatano_yui.jpg"},
		{Rank: 12, ActressName: "愛才りあ", ProfileURL: "https://www.dmm.co.jp/mono/dvd/-/list/=/article=actress/id=1099161/", ImageURL: "https://pics.dmm.co.jp/mono/actjpgs/medium/aise_ria.jpg"},
		{Rank: 13, ActressName: "三澄寧々", ProfileURL: "https://www.dmm.co.jp/mono/dvd/-/list/=/article=actress/id=1104816/", ImageURL: "https://pics.dmm.co.jp/mono/actjpgs/medium/misumi_nene.jpg"},
		{Rank: 14, ActressName: "幸村泉希", ProfileURL: "https://www.dmm.co.jp/mono/dvd/-/list/=/article=actress/id=1104612/", ImageURL: "https://pics.dmm.co.jp/mono/actjpgs/medium/yukimura_ituki.jpg"},
		{Rank: 15, ActressName: "鈴村あいり", ProfileURL: "https://www.dmm.co.jp/mono/dvd/-/list/=/article=actress/id=1019076/", ImageURL: "https://pics.dmm.co.jp/mono/actjpgs/medium/suzumura_airi.jpg"},
		{Rank: 16, ActressName: "北野未奈", ProfileURL: "https://www.dmm.co.jp/mono/dvd/-/list/=/article=actress/id=1068671/", ImageURL: "https://pics.dmm.co.jp/mono/actjpgs/medium/kitano_mina.jpg"},
		{Rank: 17, ActressName: "宮下玲奈", ProfileURL: "https://www.dmm.co.jp/mono/dvd/-/list/=/article=actress/id=1075464/", ImageURL: "https://pics.dmm.co.jp/mono/actjpgs/medium/miyasita_rena2.jpg"},
		{Rank: 18, ActressName: "北岡果林", ProfileURL: "https://www.dmm.co.jp/mono/dvd/-/list/=/article=actress/id=1092427/", ImageURL: "https://pics.dmm.co.jp/mono/actjpgs/medium/kitaoka_karin.jpg"},
		{Rank: 19, ActressName: "涼森れむ", ProfileURL: "https://www.dmm.co.jp/mono/dvd/-/list/=/article=actress/id=1051912/", ImageURL: "https://pics.dmm.co.jp/mono/actjpgs/medium/suzumori_remu.jpg"},
		{Rank: 20, ActressName: "仲村みう", ProfileURL: "https://www.dmm.co.jp/mono/dvd/-/list/=/article=actress/id=20640/", ImageURL: "https://pics.dmm.co.jp/mono/actjpgs/medium/nakamura_miu2.jpg"},
	}
	persistMonthly(&cache, "official", Result{
		Title: "【100位まで】月間AV女優ランキング1～20位 - アダルトDVD・ブルーレイ通販 - FANZA通販", SourceName: "FANZA 官方", SourceURL: officialMonthlyURL,
		Mode: "monthly", PeriodLabel: "2026年07月（FANZA 实际返回 20 位）", PeriodYear: 2026, PeriodMonth: 7,
		Total: len(works), AvailableYears: []int{2026}, AvailableMonths: []int{7}, FetchedAt: "2026-07-31T01:07:19+08:00", Items: works,
	})
	mergeBundledMonthlyHistory(&cache)
	return cache
}

// mergeSourceCache overlays user data onto the bundled bootstrap cache. Empty
// user-data files must not erase the first-run experience; genuine historical
// entries always win over the bundled snapshot for the same period.
func mergeSourceCache(base sourceCache, overlay sourceCache) sourceCache {
	merged := normalizeSourceCache(base)
	normalizedOverlay := normalizeSourceCache(overlay)
	for key, entry := range normalizedOverlay.MonthlyByPeriod {
		merged.MonthlyByPeriod[key] = entry
	}
	for key, entry := range normalizedOverlay.AnnualByYear {
		merged.AnnualByYear[key] = entry
	}
	if normalizedOverlay.MonthlyLatestKey != "" {
		merged.MonthlyLatestKey = normalizedOverlay.MonthlyLatestKey
	}
	merged.AvailableYears = normalizeYearList(append(merged.AvailableYears, normalizedOverlay.AvailableYears...))
	return merged
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
	skeleton := getBundledInitialCache()
	trimmedPath := strings.TrimSpace(filePath)
	if trimmedPath == "" {
		return skeleton
	}

	payload, err := os.ReadFile(trimmedPath)
	if err != nil {
		// Persist the bundled first-run snapshot so the user has a visible,
		// inspectable local cache even before the first online refresh succeeds.
		_ = writeCache(trimmedPath, skeleton)
		return skeleton
	}

	var current cacheFile
	if err := json.Unmarshal(payload, &current); err == nil && len(current.Sources) > 0 {
		current.Version = cacheVersion
		skeleton.Sources["avfan"] = mergeSourceCache(skeleton.Sources["avfan"], current.Sources["avfan"])
		skeleton.Sources["official"] = mergeSourceCache(skeleton.Sources["official"], current.Sources["official"])
		skeleton.Sources["localHistory"] = mergeSourceCache(skeleton.Sources["localHistory"], current.Sources["localHistory"])
		// Empty cache files were produced by earlier builds. Rewrite them once
		// with the merged snapshot instead of repeatedly reconstructing it only
		// in memory on every application launch.
		if len(listMonthlyPeriods(current, []string{"official"})) == 0 {
			_ = writeCache(trimmedPath, skeleton)
		}
		return skeleton
	}

	var legacy sourceCache
	if err := json.Unmarshal(payload, &legacy); err == nil {
		skeleton.Sources["avfan"] = normalizeSourceCache(legacy)
		return skeleton
	}
	// Preserve the unreadable payload for support instead of silently deleting
	// evidence of an interrupted or manually edited cache file.
	corruptPath := trimmedPath + ".corrupt-" + time.Now().Format("20060102-150405")
	_ = os.Rename(trimmedPath, corruptPath)
	_ = writeCache(trimmedPath, skeleton)
	return skeleton
}

func writeCache(filePath string, cache cacheFile) error {
	trimmedPath := strings.TrimSpace(filePath)
	if trimmedPath == "" {
		return nil
	}
	payload, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return common.WriteFileAtomic(trimmedPath, payload, 0o644)
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
			if len(match) != 3 || !hasUsableMonthlyData(entry.Data) {
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

// hasUsableMonthlyData deliberately uses the returned ranking entries as the
// cache validity contract. Older cache files did not always persist the page
// title, but they still contain a complete, immediately displayable ranking.
// Treating a missing cosmetic title as a cache miss caused an unnecessary
// browser request and left the Actor Atlas blank while the request timed out.
func hasUsableMonthlyData(data Result) bool {
	if data.PeriodYear <= 0 || data.PeriodMonth < 1 || data.PeriodMonth > 12 {
		return false
	}
	return len(data.Items) > 0
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

// rankingAvailabilityBuckets defines which already-cached periods may be
// offered by the selected channel. Smart mode is intentionally the union of
// the real official, AVfan, and imported local records; otherwise a successful
// official response could hide an older month that was captured by another
// source. This only exposes records that exist in cache, never guessed dates.
func rankingAvailabilityBuckets(requestedChannel string, resolvedChannel string, bucketIDs []string) []string {
	if normalizeRankingChannel(requestedChannel) != "smart" && normalizeRankingChannel(resolvedChannel) != "local" {
		return bucketIDs
	}
	merged := append([]string(nil), bucketIDs...)
	for _, bucketID := range []string{"official", "avfan", "localHistory"} {
		seen := false
		for _, existing := range merged {
			if existing == bucketID {
				seen = true
				break
			}
		}
		if !seen {
			merged = append(merged, bucketID)
		}
	}
	return merged
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

// getAnnualQueryYears only exposes years confirmed by a source response or
// cache. Do not add the current calendar year: an unpublished annual ranking
// must not appear as a selectable option.
func getAnnualQueryYears(cache cacheFile, bucketIDs []string) []int {
	return getAnnualAvailability(cache, bucketIDs)
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
	availabilityBuckets := rankingAvailabilityBuckets(requestedChannel, resolvedChannel, bucketIDs)
	availableYears, availableMonths := getMonthlyAvailability(cache, availabilityBuckets, data.PeriodYear)
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
	result.AvailableYears = getAnnualQueryYears(cache, bucketIDs)
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

// commitRankingCache serializes only the final read-merge-write operation.
// Source requests remain concurrent, while a late-finishing request cannot
// overwrite a period another request committed in the meantime.
func (s *Service) commitRankingCache(observed cacheFile, filePath, bucketID string, data Result) cacheFile {
	if strings.TrimSpace(filePath) == "" {
		if data.Mode == "annual" {
			persistAnnual(&observed, bucketID, data)
		} else {
			persistMonthly(&observed, bucketID, data)
		}
		return observed
	}

	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	committed := loadCache(filePath)
	// Imported history can be present only in the request-local cache. Merge
	// that lane without allowing an older request snapshot to replace newer
	// official/AVfan entries that another refresh already committed.
	committed.Sources["localHistory"] = mergeSourceCache(
		committed.Sources["localHistory"],
		observed.Sources["localHistory"],
	)
	if data.Mode == "annual" {
		persistAnnual(&committed, bucketID, data)
	} else {
		persistMonthly(&committed, bucketID, data)
	}
	_ = writeCache(filePath, committed)
	return committed
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

// parseAVFanAvailableYears accepts AVfan's nested year navigation. The site
// currently renders links as `div.ranking-year-link > ul > li > a`, so the
// year-link class is not necessarily the anchor's direct parent.
func parseAVFanAvailableYears(root *html.Node) []int {
	years := make([]int, 0)
	for _, yearLink := range findAll(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode && strings.EqualFold(node.Data, "a")
	}) {
		isRankingYearLink := false
		for parent := yearLink.Parent; parent != nil; parent = parent.Parent {
			if hasAllClasses(parent, "ranking-year-link") {
				isRankingYearLink = true
				break
			}
		}
		if isRankingYearLink {
			years = append(years, toIntValue(nodeText(yearLink)))
		}
	}
	return normalizeYearList(years)
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

	yearValues := parseAVFanAvailableYears(root)
	if len(items) == 0 {
		return Result{}, createRankingError("未从 AVfan 榜单页解析到有效内容。", "avfan_parse_empty")
	}

	return Result{
		Mode:           mode,
		SourceName:     "AVfan 第三方参考",
		SourceURL:      sourceURL,
		Title:          buildAVFanTitle(mode, periodYear, periodMonth),
		PeriodLabel:    periodLabel,
		PeriodYear:     periodYear,
		PeriodMonth:    periodMonth,
		Total:          len(items),
		AvailableYears: yearValues,
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

// isAVFanVerificationPage keeps verification failures distinct from empty
// ranking pages. Cloudflare's challenge markup can otherwise look like a
// successful HTML response and be misleadingly reported as "no years found".
func isAVFanVerificationPage(htmlSource string, title string) bool {
	normalized := strings.ToLower(strings.Join([]string{htmlSource, title}, "\n"))
	return strings.Contains(normalized, "one moment, please") ||
		strings.Contains(normalized, "just a moment") ||
		strings.Contains(normalized, "cf-chl") ||
		strings.Contains(normalized, "challenge-platform")
}

func parseOfficialMonthlyRankingHTML(htmlSource string, requestedChannel string) (Result, error) {
	year, month := getCurrentJapanYearMonth()
	return parseOfficialRankingHTML(htmlSource, requestedChannel, "monthly", officialMonthlyURL, year, month)
}

func parseOfficialAnnualRentalRankingHTML(htmlSource string, requestedChannel string, sourceURL string, year int) (Result, error) {
	return parseOfficialRankingHTML(htmlSource, requestedChannel, "annual", sourceURL, year, 0)
}

// parseOfficialRankingHTML normalizes DMM/FANZA's shared rank-row markup. The
// caller supplies the period because the historical rental pages have a
// generic document title that does not identify the requested year.
func parseOfficialRankingHTML(htmlSource string, requestedChannel string, mode string, sourceURL string, year int, month int) (Result, error) {
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
	rows := findAll(root, func(node *html.Node) bool {
		if node.Type != html.ElementNode ||
			(!strings.EqualFold(node.Data, "td") && !(strings.EqualFold(node.Data, "tr") && hasClass(node, "bd-b"))) {
			return false
		}
		// DMM marks most cells with bd-b, but the final pair on each page
		// intentionally has an empty class. Rank and data are the stable row
		// contract shared by both variants.
		return findFirst(node, func(child *html.Node) bool {
			return child.Type == html.ElementNode && hasClass(child, "rank")
		}) != nil && findFirst(node, func(child *html.Node) bool {
			return child.Type == html.ElementNode && hasClass(child, "data")
		}) != nil
	})
	itemsByRank := map[int]RankingItem{}
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

		if _, exists := itemsByRank[rankValue]; exists {
			continue
		}
		itemsByRank[rankValue] = RankingItem{
			Rank:        rankValue,
			ActressName: actressName,
			ProfileURL:  absoluteURL(getAttribute(actressAnchor, "href"), sourceURL),
			ImageURL:    absoluteURL(getAttribute(imageNode, "src"), sourceURL),
			LatestTitle: strings.TrimSpace(nodeText(latestWorkLink)),
			LatestURL:   absoluteURL(getAttribute(latestWorkLink, "href"), sourceURL),
			WorksCount:  worksCount,
		}
	}
	items := make([]RankingItem, 0, len(itemsByRank))
	for _, item := range itemsByRank {
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Rank < items[j].Rank
	})
	if len(items) == 0 {
		return Result{}, createRankingError("未从 DMM/FANZA 官方月榜解析到女优列表。", "official_parse_empty")
	}

	periodLabel := fmt.Sprintf("%d年%02d月（官方当前月榜）", year, month)
	availableYears := []int{year}
	availableMonths := []int{month}
	if mode == "annual" {
		periodLabel = fmt.Sprintf("%d年（FANZA 租赁年榜）", year)
		availableYears = parseOfficialAnnualAvailableYears(root)
		availableYears = normalizeYearList(append(availableYears, year))
		availableMonths = []int{}
	}

	return Result{
		Mode:            mode,
		SourceName:      getOfficialSourceName(requestedChannel),
		SourceURL:       sourceURL,
		Title:           pageTitle,
		PeriodLabel:     periodLabel,
		PeriodYear:      year,
		PeriodMonth:     month,
		Total:           len(items),
		AvailableYears:  availableYears,
		AvailableMonths: availableMonths,
		FetchedAt:       time.Now().Format(time.RFC3339),
		Items:           items,
	}, nil
}

// parseOfficialAnnualAvailableYears reads DMM rental's verified historical
// year links. Only years exposed by the source page are offered to the UI.
func parseOfficialAnnualAvailableYears(root *html.Node) []int {
	years := make([]int, 0)
	for _, anchor := range findAll(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode &&
			strings.EqualFold(node.Data, "a") &&
			strings.Contains(getAttribute(node, "href"), "t=year_")
	}) {
		matches := officialAnnualPattern.FindStringSubmatch(getAttribute(anchor, "href"))
		if len(matches) != 2 {
			continue
		}
		if year, err := strconv.Atoi(matches[1]); err == nil {
			years = append(years, year)
		}
	}
	return normalizeYearList(years)
}

func (s *Service) fetchLatestAVFanMonthlyRanking(proxyValue string) (Result, string, error) {
	htmlSource, sourceURL, pageTitle, err := s.browser.fetchAVFanHTML(avfanMonthlyURL, proxyValue)
	if err != nil {
		if strings.TrimSpace(proxyValue) != "" && isBrowserProxyError(err) {
			htmlSource, sourceURL, pageTitle, err = s.browser.fetchAVFanHTML(avfanMonthlyURL, "")
			if err == nil {
				if isAVFanVerificationPage(htmlSource, pageTitle) {
					return Result{}, "", createRankingError("AVfan 当前返回 Cloudflare 验证页，未获得真实榜单。", "avfan_verification_required")
				}
				result, parseErr := parseAVFanRankingHTML(htmlSource, "monthly", sourceURL, 0)
				if parseErr != nil {
					return Result{}, "", parseErr
				}
				return result, messages.AVFanDirectRetryNotice, nil
			}
		}
		return Result{}, "", err
	}
	if isAVFanVerificationPage(htmlSource, pageTitle) {
		return Result{}, "", createRankingError("AVfan 当前返回 Cloudflare 验证页，未获得真实榜单。", "avfan_verification_required")
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
	htmlSource, _, pageTitle, err := s.browser.fetchAVFanHTML(landingURL, proxyValue)
	notice := ""
	if err != nil {
		if strings.TrimSpace(proxyValue) != "" && isBrowserProxyError(err) {
			htmlSource, _, pageTitle, err = s.browser.fetchAVFanHTML(landingURL, "")
			if err == nil {
				notice = messages.AVFanDirectRetryNotice
			}
		}
		if err != nil {
			return Result{}, "", err
		}
	}
	if isAVFanVerificationPage(htmlSource, pageTitle) {
		return Result{}, notice, createRankingError("AVfan 当前返回 Cloudflare 验证页，未获得真实年榜。", "avfan_verification_required")
	}

	root, parseErr := parseHTMLDocument(htmlSource)
	if parseErr != nil {
		return Result{}, notice, parseErr
	}
	availableYears := parseAVFanAvailableYears(root)

	initial, err := parseAVFanRankingHTML(htmlSource, "annual", landingURL, preferredYear)
	if err == nil {
		initial.AvailableYears = normalizeYearList(append(availableYears, initial.AvailableYears...))
		return initial, notice, nil
	}
	// An explicitly selected year must never be silently replaced by another
	// year's ranking.  AVfan keeps historical year links even when a year has
	// no published rows, so falling through here would label (for example)
	// 2025 data while actually returning 2024 entries.
	if year > 0 {
		return Result{}, notice, createRankingError(
			fmt.Sprintf("%d 年榜当前没有可用的真实排名条目。", preferredYear),
			"avfan_annual_empty",
		)
	}

	fallbackYears := make([]int, 0)
	for _, value := range availableYears {
		// The preferred page was already parsed and found empty. For an
		// automatic request, move to an older published year instead of
		// retrying the same empty page.
		if value < preferredYear {
			fallbackYears = append(fallbackYears, value)
		}
	}
	// Chromium's rendered AVfan page can omit the visible year navigation.
	// Keep one bounded adjacent-year probe so an automatic request can still
	// reach a published historical ranking (2025 currently has no rows while
	// 2024 does) without fabricating a year in the UI.
	if preferredYear > 1 {
		fallbackYears = append(fallbackYears, preferredYear-1)
	}
	fallbackYears = normalizeYearList(fallbackYears)
	if len(fallbackYears) == 0 {
		return Result{}, notice, createRankingError("未找到可用的 AVfan 年榜年份。", "avfan_annual_year_missing")
	}

	for _, fallbackYear := range fallbackYears {
		fallbackURL := fmt.Sprintf("%s?year=%d", avfanYearlyURL, fallbackYear)
		fallbackHTML, _, fallbackTitle, fallbackErr := s.browser.fetchAVFanHTML(fallbackURL, proxyValue)
		if fallbackErr != nil {
			continue
		}
		if isAVFanVerificationPage(fallbackHTML, fallbackTitle) {
			continue
		}
		result, parseErr := parseAVFanRankingHTML(fallbackHTML, "annual", fallbackURL, fallbackYear)
		if parseErr != nil {
			continue
		}
		result.AvailableYears = normalizeYearList(append(availableYears, result.AvailableYears...))
		return result, notice, nil
	}
	return Result{}, notice, createRankingError("未找到可用的 AVfan 年榜年份。", "avfan_annual_year_missing")
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

func officialRentalAnnualPageURL(year int, page int) string {
	baseURL := fmt.Sprintf(officialRentalAnnualURL, year)
	if page <= 1 {
		return baseURL
	}
	return fmt.Sprintf("https://www.dmm.co.jp/rental/-/ranking/=/article=actress/t=year_%d/page=%d/", year, page)
}

// officialRentalAnnualCandidateYears lists the verified DMM rental history
// window. They are query choices only; a selection is still displayed only
// after all 100 real rows have been fetched and validated.
func officialRentalAnnualCandidateYears(year int) []int {
	if year <= 0 {
		year = time.Now().Year() - 1
	}
	if year == 2025 {
		return []int{2025, 2024}
	}
	return []int{year}
}

// fetchOfficialRentalAnnualRanking retrieves all five verified DMM rental
// result pages. Every page is required: caching a partial Top 100 as a full
// annual ranking would make the UI misleading.
func (s *Service) fetchOfficialRentalAnnualRanking(year int, proxyValue string, requestedChannel string) (Result, error) {
	if year <= 0 {
		year = time.Now().Year() - 1
	}

	itemsByRank := map[int]RankingItem{}
	availableYears := make([]int, 0)
	var first Result
	for page := 1; page <= 5; page++ {
		targetURL := officialRentalAnnualPageURL(year, page)
		htmlSource, pageURL, title, err := s.browser.fetchOfficialRankingHTML(targetURL, proxyValue)
		if err != nil {
			return Result{}, createRankingError("官方租赁年榜暂时不可用，请确认日本地区代理或 VPN 连接。", "official_rental_annual_unavailable")
		}
		if strings.Contains(pageURL, "not-available-in-your-region") {
			return Result{}, createRankingError("当前线路被 DMM/FANZA 限制，请确认已开启日本地区代理或 VPN。", "official_region_blocked")
		}
		if isAgeCheckPage(pageURL, htmlSource, title) {
			return Result{}, createRankingError("当前线路未通过 DMM/FANZA 年龄验证，请确认已开启日本地区代理或 VPN。", "official_age_check_required")
		}

		parsed, err := parseOfficialAnnualRentalRankingHTML(htmlSource, requestedChannel, targetURL, year)
		if err != nil {
			return Result{}, err
		}
		if page == 1 {
			first = parsed
		}
		availableYears = append(availableYears, parsed.AvailableYears...)
		for _, item := range parsed.Items {
			itemsByRank[item.Rank] = item
		}
	}

	items := make([]RankingItem, 0, len(itemsByRank))
	for _, item := range itemsByRank {
		items = append(items, item)
	}
	sort.Slice(items, func(left int, right int) bool {
		return items[left].Rank < items[right].Rank
	})
	if len(items) != 100 {
		return Result{}, createRankingError(fmt.Sprintf("官方租赁年榜 %d 年仅解析到 %d/100 位，未保存不完整榜单。", year, len(items)), "official_rental_annual_incomplete")
	}

	first.Mode = "annual"
	first.PeriodYear = year
	first.PeriodMonth = 0
	first.PeriodLabel = fmt.Sprintf("%d年（FANZA 租赁年榜）", year)
	first.SourceURL = officialRentalAnnualPageURL(year, 1)
	first.AvailableYears = normalizeYearList(append(availableYears, officialRentalAnnualCandidateYears(year)...))
	first.AvailableMonths = []int{}
	first.Total = len(items)
	first.Items = items
	first.FetchedAt = time.Now().Format(time.RFC3339)
	return first, nil
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
		if !context.ForceRefresh && cached != nil {
			stale := !isFresh(cached.Entry, monthlyCacheMaxAgeMS)
			notice := ""
			if stale {
				notice = "已立即显示本地榜单缓存；点击“刷新榜单”后才会联网更新。"
			}
			return decorateMonthlyResult(context.Cache, []string{bucketID}, cached.Entry.Data, context.RequestedChannel, "avfan", true, stale, notice, "", false), nil
		}

		data, notice, err := s.fetchLatestAVFanMonthlyRanking(context.Proxy)
		if err == nil {
			context.Cache = s.commitRankingCache(context.Cache, context.CacheFilePath, bucketID, data)

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
	if !context.ForceRefresh && cachedAnnual != nil {
		stale := !isFresh(cachedAnnual.Entry, yearlyCacheMaxAgeMS)
		notice := ""
		if stale {
			notice = "已立即显示本地榜单缓存；点击“刷新榜单”后才会联网更新。"
		}
		return decorateAnnualResult(context.Cache, []string{bucketID}, cachedAnnual.Entry.Data, context.RequestedChannel, "avfan", true, stale, notice, "", false), nil
	}

	data, notice, err := s.fetchAVFanAnnualRanking(context.Year, context.Proxy)
	if err == nil {
		context.Cache = s.commitRankingCache(context.Cache, context.CacheFilePath, bucketID, data)
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

	if context.Mode == "annual" {
		cachedAnnual := resolveCachedAnnualEntry(context.Cache, []string{bucketID}, context.Year, context.Year > 0)
		if !context.ForceRefresh && cachedAnnual != nil {
			stale := !isFresh(cachedAnnual.Entry, yearlyCacheMaxAgeMS)
			notice := ""
			if stale {
				notice = "已立即显示本地年榜缓存；点击刷新榜单后才会联网更新。"
			}
			return decorateAnnualResult(context.Cache, []string{bucketID}, cachedAnnual.Entry.Data, context.RequestedChannel, effectiveRequestedChannel, true, stale, notice, "", false), nil
		}

		data, err := s.fetchOfficialRentalAnnualRanking(context.Year, context.Proxy, effectiveRequestedChannel)
		if err == nil {
			context.Cache = s.commitRankingCache(context.Cache, context.CacheFilePath, bucketID, data)
			return decorateAnnualResult(context.Cache, []string{bucketID}, data, context.RequestedChannel, effectiveRequestedChannel, false, false, "", "", false), nil
		}
		if cachedAnnual != nil {
			return decorateAnnualResult(context.Cache, []string{bucketID}, cachedAnnual.Entry.Data, context.RequestedChannel, effectiveRequestedChannel, true, true, "", getErrorMessage(err), false), nil
		}
		return Result{}, err
	}

	requestedKey := getMonthKey(context.Year, context.Month)
	cached := resolveCachedMonthlyEntry(context.Cache, []string{bucketID}, context.Year, context.Month, requestedKey != "")

	if !context.ForceRefresh && cached != nil {
		stale := !isFresh(cached.Entry, monthlyCacheMaxAgeMS)
		notice := ""
		if stale {
			notice = "已立即显示本地榜单缓存；点击“刷新榜单”后才会联网更新。"
		}
		return decorateMonthlyResult(context.Cache, []string{bucketID}, cached.Entry.Data, context.RequestedChannel, effectiveRequestedChannel, true, stale, notice, "", false), nil
	}

	data, err := s.fetchOfficialMonthlyRanking(context.Proxy, effectiveRequestedChannel)
	if err == nil {
		context.Cache = s.commitRankingCache(context.Cache, context.CacheFilePath, bucketID, data)
		latestKey := getMonthKey(data.PeriodYear, data.PeriodMonth)
		if requestedKey != "" && requestedKey != latestKey {
			// The official endpoint exposes the current month only. Returning it
			// for an explicitly selected historical month would silently show the
			// wrong ranking and prevent the AVfan/local fallback from running.
			return Result{}, createRankingError(fmt.Sprintf("FANZA 官方暂未提供 %s 的历史月榜。", requestedKey), "official_month_history_missing")
		}
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
			return []string{"official", "avfan", "local"}
		}
		return []string{"official", "avfan", "local"}
	default:
		if mode == "monthly" {
			return []string{"official", "avfan", "local"}
		}
		return []string{"official", "avfan", "local"}
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
	// A selected historical month should render an exact verified snapshot
	// immediately. Without this fast path smart mode would first wait for the
	// official current-month endpoint, even when an AVfan snapshot for the
	// requested period is already bundled locally.
	if requestedChannel == "smart" && mode == "monthly" && !context.ForceRefresh && getMonthKey(context.Year, context.Month) != "" {
		buckets := []string{"official", "avfan", "localHistory"}
		if cached := resolveCachedMonthlyEntry(cache, buckets, context.Year, context.Month, true); cached != nil {
			resolvedChannel := "fanza"
			if cached.BucketID == "avfan" {
				resolvedChannel = "avfan"
			} else if cached.BucketID == "localHistory" {
				resolvedChannel = "local"
			}
			return decorateMonthlyResult(
				cache,
				buckets,
				cached.Entry.Data,
				requestedChannel,
				resolvedChannel,
				true,
				!isFresh(cached.Entry, monthlyCacheMaxAgeMS),
				"已立即显示该月份的真实本地快照；点击“刷新榜单”可尝试联网更新。",
				"",
				cached.BucketID != "official",
			), nil
		}
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
