package organizer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// run_scan_phase.go owns the Go organizer's initial classification pass.
//
// Ownership summary:
// 1) collect files from the target tree
// 2) classify them into waiting candidates, delete candidates, and unmatched
//    records
// 3) attach crawl-derived code matches once for later phases to reuse
//
// File map for maintainers:
// 1) scan-phase top-level classifier loop
// 2) candidate/delete/unmatched classification branches
// 3) progress/log emission for scan-stage milestones

// scanFiles owns the organizer's classification pass: collect files, identify
// qualified videos, and split everything else into unmatched or delete queues.
func (ctx *organizerRunContext) scanFiles() scanPhaseResult {
	files := collectFiles(ctx.normalizedRootPath, ctx.options.IncludeSubdirectories, ctx.videoExtensionSet)
	ctx.logf("info", fmt.Sprintf("\u626b\u63cf\u5b8c\u6210\uff0c\u5f85\u5904\u7406\u6587\u4ef6 %d \u4e2a\u3002", len(files)))
	ctx.progressf(ProgressEntry{"phase": progressPhaseScanStart, "total": len(files), "processed": 0})

	ctx.summary.ScannedTotal = len(files)
	result := scanPhaseResult{
		candidates:          []Candidate{},
		unmatchedCandidates: []Candidate{},
		pendingDelete:       []Candidate{},
		unmatchedRecords:    []UnmatchedRecord{},
		detectedFilmCodes:   map[string]struct{}{},
	}

	hasExpectedCodes := len(ctx.codeSet) > 0
	adActionLogPrefix := "\u5df2\u5f52\u5165\u5f85\u5220\u9664"
	if ctx.adFileAction == adFileActionDeleteDirectly {
		adActionLogPrefix = "\u5f85\u76f4\u63a5\u5220\u9664"
	}

	// The scan loop is where organizer classification becomes stable:
	// - small/non-video content becomes unmatched/delete
	// - qualified videos become waiting candidates
	// - crawl-artifact code matching is attached once and reused later
	for fileIndex, fileEntry := range files {
		srcPath := strings.TrimSpace(fileEntry.Path)
		if srcPath == "" {
			ctx.summary.FailedOperations++
			continue
		}
		// A broad download root can contain crawler artifacts, logs, and hidden
		// organizer state below a media folder. Protect these names before the
		// size/type classifier puts non-video files into pendingDelete.
		if ctx.isBatchDeleteProtectedPath(srcPath, nil) {
			result.unmatchedRecords = append(result.unmatchedRecords, UnmatchedRecord{
				Path:   srcPath,
				Reason: "软件产物或受保护内容，保留原文件",
			})
			ctx.logf("info", "检测到软件产物或受保护内容，跳过整理和删除："+srcPath)
			continue
		}

		scannedCount := fileIndex + 1
		if shouldReportProgress(scannedCount, len(files), 30) {
			ctx.progressf(ProgressEntry{
				"phase":          progressPhaseScanProgress,
				"total":          len(files),
				"processed":      scannedCount,
				"videoTotal":     ctx.summary.VideoTotal,
				"qualifiedVideo": ctx.summary.QualifiedVideo,
			})
			ctx.logf("info", fmt.Sprintf("\u626b\u63cf\u8fdb\u5ea6 %d/%d\uff08\u5df2\u8bc6\u522b\u89c6\u9891 %d\uff0c\u6709\u6548\u89c6\u9891 %d\uff09", scannedCount, len(files), ctx.summary.VideoTotal, ctx.summary.QualifiedVideo))
		}

		item := Candidate{
			Src:         srcPath,
			IsVideo:     fileEntry.IsVideo,
			IsRootLevel: fileEntry.IsRootLevel,
		}
		if item.IsVideo {
			fileInfo, err := os.Stat(srcPath)
			if err != nil || !fileInfo.Mode().IsRegular() {
				ctx.summary.FailedOperations++
				ctx.logf("warn", "\u6587\u4ef6\u72b6\u6001\u5f02\u5e38\uff0c\u5df2\u8df3\u8fc7\uff1a"+srcPath)
				continue
			}
			item.Size = fileInfo.Size()
			ctx.summary.VideoTotal++
		}

		isLargeVideo := item.IsVideo && item.Size >= ctx.minSizeBytes
		if !isLargeVideo {
			reason := "\u975e\u89c6\u9891\u6587\u4ef6"
			if item.IsVideo {
				reason = "\u4f4e\u4e8e\u6700\u5c0f\u5bb9\u91cf\u9608\u503c\uff0c\u5224\u5b9a\u4e3a\u5e7f\u544a\u6587\u4ef6"
				ctx.summary.SkippedSmall++
			}
			if item.IsRootLevel {
				result.unmatchedRecords = append(result.unmatchedRecords, UnmatchedRecord{
					Path:    item.Src,
					Size:    item.Size,
					IsVideo: item.IsVideo,
					Reason:  reason + "\uff1b\u6839\u76ee\u5f55\u6587\u4ef6\u5df2\u4fdd\u7559\uff0c\u907f\u514d\u8bef\u5220",
				})
				ctx.logf("warn", fmt.Sprintf("\u6839\u76ee\u5f55\u6587\u4ef6\u5df2\u4fdd\u7559\uff0c\u4e0d\u8fdb\u5165\u5f85\u5220\u9664\uff1a%s\uff08%s\uff09", item.Src, reason))
				continue
			}

			result.pendingDelete = append(result.pendingDelete, item)
			result.unmatchedRecords = append(result.unmatchedRecords, UnmatchedRecord{Path: item.Src, Size: item.Size, IsVideo: item.IsVideo, Reason: reason})
			if item.IsVideo {
				ctx.logf("info", fmt.Sprintf("%s\uff1a%s\uff08%s\uff09", adActionLogPrefix, item.Src, reason))
			}
			continue
		}

		ctx.summary.NonAdVideo++
		normalizedFilmCode := ""
		matchedMagnetAlias := ""
		if expectedFilmCode, alias, _ := matchExpectedCodeFromPathWithAliases(item.Src, ctx.normalizedRootPath, ctx.codeSet, ctx.tokenSet, ctx.expectedCodeAliasIndex); expectedFilmCode != "" {
			normalizedFilmCode = normalizeFilmID(expectedFilmCode)
			matchedMagnetAlias = alias
		} else if extractedFilmCode := extractFilmCodeFromFile(item.Src, ctx.codeSet, ctx.tokenSet); extractedFilmCode != "" {
			normalizedFilmCode = normalizeFilmID(extractedFilmCode)
		}
		expectedMatched := normalizedFilmCode != "" && (!hasExpectedCodes || containsCode(ctx.codeSet, normalizedFilmCode))
		if matchedMagnetAlias != "" {
			expectedMatched = true
			ctx.logf("info", fmt.Sprintf("磁力别名命中爬虫名单：%s -> %s", matchedMagnetAlias, normalizedFilmCode))
		}
		if hasExpectedCodes && !expectedMatched {
			fallbackValue := filepath.Base(item.Src)
			if relativePath, err := filepath.Rel(ctx.normalizedRootPath, item.Src); err == nil && relativePath != "." {
				fallbackValue = relativePath
			}
			if fallbackFilmCode := matchExpectedCodeFallback(fallbackValue, ctx.codeSet); fallbackFilmCode != "" {
				normalizedFilmCode = fallbackFilmCode
				expectedMatched = true
				ctx.logf("info", fmt.Sprintf("兜底识别番号并按爬虫名单规范化：%s -> %s", item.Src, normalizedFilmCode))
			}
		}
		if normalizedFilmCode != "" {
			item.FilmCode = normalizedFilmCode
			item.ExpectedCodeMatched = expectedMatched
			result.detectedFilmCodes[normalizedFilmCode] = struct{}{}
			if hasExpectedCodes && expectedMatched {
				ctx.summary.MatchedToCrawlCode++
			}
		}

		if hasExpectedCodes && ctx.options.StrictExpectedCodes && !expectedMatched {
			reason := "未命中爬虫番号名单"
			if normalizedFilmCode == "" {
				reason = "未识别到爬虫番号名单中的番号"
				ctx.summary.SkippedNoCode++
			} else {
				reason += "（" + normalizedFilmCode + "）"
			}
			item.KeepOriginalReason = reason
			result.unmatchedCandidates = append(result.unmatchedCandidates, item)
			result.unmatchedRecords = append(result.unmatchedRecords, UnmatchedRecord{
				Path:    item.Src,
				Size:    item.Size,
				IsVideo: item.IsVideo,
				Reason:  reason + "，严格匹配模式下不作为有效视频整理",
			})
			ctx.logf("info", fmt.Sprintf("严格匹配未通过，已归入待清理：%s（%s）", item.Src, reason))
			continue
		}

		if normalizedFilmCode != "" {
			item.RenameByFilmCode = true
			if expectedMatched {
				ctx.logf("info", fmt.Sprintf("\u8bc6\u522b\u4e3a\u6709\u6548\u89c6\u9891\uff1a%s -> %s", item.Src, normalizedFilmCode))
			} else if hasExpectedCodes {
				item.KeepOriginalReason = "\u672a\u547d\u4e2d\u722c\u866b\u756a\u53f7\u540d\u5355\uff08" + normalizedFilmCode + "\uff09\uff0c\u4ecd\u6309\u8bc6\u522b\u756a\u53f7\u6539\u540d"
				ctx.logf("info", fmt.Sprintf("\u8bc6\u522b\u5230\u5408\u6cd5\u756a\u53f7\u4f46\u672a\u547d\u4e2d\u722c\u866b\u756a\u53f7\u540d\u5355\uff0c\u4ecd\u6309\u756a\u53f7\u6539\u540d\uff1a%s -> %s", item.Src, normalizedFilmCode))
			} else {
				ctx.logf("info", fmt.Sprintf("\u8bc6\u522b\u4e3a\u6709\u6548\u89c6\u9891\uff1a%s -> %s", item.Src, normalizedFilmCode))
			}
		} else {
			if normalizedFilmCode == "" {
				ctx.summary.SkippedNoCode++
				item.KeepOriginalReason = "\u672a\u8bc6\u522b\u756a\u53f7\uff0c\u4fdd\u7559\u539f\u540d"
			} else {
				item.KeepOriginalReason = "\u672a\u547d\u4e2d\u722c\u866b\u756a\u53f7\u540d\u5355\uff08" + normalizedFilmCode + "\uff09\uff0c\u4fdd\u7559\u539f\u540d"
			}
			ctx.logf("info", fmt.Sprintf("\u8bc6\u522b\u4e3a\u6709\u6548\u89c6\u9891\u4f46\u4fdd\u7559\u539f\u540d\uff1a%s\uff08%s\uff09", item.Src, item.KeepOriginalReason))
		}

		ctx.summary.QualifiedVideo++
		result.candidates = append(result.candidates, item)
	}

	ctx.summary.UnmatchedVideo = len(result.unmatchedRecords)
	ctx.summary.AdFileCount = len(result.pendingDelete)
	ctx.summary.DetectedCodeCount = len(result.detectedFilmCodes)
	ctx.progressf(ProgressEntry{
		"phase":          progressPhaseScanCompleted,
		"total":          len(files),
		"processed":      len(files),
		"waitingTotal":   len(result.candidates),
		"deleteTotal":    len(result.pendingDelete),
		"introAdTotal":   0,
		"videoTotal":     ctx.summary.VideoTotal,
		"qualifiedVideo": ctx.summary.QualifiedVideo,
	})

	return result
}
