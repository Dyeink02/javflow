package organizer

import (
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// code_rules.go owns organizer-side film-code normalization and matching rules.
//
// Ownership summary:
// 1) normalize video extensions and film-code tokens
// 2) provide one filename-to-code extraction path for organizer phases
// 3) keep code-rule heuristics centralized for scan/transfer/report reuse
//
// File map for maintainers:
// 1) regex/prefix/video-extension constants
// 2) code normalization and filename cleaning helpers
// 3) code extraction / duplicate sort helpers

var (
	domainNoisePattern       = regexp.MustCompile(`(?i)https?://\S+|[a-z0-9-]+\.(com|net|org|cn|cc|tv|xyz|me|vip|top)`)
	nonAlphaNumericPattern   = regexp.MustCompile(`[^A-Z0-9]+`)
	standardCodePattern      = regexp.MustCompile(`(?i)([A-Z]{2,12})[-_ ]*([0-9]{2,8})`)
	compactCodePattern       = regexp.MustCompile(`(?i)\b([A-Z]{2,12})([0-9]{2,8})\b`)
	looseExpectedCodePattern = regexp.MustCompile(`(?i)([A-Z]{2,12})[-_ ]*0*([0-9]{1,8})`)
	fc2CodePattern           = regexp.MustCompile(`(?i)\bFC2[-_ ]*PPV[-_ ]*([0-9]{5,8})\b`)
	advancedCodePatterns     = []*regexp.Regexp{
		regexp.MustCompile(`^([A-Z]{2,6})[-_]?(\d{2,6})$`),
		regexp.MustCompile(`^(N\d{3,6})$`),
		regexp.MustCompile(`^(T-?\d{3,6})$`),
		regexp.MustCompile(`^(CARIB\d{2,6})$`),
		regexp.MustCompile(`^(HEYZO\d{2,6})$`),
		regexp.MustCompile(`^(1PONDO\d{2,6})$`),
	}
	prefixBlacklist = map[string]struct{}{
		"H264": {}, "H265": {}, "X264": {}, "X265": {}, "HEVC": {},
		"AAC": {}, "DTS": {}, "WEB": {}, "WEBRIP": {}, "WEBDL": {},
		"BLURAY": {}, "UHD": {}, "FHD": {}, "HD": {}, "SD": {},
		"MP4": {}, "MKV": {}, "TS": {}, "AVI": {}, "MOV": {}, "M4V": {},
	}
	defaultVideoExtensions = []string{".mp4", ".mkv", ".avi", ".mov", ".flv", ".wmv", ".ts", ".m4v", ".iso"}
)

// This file owns organizer-side code extraction and normalization rules.
// When a future bug says "the file was not recognized as AV code", start here.
//
// Scope rules:
// - filename/code token parsing stays here
// - shared crawl/organizer identity normalization stays in identity.go
// - filesystem routing and report shaping must not leak back into this file

func normalizeCodeToken(code string) string {
	replacer := regexp.MustCompile(`[^A-Z0-9]`)
	return replacer.ReplaceAllString(normalizeFilmID(code), "")
}

// buildExpectedCodeSets turns a crawl-derived code list into both:
// 1) normalized human-readable codes
// 2) compact alphanumeric tokens for noisy filename matching
//
// Keeping both views together avoids each scan path building its own variant.
func buildExpectedCodeSets(rawCodes []string) (map[string]struct{}, map[string]struct{}) {
	codeSet := map[string]struct{}{}
	tokenSet := map[string]struct{}{}
	for _, code := range rawCodes {
		normalizedCode := normalizeFilmID(code)
		if normalizedCode == "" {
			continue
		}
		codeSet[normalizedCode] = struct{}{}
		if token := normalizeCodeToken(normalizedCode); token != "" {
			tokenSet[token] = struct{}{}
		}
	}
	return codeSet, tokenSet
}

// buildExpectedCodeEntryMap keeps organizer supplement reports on the same
// normalized code contract as the scan phase.
func buildExpectedCodeEntryMap(rawEntries []CodeEntry) map[string][]MagnetEntry {
	result := map[string][]MagnetEntry{}
	for _, entry := range rawEntries {
		code := normalizeFilmID(entry.Code)
		if code == "" {
			continue
		}
		result[code] = mergeMagnetEntries(result[code], entry.Magnets)
	}
	return result
}

func normalizeVideoExtensionToken(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.TrimLeft(normalized, "*.")
	if normalized == "" {
		return ""
	}
	for _, char := range normalized {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') {
			return ""
		}
	}
	return "." + normalized
}

// normalizeVideoExtensions is intentionally permissive about separators because
// the desktop UI, pasted settings, and older payloads may all serialize the
// extension list slightly differently.
func normalizeVideoExtensions(rawValue string) map[string]struct{} {
	// Accept commas, Chinese commas, ideographic commas, and whitespace so the
	// UI can feed extension lists without forcing one exact separator style.
	splitter := regexp.MustCompile(`[,，、\s]+`)
	items := splitter.Split(rawValue, -1)
	result := map[string]struct{}{}
	for _, item := range items {
		if token := normalizeVideoExtensionToken(item); token != "" {
			result[token] = struct{}{}
		}
	}
	if len(result) == 0 {
		for _, item := range defaultVideoExtensions {
			result[item] = struct{}{}
		}
	}
	return result
}

func formatVideoExtensions(extensionSet map[string]struct{}) string {
	items := make([]string, 0, len(extensionSet))
	for item := range extensionSet {
		items = append(items, strings.TrimPrefix(item, "."))
	}
	sort.Strings(items)
	return strings.Join(items, ", ")
}

func isVideoFile(filePath string, extensionSet map[string]struct{}) bool {
	if len(extensionSet) == 0 {
		extensionSet = normalizeVideoExtensions("")
	}
	_, ok := extensionSet[strings.ToLower(filepath.Ext(filePath))]
	return ok
}

func stripDomainNoise(value string) string {
	return strings.TrimSpace(domainNoisePattern.ReplaceAllString(value, " "))
}

func extractAdvancedFilmCode(value string) string {
	compact := strings.ReplaceAll(strings.ToUpper(value), " ", "")
	for _, pattern := range advancedCodePatterns {
		matches := pattern.FindStringSubmatch(compact)
		if len(matches) == 0 {
			continue
		}
		if len(matches) == 3 {
			return normalizeFilmID(matches[1] + "-" + matches[2])
		}
		return normalizeFilmID(strings.ReplaceAll(matches[1], "_", "-"))
	}
	return ""
}

// extractFilmCodeFromFile is organizer's one filename-to-code classifier.
// Scan/transfer/report phases should all trust this result instead of each
// phase adding new ad hoc matching rules.
func extractFilmCodeFromFile(filePath string, expectedCodeSet map[string]struct{}, expectedTokenSet map[string]struct{}) string {
	baseName := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	if atIndex := strings.LastIndex(baseName, "@"); atIndex >= 0 && atIndex+1 < len(baseName) {
		baseName = baseName[atIndex+1:]
	}

	baseName = stripDomainNoise(baseName)
	normalized := strings.TrimSpace(nonAlphaNumericPattern.ReplaceAllString(strings.ToUpper(baseName), " "))
	if normalized == "" {
		return ""
	}

	compact := strings.ReplaceAll(normalized, " ", "")
	matchedExpected, ambiguousExpected, sawCodeLike := matchExpectedNumericVariantDetailed(normalized, expectedCodeSet)
	if ambiguousExpected {
		return ""
	}
	if matchedExpected != "" {
		return matchedExpected
	}
	if len(expectedTokenSet) > 0 && !sawCodeLike {
		tokens := make([]string, 0, len(expectedTokenSet))
		for token := range expectedTokenSet {
			tokens = append(tokens, token)
		}
		sort.Slice(tokens, func(i int, j int) bool {
			return len(tokens[i]) > len(tokens[j])
		})
		for _, token := range tokens {
			if strings.Contains(compact, token) {
				return normalizeFilmID(token)
			}
		}
	}

	if advanced := extractAdvancedFilmCode(normalized); advanced != "" {
		return advanced
	}
	if matches := fc2CodePattern.FindStringSubmatch(normalized); len(matches) >= 2 {
		return normalizeFilmID("FC2-PPV-" + matches[1])
	}
	if matches := standardCodePattern.FindStringSubmatch(normalized); len(matches) >= 3 {
		prefix := strings.ToUpper(matches[1])
		if _, blocked := prefixBlacklist[prefix]; !blocked {
			return normalizeFilmID(prefix + "-" + matches[2])
		}
	}
	if matches := compactCodePattern.FindStringSubmatch(normalized); len(matches) >= 3 {
		prefix := strings.ToUpper(matches[1])
		if _, blocked := prefixBlacklist[prefix]; !blocked {
			return normalizeFilmID(prefix + "-" + matches[2])
		}
	}

	return ""
}

func matchExpectedNumericVariant(value string, expectedCodeSet map[string]struct{}) string {
	matched, ambiguous, _ := matchExpectedNumericVariantDetailed(value, expectedCodeSet)
	if ambiguous {
		return ""
	}
	return matched
}

func matchExpectedNumericVariantDetailed(value string, expectedCodeSet map[string]struct{}) (string, bool, bool) {
	if len(expectedCodeSet) == 0 {
		return "", false, false
	}

	index := make(map[string]string, len(expectedCodeSet))
	for code := range expectedCodeSet {
		key := numericCodeIdentityKey(code)
		if key == "" {
			continue
		}
		if existing, ok := index[key]; ok && existing != code {
			index[key] = ""
			continue
		}
		index[key] = code
	}

	matches := looseExpectedCodePattern.FindAllStringSubmatch(strings.ToUpper(value), -1)
	matchedCode := ""
	sawCodeLike := len(matches) > 0
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		number, err := strconv.ParseUint(match[2], 10, 64)
		if err != nil {
			continue
		}
		candidate := index[strings.ToUpper(match[1])+"#"+strconv.FormatUint(number, 10)]
		if candidate == "" {
			continue
		}
		if matchedCode != "" && matchedCode != candidate {
			return "", true, true
		}
		matchedCode = candidate
	}
	return matchedCode, false, sawCodeLike
}

func numericCodeIdentityKey(code string) string {
	normalized := normalizeFilmID(code)
	matches := looseExpectedCodePattern.FindStringSubmatch(normalized)
	if len(matches) < 3 || matches[0] != normalized {
		return ""
	}
	number, err := strconv.ParseUint(matches[2], 10, 64)
	if err != nil {
		return ""
	}
	return strings.ToUpper(matches[1]) + "#" + strconv.FormatUint(number, 10)
}
