package organizer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"javflow/internal/common"
	"javflow/internal/contracts/crawlartifact"
	"javflow/internal/modulelog"
)

const organizerFinalizeUnits = 4

// run_context.go owns derived organizer execution context construction and the
// phase-to-phase handoff structs used by the Go organizer pipeline.
//
// Ownership summary:
// 1) normalize one organizer invocation into a shared runtime context
// 2) define the cross-phase result structs consumed by later pipeline steps
// 3) isolate filesystem bootstrap and run-start logging from the actual phase
//    implementations
//
// File map for maintainers:
// 1) shared run-context struct and phase handoff result structs
// 2) invocation normalization and expected-code preload wiring
// 3) filesystem bootstrap / managed-directory preparation
// 4) run-start reporting fields shared across later phases

// organizerRunContext holds one organizer execution's derived configuration and
// mutable summary. The phase files operate on this shared context so bugs can
// be isolated by phase without re-threading a large parameter list.
type organizerRunContext struct {
	options              RunOptions
	preloadedExpected    PreloadedExpectedCodes
	normalizedRootPath   string
	dryRun               bool
	minSizeMB            int
	minSizeBytes         int64
	adFileAction         string
	batchDelete          bool // 是否启用批量删除模式
	deleteIntervalMs     int  // 删除间隔（毫秒）
	organizeIntervalMs   int  // 整理间隔（毫秒）
	lastDeleteOpAt       time.Time
	lastOrganizeOpAt     time.Time
	adDetectionEnabled   bool
	adModelType          string
	adThreshold          int
	videoExtensionSet    map[string]struct{}
	videoExtensionsText  string
	suffixStrategy       conflictSuffixStrategy
	paths                Paths
	logf                 func(string, string)
	progressf            func(ProgressEntry)
	codeSet              map[string]struct{}
	tokenSet             map[string]struct{}
	expectedCodeAliasIndex expectedCodeAliasIndex
	expectedCodeEntryMap   map[string][]MagnetEntry
	summary              Summary
}

// throttleFileOp spaces out per-item file operations with the user-configured
// interval so cloud-drive mounts do not see one dense request burst. The
// batch-delete path deliberately skips throttling (checked batch = delete in
// bulk as fast as possible). Organizer phases run on one goroutine, so no
// locking is needed. Returns nothing; callers should invoke it immediately
// before the filesystem mutation, inside heartbeat closures when available so
// progress keeps flowing while waiting.
func (ctx *organizerRunContext) throttleFileOp(lastOpAt *time.Time, intervalMs int) {
	if ctx.dryRun || intervalMs <= 0 {
		return
	}
	now := time.Now()
	if !lastOpAt.IsZero() {
		if wait := time.Duration(intervalMs)*time.Millisecond - now.Sub(*lastOpAt); wait > 0 {
			time.Sleep(wait)
		}
	}
	*lastOpAt = time.Now()
}

// waitDeleteInterval throttles destructive operations (per-file or
// per-directory removal) with the configured delete interval.
func (ctx *organizerRunContext) waitDeleteInterval() {
	ctx.throttleFileOp(&ctx.lastDeleteOpAt, ctx.deleteIntervalMs)
}

// waitOrganizeInterval throttles move/rename operations (waiting-area and
// unmatched transfers, move-to-delete routing) with the configured organize
// interval.
func (ctx *organizerRunContext) waitOrganizeInterval() {
	ctx.throttleFileOp(&ctx.lastOrganizeOpAt, ctx.organizeIntervalMs)
}

// scanPhaseResult is the pure classification output that later phases consume
// without reopening the source tree.
type scanPhaseResult struct {
	candidates          []Candidate
	unmatchedCandidates []Candidate
	pendingDelete       []Candidate
	unmatchedRecords    []UnmatchedRecord
	detectedFilmCodes   map[string]struct{}
}

// waitingPhaseResult is the handoff from transfer phase into ad review and
// final cleanup/reporting.
type waitingPhaseResult struct {
	renameRecords            []RenameRecord
	waitingMoveFailedSources []string
}

// supplementPhaseResult contains only derived follow-up artifacts. It must stay
// side-effect free relative to earlier scan/transfer decisions.
type supplementPhaseResult struct {
	adRiskCodes          []string
	adRiskMagnetEntries  []CodeEntry
	missingCodes         []string
	missingMagnetEntries []CodeEntry
}

// newOrganizerRunContext normalizes one organizer invocation into a shared
// runtime context. This constructor validates inputs and derives stable
// expectations only; it does not mutate user files. Filesystem side effects
// begin later in prepareFilesystem/run phases.
func newOrganizerRunContext(service *Service, options RunOptions) (*organizerRunContext, error) {
	normalizedRootPath := crawlartifact.NormalizeRootPath(options.RootPath)
	if normalizedRootPath == "" {
		return nil, fmt.Errorf("\u8bf7\u5148\u9009\u62e9\u9700\u8981\u6574\u7406\u7684\u6839\u76ee\u5f55\u3002")
	}
	if err := common.ValidateWritableDirectoryPath(normalizedRootPath); err != nil {
		return nil, fmt.Errorf("\u6839\u76ee\u5f55\u8def\u5f84\u65e0\u6548\uff1a%w", err)
	}
	// An organizer root must be a user-owned folder, never a volume/share root.
	// Even though every delete branch is scoped and revalidated, accepting C:\\,
	// Z:\\, or a UNC share root unnecessarily widens the blast radius of a bad
	// configuration. Keep this rule at context construction so preview and real
	// runs have exactly the same safety boundary.
	if isFilesystemRoot(normalizedRootPath) {
		return nil, fmt.Errorf("\u4e3a\u9632\u6b62\u8bef\u5220\uff0c\u6574\u7406\u6839\u76ee\u5f55\u4e0d\u80fd\u76f4\u63a5\u9009\u62e9\u78c1\u76d8\u6216\u7f51\u7edc\u5171\u4eab\u6839\u76ee\u5f55\uff0c\u8bf7\u9009\u62e9\u5176\u4e2d\u7684\u4e13\u7528\u6587\u4ef6\u5939\u3002")
	}

	rootStat, err := os.Stat(normalizedRootPath)
	if err != nil || !rootStat.IsDir() {
		return nil, fmt.Errorf("\u6839\u76ee\u5f55\u4e0d\u5b58\u5728\uff1a%s", normalizedRootPath)
	}

	if options.CrawlOutputDir != "" {
		if err := common.ValidateWritableDirectoryPath(options.CrawlOutputDir); err != nil {
			return nil, fmt.Errorf("\u722c\u866b\u8f93\u51fa\u76ee\u5f55\u8def\u5f84\u65e0\u6548\uff1a%w", err)
		}
	}

	suffixStrategy, err := parseConflictSuffixStrategy(options.Suffix)
	if err != nil {
		return nil, err
	}

	adModelType := strings.TrimSpace(options.AdModelType)
	if adModelType == "" {
		adModelType = "mobile-net-v3-lite"
	}

	preloadedExpected, err := service.ResolvePreloadedExpectedCodes(options)
	if err != nil {
		return nil, err
	}

	ctx := &organizerRunContext{
		options:            options,
		preloadedExpected:  preloadedExpected,
		normalizedRootPath: normalizedRootPath,
		dryRun:             options.DryRun,
		minSizeMB:          toSafeInteger(options.MinSizeMB, 100, 1),
		adFileAction:       normalizeAdFileAction(options.AdFileAction),
		batchDelete:        options.BatchDelete,
		deleteIntervalMs:   toSafeInteger(options.DeleteIntervalMs, 500, 100),
		organizeIntervalMs: toSafeInteger(options.OrganizeIntervalMs, 500, 100),
		adDetectionEnabled: options.AdDetectionEnabled,
		adModelType:        adModelType,
		adThreshold:        toSafeInteger(options.AdThreshold, 60, 1),
		videoExtensionSet:  normalizeVideoExtensions(options.VideoExtensions),
		suffixStrategy:     suffixStrategy,
		paths:              service.ResolvePaths(normalizedRootPath),
		logf: func(level string, message string) {
			_ = modulelog.Append(service.paths.AppPath, modulelog.Organizer, level, message, time.Now())
			emitOrganizerLog(options.OnLog, level, message)
		},
		progressf: func(payload ProgressEntry) {
			emitOrganizerProgress(options.OnProgress, payload)
		},
	}
	ctx.minSizeBytes = int64(ctx.minSizeMB) * 1024 * 1024
	ctx.videoExtensionsText = formatVideoExtensions(ctx.videoExtensionSet)

	ctx.codeSet, ctx.tokenSet = buildExpectedCodeSets(preloadedExpected.Codes)
	ctx.expectedCodeAliasIndex = buildExpectedCodeAliasIndex(preloadedExpected.CodeEntries)
	ctx.expectedCodeEntryMap = buildExpectedCodeEntryMap(preloadedExpected.CodeEntries)
	for code := range ctx.expectedCodeEntryMap {
		ctx.codeSet[code] = struct{}{}
		if token := normalizeCodeToken(code); token != "" {
			ctx.tokenSet[token] = struct{}{}
		}
	}
	ctx.summary.ExpectedCodeTotal = len(ctx.codeSet)

	return ctx, nil
}

// isFilesystemRoot is intentionally based on filepath.Dir instead of a
// drive-letter pattern so it protects Windows drive roots, UNC share roots,
// and POSIX roots without relying on the UI platform.
func isFilesystemRoot(path string) bool {
	cleaned := filepath.Clean(strings.TrimSpace(path))
	return cleaned != "" && filepath.Dir(cleaned) == cleaned
}

// prepareFilesystem owns only the organizer-managed directories and legacy
// report cleanup. It must stay separate from the scan/transfer phases so path
// bootstrap problems can be diagnosed without mixing them into file matching.
func (ctx *organizerRunContext) prepareFilesystem() error {
	if ctx.dryRun {
		return nil
	}

	ensureTasks := []string{ctx.paths.WaitingDir, ctx.paths.UnmatchedDir, ctx.paths.IntroAdDir, ctx.paths.LogsDir, ctx.paths.StateDir}
	if ctx.adFileAction == adFileActionMoveToDelete {
		ensureTasks = append(ensureTasks, ctx.paths.ToDeleteDir)
	}
	for _, targetDir := range ensureTasks {
		if err := ensureDirectory(targetDir); err != nil {
			return err
		}
	}
	if !ctx.batchDelete || ctx.adFileAction != adFileActionDeleteDirectly {
		cleanupLegacyReportFiles(ctx.normalizedRootPath, ctx.logf)
	}
	return nil
}

// emitRunStart writes the execution banner once after context normalization.
// This is the best place to confirm which artifact source, thresholds, and
// optional ad-review settings the current run actually resolved to.
func (ctx *organizerRunContext) emitRunStart() {
	ctx.progressf(ProgressEntry{
		"phase":        progressPhaseStarting,
		"dryRun":       ctx.dryRun,
		"rootPath":     ctx.normalizedRootPath,
		"minSizeMB":    ctx.minSizeMB,
		"adFileAction": ctx.adFileAction,
		"adModelType":  ctx.adModelType,
	})
	ctx.logf(
		"info",
		fmt.Sprintf(
			"\u5f00\u59cb\u6267\u884c\u6574\u7406\uff08\u6839\u76ee\u5f55=%s\uff0c\u6700\u5c0f\u4f53\u79ef=%dMB\uff0c\u6a21\u5f0f=%s\uff0c\u5e7f\u544a\u5904\u7406=%s\uff09",
			ctx.normalizedRootPath,
			ctx.minSizeMB,
			map[bool]string{true: "\u9884\u89c8", false: "\u6267\u884c"}[ctx.dryRun],
			getAdFileActionLabel(ctx.adFileAction),
		),
	)
	ctx.logf("info", "\u89c6\u9891\u540e\u7f00\u5224\u5b9a\uff1a"+ctx.videoExtensionsText)
	if len(ctx.codeSet) > 0 {
		ctx.logf("info", fmt.Sprintf("\u5df2\u52a0\u8f7d\u722c\u866b\u756a\u53f7\u540d\u5355\uff1a%d \u6761\uff0c\u4e25\u683c\u5339\u914d=%s", len(ctx.codeSet), map[bool]string{true: "\u662f", false: "\u5426"}[ctx.options.StrictExpectedCodes]))
		ctx.logf("info", fmt.Sprintf("\u756a\u53f7\u540d\u5355\u6765\u6e90\uff1a%s", ctx.describeExpectedCodeSource()))
		aliasCount := len(ctx.expectedCodeAliasIndex.canonicalByKey)
		ambiguousAliasCount := len(ctx.expectedCodeAliasIndex.ambiguousKeys)
		if aliasCount > 0 || ambiguousAliasCount > 0 {
			ctx.logf("info", fmt.Sprintf("\u5df2\u52a0\u8f7d\u78c1\u529b\u756a\u53f7\u522b\u540d\uff1a%d \u6761\uff0c\u6b67\u4e49\u522b\u540d\uff1a%d \u6761", aliasCount, ambiguousAliasCount))
		}
	} else {
		ctx.logf("warn", "\u672a\u52a0\u8f7d\u722c\u866b\u756a\u53f7\u540d\u5355\uff0c\u5c06\u56de\u9000\u4e3a\u4ec5\u6309\u6587\u4ef6\u540d\u63d0\u53d6\u756a\u53f7\u3002")
	}
	if ctx.adDetectionEnabled {
		if ctx.options.EvaluateAdRisk != nil && !ctx.dryRun {
			ctx.logf("info", fmt.Sprintf("\u5df2\u542f\u7528\u5f00\u5934\u5e7f\u544a\u98ce\u9669\u68c0\u6d4b\uff0c\u9608\u503c=%d", ctx.adThreshold))
		} else {
			ctx.logf("warn", "\u5f00\u5934\u5e7f\u544a\u68c0\u6d4b\u5c06\u5728\u6267\u884c\u6a21\u5f0f\u4e0b\u751f\u6548\uff0c\u9884\u89c8\u6216\u65e0\u8bc4\u4f30\u670d\u52a1\u65f6\u4f1a\u8df3\u8fc7\u3002")
		}
	}
}

// emitFinalizeProgress makes the post-processing handoff visible. Report
// writing, batch deletion, root compaction, and empty-directory cleanup are
// separate operations; exposing them as four units prevents a long network
// filesystem call from looking like a stalled 83% run.
func (ctx *organizerRunContext) emitFinalizeProgress(processed int, operation string, currentPath string, extra ProgressEntry) {
	if processed < 0 {
		processed = 0
	}
	if processed > organizerFinalizeUnits {
		processed = organizerFinalizeUnits
	}
	payload := ProgressEntry{
		"phase":             progressPhaseFinalizeProgress,
		"finalizeTotal":     organizerFinalizeUnits,
		"finalizeProcessed": processed,
		"total":             organizerFinalizeUnits,
		"processed":         processed,
		"operation":         operation,
		"currentPath":       currentPath,
		"failedOperations":  ctx.summary.FailedOperations,
	}
	for key, value := range extra {
		payload[key] = value
	}
	ctx.progressf(payload)
}

// describeExpectedCodeSource reduces several possible crawl-artifact inputs
// into one operator-facing label so debugging does not require reopening every
// possible source file path.
func (ctx *organizerRunContext) describeExpectedCodeSource() string {
	sourceType := resolveExpectedSourceType(
		ctx.preloadedExpected.SourceType,
		ctx.preloadedExpected.SourcePath,
		ctx.preloadedExpected.FilmDataPath,
		ctx.preloadedExpected.OrganizerCodesPath,
		len(ctx.codeSet) > 0 || len(ctx.expectedCodeEntryMap) > 0,
		"",
	)
	sourcePath := resolveExpectedSourcePath(
		sourceType,
		ctx.preloadedExpected.SourcePath,
		ctx.preloadedExpected.FilmDataPath,
		ctx.preloadedExpected.OrganizerCodesPath,
	)
	if sourcePath == "" {
		sourcePath = strings.TrimSpace(ctx.preloadedExpected.OutputDir)
	}
	if sourceType == "" {
		sourceType = codeSourcePayload
	}
	if sourcePath == "" {
		return sourceType
	}
	return sourceType + " -> " + sourcePath
}

// writeRunReports is the handoff from in-memory organizer results to operator-
// facing artifacts. Keep report generation isolated here so output bugs do not
// get mixed into scan/transfer/ad-review flow debugging.
func (ctx *organizerRunContext) writeRunReports(
	renameRecords []RenameRecord,
	unmatchedRecords []UnmatchedRecord,
	adRiskRecords []AdRiskRecord,
	supplement supplementPhaseResult,
) (map[string]string, []string, error) {
	reportMap := map[string]string{}
	reportFiles := []string{}
	if ctx.dryRun {
		return reportMap, reportFiles, nil
	}

	loadedReportMap, err := writeReports(
		ctx.paths,
		ctx.summary,
		renameRecords,
		unmatchedRecords,
		adRiskRecords,
		supplement.adRiskMagnetEntries,
		supplement.missingMagnetEntries,
	)
	if err != nil {
		return nil, nil, err
	}
	reportMap = loadedReportMap
	reportFiles = []string{
		ctx.paths.RenameMapPath,
		ctx.paths.UnmatchedPath,
		ctx.paths.AdRiskCodesPath,
		ctx.paths.AdRiskDetailPath,
		ctx.paths.AdRiskMagnetsPath,
		ctx.paths.MissingMagnetsPath,
	}
	return reportMap, reportFiles, nil
}

// cleanupManagedDirectories is the final filesystem pass after all move/delete
// decisions are settled. Keep it late so any residual-source investigation can
// still inspect the tree before the organizer compacts it.
func (ctx *organizerRunContext) cleanupManagedDirectories(waitingMoveFailedSources []string) {
	if ctx.batchDelete && ctx.adFileAction == adFileActionDeleteDirectly {
		preservedTopDirs := managedDirectoryNames(ctx.paths, false)
		removedEmptyDirs := 0
		if !ctx.dryRun {
			removedEmptyDirs = cleanupEmptyDirectories(
				ctx.normalizedRootPath,
				preservedTopDirs,
				waitingMoveFailedSources,
				ctx.logf,
				ctx.progressf,
			)
		}
		ctx.summary.RemovedEmptyDirs = removedEmptyDirs
		ctx.logf("info", fmt.Sprintf("批量删除收口完成：已清理空目录 %d 个，必要目录和未确认内容均已保留。", removedEmptyDirs))
		ctx.emitFinalizeProgress(3, "批量删除后的空目录已清理", ctx.normalizedRootPath, ProgressEntry{
			"removedEmptyDirs": removedEmptyDirs,
		})
		ctx.emitFinalizeProgress(4, "整理收尾完成", ctx.normalizedRootPath, ProgressEntry{
			"removedEmptyDirs": removedEmptyDirs,
		})
		ctx.emitCompletionSummary()
		return
	}

	ctx.emitFinalizeProgress(2, "清理根目录残留", ctx.normalizedRootPath, nil)
	compactRemoved := compactRootDirectories(ctx.normalizedRootPath, ctx.paths, ctx.adFileAction, ctx.dryRun, waitingMoveFailedSources, ctx.logf, ctx.progressf)
	ctx.logf("info", fmt.Sprintf("\u6839\u76ee\u5f55\u6536\u53e3\u5b8c\u6210\uff1a\u5220\u9664\u6b8b\u7559\u76ee\u5f55 %d \u4e2a\u3002", compactRemoved))

	preservedTopDirs := managedDirectoryNames(ctx.paths, ctx.adFileAction == adFileActionMoveToDelete)
	removedEmptyDirs := 0
	if !ctx.dryRun {
		removedEmptyDirs = cleanupEmptyDirectories(ctx.normalizedRootPath, preservedTopDirs, waitingMoveFailedSources, ctx.logf, ctx.progressf)
	}
	ctx.summary.RemovedEmptyDirs = removedEmptyDirs

	ctx.logf("info", fmt.Sprintf("\u7a7a\u76ee\u5f55\u6e05\u7406\u5b8c\u6210\uff1a%d \u4e2a\u3002", removedEmptyDirs))
	ctx.emitFinalizeProgress(4, "整理收尾完成", ctx.normalizedRootPath, ProgressEntry{
		"removedDirectories": compactRemoved,
		"removedEmptyDirs":   removedEmptyDirs,
	})
	ctx.emitCompletionSummary()
}

func (ctx *organizerRunContext) emitCompletionSummary() {
	ctx.logf(
		"info",
		fmt.Sprintf(
			"整理完成：待整理=%d，未命中=%d，待删除=%d，含开头广告=%d，直接删除=%d，开头广告命中=%d，失败=%d",
			ctx.summary.MovedToWaiting,
			ctx.summary.MovedToUnmatched,
			ctx.summary.MovedToDelete,
			ctx.summary.MovedToIntroAd,
			ctx.summary.DeletedDirectly,
			ctx.summary.AdRiskRejected,
			ctx.summary.FailedOperations,
		),
	)
	ctx.progressf(ProgressEntry{
		"phase":            progressPhaseCompleted,
		"waitingTotal":     ctx.summary.MovedToWaiting,
		"waitingProcessed": ctx.summary.MovedToWaiting,
		"unmatchedTotal":   ctx.summary.MovedToUnmatched,
		"deleteTotal":      ctx.summary.MovedToDelete,
		"deleteProcessed":  ctx.summary.MovedToDelete,
		"introAdTotal":     ctx.summary.MovedToIntroAd,
		"adFileAction":     ctx.adFileAction,
		"deletedDirectly":  ctx.summary.DeletedDirectly,
		"failedOperations": ctx.summary.FailedOperations,
	})
}

// buildRunResult is the API-facing projection of the organizer run. Keep all
// preview truncation and summary shaping here so phase files can stay focused on
// filesystem decisions instead of response formatting.
func (ctx *organizerRunContext) buildRunResult(
	renameRecords []RenameRecord,
	unmatchedRecords []UnmatchedRecord,
	adRiskRecords []AdRiskRecord,
	supplement supplementPhaseResult,
	reportMap map[string]string,
	reportFiles []string,
) RunResult {
	return RunResult{
		RootPath:          ctx.normalizedRootPath,
		DryRun:            ctx.dryRun,
		Config:            ctx.buildRunConfig(),
		ExpectedCodeCount: len(ctx.codeSet),
		Summary:           ctx.summary,
		Paths:             ctx.buildResultPaths(),
		ReportMap:         reportMap,
		ReportFiles:       reportFiles,
		Preview: PreviewResult{
			RenameRecords:    firstN(renameRecords, 200),
			UnmatchedRecords: firstN(unmatchedRecords, 200),
			AdRiskRecords:    firstN(adRiskRecords, 200),
		},
		AdRisk: AdRiskSummary{
			RiskCodeCount:         len(supplement.adRiskCodes),
			SupplementMagnetCount: ctx.summary.SupplementMagnetCount,
			RiskCodes:             firstN(supplement.adRiskCodes, 500),
		},
		MissingDownload: MissingDownloadInfo{
			MissingCodeCount:   ctx.summary.MissingCodeCount,
			MissingMagnetCount: ctx.summary.MissingMagnetCount,
			MissingCodes:       firstN(supplement.missingCodes, 500),
		},
	}
}

// buildRunConfig projects normalized runtime options into the API-facing result
// contract so callers do not need to understand internal defaults/aliases.
func (ctx *organizerRunContext) buildRunConfig() RunConfig {
	return RunConfig{
		IncludeSubdirectories: ctx.options.IncludeSubdirectories,
		MinSizeMB:             ctx.minSizeMB,
		SuffixInput:           ctx.suffixStrategy.Raw,
		AdFileAction:          ctx.adFileAction,
		StrictExpectedCodes:   ctx.options.StrictExpectedCodes,
		AdDetectionEnabled:    ctx.adDetectionEnabled,
		AdModelType:           ctx.adModelType,
		AdThreshold:           ctx.adThreshold,
		VideoExtensions:       ctx.videoExtensionsText,
		AlistBaseURL:          ctx.options.AlistBaseURL,
	}
}

// buildResultPaths centralizes the operator-visible folder map returned to the
// bridge/UI layer.
func (ctx *organizerRunContext) buildResultPaths() map[string]string {
	return map[string]string{
		"waitingDir":   ctx.paths.WaitingDir,
		"unmatchedDir": ctx.paths.UnmatchedDir,
		"toDeleteDir":  ctx.paths.ToDeleteDir,
		"introAdDir":   ctx.paths.IntroAdDir,
		"logsDir":      ctx.paths.LogsDir,
		"reportsDir":   ctx.paths.RootPath,
	}
}
