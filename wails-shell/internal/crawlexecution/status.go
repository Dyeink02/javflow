package crawlexecution

import "strings"

// Status helpers translate raw runner/controller status into the small shared
// vocabulary consumed by bridge/UI/runtime summaries.
//
// Ownership summary:
// 1) normalize runner/controller statuses into one shared vocabulary
// 2) provide shared status-to-label mapping for UI/runtime summaries
// 3) keep status translation centralized across bridge and renderer helpers
//
// File map for maintainers:
// 1) shared runner/controller status vocabulary
// 2) normalization and label lookup helpers
// 3) completion and target-fulfillment wording helpers

var runnerStatusLabels = map[string]string{
	"idle":       "待机",
	"starting":   "启动中",
	"running":    "运行中",
	"stopping":   "停止中",
	"completed":  "已完成",
	"stopped":    "已停止",
	"error":      "异常",
	"incomplete": "未完成",
}

func NormalizeStatus(status string, fallback string) string {
	normalized := strings.ToLower(strings.TrimSpace(status))
	if normalized != "" {
		return normalized
	}

	normalizedFallback := strings.ToLower(strings.TrimSpace(fallback))
	if normalizedFallback != "" {
		return normalizedFallback
	}

	return "idle"
}

func StatusLabel(status string) string {
	normalized := NormalizeStatus(status, "")
	if label, ok := runnerStatusLabels[normalized]; ok {
		return label
	}
	return normalized
}

// CompletionStatusText 统一对外“已完成/未完成”的中文文案。
func CompletionStatusText(completed bool) string {
	if completed {
		return "已完成"
	}
	return "未完成"
}

// TargetFulfillmentText 统一对外“已补齐/未补齐”的中文文案。
func TargetFulfillmentText(shortfall int) string {
	if shortfall <= 0 {
		return "已补齐"
	}
	return "未补齐"
}

func IsActiveStatus(status string) bool {
	switch NormalizeStatus(status, "") {
	case "starting", "running", "stopping":
		return true
	default:
		return false
	}
}
