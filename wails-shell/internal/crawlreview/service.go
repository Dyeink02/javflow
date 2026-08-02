// Package crawlreview keeps the renderer-facing duplicate, unfinished, and filtered review panel.
//
// It is the review/read-model layer for exception lists only. It should not own
// queue execution, crawl result persistence, or quality-summary generation.
//
// Ownership summary:
// 1) keep the crawl review-panel read model
// 2) aggregate duplicate, unfinished, filtered, and failure-detail lists
// 3) keep review projection separate from crawl execution and artifact writes
//
// Boundary rule:
// review classification/projection belongs here, but crawl execution retries,
// queue repair, and artifact persistence must stay outside this package.
//
// File map for maintainers:
// 1) review panel DTOs and defaults
// 2) service state/update methods
// 3) list projection/truncation helpers
package crawlreview

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	EventName       = "crawl.review-panel"
	defaultMaxItems = 80
)

type Panel struct {
	Status               string         `json:"status"`
	Message              string         `json:"message"`
	GeneratedAt          string         `json:"generatedAt"`
	DuplicateItems       []string       `json:"duplicateItems"`
	DuplicateItemsTotal  int            `json:"duplicateItemsTotal"`
	UnfinishedItems      []string       `json:"unfinishedItems"`
	UnfinishedItemsTotal int            `json:"unfinishedItemsTotal"`
	PageGapItems         []string       `json:"pageGapItems"`
	PageGapItemsTotal    int            `json:"pageGapItemsTotal"`
	FilteredItems        []string       `json:"filteredItems"`
	FilteredItemsTotal   int            `json:"filteredItemsTotal"`
	CompletedItems       []string       `json:"completedItems"`
	CompletedItemsTotal  int            `json:"completedItemsTotal"`
	FailedDetails        []FailedDetail `json:"failedDetails"`
	FailedDetailsTotal   int            `json:"failedDetailsTotal"`
}

type FailedDetail struct {
	Item         string `json:"item,omitempty"`
	SourceLink   string `json:"sourceLink,omitempty"`
	Reason       string `json:"reason,omitempty"`
	Category     string `json:"category,omitempty"`
	RetryCount   int    `json:"retryCount,omitempty"`
	RetryAdvice  string `json:"retryAdvice,omitempty"`
	LastFailedAt string `json:"lastFailedAt,omitempty"`
	Recoverable  *bool  `json:"recoverable,omitempty"`
}

// Service keeps the last normalized review panel in Go so the renderer and
// event stream both consume one source of truth. That avoids rebuilding
// duplicate/unfinished/filtered summaries separately in multiple UI paths.
//
// Review scope:
// - duplicate/unfinished/page-gap/filtered item projection
// - failed-detail normalization
// - stable snapshot/signature for renderer hydration
type Service struct {
	// The review panel is shared by the live event stream and renderer bootstrap.
	// Caching the latest structured panel lets the UI restore duplicate/
	// unfinished/filtered/failed diagnostics without waiting for a fresh
	// `crawl.state` event or re-inferring them in JavaScript.
	mu       sync.RWMutex
	maxItems int
	panel    Panel
}

func NewService() *Service {
	return &Service{
		maxItems: defaultMaxItems,
		panel: Panel{
			Status:      "idle",
			GeneratedAt: time.Now().Format(time.RFC3339),
		},
	}
}

func (s *Service) FromRawMessage(rawData json.RawMessage) (Panel, bool) {
	if len(rawData) == 0 || string(rawData) == "null" {
		return Panel{}, false
	}

	payload := map[string]any{}
	if err := json.Unmarshal(rawData, &payload); err != nil {
		return Panel{}, false
	}

	return s.FromPayload(payload), true
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

// FromPayload creates a normalized review panel snapshot without mutating the
// cached service state. Use this when callers need a transient projection only.
func (s *Service) FromPayload(payload map[string]any) Panel {
	maxItems := defaultMaxItems
	if s != nil && s.maxItems > 0 {
		maxItems = s.maxItems
	}

	return buildPanel(payload, Panel{}, maxItems)
}

// ApplyPayload updates the cached review panel used by live event streaming and
// bootstrap rehydration.
func (s *Service) ApplyPayload(payload map[string]any) Panel {
	if s == nil {
		return buildPanel(payload, Panel{}, defaultMaxItems)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	next := buildPanel(payload, s.panel, s.maxItems)
	s.panel = next
	return next
}

func (s *Service) Build() Panel {
	if s == nil {
		return Panel{Status: "idle", GeneratedAt: time.Now().Format(time.RFC3339)}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.panel
}

// buildPanel normalizes raw crawl.state payloads into a renderer-friendly
// review model. It trims, deduplicates, caps list sizes, and fills totals from
// whichever payload shape is currently available.
func buildPanel(payload map[string]any, previous Panel, maxItems int) Panel {
	if maxItems <= 0 {
		maxItems = defaultMaxItems
	}

	statsPayload := mapValue(payload["stats"])
	rawDuplicateItems := stringSliceValue(payload["duplicateItems"])
	rawUnfinishedItems := stringSliceValue(payload["unfinishedItems"])
	rawMissingItems := stringSliceValue(payload["missingItems"])
	rawPageGapItems := stringSliceValue(payload["pageGapItems"])
	rawFilteredItems := firstNonEmptyStringSlice(
		payload["filteredItems"],
		payload["filteredItemIds"],
		statsPayload["filteredItems"],
		statsPayload["filteredItemIds"],
	)
	rawCompletedItems := firstNonEmptyStringSlice(
		payload["completedItems"],
		payload["completedItemIds"],
		statsPayload["completedItems"],
		statsPayload["completedItemIds"],
	)
	rawFailedDetails := failedDetailSliceValue(payload["failedDetails"])

	unfinishedItems := rawUnfinishedItems
	if len(unfinishedItems) == 0 {
		unfinishedItems = rawMissingItems
	}

	panel := Panel{
		Status:               firstNonEmpty(stringValue(payload["status"]), previous.Status, "idle"),
		Message:              firstNonEmpty(stringValue(payload["message"]), previous.Message),
		GeneratedAt:          time.Now().Format(time.RFC3339),
		DuplicateItems:       normalizeStringSlice(rawDuplicateItems, maxItems),
		DuplicateItemsTotal:  firstPositiveInt(intValue(payload["duplicateItemsTotal"]), len(rawDuplicateItems)),
		UnfinishedItems:      normalizeStringSlice(unfinishedItems, maxItems),
		UnfinishedItemsTotal: firstPositiveInt(intValue(payload["unfinishedItemsTotal"]), intValue(payload["missingItemsTotal"]), len(unfinishedItems)),
		PageGapItems:         normalizeStringSlice(rawPageGapItems, maxItems),
		PageGapItemsTotal:    firstPositiveInt(intValue(payload["pageGapItemsTotal"]), len(rawPageGapItems)),
		FilteredItems:        normalizeStringSlice(rawFilteredItems, maxItems),
		FilteredItemsTotal: firstPositiveInt(
			intValue(payload["filteredItemsTotal"]),
			intValue(statsPayload["filteredItemsTotal"]),
			intValue(payload["filteredItemsCount"]),
			intValue(statsPayload["filteredItemsCount"]),
			intValue(payload["filteredByActressCount"]),
			intValue(statsPayload["filteredByActressCount"]),
			len(rawFilteredItems),
		),
		CompletedItems: normalizeStringSlice(rawCompletedItems, maxItems),
		CompletedItemsTotal: firstPositiveInt(
			intValue(payload["completedItemsTotal"]),
			intValue(payload["completedItems"]),
			intValue(statsPayload["completedItemsTotal"]),
			intValue(statsPayload["completedItems"]),
			len(rawCompletedItems),
		),
		FailedDetails:      normalizeFailedDetails(rawFailedDetails, maxItems),
		FailedDetailsTotal: firstPositiveInt(intValue(payload["failedDetailsTotal"]), len(rawFailedDetails)),
	}

	if panel.DuplicateItemsTotal == 0 {
		panel.DuplicateItemsTotal = len(panel.DuplicateItems)
	}
	if panel.UnfinishedItemsTotal == 0 {
		panel.UnfinishedItemsTotal = len(panel.UnfinishedItems)
	}
	if panel.PageGapItemsTotal == 0 {
		panel.PageGapItemsTotal = len(panel.PageGapItems)
	}
	if panel.FilteredItemsTotal == 0 {
		panel.FilteredItemsTotal = len(panel.FilteredItems)
	}
	if panel.CompletedItemsTotal == 0 {
		panel.CompletedItemsTotal = len(panel.CompletedItems)
	}
	if panel.FailedDetailsTotal == 0 {
		panel.FailedDetailsTotal = len(panel.FailedDetails)
	}

	return panel
}

func (s *Service) Signature(panel Panel) string {
	signaturePayload := struct {
		Status               string         `json:"status"`
		Message              string         `json:"message"`
		DuplicateItems       []string       `json:"duplicateItems"`
		DuplicateItemsTotal  int            `json:"duplicateItemsTotal"`
		UnfinishedItems      []string       `json:"unfinishedItems"`
		UnfinishedItemsTotal int            `json:"unfinishedItemsTotal"`
		PageGapItems         []string       `json:"pageGapItems"`
		PageGapItemsTotal    int            `json:"pageGapItemsTotal"`
		FilteredItems        []string       `json:"filteredItems"`
		FilteredItemsTotal   int            `json:"filteredItemsTotal"`
		CompletedItems       []string       `json:"completedItems"`
		CompletedItemsTotal  int            `json:"completedItemsTotal"`
		FailedDetails        []FailedDetail `json:"failedDetails"`
		FailedDetailsTotal   int            `json:"failedDetailsTotal"`
	}{
		Status:               panel.Status,
		Message:              panel.Message,
		DuplicateItems:       panel.DuplicateItems,
		DuplicateItemsTotal:  panel.DuplicateItemsTotal,
		UnfinishedItems:      panel.UnfinishedItems,
		UnfinishedItemsTotal: panel.UnfinishedItemsTotal,
		PageGapItems:         panel.PageGapItems,
		PageGapItemsTotal:    panel.PageGapItemsTotal,
		FilteredItems:        panel.FilteredItems,
		FilteredItemsTotal:   panel.FilteredItemsTotal,
		CompletedItems:       panel.CompletedItems,
		CompletedItemsTotal:  panel.CompletedItemsTotal,
		FailedDetails:        panel.FailedDetails,
		FailedDetailsTotal:   panel.FailedDetailsTotal,
	}

	encoded, err := json.Marshal(signaturePayload)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// normalizeStringSlice keeps review lists trimmed, deduplicated, and stable so
// renderer views do not need to reimplement list hygiene.
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

// normalizeFailedDetails keeps failed-detail rows stable and deduplicated while
// preserving the most useful operator-facing fields.
func normalizeFailedDetails(items []FailedDetail, limit int) []FailedDetail {
	seen := map[string]struct{}{}
	normalized := make([]FailedDetail, 0, minInt(limit, len(items)))

	for _, item := range items {
		itemID := firstNonEmpty(item.Item, item.SourceLink, "unknown")
		reason := strings.TrimSpace(item.Reason)
		signature := itemID + "::" + reason
		if _, exists := seen[signature]; exists {
			continue
		}

		seen[signature] = struct{}{}
		normalized = append(normalized, FailedDetail{
			Item:         strings.TrimSpace(item.Item),
			SourceLink:   strings.TrimSpace(item.SourceLink),
			Reason:       reason,
			Category:     strings.TrimSpace(item.Category),
			RetryCount:   item.RetryCount,
			RetryAdvice:  strings.TrimSpace(item.RetryAdvice),
			LastFailedAt: strings.TrimSpace(item.LastFailedAt),
			Recoverable:  item.Recoverable,
		})

		if limit > 0 && len(normalized) >= limit {
			break
		}
	}

	return normalized
}

func failedDetailSliceValue(value any) []FailedDetail {
	rawItems, ok := value.([]any)
	if !ok {
		if typed, typedOK := value.([]FailedDetail); typedOK {
			return typed
		}
		return nil
	}

	result := make([]FailedDetail, 0, len(rawItems))
	for _, rawItem := range rawItems {
		itemMap, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}

		detail := FailedDetail{
			Item:         stringValue(itemMap["item"]),
			SourceLink:   stringValue(itemMap["sourceLink"]),
			Reason:       stringValue(itemMap["reason"]),
			Category:     stringValue(itemMap["category"]),
			RetryCount:   intValue(itemMap["retryCount"]),
			RetryAdvice:  stringValue(itemMap["retryAdvice"]),
			LastFailedAt: stringValue(itemMap["lastFailedAt"]),
		}

		if recoverable, ok := boolPointerValue(itemMap["recoverable"]); ok {
			detail.Recoverable = recoverable
		}

		result = append(result, detail)
	}

	return result
}

func mapValue(value any) map[string]any {
	typed, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return typed
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

func firstNonEmptyStringSlice(values ...any) []string {
	for _, value := range values {
		items := stringSliceValue(value)
		if len(items) > 0 {
			return items
		}
	}
	return nil
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

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func boolPointerValue(value any) (*bool, bool) {
	typed, ok := value.(bool)
	if !ok {
		return nil, false
	}

	result := typed
	return &result, true
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

func minInt(left int, right int) int {
	if left <= 0 {
		return right
	}
	if right < left {
		return right
	}
	return left
}
