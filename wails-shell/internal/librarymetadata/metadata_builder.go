// Ownership summary:
//   This file builds normalized MovieInfo records from crawler artifacts and metadata results.
//
// File map for maintainers:
//   1) BuildMovieInfoFromCrawlerRecord conversion.
//   2) Title, URL, and actor normalization helpers.
//   3) Provider and cover URL resolution.
//
package librarymetadata

import (
	"net/url"
	"strings"
)

// BuildMovieInfoFromCrawlerRecord converts a crawler artifact record into the
// normalized MovieInfo shape used by NFO writer and image downloader.
func BuildMovieInfoFromCrawlerRecord(record CrawlerRecord) *MovieInfo {
	if record.Code == "" {
		return nil
	}

	title := cleanCrawlerTitle(record.Title, record.Code)
	if title == "" {
		title = record.Code
	}

	// Preserve the original artifact source (e.g. "javbus", "javdb") when
	// available so the UI can show where the local metadata came from.
	provider := record.Source
	if provider == "" {
		provider = "crawler"
	}

	coverURL := normalizeCrawlerAssetURL(record.CoverURL, record.SourceLink)
	info := &MovieInfo{
		Number:      record.Code,
		Title:       title,
		Plot:        "",
		Outline:     "",
		Actors:      dedupeStrings(record.Actors),
		Genres:      dedupeStrings(record.Genres),
		CoverURL:    coverURL,
		BackdropURL: coverURL,
		ThumbURL:    coverURL,
		Provider:    provider,
		Homepage:    record.SourceLink,
	}

	// For local crawler records we usually only have a cover; use it as the
	// landscape/thumb/background fallback as well so image download has
	// something to do. MetaTube's backdrop endpoint uses BigCoverURL -> CoverURL,
	// so falling back to the cover URL matches its behavior.
	if info.CoverURL != "" {
		info.BackdropURL = info.CoverURL
		info.ThumbURL = info.CoverURL
	}

	return info
}

func normalizeCrawlerAssetURL(rawURL, sourceLink string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if strings.HasPrefix(rawURL, "//") {
		return "https:" + rawURL
	}

	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.IsAbs() {
		return rawURL
	}
	base, err := url.Parse(strings.TrimSpace(sourceLink))
	if err != nil || !base.IsAbs() || base.Host == "" {
		return rawURL
	}
	return base.ResolveReference(parsed).String()
}

// cleanCrawlerTitle removes the leading code prefix (e.g. "BBAN-452 - ") that
// many crawler titles include, leaving the human-readable title.
func cleanCrawlerTitle(title, code string) string {
	title = strings.TrimSpace(title)
	code = strings.ToUpper(strings.TrimSpace(code))
	if title == "" || code == "" {
		return title
	}

	upper := strings.ToUpper(title)
	// Common separators after code: space, dash, colon, underscore, etc.
	for _, sep := range []string{" - ", "-", " : ", ": ", ":", " _ ", "_", " ", "\u3000"} {
		if strings.HasPrefix(upper, code+sep) {
			return strings.TrimSpace(title[len(code)+len(sep):])
		}
	}
	return title
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}
