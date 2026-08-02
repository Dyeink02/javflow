// Package crawlquality summarizes crawl outputs, logs, and quality diagnostics
// for operator review.
//
// Maintenance boundary:
// - read persisted crawl artifacts and logs
// - derive operator-facing completion/gap/consistency summaries
// - stay read-only and never drive crawl execution
//
// Ownership summary:
// 1) expose the read-only crawl quality facade
// 2) keep artifact/log parsing and summary derivation in one analysis layer
// 3) provide operator-facing review outputs without mutating crawl execution
//
// File map for maintainers:
// 1) artifact/log filename defaults and parse regexes
// 2) quality summary DTOs and facade-level entrypoints
// 3) persisted artifact/log scan and parse helpers
// 4) operator-facing summary/report assembly helpers
package crawlquality

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"javflow/internal/common"
	"javflow/internal/contracts/crawlartifact"
	"javflow/internal/crawlexecution"
	"javflow/internal/crawlidentity"
)

const (
	defaultFilmDataName   = crawlartifact.CrawlFilmDataFile
	defaultMagnetName     = crawlartifact.DefaultMagnetTxt
	defaultLogDirName     = crawlartifact.DefaultLogDirName
	defaultLatestLog      = crawlartifact.DefaultLatestLogTxt
	defaultUnfinishedName = crawlartifact.DefaultUnfinishedTxt
	defaultReportName     = crawlartifact.DefaultQualitySummaryTxt
)

var (
	filmCodePattern          = regexp.MustCompile(`(?i)([A-Z@]{1,12})[-_ ]?(\d{1,8})([A-Z]*)`)
	limitPattern             = regexp.MustCompile(`(?i)"limit"\s*:\s*(\d+)`)
	totalPagesPattern        = regexp.MustCompile(`(?i)"totalPages"\s*:\s*(\d+)`)
	durationPattern          = regexp.MustCompile(`(?i)(\d+)\s*(?:秒|s|sec(?:ond)?s?)`)
	completedProgressPattern = regexp.MustCompile(`(?i)(\d+)\s*(?:条|items?)`)
)

var (
	runLogPatterns = []string{
		"运行日志-*.txt",
		"log-*.txt",
	}

	startedTokens   = []string{"开始时间", "started at"}
	sourceURLTokens = []string{"起始地址", "source url"}
	runPlanTokens   = []string{"运行方案", "run plan"}
	targetTokens    = []string{"目标数量", "target limit"}
	totalPageTokens = []string{"总页数", "total pages"}

	warningTokens = []string{"警告:", "warning:"}
	errorTokens   = []string{"错误:", "error:"}
	retryTokens   = []string{"正在重试", "retrying"}

	magnetSuccessTokens          = []string{"成功获取磁力链接", "magnet fetched"}
	largestMagnetTokens          = []string{"返回最大磁力链接", "largest magnet"}
	secondValidationPassedTokens = []string{
		"结果二次校验通过",
		"结果二次校验完成",
		"已二次校验完成",
		"second validation passed",
	}
	secondValidationFailedTokens = []string{
		"结果二次校验失败",
		"second validation failed",
	}
	completedTokens = []string{
		"抓取任务已完成",
		"抓取任务完成",
		"crawl completed",
	}
	stopRequestedTokens = []string{
		"正在终止",
		"已发送终止指令",
		"已收到终止指令",
		"user requested stop",
	}
	stoppedTokens = []string{
		"抓取任务已终止",
		"任务已终止",
		"stopped",
	}
	validationPrefixTokens = []string{"磁力内容校验", "validation"}
	timeoutTokens          = []string{"timeout", "超时", "未完成"}
	cooldownTokens         = []string{"冷却", "cooldown"}
	ajaxFallbackTokens     = []string{"回退", "备用地址", "fallback"}
)

type Options struct {
	OutputDir      string `json:"outputDir"`
	LogDir         string `json:"logDir"`
	LatestLogPath  string `json:"latestLogPath"`
	FilmDataPath   string `json:"filmDataPath"`
	MagnetPath     string `json:"magnetPath"`
	UnfinishedPath string `json:"unfinishedPath"`
	WriteReport    bool   `json:"writeReport"`
}

type Issue struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

type Summary struct {
	Available bool `json:"available"`

	Status      string `json:"status"`
	StatusText  string `json:"statusText"`
	NoticeLevel string `json:"noticeLevel"`
	SummaryLine string `json:"summaryLine"`

	TopSuggestionLines []string `json:"topSuggestionLines,omitempty"`

	StopRequested            bool `json:"stopRequested"`
	Stopped                  bool `json:"stopped"`
	StoppedWithoutOutput     bool `json:"stoppedWithoutOutput"`
	StoppedWithPartialOutput bool `json:"stoppedWithPartialOutput"`

	GeneratedAt    string `json:"generatedAt"`
	OutputDir      string `json:"outputDir"`
	LogDir         string `json:"logDir"`
	LatestLogPath  string `json:"latestLogPath"`
	FilmDataPath   string `json:"filmDataPath"`
	MagnetPath     string `json:"magnetPath"`
	UnfinishedPath string `json:"unfinishedPath"`
	ReportPath     string `json:"reportPath"`

	SourceURL   string `json:"sourceUrl"`
	RunPlan     string `json:"runPlan"`
	StartedAt   string `json:"startedAt"`
	CompletedAt string `json:"completedAt"`

	TargetLimit     int `json:"targetLimit"`
	TotalPages      int `json:"totalPages"`
	DurationSeconds int `json:"durationSeconds"`

	MagnetTotal     int `json:"magnetTotal"`
	MagnetUnique    int `json:"magnetUnique"`
	MagnetDuplicate int `json:"magnetDuplicate"`

	FilmRecordTotal                  int      `json:"filmRecordTotal"`
	FilmRecordWithMagnet             int      `json:"filmRecordWithMagnet"`
	FilmRecordFilteredByActressCount int      `json:"filmRecordFilteredByActressCount"`
	ExpectedMagnetTxtTotal           int      `json:"expectedMagnetTxtTotal"`
	FilmCodeUnique                   int      `json:"filmCodeUnique"`
	FilteredItemIDs                  []string `json:"filteredItemIds,omitempty"`

	CrawledUniqueCount         int      `json:"crawledUniqueCount"`
	SiteRawEntryCount          int      `json:"siteRawEntryCount"`
	SiteUniqueEntryCount       int      `json:"siteUniqueEntryCount"`
	SiteDuplicateEntryCount    int      `json:"siteDuplicateEntryCount"`
	RawTargetShortfallCount    int      `json:"rawTargetShortfallCount"`
	UniqueTargetShortfallCount int      `json:"uniqueTargetShortfallCount"`
	DuplicateItemIDs           []string `json:"duplicateItemIds,omitempty"`
	UnfinishedItemIDs          []string `json:"unfinishedItemIds,omitempty"`

	MagnetSuccessLogCount   int `json:"magnetSuccessLogCount"`
	LargestMagnetLogCount   int `json:"largestMagnetLogCount"`
	WarningCount            int `json:"warningCount"`
	ErrorCount              int `json:"errorCount"`
	HTTP429Count            int `json:"http429Count"`
	CloudflareLineCount     int `json:"cloudflareLineCount"`
	AjaxFallbackCount       int `json:"ajaxFallbackCount"`
	AjaxDomainCooldownCount int `json:"ajaxDomainCooldownCount"`
	ValidationTimeoutCount  int `json:"validationTimeoutCount"`
	ValidationCooldownCount int `json:"validationCooldownCount"`
	RetryCount              int `json:"retryCount"`

	SecondValidationPassed bool `json:"secondValidationPassed"`
	SecondValidationFailed bool `json:"secondValidationFailed"`
	Completed              bool `json:"completed"`

	OutputCompletionRate      float64 `json:"outputCompletionRate"`
	FilmMagnetConsistencyRate float64 `json:"filmMagnetConsistencyRate"`

	Issues []Issue `json:"issues"`
}

// Service reads existing crawl artifacts and produces a review-friendly summary
// of what happened. It is intentionally read-only: this layer explains crawl
// results but never drives crawl execution.
//
// Practical split:
//   - runner decides what happened during execution
//   - crawlquality explains what persisted outputs/logs now say happened
//   - bridge/UI consume the resulting review snapshot
//   - if counts look wrong, compare persisted filmData/log files here before
//     touching runner execution code
type Service struct{}

type filmDataMetrics struct {
	total                  int
	withMagnet             int
	filteredByActressCount int
	uniqueCodes            int
	filteredItemIDs        []string
}

type logMetrics struct {
	sourceURL               string
	runPlan                 string
	startedAt               string
	completedAt             string
	targetLimit             int
	totalPages              int
	durationSeconds         int
	magnetSuccessLogCount   int
	largestMagnetLogCount   int
	warningCount            int
	errorCount              int
	http429Count            int
	cloudflareLineCount     int
	ajaxFallbackCount       int
	ajaxDomainCooldownCount int
	validationTimeoutCount  int
	validationCooldownCount int
	retryCount              int
	secondValidationPassed  bool
	secondValidationFailed  bool
	completed               bool
	stopRequested           bool
	stopped                 bool
	completedProgressCount  int
}

type unfinishedMetrics struct {
	targetCount            int
	completedCount         int
	rawEntryCount          int
	uniqueEntryCount       int
	duplicateEntryCount    int
	duplicateItemIDs       []string
	unfinishedItemIDs      []string
	lowConfidencePageCount int
}

func NewService() *Service {
	return &Service{}
}

func DefaultReportPath(outputDir string) string {
	outputDir = cleanPath(outputDir)
	if outputDir == "" {
		return ""
	}
	return crawlartifact.DefaultQualitySummaryPath(outputDir)
}

func cleanPath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if absolute, err := filepath.Abs(trimmed); err == nil {
		return absolute
	}
	return filepath.Clean(trimmed)
}

// Summarize reconciles filmData, magnet TXT output, unfinished reports, and
// latest logs into one operator-facing snapshot.
func (s *Service) Summarize(options Options) (Summary, error) {
	// Summarize 只负责读现有输出，不触碰抓取执行链路。
	// 这里把 filmData、磁力文件、未完成清单和最新日志拼成同一份复盘视图。
	normalized := normalizeOptions(options)
	if normalized.OutputDir == "" && normalized.LatestLogPath == "" && normalized.FilmDataPath == "" && normalized.MagnetPath == "" {
		return Summary{}, fmt.Errorf("请先提供爬虫输出目录或日志路径")
	}

	summary := Summary{
		GeneratedAt:    time.Now().Format(time.RFC3339),
		OutputDir:      normalized.OutputDir,
		LogDir:         normalized.LogDir,
		LatestLogPath:  normalized.LatestLogPath,
		FilmDataPath:   normalized.FilmDataPath,
		MagnetPath:     normalized.MagnetPath,
		UnfinishedPath: normalized.UnfinishedPath,
	}

	// Summary assembly is intentionally layered:
	// 1) raw artifact metrics
	// 2) derived gap/consistency math
	// 3) status/issues labels
	// 4) UI/report presentation text
	if total, unique, duplicate, err := readMagnetMetrics(normalized.MagnetPath); err == nil {
		summary.MagnetTotal = total
		summary.MagnetUnique = unique
		summary.MagnetDuplicate = duplicate
	}

	if metrics, err := readFilmDataMetrics(normalized.FilmDataPath); err == nil {
		summary.FilmRecordTotal = metrics.total
		summary.FilmRecordWithMagnet = metrics.withMagnet
		summary.FilmRecordFilteredByActressCount = metrics.filteredByActressCount
		summary.FilmCodeUnique = metrics.uniqueCodes
		summary.FilteredItemIDs = append([]string(nil), metrics.filteredItemIDs...)
		summary.CrawledUniqueCount = metrics.withMagnet
	}

	if metrics, err := readUnfinishedMetrics(normalized.UnfinishedPath); err == nil {
		summary.TargetLimit = maxInt(summary.TargetLimit, metrics.targetCount)
		if metrics.completedCount > 0 {
			summary.CrawledUniqueCount = metrics.completedCount
		}
		summary.SiteRawEntryCount = metrics.rawEntryCount
		if metrics.uniqueEntryCount > 0 {
			summary.SiteUniqueEntryCount = metrics.uniqueEntryCount
		}
		summary.SiteDuplicateEntryCount = metrics.duplicateEntryCount
		summary.DuplicateItemIDs = append([]string(nil), metrics.duplicateItemIDs...)
		summary.UnfinishedItemIDs = append([]string(nil), metrics.unfinishedItemIDs...)
	}

	if metrics, err := parseLogMetrics(normalized.LatestLogPath); err == nil {
		summary.SourceURL = metrics.sourceURL
		summary.RunPlan = metrics.runPlan
		summary.StartedAt = metrics.startedAt
		summary.CompletedAt = metrics.completedAt
		summary.TargetLimit = maxInt(summary.TargetLimit, metrics.targetLimit)
		summary.TotalPages = metrics.totalPages
		summary.DurationSeconds = metrics.durationSeconds
		summary.MagnetSuccessLogCount = metrics.magnetSuccessLogCount
		summary.LargestMagnetLogCount = metrics.largestMagnetLogCount
		summary.WarningCount = metrics.warningCount
		summary.ErrorCount = metrics.errorCount
		summary.HTTP429Count = metrics.http429Count
		summary.CloudflareLineCount = metrics.cloudflareLineCount
		summary.AjaxFallbackCount = metrics.ajaxFallbackCount
		summary.AjaxDomainCooldownCount = metrics.ajaxDomainCooldownCount
		summary.ValidationTimeoutCount = metrics.validationTimeoutCount
		summary.ValidationCooldownCount = metrics.validationCooldownCount
		summary.RetryCount = metrics.retryCount
		summary.SecondValidationPassed = metrics.secondValidationPassed
		summary.SecondValidationFailed = metrics.secondValidationFailed
		summary.Completed = metrics.completed
		summary.StopRequested = metrics.stopRequested
		summary.Stopped = metrics.stopped
		if summary.CrawledUniqueCount == 0 {
			summary.CrawledUniqueCount = metrics.completedProgressCount
		}
	}

	if summary.SiteDuplicateEntryCount == 0 && len(summary.DuplicateItemIDs) > 0 {
		summary.SiteDuplicateEntryCount = len(summary.DuplicateItemIDs)
	}
	if summary.SiteUniqueEntryCount == 0 {
		summary.SiteUniqueEntryCount = maxInt(summary.CrawledUniqueCount, summary.FilmCodeUnique)
	}
	if summary.SiteRawEntryCount == 0 && summary.SiteUniqueEntryCount > 0 {
		summary.SiteRawEntryCount = summary.SiteUniqueEntryCount + summary.SiteDuplicateEntryCount
	}
	if summary.TargetLimit == 0 {
		summary.TargetLimit = maxInt(summary.CrawledUniqueCount, summary.FilmCodeUnique, summary.MagnetUnique)
	}
	if summary.CrawledUniqueCount == 0 {
		summary.CrawledUniqueCount = maxInt(summary.FilmCodeUnique, summary.SiteUniqueEntryCount)
	}

	summary.RawTargetShortfallCount = maxInt(summary.TargetLimit-summary.SiteRawEntryCount, 0)
	summary.UniqueTargetShortfallCount = maxInt(summary.TargetLimit-summary.SiteDuplicateEntryCount-summary.CrawledUniqueCount, 0)
	summary.ExpectedMagnetTxtTotal = maxInt(summary.FilmRecordWithMagnet-summary.FilmRecordFilteredByActressCount, 0)
	summary.OutputCompletionRate = ratio(summary.CrawledUniqueCount, summary.TargetLimit)
	if summary.ExpectedMagnetTxtTotal > 0 {
		summary.FilmMagnetConsistencyRate = ratio(summary.MagnetTotal, summary.ExpectedMagnetTxtTotal)
	}

	buildStatus(&summary)

	if normalized.WriteReport && summary.OutputDir != "" {
		summary.ReportPath = DefaultReportPath(summary.OutputDir)
		if err := writeReport(summary.ReportPath, summary); err != nil {
			return Summary{}, err
		}
	} else if summary.OutputDir != "" {
		summary.ReportPath = DefaultReportPath(summary.OutputDir)
	}

	buildPresentation(&summary)
	return summary, nil
}

// normalizeOptions resolves whichever subset of output/log/artifact inputs the
// caller provided into one consistent summary input set. If the quality report
// is reading the wrong run directory, inspect this boundary first.
func normalizeOptions(options Options) Options {
	normalized := Options{
		OutputDir:      cleanPath(options.OutputDir),
		LogDir:         cleanPath(options.LogDir),
		LatestLogPath:  cleanPath(options.LatestLogPath),
		FilmDataPath:   cleanPath(options.FilmDataPath),
		MagnetPath:     cleanPath(options.MagnetPath),
		UnfinishedPath: cleanPath(options.UnfinishedPath),
		WriteReport:    options.WriteReport,
	}

	if normalized.LogDir == "" && normalized.LatestLogPath != "" {
		normalized.LogDir = filepath.Dir(normalized.LatestLogPath)
	}
	if normalized.OutputDir == "" && normalized.LogDir != "" && strings.EqualFold(filepath.Base(normalized.LogDir), defaultLogDirName) {
		normalized.OutputDir = filepath.Dir(normalized.LogDir)
	}
	if normalized.OutputDir == "" && normalized.FilmDataPath != "" {
		normalized.OutputDir = filepath.Dir(normalized.FilmDataPath)
	}
	if normalized.OutputDir == "" && normalized.MagnetPath != "" {
		normalized.OutputDir = filepath.Dir(normalized.MagnetPath)
	}
	if normalized.OutputDir == "" && normalized.UnfinishedPath != "" {
		normalized.OutputDir = filepath.Dir(normalized.UnfinishedPath)
	}
	if normalized.LogDir == "" && normalized.OutputDir != "" {
		normalized.LogDir = crawlartifact.DefaultLogDirPath(normalized.OutputDir)
	}
	if normalized.LatestLogPath == "" && normalized.LogDir != "" {
		normalized.LatestLogPath = resolveLatestLogPath(normalized.LogDir)
	}
	if normalized.FilmDataPath == "" && normalized.OutputDir != "" {
		normalized.FilmDataPath = crawlartifact.ResolveCrawlOutputPaths(normalized.OutputDir).FilmDataPath
	}
	if normalized.MagnetPath == "" && normalized.OutputDir != "" {
		normalized.MagnetPath = crawlartifact.DefaultMagnetFilePath(normalized.OutputDir)
	}
	if normalized.UnfinishedPath == "" && normalized.OutputDir != "" {
		normalized.UnfinishedPath = crawlartifact.DefaultUnfinishedReportPath(normalized.OutputDir)
	}

	normalized.OutputDir = chooseBestOutputDir(
		normalized.OutputDir,
		dirOfExistingFile(normalized.FilmDataPath),
		dirOfExistingFile(normalized.MagnetPath),
		dirOfExistingFile(normalized.UnfinishedPath),
		deriveOutputDirFromLogDir(normalized.LogDir),
		deriveOutputDirFromLogPath(normalized.LatestLogPath),
	)

	if normalized.OutputDir != "" {
		runPaths := crawlartifact.ResolveCrawlRunPaths(normalized.OutputDir)
		if normalized.LogDir == "" {
			normalized.LogDir = runPaths.LogDir
		}
		if normalized.LatestLogPath == "" {
			normalized.LatestLogPath = resolveLatestLogPath(normalized.LogDir)
		}
		if normalized.FilmDataPath == "" {
			normalized.FilmDataPath = runPaths.FilmDataPath
		}
		if normalized.MagnetPath == "" {
			normalized.MagnetPath = runPaths.MagnetPath
		}
		if normalized.UnfinishedPath == "" {
			normalized.UnfinishedPath = crawlartifact.DefaultUnfinishedReportPath(normalized.OutputDir)
		}
	}

	return normalized
}

func deriveOutputDirFromLogDir(logDir string) string {
	logDir = cleanPath(logDir)
	if logDir == "" {
		return ""
	}
	if strings.EqualFold(filepath.Base(logDir), defaultLogDirName) {
		return filepath.Dir(logDir)
	}
	return ""
}

func deriveOutputDirFromLogPath(logPath string) string {
	logPath = cleanPath(logPath)
	if logPath == "" {
		return ""
	}
	return deriveOutputDirFromLogDir(filepath.Dir(logPath))
}

func dirOfExistingFile(filePath string) string {
	filePath = cleanPath(filePath)
	if filePath == "" || !fileExists(filePath) {
		return ""
	}
	return filepath.Dir(filePath)
}

func chooseBestOutputDir(current string, candidates ...string) string {
	best := cleanPath(current)
	bestScore := outputDirScore(best)
	seen := map[string]struct{}{}
	if best != "" {
		seen[best] = struct{}{}
	}

	for _, candidate := range candidates {
		candidate = cleanPath(candidate)
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		score := outputDirScore(candidate)
		if best == "" || score > bestScore {
			best = candidate
			bestScore = score
		}
	}
	return best
}

func outputDirScore(outputDir string) int {
	outputDir = cleanPath(outputDir)
	if outputDir == "" {
		return -1
	}
	runPaths := crawlartifact.ResolveCrawlRunPaths(outputDir)
	score := 0
	if fileExists(runPaths.FilmDataPath) {
		score += 2
	}
	if fileExists(runPaths.MagnetPath) {
		score += 2
	}
	if fileExists(crawlartifact.DefaultUnfinishedReportPath(outputDir)) {
		score++
	}
	if fileExists(runPaths.LatestLogPath) {
		score++
	}
	return score
}

func resolveLatestLogPath(logDir string) string {
	if strings.TrimSpace(logDir) == "" {
		return ""
	}
	latestPath := crawlartifact.DefaultLatestLogPath(logDir)
	if fileExists(latestPath) {
		return latestPath
	}

	matches := make([]string, 0)
	for _, pattern := range runLogPatterns {
		found, err := filepath.Glob(filepath.Join(logDir, pattern))
		if err != nil {
			continue
		}
		matches = append(matches, found...)
	}
	if len(matches) == 0 {
		return latestPath
	}

	sort.Slice(matches, func(i, j int) bool {
		leftInfo, leftErr := os.Stat(matches[i])
		rightInfo, rightErr := os.Stat(matches[j])
		if leftErr == nil && rightErr == nil {
			return leftInfo.ModTime().After(rightInfo.ModTime())
		}
		return matches[i] > matches[j]
	})
	return matches[0]
}

func fileExists(filePath string) bool {
	info, err := os.Stat(filePath)
	return err == nil && info.Mode().IsRegular()
}

func readLines(filePath string) ([]string, error) {
	contents, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	contents = bytes.TrimPrefix(contents, []byte{0xEF, 0xBB, 0xBF})
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	scanner.Buffer(make([]byte, 1024), 8*1024*1024)
	lines := make([]string, 0)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func readMagnetMetrics(filePath string) (total int, unique int, duplicate int, err error) {
	if !fileExists(filePath) {
		return 0, 0, 0, os.ErrNotExist
	}
	lines, err := readLines(filePath)
	if err != nil {
		return 0, 0, 0, err
	}
	seen := map[string]struct{}{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(strings.ToLower(line), "magnet:?") {
			continue
		}
		total++
		key := strings.ToLower(line)
		if _, ok := seen[key]; ok {
			duplicate++
			continue
		}
		seen[key] = struct{}{}
	}
	return total, len(seen), duplicate, nil
}

// readFilmDataMetrics treats `filmData.json` as the durable truth of persisted
// films, including records filtered out from TXT export.
//
// If TXT output and JSON totals disagree, compare this view against
// `readMagnetMetrics()` first; if filtered IDs look wrong, inspect the record
// shape here before touching the log parser or presentation layer.
func readFilmDataMetrics(filePath string) (filmDataMetrics, error) {
	if !fileExists(filePath) {
		return filmDataMetrics{}, os.ErrNotExist
	}
	contents, err := os.ReadFile(filePath)
	if err != nil {
		return filmDataMetrics{}, err
	}

	var parsed any
	if err := json.Unmarshal(bytes.TrimPrefix(contents, []byte{0xEF, 0xBB, 0xBF}), &parsed); err != nil {
		return filmDataMetrics{}, err
	}

	records := normalizeFilmRecords(parsed)
	metrics := filmDataMetrics{total: len(records)}
	codeSet := map[string]struct{}{}
	filterSet := map[string]struct{}{}
	for _, record := range records {
		if hasRecordMagnet(record) {
			metrics.withMagnet++
		}
		code := extractFilmCode(record)
		if code != "" {
			codeSet[code] = struct{}{}
		}
		if isActressCountFiltered(record) {
			metrics.filteredByActressCount++
			if code != "" {
				if _, ok := filterSet[code]; !ok {
					filterSet[code] = struct{}{}
					metrics.filteredItemIDs = append(metrics.filteredItemIDs, code)
				}
			}
		}
	}
	metrics.uniqueCodes = len(codeSet)
	sort.Strings(metrics.filteredItemIDs)
	return metrics, nil
}

// normalizeFilmRecords accepts the common persisted JSON shapes and flattens
// them into one record slice for summary math.
func normalizeFilmRecords(parsed any) []map[string]any {
	switch value := parsed.(type) {
	case []any:
		return objectSlice(value)
	case map[string]any:
		for _, key := range []string{"records", "filmData", "items", "data"} {
			if rawItems, ok := value[key].([]any); ok {
				return objectSlice(rawItems)
			}
		}
		return []map[string]any{value}
	default:
		return nil
	}
}

// objectSlice filters mixed JSON arrays down to object records only.
func objectSlice(values []any) []map[string]any {
	records := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if record, ok := value.(map[string]any); ok {
			records = append(records, record)
		}
	}
	return records
}

// hasRecordMagnet answers a single question: does this persisted record still
// carry any magnet payload worth counting?
func hasRecordMagnet(record map[string]any) bool {
	for _, key := range []string{"magnetLinks", "backupMagnetLinks", "magnets", "magnet"} {
		if hasMagnetValue(record[key]) {
			return true
		}
	}
	return false
}

// hasMagnetValue walks nested magnet fields without assuming a fixed record
// layout, because older outputs can nest links differently.
func hasMagnetValue(value any) bool {
	switch typed := value.(type) {
	case string:
		return strings.Contains(strings.ToLower(typed), "magnet:?")
	case []any:
		for _, item := range typed {
			if hasMagnetValue(item) {
				return true
			}
		}
	case map[string]any:
		for _, key := range []string{"link", "magnet"} {
			if hasMagnetValue(typed[key]) {
				return true
			}
		}
	}
	return false
}

// isActressCountFiltered reads the persisted filter marker written by the
// runner. This is the bridge between output policy and summary reporting.
func isActressCountFiltered(record map[string]any) bool {
	value, exists := record["filteredByActressCount"]
	if !exists {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	case float64:
		return typed != 0
	default:
		return false
	}
}

// extractFilmCode pulls the canonical film code from whichever persisted field
// still has the best source of truth.
func extractFilmCode(record map[string]any) string {
	for _, key := range []string{"filmCode", "code", "sourceLink", "title", "fileName"} {
		if code := normalizeFilmCode(fmt.Sprint(record[key])); code != "" {
			return code
		}
	}
	return ""
}

// normalizeFilmCode keeps code matching stable across separators and case.
func normalizeFilmCode(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	value = strings.TrimPrefix(value, "@")
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, " ", "")
	match := filmCodePattern.FindStringSubmatch(value)
	if len(match) != 4 {
		return ""
	}
	return crawlidentity.NormalizeFilmID(match[1] + "-" + match[2] + match[3])
}

// The unfinished report is parsed back into counters so stopped/partial runs
// can still be explained without replaying crawl execution.
//
// If the unfinished counts look wrong, inspect this parser before touching the
// runner or UI projection code.
func readUnfinishedMetrics(filePath string) (unfinishedMetrics, error) {
	if !fileExists(filePath) {
		return unfinishedMetrics{}, os.ErrNotExist
	}
	lines, err := readLines(filePath)
	if err != nil {
		return unfinishedMetrics{}, err
	}

	metrics := unfinishedMetrics{}
	duplicateSet := map[string]struct{}{}
	unfinishedSet := map[string]struct{}{}
	section := ""

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "#") {
			section = detectUnfinishedSection(trimmed)
			switch {
			case strings.HasPrefix(trimmed, "# 已完成"):
				metrics.completedCount = maxInt(metrics.completedCount, extractFirstInt(trimmed))
			case strings.HasPrefix(trimmed, "# 目标条数"):
				metrics.targetCount = maxInt(metrics.targetCount, extractFirstInt(trimmed))
			case strings.HasPrefix(trimmed, "# 站点原始条目"):
				metrics.rawEntryCount = maxInt(metrics.rawEntryCount, extractFirstInt(trimmed))
			case strings.HasPrefix(trimmed, "# 站点唯一番号"):
				metrics.uniqueEntryCount = maxInt(metrics.uniqueEntryCount, extractFirstInt(trimmed))
			case strings.HasPrefix(trimmed, "# 站点重复条目"):
				metrics.duplicateEntryCount = maxInt(metrics.duplicateEntryCount, extractFirstInt(trimmed))
			case strings.HasPrefix(trimmed, "# 低可信分页"):
				metrics.lowConfidencePageCount = maxInt(metrics.lowConfidencePageCount, extractFirstInt(trimmed))
			}
			continue
		}

		if section == "duplicate" {
			if code := normalizeFilmCode(trimmed); code != "" {
				if _, ok := duplicateSet[code]; !ok {
					duplicateSet[code] = struct{}{}
					metrics.duplicateItemIDs = append(metrics.duplicateItemIDs, code)
				}
			}
			continue
		}

		if section == "unfinished" {
			if strings.Contains(trimmed, "暂无已定位未完成番号") {
				continue
			}
			if code := normalizeFilmCode(trimmed); code != "" {
				if _, ok := unfinishedSet[code]; !ok {
					unfinishedSet[code] = struct{}{}
					metrics.unfinishedItemIDs = append(metrics.unfinishedItemIDs, code)
				}
			}
		}
	}

	sort.Strings(metrics.duplicateItemIDs)
	sort.Strings(metrics.unfinishedItemIDs)
	if metrics.duplicateEntryCount == 0 && len(metrics.duplicateItemIDs) > 0 {
		metrics.duplicateEntryCount = len(metrics.duplicateItemIDs)
	}
	if metrics.rawEntryCount == 0 && metrics.uniqueEntryCount > 0 {
		metrics.rawEntryCount = metrics.uniqueEntryCount + metrics.duplicateEntryCount
	}
	return metrics, nil
}

// detectUnfinishedSection maps a section header to the subsection that follows
// it. This keeps duplicate and unfinished item parsing separated.
func detectUnfinishedSection(line string) string {
	switch {
	case strings.Contains(line, "已定位重复番号"):
		return "duplicate"
	case strings.Contains(line, "已定位未完成番号"):
		return "unfinished"
	default:
		return ""
	}
}

// parseLogMetrics extracts operator-facing evidence from the latest task log.
// It is intentionally tolerant of mixed historical log formats because this
// service is used for post-run diagnosis across older outputs.
// When completion, retry, or limit counts look off, inspect this parser next.
func parseLogMetrics(filePath string) (logMetrics, error) {
	// 日志解析尽量容错：优先抓明确的结构化字段，再退回时间戳和关键词。
	if !fileExists(filePath) {
		return logMetrics{}, os.ErrNotExist
	}
	lines, err := readLines(filePath)
	if err != nil {
		return logMetrics{}, err
	}

	metrics := logMetrics{}
	firstTimestamp := ""
	lastTimestamp := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if timestamp := extractLogTimestamp(trimmed); timestamp != "" {
			if firstTimestamp == "" {
				firstTimestamp = timestamp
			}
			lastTimestamp = timestamp
		}

		if metrics.startedAt == "" {
			if value := extractHeaderValue(trimmed, startedTokens); value != "" {
				metrics.startedAt = value
			}
		}
		if metrics.sourceURL == "" {
			if value := extractHeaderValue(trimmed, sourceURLTokens); value != "" {
				metrics.sourceURL = value
			}
		}
		if metrics.runPlan == "" {
			if value := extractHeaderValue(trimmed, runPlanTokens); value != "" {
				metrics.runPlan = value
			}
		}
		if metrics.targetLimit == 0 {
			if match := limitPattern.FindStringSubmatch(trimmed); len(match) == 2 {
				metrics.targetLimit = mustAtoi(match[1])
			} else if value := extractLabeledInt(trimmed, targetTokens); value > 0 {
				metrics.targetLimit = value
			}
		}
		if metrics.totalPages == 0 {
			if match := totalPagesPattern.FindStringSubmatch(trimmed); len(match) == 2 {
				metrics.totalPages = mustAtoi(match[1])
			} else if value := extractLabeledInt(trimmed, totalPageTokens); value > 0 {
				metrics.totalPages = value
			}
		}

		if containsAny(trimmed, warningTokens) {
			metrics.warningCount++
		}
		if containsAny(trimmed, errorTokens) {
			metrics.errorCount++
		}
		if strings.Contains(trimmed, "429") {
			metrics.http429Count++
		}
		if strings.Contains(strings.ToLower(trimmed), "cloudflare") {
			metrics.cloudflareLineCount++
		}
		if strings.Contains(trimmed, "AJAX") && containsAny(trimmed, ajaxFallbackTokens) {
			metrics.ajaxFallbackCount++
		}
		if strings.Contains(trimmed, "AJAX") && containsAny(trimmed, cooldownTokens) {
			metrics.ajaxDomainCooldownCount++
		}
		if containsAny(trimmed, validationPrefixTokens) && containsAny(trimmed, timeoutTokens) {
			metrics.validationTimeoutCount++
		}
		if containsAny(trimmed, validationPrefixTokens) && containsAny(trimmed, cooldownTokens) {
			metrics.validationCooldownCount++
		}
		if containsAny(trimmed, retryTokens) {
			metrics.retryCount++
		}
		if containsAny(trimmed, magnetSuccessTokens) {
			metrics.magnetSuccessLogCount++
		}
		if containsAny(trimmed, largestMagnetTokens) {
			metrics.largestMagnetLogCount++
		}
		if containsAny(trimmed, secondValidationPassedTokens) {
			metrics.secondValidationPassed = true
		}
		if containsAny(trimmed, secondValidationFailedTokens) {
			metrics.secondValidationFailed = true
		}
		if containsAny(trimmed, stopRequestedTokens) {
			metrics.stopRequested = true
		}
		if containsAny(trimmed, stoppedTokens) {
			metrics.stopRequested = true
			metrics.stopped = true
		}
		if containsAny(trimmed, completedTokens) || containsAny(trimmed, secondValidationPassedTokens) {
			metrics.completed = true
		}
		if duration := extractDurationSeconds(trimmed); duration > 0 {
			metrics.durationSeconds = selectDurationSeconds(metrics.durationSeconds, duration, trimmed)
		}
		if count := extractCompletedProgressCount(trimmed); count > metrics.completedProgressCount {
			metrics.completedProgressCount = count
		}
	}

	if metrics.startedAt == "" {
		metrics.startedAt = firstTimestamp
	}
	if metrics.completed && lastTimestamp != "" {
		metrics.completedAt = lastTimestamp
	}
	if metrics.stopped && metrics.completedAt == "" {
		metrics.completedAt = lastTimestamp
	}
	if metrics.durationSeconds == 0 {
		metrics.durationSeconds = computeDurationSeconds(metrics.startedAt, metrics.completedAt)
	}
	if metrics.targetLimit == 0 && metrics.completedProgressCount > 0 {
		metrics.targetLimit = metrics.completedProgressCount
	}
	return metrics, nil
}

// extractHeaderValue finds the first labeled field value in a log line.
func extractHeaderValue(line string, tokens []string) string {
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		index := strings.Index(strings.ToLower(line), strings.ToLower(token))
		if index < 0 {
			continue
		}
		value := strings.TrimSpace(line[index+len(token):])
		value = strings.TrimLeft(value, "：:，, \t")
		return strings.TrimSpace(value)
	}
	return ""
}

// extractLabeledInt converts a labeled text field into an int if present.
func extractLabeledInt(line string, tokens []string) int {
	value := extractHeaderValue(line, tokens)
	if value == "" {
		return 0
	}
	return extractFirstInt(value)
}

// extractFirstInt returns the first integer embedded in a string.
func extractFirstInt(line string) int {
	start := -1
	for index, r := range line {
		if r >= '0' && r <= '9' {
			start = index
			break
		}
	}
	if start < 0 {
		return 0
	}
	end := start
	for end < len(line) && line[end] >= '0' && line[end] <= '9' {
		end++
	}
	return mustAtoi(line[start:end])
}

// extractCompletedProgressCount looks for the largest explicit completion
// count in a single log line so summary math can tolerate repeated messages.
func extractCompletedProgressCount(line string) int {
	matches := completedProgressPattern.FindAllStringSubmatch(line, -1)
	maxCount := 0
	for _, match := range matches {
		if len(match) != 2 {
			continue
		}
		if value := mustAtoi(match[1]); value > maxCount {
			maxCount = value
		}
	}
	return maxCount
}

// extractDurationSeconds accepts several duration spellings from historical
// logs and returns the longest matching duration signal.
func extractDurationSeconds(line string) int {
	normalized := strings.ToLower(strings.TrimSpace(line))
	if normalized == "" {
		return 0
	}
	if !strings.Contains(normalized, "duration") && !strings.Contains(line, "耗时") && !strings.Contains(line, "用时") {
		return 0
	}
	matches := durationPattern.FindAllStringSubmatch(normalized, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		if len(matches[i]) == 2 {
			if value := mustAtoi(matches[i][1]); value > 0 {
				return value
			}
		}
	}
	return 0
}

// selectDurationSeconds 优先保留“最终完成/摘要汇总”行中的耗时，其次保留最新匹配值。
// selectDurationSeconds keeps the best duration signal when multiple log lines
// report different timings.
func selectDurationSeconds(current int, next int, line string) int {
	if next <= 0 {
		return current
	}
	normalized := strings.ToLower(strings.TrimSpace(line))
	if strings.Contains(normalized, "抓取任务完成") || strings.Contains(normalized, "crawl completed") || strings.Contains(normalized, "summary") || strings.Contains(normalized, "摘要") {
		return next
	}
	if current <= 0 {
		return next
	}
	return current
}

// containsAny does a case-insensitive token scan for log classification.
func containsAny(line string, tokens []string) bool {
	lowerLine := strings.ToLower(line)
	for _, token := range tokens {
		token = strings.ToLower(strings.TrimSpace(token))
		if token == "" {
			continue
		}
		if strings.Contains(lowerLine, token) {
			return true
		}
	}
	return false
}

// extractLogTimestamp accepts both bracketed and plain timestamp styles.
func extractLogTimestamp(line string) string {
	if strings.HasPrefix(line, "[") {
		if end := strings.Index(line, "]"); end > 1 {
			return strings.TrimSpace(line[1:end])
		}
	}
	return ""
}

// computeDurationSeconds falls back to parsed timestamps when log lines omit a
// direct duration field.
func computeDurationSeconds(startedAt string, completedAt string) int {
	startTime, ok := parseFlexibleTime(startedAt)
	if !ok {
		return 0
	}
	endTime, ok := parseFlexibleTime(completedAt)
	if !ok || endTime.Before(startTime) {
		return 0
	}
	return int(endTime.Sub(startTime).Round(time.Second) / time.Second)
}

// parseFlexibleTime accepts the timestamp layouts seen across old run logs.
func parseFlexibleTime(raw string) (time.Time, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05",
		"2006/01/02 15:04:05",
		"2006/1/2 15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// mustAtoi is a soft parser used by the log/summary code.
func mustAtoi(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return parsed
}

// buildStatus assigns a coarse availability/quality label. Detailed reasons
// belong in the Issues list rather than in the status field itself.
// Keep this label coarse so the report text can carry the concrete explanation.
func buildStatus(summary *Summary) {
	// buildStatus 只负责给当前输出贴“可用/告警/阻断”标签。
	// 具体为什么可用、为什么未补齐，由 issues 逐条说明。
	issues := make([]Issue, 0, 16)
	wasStopped := summary.StopRequested || summary.Stopped
	noPrimaryOutput := summary.MagnetTotal == 0 && summary.FilmRecordTotal == 0 && summary.CrawledUniqueCount == 0
	expectedUniqueCount := maxInt(summary.TargetLimit-summary.SiteDuplicateEntryCount, 0)

	if wasStopped && noPrimaryOutput {
		summary.StoppedWithoutOutput = true
		addIssue(&issues, "warning", "任务已终止，当前目录只有日志，未生成可用的磁力文本和影片数据。")
		if summary.HTTP429Count > 0 {
			addIssue(&issues, "info", fmt.Sprintf("终止前出现 %d 次 429/限流记录，会拖慢抓取速度。", summary.HTTP429Count))
		}
		if summary.ErrorCount > 0 {
			addIssue(&issues, "info", fmt.Sprintf("终止前累计 %d 条错误日志，可结合最新运行日志继续排查。", summary.ErrorCount))
		}
		summary.Issues = issues
		summary.Available = fileExists(summary.LatestLogPath)
		summary.Status = "stopped-empty"
		summary.StatusText = "任务已终止且未产生有效输出"
		return
	}

	if wasStopped && !summary.Completed {
		summary.StoppedWithPartialOutput = true
		addIssue(&issues, "warning", fmt.Sprintf("任务已终止，但已保留部分输出：唯一影片 %d 条，影片记录 %d 条。", summary.CrawledUniqueCount, summary.FilmRecordTotal))
	}

	if noPrimaryOutput {
		addIssue(&issues, "error", "未读取到磁力输出和影片数据，请先确认输出目录是否正确。")
	} else {
		if summary.MagnetTotal == 0 {
			addIssue(&issues, "error", "magnet-links.txt 为空，磁力输出不可用。")
		}
		if summary.FilmRecordTotal == 0 {
			addIssue(&issues, "warning", "filmData.json 未读取到影片记录，整理层无法自动对照番号。")
		}
	}

	if summary.TargetLimit > 0 && summary.CrawledUniqueCount > 0 && summary.CrawledUniqueCount < summary.TargetLimit && summary.SiteRawEntryCount == 0 {
		addIssue(&issues, "warning", fmt.Sprintf("目标 %d 条，当前仅完成 %d 条。", summary.TargetLimit, summary.CrawledUniqueCount))
	}
	if summary.SiteRawEntryCount > 0 && summary.RawTargetShortfallCount > 0 {
		addIssue(&issues, "info", fmt.Sprintf("站点原始条目 %d，较目标 %d 少 %d 条。", summary.SiteRawEntryCount, summary.TargetLimit, summary.RawTargetShortfallCount))
	}
	if summary.SiteDuplicateEntryCount > 0 {
		addIssue(&issues, "info", fmt.Sprintf("站点重复番号 %d 条（%s）。", summary.SiteDuplicateEntryCount, joinIDs(summary.DuplicateItemIDs)))
	}
	if summary.UniqueTargetShortfallCount > 0 {
		addIssue(&issues, "warning", fmt.Sprintf("按唯一番号理论应完成 %d 条，当前完成 %d 条，仍少 %d 条。状态：%s。", expectedUniqueCount, summary.CrawledUniqueCount, summary.UniqueTargetShortfallCount, crawlexecution.TargetFulfillmentText(summary.UniqueTargetShortfallCount)))
	}

	if summary.FilmRecordFilteredByActressCount > 0 {
		if summary.MagnetTotal == summary.ExpectedMagnetTxtTotal {
			addIssue(&issues, "info", fmt.Sprintf("filmData.json 含磁力影片 %d 条，其中 %d 条因演员数量达到阈值仅保留在 JSON；因此 TXT 导出 %d = %d - %d，当前结果符合预期。过滤结果：%s。", summary.FilmRecordWithMagnet, summary.FilmRecordFilteredByActressCount, summary.MagnetTotal, summary.FilmRecordWithMagnet, summary.FilmRecordFilteredByActressCount, crawlexecution.TargetFulfillmentText(summary.UniqueTargetShortfallCount)))
		} else {
			addIssue(&issues, "info", fmt.Sprintf("filmData.json 含磁力影片 %d 条，其中 %d 条因演员数量达到阈值仅保留在 JSON；因此 TXT 预期应为 %d = %d - %d。过滤结果：%s。", summary.FilmRecordWithMagnet, summary.FilmRecordFilteredByActressCount, summary.ExpectedMagnetTxtTotal, summary.FilmRecordWithMagnet, summary.FilmRecordFilteredByActressCount, crawlexecution.TargetFulfillmentText(summary.UniqueTargetShortfallCount)))
		}
		if len(summary.FilteredItemIDs) > 0 {
			addIssue(&issues, "info", "被过滤番号："+strings.Join(summary.FilteredItemIDs, "、"))
		}
	}
	if summary.ExpectedMagnetTxtTotal > 0 && summary.MagnetTotal != summary.ExpectedMagnetTxtTotal {
		addIssue(&issues, "warning", fmt.Sprintf("TXT 实际输出 %d 条，但按 filmData 统计应输出 %d 条。", summary.MagnetTotal, summary.ExpectedMagnetTxtTotal))
	}
	if summary.MagnetDuplicate > 0 {
		addIssue(&issues, "info", fmt.Sprintf("磁力文本中存在 %d 条重复记录，已单独统计。", summary.MagnetDuplicate))
	}
	if summary.SecondValidationFailed {
		addIssue(&issues, "warning", "结果二次校验失败，需要人工复核输出完整性。")
	} else if summary.LatestLogPath != "" && fileExists(summary.LatestLogPath) && !summary.SecondValidationPassed && !summary.Completed {
		addIssue(&issues, "info", "日志中未发现明确的二次校验通过记录，可能是旧日志或任务未完整收尾。")
	}
	if summary.LatestLogPath != "" && fileExists(summary.LatestLogPath) && !summary.Completed && !summary.Stopped {
		addIssue(&issues, "warning", "日志中未发现明确的抓取完成记录，请确认任务是否中断。")
	}
	if summary.HTTP429Count > 0 {
		addIssue(&issues, "info", fmt.Sprintf("日志出现 %d 次 429/限流记录，会拉长整体耗时。", summary.HTTP429Count))
	}
	if summary.ValidationTimeoutCount > 0 {
		addIssue(&issues, "info", fmt.Sprintf("磁力内容校验超时 %d 次。", summary.ValidationTimeoutCount))
	}
	if summary.ErrorCount > 0 && summary.TargetLimit > 0 && summary.CrawledUniqueCount >= summary.TargetLimit {
		addIssue(&issues, "info", fmt.Sprintf("日志有 %d 条错误记录，但最终完成数已达到目标，可先按可恢复异常处理。", summary.ErrorCount))
	} else if summary.ErrorCount > 0 {
		addIssue(&issues, "warning", fmt.Sprintf("日志有 %d 条错误记录，建议回看最新运行日志。", summary.ErrorCount))
	}

	summary.Issues = issues
	summary.Available = summary.MagnetTotal > 0 || summary.FilmRecordTotal > 0 || summary.CrawledUniqueCount > 0 || fileExists(summary.LatestLogPath)

	status := "ok"
	statusText := "输出质量正常"
	for _, issue := range issues {
		if issue.Level == "error" {
			status = "error"
			statusText = "输出存在阻断问题"
			break
		}
		if issue.Level == "warning" && status != "error" {
			status = "warning"
			statusText = "输出可用，但建议复查"
		}
	}
	if summary.StoppedWithoutOutput {
		status = "stopped-empty"
		statusText = "任务已终止且未产生有效输出"
	} else if summary.StoppedWithPartialOutput {
		status = "stopped-partial"
		statusText = "任务已终止，但已保留部分输出"
	}
	if !summary.Available {
		status = "empty"
		statusText = "未找到可分析输出"
	}
	summary.Status = status
	summary.StatusText = statusText
}

// buildPresentation converts structured summary data into short UI-facing text
// so the panel and the written report stay aligned.
// If the panel wording and report wording diverge, fix the shared summary data
// first rather than adding new presentation-only branches.
// buildPresentation translates the raw summary into operator-facing labels and
// a compact one-line recap. Keep wording aligned with writeReport().
func buildPresentation(summary *Summary) {
	// buildPresentation 只负责把统计结果拼成给用户看的单行摘要。
	// 这里的措辞要和报告正文统一，避免同一结论出现多套叫法。
	targetText := "未设置"
	if summary.TargetLimit > 0 {
		targetText = strconv.Itoa(summary.TargetLimit)
	}

	validationText := "未确认"
	if summary.SecondValidationPassed {
		validationText = "通过"
	} else if summary.SecondValidationFailed {
		validationText = "失败"
	}

	txtExportText := buildTxtExportExplanation(*summary)
	gapText := buildGapExplanation(*summary)
	filterPreview := ""
	if len(summary.FilteredItemIDs) > 0 {
		filterPreview = "；过滤番号 " + previewIDs(summary.FilteredItemIDs, 6)
	}
	reportText := ""
	if strings.TrimSpace(summary.ReportPath) != "" {
		reportText = "；报告：" + strings.TrimSpace(summary.ReportPath)
	}

	summary.NoticeLevel = statusToNoticeLevel(summary.Status)
	summary.SummaryLine = strings.Join([]string{
		"Go 爬虫质量摘要：" + valueOrDefault(summary.StatusText, "已生成"),
		"目标 " + targetText,
		buildTargetGapLine(*summary, gapText),
		fmt.Sprintf("实际完成 %d", summary.CrawledUniqueCount),
		"目标补齐状态：" + crawlexecution.TargetFulfillmentText(summary.UniqueTargetShortfallCount),
		txtExportText,
		fmt.Sprintf("影片记录 %d", summary.FilmRecordTotal),
		"二次校验 " + validationText + filterPreview + reportText,
	}, "；")

	topLines := make([]string, 0, 3)
	for _, issue := range summary.Issues {
		topLines = append(topLines, issue.Message)
		if len(topLines) == 3 {
			break
		}
	}
	summary.TopSuggestionLines = topLines
}

func statusToNoticeLevel(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "error":
		return "error"
	case "warning", "stopped-empty", "stopped-partial":
		return "warn"
	default:
		return "info"
	}
}

// writeReport produces a human-readable review document. The priority here is
// explanation clarity, not machine-friendly serialization.
// The report should mirror the live summary, not invent a second set of rules.
// writeReport emits the human-readable review artifact. It mirrors the same
// conclusion text as the in-memory presentation so the report stays consistent.
func writeReport(reportPath string, summary Summary) error {
	// 报告是给人工复盘看的，不追求机器可逆，重点是把“为什么这样算”写清楚。
	if strings.TrimSpace(reportPath) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		return err
	}

	expectedUniqueCount := maxInt(summary.TargetLimit-summary.SiteDuplicateEntryCount, 0)
	lines := []string{
		"JAV 自动化爬虫 - 运行质量摘要",
		"生成时间：" + time.Now().Format("2006-01-02 15:04:05"),
		"状态：" + summary.StatusText,
		"",
		"路径信息：",
		"输出目录：" + valueOrDash(summary.OutputDir),
		"磁力文件：" + valueOrDash(summary.MagnetPath),
		"影片数据：" + valueOrDash(summary.FilmDataPath),
		"未完成番号：" + valueOrDash(summary.UnfinishedPath),
		"日志文件：" + valueOrDash(summary.LatestLogPath),
		"",
		"任务信息：",
		"起始地址：" + valueOrDash(summary.SourceURL),
		"运行方案：" + valueOrDash(summary.RunPlan),
		"开始时间：" + valueOrDash(summary.StartedAt),
		"完成时间：" + valueOrDash(summary.CompletedAt),
		"终止指令：" + yesNo(summary.StopRequested),
		"已终止：" + yesNo(summary.Stopped),
		"任务状态：" + crawlexecution.CompletionStatusText(summary.Completed),
		"目标数量：" + strconv.Itoa(summary.TargetLimit),
		"总页数：" + strconv.Itoa(summary.TotalPages),
		"目标补齐状态：" + crawlexecution.TargetFulfillmentText(summary.UniqueTargetShortfallCount),
		"总耗时秒数：" + strconv.Itoa(summary.DurationSeconds),
		"",
		"输出统计：",
		fmt.Sprintf("磁力总数：%d", summary.MagnetTotal),
		fmt.Sprintf("唯一磁力：%d", summary.MagnetUnique),
		fmt.Sprintf("重复磁力：%d", summary.MagnetDuplicate),
		fmt.Sprintf("影片记录：%d", summary.FilmRecordTotal),
		fmt.Sprintf("含磁力影片记录：%d", summary.FilmRecordWithMagnet),
		fmt.Sprintf("演员过滤影片：%d", summary.FilmRecordFilteredByActressCount),
		fmt.Sprintf("TXT 预期条数：%d", summary.ExpectedMagnetTxtTotal),
		"TXT 导出解释：" + buildTxtExportExplanation(summary),
		fmt.Sprintf("filmData 唯一番号：%d", summary.FilmCodeUnique),
		"",
		"站点复盘：",
		fmt.Sprintf("站点原始条目：%d", summary.SiteRawEntryCount),
		fmt.Sprintf("站点唯一番号：%d", summary.SiteUniqueEntryCount),
		fmt.Sprintf("站点重复条目：%d", summary.SiteDuplicateEntryCount),
		fmt.Sprintf("按唯一番号理论应完成：%d", expectedUniqueCount),
		fmt.Sprintf("当前完成：%d", summary.CrawledUniqueCount),
		fmt.Sprintf("仍少：%d", summary.UniqueTargetShortfallCount),
		"缺口解释：" + buildGapExplanation(summary) + "；实际完成 " + strconv.Itoa(summary.CrawledUniqueCount),
		"",
		"日志统计：",
		fmt.Sprintf("成功获取磁力日志：%d", summary.MagnetSuccessLogCount),
		fmt.Sprintf("返回最大磁力日志：%d", summary.LargestMagnetLogCount),
		fmt.Sprintf("警告：%d", summary.WarningCount),
		fmt.Sprintf("错误：%d", summary.ErrorCount),
		fmt.Sprintf("429/限流：%d", summary.HTTP429Count),
		fmt.Sprintf("Cloudflare：%d", summary.CloudflareLineCount),
		fmt.Sprintf("AJAX 回退：%d", summary.AjaxFallbackCount),
		fmt.Sprintf("AJAX 冷却：%d", summary.AjaxDomainCooldownCount),
		fmt.Sprintf("磁力校验超时：%d", summary.ValidationTimeoutCount),
		fmt.Sprintf("磁力校验冷却：%d", summary.ValidationCooldownCount),
		fmt.Sprintf("重试：%d", summary.RetryCount),
		"",
		"复查建议：",
	}

	if summary.FilmRecordFilteredByActressCount > 0 {
		lines = append(lines, fmt.Sprintf("1. 演员数量过滤已生效，TXT 预期输出应按 %d 条计算。", summary.ExpectedMagnetTxtTotal))
	}
	if summary.RawTargetShortfallCount > 0 {
		lines = append(lines, fmt.Sprintf("2. 站点原始条目较目标少 %d 条，优先回看分页缺口。", summary.RawTargetShortfallCount))
	}
	if len(summary.DuplicateItemIDs) > 0 {
		lines = append(lines, "3. 重复番号："+strings.Join(summary.DuplicateItemIDs, "、"))
	}
	if len(summary.UnfinishedItemIDs) > 0 {
		lines = append(lines, "4. 已定位未完成番号："+strings.Join(summary.UnfinishedItemIDs, "、"))
	}
	if len(summary.FilteredItemIDs) > 0 {
		lines = append(lines, "5. 被演员数量过滤的番号："+strings.Join(summary.FilteredItemIDs, "、"))
	}
	if len(summary.Issues) == 0 {
		lines = append(lines, "暂无明显异常。")
	} else {
		for _, issue := range summary.Issues {
			lines = append(lines, fmt.Sprintf("- [%s] %s", strings.ToLower(issue.Level), issue.Message))
		}
	}

	content := strings.Join(lines, "\r\n") + "\r\n"
	return common.WriteUTF8TextFile(reportPath, content)
}

// addIssue keeps issue construction uniform across status branches.
func addIssue(issues *[]Issue, level string, message string) {
	*issues = append(*issues, Issue{Level: level, Message: message})
}

// joinIDs formats item IDs for human-readable summary lines.
func joinIDs(ids []string) string {
	if len(ids) == 0 {
		return "暂无"
	}
	return strings.Join(ids, "、")
}

func previewIDs(ids []string, limit int) string {
	if len(ids) == 0 {
		return "暂无"
	}
	if limit <= 0 || len(ids) <= limit {
		return strings.Join(ids, "、")
	}
	return strings.Join(ids[:limit], "、") + fmt.Sprintf(" 等 %d 个", len(ids))
}

func buildTxtExportExplanation(summary Summary) string {
	// TXT 导出解释要直接说明“为什么是这个数”。
	if summary.FilmRecordFilteredByActressCount > 0 {
		return fmt.Sprintf("TXT 导出 %d = %d - %d（filmData 含磁力影片 - 演员过滤）", summary.ExpectedMagnetTxtTotal, summary.FilmRecordWithMagnet, summary.FilmRecordFilteredByActressCount)
	}
	return fmt.Sprintf("TXT 导出 %d", summary.MagnetTotal)
}

// buildGapExplanation focuses on the shortest path to "why not target N":
// target count, raw site entries, duplicate site entries, and unique completed
// items.
func buildGapExplanation(summary Summary) string {
	// 缺口解释优先说明站点原始分页、重复编号和唯一完成量之间的关系。
	if summary.TargetLimit > 0 && summary.SiteRawEntryCount > 0 {
		expectedUniqueCount := maxInt(summary.TargetLimit-summary.SiteDuplicateEntryCount, 0)
		return fmt.Sprintf("站点原始 %d（重复 %d，理论唯一 %d，仍缺 %d）", summary.SiteRawEntryCount, summary.SiteDuplicateEntryCount, expectedUniqueCount, summary.UniqueTargetShortfallCount)
	}
	if summary.TargetLimit > 0 {
		return fmt.Sprintf("目标 %d", summary.TargetLimit)
	}
	return fmt.Sprintf("站点原始 %d", summary.SiteRawEntryCount)
}

func buildTargetGapLine(summary Summary, fallback string) string {
	if summary.UniqueTargetShortfallCount > 0 {
		return fallback + "；目标补齐状态：" + crawlexecution.TargetFulfillmentText(summary.UniqueTargetShortfallCount)
	}
	return fallback + "；目标补齐状态：" + crawlexecution.TargetFulfillmentText(summary.UniqueTargetShortfallCount)
}

// These small formatting helpers keep report text consistent, which matters
// when users compare multiple runs during troubleshooting.
func valueOrDefault(value string, fallback string) string {
	// 空字符串统一回退，避免报告里出现空白字段。
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func valueOrDash(value string) string {
	// 无值时用短横线，便于人工快速扫读。
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return strings.TrimSpace(value)
}

func yesNo(value bool) string {
	// 布尔值统一映射成“是/否”，避免报告里出现 true/false 混写。
	if value {
		return "是"
	}
	return "否"
}

// ratio returns a bounded percentage-style value for report math.
func ratio(part int, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(part) / float64(total)
}

// maxInt is the local integer max helper used by summary math.
func maxInt(values ...int) int {
	max := 0
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	return max
}
