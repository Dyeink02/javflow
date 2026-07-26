package organizer

import "time"

// report.go now stays intentionally thin.
//
// It coordinates report generation, while formatting and data shaping live in
// dedicated files below. If a report path looks wrong, inspect the write
// requests here before diving into the text builders.
//
// Split:
// - this file coordinates which reports are written
// - report_data.go shapes derived aggregates
// - report_text.go owns human-readable text layout and file encoding
//
// Ownership summary:
// 1) coordinate which organizer reports get written
// 2) keep report path/write orchestration centralized
// 3) separate report coordination from data shaping and text formatting
//
// File map for maintainers:
// 1) top-level report coordinator
// 2) report write request assembly
// 3) final report result map shaping

func writeReports(
	paths Paths,
	summary Summary,
	renameRecords []RenameRecord,
	unmatchedRecords []UnmatchedRecord,
	adRiskRecords []AdRiskRecord,
	adRiskMagnetEntries []CodeEntry,
	missingMagnetEntries []CodeEntry,
) (map[string]string, error) {
	// Report generation stays as one thin coordinator so path resolution and
	// per-report text building do not get mixed together again.
	nowText := time.Now().Format("2006-01-02 15:04:05")

	writes := []reportWriteRequest{
		{
			key:   "renameMap",
			path:  paths.RenameMapPath,
			lines: buildRenameReportLines(nowText, summary, renameRecords),
		},
		{
			key:   "unmatched",
			path:  paths.UnmatchedPath,
			lines: buildUnmatchedReportLines(nowText, summary, unmatchedRecords),
		},
		{
			key:   "adRiskCodes",
			path:  paths.AdRiskCodesPath,
			lines: buildAdRiskCodeLines(nowText, adRiskRecords),
		},
		{
			key:   "adRiskDetail",
			path:  paths.AdRiskDetailPath,
			lines: buildAdRiskDetailLines(nowText, adRiskRecords),
		},
		{
			key:   "adRiskMagnets",
			path:  paths.AdRiskMagnetsPath,
			lines: buildMagnetReportLines(nowText, "\u89c6\u9891\u6574\u7406\u52a9\u624b - \u542b\u5f00\u5934\u5e7f\u544a\u8865\u6293\u78c1\u529b", len(adRiskMagnetEntries), adRiskMagnetEntries, "\u672a\u751f\u6210\u542b\u5f00\u5934\u5e7f\u544a\u8865\u6293\u78c1\u529b\u3002"),
		},
		{
			key:   "missingMagnets",
			path:  paths.MissingMagnetsPath,
			lines: buildMissingMagnetLines(nowText, summary, missingMagnetEntries),
		},
	}

	reportMap := map[string]string{}
	for _, item := range writes {
		if err := writeTextFile(item.path, item.lines); err != nil {
			return nil, err
		}
		reportMap[item.key] = item.path
	}

	return reportMap, nil
}
