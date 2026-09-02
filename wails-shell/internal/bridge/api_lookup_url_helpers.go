package bridge

// Ownership summary:
// 1) build public actress target fallback bases
// 2) keep URL normalization independent from subscription persistence
// 3) provide one lookup-side fallback policy for active bridge callers
//
// File map for maintainers:
// 1) deduplicated fallback base construction
// 2) target-origin parsing helper

import (
	"net/url"
	"strings"
)

// Target lookup shares one fallback-base policy with the subscription V2
// workflow. Keeping it beside lookup code prevents a retired subscription API
// from remaining as a hidden dependency of actress target resolution.
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

func fallbackSubscriptionBase(targetURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(targetURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}
