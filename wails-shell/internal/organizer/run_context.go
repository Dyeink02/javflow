package organizer

import (
	"fmt"
	"os"
	"strings"
	"time"

	"javflow/internal/common"
	"javflow/internal/contracts/crawlartifact"
	"javflow/internal/modulelog"
)

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
	options                RunOptions
	preloadedExpected      PreloadedExpectedCodes
	normalizedRootPath     string
	dryRun                 bool
	minSizeMB              int
	minSizeBytes           int64
	adFileAction           string
	batchDelete            bool // 是否启用批量删除模式
	deleteIntervalMs       int  // 删除间隔（毫秒）
	organizeIntervalMs     int  // 整理间隔（毫秒）
	adDetectionEnabled     bool
	adModelType            string
	adThreshold            int
	videoExtensionSet      map[string]struct{}
	videoExtensionsText    string
	suffixStrategy         conflictSuffixStrategy
	paths                  Paths
	logf                   func(string, string)
	progressf              func(ProgressEntry)
	codeSet                map[string]struct{}
	tokenSet               map[string]struct{}
	expectedCodeAliasIndex expectedCodeAliasIndex
	expectedCodeEntryMap   map[string][]MagnetEntry
	summary                Summary
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
		ctx.summary.RemovedEmptyDirs = 0
		ctx.logf("info", "批量删除模式已完成统一收口，不再执行逐目录清理。")
		ctx.emitCompletionSummary()
		return
	}

	compactRemoved := compactRootDirectories(ctx.normalizedRootPath, ctx.paths, ctx.adFileAction, ctx.dryRun, waitingMoveFailedSources, ctx.logf)
	ctx.logf("info", fmt.Sprintf("\u6839\u76ee\u5f55\u6536\u53e3\u5b8c\u6210\uff1a\u5220\u9664\u6b8b\u7559\u76ee\u5f55 %d \u4e2a\u3002", compactRemoved))

	preservedTopDirs := managedDirectoryNames(ctx.paths, ctx.adFileAction == adFileActionMoveToDelete)
	removedEmptyDirs := 0
	if !ctx.dryRun {
		removedEmptyDirs = cleanupEmptyDirectories(ctx.normalizedRootPath, preservedTopDirs, ctx.logf)
	}
	ctx.summary.RemovedEmptyDirs = removedEmptyDirs

	ctx.logf("info", fmt.Sprintf("\u7a7a\u76ee\u5f55\u6e05\u7406\u5b8c\u6210\uff1a%d \u4e2a\u3002", removedEmptyDirs))
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
