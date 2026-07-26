package crawlquality

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"javflow/internal/runtimecache"
)

func rawPayload(t *testing.T, payload map[string]any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return raw
}

func TestAutoReporterWritesReportFromRuntimeCacheOnCompleted(t *testing.T) {
	outputDir := t.TempDir()
	logDir := filepath.Join(outputDir, defaultLogDirName)
	latestLogPath := filepath.Join(logDir, defaultLatestLog)

	writeTestFile(t, filepath.Join(outputDir, defaultMagnetName), "magnet:?xt=urn:btih:AAA&dn=ABC-001\r\n")
	writeTestFile(t, filepath.Join(outputDir, defaultFilmDataName), `[{"title":"ABC-001","magnetLinks":[{"link":"magnet:?xt=urn:btih:AAA"}]}]`)
	writeTestFile(t, latestLogPath, "crawl completed\nsecond validation passed\n")

	state := runtimecache.NewState()
	reporter := NewAutoReporter(NewService(), state, nil)

	state.ObserveEvent("crawl.log-context", rawPayload(t, map[string]any{
		"logDir":        logDir,
		"latestLogPath": latestLogPath,
	}))
	state.ObserveEvent("crawl.state", rawPayload(t, map[string]any{
		"status":    "running",
		"outputDir": outputDir,
	}))

	reporter.ObserveEvent("crawl.state", rawPayload(t, map[string]any{
		"status":  "completed",
		"message": "completed",
	}))

	reportPath := filepath.Join(outputDir, defaultReportName)
	if _, err := os.Stat(reportPath); err != nil {
		t.Fatalf("expected report file: %v", err)
	}
}

func TestAutoReporterEmitsSummaryEventOnCompleted(t *testing.T) {
	outputDir := t.TempDir()
	logDir := filepath.Join(outputDir, defaultLogDirName)
	latestLogPath := filepath.Join(logDir, defaultLatestLog)

	writeTestFile(t, filepath.Join(outputDir, defaultMagnetName), "magnet:?xt=urn:btih:AAA&dn=ABC-001\r\n")
	writeTestFile(t, filepath.Join(outputDir, defaultFilmDataName), `[{"title":"ABC-001","magnetLinks":[{"link":"magnet:?xt=urn:btih:AAA"}]}]`)
	writeTestFile(t, latestLogPath, "crawl completed\n")

	state := runtimecache.NewState()
	reporter := NewAutoReporter(NewService(), state, nil)

	var emittedEventName string
	var emittedSummary Summary
	reporter.emit = func(eventName string, payload any) {
		emittedEventName = eventName
		summary, ok := payload.(Summary)
		if !ok {
			t.Fatalf("expected Summary payload, got %T", payload)
		}
		emittedSummary = summary
	}

	state.ObserveEvent("crawl.log-context", rawPayload(t, map[string]any{
		"logDir":        logDir,
		"latestLogPath": latestLogPath,
	}))
	state.ObserveEvent("crawl.state", rawPayload(t, map[string]any{
		"status":    "running",
		"outputDir": outputDir,
	}))

	reporter.ObserveEvent("crawl.state", rawPayload(t, map[string]any{
		"status":  "completed",
		"message": "completed",
	}))

	if emittedEventName != EventName {
		t.Fatalf("expected event %q, got %q", EventName, emittedEventName)
	}
	if !emittedSummary.Available {
		t.Fatalf("expected emitted summary to be available")
	}
	if emittedSummary.ReportPath == "" {
		t.Fatalf("expected emitted summary report path")
	}
	if emittedSummary.OutputDir != outputDir {
		t.Fatalf("expected output dir %q, got %q", outputDir, emittedSummary.OutputDir)
	}
}

func TestAutoReporterSchedulesFinalRefreshAfterTerminalState(t *testing.T) {
	outputDir := t.TempDir()
	logDir := filepath.Join(outputDir, defaultLogDirName)
	latestLogPath := filepath.Join(logDir, defaultLatestLog)

	writeTestFile(t, filepath.Join(outputDir, defaultMagnetName), "magnet:?xt=urn:btih:AAA&dn=ABC-001\r\n")
	writeTestFile(t, filepath.Join(outputDir, defaultFilmDataName), `[{"title":"ABC-001","magnetLinks":[{"link":"magnet:?xt=urn:btih:AAA"}]}]`)
	writeTestFile(t, latestLogPath, `[2026-05-06 15:48:48] 信息: finalizing output artifacts`+"\n")

	state := runtimecache.NewState()
	reporter := NewAutoReporter(NewService(), state, nil)

	type captured struct {
		statusText string
		completed  bool
	}
	events := make([]captured, 0, 4)
	reporter.emit = func(eventName string, payload any) {
		summary, ok := payload.(Summary)
		if !ok {
			t.Fatalf("expected Summary payload, got %T", payload)
		}
		events = append(events, captured{
			statusText: summary.StatusText,
			completed:  summary.Completed,
		})
	}

	state.ObserveEvent("crawl.log-context", rawPayload(t, map[string]any{
		"logDir":        logDir,
		"latestLogPath": latestLogPath,
	}))
	state.ObserveEvent("crawl.state", rawPayload(t, map[string]any{
		"status":    "running",
		"outputDir": outputDir,
	}))

	reporter.ObserveEvent("crawl.state", rawPayload(t, map[string]any{
		"status":  "incomplete",
		"message": "task incomplete",
	}))

	time.Sleep(150 * time.Millisecond)
	writeTestFile(t, latestLogPath, strings.Join([]string{
		`[2026-05-06 15:45:10] 信息: start`,
		`[2026-05-06 15:48:55] 信息: 结果二次校验通过：输出结果内部一致性正常；目标是否补齐请以最终任务汇总为准。`,
		`[2026-05-06 15:48:55] 信息: [重点] 抓取任务完成，总耗时 225 秒。`,
	}, "\n"))

	time.Sleep(1200 * time.Millisecond)

	if len(events) == 0 {
		t.Fatalf("expected at least one emitted summary")
	}
	if !events[len(events)-1].completed {
		t.Fatalf("expected final refresh to observe completed=true, got %#v", events)
	}
}
