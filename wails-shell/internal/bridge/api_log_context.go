package bridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"javflow/internal/common"
	"javflow/internal/modulelog"
)

// Log context helpers are isolated because filename/output-path diagnostics are
// a separate class of issues from crawl execution and state panels.
//
// Ownership summary:
// 1) initialize and expose the crawl log-context read model
// 2) centralize session/latest log path creation and cache updates
// 3) keep log-context handling separate from crawl execution flow
//
// File map for maintainers:
// 1) task-log initialization and file creation helpers
// 2) runtime cache/log-context update helpers
// 3) query payload shaping for log-context consumers

// initTaskLog initializes one run-log context and feeds the runtime cache/log
// context query path.
func (a *API) initTaskLog(outputDir string, payload map[string]any) {
	now := time.Now()
	logDir := modulelog.Directory(a.runtime.paths.AppPath, modulelog.JAVCrawl)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		a.emitLogEntry("error", fmt.Sprintf("创建日志目录失败：%s", err.Error()))
		return
	}

	sessionID := now.Format("20060102-150405")
	sessionLogPath := modulelog.SessionPath(a.runtime.paths.AppPath, modulelog.JAVCrawl, now)
	latestLogPath := filepath.Join(logDir, "\u8fd0\u884c\u65e5\u5fd7.txt")

	header := fmt.Sprintf(
		"JAV crawl log\r\nstartedAt: %s\r\noutputDir: %s\r\nbaseURL: %s\r\nsearch: %s\r\n------------------------------------------------------------\r\n",
		time.Now().Format("2006-01-02 15:04:05"),
		outputDir,
		resolveCrawlerBaseURL(payload),
		crawlSearchFromPayload(payload),
	)

	if err := common.WriteUTF8TextFile(sessionLogPath, header); err != nil {
		a.emitLogEntry("error", fmt.Sprintf("写入会话日志失败：%s", err.Error()))
	}
	if err := common.WriteUTF8TextFile(latestLogPath, header); err != nil {
		a.emitLogEntry("error", fmt.Sprintf("写入最新日志失败：%s", err.Error()))
	}
	if err := common.AppendUTF8TextFile(modulelog.DailyPath(a.runtime.paths.AppPath, modulelog.JAVCrawl, now), header); err != nil {
		a.emitLogEntry("error", fmt.Sprintf("写入日期日志失败：%s", err.Error()))
	}

	raw, _ := json.Marshal(map[string]any{
		"sessionLogPath": sessionLogPath,
		"latestLogPath":  latestLogPath,
		"logDir":         logDir,
		"sessionId":      sessionID,
	})
	a.runtime.bus.Publish("", "context", "crawl.log-context", "", "", "", time.Now().Format(time.RFC3339), raw)
	a.emitLogEntry("info", fmt.Sprintf("crawl log created: %s", sessionLogPath))
}

// getLogContext is the read-model query for current log/session path metadata.
func (a *API) getLogContext() (map[string]any, error) {
	runContext, err := a.getCrawlRunContext()
	if err != nil {
		return nil, err
	}

	// Expose only the resolved log-path view needed by the renderer. Path
	// creation/rotation still belongs to initTaskLog and crawl artifact helpers.
	return map[string]any{
		"logDir":         runContext.LogDir,
		"sessionLogPath": runContext.SessionLogPath,
		"latestLogPath":  runContext.LatestLogPath,
	}, nil
}
