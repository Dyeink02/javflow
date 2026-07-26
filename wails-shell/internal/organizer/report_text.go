package organizer

import (
	"fmt"
	"strings"

	"javflow/internal/common"
)

// report_text.go owns human-readable organizer report text assembly and file
// writing helpers.
//
// Ownership summary:
// 1) format organizer report text blocks from normalized run data
// 2) keep operator-facing summary wording out of scan/transfer logic
// 3) centralize report-file text shaping for consistency
//
// File map for maintainers:
// 1) report write request DTOs
// 2) text-file writer and row-format helpers
// 3) operator-facing report text block builders

type reportWriteRequest struct {
	key   string
	path  string
	lines []string
}

// report_text.go owns human-facing report text layout and disk writes. If the
// report wording or output file content needs adjustment, start here.
func writeTextFile(filePath string, lines []string) error {
	// Organizer reports are user-facing Chinese text files, so keep them on the
	// same UTF-8-with-BOM path as crawl logs/quality reports to avoid Windows
	// editor/codepage ambiguity during troubleshooting.
	return common.WriteUTF8TextFile(filePath, strings.Join(lines, "\r\n")+"\r\n")
}

// formatRenameRecordLine keeps rename-map rows stable so report consumers and
// support diagnostics do not depend on several handwritten line formats.
func formatRenameRecordLine(record RenameRecord, index int) string {
	filmCodeLabel := strings.TrimSpace(record.FilmCode)
	if filmCodeLabel == "" {
		filmCodeLabel = "\u672a\u8bc6\u522b\u756a\u53f7"
	}
	actionLabel := "\u6309\u756a\u53f7\u6539\u540d"
	if !record.RenameApplied {
		note := strings.TrimSpace(record.Note)
		if note == "" {
			note = "\u672a\u547d\u4e2d\u756a\u53f7"
		}
		actionLabel = "\u4fdd\u7559\u539f\u540d\uff08" + note + "\uff09"
	}
	return fmt.Sprintf("%d. %s => %s | [%s] | %s | %s", index+1, record.OriginalName, record.NewName, filmCodeLabel, actionLabel, record.OriginalPath)
}

func buildRenameReportLines(nowText string, summary Summary, renameRecords []RenameRecord) []string {
	lines := []string{
		"\u89c6\u9891\u6574\u7406\u52a9\u624b - \u66f4\u6539\u524d\u540e\u5bf9\u7167",
		"\u751f\u6210\u65f6\u95f4\uff1a" + nowText,
		fmt.Sprintf("\u626b\u63cf\u603b\u6570\uff1a%d", summary.ScannedTotal),
		fmt.Sprintf("\u89c6\u9891\u603b\u6570\uff1a%d", summary.VideoTotal),
		fmt.Sprintf("\u547d\u4e2d\u756a\u53f7\uff1a%d", summary.QualifiedVideo),
		fmt.Sprintf("\u79fb\u5165\u5f85\u6574\u7406\uff1a%d", summary.MovedToWaiting),
		"",
		"\u660e\u7ec6\uff08\u539f\u540d => \u65b0\u540d | \u756a\u53f7 | \u539f\u8def\u5f84\uff09\uff1a",
		"----------------------------------------",
	}
	for index, record := range renameRecords {
		lines = append(lines, formatRenameRecordLine(record, index))
	}
	return lines
}

func buildUnmatchedReportLines(nowText string, summary Summary, unmatchedRecords []UnmatchedRecord) []string {
	displayUnmatched, omittedCount := sortedUnmatchedRecords(unmatchedRecords, 600)
	lines := []string{
		"\u89c6\u9891\u6574\u7406\u52a9\u624b - \u5f85\u5220\u9664\u6216\u672a\u547d\u4e2d\u660e\u7ec6",
		"\u751f\u6210\u65f6\u95f4\uff1a" + nowText,
		fmt.Sprintf("\u89c6\u9891\u603b\u6570\uff1a%d", summary.VideoTotal),
		fmt.Sprintf("\u547d\u4e2d\u756a\u53f7\uff1a%d", summary.QualifiedVideo),
		fmt.Sprintf("\u5f85\u5220\u9664\u5904\u7406\u603b\u6570\uff1a%d", summary.MovedToDelete+summary.DeletedDirectly),
		fmt.Sprintf("\u79fb\u5165\u5f85\u5220\u9664\uff1a%d", summary.MovedToDelete),
		fmt.Sprintf("\u76f4\u63a5\u5220\u9664\uff1a%d", summary.DeletedDirectly),
		fmt.Sprintf("\u5f52\u5165\u542b\u5f00\u5934\u5e7f\u544a\uff1a%d", summary.MovedToIntroAd),
		"",
		"\u660e\u7ec6\uff08\u539f\u56e0 | \u5927\u5c0fGB | \u8def\u5f84\uff09\uff1a",
		"----------------------------------------",
	}
	for index, record := range displayUnmatched {
		lines = append(lines, fmt.Sprintf("%d. [%s] %sGB | %s", index+1, record.Reason, formatBytesToGB(record.Size), record.Path))
	}
	if omittedCount > 0 {
		lines = append(lines, fmt.Sprintf("... \u5176\u4f59 %d \u6761\u8bb0\u5f55\u5df2\u7701\u7565\uff08\u907f\u514d\u62a5\u544a\u8fc7\u5927\uff09", omittedCount))
	}
	return lines
}

func buildAdRiskCodeLines(nowText string, adRiskRecords []AdRiskRecord) []string {
	adRiskCodeDetails := buildAdRiskCodeDetails(adRiskRecords)
	adRiskCodes := make([]string, 0, len(adRiskCodeDetails))
	highRiskCount := 0
	reviewRiskCount := 0
	observedRiskCount := 0
	for _, item := range adRiskCodeDetails {
		adRiskCodes = append(adRiskCodes, item.FilmCode)
		switch {
		case item.MaxScore >= 80:
			highRiskCount++
		case item.MaxScore >= 70:
			reviewRiskCount++
		default:
			observedRiskCount++
		}
	}
	lines := []string{
		"\u89c6\u9891\u6574\u7406\u52a9\u624b - \u542b\u5f00\u5934\u5e7f\u544a\u756a\u53f7",
		"\u751f\u6210\u65f6\u95f4\uff1a" + nowText,
		fmt.Sprintf("\u542b\u5f00\u5934\u5e7f\u544a\u756a\u53f7\u603b\u6570\uff1a%d", len(adRiskCodes)),
		fmt.Sprintf("\u9ad8\u7f6e\u4fe1\uff08>=80\uff09\uff1a%d", highRiskCount),
		fmt.Sprintf("\u5efa\u8bae\u590d\u6838\uff0870-79\uff09\uff1a%d", reviewRiskCount),
		fmt.Sprintf("\u89c2\u5bdf\u9879\uff08<70\uff09\uff1a%d", observedRiskCount),
		"",
	}
	if len(adRiskCodes) == 0 {
		lines = append(lines, "\u672a\u53d1\u73b0\u542b\u5f00\u5934\u5e7f\u544a\u756a\u53f7\u3002")
		return lines
	}
	for index, code := range adRiskCodes {
		lines = append(lines, fmt.Sprintf("%d. %s", index+1, code))
	}
	return lines
}

func buildAdRiskDetailLines(nowText string, adRiskRecords []AdRiskRecord) []string {
	adRiskCodeDetails := buildAdRiskCodeDetails(adRiskRecords)
	lines := []string{
		"\u89c6\u9891\u6574\u7406\u52a9\u624b - \u542b\u5f00\u5934\u5e7f\u544a\u660e\u7ec6",
		"\u751f\u6210\u65f6\u95f4\uff1a" + nowText,
		fmt.Sprintf("\u542b\u5f00\u5934\u5e7f\u544a\u756a\u53f7\u603b\u6570\uff1a%d", len(adRiskCodeDetails)),
		"\u5206\u7ea7\u89c4\u5219\uff1a\u9ad8\u7f6e\u4fe1 >=80\uff1b\u5efa\u8bae\u590d\u6838 70-79\uff1b\u89c2\u5bdf\u9879 <70",
		"",
		"\u660e\u7ec6\uff08\u756a\u53f7 | \u8bc4\u5206 | \u5927\u5c0fGB | \u539f\u56e0 | \u8bc1\u636e | \u8def\u5f84\uff09\uff1a",
		"----------------------------------------",
	}
	if len(adRiskCodeDetails) == 0 {
		lines = append(lines, "\u672a\u53d1\u73b0\u542b\u5f00\u5934\u5e7f\u544a\u660e\u7ec6\u3002")
		return lines
	}
	for index, item := range adRiskCodeDetails {
		reasons := "-"
		if len(item.Reasons) > 0 {
			reasons = strings.Join(item.Reasons, "; ")
		}
		sourcePath := item.SourcePath
		if sourcePath == "" {
			sourcePath = "-"
		}
		lines = append(
			lines,
			fmt.Sprintf("%d. %s | %.0f | %sGB | %s | %s | %s", index+1, item.FilmCode, item.MaxScore, formatBytesToGB(item.Size), reasons, evidenceSummary(item.Evidence), sourcePath),
		)
	}
	return lines
}

func buildMagnetReportLines(nowText string, title string, totalEntries int, entries []CodeEntry, emptyText string) []string {
	lines := []string{
		title,
		"\u751f\u6210\u65f6\u95f4\uff1a" + nowText,
		fmt.Sprintf("\u8865\u6293\u6761\u76ee\u603b\u6570\uff1a%d", totalEntries),
		"",
	}
	appendMagnetEntryLines(&lines, entries, emptyText)
	return lines
}

func buildMissingMagnetLines(nowText string, summary Summary, missingMagnetEntries []CodeEntry) []string {
	lines := []string{
		"\u89c6\u9891\u6574\u7406\u52a9\u624b - \u9057\u6f0f\u756a\u53f7\u78c1\u529b\u8865\u6293",
		"\u751f\u6210\u65f6\u95f4\uff1a" + nowText,
		fmt.Sprintf("\u722c\u866b\u756a\u53f7\u603b\u6570\uff1a%d", summary.ExpectedCodeTotal),
		fmt.Sprintf("\u672c\u5730\u8bc6\u522b\u756a\u53f7\u603b\u6570\uff1a%d", summary.DetectedCodeCount),
		fmt.Sprintf("\u9057\u6f0f\u756a\u53f7\u603b\u6570\uff1a%d", summary.MissingCodeCount),
		fmt.Sprintf("\u8865\u6293\u78c1\u529b\u603b\u6570\uff1a%d", summary.MissingMagnetCount),
		"",
	}
	appendMagnetEntryLines(&lines, missingMagnetEntries, "\u672a\u751f\u6210\u9057\u6f0f\u756a\u53f7\u8865\u6293\u78c1\u529b\u3002")
	return lines
}

// appendMagnetEntryLines is the shared text layout for supplement reports.
// Missing-download and intro-ad magnet reports differ in headings only.
func appendMagnetEntryLines(lines *[]string, entries []CodeEntry, emptyText string) {
	if len(entries) == 0 {
		*lines = append(*lines, emptyText)
		return
	}
	for index, entry := range entries {
		code := normalizeFilmID(entry.Code)
		if code == "" {
			code = "\u672a\u77e5\u756a\u53f7"
		}
		*lines = append(*lines, fmt.Sprintf("%d. [%s]", index+1, code))
		magnets := mergeMagnetEntries(entry.Magnets)
		if len(magnets) == 0 {
			*lines = append(*lines, "   \uff08\u65e0\u53ef\u7528\u78c1\u529b\uff09")
		} else {
			for magnetIndex, magnet := range magnets {
				sizeLabel := ""
				if strings.TrimSpace(magnet.Size) != "" {
					sizeLabel = " [" + magnet.Size + "]"
				}
				*lines = append(*lines, fmt.Sprintf("   %d)%s %s", magnetIndex+1, sizeLabel, magnet.Link))
			}
		}
		*lines = append(*lines, "")
	}
}
