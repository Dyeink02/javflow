package organizer

import (
	"fmt"
	"os"
	"strings"
)

// run_review_phase.go owns the Go organizer's post-transfer review work.
//
// Ownership summary:
// 1) perform optional intro-ad risk review on waiting-area files
// 2) collect review-stage diagnostics without reopening scan classification
// 3) keep evaluator-driven post-processing separate from transfer/report phases
//
// File map for maintainers:
// 1) review-phase top-level loop
// 2) evaluator invocation and result classification
// 3) progress/log emission for review-stage milestones

// reviewIntroAdRisk owns the optional post-move ad review. It is intentionally
// isolated because it is the only organizer phase that may call an external
// evaluator and then move files again.
func (ctx *organizerRunContext) reviewIntroAdRisk(renameRecords []RenameRecord) []AdRiskRecord {
	// Review phase reads waiting outputs only. It must not alter earlier scan
	// classification or rename planning rules.
	adRiskRecords := []AdRiskRecord{}
	ctx.progressf(ProgressEntry{
		"phase":            progressPhaseIntroAdStart,
		"total":            len(renameRecords),
		"processed":        0,
		"failedOperations": ctx.summary.FailedOperations,
	})

	if !ctx.adDetectionEnabled {
		ctx.logf("info", "\u5df2\u5173\u95ed\u5f00\u5934\u5e7f\u544a\u68c0\u6d4b\uff0c\u8df3\u8fc7\u540e\u7f6e\u590d\u6838\u9636\u6bb5\u3002")
		ctx.progressf(ProgressEntry{"phase": progressPhaseIntroAdProgress, "total": len(renameRecords), "processed": len(renameRecords), "failedOperations": ctx.summary.FailedOperations})
		return adRiskRecords
	}
	if ctx.options.EvaluateAdRisk == nil || ctx.dryRun {
		ctx.logf("warn", "\u5f00\u5934\u5e7f\u544a\u68c0\u6d4b\u5df2\u542f\u7528\uff0c\u4f46\u5f53\u524d\u4e0d\u662f\u53ef\u6267\u884c\u68c0\u6d4b\u72b6\u6001\uff0c\u5df2\u8df3\u8fc7\u540e\u7f6e\u590d\u6838\u3002")
		ctx.progressf(ProgressEntry{"phase": progressPhaseIntroAdProgress, "total": len(renameRecords), "processed": len(renameRecords), "failedOperations": ctx.summary.FailedOperations})
		return adRiskRecords
	}

	ctx.logf("info", "\u5f00\u5934\u5e7f\u544a\u540e\u7f6e\u590d\u6838\u5e76\u53d1\u6570\uff1a1")
	for index, record := range renameRecords {
		waitingPath := strings.TrimSpace(record.WaitingPath)
		filmCode := normalizeFilmID(record.FilmCode)
		if waitingPath == "" {
			ctx.summary.FailedOperations++
			ctx.logf("warn", "\u5f00\u5934\u5e7f\u544a\u540e\u7f6e\u590d\u6838\u8df3\u8fc7\uff1a\u7f3a\u5c11\u5f85\u6574\u7406\u8def\u5f84\u3002")
		} else {
			if fileInfo, err := os.Stat(waitingPath); err != nil || !fileInfo.Mode().IsRegular() {
				ctx.summary.AdDetectionErrors++
				ctx.logf("warn", "\u5f00\u5934\u5e7f\u544a\u540e\u7f6e\u590d\u6838\u5931\u8d25\uff1a"+waitingPath+"\uff0c\u539f\u56e0\uff1a\u5f85\u6574\u7406\u6587\u4ef6\u4e0d\u5b58\u5728\u6216\u4e0d\u53ef\u8bbf\u95ee")
			} else {
				adRiskResult, err := ctx.options.EvaluateAdRisk(AdRiskRequest{
					VideoPath:   waitingPath,
					StreamURL:   mapPathToStreamURL(waitingPath, ctx.normalizedRootPath, ctx.options.AlistBaseURL),
					FilmCode:    filmCode,
					AdThreshold: ctx.adThreshold,
					ModelType:   ctx.adModelType,
				})
				if err != nil {
					ctx.summary.AdDetectionErrors++
					ctx.logf("warn", fmt.Sprintf("\u5f00\u5934\u5e7f\u544a\u540e\u7f6e\u590d\u6838\u5931\u8d25\uff1a%s\uff0c\u539f\u56e0\uff1a%s", waitingPath, err.Error()))
				} else if adRiskResult.IsAd {
					reasonText := buildAdRiskReason(adRiskResult)
					destinationPath := waitingPath
					movedToIntroAd := false
					ctx.summary.AdRiskRejected++
					targetPath := resolveIntroAdDestinationPath(ctx.paths, waitingPath)
					movedPath, err := moveWithUnique(waitingPath, targetPath)
					if err != nil {
						ctx.summary.FailedOperations++
						ctx.logf("warn", fmt.Sprintf("\u547d\u4e2d\u5f00\u5934\u5e7f\u544a\u98ce\u9669\uff0c\u4f46\u79fb\u5165\u201c\u542b\u5f00\u5934\u5e7f\u544a\u201d\u5931\u8d25\uff1a%s\uff0c\u539f\u56e0\uff1a%s", waitingPath, err.Error()))
					} else {
						destinationPath = movedPath
						ctx.summary.MovedToIntroAd++
						if ctx.summary.MovedToWaiting > 0 {
							ctx.summary.MovedToWaiting--
						}
						movedToIntroAd = true
					}
					adRiskRecords = append(adRiskRecords, AdRiskRecord{
						FilmCode:   filmCode,
						SourcePath: destinationPath,
						Size:       record.Size,
						Score:      adRiskResult.Score,
						Threshold:  adRiskResult.Threshold,
						Reasons:    adRiskResult.Reasons,
						Evidence:   adRiskResult.Evidence,
					})
					if movedToIntroAd {
						ctx.logf("warn", fmt.Sprintf("\u547d\u4e2d\u5f00\u5934\u5e7f\u544a\u98ce\u9669\uff0c\u5df2\u5f52\u5165\u201c\u542b\u5f00\u5934\u5e7f\u544a\u201d\uff1a%s -> %s\uff08%s\uff09", waitingPath, destinationPath, reasonText))
					} else {
						ctx.logf("info", fmt.Sprintf("\u547d\u4e2d\u5f00\u5934\u5e7f\u544a\u98ce\u9669\uff0c\u4fdd\u7559\u5728\u5f85\u6574\u7406\u5f85\u4eba\u5de5\u590d\u6838\uff1a%s\uff08%s\uff09", waitingPath, reasonText))
					}
				}
			}
		}

		processed := index + 1
		if shouldReportProgress(processed, len(renameRecords), 20) {
			ctx.progressf(ProgressEntry{"phase": progressPhaseIntroAdProgress, "total": len(renameRecords), "processed": processed, "failedOperations": ctx.summary.FailedOperations})
		}
		if shouldReportProgress(processed, len(renameRecords), 80) {
			ctx.logf("info", fmt.Sprintf("\u5f00\u5934\u5e7f\u544a\u540e\u7f6e\u590d\u6838\u8fdb\u5ea6 %d/%d", processed, len(renameRecords)))
		}
	}

	return adRiskRecords
}

// buildSupplementData prepares the organizer-side follow-up artifacts after all
// file movement is settled. Missing-code and intro-ad supplement outputs are
// derived data only; they should not change earlier scan/transfer decisions.
func (ctx *organizerRunContext) buildSupplementData(adRiskRecords []AdRiskRecord, detectedFilmCodes map[string]struct{}) supplementPhaseResult {
	// Supplement generation is a reporting/artifact pass only. It should derive
	// from settled results, never re-open transfer decisions.
	result := supplementPhaseResult{
		adRiskCodes:          []string{},
		adRiskMagnetEntries:  []CodeEntry{},
		missingCodes:         []string{},
		missingMagnetEntries: []CodeEntry{},
	}

	for _, record := range adRiskRecords {
		if code := normalizeFilmID(record.FilmCode); code != "" {
			result.adRiskCodes = append(result.adRiskCodes, code)
		}
	}
	result.adRiskCodes = sortCodeAlphabetically(result.adRiskCodes)
	result.adRiskMagnetEntries = buildSupplementMagnetEntries(result.adRiskCodes, ctx.expectedCodeEntryMap)
	ctx.summary.SupplementMagnetCount = countMagnets(result.adRiskMagnetEntries)

	for code := range ctx.codeSet {
		if _, detected := detectedFilmCodes[code]; !detected {
			result.missingCodes = append(result.missingCodes, code)
		}
	}
	result.missingCodes = sortCodeAlphabetically(result.missingCodes)
	result.missingMagnetEntries = buildSupplementMagnetEntries(result.missingCodes, ctx.expectedCodeEntryMap)
	ctx.summary.MissingCodeCount = len(result.missingCodes)
	ctx.summary.MissingMagnetCount = countMagnets(result.missingMagnetEntries)

	if ctx.summary.MissingCodeCount > 0 {
		ctx.logf("warn", fmt.Sprintf("\u53d1\u73b0\u9057\u6f0f\u756a\u53f7 %d \u6761\uff0c\u5df2\u751f\u6210\u8865\u6293\u78c1\u529b\u62a5\u544a\uff08\u603b\u78c1\u529b %d \u6761\uff09\u3002", ctx.summary.MissingCodeCount, ctx.summary.MissingMagnetCount))
	} else {
		ctx.logf("info", "\u672a\u53d1\u73b0\u9057\u6f0f\u756a\u53f7\u3002")
	}

	return result
}
