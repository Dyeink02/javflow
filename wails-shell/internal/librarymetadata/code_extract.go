// Ownership summary:
//
//	This file extracts film codes from filenames and paths for library metadata matching.
//
// File map for maintainers:
//  1. Code extraction result type.
//  2. Regular-expression code patterns.
//  3. Public extraction and normalization helpers.
package librarymetadata

import (
	"path/filepath"
	"regexp"
	"strings"
)

// CodeExtractionResult holds the normalized code plus any suffix tags/part markers.
type CodeExtractionResult struct {
	Code          string
	Tags          string
	Part          string
	candidatePart string
}

var (
	// Common code patterns supported by the scanner.
	// Order matters: more specific patterns should come before generic ones.
	codePatterns = []*regexp.Regexp{
		// FC2 codes: FC2-1234567, FC2PPV-1234567
		regexp.MustCompile(`(?i)\b(FC2(?:PPV)?[-_]?\d{6,})\b`),
		// HEYZO codes: HEYZO-1234
		regexp.MustCompile(`(?i)\b(HEYZO[-_]?\d{3,})\b`),
		// KIN8 codes: KIN8-1675
		regexp.MustCompile(`(?i)\b(KIN8[-_]?\d{3,})\b`),
		// 1pondo / 10musume / Carib style: 123456_999, 543210-001
		regexp.MustCompile(`(?i)\b(\d{6}[_-]\d{2,3})\b`),
		// 300MAAN / 390JAC style: 300MAAN-783, 390JAC-132
		regexp.MustCompile(`(?i)\b(\d{3}[A-Z]{2,4}[-_]?\d{2,4})\b`),
		// Generic studio-number style: SSIS-001, IPZZ-123, BBAN-452, ADN-644
		regexp.MustCompile(`(?i)\b([A-Z]{2,6}[-_]?\d{1,4})\b`),
		// MYWIFE / GETCHU / GCOLLE / PCOLLE codes
		regexp.MustCompile(`(?i)\b(MYWIFE[-_]?\d{3,})\b`),
		regexp.MustCompile(`(?i)\b(GETCHU[-_]?\d{5,})\b`),
		regexp.MustCompile(`(?i)\b(GCOLLE[-_]?\d{5,})\b`),
		regexp.MustCompile(`(?i)\b(PCOLLE[-_]?\d{5,})\b`),
	}

	tagMarkers = []string{"-C", "-c", "_C", "_c", "-CH", "-ch", "_CH", "_ch"}
	partMarker = regexp.MustCompile(`(?i)[-_]?(cd\d+|part\d+|pt\d+)`)
	// Alphabetic, numeric, and duplicate suffixes stay separate from the lookup
	// code so every local split file can share one metadata query.
	// The end anchor is intentional: a token in the middle of a noisy filename
	// must not turn the whole file into a false A/B or duplicate part.
	fixedPartMarker = regexp.MustCompile(`(?i)(_DUP\d+|[-_](?:[AB]|[D-Z])|[-_]\d+)$`)
	// -C is historically used for Chinese subtitles. Keep it as a tag when it
	// appears by itself, but expose it as a candidate so the scanner can promote
	// it to a split part when sibling files prove it belongs to a series.
	contextualCPartMarker = regexp.MustCompile(`(?i)([-_]C)$`)
)

// ExtractCodeFromFilename extracts a JAV code from a media filename.
// It strips known extensions and suffix markers first.
func ExtractCodeFromFilename(filename string) CodeExtractionResult {
	name := filename
	ext := filepath.Ext(name)
	for ext != "" {
		lowerExt := strings.ToLower(ext)
		if lowerExt == ".strm" || lowerExt == ".mp4" || lowerExt == ".mkv" ||
			lowerExt == ".avi" || lowerExt == ".mov" || lowerExt == ".flv" ||
			lowerExt == ".wmv" || lowerExt == ".ts" || lowerExt == ".m4v" ||
			lowerExt == ".iso" {
			name = strings.TrimSuffix(name, ext)
			ext = filepath.Ext(name)
			continue
		}
		break
	}

	// Extract part marker before code matching.
	part := ""
	if match := partMarker.FindStringSubmatch(name); len(match) > 1 {
		part = strings.ToLower(match[1])
		name = partMarker.ReplaceAllString(name, "")
	} else if match := fixedPartMarker.FindStringSubmatch(name); len(match) > 1 {
		candidateName := strings.TrimSuffix(name, match[1])
		if candidateCode := extractNormalizedCode(candidateName); candidateCode != "" {
			part = strings.ToUpper(strings.TrimSpace(match[1]))
			if !strings.HasPrefix(part, "_") {
				part = strings.Trim(part, "-_")
			}
			name = candidateName
		}
	}
	candidatePart := ""
	if part == "" {
		if match := contextualCPartMarker.FindStringSubmatch(name); len(match) > 1 {
			candidateName := strings.TrimSuffix(name, match[1])
			if candidateCode := extractNormalizedCode(candidateName); candidateCode != "" {
				candidatePart = "C"
			}
		}
	}

	// Extract Chinese subtitle / other tags.
	tag := ""
	for _, marker := range tagMarkers {
		if strings.Contains(name, marker) || strings.Contains(strings.ToUpper(name), strings.ToUpper(marker)) {
			tag = "中文字幕"
			name = strings.ReplaceAll(name, marker, "")
			name = strings.ReplaceAll(name, strings.ToLower(marker), "")
			name = strings.ReplaceAll(name, strings.ToUpper(marker), "")
			break
		}
	}

	name = strings.ReplaceAll(name, ".", "-")
	name = strings.ReplaceAll(name, "_", "-")

	if code := extractNormalizedCode(name); code != "" {
		return CodeExtractionResult{Code: code, Tags: tag, Part: part, candidatePart: candidatePart}
	}

	return CodeExtractionResult{Tags: tag, Part: part, candidatePart: candidatePart}
}

func extractNormalizedCode(name string) string {
	for _, pattern := range codePatterns {
		if match := pattern.FindStringSubmatch(name); len(match) > 1 {
			if code := normalizeCode(match[1]); code != "" {
				return code
			}
		}
	}
	return ""
}

var fc2NormalizePattern = regexp.MustCompile(`^FC2(PPV)?-?(\d{6,})$`)

func normalizeCode(raw string) string {
	code := strings.ToUpper(strings.TrimSpace(raw))
	code = strings.ReplaceAll(code, "_", "-")
	code = strings.Trim(code, "-")
	if code == "" {
		return ""
	}
	// Normalize FC2 variants to FC2-PPV-XXXXXXX or FC2-XXXXXXX.
	if m := fc2NormalizePattern.FindStringSubmatch(code); m != nil {
		if m[1] == "PPV" {
			return "FC2-PPV-" + m[2]
		}
		return "FC2-" + m[2]
	}
	return code
}
