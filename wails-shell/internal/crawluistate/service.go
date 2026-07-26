// Package crawluistate normalizes raw crawl events into the renderer-facing ui-state panel.
//
// This package owns the small "live status" projection only:
// active items, filtered-item ids, and lightweight counters. It should not grow
// into a second result/stage panel or task-state service.
//
// Boundary rule:
// ui-state should stay lightweight. Richer artifact/review/quality projection
// belongs in dedicated read-model packages instead of accumulating here.
//
// Ownership summary:
// 1) maintain the lightweight crawl ui-state read model
// 2) keep active items and compact counters normalized for the renderer
// 3) avoid growing this package into a second result/review/task projection layer
//
// File map for maintainers:
// 1) ui-state DTOs and defaults
// 2) state normalization/update helpers
// 3) active-item truncation helpers
package crawluistate

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	EventName          = "crawl.ui-state"
	defaultActiveLimit = 12
)

type Stats struct {
	Queued                 int      `json:"queued"`
	Attempted              int      `json:"attempted"`
	Completed              int      `json:"completed"`
	PageIndex              int      `json:"pageIndex"`
	FilteredByActressCount int      `json:"filteredByActressCount"`
	FilteredItemIDs        []string `json:"filteredItemIds,omitempty"`
}

type State struct {
	Status           string   `json:"status"`
	Message          string   `json:"message"`
	GeneratedAt      string   `json:"generatedAt"`
	OutputDir        string   `json:"outputDir,omitempty"`
	ActiveItems      []string `json:"activeItems"`
	ActiveItemsTotal int      `json:"activeItemsTotal"`
	Stats            Stats    `json:"stats"`
}

type Service struct {
	activeLimit int
}

// NewService keeps one central active-item truncation rule for all UI-state
// snapshots so renderer lists and logs do not invent their own limits.
func NewService() *Service {
	return &Service{activeLimit: defaultActiveLimit}
}

func (s *Service) FromRawMessage(rawData json.RawMessage) (State, bool) {
	if len(rawData) == 0 || string(rawData) == "null" {
		return State{}, false
	}

	payload := map[string]any{}
	if err := json.Unmarshal(rawData, &payload); err != nil {
		return State{}, false
	}

	return s.FromPayload(payload), true
}

// FromPayload converts raw crawl events into the normalized ui-state snapshot
// consumed by the renderer shell.
func (s *Service) FromPayload(payload map[string]any) State {
	activeLimit := defaultActiveLimit
	if s != nil && s.activeLimit > 0 {
		activeLimit = s.activeLimit
	}

	rawActiveItems := stringSliceValue(payload["activeItems"])
	stats := statsValue(payload["stats"])

	state := State{
		Status:           stringValue(payload["status"]),
		Message:          stringValue(payload["message"]),
		GeneratedAt:      time.Now().Format(time.RFC3339),
		OutputDir:        firstNonEmpty(stringValue(payload["outputDir"]), stringValue(payload["currentTaskOutputDir"]), stringValue(payload["targetOutput"])),
		ActiveItems:      normalizeStringSlice(rawActiveItems, activeLimit),
		ActiveItemsTotal: firstPositiveInt(intValue(payload["activeItemsTotal"]), len(rawActiveItems)),
		Stats:            stats,
	}

	if state.ActiveItemsTotal == 0 {
		state.ActiveItemsTotal = len(state.ActiveItems)
	}

	return state
}

func (s *Service) Signature(state State) string {
	signaturePayload := struct {
		Status           string   `json:"status"`
		Message          string   `json:"message"`
		OutputDir        string   `json:"outputDir"`
		ActiveItems      []string `json:"activeItems"`
		ActiveItemsTotal int      `json:"activeItemsTotal"`
		Stats            Stats    `json:"stats"`
	}{
		Status:           state.Status,
		Message:          state.Message,
		OutputDir:        state.OutputDir,
		ActiveItems:      state.ActiveItems,
		ActiveItemsTotal: state.ActiveItemsTotal,
		Stats:            state.Stats,
	}

	encoded, err := json.Marshal(signaturePayload)
	if err != nil {
		return ""
	}

	return string(encoded)
}

// normalizeStringSlice keeps ui-state item lists deterministic, deduplicated,
// and capped for renderer consumption.
func normalizeStringSlice(items []string, limit int) []string {
	seen := map[string]struct{}{}
	normalized := make([]string, 0, minInt(limit, len(items)))

	for _, rawItem := range items {
		item := strings.TrimSpace(rawItem)
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}

		seen[item] = struct{}{}
		normalized = append(normalized, item)

		if limit > 0 && len(normalized) >= limit {
			break
		}
	}

	return normalized
}

// statsValue extracts only the lightweight counters that belong in ui-state.
// Richer run quality or artifact interpretation should stay in dedicated
// read-model packages.
func statsValue(value any) Stats {
	statsMap, ok := value.(map[string]any)
	if !ok {
		return Stats{}
	}

	return Stats{
		Queued:                 intValue(statsMap["queued"]),
		Attempted:              intValue(statsMap["attempted"]),
		Completed:              intValue(statsMap["completed"]),
		PageIndex:              intValue(statsMap["pageIndex"]),
		FilteredByActressCount: intValue(statsMap["filteredByActressCount"]),
		FilteredItemIDs:        normalizeStringSlice(stringSliceValue(firstNonNil(statsMap["filteredItemIds"], statsMap["filteredItems"])), 200),
	}
}

func stringSliceValue(value any) []string {
	rawItems, ok := value.([]any)
	if !ok {
		if typed, typedOK := value.([]string); typedOK {
			return typed
		}
		return nil
	}

	result := make([]string, 0, len(rawItems))
	for _, rawItem := range rawItems {
		item := strings.TrimSpace(anyStringValue(rawItem))
		if item != "" {
			result = append(result, item)
		}
	}

	return result
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

func stringValue(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func anyStringValue(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(fmt.Sprint(value), "\r", " "), "\n", " "))
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func minInt(left int, right int) int {
	if left <= 0 {
		return right
	}
	if right < left {
		return right
	}
	return left
}
