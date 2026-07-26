// Package crawlstage builds the renderer-facing phase/stage panel read model.
//
// It projects crawl execution into one stable "current phase" panel and should
// not own queue execution, task reconciliation, or result/report decisions.
//
// Ownership summary:
// 1) maintain the stage-panel read model seen by the renderer
// 2) translate execution events into stable phase/status text and counters
// 3) keep stage projection separate from runner/task business logic
//
// Boundary rule:
// this package may read crawl execution metadata, but it must not start owning
// task-control, queue, or artifact-writing behavior.
//
// File map for maintainers:
// 1) stage panel DTOs and defaults
// 2) service state/update methods
// 3) phase/status wording and truncation helpers
package crawlstage

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"javflow/internal/crawlexecution"
)

const EventName = "crawl.stage-panel"

type Stats struct {
	Queued    int `json:"queued"`
	Attempted int `json:"attempted"`
	Completed int `json:"completed"`
	PageIndex int `json:"pageIndex"`
}

type Panel struct {
	Status            string   `json:"status"`
	Message           string   `json:"message"`
	GeneratedAt       string   `json:"generatedAt"`
	OutputDir         string   `json:"outputDir,omitempty"`
	PhaseKey          string   `json:"phaseKey"`
	PhaseTitle        string   `json:"phaseTitle"`
	PhaseDescription  string   `json:"phaseDescription"`
	PhaseIndex        int      `json:"phaseIndex"`
	PhaseTotal        int      `json:"phaseTotal"`
	PhaseProgressText string   `json:"phaseProgressText"`
	PhasePlanKeys     []string `json:"phasePlanKeys,omitempty"`
	IsFinal           bool     `json:"isFinal"`
	Stats             Stats    `json:"stats"`
}

type Service struct {
	mu    sync.RWMutex
	panel Panel
}

// NewService seeds the stage panel with the canonical phase plan. Keep boot
// defaults here so renderer startup and late query reads converge on the same
// initial stage semantics.
func NewService() *Service {
	boot, _ := crawlexecution.Lookup("boot")
	planKeys := crawlexecution.NormalizePhaseKeys(nil)
	total := len(planKeys)

	return &Service{
		panel: Panel{
			Status:            "idle",
			Message:           "等待开始抓取。",
			GeneratedAt:       time.Now().Format(time.RFC3339),
			OutputDir:         "",
			PhaseKey:          boot.Key,
			PhaseTitle:        boot.Title,
			PhaseDescription:  boot.Description,
			PhaseIndex:        1,
			PhaseTotal:        total,
			PhaseProgressText: buildProgressText(1, total),
			PhasePlanKeys:     planKeys,
			IsFinal:           false,
			Stats:             Stats{},
		},
	}
}

func (s *Service) ApplyRawMessage(rawData json.RawMessage) (Panel, bool) {
	if len(rawData) == 0 || string(rawData) == "null" {
		return Panel{}, false
	}

	payload := map[string]any{}
	if err := json.Unmarshal(rawData, &payload); err != nil {
		return Panel{}, false
	}

	return s.ApplyPayload(payload), true
}

// ApplyPayload is the write-side projection from raw crawl progress events into
// the stage panel model. Event parsing should stay here instead of being
// reimplemented in bridge or renderer code.
func (s *Service) ApplyPayload(payload map[string]any) Panel {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := buildPanel(payload, s.panel)
	s.panel = next
	return next
}

// Build returns the latest stage panel snapshot without mutating phase meaning.
func (s *Service) Build() Panel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.panel
}

func (s *Service) Signature(panel Panel) string {
	encoded, err := json.Marshal(panel)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// buildPanel owns the stage-panel shaping rules:
// - status/message normalization
// - phase inference from structured keys or legacy labels
// - progress text derived from the shared execution plan
func buildPanel(payload map[string]any, previous Panel) Panel {
	status := normalizeStatus(payload["status"], previous.Status)
	message := firstNonEmpty(cleanString(payload["message"]), previous.Message)
	outputDir := firstNonEmpty(
		cleanString(payload["outputDir"]),
		cleanString(payload["currentTaskOutputDir"]),
		cleanString(payload["targetOutput"]),
		previous.OutputDir,
	)
	stats := statsValue(payload["stats"])
	phasePlanKeys := phasePlanKeysValue(payload["phasePlanKeys"], previous.PhasePlanKeys)
	structuredPhaseKey := cleanString(payload["phaseKey"])

	phaseKey, phaseTitle := inferPhase(status, message, structuredPhaseKey, phasePlanKeys, previous)
	phaseMetaValue, ok := crawlexecution.Lookup(phaseKey)
	if !ok {
		phaseMetaValue, _ = crawlexecution.Lookup("boot")
		phaseKey = phaseMetaValue.Key
	}

	phaseIndex := crawlexecution.FindPlanIndex(phasePlanKeys, phaseKey)
	if phaseIndex <= 0 {
		phaseIndex = previous.PhaseIndex
	}
	if phaseIndex <= 0 {
		phaseIndex = 1
	}

	description := phaseMetaValue.Description
	if crawlexecution.IsFinalStatus(status) {
		description = finalDescription(status, message)
	}

	total := len(phasePlanKeys)
	if total <= 0 {
		total = crawlexecution.TotalPhases()
	}

	return Panel{
		Status:            status,
		Message:           message,
		GeneratedAt:       time.Now().Format(time.RFC3339),
		OutputDir:         outputDir,
		PhaseKey:          phaseKey,
		PhaseTitle:        phaseTitle,
		PhaseDescription:  description,
		PhaseIndex:        phaseIndex,
		PhaseTotal:        total,
		PhaseProgressText: buildProgressText(phaseIndex, total),
		PhasePlanKeys:     phasePlanKeys,
		IsFinal:           crawlexecution.IsFinalStatus(status),
		Stats:             stats,
	}
}

// inferPhase is the compatibility bridge from mixed progress inputs into one
// canonical phase key/title pair. If stage text and phase plan disagree, debug
// this helper before changing renderer labels.
func inferPhase(status string, message string, structuredPhaseKey string, phasePlanKeys []string, previous Panel) (string, string) {
	if crawlexecution.IsFinalStatus(status) {
		return "final_drain", crawlexecution.FinalTitleForStatus(status)
	}

	if crawlexecution.FindPlanIndex(phasePlanKeys, structuredPhaseKey) > 0 {
		if meta, ok := crawlexecution.Lookup(structuredPhaseKey); ok && meta.Key != "" {
			return structuredPhaseKey, meta.Title
		}
	}

	label := extractStageLabel(message)
	if label != "" {
		if phaseKey := crawlexecution.FindKeyByLabel(label); phaseKey != "" {
			if meta, ok := crawlexecution.Lookup(phaseKey); ok && meta.Key != "" {
				return phaseKey, meta.Title
			}
		}
		return previous.PhaseKey, label
	}

	if phaseKey := crawlexecution.InferKeyFromMessage(message); phaseKey != "" {
		if meta, ok := crawlexecution.Lookup(phaseKey); ok && meta.Key != "" {
			return phaseKey, meta.Title
		}
	}

	switch status {
	case "starting":
		firstPhaseKey := phasePlanKeys[0]
		meta, _ := crawlexecution.Lookup(firstPhaseKey)
		return firstPhaseKey, meta.Title
	case "running":
		if previous.PhaseKey != "" {
			return previous.PhaseKey, previous.PhaseTitle
		}
		firstPhaseKey := phasePlanKeys[0]
		meta, _ := crawlexecution.Lookup(firstPhaseKey)
		return firstPhaseKey, meta.Title
	case "stopping":
		if previous.PhaseKey != "" {
			return previous.PhaseKey, previous.PhaseTitle
		}
		meta, _ := crawlexecution.Lookup("final_drain")
		return "final_drain", meta.Title
	default:
		if previous.PhaseKey != "" {
			return previous.PhaseKey, previous.PhaseTitle
		}
		meta, _ := crawlexecution.Lookup("boot")
		return "boot", meta.Title
	}
}

func extractStageLabel(message string) string {
	const prefix = "当前阶段："

	trimmed := strings.TrimSpace(message)
	if trimmed == "" || !strings.Contains(trimmed, prefix) {
		return ""
	}

	label := strings.TrimSpace(trimmed[strings.Index(trimmed, prefix)+len(prefix):])
	label = strings.TrimSuffix(label, "。")
	label = strings.TrimSuffix(label, ".")
	return strings.TrimSpace(label)
}

func finalDescription(status string, message string) string {
	switch status {
	case "completed":
		return "本次抓取链路已完成，结果入口与复盘数据已就绪。"
	case "stopped":
		return "任务已停止，当前输出可能为部分结果，建议结合复盘摘要继续补抓。"
	case "incomplete":
		return "任务已结束但存在未完成项，建议查看复盘面板与日志。"
	case "error":
		if strings.TrimSpace(message) != "" {
			return "任务异常结束，请结合日志排查：" + strings.TrimSpace(message)
		}
		return "任务异常结束，请结合日志排查。"
	default:
		return ""
	}
}

func buildProgressText(current int, total int) string {
	if total <= 0 {
		total = crawlexecution.TotalPhases()
	}
	if current <= 0 {
		current = 1
	}
	if current > total {
		current = total
	}
	return fmt.Sprintf("阶段 %d/%d", current, total)
}

func phasePlanKeysValue(value any, fallback []string) []string {
	keys := stringSliceValue(value)
	if len(keys) == 0 && len(fallback) > 0 {
		keys = fallback
	}
	return crawlexecution.NormalizePhaseKeys(keys)
}

func stringSliceValue(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string{}, typed...)
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			normalized := cleanString(item)
			if normalized == "" {
				continue
			}
			result = append(result, normalized)
		}
		return result
	default:
		return nil
	}
}

func statsValue(value any) Stats {
	statsMap, ok := value.(map[string]any)
	if !ok {
		return Stats{}
	}

	return Stats{
		Queued:    intValue(statsMap["queued"]),
		Attempted: intValue(statsMap["attempted"]),
		Completed: intValue(statsMap["completed"]),
		PageIndex: intValue(statsMap["pageIndex"]),
	}
}

func normalizeStatus(value any, fallback string) string {
	status := strings.ToLower(cleanString(value))
	if status == "" {
		status = strings.ToLower(strings.TrimSpace(fallback))
	}
	if status == "" {
		return "idle"
	}
	return status
}

func cleanString(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float32:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
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
