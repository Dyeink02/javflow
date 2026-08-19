package organizer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// run_transfer_phase.go owns the Go organizer transfer-stage filesystem side
// effects after scan/classification is complete.
//
// Ownership summary:
// 1) move accepted candidates into waiting-area with planned names
// 2) route delete/ad candidates after waiting transfers settle
// 3) keep transfer/delete-specific failure handling separate from scan/report
//    phases
//
// File map for maintainers:
// 1) waiting-area transfer path
// 2) pending-delete/direct-delete path
// 3) transfer/delete progress + failure accounting
// 4) source-folder protection rules after failed waiting moves

// moveCandidatesToWaiting owns rename planning and transfer into the waiting
// directory. This phase is the main source of "recognized but not moved"
// organizer bugs, so its state is kept separate from scan/delete logic.
func (ctx *organizerRunContext) moveCandidatesToWaiting(candidates []Candidate) waitingPhaseResult {
	// Transfer phase consumes only scan output plus normalized context. It should
	// not rediscover crawl-artifact rules or rescan the filesystem tree.
	result := waitingPhaseResult{
		renameRecords:            []RenameRecord{},
		waitingMoveFailedSources: []string{},
	}

	targetNames := planTargetNames(candidates, ctx.suffixStrategy)
	ctx.progressf(ProgressEntry{
		"phase":        progressPhaseWaitingStart,
		"total":        len(candidates),
		"processed":    0,
		"operation":    "准备改名并移入待整理",
		"deleteTotal":  ctx.summary.AdFileCount,
		"introAdTotal": 0,
	})

	for index, candidate := range candidates {
		plannedName := strings.TrimSpace(targetNames[index])
		if plannedName == "" {
			plannedName = filepath.Base(candidate.Src)
		}
		destinationPath := filepath.Join(ctx.paths.WaitingDir, plannedName)
		originalName := filepath.Base(candidate.Src)
		renameApplied := candidate.RenameByFilmCode && candidate.FilmCode != ""
		keepOriginalReason := candidate.KeepOriginalReason
		if keepOriginalReason == "" && !renameApplied {
			keepOriginalReason = "\u672a\u547d\u4e2d\u756a\u53f7\uff0c\u4fdd\u7559\u539f\u540d"
		}

		if ctx.dryRun {
			ctx.summary.MovedToWaiting++
			result.renameRecords = append(result.renameRecords, RenameRecord{
				OriginalName:        originalName,
				OriginalPath:        candidate.Src,
				WaitingPath:         destinationPath,
				NewName:             filepath.Base(destinationPath),
				FilmCode:            candidate.FilmCode,
				RenameApplied:       renameApplied,
				Note:                keepOriginalReason,
				ExpectedCodeMatched: candidate.ExpectedCodeMatched,
				Size:                candidate.Size,
			})
			ctx.logf("info", fmt.Sprintf("[\u9884\u89c8] \u5f85\u6574\u7406\uff1a%s -> %s", candidate.Src, destinationPath))
		} else {
			var movedPath string
			err := runWithProgressHeartbeat(
				"视频改名并移动",
				candidate.Src,
				ProgressEntry{
					"phase":      progressPhaseWaitingProgress,
					"total":      len(candidates),
					"processed":  index,
					"targetPath": destinationPath,
				},
				ctx.progressf,
				func() error {
					var moveErr error
					movedPath, moveErr = moveWithUnique(candidate.Src, destinationPath)
					return moveErr
				},
				ctx.logf,
			)
			if err != nil {
				ctx.summary.FailedOperations++
				result.waitingMoveFailedSources = append(result.waitingMoveFailedSources, filepath.Clean(candidate.Src))
				ctx.logf("warn", fmt.Sprintf("\u79fb\u52a8\u5230\u5f85\u6574\u7406\u5931\u8d25\uff1a%s\uff0c\u539f\u56e0\uff1a%s", candidate.Src, err.Error()))
			} else {
				ctx.summary.MovedToWaiting++
				result.renameRecords = append(result.renameRecords, RenameRecord{
					OriginalName:        originalName,
					OriginalPath:        candidate.Src,
					WaitingPath:         movedPath,
					NewName:             filepath.Base(movedPath),
					FilmCode:            candidate.FilmCode,
					RenameApplied:       renameApplied,
					Note:                keepOriginalReason,
					ExpectedCodeMatched: candidate.ExpectedCodeMatched,
					Size:                candidate.Size,
				})
				if renameApplied {
					ctx.logf("info", fmt.Sprintf("\u5df2\u79fb\u5165\u5f85\u6574\u7406\u5e76\u6309\u756a\u53f7\u6539\u540d\uff1a%s -> %s", candidate.Src, movedPath))
				} else {
					ctx.logf("info", fmt.Sprintf("\u5df2\u79fb\u5165\u5f85\u6574\u7406\u5e76\u4fdd\u7559\u539f\u540d\uff1a%s -> %s\uff08%s\uff09", candidate.Src, movedPath, keepOriginalReason))
				}
			}
		}

		ctx.progressf(ProgressEntry{
			"phase":            progressPhaseWaitingProgress,
			"total":            len(candidates),
			"processed":        index + 1,
			"operation":        "视频改名并移动",
			"currentPath":      candidate.Src,
			"targetPath":       destinationPath,
			"deleteTotal":      ctx.summary.AdFileCount,
			"introAdTotal":     0,
			"failedOperations": ctx.summary.FailedOperations,
		})
	}

	return result
}

func (ctx *organizerRunContext) moveUnmatchedCandidates(candidates []Candidate) []string {
	failedSources := []string{}
	if len(candidates) == 0 {
		return failedSources
	}

	ctx.logf("info", fmt.Sprintf("严格名单匹配未命中：%d 个视频将转移到未命中目录。", len(candidates)))
	for _, item := range candidates {
		if ctx.dryRun {
			ctx.summary.MovedToUnmatched++
			ctx.logf("info", "[预览] 待移入未命中："+item.Src)
			continue
		}
		destinationPath := filepath.Join(ctx.paths.UnmatchedDir, filepath.Base(item.Src))
		movedPath, err := moveWithUnique(item.Src, destinationPath)
		if err != nil {
			ctx.summary.FailedOperations++
			failedSources = append(failedSources, filepath.Clean(item.Src))
			ctx.logf("warn", fmt.Sprintf("移入未命中失败，源文件已保留：%s，原因：%s", item.Src, err.Error()))
			continue
		}
		ctx.summary.MovedToUnmatched++
		ctx.logf("warn", fmt.Sprintf("番号名单无法唯一命中，已移入未命中：%s -> %s", item.Src, movedPath))
	}
	return failedSources
}

// processPendingDelete owns the ad/delete queue. It is separated from waiting
// moves because delete failures and waiting failures are usually diagnosed from
// different reports and operator actions.
func (ctx *organizerRunContext) processPendingDelete(pendingDelete []Candidate, waitingMoveFailedSources []string) {
	// Delete routing runs after waiting moves so failed waiting transfers can
	// protect their source folders from aggressive cleanup.
	ctx.progressf(ProgressEntry{
		"phase":        progressPhaseDeleteStart,
		"total":        len(pendingDelete),
		"processed":    0,
		"operation":    "准备处理待删除内容",
		"adFileAction": ctx.adFileAction,
		"introAdTotal": 0,
	})
	if ctx.batchDelete && ctx.adFileAction == adFileActionDeleteDirectly {
		ctx.logf("info", fmt.Sprintf("批量删除已启用：%d 个待清理文件将在有效视频全部转移并写入报告后统一删除。", len(pendingDelete)))
		ctx.progressf(ProgressEntry{
			"phase":        progressPhaseDeleteProgress,
			"total":        len(pendingDelete),
			"processed":    0,
			"operation":    "等待生成报告后统一删除",
			"currentPath":  ctx.normalizedRootPath,
			"adFileAction": ctx.adFileAction,
		})
		return
	}

	deleteProcessed := 0
	pendingDeleteMap := map[string]Candidate{}
	for _, item := range pendingDelete {
		if item.Src != "" {
			pendingDeleteMap[filepath.Clean(item.Src)] = item
		}
	}

	// Directory-first delete/move keeps cloud-drive operations coarse-grained.
	// Once qualified videos have already been moved out, the remaining ad/trash
	// items under the same source folder should prefer one directory operation
	// over many per-file operations, unless that folder is protected by a failed
	// waiting move or is itself a managed/root directory.
	if !ctx.dryRun && len(pendingDeleteMap) > 0 {
		pendingDeleteSet := makePendingDeletePathSet(ctx.normalizedRootPath, pendingDelete)
		protectedPaths := normalizeProtectedPaths(waitingMoveFailedSources)
		directoryCandidates := map[string]struct{}{}
		for _, item := range pendingDeleteMap {
			sourcePath := filepath.Clean(item.Src)
			sourceDir := filepath.Dir(sourcePath)
			if sourceDir == "" || sourceDir == filepath.Clean(ctx.paths.RootPath) || isManagedRootChild(ctx.paths, sourceDir) {
				continue
			}
			directoryCandidates[sourceDir] = struct{}{}
		}

		sortedDirs := make([]string, 0, len(directoryCandidates))
		for dir := range directoryCandidates {
			sortedDirs = append(sortedDirs, dir)
		}
		sort.Slice(sortedDirs, func(i int, j int) bool {
			return len(sortedDirs[i]) < len(sortedDirs[j])
		})

		for _, sourceDir := range sortedDirs {
			hasProtectedSource := false
			for _, failedSource := range waitingMoveFailedSources {
				if failedSource == sourceDir || strings.HasPrefix(failedSource, sourceDir+string(os.PathSeparator)) {
					hasProtectedSource = true
					break
				}
			}
			if hasProtectedSource {
				continue
			}
			// Never use RemoveAll (or move an entire source directory) unless
			// every remaining regular file was explicitly classified for cleanup.
			// Download folders can also contain software caches, crawler output,
			// or user files that were not part of this run.
			if !ctx.batchDeleteDirectorySafe(sourceDir, pendingDeleteSet, protectedPaths) {
				ctx.logf("info", "按目录处理跳过未确认内容，改为只处理明确待删除文件："+sourceDir)
				continue
			}

			removedByDirectory := false
			operation := "按目录删除"
			switch ctx.adFileAction {
			case adFileActionDeleteDirectly:
				err := runWithProgressHeartbeat(
					operation,
					sourceDir,
					ProgressEntry{
						"phase":        progressPhaseDeleteProgress,
						"total":        len(pendingDelete),
						"processed":    deleteProcessed,
						"adFileAction": ctx.adFileAction,
					},
					ctx.progressf,
					func() error {
						// The first check above plans the coarse operation. Check again
						// at the exact destructive boundary because a downloader or
						// cloud mount may add an unclassified file while RemoveAll is
						// waiting to start.
						if !ctx.batchDeleteDirectorySafe(sourceDir, pendingDeleteSet, protectedPaths) {
							return fmt.Errorf("\u76ee\u5f55\u5728\u5220\u9664\u524d\u51fa\u73b0\u672a\u786e\u8ba4\u5185\u5bb9")
						}
						return removeDirectoryWithRetry(sourceDir, 5)
					},
					ctx.logf,
				)
				if err != nil {
					ctx.summary.FailedOperations++
					ctx.logf("warn", fmt.Sprintf("按目录删除失败：%s，原因：%s", sourceDir, err.Error()))
					continue
				}
				removedByDirectory = true
			case adFileActionMoveToDelete:
				operation = "按目录移入待删除"
				destinationDir := resolveDeleteDestinationPath(ctx.paths, sourceDir)
				movedDir := ""
				err := runWithProgressHeartbeat(
					operation,
					sourceDir,
					ProgressEntry{
						"phase":        progressPhaseDeleteProgress,
						"total":        len(pendingDelete),
						"processed":    deleteProcessed,
						"adFileAction": ctx.adFileAction,
						"targetPath":   destinationDir,
					},
					ctx.progressf,
					func() error {
						var moveErr error
						movedDir, moveErr = moveDirectoryWithUnique(sourceDir, destinationDir)
						return moveErr
					},
					ctx.logf,
				)
				if err == nil {
					removedByDirectory = true
					ctx.logf("info", fmt.Sprintf("已按目录整包移入待删除：%s -> %s", sourceDir, movedDir))
				}
			}
			if !removedByDirectory {
				continue
			}

			removedInDir := 0
			for sourcePath := range pendingDeleteMap {
				if sourcePath == sourceDir || strings.HasPrefix(sourcePath, sourceDir+string(os.PathSeparator)) {
					delete(pendingDeleteMap, sourcePath)
					removedInDir++
				}
			}
			if removedInDir > 0 {
				deleteProcessed += removedInDir
				if ctx.adFileAction == adFileActionDeleteDirectly {
					ctx.summary.DeletedDirectly += removedInDir
					ctx.logf("info", fmt.Sprintf("已按目录快速删除：%s（文件 %d 个）", sourceDir, removedInDir))
				} else {
					ctx.summary.MovedToDelete += removedInDir
					ctx.logf("info", fmt.Sprintf("已按目录整包移入待删除：%s（文件 %d 个）", sourceDir, removedInDir))
				}
				if shouldReportProgress(deleteProcessed, len(pendingDelete), 10) {
					ctx.progressf(ProgressEntry{
						"phase":            progressPhaseDeleteProgress,
						"total":            len(pendingDelete),
						"processed":        deleteProcessed,
						"operation":        operation,
						"currentPath":      sourceDir,
						"adFileAction":     ctx.adFileAction,
						"introAdTotal":     0,
						"failedOperations": ctx.summary.FailedOperations,
					})
				}
			}
		}
	}

	remainingDeleteItems := make([]Candidate, 0, len(pendingDeleteMap))
	for _, item := range pendingDeleteMap {
		remainingDeleteItems = append(remainingDeleteItems, item)
	}
	sort.Slice(remainingDeleteItems, func(i int, j int) bool {
		return strings.ToLower(remainingDeleteItems[i].Src) < strings.ToLower(remainingDeleteItems[j].Src)
	})

	for _, item := range remainingDeleteItems {
		shouldLogDeleteDetail := item.IsVideo || shouldReportProgress(deleteProcessed+1, len(pendingDelete), 80)
		if ctx.dryRun {
			if ctx.adFileAction == adFileActionDeleteDirectly {
				ctx.summary.DeletedDirectly++
			} else {
				ctx.summary.MovedToDelete++
			}
			if shouldLogDeleteDetail {
				if ctx.adFileAction == adFileActionDeleteDirectly {
					ctx.logf("info", "[\u9884\u89c8] \u5f85\u76f4\u63a5\u5220\u9664\uff1a"+item.Src)
				} else {
					ctx.logf("info", "[\u9884\u89c8] \u5f85\u79fb\u5165\u5f85\u5220\u9664\uff1a"+item.Src)
				}
			}
		} else if ctx.adFileAction == adFileActionDeleteDirectly {
			err := runWithProgressHeartbeat(
				"删除文件",
				item.Src,
				ProgressEntry{
					"phase":        progressPhaseDeleteProgress,
					"total":        len(pendingDelete),
					"processed":    deleteProcessed,
					"adFileAction": ctx.adFileAction,
				},
				ctx.progressf,
				func() error { return os.Remove(item.Src) },
				ctx.logf,
			)
			if err != nil {
				ctx.summary.FailedOperations++
				ctx.logf("warn", fmt.Sprintf("\u76f4\u63a5\u5220\u9664\u5931\u8d25\uff1a%s\uff0c\u539f\u56e0\uff1a%s", item.Src, err.Error()))
			} else {
				ctx.summary.DeletedDirectly++
				if shouldLogDeleteDetail {
					ctx.logf("info", "\u5df2\u76f4\u63a5\u5220\u9664\uff1a"+item.Src)
				}
			}
		} else {
			destinationPath := resolveDeleteDestinationPath(ctx.paths, item.Src)
			movedPath := ""
			err := runWithProgressHeartbeat(
				"移入待删除",
				item.Src,
				ProgressEntry{
					"phase":        progressPhaseDeleteProgress,
					"total":        len(pendingDelete),
					"processed":    deleteProcessed,
					"adFileAction": ctx.adFileAction,
					"targetPath":   destinationPath,
				},
				ctx.progressf,
				func() error {
					var moveErr error
					movedPath, moveErr = moveWithUnique(item.Src, destinationPath)
					return moveErr
				},
				ctx.logf,
			)
			if err != nil {
				ctx.summary.FailedOperations++
				ctx.logf("warn", fmt.Sprintf("\u79fb\u5165\u5f85\u5220\u9664\u5931\u8d25\uff1a%s\uff0c\u539f\u56e0\uff1a%s", item.Src, err.Error()))
			} else {
				ctx.summary.MovedToDelete++
				if shouldLogDeleteDetail {
					ctx.logf("info", fmt.Sprintf("\u5df2\u79fb\u5165\u5f85\u5220\u9664\uff1a%s -> %s", item.Src, movedPath))
				}
			}
		}

		deleteProcessed++
		if shouldReportProgress(deleteProcessed, len(pendingDelete), 10) {
			ctx.progressf(ProgressEntry{
				"phase":            progressPhaseDeleteProgress,
				"total":            len(pendingDelete),
				"processed":        deleteProcessed,
				"operation":        "处理待删除内容",
				"currentPath":      item.Src,
				"adFileAction":     ctx.adFileAction,
				"introAdTotal":     0,
				"failedOperations": ctx.summary.FailedOperations,
			})
		}
	}
}
