package crawlconfig

import (
	"math/rand"
	"regexp"
)

// User-agent helpers stay in crawlconfig so request behavior can rotate agents
// without hard-coding browser identity lists across the fetch layer.
//
// Ownership summary:
// 1) centralize the rotating user-agent catalog for crawl requests
// 2) derive browser identity helper headers from one chosen user agent
// 3) keep browser-identity rotation out of request callers
//
// File map for maintainers:
// 1) rotating user-agent catalog
// 2) browser version/header derivation helpers
// 3) sec-ch-ua and browser identity normalization utilities

var UserAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Edge/119.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Edge/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:109.0) Gecko/20100101 Firefox/120.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:109.0) Gecko/20100101 Firefox/121.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.1 Safari/605.1.15",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
}

var (
	browserVersionPattern = regexp.MustCompile(`(?i)(Chrome|Firefox|Edge|Edg)[/\s](\d+)`)
)

// RandomUserAgent and BuildSecChUa keep browser-identity rotation inside
// crawlconfig rather than scattering it across request callers.
func RandomUserAgent() string {
	if len(UserAgents) == 0 {
		return "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36"
	}
	return UserAgents[rand.Intn(len(UserAgents))]
}

func BuildSecChUa(userAgent string) string {
	isChrome := regexp.MustCompile(`(?i)Chrome`).MatchString(userAgent) && !regexp.MustCompile(`(?i)Edge|Edg`).MatchString(userAgent)
	isEdge := regexp.MustCompile(`(?i)Edge|Edg`).MatchString(userAgent)
	isFirefox := regexp.MustCompile(`(?i)Firefox`).MatchString(userAgent)

	browserVersion := "119"
	versionMatches := browserVersionPattern.FindStringSubmatch(userAgent)
	if len(versionMatches) >= 3 {
		browserVersion = versionMatches[2]
	}

	if isChrome {
		return `"Chromium";v="` + browserVersion + `", "Not?A_Brand";v="99"`
	}
	if isEdge {
		return `"Microsoft Edge";v="` + browserVersion + `", "Not?A_Brand";v="99"`
	}
	if isFirefox {
		return `"Not.A/Brand";v="8", "Chromium";v="` + browserVersion + `", "Google Chrome";v="` + browserVersion + `"`
	}

	return `"Chromium";v="` + browserVersion + `", "Not?A_Brand";v="99"`
}
