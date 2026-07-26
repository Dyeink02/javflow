package crawlquality

import (
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"javflow/internal/events"
	"javflow/internal/runtimecache"
)

// autoreport.go owns live quality-summary emission derived from runtime state
// plus crawlquality service output.
//
// Ownership summary:
// 1) emit live quality-summary projections from runtime state plus quality service data
// 2) suppress duplicate emissions through signature tracking
// 3) keep auto-report fanout separate from crawlquality core calculations
//
// File map for maintainers:
// 1) auto-report event names and emitter types
// 2) signature-tracked reporter state
// 3) runtime event observation and emit gating
//
// This file is fanout glue only. If summary math is wrong, inspect
// `service.go`; if only live UI emission timing is wrong, inspect here.

const EventName = "crawl.quality-summary"

type emitFunc func(eventName string, payload any)

type AutoReporter struct {
	service *Service
	state   *runtimecache.State
	emit    emitFunc

	mu            sync.Mutex
	lastSignature string
	lastFinalKey  string
}

func NewAutoReporter(service *Service, state *runtimecache.State, bus *events.Bus) *AutoReporter {
	if service == nil || state == nil {
		return nil
	}

	var emitter emitFunc
	if bus != nil {
		emitter = bus.Emit
	}

	return &AutoReporter{
		service: service,
		state:   state,
		emit:    emitter,
	}
}

func (r *AutoReporter) ObserveEvent(eventName string, rawData json.RawMessage) {
	if r == nil || r.service == nil || r.state == nil {
		return
	}
	if eventName != "crawl.state" || len(rawData) == 0 || string(rawData) == "null" {
		return
	}

	payload := map[string]any{}
	if err := json.Unmarshal(rawData, &payload); err != nil {
		return
	}

	status := strings.ToLower(strings.TrimSpace(stringValue(payload["status"])))
	if !isTerminalStatus(status) {
		return
	}

	snapshot := r.state.Snapshot()
	outputDir := cleanPath(firstNonEmpty(
		stringValue(payload["outputDir"]),
		stringValue(payload["currentTaskOutputDir"]),
		stringValue(payload["targetOutput"]),
		stringValue(snapshot["lastTaskOutputDir"]),
		stringValue(snapshot["currentTaskOutputDir"]),
	))
	logDir := cleanPath(firstNonEmpty(
		stringValue(payload["logDir"]),
		stringValue(snapshot["logDir"]),
	))
	latestLogPath := cleanPath(firstNonEmpty(
		stringValue(payload["latestLogPath"]),
		stringValue(snapshot["latestLogPath"]),
	))
	signature := strings.Join([]string{
		status,
		outputDir,
		latestLogPath,
		strings.TrimSpace(stringValue(payload["message"])),
	}, "|")

	r.mu.Lock()
	if signature == r.lastSignature {
		r.mu.Unlock()
		return
	}
	r.lastSignature = signature
	r.mu.Unlock()

	summary, err := r.service.Summarize(Options{
		OutputDir:     outputDir,
		LogDir:        logDir,
		LatestLogPath: latestLogPath,
		WriteReport:   true,
	})
	if err != nil {
		log.Printf("crawlquality autoreport failed: %v", err)
		return
	}

	if r.emit != nil && summary.Available {
		r.emit(EventName, summary)
	}

	if isTerminalStatus(status) {
		r.scheduleFinalRefresh(status, outputDir, logDir, latestLogPath, stringValue(payload["message"]))
	}
}

func (r *AutoReporter) scheduleFinalRefresh(status string, outputDir string, logDir string, latestLogPath string, message string) {
	finalKey := strings.Join([]string{
		strings.ToLower(strings.TrimSpace(status)),
		cleanPath(outputDir),
		cleanPath(latestLogPath),
		strings.TrimSpace(message),
	}, "|")
	if finalKey == "|||" {
		return
	}

	r.mu.Lock()
	r.lastFinalKey = finalKey
	r.mu.Unlock()

	go func(expectedKey string) {
		time.Sleep(900 * time.Millisecond)

		r.mu.Lock()
		if r.lastFinalKey != expectedKey {
			r.mu.Unlock()
			return
		}
		r.mu.Unlock()

		summary, err := r.service.Summarize(Options{
			OutputDir:     outputDir,
			LogDir:        logDir,
			LatestLogPath: latestLogPath,
			WriteReport:   true,
		})
		if err != nil {
			log.Printf("crawlquality autoreport final refresh failed: %v", err)
			return
		}

		if r.emit != nil && summary.Available {
			r.emit(EventName, summary)
		}
	}(finalKey)
}

func stringValue(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func isTerminalStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "error", "stopped", "incomplete":
		return true
	default:
		return false
	}
}
