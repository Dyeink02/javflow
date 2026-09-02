// Ownership summary:
//
//	This file implements organizer batch deletion for files explicitly
//	classified by the scan phase as removable.
//
// File map for maintainers:
//  1. Build a deletion plan from pendingDelete only.
//  2. Revalidate directory contents immediately before removal.
//  3. Execute progress-aware recursive removals.
package organizer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// executeBatchDelete removes only entries that the scan phase classified as
// pending deletion. The old implementation swept every non-whitelisted root
// entry into a temporary directory and then removed that directory. That made
// unrelated crawl artifacts look like deletion targets and was especially slow
// on cloud mounts. Directory targets are still removed recursively, but only
// after a second safety scan confirms that every remaining file is in
// pendingDelete.
func (ctx *organizerRunContext) executeBatchDelete(pendingDelete []Candidate, waitingMoveFailedSources []string) {
	if !ctx.batchDelete || ctx.adFileAction != adFileActionDeleteDirectly {
		return
	}

	targets := ctx.collectBatchDeleteTargets(pendingDelete, waitingMoveFailedSources)
	pendingSet := makePendingDeletePathSet(ctx.normalizedRootPath, pendingDelete)
	protectedPaths := normalizeProtectedPaths(waitingMoveFailedSources)
	for path := range pendingSet {
		if ctx.isBatchDeleteProtectedPath(path, protectedPaths) {
			delete(pendingSet, path)
		}
	}
	if ctx.dryRun {
		plannedCount := countPendingPathsUnderTargets(targets, pendingSet)
		ctx.summary.DeletedDirectly = plannedCount
		ctx.logf("info", fmt.Sprintf("[预览] 批量删除只处理 %d 个明确目标（待清理文件 %d 个），其他根目录内容保留。", len(targets), plannedCount))
		ctx.emitFinalizeProgress(1, "预览批量删除目标", ctx.normalizedRootPath, ProgressEntry{
			"subTotal":        len(targets),
			"subProcessed":    len(targets),
			"deletedDirectly": plannedCount,
		})
		return
	}
	if len(targets) == 0 {
		ctx.logf("info", "批量删除无需执行：没有明确判定为待删除的内容，其他根目录内容已保留。")
		return
	}

	deletedPendingCount := 0
	deletedTargetCount := 0
	totalTargets := len(targets)
	for i, targetPath := range targets {
		pendingCount := countPendingPathsUnderTarget(targetPath, pendingSet)
		if pendingCount == 0 {
			ctx.logf("warn", "批量删除跳过没有明确待删除文件的目标："+targetPath)
			continue
		}

		info, err := os.Lstat(targetPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			ctx.summary.FailedOperations++
			ctx.logf("warn", fmt.Sprintf("批量删除无法读取目标，已保留：%s，原因：%s", targetPath, err.Error()))
			continue
		}
		if info.IsDir() && !ctx.batchDeleteDirectorySafe(targetPath, pendingSet, protectedPaths) {
			ctx.logf("warn", "批量删除发现目标目录包含未确认内容，已保留以避免误删："+targetPath)
			continue
		}

		ctx.progressf(ProgressEntry{
			"phase":             progressPhaseFinalizeProgress,
			"finalizeTotal":     organizerFinalizeUnits,
			"finalizeProcessed": 1,
			"total":             totalTargets,
			"processed":         i,
			"subTotal":          totalTargets,
			"subProcessed":      i,
			"operation":         "批量删除已确认目标",
			"currentPath":       targetPath,
			"failedOperations":  ctx.summary.FailedOperations,
		})

		err = runWithProgressHeartbeat(
			"批量删除已确认目标",
			targetPath,
			ProgressEntry{
				"phase":             progressPhaseFinalizeProgress,
				"finalizeTotal":     organizerFinalizeUnits,
				"finalizeProcessed": 1,
				"total":             totalTargets,
				"processed":         i,
				"subTotal":          totalTargets,
				"subProcessed":      i,
				"failedOperations":  ctx.summary.FailedOperations,
			},
			ctx.progressf,
			func() error {
				currentInfo, statErr := os.Lstat(targetPath)
				if statErr != nil {
					return statErr
				}
				if currentInfo.IsDir() {
					// The directory may have changed while a network/cloud mount
					// was responding. Recheck immediately before RemoveAll so a
					// newly-created software artifact cannot be swept in.
					if !ctx.batchDeleteDirectorySafe(targetPath, pendingSet, protectedPaths) {
						return fmt.Errorf("目标目录在删除前出现未确认内容")
					}
					ctx.waitDeleteInterval()
					return removeDirectoryWithRetry(targetPath, 5)
				}
				ctx.waitDeleteInterval()
				return os.Remove(targetPath)
			},
			ctx.logf,
		)
		if err != nil {
			ctx.summary.FailedOperations++
			ctx.logf("warn", fmt.Sprintf("批量删除失败，已保留原内容：%s，原因：%s", targetPath, err.Error()))
			continue
		}

		deletedPendingCount += pendingCount
		deletedTargetCount++
		ctx.progressf(ProgressEntry{
			"phase":             progressPhaseFinalizeProgress,
			"finalizeTotal":     organizerFinalizeUnits,
			"finalizeProcessed": 1,
			"total":             totalTargets,
			"processed":         i + 1,
			"subTotal":          totalTargets,
			"subProcessed":      i + 1,
			"operation":         "批量删除已确认目标",
			"currentPath":       targetPath,
			"adFileAction":      ctx.adFileAction,
			"deletedDirectly":   deletedPendingCount,
			"failedOperations":  ctx.summary.FailedOperations,
		})
	}

	ctx.summary.DeletedDirectly += deletedPendingCount
	ctx.logf("info", fmt.Sprintf("批量删除完成：已清理 %d/%d 个明确目标（待清理文件 %d/%d 个），未确认内容均已保留。", deletedTargetCount, totalTargets, deletedPendingCount, len(pendingDelete)))
	ctx.progressf(ProgressEntry{
		"phase":             progressPhaseFinalizeProgress,
		"finalizeTotal":     organizerFinalizeUnits,
		"finalizeProcessed": 1,
		"total":             len(pendingDelete),
		"processed":         deletedPendingCount,
		"subTotal":          len(pendingDelete),
		"subProcessed":      deletedPendingCount,
		"operation":         "批量删除完成",
		"currentPath":       ctx.normalizedRootPath,
		"adFileAction":      ctx.adFileAction,
		"deletedDirectly":   ctx.summary.DeletedDirectly,
		"failedOperations":  ctx.summary.FailedOperations,
	})
}

// collectBatchDeleteTargets builds a conservative plan from pendingDelete. A
// root child is eligible for one recursive removal only when every file under
// it is in the pending set. Otherwise only individual pending files are
// returned, leaving unknown files and software artifacts untouched.
func (ctx *organizerRunContext) collectBatchDeleteTargets(pendingDelete []Candidate, waitingMoveFailedSources []string) []string {
	pendingSet := makePendingDeletePathSet(ctx.normalizedRootPath, pendingDelete)
	protectedPaths := normalizeProtectedPaths(waitingMoveFailedSources)
	rootPath := filepath.Clean(ctx.normalizedRootPath)
	topDirs := map[string]struct{}{}
	rootFiles := make([]string, 0)

	for sourcePath := range pendingSet {
		if ctx.isBatchDeleteProtectedPath(sourcePath, protectedPaths) {
			ctx.logf("warn", "批量删除已跳过受保护目标："+sourcePath)
			continue
		}
		relativePath, err := filepath.Rel(rootPath, sourcePath)
		if err != nil || relativePath == "" || strings.HasPrefix(relativePath, "..") || filepath.IsAbs(relativePath) {
			continue
		}
		if !strings.Contains(relativePath, string(os.PathSeparator)) {
			rootFiles = append(rootFiles, sourcePath)
			continue
		}
		topName := strings.Split(relativePath, string(os.PathSeparator))[0]
		topPath := filepath.Join(rootPath, topName)
		if isBatchDeleteProtectedName(ctx.paths, topName) {
			ctx.logf("warn", "批量删除已跳过软件管理目录："+topPath)
			continue
		}
		topDirs[topPath] = struct{}{}
	}

	targets := make([]string, 0, len(topDirs)+len(rootFiles))
	for topPath := range topDirs {
		if ctx.batchDeleteDirectorySafe(topPath, pendingSet, protectedPaths) {
			targets = append(targets, topPath)
			continue
		}
		for pendingPath := range pendingSet {
			if isPathInside(topPath, pendingPath) && !ctx.isBatchDeleteProtectedPath(pendingPath, protectedPaths) {
				targets = append(targets, pendingPath)
			}
		}
	}
	for _, rootFile := range rootFiles {
		if !ctx.isBatchDeleteProtectedPath(rootFile, protectedPaths) {
			targets = append(targets, rootFile)
		}
	}

	return deduplicateBatchDeleteTargets(targets)
}

func makePendingDeletePathSet(rootPath string, pendingDelete []Candidate) map[string]struct{} {
	result := make(map[string]struct{}, len(pendingDelete))
	for _, item := range pendingDelete {
		path := filepath.Clean(strings.TrimSpace(item.Src))
		if path == "" || path == filepath.Clean(rootPath) || !isPathInside(rootPath, path) {
			continue
		}
		if info, err := os.Lstat(path); err != nil || info.IsDir() {
			continue
		}
		result[path] = struct{}{}
	}
	return result
}

func normalizeProtectedPaths(paths []string) []string {
	result := make([]string, 0, len(paths))
	for _, item := range paths {
		path := filepath.Clean(strings.TrimSpace(item))
		if path != "" && path != "." {
			result = append(result, path)
		}
	}
	return result
}

func isBatchDeleteProtectedName(paths Paths, name string) bool {
	trimmed := strings.TrimSpace(name)
	lower := strings.ToLower(trimmed)
	if lower == "" || strings.HasPrefix(lower, ".") || strings.HasPrefix(lower, "run-") || strings.HasPrefix(lower, "video-organizer-") {
		return true
	}
	for whitelistName := range paths.BatchDeleteWhitelist() {
		if strings.EqualFold(strings.TrimSpace(whitelistName), trimmed) {
			return true
		}
	}
	return false
}

func (ctx *organizerRunContext) isBatchDeleteProtectedPath(path string, protectedPaths []string) bool {
	cleanedPath := filepath.Clean(path)
	if !isPathInside(ctx.normalizedRootPath, cleanedPath) || cleanedPath == filepath.Clean(ctx.normalizedRootPath) {
		return true
	}
	if isOperationalLogPath(cleanedPath) {
		return true
	}
	if isBatchDeleteProtectedName(ctx.paths, filepath.Base(cleanedPath)) {
		return true
	}
	for _, protectedPath := range protectedPaths {
		if isPathInside(protectedPath, cleanedPath) || isPathInside(cleanedPath, protectedPath) {
			return true
		}
	}
	return false
}

// isOperationalLogPath protects log/report text even when a compatibility
// caller places it below a non-standard folder name. The normal "logs" and
// "log" directories are already managed-directory exclusions; this broader
// check covers *.log and Chinese log filenames in user-selected subtrees.
func isOperationalLogPath(targetPath string) bool {
	cleaned := filepath.Clean(strings.TrimSpace(targetPath))
	if cleaned == "" || cleaned == "." {
		return false
	}
	// filepath.Ext("run.log.txt") is ".txt", so use suffix checks for
	// double-extension task logs as well as the ordinary .log form.
	lowerPath := strings.ToLower(filepath.ToSlash(cleaned))
	if strings.HasSuffix(lowerPath, ".log") || strings.HasSuffix(lowerPath, ".log.txt") {
		return true
	}
	base := strings.ToLower(filepath.Base(cleaned))
	if strings.Contains(base, "日志") || strings.Contains(base, "运行日志") || strings.Contains(base, "debug-log") {
		return true
	}
	for _, part := range strings.Split(filepath.ToSlash(cleaned), "/") {
		name := strings.ToLower(strings.TrimSpace(part))
		switch name {
		case "log", "logs", "日志", "运行日志", "运行日志文件":
			return true
		}
	}
	return false
}

func (ctx *organizerRunContext) batchDeleteDirectorySafe(directoryPath string, pendingSet map[string]struct{}, protectedPaths []string) bool {
	if ctx.isBatchDeleteProtectedPath(directoryPath, protectedPaths) {
		return false
	}
	info, err := os.Lstat(directoryPath)
	if err != nil || !info.IsDir() {
		return false
	}

	safe := true
	err = filepath.Walk(directoryPath, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			safe = false
			return walkErr
		}
		if path == directoryPath {
			return nil
		}
		if ctx.isBatchDeleteProtectedPath(path, protectedPaths) || isBatchDeleteProtectedName(ctx.paths, info.Name()) {
			safe = false
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			safe = false
			return nil
		}
		if _, ok := pendingSet[filepath.Clean(path)]; !ok {
			safe = false
		}
		return nil
	})
	return err == nil && safe
}

func countPendingPathsUnderTargets(targets []string, pendingSet map[string]struct{}) int {
	count := 0
	for _, targetPath := range targets {
		count += countPendingPathsUnderTarget(targetPath, pendingSet)
	}
	return count
}

func countPendingPathsUnderTarget(targetPath string, pendingSet map[string]struct{}) int {
	count := 0
	for pendingPath := range pendingSet {
		if pendingPath == targetPath || isPathInside(targetPath, pendingPath) {
			count++
		}
	}
	return count
}

func deduplicateBatchDeleteTargets(targets []string) []string {
	unique := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		cleaned := filepath.Clean(strings.TrimSpace(target))
		if cleaned != "" {
			unique[cleaned] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(unique))
	for target := range unique {
		ordered = append(ordered, target)
	}
	sort.Slice(ordered, func(i, j int) bool {
		return strings.ToLower(ordered[i]) < strings.ToLower(ordered[j])
	})
	filtered := ordered[:0]
	for _, target := range ordered {
		covered := false
		for _, parent := range filtered {
			if isPathInside(parent, target) {
				covered = true
				break
			}
		}
		if !covered {
			filtered = append(filtered, target)
		}
	}
	return filtered
}
