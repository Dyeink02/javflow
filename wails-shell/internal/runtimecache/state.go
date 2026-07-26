// Package runtimecache stores last-observed runtime snapshots for bridge/bootstrap read models.
//
// Ownership summary:
// 1) cache lightweight last-observed runtime snapshots for UI bootstrap/readback
// 2) keep transient output/log/mode hints available across bridge queries
// 3) avoid turning this cache into the source of truth for crawl artifacts
//
// File map for maintainers:
// 1) runtime cache state fields and constructor
// 2) observed runtime update methods
// 3) snapshot/export and preferred-path helpers
package runtimecache

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"

	"javflow/internal/crawlexecution"
)

// State stores last-observed runtime snapshots for bootstrap/read-model helpers.
// It is a convenience cache only, not the source of truth for crawler or
// artifact state.
type State struct {
	mu sync.RWMutex

	// These fields mirror observed runtime events. They are intentionally simple
	// snapshots, not the canonical place to derive crawl artifact defaults.
	// Canonical output/log/artifact read models are assembled by
	// crawlruncontext.Service.
	currentTaskOutputDir string
	lastTaskOutputDir    string
	logDir               string
	sessionLogPath       string
	latestLogPath        string
	lastCrawlStatus      string
	lastCrawlMessage     string
	executionMode        string
	controllerMode       string
}

func NewState() *State {
	return &State{}
}

func cleanString(value any) string {
	if value == nil {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func normalizePath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if absolute, err := filepath.Abs(trimmed); err == nil {
		return absolute
	}
	return filepath.Clean(trimmed)
}

// ObserveEvent ingests bus events into the minimal runtime snapshot used by
// bridge/bootstrap read models.
func (s *State) ObserveEvent(eventName string, rawData json.RawMessage) {
	if len(rawData) == 0 || string(rawData) == "null" {
		return
	}

	payload := map[string]any{}
	if err := json.Unmarshal(rawData, &payload); err != nil {
		return
	}

	switch eventName {
	case "crawl.log-context":
		s.observeLogContext(payload)
	case "crawl.state":
		s.observeCrawlState(payload)
	}
}

func (s *State) observeLogContext(payload map[string]any) {
	logDir := normalizePath(cleanString(payload["logDir"]))
	sessionLogPath := normalizePath(cleanString(payload["sessionLogPath"]))
	latestLogPath := normalizePath(cleanString(payload["latestLogPath"]))

	s.mu.Lock()
	defer s.mu.Unlock()

	if logDir != "" {
		s.logDir = logDir
	}
	if sessionLogPath != "" {
		s.sessionLogPath = sessionLogPath
	}
	if latestLogPath != "" {
		s.latestLogPath = latestLogPath
	}
	if s.logDir == "" && s.latestLogPath != "" {
		s.logDir = filepath.Dir(s.latestLogPath)
	}
}

func (s *State) observeCrawlState(payload map[string]any) {
	status := strings.ToLower(cleanString(payload["status"]))
	message := cleanString(payload["message"])
	outputDir := normalizePath(firstNonEmpty(
		cleanString(payload["outputDir"]),
		cleanString(payload["currentTaskOutputDir"]),
		cleanString(payload["targetOutput"]),
	))

	s.mu.Lock()
	defer s.mu.Unlock()

	if status != "" {
		s.lastCrawlStatus = status
	}
	if message != "" {
		s.lastCrawlMessage = message
	}
	if outputDir != "" {
		s.currentTaskOutputDir = outputDir
		s.lastTaskOutputDir = outputDir
	}
	if crawlexecution.IsFinalStatus(status) {
		s.currentTaskOutputDir = ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (s *State) ApplyIntegrationContext(payload map[string]any) {
	currentTaskOutputDir := normalizePath(cleanString(payload["currentTaskOutputDir"]))
	lastTaskOutputDir := normalizePath(cleanString(payload["lastTaskOutputDir"]))
	preferredOutputDir := normalizePath(cleanString(payload["preferredOutputDir"]))

	s.mu.Lock()
	defer s.mu.Unlock()

	if currentTaskOutputDir != "" {
		s.currentTaskOutputDir = currentTaskOutputDir
	}
	if lastTaskOutputDir != "" {
		s.lastTaskOutputDir = lastTaskOutputDir
	}
	if preferredOutputDir != "" {
		if s.lastTaskOutputDir == "" {
			s.lastTaskOutputDir = preferredOutputDir
		}
		if s.currentTaskOutputDir == "" && s.lastCrawlStatus != "completed" {
			s.currentTaskOutputDir = preferredOutputDir
		}
	}
}

func (s *State) ApplyLogContext(payload map[string]any) {
	s.observeLogContext(payload)
}

// Snapshot returns the current bridge-facing runtime read model.
func (s *State) Snapshot() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]any{
		"currentTaskOutputDir": s.currentTaskOutputDir,
		"lastTaskOutputDir":    s.lastTaskOutputDir,
		"logDir":               s.logDir,
		"sessionLogPath":       s.sessionLogPath,
		"latestLogPath":        s.latestLogPath,
		"lastCrawlStatus":      s.lastCrawlStatus,
		"lastCrawlMessage":     s.lastCrawlMessage,
		"executionMode":        s.executionMode,
		"controllerMode":       s.controllerMode,
	}
}

// SetExecutionMode and SetControllerMode let bootstrap code seed mode hints
// before the next crawl-state event arrives.
func (s *State) SetExecutionMode(mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized := strings.TrimSpace(mode)
	if normalized == "" {
		return
	}
	s.executionMode = normalized
}

func (s *State) SetControllerMode(mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized := strings.TrimSpace(mode)
	if normalized == "" {
		return
	}
	s.controllerMode = normalized
}

func (s *State) ExecutionMode() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.executionMode
}

func (s *State) ControllerMode() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.controllerMode
}
