package organizer

import (
	"net/url"
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
	standardCodePattern      = regexp.MustCompile(`(?i)([A-Z]{2,12})[-_ ]*([0-9]{1,8})`)
	compactCodePattern       = regexp.MustCompile(`(?i)\b([A-Z]{2,12})([0-9]{1,8})\b`)
	looseExpectedCodePattern = regexp.MustCompile(`(?i)([A-Z]{2,12})[-_ ]*0*([0-9]{1,8})`)
	fc2CodePattern           = regexp.MustCompile(`(?i)\bFC2[-_ ]*PPV[-_ ]*([0-9]{5,8})\b`)
	advancedCodePatterns     = []*regexp.Regexp{
		regexp.MustCompile(`^([A-Z]{2,6})[-_]?(\d{1,6})$`),
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
	defaultVideoExtensions        = []string{".mp4", ".mkv", ".avi", ".mov", ".flv", ".wmv", ".ts", ".m4v", ".iso"}
	invalidWindowsFilenamePattern = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)
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

// expectedCodeAliasIndex maps a release name found in a crawl magnet's `dn`
// field back to the canonical code of the crawl record that supplied it.
//
// Magnet display names are not guaranteed to match the detail-page code. For
// example, the crawl record MXGS-112 can legitimately carry dn=MXGS1121. The
// mapping is deliberately kept separate from codeSet: an alias is evidence for
// an existing crawl code, never a new code that should be organized by itself.
type expectedCodeAliasIndex struct {
	canonicalByKey map[string]string
	aliasByKey     map[string]string
	ambiguousKeys  map[string]struct{}
}

func newExpectedCodeAliasIndex() expectedCodeAliasIndex {
	return expectedCodeAliasIndex{
		canonicalByKey: map[string]string{},
		aliasByKey:     map[string]string{},
		ambiguousKeys:  map[string]struct{}{},
	}
}

// buildExpectedCodeAliasIndex builds aliases only from persisted magnet
// evidence. An alias is accepted only when it maps to one canonical code. If
// two records claim the same alias, the alias is removed and treated as
// ambiguous by the strict matcher.
func buildExpectedCodeAliasIndex(rawEntries []CodeEntry) expectedCodeAliasIndex {
	index := newExpectedCodeAliasIndex()
	canonicalByKey := map[string]string{}
	for _, entry := range rawEntries {
		canonical := normalizeFilmID(entry.Code)
		key := numericCodeIdentityKey(canonical)
		if canonical == "" || key == "" {
			continue
		}
		if existing, ok := canonicalByKey[key]; !ok || existing == canonical {
			canonicalByKey[key] = canonical
		} else {
			// This is already a malformed/ambiguous expected list. Keep the
			// key unavailable for alias mapping rather than guessing a target.
			delete(canonicalByKey, key)
		}
	}

	for _, entry := range rawEntries {
		canonical := normalizeFilmID(entry.Code)
		canonicalKey := numericCodeIdentityKey(canonical)
		if canonical == "" || canonicalKey == "" {
			continue
		}
		for _, magnet := range entry.Magnets {
			for _, candidate := range extractMagnetCodeCandidates(magnet.Link) {
				alias := normalizeFilmID(candidate)
				aliasKey := numericCodeIdentityKey(alias)
				if alias == "" || aliasKey == "" || aliasKey == canonicalKey || !likelyExpectedMagnetAlias(alias, canonical) {
					continue
				}

				// A display name that is itself a canonical code for another
				// record cannot be silently reassigned to this record.
				if canonicalOwner, isCanonical := canonicalByKey[aliasKey]; isCanonical && canonicalOwner != canonical {
					index.ambiguousKeys[aliasKey] = struct{}{}
					delete(index.canonicalByKey, aliasKey)
					delete(index.aliasByKey, aliasKey)
					continue
				}
				if _, ambiguous := index.ambiguousKeys[aliasKey]; ambiguous {
					continue
				}
				if existing, exists := index.canonicalByKey[aliasKey]; exists && existing != canonical {
					index.ambiguousKeys[aliasKey] = struct{}{}
					delete(index.canonicalByKey, aliasKey)
					delete(index.aliasByKey, aliasKey)
					continue
				}
				index.canonicalByKey[aliasKey] = canonical
				if _, exists := index.aliasByKey[aliasKey]; !exists {
					index.aliasByKey[aliasKey] = alias
				}
			}
		}
	}

	return index
}

// likelyExpectedMagnetAlias keeps magnet evidence within the same code family.
// Providers commonly append one or two release digits (MXGS-112 -> MXGS-1121),
// while a different prefix or an unrelated number must not become an alias.
func likelyExpectedMagnetAlias(alias, canonical string) bool {
	aliasMatch := looseExpectedCodePattern.FindStringSubmatch(normalizeFilmID(alias))
	canonicalMatch := looseExpectedCodePattern.FindStringSubmatch(normalizeFilmID(canonical))
	if len(aliasMatch) < 3 || len(canonicalMatch) < 3 {
		return false
	}
	if aliasMatch[0] != normalizeFilmID(alias) || canonicalMatch[0] != normalizeFilmID(canonical) {
		return false
	}
	if !strings.EqualFold(aliasMatch[1], canonicalMatch[1]) {
		return false
	}
	aliasNumber := strings.TrimLeft(aliasMatch[2], "0")
	canonicalNumber := strings.TrimLeft(canonicalMatch[2], "0")
	if aliasNumber == "" {
		aliasNumber = "0"
	}
	if canonicalNumber == "" {
		canonicalNumber = "0"
	}
	if aliasNumber == canonicalNumber {
		return false
	}
	return len(aliasNumber) > len(canonicalNumber) &&
		len(aliasNumber)-len(canonicalNumber) <= 2 &&
		strings.HasPrefix(aliasNumber, canonicalNumber)
}

// extractMagnetCodeCandidates reads only the magnet display name (`dn`). The
// info hash and tracker query parameters are not film identity evidence and
// must never enter the organizer's alias set.
func extractMagnetCodeCandidates(link string) []string {
	trimmed := strings.TrimSpace(link)
	if trimmed == "" {
		return nil
	}

	displayNames := make([]string, 0, 1)
	if parsed, err := url.Parse(trimmed); err == nil {
		query := parsed.Query()
		for key, values := range query {
			if !strings.EqualFold(key, "dn") {
				continue
			}
			displayNames = append(displayNames, values...)
		}
	}
	if len(displayNames) == 0 {
		// Keep compatibility with malformed/partially escaped magnet links.
		lower := strings.ToLower(trimmed)
		if marker := strings.Index(lower, "dn="); marker >= 0 {
			value := trimmed[marker+3:]
			if separator := strings.IndexAny(value, "&"); separator >= 0 {
				value = value[:separator]
			}
			if decoded, err := url.QueryUnescape(value); err == nil {
				value = decoded
			}
			displayNames = append(displayNames, value)
		}
	}

	seen := map[string]struct{}{}
	result := make([]string, 0, len(displayNames))
	for _, displayName := range displayNames {
		for _, candidate := range extractFilmCodeCandidates(displayName) {
			if _, exists := seen[candidate]; exists {
				continue
			}
			seen[candidate] = struct{}{}
			result = append(result, candidate)
		}
	}
	return result
}

// extractFilmCodeCandidates is intentionally independent from the expected
// list. It is used only to parse magnet display names before those candidates
// are checked against a canonical CodeEntry mapping.
func extractFilmCodeCandidates(value string) []string {
	cleaned := stripDomainNoise(value)
	normalized := strings.TrimSpace(nonAlphaNumericPattern.ReplaceAllString(strings.ToUpper(cleaned), " "))
	if normalized == "" {
		return nil
	}

	seen := map[string]struct{}{}
	result := make([]string, 0, 2)
	appendCandidate := func(candidate string) {
		candidate = normalizeFilmID(candidate)
		if candidate == "" {
			return
		}
		if _, exists := seen[candidate]; exists {
			return
		}
		seen[candidate] = struct{}{}
		result = append(result, candidate)
	}

	if matches := fc2CodePattern.FindAllStringSubmatch(normalized, -1); len(matches) > 0 {
		for _, match := range matches {
			if len(match) >= 2 {
				appendCandidate("FC2-PPV-" + match[1])
			}
		}
	}
	for _, match := range standardCodePattern.FindAllStringSubmatch(normalized, -1) {
		if len(match) >= 3 {
			appendCandidate(strings.ToUpper(match[1]) + "-" + match[2])
		}
	}
	for _, match := range compactCodePattern.FindAllStringSubmatch(normalized, -1) {
		if len(match) >= 3 {
			appendCandidate(strings.ToUpper(match[1]) + "-" + match[2])
		}
	}
	return result
}

// matchExpectedCodeAliasFromValue resolves one path/name fragment through the
// magnet-derived alias index. Multiple canonical targets are always rejected.
func matchExpectedCodeAliasFromValue(value string, index expectedCodeAliasIndex) (string, string, bool) {
	if len(index.canonicalByKey) == 0 && len(index.ambiguousKeys) == 0 {
		return "", "", false
	}
	matchedCanonical := ""
	matchedAlias := ""
	for _, candidate := range extractFilmCodeCandidates(value) {
		key := numericCodeIdentityKey(candidate)
		if key == "" {
			continue
		}
		if _, ambiguous := index.ambiguousKeys[key]; ambiguous {
			return "", "", true
		}
		canonical, exists := index.canonicalByKey[key]
		if !exists {
			continue
		}
		if matchedCanonical != "" && matchedCanonical != canonical {
			return "", "", true
		}
		matchedCanonical = canonical
		matchedAlias = firstNonEmpty(index.aliasByKey[key], candidate)
	}
	return matchedCanonical, matchedAlias, false
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

// normalizeTargetFilmCode is the final safety gate before a recognized code is
// used as a Windows filename. Scan candidates should already be canonical, but
// imported snapshots and older state can retain title fragments such as
// "DRDZ-002.mp4**". Never allow those fragments into the target-name plan.
func normalizeTargetFilmCode(value string) string {
	if extracted := extractFilmID(value); extracted != "" {
		return extracted
	}
	return extractAdvancedFilmCode(value)
}

// sanitizeOutputFileName is the final filename boundary for every organizer
// move, including candidates that have no recognized film code. Imported
// states and ISO release names can contain shell globs such as "*.iso";
// passing those through would fail on Windows or create misleading paths.
func sanitizeOutputFileName(value string, fallback string) string {
	name := strings.TrimSpace(filepath.Base(value))
	name = invalidWindowsFilenamePattern.ReplaceAllString(name, "_")
	name = strings.TrimRight(name, ". ")
	if name == "" || name == "." || name == ".." {
		name = strings.TrimSpace(fallback)
	}
	if name == "" {
		name = "UNNAMED"
	}
	base := strings.TrimSuffix(name, filepath.Ext(name))
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		name = "_" + name
	}
	return name
}

func normalizedSourceExtension(sourcePath string) string {
	ext := strings.ToLower(filepath.Ext(filepath.Base(strings.TrimSpace(sourcePath))))
	if ext == "" || len(ext) > 10 || !regexp.MustCompile(`^\.[a-z0-9]+$`).MatchString(ext) {
		return ""
	}
	return ext
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

// matchExpectedCodeFallback is a last-resort matcher for noisy paths. The
// primary extractor intentionally works on one filename at a time; this
// fallback is only used when that result did not match the loaded crawl list.
// It reuses the numeric identity matcher over the cleaned full path so names
// such as "site.example@MXGS1358" and "mxgs01121" can resolve to the
// canonical code already present in the expected-code set.
func matchExpectedCodeFallback(value string, expectedCodeSet map[string]struct{}) string {
	if len(expectedCodeSet) == 0 {
		return ""
	}

	cleanedValue := strings.ToUpper(stripDomainNoise(value))
	matched, ambiguous, _ := matchExpectedNumericVariantDetailed(cleanedValue, expectedCodeSet)
	if ambiguous {
		return ""
	}
	return matched
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
