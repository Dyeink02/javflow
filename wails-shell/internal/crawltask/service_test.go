package crawltask

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"javflow/internal/contracts/crawlartifact"
	"javflow/internal/crawlexecution"
	"javflow/internal/crawltaskstate"
	"javflow/internal/runtimecache"
)

type fakeManager struct {
	startCalls int
	startErr   error
	callErr    error
	callResult json.RawMessage
	callFunc   func(ctx context.Context, domain string, action string, payload any) (json.RawMessage, error)
	callCh     chan managerCall
	calls      []managerCall
}

type managerCall struct {
	domain  string
	action  string
	payload map[string]any
}

func (f *fakeManager) Start(ctx context.Context) error {
	f.startCalls++
	return f.startErr
}

func (f *fakeManager) Call(ctx context.Context, domain string, action string, payload any) (json.RawMessage, error) {
	record := managerCall{
		domain: domain,
		action: action,
	}
	if typed, ok := payload.(map[string]any); ok {
		record.payload = clonePayload(typed)
	}
	f.calls = append(f.calls, record)
	if f.callCh != nil {
		f.callCh <- record
	}

	if f.callFunc != nil {
		return f.callFunc(ctx, domain, action, payload)
	}
	if f.callErr != nil {
		return nil, f.callErr
	}
	if len(f.callResult) > 0 {
		return f.callResult, nil
	}
	return json.RawMessage(`{"ok":true}`), nil
}

type fakeOutputStore struct {
	output string
	err    error
}

func (f fakeOutputStore) GetCurrentOutputDir() (string, error) {
	return f.output, f.err
}

func newTestService(manager *fakeManager, outputDir string) *Service {
	return &Service{
		manager:      manager,
		runtimeState: runtimecache.NewState(),
		outputStore:  fakeOutputStore{output: outputDir},
		now: func() time.Time {
			return time.Date(2026, 4, 12, 16, 0, 0, 0, time.Local)
		},
	}
}

func emitState(t *testing.T, service *Service, status string, outputDir string, message string) {
	t.Helper()

	raw, err := json.Marshal(map[string]any{
		"status":    status,
		"outputDir": outputDir,
		"message":   message,
	})
	if err != nil {
		t.Fatalf("marshal crawl state: %v", err)
	}

	service.ObserveEvent("crawl.state", raw)
}

func waitForCall(t *testing.T, ch <-chan managerCall, action string) managerCall {
	t.Helper()

	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()

	for {
		select {
		case record := <-ch:
			if record.action == action {
				return record
			}
		case <-timeout.C:
			t.Fatalf("timed out waiting for action %q", action)
		}
	}
}

func TestStartResolvesIsolatedOutputDirectoryWhenBaseHasArtifacts(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, crawlartifact.CrawlFilmDataFile), []byte("[]"), 0o644); err != nil {
		t.Fatalf("write filmData.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, crawlartifact.DefaultMagnetTxt), []byte("magnet:?xt=urn:btih:test"), 0o644); err != nil {
		t.Fatalf("write magnet-links.txt: %v", err)
	}

	manager := &fakeManager{callCh: make(chan managerCall, 4)}
	service := newTestService(manager, tempDir)

	if _, err := service.Start(context.Background(), map[string]any{
		"output": tempDir,
	}); err != nil {
		t.Fatalf("start crawl: %v", err)
	}

	if manager.startCalls != 1 {
		t.Fatalf("expected manager.Start to be called once, got %d", manager.startCalls)
	}

	record := waitForCall(t, manager.callCh, "start")
	expectedOutput := filepath.Join(tempDir, "run-20260412-160000")

	if record.domain != "crawl" {
		t.Fatalf("unexpected domain: %s", record.domain)
	}
	if got := cleanString(record.payload["output"]); got != expectedOutput {
		t.Fatalf("expected output %q, got %q", expectedOutput, got)
	}
	if got := cleanString(record.payload["outputDir"]); got != expectedOutput {
		t.Fatalf("expected outputDir %q, got %q", expectedOutput, got)
	}
	if got := cleanString(record.payload["currentTaskOutputDir"]); got != expectedOutput {
		t.Fatalf("expected currentTaskOutputDir %q, got %q", expectedOutput, got)
	}
	if resumeExisting, ok := record.payload["resumeExisting"].(bool); !ok || resumeExisting {
		t.Fatalf("expected resumeExisting=false, got %#v", record.payload["resumeExisting"])
	}
	if outputResolved, ok := record.payload["outputResolved"].(bool); !ok || !outputResolved {
		t.Fatalf("expected outputResolved=true, got %#v", record.payload["outputResolved"])
	}
	if goTaskController, ok := record.payload["goTaskController"].(bool); !ok || !goTaskController {
		t.Fatalf("expected goTaskController=true, got %#v", record.payload["goTaskController"])
	}
	executionPlan, ok := record.payload["goExecutionPlan"].(*crawlexecution.RunPlan)
	if !ok || executionPlan == nil {
		t.Fatalf("expected goExecutionPlan, got %#v", record.payload["goExecutionPlan"])
	}
	if executionPlan.ResumePendingFirst {
		t.Fatalf("expected fresh-start plan to skip resume_pending, got %#v", executionPlan)
	}
	if len(executionPlan.PhaseKeys) == 0 || executionPlan.PhaseKeys[0] != "boot" {
		t.Fatalf("unexpected phase keys: %#v", executionPlan.PhaseKeys)
	}
	for _, key := range executionPlan.PhaseKeys {
		if key == "resume_pending" {
			t.Fatalf("fresh start should not include resume_pending, got %#v", executionPlan.PhaseKeys)
		}
	}
	if got := executionPlan.NextPhaseByKey["queue_drain"]; got != "page_gap_recovery" {
		t.Fatalf("expected queue_drain -> page_gap_recovery, got %q", got)
	}
	if executionPlan.StopRedirectPhaseKey != "final_drain" {
		t.Fatalf("expected StopRedirectPhaseKey=final_drain, got %#v", executionPlan.StopRedirectPhaseKey)
	}

	snapshot := service.Snapshot()
	if got := cleanString(snapshot["lastResolutionReason"]); got != "isolated-existing-output" {
		t.Fatalf("expected resolution reason isolated-existing-output, got %q", got)
	}
}

func TestStartResultExposesResolvedRunDirectory(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, crawlartifact.CrawlFilmDataFile), []byte("[]"), 0o644); err != nil {
		t.Fatalf("write filmData.json: %v", err)
	}

	manager := &fakeManager{callResult: json.RawMessage(`{"ok":true,"status":"started"}`)}
	service := newTestService(manager, tempDir)

	raw, err := service.Start(context.Background(), map[string]any{
		"output": tempDir,
	})
	if err != nil {
		t.Fatalf("start crawl: %v", err)
	}

	decoded := map[string]any{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode start result: %v", err)
	}
	expectedOutput := filepath.Join(tempDir, "run-20260412-160000")
	if got := cleanString(decoded["outputDir"]); got != expectedOutput {
		t.Fatalf("expected outputDir %q, got %q in %s", expectedOutput, got, string(raw))
	}
	if got := cleanString(decoded["currentTaskOutputDir"]); got != expectedOutput {
		t.Fatalf("expected currentTaskOutputDir %q, got %q in %s", expectedOutput, got, string(raw))
	}
	if got := cleanString(decoded["baseOutputDir"]); got != tempDir {
		t.Fatalf("expected baseOutputDir %q, got %q in %s", tempDir, got, string(raw))
	}
}

func TestRestartQueuesStopAndResumesFromCurrentOutputDirectory(t *testing.T) {
	tempDir := t.TempDir()
	runDir := filepath.Join(tempDir, "run-20260430-120000")
	manager := &fakeManager{callCh: make(chan managerCall, 8)}
	service := newTestService(manager, tempDir)

	emitState(t, service, "running", runDir, "正在抓取")

	raw, err := service.Restart(context.Background(), map[string]any{
		"output": tempDir,
	})
	if err != nil {
		t.Fatalf("restart crawl: %v", err)
	}
	if !strings.Contains(string(raw), `"restarting":true`) {
		t.Fatalf("expected restarting=true, got %s", string(raw))
	}

	stopRecord := waitForCall(t, manager.callCh, "stop")
	if stopRecord.domain != "crawl" {
		t.Fatalf("unexpected stop domain: %s", stopRecord.domain)
	}

	emitState(t, service, "stopped", runDir, "已停止")
	startRecord := waitForCall(t, manager.callCh, "start")

	if got := cleanString(startRecord.payload["output"]); got != runDir {
		t.Fatalf("expected resumed output %q, got %q", runDir, got)
	}
	if resumeExisting, ok := startRecord.payload["resumeExisting"].(bool); !ok || !resumeExisting {
		t.Fatalf("expected resumeExisting=true, got %#v", startRecord.payload["resumeExisting"])
	}
}

func TestRestartWhileStoppingOnlyRefreshesPendingRestart(t *testing.T) {
	tempDir := t.TempDir()
	runDir := filepath.Join(tempDir, "run-20260430-120000")
	manager := &fakeManager{callCh: make(chan managerCall, 8)}
	service := newTestService(manager, tempDir)

	emitState(t, service, "stopping", runDir, "姝ｅ湪鍋滄")

	raw, err := service.Restart(context.Background(), map[string]any{
		"output": tempDir,
	})
	if err != nil {
		t.Fatalf("restart crawl while stopping: %v", err)
	}
	if !strings.Contains(string(raw), `"restarting":true`) {
		t.Fatalf("expected restarting=true, got %s", string(raw))
	}

	select {
	case record := <-manager.callCh:
		t.Fatalf("expected no extra sidecar call while already stopping, got %#v", record)
	case <-time.After(300 * time.Millisecond):
	}

	snapshot := service.Snapshot()
	if hasPendingRestart, ok := snapshot["hasPendingRestart"].(bool); !ok || !hasPendingRestart {
		t.Fatalf("expected pending restart to be recorded, got %#v", snapshot["hasPendingRestart"])
	}
	if got := cleanString(snapshot["controllerMessage"]); got != crawlexecution.ResolveQueuedRestartControllerMessage() {
		t.Fatalf("expected queued restart controller message, got %q", got)
	}
}

func TestRestartFallsBackToImmediateStartWhenStopSeesSidecarNotStarted(t *testing.T) {
	tempDir := t.TempDir()
	runDir := filepath.Join(tempDir, "run-20260430-120010")
	manager := &fakeManager{
		callCh: make(chan managerCall, 8),
		callFunc: func(ctx context.Context, domain string, action string, payload any) (json.RawMessage, error) {
			if action == "stop" {
				return nil, errors.New("sidecar not started")
			}
			return json.RawMessage(`{"ok":true}`), nil
		},
	}
	service := newTestService(manager, tempDir)

	emitState(t, service, "running", runDir, "姝ｅ湪鎶撳彇")

	raw, err := service.Restart(context.Background(), map[string]any{
		"output": tempDir,
	})
	if err != nil {
		t.Fatalf("restart crawl with not-started fallback: %v", err)
	}
	if !strings.Contains(string(raw), `"restarting":false`) {
		t.Fatalf("expected restarting=false after direct fallback start, got %s", string(raw))
	}

	stopRecord := waitForCall(t, manager.callCh, "stop")
	if stopRecord.domain != "crawl" {
		t.Fatalf("unexpected stop domain: %s", stopRecord.domain)
	}
	startRecord := waitForCall(t, manager.callCh, "start")
	if got := cleanString(startRecord.payload["output"]); got != runDir {
		t.Fatalf("expected fallback restart to reuse output %q, got %q", runDir, got)
	}
}

func TestStopClearsPendingRestartBeforeFinalState(t *testing.T) {
	tempDir := t.TempDir()
	runDir := filepath.Join(tempDir, "run-20260430-120001")
	manager := &fakeManager{callCh: make(chan managerCall, 8)}
	service := newTestService(manager, tempDir)

	emitState(t, service, "running", runDir, "正在抓取")

	if _, err := service.Restart(context.Background(), map[string]any{
		"output": tempDir,
	}); err != nil {
		t.Fatalf("queue restart: %v", err)
	}
	waitForCall(t, manager.callCh, "stop")

	if _, err := service.Stop(context.Background()); err != nil {
		t.Fatalf("stop crawl: %v", err)
	}
	waitForCall(t, manager.callCh, "stop")

	emitState(t, service, "stopped", runDir, "已停止")

	select {
	case record := <-manager.callCh:
		if record.action == "start" {
			t.Fatalf("expected pending restart to be cleared, but got start action")
		}
	case <-time.After(300 * time.Millisecond):
	}
}

func TestStopReturnsAlreadyStoppedWhenInactive(t *testing.T) {
	service := newTestService(&fakeManager{}, t.TempDir())

	raw, err := service.Stop(context.Background())
	if err != nil {
		t.Fatalf("stop crawl: %v", err)
	}
	if string(raw) != `{"stopped":true,"alreadyStopped":true}` {
		t.Fatalf("unexpected stop payload: %s", string(raw))
	}
}

func TestStopTreatsSidecarNotStartedAsAlreadyStopped(t *testing.T) {
	tempDir := t.TempDir()
	runDir := filepath.Join(tempDir, "run-20260430-120020")
	manager := &fakeManager{
		callFunc: func(ctx context.Context, domain string, action string, payload any) (json.RawMessage, error) {
			return nil, errors.New("sidecar not started")
		},
	}
	service := newTestService(manager, tempDir)

	emitState(t, service, "running", runDir, "姝ｅ湪鎶撳彇")

	raw, err := service.Stop(context.Background())
	if err != nil {
		t.Fatalf("stop crawl with not-started fallback: %v", err)
	}
	if string(raw) != `{"stopped":true,"alreadyStopped":true}` {
		t.Fatalf("unexpected stop payload: %s", string(raw))
	}

	snapshot := service.Snapshot()
	if running, ok := snapshot["isRunning"].(bool); !ok || running {
		t.Fatalf("expected service to become inactive, got snapshot %#v", snapshot)
	}
	if got := cleanString(snapshot["controllerMessage"]); got != crawlexecution.ResolveSidecarNotStartedControllerMessage("stop") {
		t.Fatalf("expected stop fallback controller message, got %q", got)
	}
}

func TestSnapshotIncludesExecutionAndControllerMode(t *testing.T) {
	tempDir := t.TempDir()
	manager := &fakeManager{}
	service := newTestService(manager, tempDir)
	runtimeState := service.runtimeState.(*runtimecache.State)
	runtimeState.SetExecutionMode("go-native")
	runtimeState.SetControllerMode("go-task-controller")

	snapshot := service.Snapshot()
	if got := cleanString(snapshot["executionMode"]); got != "go-native" {
		t.Fatalf("expected executionMode go-native, got %q", got)
	}
	if got := cleanString(snapshot["controllerMode"]); got != "go-task-controller" {
		t.Fatalf("expected controllerMode go-task-controller, got %q", got)
	}
}

func TestShutdownStopsActiveTaskAndWaitsForFinalState(t *testing.T) {
	tempDir := t.TempDir()
	runDir := filepath.Join(tempDir, "run-20260430-180000")
	manager := &fakeManager{callCh: make(chan managerCall, 8)}
	service := newTestService(manager, tempDir)

	emitState(t, service, "running", runDir, "正在抓取")

	done := make(chan error, 1)
	go func() {
		done <- service.Shutdown(context.Background())
	}()

	waitForCall(t, manager.callCh, "stop")
	emitState(t, service, "stopped", runDir, "已停止")

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for shutdown")
	}

	snapshot := service.Snapshot()
	if running, ok := snapshot["isRunning"].(bool); !ok || running {
		t.Fatalf("expected shutdown to leave service inactive, got snapshot %#v", snapshot)
	}
	if lastCommand := cleanString(snapshot["lastCommand"]); lastCommand != "shutdown" {
		t.Fatalf("expected last command to be shutdown, got %q", lastCommand)
	}
}

func TestShutdownReturnsNilWhenStopSeesSidecarNotStarted(t *testing.T) {
	tempDir := t.TempDir()
	runDir := filepath.Join(tempDir, "run-20260430-180100")
	manager := &fakeManager{
		callFunc: func(ctx context.Context, domain string, action string, payload any) (json.RawMessage, error) {
			return nil, errors.New("sidecar not started")
		},
	}
	service := newTestService(manager, tempDir)

	emitState(t, service, "running", runDir, "正在抓取")

	if err := service.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown with not-started sidecar returned error: %v", err)
	}

	snapshot := service.Snapshot()
	if running, ok := snapshot["isRunning"].(bool); !ok || running {
		t.Fatalf("expected shutdown fallback to leave service inactive, got snapshot %#v", snapshot)
	}
	if got := cleanString(snapshot["controllerMessage"]); got != crawlexecution.ResolveSidecarNotStartedControllerMessage("stop") {
		t.Fatalf("expected shutdown to reuse stop fallback message, got %q", got)
	}
}

func TestSnapshotIncludesTaskStatePaths(t *testing.T) {
	tempDir := t.TempDir()
	runDir := filepath.Join(tempDir, "run-20260501-100000")
	manager := &fakeManager{}
	service := newTestService(manager, tempDir)

	emitState(t, service, "running", runDir, "姝ｅ湪鎶撳彇")

	snapshot := service.Snapshot()
	taskStatePath := cleanString(snapshot["taskStatePath"])
	validationReportPath := cleanString(snapshot["validationReportPath"])
	runtimeDir := cleanString(snapshot["taskStateRuntimeDir"])

	if taskStatePath == "" || !strings.HasSuffix(taskStatePath, "task-state.json") {
		t.Fatalf("expected taskStatePath, got %#v", snapshot["taskStatePath"])
	}
	if validationReportPath == "" || !strings.HasSuffix(validationReportPath, "validation-report.json") {
		t.Fatalf("expected validationReportPath, got %#v", snapshot["validationReportPath"])
	}
	if runtimeDir == "" {
		t.Fatalf("expected taskStateRuntimeDir, got %#v", snapshot["taskStateRuntimeDir"])
	}
}

func TestSnapshotIncludesTaskStateRestoreSummary(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "runtime-state")
	t.Setenv(crawltaskstate.StateRootEnvName, stateRoot)

	runDir := filepath.Join(t.TempDir(), "run-20260501-100100")
	taskStateManager, err := crawltaskstate.NewManager(runDir, crawltaskstate.ManagerOptions{StateRoot: stateRoot})
	if err != nil {
		t.Fatalf("new task state manager: %v", err)
	}
	if err := taskStateManager.SaveSnapshot(crawltaskstate.Snapshot{
		Status: "running",
		Progress: crawltaskstate.SnapshotProgress{
			NextPageIndex: 6,
			Queued:        8,
			Attempted:     6,
			Completed:     5,
		},
		Links: crawltaskstate.SnapshotLinks{
			Expected:         []string{"https://www.javbus.com/ABP-001", "https://www.javbus.com/ABP-002"},
			Queued:           []string{"https://www.javbus.com/ABP-001", "https://www.javbus.com/ABP-002"},
			Persisted:        []string{"https://www.javbus.com/ABP-001"},
			PersistedFilmIDs: []string{"ABP-001"},
			SkippedIDs:       []string{},
		},
		FailedDetails: []crawltaskstate.FailedDetailRecord{
			{Item: "ABP-002", SourceLink: "https://www.javbus.com/ABP-002", Reason: "failed"},
		},
		PageAudits: []crawltaskstate.PageAuditRecord{},
	}, true); err != nil {
		t.Fatalf("save task state snapshot: %v", err)
	}

	service := newTestService(&fakeManager{}, runDir)
	emitState(t, service, "running", runDir, "姝ｅ湪鎶撳彇")

	snapshot := service.Snapshot()
	if canRestore, ok := snapshot["taskStateCanRestore"].(bool); !ok || !canRestore {
		t.Fatalf("expected taskStateCanRestore=true, got %#v", snapshot["taskStateCanRestore"])
	}
	if pageIndex, ok := snapshot["taskStateRestorePageIndex"].(int); !ok || pageIndex != 6 {
		t.Fatalf("expected taskStateRestorePageIndex=6, got %#v", snapshot["taskStateRestorePageIndex"])
	}
	if pendingCount, ok := snapshot["taskStatePendingDetailCount"].(int); !ok || pendingCount != 1 {
		t.Fatalf("expected taskStatePendingDetailCount=1, got %#v", snapshot["taskStatePendingDetailCount"])
	}
	if failedCount, ok := snapshot["taskStateFailedDetailCount"].(int); !ok || failedCount != 1 {
		t.Fatalf("expected taskStateFailedDetailCount=1, got %#v", snapshot["taskStateFailedDetailCount"])
	}
}

func TestRestartPayloadIncludesRestoreHints(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "runtime-state")
	t.Setenv(crawltaskstate.StateRootEnvName, stateRoot)

	runDir := filepath.Join(t.TempDir(), "run-20260501-100200")
	taskStateManager, err := crawltaskstate.NewManager(runDir, crawltaskstate.ManagerOptions{StateRoot: stateRoot})
	if err != nil {
		t.Fatalf("new task state manager: %v", err)
	}
	if err := taskStateManager.SaveSnapshot(crawltaskstate.Snapshot{
		Status: "running",
		Progress: crawltaskstate.SnapshotProgress{
			NextPageIndex: 4,
			Queued:        5,
			Attempted:     3,
			Completed:     2,
		},
		Links: crawltaskstate.SnapshotLinks{
			Expected:         []string{"https://www.javbus.com/ABP-101", "https://www.javbus.com/ABP-102"},
			Queued:           []string{"https://www.javbus.com/ABP-101", "https://www.javbus.com/ABP-102"},
			Persisted:        []string{"https://www.javbus.com/ABP-101"},
			PersistedFilmIDs: []string{"ABP-101"},
			SkippedIDs:       []string{},
		},
		FailedDetails: []crawltaskstate.FailedDetailRecord{},
		PageAudits:    []crawltaskstate.PageAuditRecord{},
	}, true); err != nil {
		t.Fatalf("save task state snapshot: %v", err)
	}

	manager := &fakeManager{callCh: make(chan managerCall, 4)}
	service := newTestService(manager, runDir)
	raw, err := service.Restart(context.Background(), map[string]any{
		"output": runDir,
	})
	if err != nil {
		t.Fatalf("restart crawl: %v", err)
	}
	if !strings.Contains(string(raw), `"restarting":false`) {
		t.Fatalf("expected inactive restart to return restarting=false, got %s", string(raw))
	}

	record := waitForCall(t, manager.callCh, "start")
	if detected, ok := record.payload["goRestoreStateDetected"].(bool); !ok || !detected {
		t.Fatalf("expected goRestoreStateDetected=true, got %#v", record.payload["goRestoreStateDetected"])
	}
	if pendingCount, ok := record.payload["goRestorePendingCount"].(int); !ok || pendingCount != 1 {
		t.Fatalf("expected goRestorePendingCount=1, got %#v", record.payload["goRestorePendingCount"])
	}
	if pageIndex, ok := record.payload["goRestorePageIndex"].(int); !ok || pageIndex != 4 {
		t.Fatalf("expected goRestorePageIndex=4, got %#v", record.payload["goRestorePageIndex"])
	}
	if message := cleanString(record.payload["goRestoreMessage"]); message == "" {
		t.Fatalf("expected goRestoreMessage, got %#v", record.payload["goRestoreMessage"])
	}
	restoredState, ok := record.payload["goRestoredTaskState"].(*crawltaskstate.RestoredState)
	if !ok || restoredState == nil {
		t.Fatalf("expected goRestoredTaskState, got %#v", record.payload["goRestoredTaskState"])
	}
	if restoredState.PageIndex != 4 {
		t.Fatalf("expected restored state page index=4, got %#v", restoredState.PageIndex)
	}
	if len(restoredState.PendingDetailLinks) != 1 {
		t.Fatalf("expected restored state pending detail count=1, got %#v", restoredState.PendingDetailLinks)
	}
	executionPlan, ok := record.payload["goExecutionPlan"].(*crawlexecution.RunPlan)
	if !ok || executionPlan == nil {
		t.Fatalf("expected goExecutionPlan, got %#v", record.payload["goExecutionPlan"])
	}
	if !executionPlan.ResumePendingFirst {
		t.Fatalf("expected restart plan to resume pending details first, got %#v", executionPlan)
	}
	if len(executionPlan.PhaseKeys) < 3 || executionPlan.PhaseKeys[2] != "resume_pending" {
		t.Fatalf("expected resume_pending as third phase, got %#v", executionPlan.PhaseKeys)
	}
	if got := executionPlan.NextPhaseByKey["detail_recovery"]; got != "final_drain" {
		t.Fatalf("expected detail_recovery -> final_drain, got %q", got)
	}
	if executionPlan.StopRedirectPhaseKey != "final_drain" {
		t.Fatalf("expected StopRedirectPhaseKey=final_drain, got %#v", executionPlan.StopRedirectPhaseKey)
	}
}

func TestRestartPayloadIncludesPersistedOutputState(t *testing.T) {
	runDir := filepath.Join(t.TempDir(), "run-20260501-100300")
	filmDataPath := filepath.Join(runDir, crawlartifact.CrawlFilmDataFile)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	content := `[
  {"title":"ABP-301 title","sourceLink":"https://www.javbus.com/ABP-301"},
  {"title":"ABP-302 title","sourceLink":"https://www.javbus.com/ABP-302"}
]`
	if err := os.WriteFile(filmDataPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write filmData.json: %v", err)
	}

	manager := &fakeManager{callCh: make(chan managerCall, 4)}
	service := newTestService(manager, runDir)
	raw, err := service.Restart(context.Background(), map[string]any{
		"output": runDir,
	})
	if err != nil {
		t.Fatalf("restart crawl: %v", err)
	}
	if !strings.Contains(string(raw), `"restarting":false`) {
		t.Fatalf("expected inactive restart to return restarting=false, got %s", string(raw))
	}

	record := waitForCall(t, manager.callCh, "start")
	persistedState, ok := record.payload["goPersistedOutputState"].(*crawltaskstate.PersistedOutputState)
	if !ok || persistedState == nil {
		t.Fatalf("expected goPersistedOutputState, got %#v", record.payload["goPersistedOutputState"])
	}
	if !persistedState.FilmDataExists {
		t.Fatalf("expected film data exists, got %#v", persistedState)
	}
	if persistedState.RecordCount != 2 {
		t.Fatalf("expected record count 2, got %#v", persistedState.RecordCount)
	}
	if len(persistedState.Records) != 2 {
		t.Fatalf("expected normalized records 2, got %#v", persistedState.Records)
	}
}
