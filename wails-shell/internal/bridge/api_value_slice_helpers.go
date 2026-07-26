package bridge

import (
	"fmt"
	"strings"
)

// Slice/list coercion helpers are separated from scalar parsing because they
// tend to be debugged around textarea/list payload issues.
//
// Ownership summary:
// 1) coerce slice/list bridge payloads into normalized string lists
// 2) centralize textarea/list parsing helpers apart from scalar coercion
// 3) keep list parsing generic and separate from domain-specific payload logic
//
// File map for maintainers:
// 1) list/slice coercion helpers
// 2) textarea/list split and cleanup helpers

func stringSliceValue(value any) []string {
	rawItems, ok := value.([]any)
	if !ok {
		if typed, typedOk := value.([]string); typedOk {
			return typed
		}
		if typed, typedOk := value.(string); typedOk {
			return splitTextList(typed)
		}
		return nil
	}
	result := make([]string, 0, len(rawItems))
	for _, item := range rawItems {
		if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
			result = append(result, text)
		}
	}
	return result
}

func splitTextList(rawValue string) []string {
	fields := strings.FieldsFunc(rawValue, func(r rune) bool {
		return r == '\r' || r == '\n' || r == ',' || r == ';' || r == ' ' || r == '\t'
	})
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		if value := strings.TrimSpace(field); value != "" {
			result = append(result, value)
		}
	}
	return result
}
