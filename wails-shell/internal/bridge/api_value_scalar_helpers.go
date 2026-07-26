package bridge

import (
	"fmt"
	"strings"
)

// Scalar coercion helpers are shared across most bridge commands. Keeping them
// separate from slice/JSON helpers makes payload parsing easier to scan.
//
// Ownership summary:
// 1) coerce scalar bridge payload values into normalized Go primitives
// 2) centralize lightweight string/bool/int helpers used across commands
// 3) keep scalar parsing separate from domain-specific payload logic
//
// File map for maintainers:
// 1) string/bool coercion helpers
// 2) numeric scalar coercion helpers
// 3) fallback/default scalar utilities

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func nonEmptyString(value any) string {
	return strings.TrimSpace(stringValue(value))
}

func boolValue(value any, fallback bool) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		normalized := strings.ToLower(strings.TrimSpace(typed))
		if normalized == "true" || normalized == "1" || normalized == "yes" || normalized == "on" {
			return true
		}
		if normalized == "false" || normalized == "0" || normalized == "no" || normalized == "off" {
			return false
		}
	case float64:
		return typed != 0
	case int:
		return typed != 0
	}
	return fallback
}

func intValue(value any, fallback int) int {
	switch typed := value.(type) {
	case int:
		if typed != 0 {
			return typed
		}
	case int64:
		if typed != 0 {
			return int(typed)
		}
	case float64:
		if typed != 0 {
			return int(typed)
		}
	case string:
		var parsed int
		if _, err := fmt.Sscanf(strings.TrimSpace(typed), "%d", &parsed); err == nil && parsed != 0 {
			return parsed
		}
	}
	return fallback
}

func safeOptionalInt(value any) *int {
	switch typed := value.(type) {
	case int:
		return &typed
	case int64:
		next := int(typed)
		return &next
	case float64:
		next := int(typed)
		return &next
	case string:
		var parsed int
		if _, err := fmt.Sscanf(strings.TrimSpace(typed), "%d", &parsed); err == nil {
			return &parsed
		}
	}
	return nil
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func cleanAnyString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func floatValue(value any, fallback float64) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case string:
		var parsed float64
		if _, err := fmt.Sscanf(strings.TrimSpace(typed), "%f", &parsed); err == nil {
			return parsed
		}
	}
	return fallback
}
