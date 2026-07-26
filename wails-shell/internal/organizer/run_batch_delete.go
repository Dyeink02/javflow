// Ownership summary:
//   This file implements organizer batch deletion of ad/promo files after a run.
//
// File map for maintainers:
//   1) Batch delete target collection.
//   2) Staging directory creation and cleanup.
//   3) Safe recursive delete execution.
//
package organizer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// executeBatchDelete consolidates every non-preserved root entry into one
// temporary directory and issues one recursive delete only after all accepted
// videos and reports are safely written.
func (ctx *organizerRunContext) executeBatchDelete(pendingDelete []Candidate, waitingMoveFailedSources []string) {
	if !ctx.batchDelete || ctx.adFileAction != adFileActionDeleteDirectly {
		return
	}

	// Remove any stale staging directories left behind by previous failed runs
	// before creating a new one. This prevents .video-organizer-batch-delete-*
	// folders from accumulating on disk.
	cleanupStaleBatchDeleteStaging(ctx.normalizedRootPath)

	targets := ctx.collectBatchDeleteTargets(waitingMoveFailedSources)
	if ctx.dryRun {
		ctx.summary.DeletedDirectly = len(pendingDelete)
		ctx.logf("info", fmt.Sprintf("[预览] 批量删除将在整理完成后统一清理 %d 个根目录条目（待清理文件 %d 个）。", len(targets), len(pendingDelete)))
		return
	}
	if len(targets) == 0 {
		ctx.logf("info", "批量删除无需执行：根目录中没有非保留内容。")
		return
	}

	stagingDir, err := os.MkdirTemp(ctx.normalizedRootPath, batchDeleteStagingPrefix)
	if err != nil {
		ctx.summary.FailedOperations++
		ctx.logf("warn", fmt.Sprintf("创建批量删除临时目录失败：%s", err.Error()))
		return
	}

	stagedCount := 0
	deletedPendingCount := 0
	totalTargets := len(targets)
	for i, sourcePath := range targets {
		if filepath.Clean(sourcePath) == filepath.Clean(stagingDir) {
			continue
		}
		targetPath := filepath.Join(stagingDir, filepath.Base(sourcePath))
		if err := os.Rename(sourcePath, targetPath); err != nil {
			ctx.summary.FailedOperations++
			ctx.logf("warn", fmt.Sprintf("批量清理归集失败，已保留原内容：%s，原因：%s", sourcePath, err.Error()))
			continue
		}
		stagedCount++
		for _, item := range pendingDelete {
			candidatePath := filepath.Clean(strings.TrimSpace(item.Src))
			if candidatePath == sourcePath || isPathInside(sourcePath, candidatePath) {
				deletedPendingCount++
			}
		}
		// Report progress periodically so the UI does not look frozen during
		// long batch-delete operations on network drives.
		if totalTargets > 0 && (i+1)%5 == 0 {
			ctx.progressf(ProgressEntry{
				"phase":           progressPhaseDeleteProgress,
				"total":           totalTargets,
				"processed":       i + 1,
				"adFileAction":    ctx.adFileAction,
				"deletedDirectly": ctx.summary.DeletedDirectly,
			})
		}
	}

	if stagedCount == 0 {
		_ = os.Remove(stagingDir)
		ctx.logf("warn", "批量删除未执行：没有内容成功归集到临时目录。")
		return
	}

	ctx.logf("info", fmt.Sprintf("批量清理归集完成：%d 个根目录条目，开始执行一次性目录删除。", stagedCount))
	ctx.progressf(ProgressEntry{
		"phase":           progressPhaseDeleteProgress,
		"total":           totalTargets,
		"processed":       stagedCount,
		"adFileAction":    ctx.adFileAction,
		"deletedDirectly": ctx.summary.DeletedDirectly,
	})
	if err := os.RemoveAll(stagingDir); err != nil || pathExists(stagingDir) {
		ctx.summary.FailedOperations++
		if err != nil {
			ctx.logf("warn", fmt.Sprintf("一次性批量删除失败，临时目录已保留以便重试：%s，原因：%s", stagingDir, err.Error()))
		} else {
			ctx.logf("warn", "一次性批量删除后临时目录仍存在，已保留以便下次重试："+stagingDir)
		}
		return
	}

	ctx.summary.DeletedDirectly += deletedPendingCount
	ctx.logf("info", fmt.Sprintf("一次性批量删除完成：已清理 %d 个根目录条目（待清理文件 %d/%d 个）。", stagedCount, deletedPendingCount, len(pendingDelete)))
	ctx.progressf(ProgressEntry{
		"phase":           progressPhaseDeleteProgress,
		"total":           len(pendingDelete),
		"processed":       deletedPendingCount,
		"adFileAction":    ctx.adFileAction,
		"deletedDirectly": ctx.summary.DeletedDirectly,
	})
}

func (ctx *organizerRunContext) collectBatchDeleteTargets(waitingMoveFailedSources []string) []string {
	entries, err := os.ReadDir(ctx.normalizedRootPath)
	if err != nil {
		return nil
	}

	whitelist := map[string]struct{}{}
	for name := range ctx.paths.BatchDeleteWhitelist() {
		whitelist[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	protectedPaths := make([]string, 0, len(waitingMoveFailedSources))
	for _, sourcePath := range waitingMoveFailedSources {
		if trimmed := strings.TrimSpace(sourcePath); trimmed != "" {
			protectedPaths = append(protectedPaths, filepath.Clean(trimmed))
		}
	}

	targets := make([]string, 0, len(entries))
	for _, entry := range entries {
		if _, keep := whitelist[strings.ToLower(entry.Name())]; keep {
			continue
		}
		entryPath := filepath.Join(ctx.normalizedRootPath, entry.Name())
		protected := false
		for _, sourcePath := range protectedPaths {
			if sourcePath == entryPath || isPathInside(entryPath, sourcePath) {
				protected = true
				break
			}
		}
		if protected {
			ctx.logf("warn", "批量删除已保留移动失败视频所在目录："+entryPath)
			continue
		}
		targets = append(targets, entryPath)
	}
	sort.Slice(targets, func(i, j int) bool {
		return strings.ToLower(targets[i]) < strings.ToLower(targets[j])
	})
	return targets
}

// cleanupStaleBatchDeleteStaging removes leftover .video-organizer-batch-delete-*
// staging directories that may remain after a previous run crashed or failed to
// delete the staged contents. These directories are purely temporary and safe
// to remove once they are no longer actively being processed.
func cleanupStaleBatchDeleteStaging(rootPath string) {
	entries, err := os.ReadDir(rootPath)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), batchDeleteStagingPrefix) {
			_ = os.RemoveAll(filepath.Join(rootPath, entry.Name()))
		}
	}
}
