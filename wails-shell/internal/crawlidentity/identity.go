package crawlidentity

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Package crawlidentity owns film-code normalization and extraction rules used
// across index parsing, output reconciliation, and organizer/subscription
// artifact flows.
//
// Ownership summary:
// 1) normalize film-code identifiers into one canonical shape
// 2) extract film identity from text and detail URLs
// 3) keep identity rules centralized across crawler/organizer/subscription flows
//
// File map for maintainers:
// 1) film identity regexes and normalization helpers
// 2) text/URL extraction entrypoints
// 3) canonical ID cleanup and comparison utilities

var (
	directFilmIDPattern     = regexp.MustCompile(`([A-Z]{2,12}-?\d{2,8}[A-Z]*)`)
	normalizedFilmIDPattern = regexp.MustCompile(`^([A-Z]{2,12})-?(\d{2,8})([A-Z]*)$`)
)

// NormalizeFilmID is the single normalization rule for film-code text shared by
// crawler output, organizer matching, and subscription identity helpers.
func NormalizeFilmID(rawValue string) string {
	compactValue := strings.ToUpper(strings.TrimSpace(rawValue))
	compactValue = strings.ReplaceAll(compactValue, "_", "-")
	compactValue = strings.Join(strings.Fields(compactValue), "-")
	for strings.Contains(compactValue, "--") {
		compactValue = strings.ReplaceAll(compactValue, "--", "-")
	}

	matches := normalizedFilmIDPattern.FindStringSubmatch(compactValue)
	if len(matches) != 4 {
		return compactValue
	}

	numberPart := strings.TrimSpace(matches[2])
	if parsed, err := strconv.Atoi(numberPart); err == nil {
		if parsed < 100 {
			numberPart = fmt.Sprintf("%03d", parsed)
		} else {
			numberPart = strconv.Itoa(parsed)
		}
	}

	return strings.Trim(strings.Join([]string{matches[1], numberPart + matches[3]}, "-"), "-")
}

// ExtractFilmID prefers direct code text but can also derive identity from the
// trailing segment of a detail-page URL.
func ExtractFilmID(value string) string {
	normalizedValue := strings.ToUpper(value)
	directMatch := directFilmIDPattern.FindStringSubmatch(normalizedValue)
	if len(directMatch) >= 2 {
		return NormalizeFilmID(directMatch[1])
	}

	parsedURL, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return ""
	}

	pathSegments := strings.Split(strings.Trim(parsedURL.Path, "/"), "/")
	if len(pathSegments) == 0 {
		return ""
	}

	lastSegment := strings.ToUpper(pathSegments[len(pathSegments)-1])
	pathMatch := directFilmIDPattern.FindStringSubmatch(lastSegment)
	if len(pathMatch) >= 2 {
		return NormalizeFilmID(pathMatch[1])
	}

	return ""
}

// NormalizeSourceLink reduces source-link noise so cross-run reconciliation does
// not depend on query/hash fragments or trailing slash differences.
func NormalizeSourceLink(sourceLink string) string {
	rawValue := strings.TrimSpace(sourceLink)
	if rawValue == "" {
		return ""
	}

	parsedURL, err := url.Parse(rawValue)
	if err == nil && parsedURL.Scheme != "" && parsedURL.Host != "" {
		normalizedPath := strings.TrimRight(parsedURL.Path, "/")
		if normalizedPath == "" {
			normalizedPath = "/"
		}
		return strings.ToLower(parsedURL.Scheme + "://" + parsedURL.Host + normalizedPath)
	}

	fallback := rawValue
	if index := strings.IndexAny(fallback, "?#"); index >= 0 {
		fallback = fallback[:index]
	}
	return strings.ToLower(strings.TrimRight(fallback, "/"))
}

// NormalizeTitle is the broad fallback text normalizer used when no stable film
// code or source link is available.
func NormalizeTitle(title string) string {
	normalized := strings.ToLower(title)
	builder := strings.Builder{}
	builder.Grow(len(normalized))

	for _, char := range normalized {
		if unicode.IsLetter(char) || unicode.IsDigit(char) || unicode.IsSpace(char) {
			builder.WriteRune(char)
		}
	}

	return strings.Join(strings.Fields(builder.String()), " ")
}
