package crawltask

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"javflow/internal/common"
	"javflow/internal/contracts/crawlartifact"
)

// task_log.go owns the operator-facing UTF-8 task log envelope written by the
// Go task-controller path.
//
// Ownership summary:
// 1) create and append the operator-facing UTF-8 run log
// 2) normalize visible task log sections, highlights, and noisy-line filtering
// 3) keep log-file presentation policy separate from runner execution logic
//
// File map for maintainers:
// 1) task-log labels and noisy/highlight pattern constants
// 2) UTF-8 log writer lifecycle and append helpers
// 3) human-facing line normalization and section rendering

const (
	taskLogPrefix         = "爬虫"
	crawlLogPrefix        = "爬虫"
	subscriptionLogPrefix = "订阅"
	taskLogRetentionCount = 10
	taskLogTitle          = "JavFlow 任务日志"
	taskLogKeyPrefix      = "[重点] "
	taskLogStatePrefix    = "状态"
)

var (
	taskLogNoisyPatterns = []string{
		"QueueManager: [索引页] 任务开始",
		"QueueManager: [详情页] 任务开始",
		"ResourceMonitor:",
	}
	taskLogKeyPatterns = []string{
		"抓取任务完成",
		"抓取任务已完成",
		"任务未完成",
		"结果二次校验完成",
		"补爬结束",
		"分页缺口",
		"失败详情页",
		"Cloudflare",
		"已发送终止指令",
		"重新爬取",
	}
	taskLogLevelLabels = map[string]string{
		"info":  "信息",
		"warn":  "警告",
		"error": "错误",
		"debug": "调试",
	}
	taskLogPrefixLabels = map[string]string{
		"QueueManager:":               "队列管理：",
		"FileHandler:":                "文件处理：",
		"RequestHandler:":             "请求处理：",
		"Parser:":                     "解析器：",
		"fetchMagnet:":                "磁力抓取：",
		"parseMetadata:":              "元数据解析：",
		"getPage:":                    "页面抓取：",
		"executeAjax:":                "AJAX 请求：",
		"executeAjaxWithCloudflare:":  "Cloudflare AJAX：",
		"CloudflareBypass:":           "Cloudflare 绕过：",
		"CloudflareAjaxWorkerClient:": "Cloudflare Worker：",
		"PuppeteerPool:":              "浏览器池：",
		"ResourceMonitor:":            "资源监控：",
		"handleGenericError:":         "异常处理：",
		"ResultValidator:":            "结果校验：",
	}
	finalTaskStates = map[string]struct{}{
		"completed":  {},
		"error":      {},
		"stopped":    {},
		"incomplete": {},
	}
)

type taskLogWriter struct {
	mu sync.Mutex

	currentOutputDir string
	logDir           string
	sessionLogPath   string
	latestLogPath    string
	sessionID        string

	lastTaskStateSignature string
	lastTaskStateAt        time.Time
}

func newTaskLogWriter() *taskLogWriter {
	return &taskLogWriter{}
}

func (w *taskLogWriter) currentContext() map[string]any {
	w.mu.Lock()
	defer w.mu.Unlock()

	return map[string]any{
		"logDir":         w.logDir,
		"sessionLogPath": w.sessionLogPath,
		"latestLogPath":  w.latestLogPath,
		"sessionId":      w.sessionID,
	}
}

func (w *taskLogWriter) initialize(outputDir string, payload map[string]any, now time.Time) (map[string]any, error) {
	if now.IsZero() {
		now = time.Now()
	}

	runPaths := crawlartifact.ResolveCrawlRunPaths(outputDir)
	logDir := strings.TrimSpace(cleanString(payload["logDir"]))
	if logDir == "" {
		logDir = runPaths.LogDir
	}
	sessionID := now.Format("20060102-150405")
	// 单份日志策略：每次运行只写一个「模块-启动时间.txt」快照文件。
	// 爬虫与订阅复用同一写入器，靠 subscriptionTask 标记区分前缀；
	// latestLogPath 与会话文件同路径，质量摘要等消费方无需感知变化。
	logPrefix := crawlLogPrefix
	if subscribed, ok := payload["subscriptionTask"].(bool); ok && subscribed {
		logPrefix = subscriptionLogPrefix
	}
	sessionLogPath := filepath.Join(logDir, fmt.Sprintf("%s-%s.txt", logPrefix, sessionID))
	latestLogPath := sessionLogPath

	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, err
	}
	pruneTaskLogFiles(logDir, logPrefix, taskLogRetentionCount)

	headerLines := []string{
		taskLogTitle,
		fmt.Sprintf("开始时间: %s", formatTaskLogLineStamp(now.Format(time.RFC3339))),
		fmt.Sprintf("输出目录: %s", outputDir),
		fmt.Sprintf("起始地址: %s", cleanString(payload["base"])),
		fmt.Sprintf("运行方案: %s", firstNonEmpty(cleanString(payload["demoLabel"]), cleanString(payload["demoMode"]), "AED")),
	}
	// 抓取配置快照：把用户当次的全部抓取设置写进日志头，排查问题时
	// 无需再回问操作者当时的勾选与过滤条件。
	if configLine := buildCrawlConfigLine(payload); configLine != "" {
		headerLines = append(headerLines, fmt.Sprintf("抓取配置: %s", configLine))
	}
	headerLines = append(headerLines, "------------------------------------------------------------")
	header := strings.Join(headerLines, "\r\n") + "\r\n"

	if err := common.WriteUTF8TextFile(sessionLogPath, header); err != nil {
		return nil, err
	}

	w.mu.Lock()
	w.currentOutputDir = outputDir
	w.logDir = logDir
	w.sessionLogPath = sessionLogPath
	w.latestLogPath = latestLogPath
	w.sessionID = sessionID
	w.lastTaskStateSignature = ""
	w.lastTaskStateAt = time.Time{}
	w.mu.Unlock()

	return map[string]any{
		"logDir":         logDir,
		"sessionLogPath": sessionLogPath,
		"latestLogPath":  latestLogPath,
		"sessionId":      sessionID,
	}, nil
}

func (w *taskLogWriter) writeTaskLog(level string, message string, timestamp string) {
	normalizedMessage := strings.TrimSpace(message)
	if normalizedMessage == "" {
		return
	}
	line := fmt.Sprintf("[%s] %s: %s", formatTaskLogLineStamp(timestamp), getLocalizedTaskLogLevel(level), normalizedMessage)
	_ = w.appendLine(line)
}

func (w *taskLogWriter) appendLogEntry(payload map[string]any) {
	level := strings.ToLower(cleanString(payload["level"]))
	message := cleanString(payload["message"])
	if !shouldWriteTaskLogEntry(level, message) {
		return
	}

	w.writeTaskLog(level, formatTaskLogMessage(message), cleanString(payload["timestamp"]))
}

func (w *taskLogWriter) appendState(payload map[string]any) {
	status := strings.ToLower(cleanString(payload["status"]))
	message := cleanString(payload["message"])
	if !w.shouldWriteTaskState(status, message) {
		return
	}

	localizedStatus := status
	switch status {
	case "starting":
		localizedStatus = "启动中"
	case "running":
		localizedStatus = "运行中"
	case "stopping":
		localizedStatus = "停止中"
	case "completed":
		localizedStatus = "已完成"
	case "error":
		localizedStatus = "异常"
	case "stopped":
		localizedStatus = "已终止"
	case "incomplete":
		localizedStatus = "未完成"
	case "idle":
		localizedStatus = "空闲"
	}

	line := fmt.Sprintf("[%s] %s(%s): %s", formatTaskLogLineStamp(cleanString(payload["timestamp"])), taskLogStatePrefix, localizedStatus, formatTaskLogMessage(message))
	_ = w.appendLine(line)
}

func (w *taskLogWriter) shouldWriteTaskState(status string, message string) bool {
	now := time.Now()
	signature := strings.TrimSpace(status) + "|" + strings.TrimSpace(message)

	w.mu.Lock()
	defer w.mu.Unlock()

	if _, isFinalState := finalTaskStates[status]; isFinalState {
		w.lastTaskStateSignature = signature
		w.lastTaskStateAt = now
		return true
	}

	if signature == w.lastTaskStateSignature && !w.lastTaskStateAt.IsZero() && now.Sub(w.lastTaskStateAt) < 2*time.Second {
		return false
	}

	w.lastTaskStateSignature = signature
	w.lastTaskStateAt = now
	return true
}

func (w *taskLogWriter) appendLine(line string) error {
	w.mu.Lock()
	sessionLogPath := w.sessionLogPath
	w.mu.Unlock()

	if sessionLogPath == "" {
		return nil
	}

	payload := strings.TrimRight(line, "\r\n") + "\r\n"
	if err := common.AppendUTF8TextFile(sessionLogPath, payload); err != nil {
		return err
	}

	return nil
}

// pruneTaskLogFiles keeps only the newest `keep` log files of one module
// prefix (by modification time) so one-run-per-file naming cannot grow the
// logs directory without bound.
func pruneTaskLogFiles(logDir string, prefix string, keep int) {
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return
	}
	type logFile struct {
		name    string
		modTime time.Time
	}
	matches := make([]logFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) || !strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		matches = append(matches, logFile{name: filepath.Join(logDir, entry.Name()), modTime: info.ModTime()})
	}
	if len(matches) <= keep {
		return
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].modTime.After(matches[j].modTime)
	})
	for _, stale := range matches[keep:] {
		_ = os.Remove(stale.name)
	}
}

func shouldWriteTaskLogEntry(level string, message string) bool {
	switch level {
	case "info", "warn", "error":
	default:
		return false
	}

	for _, pattern := range taskLogNoisyPatterns {
		if strings.Contains(message, pattern) {
			return false
		}
	}
	return strings.TrimSpace(message) != ""
}

func getLocalizedTaskLogLevel(level string) string {
	if label, ok := taskLogLevelLabels[strings.ToLower(strings.TrimSpace(level))]; ok {
		return label
	}
	return strings.ToUpper(strings.TrimSpace(level))
}

func formatTaskLogMessage(message string) string {
	normalized := localizeTaskLogPrefix(message)
	if normalized == "" {
		return ""
	}
	if strings.HasPrefix(normalized, taskLogKeyPrefix) {
		return normalized
	}
	for _, pattern := range taskLogKeyPatterns {
		if strings.Contains(normalized, pattern) {
			return taskLogKeyPrefix + normalized
		}
	}
	return normalized
}

func localizeTaskLogPrefix(message string) string {
	normalized := strings.TrimSpace(message)
	for sourcePrefix, localizedPrefix := range taskLogPrefixLabels {
		if strings.HasPrefix(normalized, sourcePrefix) {
			return localizedPrefix + strings.TrimSpace(strings.TrimPrefix(normalized, sourcePrefix))
		}
	}
	return normalized
}

func formatTaskLogLineStamp(value string) string {
	if strings.TrimSpace(value) == "" {
		return time.Now().Format("2006-01-02 15:04:05")
	}

	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.Local().Format("2006-01-02 15:04:05")
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.Local().Format("2006-01-02 15:04:05")
	}
	return strings.TrimSpace(value)
}

// buildCrawlConfigLine renders the operator's crawl settings into one compact
// log line so an output folder is self-describing when a troubleshooting
// report comes back without the original form state.
func buildCrawlConfigLine(payload map[string]any) string {
	stringOf := func(key string) string { return strings.TrimSpace(cleanString(payload[key])) }
	onOff := func(key string) string {
		if enabled, ok := payload[key].(bool); ok && enabled {
			return "开"
		}
		return "关"
	}
	numberOr := func(key string, fallback string) string {
		if raw, ok := payload[key].(float64); ok && raw > 0 {
			return fmt.Sprintf("%.0f", raw)
		}
		return fallback
	}

	parts := make([]string, 0, 12)
	if proxy := stringOf("proxy"); proxy != "" {
		parts = append(parts, "代理="+proxy)
	} else {
		// 显式标明直连模式，避免“看起来像漏记了代理”。
		parts = append(parts, "代理=直连（未配置）")
	}
	if limit := numberOr("limit", ""); limit != "" {
		parts = append(parts, "目标数量="+limit)
	}
	if keywords := stringOf("magnetExcludeKeywords"); keywords != "" {
		parts = append(parts, "磁力过滤词="+keywords)
	}
	if releaseDate := stringOf("minReleaseDate"); releaseDate != "" {
		parts = append(parts, "过滤发行日期="+releaseDate)
	}
	if threshold := numberOr("actressCountFilterThreshold", ""); threshold != "" {
		parts = append(parts, "过滤演员数量="+threshold)
	}
	if codes := stringOf("filmCodeFilterThreshold"); codes != "" {
		parts = append(parts, "过滤番号="+codes)
	}
	parts = append(parts,
		"Cloudflare绕过="+onOff("cloudflare"),
		"跳过无磁力影片="+onOff("nomag"),
		"磁力全部爬取="+onOff("allmag"),
		"仅抓取影片信息="+onOff("metadataOnly"),
		"跳过图片下载="+onOff("nopic"),
		"磁力严格校验="+onOff("magnetContentValidation"),
	)
	return strings.Join(parts, " | ")
}
