package crawlrequest

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// magnet.go owns magnet-link extraction, normalization, and ranking from raw
// detail-page HTML fragments.
//
// Ownership summary:
// 1) extract raw magnet candidates from detail-page markup
// 2) normalize, filter, and rank magnet candidates for output selection
// 3) keep magnet parsing policy separate from page fetch and artifact writes
//
// File map for maintainers:
// 1) raw magnet parsing regexes and DTOs
// 2) normalization/filter/sort helpers
// 3) selected-versus-backup magnet result assembly

var (
	// Some JAV ajax/browser responses return only the bare magnet URI without
	// a dn parameter. Extraction must therefore accept any valid btih magnet and
	// leave display-name parsing to downstream helpers.
	magnetLinkPattern = regexp.MustCompile(`(?i)magnet:\?xt=urn:btih:[A-F0-9]+(?:&[^"'\\s<]+)*`)
	sizeTokenPattern  = regexp.MustCompile(`(?i)\d+(\.\d+)?[GM]B`)
	magnetDNPattern   = regexp.MustCompile(`(?i)[?&]dn=([^&]+)`)
)

type ParsedMagnetCandidate struct {
	MagnetLink  string  `json:"magnetLink"`
	Size        float64 `json:"size"`
	DisplayName string  `json:"displayName"`
}

type MagnetLink struct {
	Link string `json:"link"`
	Size string `json:"size"`
}

type MagnetResult struct {
	Magnet            string       `json:"magnet"`
	MagnetLinks       []MagnetLink `json:"magnetLinks"`
	BackupMagnetLinks []MagnetLink `json:"backupMagnetLinks"`
}

func GetMagnetExcludeKeywords(rawValue string) []string {
	normalized := strings.TrimSpace(rawValue)
	if normalized == "" {
		return nil
	}
	fields := strings.FieldsFunc(normalized, func(r rune) bool {
		return r == '\r' || r == '\n' || r == ',' || r == '，' || r == '、'
	})
	seen := map[string]struct{}{}
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		keyword := strings.ToLower(strings.TrimSpace(field))
		if keyword == "" {
			continue
		}
		if _, exists := seen[keyword]; exists {
			continue
		}
		seen[keyword] = struct{}{}
		result = append(result, keyword)
	}
	return result
}

func GetMagnetDisplayName(magnetLink string) string {
	rawValue := strings.TrimSpace(magnetLink)
	if rawValue == "" {
		return ""
	}
	parsedURL, err := url.Parse(rawValue)
	if err == nil {
		decodedName := strings.TrimSpace(parsedURL.Query().Get("dn"))
		if decodedName != "" {
			return decodedName
		}
	}
	matches := magnetDNPattern.FindStringSubmatch(rawValue)
	if len(matches) < 2 {
		return rawValue
	}
	decodedValue, err := url.QueryUnescape(strings.ReplaceAll(matches[1], "+", " "))
	if err != nil {
		return strings.TrimSpace(matches[1])
	}
	return strings.TrimSpace(decodedValue)
}

func ApplyMagnetExcludeFilter(candidates []ParsedMagnetCandidate, rawKeywords string) []ParsedMagnetCandidate {
	keywords := GetMagnetExcludeKeywords(rawKeywords)
	if len(keywords) == 0 || len(candidates) == 0 {
		return candidates
	}
	result := make([]ParsedMagnetCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		displayName := strings.ToLower(candidate.DisplayName)
		magnetText := strings.ToLower(candidate.MagnetLink)
		matched := false
		for _, keyword := range keywords {
			if strings.Contains(displayName, keyword) || strings.Contains(magnetText, keyword) {
				matched = true
				break
			}
		}
		if !matched {
			result = append(result, candidate)
		}
	}
	return result
}

func ExtractMagnetLinks(responseBody string) []string {
	matches := magnetLinkPattern.FindAllString(responseBody, -1)
	seen := map[string]struct{}{}
	result := make([]string, 0, len(matches))
	for _, match := range matches {
		link := strings.TrimSpace(match)
		if link == "" {
			continue
		}
		key := strings.ToLower(link)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, link)
	}
	return result
}

func ExtractSizeTokens(responseBody string) []string {
	matches := sizeTokenPattern.FindAllString(responseBody, -1)
	result := make([]string, 0, len(matches))
	for _, match := range matches {
		result = append(result, strings.ToUpper(strings.TrimSpace(match)))
	}
	return result
}

func BuildParsedMagnetCandidates(magnetLinks []string, sizeTokens []string) []ParsedMagnetCandidate {
	result := make([]ParsedMagnetCandidate, 0, len(magnetLinks))
	for index, magnetLink := range magnetLinks {
		sizeToken := "0MB"
		if index < len(sizeTokens) && strings.TrimSpace(sizeTokens[index]) != "" {
			sizeToken = strings.ToUpper(strings.TrimSpace(sizeTokens[index]))
		}
		sizeValueText := strings.NewReplacer("GB", "", "MB", "").Replace(sizeToken)
		sizeValue, err := strconv.ParseFloat(sizeValueText, 64)
		if err != nil {
			sizeValue = 0
		}
		if strings.Contains(sizeToken, "GB") {
			sizeValue *= 1024
		}
		result = append(result, ParsedMagnetCandidate{
			MagnetLink:  strings.TrimSpace(magnetLink),
			Size:        sizeValue,
			DisplayName: GetMagnetDisplayName(magnetLink),
		})
	}
	return result
}

func SelectLargestMagnetCandidate(candidates []ParsedMagnetCandidate) *ParsedMagnetCandidate {
	if len(candidates) == 0 {
		return nil
	}
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.Size > best.Size {
			best = candidate
		}
	}
	return &best
}

func BuildMagnetResult(candidates []ParsedMagnetCandidate, keepAll bool, backupTopN int) *MagnetResult {
	if len(candidates) == 0 {
		return nil
	}

	sortedBySize := append([]ParsedMagnetCandidate(nil), candidates...)
	sort.Slice(sortedBySize, func(i int, j int) bool {
		return sortedBySize[i].Size > sortedBySize[j].Size
	})
	if backupTopN < 1 {
		backupTopN = 1
	}
	if backupTopN > 10 {
		backupTopN = 10
	}

	backupLinks := make([]MagnetLink, 0, minInt(backupTopN, len(sortedBySize)))
	for _, candidate := range sortedBySize[:minInt(backupTopN, len(sortedBySize))] {
		backupLinks = append(backupLinks, MagnetLink{
			Link: candidate.MagnetLink,
			Size: FormatFileSize(candidate.Size),
		})
	}

	if keepAll {
		magnetLinks := make([]MagnetLink, 0, len(candidates))
		allLinks := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			allLinks = append(allLinks, candidate.MagnetLink)
			magnetLinks = append(magnetLinks, MagnetLink{
				Link: candidate.MagnetLink,
				Size: FormatFileSize(candidate.Size),
			})
		}
		return &MagnetResult{
			Magnet:            strings.Join(allLinks, "\n"),
			MagnetLinks:       magnetLinks,
			BackupMagnetLinks: backupLinks,
		}
	}

	selected := SelectLargestMagnetCandidate(candidates)
	if selected == nil {
		return nil
	}
	return &MagnetResult{
		Magnet: selected.MagnetLink,
		MagnetLinks: []MagnetLink{
			{
				Link: selected.MagnetLink,
				Size: FormatFileSize(selected.Size),
			},
		},
		BackupMagnetLinks: backupLinks,
	}
}

func FormatFileSize(sizeInMB float64) string {
	if sizeInMB >= 1024 {
		return fmt.Sprintf("%.2fGB", sizeInMB/1024)
	}
	return fmt.Sprintf("%.2fMB", sizeInMB)
}

func NormalizeAjaxImageParam(value string) string {
	rawValue := strings.TrimSpace(value)
	if rawValue == "" {
		return ""
	}
	unescapedValue := strings.ReplaceAll(rawValue, `\/`, `/`)
	absoluteURL, err := url.Parse(unescapedValue)
	if err == nil && absoluteURL.Scheme != "" && absoluteURL.Host != "" {
		return strings.TrimLeft(absoluteURL.Path+"?"+absoluteURL.RawQuery, "/?")
	}
	return strings.TrimLeft(unescapedValue, "/")
}

func minInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
}
