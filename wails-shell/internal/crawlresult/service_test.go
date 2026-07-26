package crawlresult

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"javflow/internal/contracts/crawlartifact"
	"javflow/internal/crawlquality"
	"javflow/internal/crawlruncontext"
	"javflow/internal/runtimecache"
)

type stubOutputDirProvider struct {
	outputDir string
}

func (s stubOutputDirProvider) GetCurrentOutputDir() (string, error) {
	return s.outputDir, nil
}

func writeTestFile(t *testing.T, filePath string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(filePath), err)
	}
	if err := os.WriteFile(filePath, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", filePath, err)
	}
}

func TestBuildUsesRunContextAndQualitySummary(t *testing.T) {
	outputDir := t.TempDir()
	writeTestFile(t, filepath.Join(outputDir, crawlartifact.DefaultMagnetTxt), "magnet:?xt=urn:btih:AAA&dn=ABC-001\r\n")
	writeTestFile(t, filepath.Join(outputDir, crawlartifact.CrawlFilmDataFile), `[{"title":"ABC-001","magnetLinks":[{"link":"magnet:?xt=urn:btih:AAA"}]}]`)
	writeTestFile(t, crawlartifact.DefaultLatestLogPath(filepath.Join(outputDir, crawlartifact.DefaultLogDirName)), `[2026-04-30 20:04:04] 信息: [重点] 抓取任务已完成，已二次校验完成。`)

	runtimeState := runtimecache.NewState()
	runtimeState.ApplyIntegrationContext(map[string]any{
		"lastTaskOutputDir": outputDir,
	})
	runtimeState.ApplyLogContext(map[string]any{
		"logDir":         filepath.Join(outputDir, crawlartifact.DefaultLogDirName),
		"latestLogPath":  crawlartifact.DefaultLatestLogPath(filepath.Join(outputDir, crawlartifact.DefaultLogDirName)),
		"sessionLogPath": filepath.Join(outputDir, "logs", "运行日志-20260430-200404.txt"),
	})
	runtimeState.ObserveEvent("crawl.state", mustRawMessage(map[string]any{
		"status":  "completed",
		"message": "抓取任务已完成，已二次校验完成。",
	}))

	runContext := crawlruncontext.NewService(
		stubOutputDirProvider{outputDir: outputDir},
		runtimeState,
	)
	service := NewService(crawlquality.NewService(), runContext)

	panel := service.Build()
	if panel.OutputDir != outputDir {
		t.Fatalf("expected output dir %q, got %q", outputDir, panel.OutputDir)
	}
	if !panel.MagnetExists || !panel.FilmDataExists || !panel.LatestLogExists {
		t.Fatalf("expected result files to exist: %#v", panel)
	}
	if !panel.QualityAvailable {
		t.Fatalf("expected quality summary to be available")
	}
}

func TestApplyPayloadWritesReportOnTerminalStatus(t *testing.T) {
	outputDir := t.TempDir()
	writeTestFile(t, filepath.Join(outputDir, crawlartifact.DefaultMagnetTxt), "magnet:?xt=urn:btih:AAA&dn=ABC-001\r\n")
	writeTestFile(t, filepath.Join(outputDir, crawlartifact.CrawlFilmDataFile), `[{"title":"ABC-001","magnetLinks":[{"link":"magnet:?xt=urn:btih:AAA"}]}]`)
	writeTestFile(t, crawlartifact.DefaultLatestLogPath(filepath.Join(outputDir, crawlartifact.DefaultLogDirName)), `[2026-04-30 20:04:04] 信息: [重点] 抓取任务已完成，已二次校验完成。`)

	runtimeState := runtimecache.NewState()
	runtimeState.ApplyIntegrationContext(map[string]any{
		"lastTaskOutputDir": outputDir,
	})
	runtimeState.ApplyLogContext(map[string]any{
		"logDir":        filepath.Join(outputDir, crawlartifact.DefaultLogDirName),
		"latestLogPath": crawlartifact.DefaultLatestLogPath(filepath.Join(outputDir, crawlartifact.DefaultLogDirName)),
	})

	runContext := crawlruncontext.NewService(
		stubOutputDirProvider{outputDir: outputDir},
		runtimeState,
	)
	service := NewService(crawlquality.NewService(), runContext)

	panel := service.ApplyPayload(map[string]any{
		"status":  "completed",
		"message": "抓取任务已完成。",
	})

	if !panel.ReportExists {
		t.Fatalf("expected report to exist: %#v", panel)
	}
}

func TestObserverEmitsOnlyWhenResultChanges(t *testing.T) {
	service := NewService(nil, nil)
	emitter := &stubEmitter{}
	observer := NewObserver(service, emitter)

	rawPayload := mustRawMessage(map[string]any{
		"status":  "running",
		"message": "当前阶段：抓取索引页。",
	})

	observer.ObserveEvent("crawl.state", rawPayload)
	observer.ObserveEvent("crawl.state", rawPayload)

	if len(emitter.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(emitter.events))
	}
	if emitter.events[0].name != EventName {
		t.Fatalf("unexpected event name: %q", emitter.events[0].name)
	}
}

type stubEmitter struct {
	events []capturedEvent
}

type capturedEvent struct {
	name    string
	payload any
}

func (s *stubEmitter) Emit(name string, payload any) {
	s.events = append(s.events, capturedEvent{name: name, payload: payload})
}

func mustRawMessage(payload any) json.RawMessage {
	encoded, _ := json.Marshal(payload)
	return encoded
}
