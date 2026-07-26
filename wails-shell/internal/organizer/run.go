// Package organizer is the current Go-native video organizer domain.
//
// run.go owns the organizer's top-level orchestration across scan, transfer,
// review, and reporting phases.
//
// Ownership summary:
// 1) orchestrate the organizer pipeline across scan, transfer, review, and report
// 2) define cross-phase handoff and shared logging/progress envelopes
// 3) keep phase-specific rules in sibling files instead of growing this root
package organizer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Phase-specific rules live in sibling files, while this file keeps the
// overall execution envelope readable. If a maintenance issue spans phases,
// inspect this file first.
//
// Ownership split inside this file:
// - pipeline order and cross-phase handoff live here
// - per-phase scan/judge/rename/report rules stay in dedicated files
// - helpers here should exist only when they describe cross-phase behavior
//
// File map for maintainers:
// 1) cross-phase log/progress envelope helpers
// 2) shared rename-plan/path helpers
// 3) top-level organizer pipeline orchestration
// 4) final result/report shaping that spans multiple phases
//
// emitOrganizerLog is the narrow timestamped log sink used by all organizer
// phases. Keeping one formatter here avoids each phase inventing slightly
// different log envelopes.
func emitOrganizerLog(onLog LogSink, level string, message string) {
	if onLog == nil {
		return
	}
	if strings.TrimSpace(level) == "" {
		level = "info"
	}
	onLog(LogEntry{
		Level:     level,
		Message:   message,
		Timestamp: time.Now().Format(time.RFC3339),
	})
}

// emitOrganizerProgress keeps organizer progress payloads structurally stable
// before they cross into the bridge/UI layer.
func emitOrganizerProgress(onProgress ProgressSink, payload ProgressEntry) {
	if onProgress == nil {
		return
	}
	if payload == nil {
		payload = ProgressEntry{}
	}
	if _, ok := payload["scope"]; !ok {
		payload["scope"] = "organizer"
	}
	if _, ok := payload["timestamp"]; !ok {
		payload["timestamp"] = time.Now().Format(time.RFC3339)
	}
	onProgress(payload)
}

// shouldReportProgress throttles repetitive scan/move/delete progress chatter
// so logs stay readable on very large trees.
func shouldReportProgress(processed int, total int, step int) bool {
	if total <= 0 {
		return true
	}
	if processed <= 1 || processed >= total {
		return true
	}
	if step < 1 {
		step = 1
	}
	return processed%step == 0
}

// planTargetNames is the organizer's single rename-plan pass. Multi-part title
// handling and conflict suffix rules should converge here before files move.
func planTargetNames(candidates []Candidate, strategy conflictSuffixStrategy) []string {
	outputNames := make([]string, len(candidates))
	grouped := map[string][]int{}
	for index, item := range candidates {
		if !item.RenameByFilmCode || item.FilmCode == "" {
			originalName := filepath.Base(strings.TrimSpace(item.Src))
			if originalName == "" {
				originalName = "UNNAMED_" + intToString(index+1)
			}
			outputNames[index] = originalName
			continue
		}
		grouped[item.FilmCode] = append(grouped[item.FilmCode], index)
	}

	codes := make([]string, 0, len(grouped))
	for code := range grouped {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, filmCode := range codes {
		indexes := grouped[filmCode]
		sort.Slice(indexes, func(i int, j int) bool {
			return strings.ToLower(candidates[indexes[i]].Src) < strings.ToLower(candidates[indexes[j]].Src)
		})
		useSuffix := len(indexes) > 1
		for sequence, candidateIndex := range indexes {
			extension := strings.ToLower(filepath.Ext(candidates[candidateIndex].Src))
			suffix := ""
			if useSuffix {
				suffix = formatSuffix(strategy, sequence)
			}
			outputNames[candidateIndex] = filmCode + suffix + extension
		}
	}
	return outputNames
}

func safeRelativePath(rootPath string, sourcePath string) (string, bool) {
	relativePath, err := filepath.Rel(rootPath, sourcePath)
	if err != nil || relativePath == "" || strings.HasPrefix(relativePath, "..") || filepath.IsAbs(relativePath) {
		return "", false
	}
	return relativePath, true
}

func isPathInside(parentPath string, targetPath string) bool {
	relativePath, err := filepath.Rel(parentPath, targetPath)
	if err != nil {
		return false
	}
	if relativePath == "" {
		return true
	}
	return !strings.HasPrefix(relativePath, "..") && !filepath.IsAbs(relativePath)
}

func isManagedRootChild(paths Paths, directoryPath string) bool {
	rootPath := filepath.Clean(paths.RootPath)
	targetPath := filepath.Clean(directoryPath)
	if !isPathInside(rootPath, targetPath) {
		return true
	}
	relativePath, ok := safeRelativePath(rootPath, targetPath)
	if !ok {
		return true
	}
	topDirName := strings.Split(relativePath, string(os.PathSeparator))[0]
	managed := managedDirectoryNames(paths, true)
	_, exists := managed[topDirName]
	return exists
}

func resolveDeleteDestinationPath(paths Paths, sourcePath string) string {
	if relativePath, ok := safeRelativePath(paths.RootPath, sourcePath); ok {
		return filepath.Join(paths.ToDeleteDir, relativePath)
	}
	return filepath.Join(paths.ToDeleteDir, filepath.Base(sourcePath))
}

func resolveIntroAdDestinationPath(paths Paths, sourcePath string) string {
	if relativePath, ok := safeRelativePath(paths.WaitingDir, sourcePath); ok {
		return filepath.Join(paths.IntroAdDir, relativePath)
	}
	return filepath.Join(paths.IntroAdDir, filepath.Base(sourcePath))
}

// compactRootDirectories performs the last cleanup pass after waiting/delete
// routing finishes. It should only touch organizer-managed leftovers.
func compactRootDirectories(rootPath string, paths Paths, adFileAction string, dryRun bool, protectedSourcePaths []string, logf func(string, string)) int {
	if rootPath == "" || !filepath.IsAbs(rootPath) {
		return 0
	}
	keepTopDirs := managedDirectoryNames(paths, adFileAction == adFileActionMoveToDelete)
	normalizedProtected := make([]string, 0, len(protectedSourcePaths))
	for _, item := range protectedSourcePaths {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			normalizedProtected = append(normalizedProtected, filepath.Clean(trimmed))
		}
	}

	removedDirs := 0
	entries, err := os.ReadDir(rootPath)
	if err != nil {
		return 0
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, keep := keepTopDirs[entry.Name()]; keep {
			continue
		}

		sourceDir := filepath.Join(rootPath, entry.Name())
		protected := false
		for _, protectedPath := range normalizedProtected {
			if protectedPath == sourceDir || strings.HasPrefix(protectedPath, sourceDir+string(os.PathSeparator)) {
				protected = true
				break
			}
		}
		if protected {
			if logf != nil {
				logf("warn", "\u6839\u76ee\u5f55\u6b8b\u7559\u76ee\u5f55\u5df2\u4fdd\u7559\uff1a"+sourceDir+"\uff08\u5b58\u5728\u79fb\u52a8\u5931\u8d25\u7684\u89c6\u9891\uff0c\u8bf7\u4eba\u5de5\u590d\u6838\uff09")
			}
			continue
		}
		if dryRun {
			if logf != nil {
				logf("info", "[\u9884\u89c8] \u6839\u76ee\u5f55\u6b8b\u7559\u76ee\u5f55\u5f85\u5904\u7406\uff1a"+sourceDir)
			}
			continue
		}
		if err := removeDirectoryWithRetry(sourceDir, 5); err == nil {
			removedDirs++
			if logf != nil {
				logf("info", "\u6839\u76ee\u5f55\u6b8b\u7559\u76ee\u5f55\u5df2\u5220\u9664\uff1a"+sourceDir)
			}
		} else if logf != nil {
			logf("warn", fmt.Sprintf("根目录残留目录删除失败：%s，原因：%s", sourceDir, err.Error()))
		}
	}
	return removedDirs
}

func firstN[T any](values []T, limit int) []T {
	if limit < 0 {
		limit = 0
	}
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

// RunOrganizer executes the organizer's stable pipeline:
// 1) scan root files into candidate/ad/unmatched buckets
// 2) move and rename accepted videos into waiting
// 3) optionally review intro-ad risk on waiting files
// 4) write reports and clean up managed directories
//
// When troubleshooting, map the symptom to the matching phase file first
// instead of tracing the whole organizer stack end to end.
func (s *Service) RunOrganizer(options RunOptions) (RunResult, error) {
	// Top-level organizer flow should stay readable enough that a maintainer can
	// classify failures by phase before drilling into lower-level rule files.
	runCtx, err := newOrganizerRunContext(s, options)
	if err != nil {
		return RunResult{}, err
	}
	if err := runCtx.prepareFilesystem(); err != nil {
		return RunResult{}, err
	}
	runCtx.emitRunStart()

	scanResult := runCtx.scanFiles()
	waitingResult := runCtx.moveCandidatesToWaiting(scanResult.candidates)
	if !runCtx.dryRun {
		if historyErr := appendRenameHistory(runCtx.paths.RenameHistoryPath, waitingResult.renameRecords); historyErr != nil {
			runCtx.summary.FailedOperations++
			runCtx.logf("warn", "保存隐藏改名历史失败："+historyErr.Error())
		}
	}
	unmatchedMoveFailures := runCtx.moveUnmatchedCandidates(scanResult.unmatchedCandidates)
	protectedSources := append(append([]string{}, waitingResult.waitingMoveFailedSources...), unmatchedMoveFailures...)
	runCtx.processPendingDelete(scanResult.pendingDelete, protectedSources)
	adRiskRecords := runCtx.reviewIntroAdRisk(waitingResult.renameRecords)
	supplementResult := runCtx.buildSupplementData(adRiskRecords, scanResult.detectedFilmCodes)

	reportMap, reportFiles, err := runCtx.writeRunReports(
		waitingResult.renameRecords,
		scanResult.unmatchedRecords,
		adRiskRecords,
		supplementResult,
	)
	if err != nil {
		return RunResult{}, err
	}

	runCtx.executeBatchDelete(scanResult.pendingDelete, protectedSources)
	runCtx.cleanupManagedDirectories(protectedSources)

	return runCtx.buildRunResult(
		waitingResult.renameRecords,
		scanResult.unmatchedRecords,
		adRiskRecords,
		supplementResult,
		reportMap,
		reportFiles,
	), nil
}

func mapPathToStreamURL(videoPath string, rootPath string, alistBaseURL string) string {
	baseURL := strings.TrimSpace(alistBaseURL)
	if baseURL == "" {
		return ""
	}
	rootPath = strings.TrimSpace(rootPath)
	if rootPath == "" {
		return ""
	}
	baseURL = strings.TrimRight(baseURL, "/")
	videoPath = filepath.ToSlash(videoPath)
	rootPath = filepath.ToSlash(rootPath)
	if !strings.HasPrefix(strings.ToLower(videoPath), strings.ToLower(rootPath)) {
		return ""
	}
	relativePath := strings.TrimPrefix(videoPath, rootPath)
	relativePath = strings.TrimLeft(relativePath, "/")
	return baseURL + "/" + relativePath
}

func containsCode(codeSet map[string]struct{}, code string) bool {
	_, ok := codeSet[normalizeFilmID(code)]
	return ok
}

func buildAdRiskReason(adRiskResult AdRiskResult) string {
	reasonParts := []string{}
	if adRiskResult.Score > 0 || adRiskResult.Threshold > 0 {
		reasonParts = append(reasonParts, fmt.Sprintf("\u5f00\u5934\u5e7f\u544a\u98ce\u9669\u8bc4\u5206 %.0f/%.0f", adRiskResult.Score, adRiskResult.Threshold))
	}
	if len(adRiskResult.Reasons) > 0 {
		reasonParts = append(reasonParts, strings.Join(adRiskResult.Reasons, "\uff1b"))
	}
	if len(reasonParts) == 0 {
		return "\u547d\u4e2d\u5f00\u5934\u5e7f\u544a\u98ce\u9669"
	}
	return strings.Join(reasonParts, "\uff1b")
}
