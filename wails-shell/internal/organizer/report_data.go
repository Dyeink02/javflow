package organizer

import (
	"fmt"
	"sort"
	"strings"

	"javflow/internal/contracts/crawlartifact"
)

// report_data.go owns organizer report aggregation helpers and sortable
// data-shaping logic for operator-facing text outputs.
//
// If a report is missing data or grouping codes incorrectly before it is
// rendered to text, inspect this file before changing the writers.
//
// Ownership summary:
// 1) aggregate organizer report-side data before text rendering
// 2) normalize/sort code- and ad-risk-related report structures
// 3) keep report grouping logic out of the filesystem-writing layer
//
// File map for maintainers:
// 1) sortable report DTO helpers
// 2) code/ad-risk aggregation helpers
// 3) report grouping and derived-stat assembly helpers

type adRiskCodeDetail struct {
	FilmCode   string
	MaxScore   float64
	SourcePath string
	Size       int64
	Reasons    []string
	Evidence   map[string]any
}

func formatBytesToGB(bytes int64) string {
	return fmt.Sprintf("%.2f", float64(bytes)/1024/1024/1024)
}

func sortCodeAlphabetically(codes []string) []string {
	result := make([]string, 0, len(codes))
	seen := map[string]struct{}{}
	for _, code := range codes {
		normalized := normalizeFilmID(code)
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

// report_data.go owns report-side normalization and aggregation. If the report
// content is structurally wrong before it is written to disk, debug here first.
func buildAdRiskCodeDetails(records []AdRiskRecord) []adRiskCodeDetail {
	codeMap := map[string]adRiskCodeDetail{}
	for _, record := range records {
		filmCode := normalizeFilmID(record.FilmCode)
		if filmCode == "" {
			continue
		}
		reasons := make([]string, 0, len(record.Reasons))
		for _, reason := range record.Reasons {
			trimmed := strings.TrimSpace(reason)
			if trimmed != "" {
				reasons = append(reasons, trimmed)
			}
			if len(reasons) >= 6 {
				break
			}
		}
		existing, exists := codeMap[filmCode]
		if !exists || record.Score > existing.MaxScore {
			codeMap[filmCode] = adRiskCodeDetail{
				FilmCode:   filmCode,
				MaxScore:   record.Score,
				SourcePath: record.SourcePath,
				Size:       record.Size,
				Reasons:    reasons,
				Evidence:   record.Evidence,
			}
			continue
		}
		if len(existing.Reasons) == 0 && len(reasons) > 0 {
			existing.Reasons = reasons
			codeMap[filmCode] = existing
		}
	}

	result := make([]adRiskCodeDetail, 0, len(codeMap))
	for _, item := range codeMap {
		result = append(result, item)
	}
	sort.Slice(result, func(i int, j int) bool {
		if result[i].MaxScore != result[j].MaxScore {
			return result[i].MaxScore > result[j].MaxScore
		}
		return result[i].FilmCode < result[j].FilmCode
	})
	return result
}

// buildSupplementMagnetEntries is the report-side bridge from organizer code
// lists back to crawl artifact magnet snapshots. The earlier scan phases should
// not need to know how supplement reports are formatted.
func buildSupplementMagnetEntries(codes []string, expectedCodeEntryMap map[string][]MagnetEntry) []CodeEntry {
	sortedCodes := sortCodeAlphabetically(codes)
	result := make([]CodeEntry, 0, len(sortedCodes))
	for _, code := range sortedCodes {
		result = append(result, CodeEntry{
			Code:    code,
			Magnets: mergeMagnetEntries(expectedCodeEntryMap[code]),
		})
	}
	return result
}

func countMagnets(entries []CodeEntry) int {
	total := 0
	for _, entry := range entries {
		total += len(mergeMagnetEntries(entry.Magnets))
	}
	return total
}

// sortedUnmatchedRecords keeps high-signal review items near the top of the
// unmatched report without changing the underlying organizer summary counts.
func sortedUnmatchedRecords(records []UnmatchedRecord, maxDisplay int) ([]UnmatchedRecord, int) {
	result := append([]UnmatchedRecord{}, records...)
	sort.Slice(result, func(i int, j int) bool {
		leftVideo := result[i].IsVideo
		rightVideo := result[j].IsVideo
		if leftVideo != rightVideo {
			return leftVideo
		}
		if result[i].Size != result[j].Size {
			return result[i].Size > result[j].Size
		}
		return strings.ToLower(result[i].Path) < strings.ToLower(result[j].Path)
	})
	if maxDisplay < 1 {
		maxDisplay = 1
	}
	if len(result) <= maxDisplay {
		return result, 0
	}
	return result[:maxDisplay], len(result) - maxDisplay
}

// evidenceSummary intentionally compresses verbose ad-review evidence into one
// stable text line for operator reports.
func evidenceSummary(evidence map[string]any) string {
	if len(evidence) == 0 {
		return "-"
	}
	frameHashCount := 0
	if frameHashes, ok := evidence["frameHashes"].([]any); ok {
		frameHashCount = len(frameHashes)
	}
	templateID := "-"
	if bestTemplate, ok := evidence["bestTemplateMatch"].(map[string]any); ok {
		if value := strings.TrimSpace(crawlartifact.AnyToString(bestTemplate["templateId"])); value != "" {
			templateID = value
		}
	}
	adSampleID := "-"
	if bestSample, ok := evidence["bestAdSampleMatch"].(map[string]any); ok {
		if value := strings.TrimSpace(crawlartifact.AnyToString(bestSample["sampleId"])); value != "" {
			adSampleID = value
		}
	}
	return fmt.Sprintf("\u5e27\u54c8\u5e0c\u6570=%d\uff1b\u6a21\u677f\u547d\u4e2d=%s\uff1b\u5e7f\u544a\u6837\u672c\u547d\u4e2d=%s", frameHashCount, templateID, adSampleID)
}
