package crawlruncontext

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"javflow/internal/contracts/crawlartifact"
	"javflow/internal/runtimecache"
)

type stubOutputDirProvider struct {
	outputDir string
}

func (s stubOutputDirProvider) GetCurrentOutputDir() (string, error) {
	return s.outputDir, nil
}

type capturedEvent struct {
	name    string
	payload any
}

type stubEmitter struct {
	events []capturedEvent
}

func (s *stubEmitter) Emit(name string, payload any) {
	s.events = append(s.events, capturedEvent{name: name, payload: payload})
}

func TestBuildUsesRuntimeCachePaths(t *testing.T) {
	runtimeState := runtimecache.NewState()
	runtimeState.ApplyIntegrationContext(map[string]any{
		"currentTaskOutputDir": `C:\crawl\run-001`,
		"lastTaskOutputDir":    `C:\crawl\run-000`,
	})
	runtimeState.ApplyLogContext(map[string]any{
		"logDir":         `C:\crawl\run-001\logs`,
		"sessionLogPath": `C:\crawl\run-001\logs\运行日志-20260430-200000.txt`,
		"latestLogPath":  `C:\crawl\run-001\logs\latest-log.txt`,
	})
	runtimeState.ObserveEvent("crawl.state", mustRawMessage(map[string]any{
		"status":    "running",
		"message":   "正在抓取详情页：SOE-927",
		"outputDir": `C:\crawl\run-001`,
	}))

	service := NewService(
		stubOutputDirProvider{outputDir: `C:\fallback`},
		runtimeState,
	)

	context := service.Build()
	if context.PreferredOutputDir != filepath.Clean(`C:\crawl\run-001`) {
		t.Fatalf("expected preferred output dir to be current run dir, got %q", context.PreferredOutputDir)
	}
	if context.PreferredFilmDataPath != filepath.Join(filepath.Clean(`C:\crawl\run-001`), crawlartifact.CrawlFilmDataFile) {
		t.Fatalf("unexpected film data path: %q", context.PreferredFilmDataPath)
	}
	if context.PreferredCrawlProfilePath != filepath.Join(filepath.Clean(`C:\crawl\run-001`), "crawl-profile.json") {
		t.Fatalf("unexpected crawl profile path: %q", context.PreferredCrawlProfilePath)
	}
	if context.PreferredOrganizerCodesPath != filepath.Join(filepath.Clean(`C:\crawl\run-001`), "organizer-codes.json") {
		t.Fatalf("unexpected organizer codes path: %q", context.PreferredOrganizerCodesPath)
	}
	if context.PreferredMagnetPath != filepath.Join(filepath.Clean(`C:\crawl\run-001`), crawlartifact.DefaultMagnetTxt) {
		t.Fatalf("unexpected magnet path: %q", context.PreferredMagnetPath)
	}
	if context.LogDir != filepath.Join(filepath.Clean(`C:\crawl\run-001`), crawlartifact.DefaultLogDirName) {
		t.Fatalf("unexpected log dir: %q", context.LogDir)
	}
	if context.LastStatus != "running" || !context.IsRunning {
		t.Fatalf("expected running context, got status=%q running=%v", context.LastStatus, context.IsRunning)
	}
}

func TestBuildFallsBackToConfiguredOutputDir(t *testing.T) {
	service := NewService(
		stubOutputDirProvider{outputDir: `C:\output`},
		runtimecache.NewState(),
	)

	context := service.Build()
	if context.PreferredOutputDir != filepath.Clean(`C:\output`) {
		t.Fatalf("expected configured output dir, got %q", context.PreferredOutputDir)
	}
	if context.LogDir != filepath.Join(filepath.Clean(`C:\output`), crawlartifact.DefaultLogDirName) {
		t.Fatalf("unexpected fallback log dir: %q", context.LogDir)
	}
	if context.PreferredOrganizerCodesPath != filepath.Join(filepath.Clean(`C:\output`), "organizer-codes.json") {
		t.Fatalf("unexpected fallback organizer codes path: %q", context.PreferredOrganizerCodesPath)
	}
	if context.LatestLogPath != crawlartifact.DefaultLatestLogPath(filepath.Join(filepath.Clean(`C:\output`), crawlartifact.DefaultLogDirName)) {
		t.Fatalf("unexpected fallback latest log path: %q", context.LatestLogPath)
	}
	if context.IsRunning {
		t.Fatalf("expected non-running context")
	}
}

func TestObserverEmitsOnlyWhenContextChanges(t *testing.T) {
	runtimeState := runtimecache.NewState()
	service := NewService(
		stubOutputDirProvider{outputDir: `C:\output`},
		runtimeState,
	)
	emitter := &stubEmitter{}
	observer := NewObserver(service, emitter)

	statePayload := mustRawMessage(map[string]any{
		"status":    "running",
		"message":   "正在抓取详情页：ABP-001",
		"outputDir": `C:\output\run-001`,
	})

	runtimeState.ObserveEvent("crawl.state", statePayload)
	observer.ObserveEvent("crawl.state", statePayload)
	runtimeState.ObserveEvent("crawl.state", statePayload)
	observer.ObserveEvent("crawl.state", statePayload)

	if len(emitter.events) != 1 {
		t.Fatalf("expected exactly 1 emitted event, got %d", len(emitter.events))
	}
	if emitter.events[0].name != EventName {
		t.Fatalf("expected event name %q, got %q", EventName, emitter.events[0].name)
	}
}

func mustRawMessage(payload any) json.RawMessage {
	encoded, _ := json.Marshal(payload)
	return encoded
}
