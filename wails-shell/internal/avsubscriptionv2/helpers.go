// Ownership summary:
//   This file implements domain logic for the javflow backend.
//
// File map for maintainers:
//   1) Domain-specific types and helpers.
//   2) Internal service implementation.
//

package avsubscriptionv2

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"javflow/internal/contracts/crawlartifact"
	"javflow/internal/crawlidentity"
)

var filmCodePattern = regexp.MustCompile(`([A-Z]{2,12})-?(\d{1,8}[A-Z]*)`)

func normalizeName(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), ""))
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func calcPages(total int, itemsPerPage int) int {
	if total <= 0 {
		return 1
	}
	if itemsPerPage <= 0 {
		itemsPerPage = defaultItemsPerPage
	}
	pages := total / itemsPerPage
	if total%itemsPerPage != 0 {
		pages++
	}
	if pages < 1 {
		return 1
	}
	return pages
}

func normalizeCodes(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}

	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		normalized := normalizeFilmCode(value)
		if normalized == "" {
			normalized = crawlidentity.NormalizeFilmID(value)
		}
		normalized = strings.TrimSpace(normalized)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	sort.Strings(result)
	return result
}

func diffCodes(left []string, remove []string) []string {
	if len(left) == 0 {
		return []string{}
	}
	removeSet := map[string]struct{}{}
	for _, value := range normalizeCodes(remove) {
		removeSet[value] = struct{}{}
	}
	filtered := make([]string, 0, len(left))
	for _, value := range normalizeCodes(left) {
		if _, exists := removeSet[value]; exists {
			continue
		}
		filtered = append(filtered, value)
	}
	return filtered
}

func normalizeFilmCode(value string) string {
	upper := strings.ToUpper(strings.TrimSpace(value))
	if upper == "" {
		return ""
	}
	if normalized := crawlidentity.NormalizeFilmID(upper); strings.TrimSpace(normalized) != "" && normalized != upper {
		return normalized
	}
	if extracted := crawlidentity.ExtractFilmID(upper); strings.TrimSpace(extracted) != "" {
		return extracted
	}
	if matches := filmCodePattern.FindStringSubmatch(upper); len(matches) == 3 {
		return crawlidentity.NormalizeFilmID(matches[1] + "-" + matches[2])
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func inferBaseFromURL(crawlURL string) string {
	trimmed := strings.TrimSpace(crawlURL)
	if trimmed == "" {
		return ""
	}
	parts := strings.SplitN(trimmed, "/", 4)
	if len(parts) >= 3 && strings.HasPrefix(strings.ToLower(parts[0]), "http") {
		return parts[0] + "//" + parts[2]
	}
	return ""
}

func extractCodesFromOutput(outputDir string, userDataDir string) []string {
	_, records, err := crawlartifact.ReadFilmDataRecordsWithUserData(outputDir, userDataDir)
	if err != nil || len(records) == 0 {
		return []string{}
	}
	codes := make([]string, 0, len(records))
	for index, record := range records {
		identity := extractRecordIdentity(record, index)
		normalized := normalizeFilmCode(identity)
		if normalized == "" {
			normalized = normalizeFilmCode(recordFieldText(record["title"]))
		}
		if normalized == "" {
			normalized = normalizeFilmCode(recordFieldText(record["sourceLink"]))
		}
		if normalized != "" {
			codes = append(codes, normalized)
		}
	}
	return normalizeCodes(codes)
}

func extractRecordIdentity(record map[string]any, index int) string {
	candidates := []string{
		recordFieldText(record["filmCode"]),
		recordFieldText(record["code"]),
		recordFieldText(record["sourceLink"]),
		recordFieldText(record["title"]),
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if normalized := normalizeFilmCode(candidate); normalized != "" {
			return normalized
		}
		if strings.TrimSpace(candidate) != "" {
			return candidate
		}
	}
	return fmt.Sprintf("record-%d", index)
}

func recordFieldText(value any) string {
	if value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || strings.EqualFold(text, "<nil>") {
		return ""
	}
	return text
}

func outputDirHints(outputDir string) []string {
	cleanPath := filepath.Clean(strings.TrimSpace(outputDir))
	if cleanPath == "" || cleanPath == "." {
		return nil
	}
	hints := []string{filepath.Base(cleanPath)}
	parent := filepath.Base(filepath.Dir(cleanPath))
	if parent != "" && parent != "." && parent != hints[0] {
		hints = append(hints, parent)
	}
	return hints
}

func normalizeFolderHint(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "run-") {
		return ""
	}
	runes := []rune(trimmed)
	end := len(runes)
	for end > 0 {
		last := runes[end-1]
		if unicode.IsDigit(last) || unicode.IsSpace(last) || strings.ContainsRune("-_()[]{}，。（）", last) {
			end--
			continue
		}
		break
	}
	return normalizeName(strings.TrimSpace(string(runes[:end])))
}

func extractSourceURLFromLine(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	if !strings.Contains(lower, "起始地址") && !strings.Contains(lower, "source url") {
		return ""
	}
	for _, separator := range []string{"：", ":"} {
		if index := strings.Index(trimmed, separator); index >= 0 {
			value := strings.TrimSpace(trimmed[index+len(separator):])
			if strings.HasPrefix(strings.ToLower(value), "http://") || strings.HasPrefix(strings.ToLower(value), "https://") {
				return value
			}
		}
	}
	return ""
}

func recoverTargetMetadataFromTextFile(filePath string) (string, string) {
	trimmedPath := strings.TrimSpace(filePath)
	if trimmedPath == "" {
		return "", ""
	}
	file, err := os.Open(trimmedPath)
	if err != nil {
		return "", ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		crawlURL := extractSourceURLFromLine(scanner.Text())
		if crawlURL == "" {
			continue
		}
		return crawlURL, inferBaseFromURL(crawlURL)
	}
	return "", ""
}
