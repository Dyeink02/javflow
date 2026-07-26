package runtimecache

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func rawPayload(t *testing.T, value map[string]any) json.RawMessage {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return payload
}

func TestStateObservesLogContext(t *testing.T) {
	state := NewState()
	logDir := filepath.Join(t.TempDir(), "logs")
	latestLogPath := filepath.Join(logDir, "latest-log.txt")
	sessionLogPath := filepath.Join(logDir, "crawl-session.txt")

	state.ObserveEvent("crawl.log-context", rawPayload(t, map[string]any{
		"logDir":         logDir,
		"sessionLogPath": sessionLogPath,
		"latestLogPath":  latestLogPath,
	}))

	snapshot := state.Snapshot()
	if snapshot["logDir"] != logDir {
		t.Fatalf("expected logDir %s, got %v", logDir, snapshot["logDir"])
	}
	if snapshot["latestLogPath"] != latestLogPath {
		t.Fatalf("expected latestLogPath %s, got %v", latestLogPath, snapshot["latestLogPath"])
	}
}

func TestStateKeepsLastOutputAfterCompletedCrawl(t *testing.T) {
	state := NewState()
	outputDir := filepath.Join(t.TempDir(), "run-output")

	state.ObserveEvent("crawl.state", rawPayload(t, map[string]any{
		"status":    "running",
		"outputDir": outputDir,
		"message":   "running",
	}))
	state.ObserveEvent("crawl.state", rawPayload(t, map[string]any{
		"status":  "completed",
		"message": "completed",
	}))

	snapshot := state.Snapshot()
	if snapshot["currentTaskOutputDir"] != "" {
		t.Fatalf("expected current task output to be cleared, got %v", snapshot["currentTaskOutputDir"])
	}
	if snapshot["lastTaskOutputDir"] != outputDir {
		t.Fatalf("expected last task output %s, got %v", outputDir, snapshot["lastTaskOutputDir"])
	}
}

func TestStateStoresExecutionAndControllerMode(t *testing.T) {
	state := NewState()
	state.SetExecutionMode("go-native")
	state.SetControllerMode("go-task-controller")

	snapshot := state.Snapshot()
	if snapshot["executionMode"] != "go-native" {
		t.Fatalf("expected executionMode go-native, got %v", snapshot["executionMode"])
	}
	if snapshot["controllerMode"] != "go-task-controller" {
		t.Fatalf("expected controllerMode go-task-controller, got %v", snapshot["controllerMode"])
	}

	if snapshot["executionMode"] != "go-native" {
		t.Fatalf("expected snapshot executionMode go-native, got %v", snapshot["executionMode"])
	}
	if snapshot["controllerMode"] != "go-task-controller" {
		t.Fatalf("expected snapshot controllerMode go-task-controller, got %v", snapshot["controllerMode"])
	}
}
