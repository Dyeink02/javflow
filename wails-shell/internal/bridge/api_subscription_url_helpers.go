package bridge

import (
	"fmt"
	"net/url"
	"strings"
)

// URL fallback helpers are intentionally isolated because subscription target
// guessing bugs are different from lookup-profile or persistence bugs.
//
// Ownership summary:
// 1) derive fallback subscription base URLs and names
// 2) centralize target-URL normalization helpers for subscription flows
// 3) keep URL-guessing rules separate from refresh and persistence logic
//
// File map for maintainers:
// 1) URL/base/name dedupe helpers
// 2) fallback base-origin derivation helpers
// 3) subscription name/URL normalization utilities

func uniqueNonEmptyStrings(values ...string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func buildSubscriptionFallbackBases(targetURL string, preferredBase string) []string {
	return uniqueNonEmptyStrings(
		preferredBase,
		fallbackSubscriptionBase(targetURL),
		"https://www.javbus.com",
		"https://www.busjav.cyou",
		"https://www.fanbus.bond",
		"https://www.cdnbus.bond",
	)
}

func fallbackSubscriptionNameFromURL(targetURL string) string {
	trimmed := strings.TrimSpace(targetURL)
	if trimmed == "" {
		return ""
	}

	parsed, err := url.Parse(trimmed)
	if err == nil {
		segments := nonEmptyURLSegments(parsed.Path)
		if len(segments) >= 2 && strings.EqualFold(segments[len(segments)-2], "star") {
			return fmt.Sprintf("star/%s", segments[len(segments)-1])
		}
		if len(segments) > 0 {
			return fmt.Sprintf("target %s", segments[len(segments)-1])
		}
		if parsed.Host != "" {
			return fmt.Sprintf("target %s", parsed.Host)
		}
	}

	trimmed = strings.TrimRight(trimmed, "/")
	parts := strings.Split(trimmed, "/")
	last := parts[len(parts)-1]
	if last == "" {
		last = trimmed
	}
	return fmt.Sprintf("target %s", last)
}

func fallbackSubscriptionBase(targetURL string) string {
	trimmed := strings.TrimSpace(targetURL)
	if trimmed == "" {
		return ""
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

func nonEmptyURLSegments(pathValue string) []string {
	rawSegments := strings.Split(pathValue, "/")
	segments := make([]string, 0, len(rawSegments))
	for _, segment := range rawSegments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		segments = append(segments, segment)
	}
	return segments
}
