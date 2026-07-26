// Package crawltask is the unified task-controller layer between the bridge
// and crawl execution.
//
// Maintenance boundary:
// - own command intent and start/stop/restart orchestration
// - own output-directory resolution and task-log/session context
// - own controller-facing snapshot/read-model state
// - do not absorb the crawl state machine itself
//
// Ownership summary:
// 1) expose the unified Go task-controller facade for crawl lifecycle commands
// 2) keep output/log/session/runtime-mode orchestration above the runner layer
// 3) publish controller-facing snapshots without reclaiming runner state-machine ownership
//
// File map for maintainers:
// 1) bridge/runtime dependency interfaces and native controller hooks
// 2) task-controller service state and constructor wiring
// 3) start/stop/restart command orchestration and runtime mode handling
// 4) snapshot/state publication and log/output-session helpers
package crawltask

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"javflow/internal/common"
	"javflow/internal/crawlexecution"
	"javflow/internal/crawltaskstate"
	"javflow/internal/events"
	"javflow/internal/runtimecache"
	"javflow/internal/sidecar"
)

const defaultCommandTimeout = 30 * time.Second

type managerBridge interface {
	Start(ctx context.Context) error
	Call(ctx context.Context, domain string, action string, payload any) (json.RawMessage, error)
}

type outputStore interface {
	GetCurrentOutputDir() (string, error)
}

type modeAwareRuntimeState interface {
	Snapshot() map[string]any
	SetControllerMode(mode string)
}

type nativeStartFunc func(ctx context.Context, payload map[string]any) (json.RawMessage, error)
type nativeStopFunc func(ctx context.Context) (json.RawMessage, error)

type snapshotInputs struct {
	runtimeSnapshot       map[string]any
	taskLogContext        map[string]any
	lastCommand           string
	lastCommandAt         string
	lastCommandErr        string
	controllerStatus      string
	controllerMessage     string
	currentOutputDir      string
	lastResolvedOutputDir string
	lastResolutionReason  string
	lastCreatedRunDir     bool
	pendingRestart        map[string]any
}

// Service is the unified task-controller layer between the Wails bridge and
// the actual crawl execution path.
//
// It owns command intent, output-directory resolution, restart/resume hints,
// controller-facing status, and task-log/session context. It does not own the
// crawl state machine itself; actual crawl execution still lives in
// crawlrunner.Runner.
//
// Practical split:
//   - bridge/UI questions about "what did the controller ask for?" start here
//   - runner questions about "what did the crawl actually execute?" start in
//     `internal/crawlrunner`
//   - artifact review questions start in `internal/crawlquality`
//   - when debugging a "page refreshed but config snapped back" issue, inspect
//     the controller snapshot path here before checking the runner loop
type Service struct {
	manager      managerBridge
	runtimeState modeAwareRuntimeState
	outputStore  outputStore
	bus          *events.Bus
	taskLog      *taskLogWriter
	now          func() time.Time
	nativeStart  nativeStartFunc
	nativeStop   nativeStopFunc

	mu                    sync.RWMutex
	lastCommand           string
	lastCommandAt         string
	lastCommandErr        string
	controllerStatus      string
	controllerMessage     string
	currentOutputDir      string
	lastResolvedOutputDir string
	lastResolutionReason  string
	lastCreatedRunDir     bool
	pendingRestart        map[string]any
	finalStateWaiters     map[chan string]struct{}
}

func NewService(
	manager *sidecar.Manager,
	runtimeState *runtimecache.State,
	outputStore outputStore,
	bus *events.Bus,
) *Service {
	return &Service{
		manager:           manager,
		runtimeState:      runtimeState,
		outputStore:       outputStore,
		bus:               bus,
		taskLog:           newTaskLogWriter(),
		now:               time.Now,
		finalStateWaiters: map[chan string]struct{}{},
	}
}

func (s *Service) SetNativeController(start nativeStartFunc, stop nativeStopFunc) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nativeStart = start
	s.nativeStop = stop
}

// Small scalar/path helpers below intentionally forward to the shared common
// package so the task controller can keep its own call sites readable without
// duplicating normalization rules inline.
func cleanString(value any) string {
	return common.CleanString(value)
}

func boolValue(value any) bool {
	return common.BoolValue(value, false)
}

func firstNonEmpty(values ...string) string {
	return common.FirstNonEmpty(values...)
}

func normalizePath(value string) string {
	return common.NormalizePath(value)
}

func clonePayload(payload map[string]any) map[string]any {
	return common.CloneMap(payload)
}

// isSidecarNotStartedError classifies compatibility-lane startup failures
// without forcing callers to duplicate localized substring checks.
func isSidecarNotStartedError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(strings.TrimSpace(err.Error()))
	if !strings.Contains(message, "sidecar") {
		return false
	}

	return strings.Contains(message, "未启动") ||
		strings.Contains(message, "尚未启动") ||
		strings.Contains(message, "not started")
}

func (s *Service) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		return context.WithTimeout(context.Background(), defaultCommandTimeout)
	}

	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}
	}

	return context.WithTimeout(ctx, defaultCommandTimeout)
}

func (s *Service) rememberCommand(action string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastCommand = strings.TrimSpace(action)
	s.lastCommandAt = time.Now().Format(time.RFC3339)
	if err != nil {
		s.lastCommandErr = strings.TrimSpace(err.Error())
		return
	}
	s.lastCommandErr = ""
}

func (s *Service) runtimeSnapshot() map[string]any {
	if s == nil || s.runtimeState == nil {
		return map[string]any{}
	}
	return s.runtimeState.Snapshot()
}

// loadSnapshotInputsFromRuntimeSnapshot collects controller-owned fields and
// runtime-observed fields into one temporary struct so Snapshot/diagnostics can
// assemble a read model without mutating task execution state.
func (s *Service) loadSnapshotInputsFromRuntimeSnapshot(runtimeSnapshot map[string]any) snapshotInputs {
	inputs := snapshotInputs{
		runtimeSnapshot: runtimeSnapshot,
		taskLogContext:  map[string]any{},
		pendingRestart:  map[string]any{},
	}
	if s != nil && s.taskLog != nil {
		inputs.taskLogContext = s.taskLog.currentContext()
	}

	s.mu.RLock()
	inputs.lastCommand = s.lastCommand
	inputs.lastCommandAt = s.lastCommandAt
	inputs.lastCommandErr = s.lastCommandErr
	inputs.controllerStatus = strings.ToLower(strings.TrimSpace(s.controllerStatus))
	inputs.controllerMessage = s.controllerMessage
	inputs.currentOutputDir = s.currentOutputDir
	inputs.lastResolvedOutputDir = s.lastResolvedOutputDir
	inputs.lastResolutionReason = s.lastResolutionReason
	inputs.lastCreatedRunDir = s.lastCreatedRunDir
	if s.pendingRestart != nil {
		inputs.pendingRestart = clonePayload(s.pendingRestart)
	}
	s.mu.RUnlock()

	return inputs
}

func resolveSnapshotStatus(inputs snapshotInputs) string {
	if inputs.controllerStatus != "" && inputs.controllerStatus != "idle" {
		return inputs.controllerStatus
	}

	runtimeStatus := strings.ToLower(cleanString(inputs.runtimeSnapshot["lastCrawlStatus"]))
	if runtimeStatus != "" {
		return runtimeStatus
	}
	if inputs.controllerStatus != "" {
		return inputs.controllerStatus
	}
	return "idle"
}

func resolveSnapshotMessage(inputs snapshotInputs) string {
	runtimeMessage := cleanString(inputs.runtimeSnapshot["lastCrawlMessage"])
	if !crawlexecution.IsActiveStatus(inputs.controllerStatus) {
		return firstNonEmpty(runtimeMessage, inputs.controllerMessage)
	}
	return firstNonEmpty(inputs.controllerMessage, runtimeMessage)
}

func resolveSnapshotOutputDirs(inputs snapshotInputs) (string, string) {
	currentTaskOutputDir := firstNonEmpty(
		cleanString(inputs.runtimeSnapshot["currentTaskOutputDir"]),
		inputs.currentOutputDir,
	)
	lastTaskOutputDir := firstNonEmpty(
		cleanString(inputs.runtimeSnapshot["lastTaskOutputDir"]),
		inputs.lastResolvedOutputDir,
		inputs.currentOutputDir,
	)
	return currentTaskOutputDir, lastTaskOutputDir
}

// Snapshot assembles the controller-facing state view used by the UI and
// debugging panels. It merges controller-owned state, runtime cache state,
// task-log context, and persisted task-state hints into one lightweight view.
//
// Snapshot should stay a projection layer, not become a second crawl
// execution engine.
func (s *Service) Snapshot() map[string]any {
	// Snapshot is the main read-model entry for the desktop panels. It assembles
	// controller-owned fields with runtime cache fields, but it does not
	// recompute crawl execution state from scratch.
	runtimeSnapshot := s.runtimeSnapshot()
	inputs := s.loadSnapshotInputsFromRuntimeSnapshot(runtimeSnapshot)
	status := resolveSnapshotStatus(inputs)
	message := resolveSnapshotMessage(inputs)
	currentTaskOutputDir, lastTaskOutputDir := resolveSnapshotOutputDirs(inputs)
	taskStateContext := buildTaskStateContext(firstNonEmpty(currentTaskOutputDir, lastTaskOutputDir))

	result := map[string]any{
		"controllerStatus":      status,
		"controllerMode":        firstNonEmpty(cleanString(inputs.runtimeSnapshot["controllerMode"]), "go-task-controller"),
		"executionMode":         cleanString(inputs.runtimeSnapshot["executionMode"]),
		"controllerMessage":     message,
		"isRunning":             crawlexecution.IsActiveStatus(status),
		"hasPendingRestart":     len(inputs.pendingRestart) > 0,
		"pendingRestartOutput":  cleanString(inputs.pendingRestart["output"]),
		"lastCommand":           inputs.lastCommand,
		"lastCommandAt":         inputs.lastCommandAt,
		"lastCommandError":      inputs.lastCommandErr,
		"lastCrawlStatus":       cleanString(inputs.runtimeSnapshot["lastCrawlStatus"]),
		"lastCrawlMessage":      cleanString(inputs.runtimeSnapshot["lastCrawlMessage"]),
		"currentTaskOutputDir":  currentTaskOutputDir,
		"lastTaskOutputDir":     lastTaskOutputDir,
		"logDir":                firstNonEmpty(cleanString(inputs.runtimeSnapshot["logDir"]), cleanString(inputs.taskLogContext["logDir"])),
		"sessionLogPath":        firstNonEmpty(cleanString(inputs.runtimeSnapshot["sessionLogPath"]), cleanString(inputs.taskLogContext["sessionLogPath"])),
		"latestLogPath":         firstNonEmpty(cleanString(inputs.runtimeSnapshot["latestLogPath"]), cleanString(inputs.taskLogContext["latestLogPath"])),
		"taskSessionID":         cleanString(inputs.taskLogContext["sessionId"]),
		"lastResolvedOutputDir": inputs.lastResolvedOutputDir,
		"lastResolutionReason":  inputs.lastResolutionReason,
		"lastCreatedRunDir":     inputs.lastCreatedRunDir,
	}
	for key, value := range taskStateContext {
		result[key] = value
	}
	return result
}

func (s *Service) currentOutputHint() string {
	runtimeSnapshot := s.runtimeSnapshot()

	s.mu.RLock()
	currentOutputDir := s.currentOutputDir
	lastResolvedOutputDir := s.lastResolvedOutputDir
	s.mu.RUnlock()

	return firstNonEmpty(
		cleanString(runtimeSnapshot["currentTaskOutputDir"]),
		currentOutputDir,
		cleanString(runtimeSnapshot["lastTaskOutputDir"]),
		lastResolvedOutputDir,
	)
}

func (s *Service) currentControllerStatus() string {
	runtimeSnapshot := s.runtimeSnapshot()

	s.mu.RLock()
	controllerStatus := strings.ToLower(strings.TrimSpace(s.controllerStatus))
	pendingRestart := len(s.pendingRestart) > 0
	s.mu.RUnlock()

	if crawlexecution.IsActiveStatus(controllerStatus) {
		return controllerStatus
	}

	runtimeStatus := strings.ToLower(cleanString(runtimeSnapshot["lastCrawlStatus"]))
	if crawlexecution.IsActiveStatus(runtimeStatus) {
		return runtimeStatus
	}

	if pendingRestart {
		return "stopping"
	}

	return "idle"
}

func (s *Service) isActive() bool {
	return crawlexecution.IsActiveStatus(s.currentControllerStatus())
}

func (s *Service) resolveDefaultOutputDir(payload map[string]any) (string, error) {
	// Output resolution precedence stays deliberately narrow:
	// 1) explicit request payload
	// 2) controller/runtime hint from the current session
	// 3) persisted settings store
	if outputDir := normalizePath(cleanString(payload["output"])); outputDir != "" {
		return outputDir, nil
	}

	if hint := normalizePath(s.currentOutputHint()); hint != "" {
		return hint, nil
	}

	if s.outputStore != nil {
		outputDir, err := s.outputStore.GetCurrentOutputDir()
		if err != nil {
			return "", err
		}
		if normalized := normalizePath(outputDir); normalized != "" {
			return normalized, nil
		}
	}

	return "", fmt.Errorf("抓取输出目录不能为空")
}

func (s *Service) prepareStartPayload(payload map[string]any) (map[string]any, OutputDirectoryResolution, error) {
	baseOutputDir, err := s.resolveDefaultOutputDir(payload)
	if err != nil {
		return nil, OutputDirectoryResolution{}, err
	}

	resolution := ResolveRunOutputDirectory(baseOutputDir, false, s.now())
	nextPayload := clonePayload(payload)
	nextPayload["output"] = resolution.OutputDir
	nextPayload["outputDir"] = resolution.OutputDir
	nextPayload["currentTaskOutputDir"] = resolution.OutputDir
	nextPayload["outputResolved"] = true
	nextPayload["resumeExisting"] = false
	nextPayload["goTaskController"] = true
	applyExecutionPlanHints(nextPayload, false, nil)
	return nextPayload, resolution, nil
}

// prepareRestartPayload is the restart/resume normalization boundary. It keeps
// persisted-output hints, restore hints, and execution-plan hints aligned so
// callers do not each rebuild resume semantics differently.
func (s *Service) prepareRestartPayload(payload map[string]any) (map[string]any, OutputDirectoryResolution, error) {
	restartOutputDir := firstNonEmpty(
		s.currentOutputHint(),
		cleanString(payload["output"]),
	)
	if restartOutputDir == "" && s.outputStore != nil {
		outputDir, err := s.outputStore.GetCurrentOutputDir()
		if err != nil {
			return nil, OutputDirectoryResolution{}, err
		}
		restartOutputDir = outputDir
	}

	restartOutputDir = normalizePath(restartOutputDir)
	if restartOutputDir == "" {
		return nil, OutputDirectoryResolution{}, fmt.Errorf("没有可继续的抓取输出目录")
	}

	resolution := ResolveRunOutputDirectory(restartOutputDir, true, s.now())
	nextPayload := clonePayload(payload)
	nextPayload["output"] = resolution.OutputDir
	nextPayload["outputDir"] = resolution.OutputDir
	nextPayload["currentTaskOutputDir"] = resolution.OutputDir
	nextPayload["outputResolved"] = true
	nextPayload["resumeExisting"] = true
	nextPayload["goTaskController"] = true
	applyPersistedOutputHints(nextPayload, restartOutputDir)
	inspection := applyRestoreHints(nextPayload, restartOutputDir)
	applyExecutionPlanHints(nextPayload, true, inspection)
	return nextPayload, resolution, nil
}

// callSidecarAction is the legacy compatibility transport. Native Go control
// paths should stay above this function so sidecar startup/timeout behavior is
// isolated in one place.
func (s *Service) callSidecarAction(
	ctx context.Context,
	sidecarAction string,
	rememberAction string,
	payload map[string]any,
	ensureStarted bool,
) (json.RawMessage, error) {
	if s == nil || s.manager == nil {
		err := fmt.Errorf("抓取任务主控服务未初始化")
		if s != nil {
			s.rememberCommand(rememberAction, err)
		}
		return nil, err
	}

	callCtx, cancel := s.withTimeout(ctx)
	defer cancel()

	if ensureStarted {
		if err := s.manager.Start(callCtx); err != nil {
			s.rememberCommand(rememberAction, err)
			return nil, err
		}
	}

	raw, err := s.manager.Call(callCtx, "crawl", sidecarAction, clonePayload(payload))
	s.rememberCommand(rememberAction, err)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func (s *Service) callStartAction(ctx context.Context, rememberAction string, payload map[string]any) (json.RawMessage, error) {
	s.mu.RLock()
	nativeStart := s.nativeStart
	s.mu.RUnlock()
	if nativeStart != nil {
		raw, err := nativeStart(ctx, clonePayload(payload))
		s.rememberCommand(rememberAction, err)
		return raw, err
	}
	return s.callSidecarAction(ctx, "start", rememberAction, payload, true)
}

func (s *Service) callStopAction(ctx context.Context, rememberAction string) (json.RawMessage, error) {
	s.mu.RLock()
	nativeStop := s.nativeStop
	s.mu.RUnlock()
	if nativeStop != nil {
		raw, err := nativeStop(ctx)
		s.rememberCommand(rememberAction, err)
		return raw, err
	}
	return s.callSidecarAction(ctx, "stop", rememberAction, map[string]any{}, false)
}

func (s *Service) beginStart(action string, resolution OutputDirectoryResolution) {
	s.mu.Lock()
	s.controllerStatus = "starting"
	s.controllerMessage = crawlexecution.ResolveControllerStartMessage(action)
	s.currentOutputDir = resolution.OutputDir
	s.lastResolvedOutputDir = resolution.OutputDir
	s.lastResolutionReason = resolution.Reason
	s.lastCreatedRunDir = resolution.CreatedRunDir
	s.mu.Unlock()

	if s.runtimeState != nil {
		s.runtimeState.SetControllerMode("go-task-controller")
	}
}

// initializeTaskLogContext transfers log ownership to the controller for the
// upcoming run and publishes the resulting context once to renderer listeners.
func (s *Service) initializeTaskLogContext(payload map[string]any, resolution OutputDirectoryResolution) error {
	if s == nil || s.taskLog == nil {
		return nil
	}

	contextPayload, err := s.taskLog.initialize(resolution.OutputDir, payload, s.now())
	if err != nil {
		return err
	}

	s.bus.Publish(
		"1",
		"event",
		"crawl.log-context",
		"crawl",
		"log-context",
		"go-controller",
		time.Now().Format(time.RFC3339),
		mustRawJSON(contextPayload),
	)
	s.taskLog.writeTaskLog(
		"info",
		crawlexecution.ResolveTaskLogCreatedMessage(cleanString(contextPayload["sessionLogPath"])),
		time.Now().Format(time.RFC3339),
	)
	return nil
}

func (s *Service) failStart(err error) {
	s.mu.Lock()
	s.controllerStatus = "idle"
	s.controllerMessage = cleanString(err)
	s.currentOutputDir = ""
	s.mu.Unlock()
}

// setControllerState updates the controller-owned lifecycle state. The status
// field is machine-oriented, while the message is operator-facing text shown in
// panels and notices.
func (s *Service) setControllerState(status string, message string, clearCurrentOutput bool) {
	// controllerStatus is the controller's machine-facing lifecycle state,
	// while controllerMessage is the operator-facing explanation shown in UI.
	// Keep them separate so callers do not infer state transitions from prose.
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(status) != "" {
		s.controllerStatus = status
	}
	if strings.TrimSpace(message) != "" {
		s.controllerMessage = message
	}
	if clearCurrentOutput {
		s.currentOutputDir = ""
	}
}

// runStartAction is the write-side launch coordinator: initialize logs, switch
// controller state, emit restart/output notices, then delegate to native or
// sidecar execution.
func (s *Service) runStartAction(
	ctx context.Context,
	action string,
	payload map[string]any,
	resolution OutputDirectoryResolution,
	allowWhileActive bool,
) (json.RawMessage, error) {
	if !allowWhileActive && s.isActive() {
		err := fmt.Errorf("当前已有抓取任务在运行，请先停止后再启动")
		s.rememberCommand(action, err)
		return nil, err
	}

	if err := s.initializeTaskLogContext(payload, resolution); err != nil {
		s.rememberCommand(action, err)
		return nil, err
	}

	s.beginStart(action, resolution)
	if action == "restart" {
		if restoreMessage := cleanString(payload["goRestoreMessage"]); restoreMessage != "" {
			s.emitCrawlLog("info", restoreMessage)
		}
	}
	if resolution.CreatedRunDir {
		s.emitCrawlLog("info", crawlexecution.ResolveOutputRedirectLogMessage(resolution.OutputDir))
	}

	raw, err := s.callStartAction(ctx, action, payload)
	if err != nil {
		s.failStart(err)
		if s.taskLog != nil {
			s.taskLog.writeTaskLog("error", cleanString(err.Error()), time.Now().Format(time.RFC3339))
		}
		return nil, err
	}

	return decorateStartResultWithOutputResolution(raw, resolution), nil
}

func (s *Service) decorateRestartResult(raw json.RawMessage, restarting bool) json.RawMessage {
	payload := map[string]any{
		"ok":         true,
		"restarting": restarting,
	}

	if len(raw) > 0 && string(raw) != "null" {
		decoded := map[string]any{}
		if json.Unmarshal(raw, &decoded) == nil {
			for key, value := range decoded {
				payload[key] = value
			}
		}
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		if len(raw) > 0 {
			return raw
		}
		return json.RawMessage(`{"ok":true}`)
	}
	return encoded
}

// decorateStartResultWithOutputResolution makes the resolved run directory
// visible to callers that need post-run artifact cleanup. This is especially
// important for AV subscription: it starts from a stable module directory, but
// the task controller may redirect the actual crawl into run-YYYYMMDD-HHMMSS.
func decorateStartResultWithOutputResolution(raw json.RawMessage, resolution OutputDirectoryResolution) json.RawMessage {
	payload := map[string]any{
		"ok":                   true,
		"output":               resolution.OutputDir,
		"outputDir":            resolution.OutputDir,
		"currentTaskOutputDir": resolution.OutputDir,
		"baseOutputDir":        resolution.BaseOutputDir,
		"createdRunDir":        resolution.CreatedRunDir,
		"outputResolution":     resolution.Reason,
	}

	if len(raw) > 0 && string(raw) != "null" {
		decoded := map[string]any{}
		if json.Unmarshal(raw, &decoded) == nil {
			for key, value := range decoded {
				payload[key] = value
			}
		}
	}

	// The resolved run directory is authoritative even when the lower runtime
	// returned only `{ok:true}` or `{status:"started"}`.
	payload["output"] = resolution.OutputDir
	payload["outputDir"] = resolution.OutputDir
	payload["currentTaskOutputDir"] = resolution.OutputDir
	payload["baseOutputDir"] = resolution.BaseOutputDir
	payload["createdRunDir"] = resolution.CreatedRunDir
	payload["outputResolution"] = resolution.Reason

	encoded, err := json.Marshal(payload)
	if err != nil {
		if len(raw) > 0 {
			return raw
		}
		return json.RawMessage(`{"ok":true}`)
	}
	return encoded
}

// queueRestart stores the prepared restart payload while the current task is
// still winding down. Actual execution resumes only after a terminal state.
func (s *Service) queueRestart(payload map[string]any, resolution OutputDirectoryResolution) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pendingRestart = clonePayload(payload)
	s.controllerStatus = "stopping"
	s.controllerMessage = crawlexecution.ResolveQueuedRestartControllerMessage()
	s.lastResolvedOutputDir = resolution.OutputDir
	s.lastResolutionReason = resolution.Reason
	s.lastCreatedRunDir = resolution.CreatedRunDir
}

func (s *Service) clearPendingRestart() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingRestart = nil
}

func (s *Service) registerFinalStateWaiter(waiter chan string) {
	if s == nil || waiter == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.finalStateWaiters == nil {
		s.finalStateWaiters = map[chan string]struct{}{}
	}
	s.finalStateWaiters[waiter] = struct{}{}
}

func (s *Service) unregisterFinalStateWaiter(waiter chan string) {
	if s == nil || waiter == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.finalStateWaiters, waiter)
}

// notifyFinalStateWaiters is used by restart/shutdown coordination. Waiters do
// not care about intermediate progress; they only block until a terminal task
// status is observed.
func (s *Service) notifyFinalStateWaiters(status string) {
	// waiters only care about terminal task states; they do not block on intermediate progress.
	// This keeps restart/shutdown coordination narrow and avoids leaking waiter logic
	// into the main crawl execution flow.
	if s == nil {
		return
	}

	normalizedStatus := strings.ToLower(strings.TrimSpace(status))
	if !crawlexecution.IsFinalStatus(normalizedStatus) {
		return
	}

	s.mu.RLock()
	waiters := make([]chan string, 0, len(s.finalStateWaiters))
	for waiter := range s.finalStateWaiters {
		waiters = append(waiters, waiter)
	}
	s.mu.RUnlock()

	for _, waiter := range waiters {
		select {
		case waiter <- normalizedStatus:
		default:
		}
	}
}

// Start is the task-controller write entry for crawl launch. It resolves the
// output directory, initializes task-log ownership, selects native/compat
// execution, and persists controller-facing restart metadata.
func (s *Service) Start(ctx context.Context, payload map[string]any) (json.RawMessage, error) {
	nextPayload, resolution, err := s.prepareStartPayload(payload)
	if err != nil {
		s.rememberCommand("start", err)
		return nil, err
	}
	return s.runStartAction(ctx, "start", nextPayload, resolution, false)
}

func (s *Service) Restart(ctx context.Context, payload map[string]any) (json.RawMessage, error) {
	nextPayload, resolution, err := s.prepareRestartPayload(payload)
	if err != nil {
		s.rememberCommand("restart", err)
		return nil, err
	}

	decision := crawlexecution.ResolveRestartCommandDecision(s.currentControllerStatus())
	if decision.ShouldStartImmediately {
		raw, startErr := s.runStartAction(ctx, "restart", nextPayload, resolution, false)
		if startErr != nil {
			return nil, startErr
		}
		return s.decorateRestartResult(raw, false), nil
	}

	s.queueRestart(nextPayload, resolution)
	if !decision.ShouldRequestStop {
		s.rememberCommand("restart", nil)
		s.emitCrawlLog("info", crawlexecution.ResolveQueuedRestartRefreshLogMessage())
		return json.RawMessage(`{"ok":true,"restarting":true}`), nil
	}

	_, stopErr := s.callStopAction(ctx, "restart")
	if stopErr != nil {
		s.clearPendingRestart()
		if isSidecarNotStartedError(stopErr) {
			fallback := crawlexecution.ResolveSidecarNotStartedFallback("restart")
			if fallback.ShouldResetControllerToIdle {
				s.setControllerState(
					"idle",
					crawlexecution.ResolveSidecarNotStartedControllerMessage("restart"),
					fallback.ClearCurrentOutput,
				)
			}
			if fallback.ShouldStartImmediately {
				raw, startErr := s.runStartAction(ctx, "restart", nextPayload, resolution, false)
				if startErr != nil {
					return nil, startErr
				}
				return s.decorateRestartResult(raw, false), nil
			}
		}
		return nil, stopErr
	}

	s.emitCrawlLog("info", crawlexecution.ResolveRestartStopRequestedLogMessage())
	return json.RawMessage(`{"ok":true,"restarting":true}`), nil
}

// Stop is the controller-side stop boundary. It should only translate user
// stop intent into the active execution path and controller status updates; it
// does not decide final crawl quality by itself.
func (s *Service) Stop(ctx context.Context) (json.RawMessage, error) {
	s.clearPendingRestart()

	decision := crawlexecution.ResolveStopCommandDecision(s.currentControllerStatus())
	if decision.AlreadyStopped {
		s.rememberCommand("stop", nil)
		return json.RawMessage(`{"stopped":true,"alreadyStopped":true}`), nil
	}

	if decision.ShouldMarkStopping {
		s.setControllerState("stopping", crawlexecution.ResolveStopRequestedControllerMessage(), false)
	}

	raw, err := s.callStopAction(ctx, "stop")
	if err != nil && isSidecarNotStartedError(err) {
		fallback := crawlexecution.ResolveSidecarNotStartedFallback("stop")
		if fallback.ShouldResetControllerToIdle {
			s.setControllerState(
				"idle",
				crawlexecution.ResolveSidecarNotStartedControllerMessage("stop"),
				fallback.ClearCurrentOutput,
			)
		}
		if fallback.TreatAsAlreadyStopped {
			s.rememberCommand("stop", nil)
			return json.RawMessage(`{"stopped":true,"alreadyStopped":true}`), nil
		}
	}
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func (s *Service) Shutdown(ctx context.Context) error {
	s.clearPendingRestart()

	decision := crawlexecution.ResolveShutdownCommandDecision(s.currentControllerStatus())
	if decision.AlreadyInactive {
		s.rememberCommand("shutdown", nil)
		return nil
	}

	waitCtx, cancel := s.withTimeout(ctx)
	defer cancel()

	waiter := make(chan string, 1)
	s.registerFinalStateWaiter(waiter)
	defer s.unregisterFinalStateWaiter(waiter)

	s.emitCrawlLog("info", crawlexecution.ResolveShutdownWaitLogMessage())

	if _, err := s.Stop(waitCtx); err != nil {
		return err
	}

	if !decision.ShouldWaitForFinalState || !s.isActive() {
		s.rememberCommand("shutdown", nil)
		return nil
	}

	select {
	case finalStatus := <-waiter:
		s.emitCrawlLog("info", crawlexecution.ResolveShutdownCompletedLogMessage(finalStatus))
		s.rememberCommand("shutdown", nil)
		return nil
	case <-waitCtx.Done():
		return fmt.Errorf("等待抓取任务关停收尾超时：%w", waitCtx.Err())
	}
}

// ObserveEvent keeps the controller read model synchronized with emitted crawl
// events. When UI/status/log context drift appears, this is the first bridge
// layer to inspect before going deeper into runtimecache or runner logic.
func (s *Service) ObserveEvent(eventName string, rawData json.RawMessage) {
	if len(rawData) == 0 || string(rawData) == "null" {
		return
	}

	payload := map[string]any{}
	if err := json.Unmarshal(rawData, &payload); err != nil {
		return
	}

	switch eventName {
	case "crawl.log":
		if s.taskLog != nil {
			s.taskLog.appendLogEntry(payload)
		}
		return
	case "crawl.state":
		if s.taskLog != nil {
			s.taskLog.appendState(payload)
		}
	default:
		return
	}

	status := strings.ToLower(cleanString(payload["status"]))
	if status == "" {
		return
	}

	message := cleanString(payload["message"])
	outputDir := normalizePath(firstNonEmpty(
		cleanString(payload["outputDir"]),
		cleanString(payload["currentTaskOutputDir"]),
		cleanString(payload["targetOutput"]),
	))

	var restartPayload map[string]any
	var transition crawlexecution.ObservedStateTransition

	s.mu.Lock()
	transition = crawlexecution.ResolveObservedStateTransition(status, len(s.pendingRestart) > 0)
	if outputDir != "" {
		s.currentOutputDir = outputDir
		s.lastResolvedOutputDir = outputDir
	}
	if message != "" {
		s.controllerMessage = message
	}
	s.controllerStatus = transition.ControllerStatus
	if transition.ClearCurrentOutput {
		s.currentOutputDir = ""
	}
	if transition.ConsumePendingRestart {
		restartPayload = clonePayload(s.pendingRestart)
		s.pendingRestart = nil
	}
	s.mu.Unlock()

	if transition.NotifyFinalStateWaiter {
		s.notifyFinalStateWaiters(status)
	}

	if restartPayload != nil && transition.ResumePendingRestart {
		s.emitCrawlLog("info", crawlexecution.ResolvePendingRestartResumeLogMessage())
		go s.resumePendingRestart(restartPayload)
		return
	}

	if transition.EmitCompletionNotice && s.bus != nil && message != "" {
		level := transition.CompletionNoticeLevel
		if level == "" {
			level = "warn"
		}
		s.bus.Emit("app.notice", map[string]any{
			"level":     level,
			"message":   message,
			"timestamp": time.Now().Format(time.RFC3339),
		})
	}
}

// resumePendingRestart is intentionally tiny: by the time it runs, all restart
// decisions are already made and it only needs to replay the prepared payload.
func (s *Service) resumePendingRestart(payload map[string]any) {
	outputDir := normalizePath(cleanString(payload["output"]))
	resolution := OutputDirectoryResolution{
		BaseOutputDir:        outputDir,
		OutputDir:            outputDir,
		CreatedRunDir:        false,
		ReusedExistingDir:    true,
		HasExistingArtifacts: false,
		Reason:               "resume-existing",
	}

	_, err := s.runStartAction(context.Background(), "restart", payload, resolution, true)
	if err != nil {
		errorMessage := crawlexecution.ResolvePendingRestartErrorMessage(err)
		s.emitCrawlLog("error", errorMessage)
		if s.bus != nil {
			s.bus.Emit("app.notice", map[string]any{
				"level":     "error",
				"message":   errorMessage,
				"timestamp": time.Now().Format(time.RFC3339),
			})
		}
	}
}

// emitCrawlLog uses the same event shape as runtime crawl logs so the UI does
// not need a separate reader path for controller-originated messages.
func (s *Service) emitCrawlLog(level string, message string) {
	if s == nil || s.bus == nil {
		return
	}

	timestamp := time.Now().Format(time.RFC3339)
	s.bus.Publish(
		"1",
		"event",
		"crawl.log",
		"crawl",
		"log",
		"go-controller",
		timestamp,
		mustRawJSON(map[string]any{
			"level":     strings.TrimSpace(level),
			"message":   strings.TrimSpace(message),
			"timestamp": timestamp,
		}),
	)
}

func mustRawJSON(value any) json.RawMessage {
	payload, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return payload
}

// buildTaskStateContext summarizes persisted task-state artifacts for panel
// display. It intentionally does not decide whether restore should happen.
func buildTaskStateContext(outputDir string) map[string]any {
	// task-state context only summarizes restore artifacts already present on disk.
	// It is a read-model helper for panels and should not make restore decisions.
	normalizedOutputDir := normalizePath(outputDir)
	if normalizedOutputDir == "" {
		return map[string]any{}
	}

	inspection, err := crawltaskstate.Inspect(normalizedOutputDir, crawltaskstate.ManagerOptions{})
	if err != nil {
		return map[string]any{}
	}

	context := map[string]any{
		"taskStateRuntimeDir":         inspection.Paths.RuntimeDir,
		"taskStateBackupDir":          inspection.Paths.BackupDir,
		"taskStatePath":               inspection.Paths.TaskStatePath,
		"validationReportPath":        inspection.Paths.ValidationReportPath,
		"taskStateRuntimeDirExists":   dirExists(inspection.Paths.RuntimeDir),
		"taskStateExists":             inspection.TaskStateExists,
		"validationReportExists":      inspection.ValidationReportExists,
		"taskStateSnapshotStatus":     inspection.SnapshotStatus,
		"taskStateCanRestore":         inspection.CanRestore,
		"taskStateRestorePageIndex":   inspection.RestorePageIndex,
		"taskStatePendingDetailCount": inspection.RestorePendingDetailCount,
		"taskStateFailedDetailCount":  inspection.RestoreFailedDetailCount,
		"taskStateQueuedCount":        inspection.RestoreQueuedCount,
		"taskStateAttemptedCount":     inspection.RestoreAttemptedCount,
		"taskStateCompletedCount":     inspection.RestoreCompletedCount,
		"taskStateRestoreMessage":     inspection.RestoreMessage,
	}
	if inspection.RestoredState != nil {
		context["taskStatePendingDetailLinks"] = inspection.RestoredState.PendingDetailLinks
	}
	return context
}

// applyRestoreHints attaches restore metadata onto the launch payload once so
// downstream native or compatibility execution paths can consume one shared
// resume summary.
func applyRestoreHints(payload map[string]any, outputDir string) *crawltaskstate.Inspection {
	normalizedOutputDir := normalizePath(outputDir)
	if normalizedOutputDir == "" {
		return nil
	}

	inspection, err := crawltaskstate.Inspect(normalizedOutputDir, crawltaskstate.ManagerOptions{})
	if err != nil || !inspection.CanRestore {
		return nil
	}

	payload["goRestoreStateDetected"] = true
	payload["goRestorePageIndex"] = inspection.RestorePageIndex
	payload["goRestorePendingCount"] = inspection.RestorePendingDetailCount
	payload["goRestoreFailedCount"] = inspection.RestoreFailedDetailCount
	payload["goRestoreQueuedCount"] = inspection.RestoreQueuedCount
	payload["goRestoreCompletedCount"] = inspection.RestoreCompletedCount
	payload["goRestoreMessage"] = inspection.RestoreMessage
	payload["goRestoredTaskState"] = inspection.RestoredState
	return &inspection
}

func applyPersistedOutputHints(payload map[string]any, outputDir string) {
	normalizedOutputDir := normalizePath(outputDir)
	if normalizedOutputDir == "" {
		return
	}

	persistedOutputState, err := crawltaskstate.InspectPersistedOutput(normalizedOutputDir)
	if err != nil || !persistedOutputState.FilmDataExists {
		return
	}

	payload["goPersistedOutputState"] = &persistedOutputState
}

// applyExecutionPlanHints describes intended execution shape, such as fresh
// start versus resume-existing versus resume-with-restored-pending-details.
func applyExecutionPlanHints(payload map[string]any, resumeExisting bool, inspection *crawltaskstate.Inspection) {
	// Execution-plan hints describe how this launch should proceed; they are not
	// final crawl state. Downstream code only uses them to distinguish fresh start,
	// resume, and restore-assisted resume paths.
	if payload == nil {
		return
	}

	hasRestoreState := false
	pendingDetailCount := 0
	if inspection != nil && inspection.CanRestore {
		hasRestoreState = true
		pendingDetailCount = inspection.RestorePendingDetailCount
	}

	plan := crawlexecution.BuildRunPlan(crawlexecution.RunPlanOptions{
		ResumeExisting:     resumeExisting,
		HasRestoreState:    hasRestoreState,
		PendingDetailCount: pendingDetailCount,
		SecondValidation:   boolValue(payload["secondValidation"]),
	})
	payload["goExecutionPlan"] = &plan
}

func fileExists(targetPath string) bool {
	info, err := os.Stat(strings.TrimSpace(targetPath))
	return err == nil && !info.IsDir()
}

func dirExists(targetPath string) bool {
	info, err := os.Stat(strings.TrimSpace(targetPath))
	return err == nil && info.IsDir()
}
