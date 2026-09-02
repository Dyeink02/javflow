// Package crawlrunner owns the Go-native crawl state machine and execution
// loop.
//
// Maintenance boundary:
// - own the crawl phase chain and phase-to-phase handoff
// - own resume/recovery/reconciliation behavior
// - own final output/status decision logic
// - do not absorb bridge/controller UI state policy
//
// Ownership summary:
// 1) expose the Go-native crawler runtime facade and lifecycle
// 2) keep crawl phase execution, recovery, and reconciliation in one runtime layer
// 3) hand final status/output to diagnostics and read-model consumers without owning UI policy
//
// File map for maintainers:
// 1) runner facade/dependency fields and lifecycle callbacks
// 2) top-level run/start/stop execution entrypoints
// 3) phase-chain helpers across index/detail/magnet/recovery/reconcile work
// 4) final status/output handoff into diagnostics and quality consumers
package crawlrunner

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"javflow/internal/common"
	"javflow/internal/contracts/crawlartifact"
	"javflow/internal/crawlexecution"
	"javflow/internal/crawloutput"
	"javflow/internal/crawlparse"
	"javflow/internal/crawlqueue"
	"javflow/internal/crawlrequest"
	"javflow/internal/crawltaskstate"
)

// Runner is the Go-native crawl state machine.
//
// It keeps scheduling, resume, gap recovery, validation, and output
// finalization in one execution chain so operators can debug "where did the
// crawl stop losing completeness" from one primary file.
//
// Practical split:
//   - task/controller questions start in `internal/crawltask`
//   - state-machine questions start here
//   - post-run quality review questions start in `internal/crawlquality`
//   - use this file when you need to follow resume/recovery/reconciliation logic
//     without jumping into controller snapshot or quality-summary code
type Runner struct {
	config    Config
	tracker   *Tracker
	writer    *crawloutput.Writer
	outputDir string

	// runCtx carries the caller's lifecycle and is the base for all internal
	// request timeouts. It is set once per Run and must not be reused across runs.
	runCtx context.Context

	pageIndex                int
	filmCount                int
	filmsQueued              int
	filmsAttempted           int
	expectedItemsPerPage     *int
	pageAudits               []PageAudit
	startedAt                string
	filteredActressItemIDs   map[string]struct{}
	validationReport         *crawltaskstate.ResultValidationReport
	failedDetailMap          map[string]FailedDetail
	detailRecoveryAttemptMap map[string]int

	queueRunner       *crawlqueue.Runner
	fetchService      any
	fetchIndexPage    func(ctx context.Context, baseURL string, search string, pageNumber int) ([]string, crawlrequest.PageResponse, error)
	fetchDetail       func(ctx context.Context, detailURL string) (crawlparse.Metadata, crawlrequest.PageResponse, error)
	fetchMagnetFn     func(ctx context.Context, gid string, uc string, img string, title string) (*crawlrequest.MagnetResult, error)
	createFetchClient func(proxy string, timeout time.Duration) (*crawlrequest.Client, error)

	executionPlan ExecutionPlan
	currentPhase  PhaseKey
	phaseStack    []PhaseKey

	isRunning  bool
	isStopping bool
	mu         sync.Mutex

	resumeExisting bool
	shuttingDown   chan struct{}

	eventHandlers map[PhaseEventType][]EventHandler
	eventMu       sync.RWMutex

	stopCh chan struct{}
	doneCh chan struct{}

	lastSnapshotPersistAtMs int64

	filteredFilmCodeItemIDs map[string]struct{}
}

// Runner 是 Go 原生抓取主循环。
//
// 设计目标不是拆到最细，而是把调度、恢复、补查、二次校验和结果落盘
// 放在同一条状态链里，后续排错时只看这一处就能串起整次任务。
func NewRunner(cfg Config, outputDir string) (*Runner, error) {
	artifactPaths := crawlartifact.ResolveCrawlOutputPaths(outputDir)
	if strings.TrimSpace(cfg.UserDataDir) != "" {
		artifactPaths = crawlartifact.ResolveInternalArtifactPaths(cfg.UserDataDir, outputDir)
	}

	writer, err := crawloutput.NewWriterWithArtifactPaths(outputDir, artifactPaths)
	if err != nil {
		return nil, fmt.Errorf("create output writer: %w", err)
	}
	writer.SetMinReleaseDate(cfg.MinReleaseDate)

	r := &Runner{
		config:                   cfg,
		outputDir:                outputDir,
		writer:                   writer,
		tracker:                  NewTracker(),
		executionPlan:            DefaultExecutionPlan(),
		pageIndex:                1,
		expectedItemsPerPage:     intPtr(cfg.ItemsPerPage),
		filteredActressItemIDs:   map[string]struct{}{},
		filteredFilmCodeItemIDs:  map[string]struct{}{},
		failedDetailMap:          map[string]FailedDetail{},
		detailRecoveryAttemptMap: map[string]int{},
		eventHandlers:            map[PhaseEventType][]EventHandler{},
		stopCh:                   make(chan struct{}),
		doneCh:                   make(chan struct{}),
	}
	return r, nil
}

func (r *Runner) SetFetchFuncs(
	indexFunc func(ctx context.Context, baseURL string, search string, pageNumber int) ([]string, crawlrequest.PageResponse, error),
	detailFunc func(ctx context.Context, detailURL string) (crawlparse.Metadata, crawlrequest.PageResponse, error),
) {
	r.fetchIndexPage = indexFunc
	r.fetchDetail = detailFunc
}

func (r *Runner) On(eventType PhaseEventType, handler EventHandler) {
	r.eventMu.Lock()
	defer r.eventMu.Unlock()
	r.eventHandlers[eventType] = append(r.eventHandlers[eventType], handler)
}

// Event handlers are the runner's outward observation boundary. They should
// carry enough structured state for read-model services to project their own
// UI without reparsing internal runner details.
func (r *Runner) emitState(status RunnerStatus, message string) {
	event := PhaseEvent{
		Type:    EventState,
		Status:  status,
		Message: message,
		Phase:   r.currentPhase,
		Stats:   r.statsForStatus(status),
	}
	// `crawl.state` is the raw source of truth for task progress. The event
	// payload intentionally contains enough structured fields for later review,
	// quality, and UI projection services to rebuild their own panels without
	// reparsing log lines.
	event.Data = r.stateDetails(status)
	r.persistTaskState(status, message, false, "")
	r.emit(event)
}

func (r *Runner) emitLog(level string, message string) {
	r.emit(PhaseEvent{
		Type:    EventLog,
		Status:  StatusRunning,
		Message: fmt.Sprintf("[%s] %s", level, message),
	})
}

func (r *Runner) emit(event PhaseEvent) {
	r.eventMu.RLock()
	defer r.eventMu.RUnlock()
	if handlers, ok := r.eventHandlers[event.Type]; ok {
		for _, h := range handlers {
			h(event)
		}
	}
}

func (r *Runner) stats() *RunnerStats {
	return r.statsForStatus(StatusRunning)
}

// statsForStatus exposes one completion contract to every live/terminal state
// event. During an active run the counter reflects records already persisted;
// for a terminal state it uses the explicit accounting rule:
// total - filtered - duplicate - failed.
func (r *Runner) statsForStatus(status RunnerStatus) *RunnerStats {
	final := isFinalRunnerStatus(status)
	breakdown := r.completionBreakdown(final)
	completedIDs := r.completedItemIDsFor(final)
	completed := calculateCompletedCount(status, breakdown, completedIDs)
	filteredItems := r.filteredItems()
	filteredActressItems := r.filteredActressItems()
	return &RunnerStats{
		Queued:                 r.filmsQueued,
		Attempted:              r.filmsAttempted,
		Completed:              completed,
		TotalItems:             breakdown.Total,
		FilteredItemsCount:     breakdown.Filtered,
		DuplicateItemsCount:    breakdown.Duplicates,
		FailedItemsCount:       breakdown.Failed,
		PageIndex:              r.pageIndex,
		FilteredByActressCount: len(filteredActressItems),
		FilteredItemIDs:        filteredItems,
		CompletedItems:         completed,
		CompletedItemIDs:       completedIDs,
		CompletedMagnetCount:   r.writer.OutputMagnetCount(),
	}
}

func (r *Runner) Run(ctx context.Context) error {
	// Run owns the full crawl lifecycle from startup through final output
	// publication. Higher layers should treat this as the single execution
	// boundary instead of trying to reproduce terminal-state logic themselves.
	r.mu.Lock()
	if r.isRunning {
		r.mu.Unlock()
		return fmt.Errorf("crawl task is already running")
	}
	r.isRunning = true
	r.isStopping = false
	r.doneCh = make(chan struct{})
	r.runCtx = ctx
	r.mu.Unlock()

	r.startedAt = time.Now().Format(time.RFC3339)
	r.emitState(StatusStarting, "preparing crawl configuration")
	r.emitLog("info", "starting JAV crawl task")

	r.applyRestoredState()

	if err := r.mainExecution(ctx); err != nil {
		r.persistTaskState(StatusError, err.Error(), true, "")
		r.emitState(StatusError, err.Error())
		r.mu.Lock()
		r.isRunning = false
		doneCh := r.doneCh
		r.mu.Unlock()
		close(doneCh)
		return err
	}

	r.mu.Lock()
	stopped := r.isStopping
	r.isRunning = false
	r.isStopping = false
	doneCh := r.doneCh
	r.mu.Unlock()

	if stopped {
		final := FinalStateOutput{Status: StatusStopped, Message: "crawl task stopped"}
		r.finalizeOutputArtifacts(final)
		r.persistTaskState(StatusStopped, "crawl task stopped", true, "")
		r.emitState(StatusStopped, "crawl task stopped")
	} else {
		final := r.determineFinalState()
		r.persistTaskState(final.Status, final.Message, true, "")
		r.emitState(final.Status, final.Message)
	}

	close(doneCh)
	return nil
}

func (r *Runner) Stop() {
	// Stop is cooperative: it marks the runner stopping and asks downstream work
	// queues to drain/shutdown, while final output/status emission still happens
	// back in Run() after execution returns.
	r.mu.Lock()
	if !r.isRunning || r.isStopping {
		r.mu.Unlock()
		return
	}
	r.isStopping = true
	r.mu.Unlock()

	r.emitState(StatusStopping, "stopping crawl task")
	r.emitLog("warn", "stop requested, shutting down work queues")

	if r.queueRunner != nil {
		r.queueRunner.Shutdown()
	}
}

// applyRestoredState is the resume bootstrap boundary. It merges explicit
// persisted task snapshots with durable crawl artifacts before any new crawl
// work starts.
func (r *Runner) applyRestoredState() {
	state := r.config.RestoredState
	if state == nil || !state.ShouldRestore {
		// Even without an explicit runtime snapshot we still restore persisted
		// film output. This lets the runner rebuild dedupe/filter knowledge from
		// `filmData.json` after manual stops, partial runs, or frontend reloads.
		r.restorePersistedOutputState()
		return
	}

	r.resumeExisting = true
	if state.PageIndex > 0 {
		r.pageIndex = state.PageIndex
	}
	if state.ExpectedItemsPerPage != nil {
		r.expectedItemsPerPage = state.ExpectedItemsPerPage
	}
	r.filmsQueued = state.FilmsQueued
	r.filmsAttempted = state.FilmsAttempted
	r.filmCount = state.FilmCount
	r.pageAudits = convertRestoredPageAudits(state.PageAudits)
	r.validationReport = cloneValidationReport(state.ValidationReport)
	if r.filteredActressItemIDs == nil {
		r.filteredActressItemIDs = map[string]struct{}{}
	}
	r.tracker.RestoreExpectedLinks(state.ExpectedLinks)
	r.tracker.RestoreQueuedLinks(state.QueuedLinks)
	r.tracker.RestoreProcessedLinks(state.ProcessedLinks)
	r.tracker.RestorePersistedLinks(state.PersistedLinks, state.PersistedFilmIDs)
	r.restoreFailedDetails(state.FailedDetails)
	for _, itemID := range state.SkippedItemIDs {
		r.tracker.MarkSkipped(itemID)
	}
	r.restorePersistedOutputState()
	if state.LogMessage != "" {
		r.emitLog("info", state.LogMessage)
	}
	r.persistTaskState(StatusRunning, "restored previous task state", true, "")
}

// mainExecution runs the phase chain in order. Each phase is expected to own a
// narrow responsibility so recovery and validation logic do not scatter.
func (r *Runner) mainExecution(ctx context.Context) error {
	// phase 链是执行顺序的唯一入口。
	// 每个阶段都尽量只处理自己的职责，避免恢复/补查逻辑散落在多个函数里。
	phases, err := r.buildPhaseChain(ctx)
	if err != nil {
		return err
	}

	for _, phase := range phases {
		r.mu.Lock()
		stopped := r.isStopping
		r.mu.Unlock()
		if stopped {
			break
		}

		r.currentPhase = phase.Key
		r.emitLog("info", "phase switch: "+phase.Label)
		r.emitState(StatusRunning, "current phase: "+phase.Label)

		if phase.Execute == nil {
			continue
		}

		runnerRef := r
		if phase.ShouldSkip != nil && phase.ShouldSkip(runnerRef) {
			r.emitLog("info", "phase skipped: "+phase.Label)
			continue
		}

		if err := phase.Execute(runnerRef); err != nil {
			if phase.Key == "boot" || phase.Key == "queue_setup" {
				r.emitLog("error", "critical phase failed: "+err.Error())
				return err
			}
			r.emitLog("error", "phase failed: "+err.Error())
		}
	}

	return nil
}

// Phase chain is the single execution-order contract for the Go crawler.
// When a task appears to stop after a certain stage, start from the matching
// phase key here before drilling into lower-level helpers.
func (r *Runner) buildPhaseChain(ctx context.Context) ([]PhaseDefinition, error) {
	plan := r.executionPlan
	phases := make([]PhaseDefinition, 0)

	for _, key := range plan.PhaseKeys {
		def := PhaseDefinition{Key: key}
		switch key {
		case "boot":
			def.Label = "start crawl flow"
			def.Execute = r.executeBoot
			def.NextPhase = plan.ResolveNextPhase(key, false)
		case "queue_setup":
			def.Label = "initialize work queues"
			def.Execute = r.executeQueueSetup
			def.NextPhase = plan.ResolveNextPhase(key, false)
		case "resume_pending":
			def.Label = "resume pending detail tasks"
			def.Execute = r.executeResumePending
			def.ShouldSkip = func(runnerRef any) bool { return !runnerRef.(*Runner).resumeExisting }
			def.NextPhase = plan.ResolveNextPhase(key, false)
		case "index_discovery":
			def.Label = "discover index pages"
			def.Execute = r.executeIndexDiscovery
			def.NextPhase = plan.ResolveNextPhase(key, false)
		case "queue_drain":
			def.Label = "drain work queues"
			def.Execute = r.executeQueueDrain
			def.NextPhase = plan.ResolveNextPhase(key, false)
		case "page_gap_recovery":
			def.Label = "recover page gaps"
			def.Execute = r.executePageGapRecovery
			def.NextPhase = plan.ResolveNextPhase(key, false)
		case "queue_gap_recovery":
			def.Label = "recover queue gaps"
			def.Execute = r.executeQueueGapRecovery
			def.NextPhase = plan.ResolveNextPhase(key, false)
		case "detail_recovery":
			def.Label = "recover failed details"
			def.Execute = r.executeDetailRecovery
			def.NextPhase = plan.ResolveNextPhase(key, false)
		case "second_validation":
			def.Label = "second validation"
			def.Execute = r.executeSecondValidation
			def.ShouldSkip = func(runnerRef any) bool { return !runnerRef.(*Runner).configSecondValidation() }
			def.NextPhase = plan.ResolveNextPhase(key, false)
		case "final_drain":
			def.Label = "final output flush"
			def.Execute = r.executeFinalDrain
			def.NextPhase = PhaseKey("")
		}
		phases = append(phases, def)
	}
	return phases, ctx.Err()
}

func (r *Runner) configSecondValidation() bool {
	return r.config.SecondValidation
}

func (r *Runner) executeBoot(runnerRef any) error {
	r.emitLog("info", "booting JAV crawl task")
	r.emitLog("info", fmt.Sprintf("runner config: %+v", r.config))
	r.emitLog(
		"info",
		fmt.Sprintf(
			"filter diagnostics: actressCountFilterThreshold=%d, filmCodeFilterThreshold=%q, nomag=%t, metadataOnly=%t, output=%s",
			r.config.ActressCountFilterThreshold,
			r.config.FilmCodeFilterThreshold,
			r.config.Nomag,
			r.config.MetadataOnly,
			r.outputDir,
		),
	)
	return nil
}

// Queue setup wires crawlqueue events back into runner-owned counters,
// failure tracking, and output persistence bookkeeping.
func (r *Runner) executeQueueSetup(runnerRef any) error {
	r.emitLog("info", "initializing work queues")

	r.queueRunner = crawlqueue.NewRunner(crawlqueue.RunnerOptions{
		DetailConcurrency: r.config.Parallel,
		DetailHandler: func(ctx context.Context, task crawlqueue.DetailPageTask) error {
			return r.processDetailTask(ctx, task)
		},
		FileConcurrency: 1,
		FileHandler: func(ctx context.Context, task any) error {
			fd, ok := task.(crawlparse.FilmData)
			if !ok {
				return nil
			}
			_, err := r.writer.WriteFilmData(r.buildOutputFilmData(fd, "", nil))
			return err
		},
	})
	r.queueRunner.On(crawlqueue.EventDetailPageStart, func(event crawlqueue.QueueEvent) {
		r.filmsAttempted++
	})
	r.queueRunner.On(crawlqueue.EventDetailPageFailed, func(event crawlqueue.QueueEvent) {
		link := ""
		reason := ""
		if payload, ok := event.Data.(map[string]string); ok {
			link = strings.TrimSpace(payload["link"])
			reason = strings.TrimSpace(payload["reason"])
		}
		if reason == "" {
			reason = strings.TrimSpace(fmt.Sprintf("%v", event.Data))
		}
		if link != "" {
			r.recordDetailFailure(link, reason)
			r.emitLog("warn", fmt.Sprintf("detail page failed: %s | %s", link, reason))
			return
		}
		r.emitLog("warn", "detail page failed: "+reason)
	})
	r.queueRunner.On(crawlqueue.EventFilmDataSaved, func(event crawlqueue.QueueEvent) {
		r.filmCount = r.writer.RecordCount()
	})
	r.queueRunner.Start()
	return nil
}

// Detail-task processing is the record-preservation boundary: parsed metadata,
// optional magnet fetch, and writer persistence converge here before
// reconciliation/final-state logic consumes the result.
func (r *Runner) processDetailTask(ctx context.Context, task crawlqueue.DetailPageTask) error {
	detailURL := strings.TrimSpace(task.Link)
	r.tracker.MarkAttempted(detailURL)

	if r.fetchDetail == nil {
		return fmt.Errorf("detail fetcher is not configured")
	}

	metadata, _, err := r.fetchDetail(ctx, detailURL)
	if err != nil {
		return err
	}

	filmData := crawlparse.ParseFilmData(metadata, detailURL)
	r.tracker.MarkProcessed(detailURL)

	var magnetResult *crawlrequest.MagnetResult
	var magnetErr error
	if r.config.MetadataOnly {
		r.emitLog("info", fmt.Sprintf("metadata-only: skip magnet lookup for %s", metadata.Title))
	} else {
		magnetResult, magnetErr = r.fetchMagnetForFilm(ctx, metadata, detailURL)
	}
	if magnetErr != nil {
		r.emitLog("warn", fmt.Sprintf("magnet fetch failed %s: %v", metadata.Title, magnetErr))
	}

	magnet := ""
	magnetLinks := []crawlrequest.MagnetLink{}
	if magnetResult != nil {
		magnet = magnetResult.Magnet
		magnetLinks = magnetResult.MagnetLinks
	}

	var outputLinks []struct {
		Link        string `json:"link"`
		Size        string `json:"size"`
		DisplayName string `json:"displayName,omitempty"`
	}
	for _, ml := range magnetLinks {
		outputLinks = append(outputLinks, struct {
			Link        string `json:"link"`
			Size        string `json:"size"`
			DisplayName string `json:"displayName,omitempty"`
		}{Link: ml.Link, Size: ml.Size, DisplayName: ml.DisplayName})
	}

	itemID := firstNonEmptyNonZero(getDetailItemIDFromLink(detailURL), extractFilmIDFromLink(detailURL), normalizeSourceLink(detailURL))
	if r.config.Nomag && !r.config.MetadataOnly && strings.TrimSpace(magnet) == "" {
		// `nomag` means "skip entries that still have no magnet after real magnet
		// lookup", not "skip magnet lookup entirely". We therefore preserve the
		// film in no output artifact and only mark the unique item as policy-skipped
		// so completion math and operator-facing summaries stay truthful.
		r.tracker.MarkSkipped(itemID)
		r.clearDetailFailure(detailURL)
		skippedTarget := itemID
		if strings.TrimSpace(skippedTarget) == "" {
			skippedTarget = detailURL
		}
		r.emitLog("info", fmt.Sprintf("nomag skipped: %s has no magnet after lookup", skippedTarget))
		return nil
	}

	if _, err := r.writer.WriteFilmData(r.buildOutputFilmData(filmData, magnet, outputLinks)); err != nil {
		return err
	}

	filmID := extractFilmIDFromLink(detailURL)
	r.tracker.MarkPersisted(detailURL, filmID)
	if itemID != "" {
		r.tracker.MarkPersisted("", itemID)
	}
	if magnetResult == nil && magnetErr == nil && !r.config.MetadataOnly {
		r.tracker.MarkSkipped(itemID)
	}
	r.clearDetailFailure(detailURL)
	r.filmCount = r.writer.RecordCount()

	return nil
}

// buildOutputFilmData is the output policy boundary.
//
// Keep the record in `filmData.json` for audit/review/resume, and only let the
// actress-count rule suppress magnet TXT export. If TXT counts look wrong, this
// is the first place to inspect.
func (r *Runner) buildOutputFilmData(filmData crawlparse.FilmData, magnet string, magnetLinks []struct {
	Link        string `json:"link"`
	Size        string `json:"size"`
	DisplayName string `json:"displayName,omitempty"`
}) crawloutput.FilmData {
	actressCount := len(filmData.Actress)
	output := crawloutput.FilmData{
		Title:        filmData.Title,
		SourceLink:   filmData.SourceLink,
		Category:     filmData.Category,
		Actress:      filmData.Actress,
		CoverImage:   filmData.CoverImage,
		ReleaseDate:  filmData.ReleaseDate,
		Maker:        filmData.Maker,
		Label:        filmData.Label,
		Series:       filmData.Series,
		MagnetLinks:  magnetLinks,
		Magnet:       magnet,
		ActressCount: actressCount,
	}

	threshold := r.config.ActressCountFilterThreshold
	if threshold > 0 && actressCount >= threshold {
		// The actress-count rule is output-only filtering. The full film record
		// must still be preserved in `filmData.json` for audit/review/resume, but
		// its magnet should not be exported to `magnet-links.txt`.
		output.FilteredByActressCount = true
		output.FilterRemark = fmt.Sprintf("actress count %d >= threshold %d, skip magnet-links.txt output only", actressCount, threshold)
		itemID := r.recordActressCountFiltered(filmData.SourceLink)
		r.emitActressCountFilteredLog(itemID, actressCount, threshold)
	}

	if r.isFilmCodeOrTitleFiltered(filmData.Title, filmData.SourceLink) {
		output.FilteredByFilmCode = true
		if output.FilterRemark != "" {
			output.FilterRemark += "; "
		}
		output.FilterRemark += fmt.Sprintf("film code matches filter %q, skip magnet-links.txt output only", r.config.FilmCodeFilterThreshold)
		itemID := r.recordFilmCodeFiltered(filmData.SourceLink)
		r.emitFilmCodeFilteredLog(itemID, r.config.FilmCodeFilterThreshold)
	}

	return output
}

// isFilmCodeOrTitleFiltered checks both the title and the extracted film code
// against the user-configured substring filter. Relying on the title alone
// misses codes like MDVR-393 when the title does not literally contain "VR".
func (r *Runner) isFilmCodeOrTitleFiltered(title, sourceLink string) bool {
	if r.isFilmCodeStringMatch(title) {
		return true
	}
	code := extractFilmIDFromLink(title)
	if code == "" {
		code = extractFilmIDFromLink(sourceLink)
	}
	return r.isFilmCodeStringMatch(code)
}

func (r *Runner) isFilmCodeStringMatch(value string) bool {
	if r.config.FilmCodeFilterThreshold == "" {
		return false
	}

	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return false
	}

	for _, part := range splitFilmCodeFilterKeywords(r.config.FilmCodeFilterThreshold) {
		substring := strings.ToLower(strings.TrimSpace(part))
		if substring != "" && strings.Contains(normalized, substring) {
			return true
		}
	}
	return false
}

// splitFilmCodeFilterKeywords accepts the separators users naturally enter in
// the desktop settings. In particular, Chinese/Japanese punctuation such as
// "VR、OFJE" must behave the same as the documented "VR, OFJE" form.
func splitFilmCodeFilterKeywords(value string) []string {
	return strings.FieldsFunc(value, func(char rune) bool {
		switch char {
		case ',', '，', '、', ';', '；', '\n', '\r', '\t':
			return true
		default:
			return false
		}
	})
}

// isFilmCodeLinkFiltered checks a detail link's extracted film code against
// the user-configured substring filter. This is used at enqueue time to skip
// fetching details/magnets for matching codes entirely.
func (r *Runner) isFilmCodeLinkFiltered(link string) bool {
	if r.config.FilmCodeFilterThreshold == "" {
		return false
	}
	code := extractFilmIDFromLink(link)
	if code == "" {
		code = strings.ToLower(normalizeSourceLink(link))
	}
	return r.isFilmCodeStringMatch(code)
}

// skipFilmCodeFilteredLink persists a minimal audit record for a film-code
// filtered item without fetching its detail or magnet pages.
func (r *Runner) skipFilmCodeFilteredLink(link, itemID string) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		itemID = strings.TrimSpace(firstNonEmptyNonZero(getDetailItemIDFromLink(link), extractFilmIDFromLink(link), normalizeSourceLink(link)))
	}

	r.recordFilmCodeFiltered(link)
	r.emitFilmCodeFilteredLog(itemID, r.config.FilmCodeFilterThreshold)

	title := itemID
	if title == "" {
		title = strings.TrimSpace(link)
	}

	output := crawloutput.FilmData{
		Title:              title,
		SourceLink:         link,
		FilteredByFilmCode: true,
		FilterRemark:       fmt.Sprintf("film code matches filter %q, skip detail/magnet fetch and magnet-links.txt output", r.config.FilmCodeFilterThreshold),
	}
	if _, err := r.writer.WriteFilmData(output); err != nil {
		r.emitLog("warn", fmt.Sprintf("write filtered film code record failed %s: %v", itemID, err))
	}

	r.tracker.MarkPersisted(link, itemID)
	if itemID != "" {
		r.tracker.MarkPersisted("", itemID)
	}
	r.filmCount = r.writer.RecordCount()
}

// recordActressCountFiltered tracks the item ID for operator-facing filtered
// summaries. It does not alter crawl completion state or restore state.
func (r *Runner) recordActressCountFiltered(link string) string {
	itemID := getDetailItemIDFromLink(link)
	if itemID == "" {
		itemID = extractFilmIDFromLink(link)
	}
	if itemID == "" {
		itemID = normalizeSourceLink(link)
	}
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.filteredActressItemIDs == nil {
		r.filteredActressItemIDs = map[string]struct{}{}
	}
	r.filteredActressItemIDs[itemID] = struct{}{}
	return itemID
}

// filteredActressItems returns a stable, sorted view for logs and summaries.
func (r *Runner) filteredActressItems() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]string, 0, len(r.filteredActressItemIDs))
	for itemID := range r.filteredActressItemIDs {
		items = append(items, itemID)
	}
	sort.Strings(items)
	return items
}

// completedItemIDs returns persisted film IDs that have a magnet and are not
// filtered by actress-count, film-code substring, or no-magnet policy.
func (r *Runner) completedItemIDs() []string {
	return r.completedItemIDsFor(false)
}

// completedItemIDsFor returns the persisted item IDs that are eligible to be
// counted as successful output. Failed detail IDs are excluded so a record
// left behind by a partial magnet/detail failure cannot inflate completion.
func (r *Runner) completedItemIDsFor(includeInferredFailures bool) []string {
	excluded := make(map[string]struct{})
	for _, id := range r.filteredItems() {
		excluded[id] = struct{}{}
	}
	for _, id := range r.failedItemIDs(includeInferredFailures) {
		excluded[id] = struct{}{}
	}
	recon := r.tracker.BuildReconciliation()
	for _, id := range recon.SkippedItemIDs {
		excluded[id] = struct{}{}
	}

	persisted := r.tracker.GetPersistedItemIDs()
	items := make([]string, 0, len(persisted))
	for _, id := range persisted {
		if id == "" {
			continue
		}
		if _, ok := excluded[id]; ok {
			continue
		}
		items = append(items, id)
	}
	sort.Strings(items)
	return items
}

// filteredItems merges all configured item-level filters into one stable list
// used by completion math and the review panel. The actress-specific list is
// retained separately for its detailed log wording.
func (r *Runner) filteredItems() []string {
	seen := map[string]struct{}{}
	for _, id := range r.filteredActressItems() {
		if id != "" {
			seen[id] = struct{}{}
		}
	}
	for _, id := range r.filteredFilmCodeItems() {
		if id != "" {
			seen[id] = struct{}{}
		}
	}
	items := make([]string, 0, len(seen))
	for id := range seen {
		items = append(items, id)
	}
	sort.Strings(items)
	return items
}

// emitActressCountFilteredLog keeps the single-item decision visible in the
// live log stream so the operator can trace why a magnet was suppressed.
func (r *Runner) emitActressCountFilteredLog(itemID string, actressCount int, threshold int) {
	if strings.TrimSpace(itemID) == "" {
		itemID = "unknown-item"
	}
	r.emitLog(
		"info",
		fmt.Sprintf(
			"actress count filtered: %s actressCount=%d threshold=%d, keep in filmData.json and skip magnet-links.txt",
			itemID,
			actressCount,
			threshold,
		),
	)
}

// emitActressCountFilteredSummaryLog is the final rollup for the filtered-item
// list. Use it when the per-item logs are too noisy and you want the one-line
// recap before the run exits.
func (r *Runner) emitActressCountFilteredSummaryLog() {
	filteredItems := r.filteredActressItems()
	if len(filteredItems) == 0 {
		return
	}
	r.emitLog(
		"info",
		fmt.Sprintf(
			"actress count filter summary: total=%d items=%s",
			len(filteredItems),
			strings.Join(filteredItems, ", "),
		),
	)
}

func (r *Runner) recordFilmCodeFiltered(link string) string {
	itemID := getDetailItemIDFromLink(link)
	if itemID == "" {
		itemID = extractFilmIDFromLink(link)
	}
	if itemID != "" {
		r.mu.Lock()
		r.filteredFilmCodeItemIDs[itemID] = struct{}{}
		r.mu.Unlock()
	}
	return itemID
}

func (r *Runner) filteredFilmCodeItems() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]string, 0, len(r.filteredFilmCodeItemIDs))
	for itemID := range r.filteredFilmCodeItemIDs {
		items = append(items, itemID)
	}
	sort.Strings(items)
	return items
}

func (r *Runner) emitFilmCodeFilteredLog(itemID string, filter string) {
	if strings.TrimSpace(itemID) == "" {
		itemID = "unknown-item"
	}
	r.emitLog(
		"info",
		fmt.Sprintf(
			"film code filtered: %s filter=%q, keep in filmData.json and skip magnet-links.txt",
			itemID,
			filter,
		),
	)
}

func (r *Runner) emitFilmCodeFilteredSummaryLog() {
	filteredItems := r.filteredFilmCodeItems()
	if len(filteredItems) == 0 {
		return
	}
	r.emitLog(
		"info",
		fmt.Sprintf(
			"film code filter summary: total=%d items=%s",
			len(filteredItems),
			strings.Join(filteredItems, ", "),
		),
	)
}

func (r *Runner) fetchMagnetForFilm(ctx context.Context, metadata crawlparse.Metadata, detailURL string) (*crawlrequest.MagnetResult, error) {
	if r.fetchMagnetFn != nil {
		return r.fetchMagnetFn(ctx, metadata.GID, metadata.UC, metadata.Img, metadata.Title)
	}

	if r.createFetchClient == nil {
		return nil, fmt.Errorf("magnet fetch client is not configured")
	}

	client, err := r.createFetchClient(r.config.Proxy, r.config.Timeout)
	if err != nil {
		return nil, err
	}

	baseURL := r.config.BaseURL
	if strings.TrimSpace(r.config.Base) != "" {
		baseURL = r.config.Base
	}

	candidates, err := crawlrequest.FetchMagnetCandidatesWithFallback(ctx, client, metadata, baseURL, detailURL)
	if err != nil {
		return nil, err
	}
	filteredCandidates := crawlrequest.ApplyMagnetExcludeFilter(candidates, r.config.MagnetExcludeKeywords)
	if removed := len(candidates) - len(filteredCandidates); removed > 0 {
		r.emitLog("warn", fmt.Sprintf("磁力候选已过滤 %d 条合集/用户排除项，避免大合集污染当前番号：%s", removed, metadata.Title))
	}
	candidates = filteredCandidates
	return crawlrequest.BuildMagnetResult(candidates, r.config.Allmag, r.config.SupplementMagnetTopN), nil
}

// Resume only requeues links that are still missing from persisted output.
// This keeps manual restarts and restore cycles from duplicating finished
// detail work.
func (r *Runner) executeResumePending(runnerRef any) error {
	r.emitLog("info", "checking pending detail tasks for resume")
	pendingLinks := []string{}
	if state := r.config.RestoredState; state != nil && len(state.PendingDetailLinks) > 0 {
		pendingLinks = append(pendingLinks, state.PendingDetailLinks...)
	} else {
		pendingLinks = r.missingDetailLinksForRecovery()
	}

	recoverable := make([]string, 0, len(pendingLinks))
	seen := map[string]struct{}{}
	for _, link := range pendingLinks {
		trimmed := strings.TrimSpace(link)
		if trimmed == "" || r.tracker.IsAlreadyPersisted(trimmed, true) {
			continue
		}
		normalized := normalizeDetailLink(trimmed)
		if normalized == "" {
			normalized = strings.ToLower(trimmed)
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		recoverable = append(recoverable, trimmed)
	}

	if len(recoverable) == 0 {
		r.emitLog("info", "no pending detail tasks need resume")
		return nil
	}

	r.emitLog("info", fmt.Sprintf("resuming %d pending detail tasks", len(recoverable)))
	r.emitState(StatusRunning, fmt.Sprintf("resuming %d pending detail tasks", len(recoverable)))
	r.enqueueDetailLinksWithMode(recoverable, false)
	return nil
}

// Index discovery is the only phase that grows the expected-detail frontier.
// Later recovery phases work from the audits and reconciliation it leaves
// behind instead of rediscovering target pages from scratch.
func (r *Runner) executeIndexDiscovery(runnerRef any) error {
	if r.fetchIndexPage == nil {
		return fmt.Errorf("index fetcher is not configured")
	}

	baseURL := r.config.BaseURL
	if r.config.Base != "" {
		baseURL = r.config.Base
	}

	shouldStopIndexing := false
	for !shouldStopIndexing {
		r.mu.Lock()
		stopped := r.isStopping
		r.mu.Unlock()
		if stopped {
			break
		}

		currentPage := r.pageIndex
		state := resolveIndexTargetPageState(currentPage, r.config.TotalPages, r.config.Limit, r.expectedItemsPerPage)
		expectedCount := getExpectedCountForPage(currentPage, state.TargetTotalPages, r.config.Limit, r.expectedItemsPerPage)

		r.emitLog("info", fmt.Sprintf("fetching index page %d: %s", currentPage, buildIndexPageURL(baseURL, r.config.Search, "", currentPage)))
		r.emitState(StatusRunning, fmt.Sprintf("fetching index page %d", currentPage))

		timeout := r.config.Timeout
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		pageCtx, cancel := context.WithTimeout(r.runCtx, timeout)
		links, response, err := r.fetchIndexPage(pageCtx, baseURL, r.config.Search, currentPage)
		cancel()

		if err != nil {
			r.emitLog("warn", fmt.Sprintf("index page %d fetch failed: %s", currentPage, err.Error()))
			delay := computeErrorDelay(r.config.Delay, err)
			r.emitLog("info", fmt.Sprintf("retrying index fetch after %v", delay))
			time.Sleep(delay)
			continue
		}

		trackedLinks := getTrackedPageLinks(links, r.config.Limit, len(r.tracker.expectedItemIDs))
		audit := r.createPageAudit(
			currentPage,
			buildIndexPageURL(baseURL, r.config.Search, "", currentPage),
			expectedCount,
			indexSourceEntryCount(response, len(links)),
			1,
			true,
			state.IsLastTargetPage,
			response.IndexDuplicateCount,
			response.IndexDuplicateItemIDs,
		)
		r.pageAudits = append(r.pageAudits, audit)
		r.tracker.RecordExpectedPageLinks(currentPage, trackedLinks)

		r.emitLog("info", fmt.Sprintf("index page %d parsed %d detail links", currentPage, len(links)))
		r.emitState(StatusRunning, fmt.Sprintf("index page %d parsed %d detail links", currentPage, len(links)))

		newLinks := make([]string, 0)
		for _, link := range trackedLinks {
			if !r.tracker.IsAlreadyPersisted(link, r.resumeExisting) && !r.tracker.IsQueued(getDetailItemIDFromLink(link)) {
				newLinks = append(newLinks, link)
			}
		}

		limitDecision := resolveIndexQueueLimit(r.config.Limit, r.filmsQueued, len(newLinks))
		if limitDecision.ShouldStopBeforeQueue {
			r.emitLog("info", "target limit reached before queueing new details")
			shouldStopIndexing = true
			break
		}

		queueCount := limitDecision.QueueCount
		if queueCount > len(newLinks) {
			queueCount = len(newLinks)
		}
		if queueCount > 0 {
			r.enqueueDetailLinks(newLinks[:queueCount])
		}

		if limitDecision.ShouldStopAfterQueue {
			shouldStopIndexing = true
			break
		}

		if state.IsLastTargetPage {
			shouldStopIndexing = true
			break
		}

		r.pageIndex = currentPage + 1
		delay := computePageDelay(r.config.Delay, r.queueRunner)
		r.emitLog("info", fmt.Sprintf("waiting %v before next page", delay))
		time.Sleep(delay)
	}

	if !r.isStopping {
		r.emitLog("info", "index discovery finished")
	}
	return nil
}

func (r *Runner) enqueueDetailLinks(links []string) {
	r.enqueueDetailLinksWithMode(links, true)
}

func (r *Runner) enqueueDetailLinksWithMode(links []string, countAsQueued bool) {
	if len(links) == 0 {
		return
	}

	tasks := make([]crawlqueue.DetailPageTask, 0, len(links))
	seen := map[string]struct{}{}
	for _, rawLink := range links {
		link := strings.TrimSpace(rawLink)
		if link == "" {
			continue
		}

		normalized := normalizeDetailLink(link)
		if normalized == "" {
			normalized = strings.ToLower(link)
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}

		itemID := firstNonEmptyNonZero(getDetailItemIDFromLink(link), extractFilmIDFromLink(link), normalizeSourceLink(link))
		// Film-code filtering is applied before enqueueing so the crawler does
		// not waste time fetching details/magnets for filtered codes. A minimal
		// record is still written to filmData.json/filtered-codes.txt for audit.
		if r.isFilmCodeLinkFiltered(link) {
			if countAsQueued {
				if itemID != "" && r.tracker.IsQueued(itemID) {
					continue
				}
				r.tracker.MarkQueued(link, itemID)
				r.filmsQueued++
			}
			r.skipFilmCodeFilteredLink(link, itemID)
			continue
		}
		if countAsQueued {
			if itemID != "" && r.tracker.IsQueued(itemID) {
				continue
			}
			r.tracker.MarkQueued(link, itemID)
			r.filmsQueued++
		}

		tasks = append(tasks, crawlqueue.DetailPageTask{Link: link})
	}

	if r.queueRunner != nil && len(tasks) > 0 {
		r.queueRunner.PushDetailBatch(tasks)
	}
}

func (r *Runner) executeQueueDrain(runnerRef any) error {
	r.emitLog("info", "index discovery finished, waiting for work queues")
	if err := r.waitForWorkQueues(); err != nil {
		return err
	}
	if err := r.writer.Flush(); err != nil {
		r.emitLog("error", "flush output failed: "+err.Error())
	}
	return nil
}

func (r *Runner) waitForWorkQueues() error {
	for {
		r.mu.Lock()
		stopped := r.isStopping
		r.mu.Unlock()
		if stopped {
			return nil
		}
		if r.queueRunner == nil || r.queueRunner.WorkQueuesFinished() {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// Page-gap recovery re-fetches suspicious index pages, not detail pages. Its
// job is to repair missing frontier discovery before detail recovery runs.
func (r *Runner) executePageGapRecovery(runnerRef any) error {
	r.emitLog("info", "checking page gaps")
	if r.fetchIndexPage == nil {
		return fmt.Errorf("index fetcher is not configured")
	}

	baseURL := r.config.BaseURL
	if r.config.Base != "" {
		baseURL = r.config.Base
	}

	const totalPasses = 2
	for pass := 1; pass <= totalPasses; pass++ {
		recoverable := r.getRecoverableAudits()
		passStart := crawlexecution.ResolvePageGapPassStart(len(recoverable), pass)
		if passStart.LogMessage != "" {
			r.emitLog("info", passStart.LogMessage)
		}
		if !passStart.ShouldRunPass {
			return nil
		}

		recoveredCount := 0
		messages := crawlexecution.BuildPageGapRecoveryMessages(len(recoverable), pass, totalPasses)
		r.emitLog("info", messages.LogMessage)
		r.emitState(StatusRunning, messages.StateMessage)

		for _, audit := range recoverable {
			r.mu.Lock()
			stopped := r.isStopping
			r.mu.Unlock()
			if stopped {
				break
			}
			if audit.ExpectedCount == nil || *audit.ExpectedCount <= 0 {
				continue
			}

			timeout := r.config.Timeout
			if timeout <= 0 {
				timeout = 30 * time.Second
			}

			pageNumber := audit.PageNumber
			expectedCount := *audit.ExpectedCount
			r.emitLog("info", fmt.Sprintf("recovering page gap page=%d current=%d expected=%d", pageNumber, audit.ActualCount, expectedCount))
			r.emitState(StatusRunning, crawlexecution.BuildPageGapActiveStateMessage(pageNumber))

			pageCtx, cancel := context.WithTimeout(r.runCtx, timeout)
			links, response, err := r.fetchIndexPage(pageCtx, baseURL, r.config.Search, pageNumber)
			cancel()
			if err != nil {
				r.emitLog("warn", fmt.Sprintf("page gap retry failed page=%d err=%s", pageNumber, err.Error()))
				continue
			}

			trackedLinks := getTrackedPageLinks(links, r.config.Limit, r.tracker.ExpectedEntryCount())

			newLinks := make([]string, 0, len(trackedLinks))
			for _, link := range trackedLinks {
				itemID := firstNonEmptyNonZero(getDetailItemIDFromLink(link), extractFilmIDFromLink(link), normalizeSourceLink(link))
				if r.tracker.IsAlreadyPersisted(link, true) || (itemID != "" && r.tracker.IsQueued(itemID)) {
					continue
				}
				newLinks = append(newLinks, link)
			}
			// A recovery pass revisits an already-audited page. Only genuinely new
			// links belong in Tracker; recording the whole page again would turn the
			// retry itself into a false duplicate.
			r.tracker.RecordExpectedPageLinks(pageNumber, newLinks)

			mergeResult := crawlexecution.CalculatePageGapRecoveryResult(crawlexecution.MergePageGapRecoveryInput{
				ExpectedCount:      expectedCount,
				CurrentActualCount: audit.ActualCount,
				FetchedActualCount: indexSourceEntryCount(response, len(links)),
				NewLinksCount:      len(newLinks),
			})
			recoveredCount += mergeResult.RecoveredCount

			retryCount := common.MaxInt(1, audit.RetryCount+1)
			isLastTargetPage := r.config.TotalPages > 0 && pageNumber >= r.config.TotalPages
			refreshedAudit := r.createPageAudit(
				pageNumber,
				buildIndexPageURL(baseURL, r.config.Search, "", pageNumber),
				audit.ExpectedCount,
				mergeResult.MergedActualCount,
				retryCount,
				mergeResult.ValidationPassed,
				isLastTargetPage,
				response.IndexDuplicateCount,
				response.IndexDuplicateItemIDs,
			)
			refreshedAudit.Reason = strings.TrimSpace(mergeResult.Reason + "; " + refreshedAudit.Reason)
			r.replacePageAudit(refreshedAudit)
			r.emitState(StatusRunning, fmt.Sprintf("page %d gap review updated", pageNumber))

			followUp := crawlexecution.ResolvePageGapAuditFollowUp(
				pageNumber,
				expectedCount,
				mergeResult.MergedActualCount,
				len(newLinks),
				mergeResult.ValidationPassed,
			)
			r.emitLog("info", followUp.LogMessage)
			if followUp.Action == "enqueue_new_links" {
				r.enqueueDetailLinksWithMode(newLinks, true)
			}
		}

		if err := r.waitForWorkQueues(); err != nil {
			return err
		}

		remainingAudits := r.getRecoverableAudits()
		passEnd := crawlexecution.ResolvePageGapPassEnd(len(remainingAudits), recoveredCount)
		if passEnd.LogMessage != "" {
			r.emitLog("info", passEnd.LogMessage)
		}
		if passEnd.StopRecovery {
			break
		}
	}

	if remaining := r.getRecoverableAudits(); len(remaining) > 0 {
		r.emitLog("info", crawlexecution.BuildPageGapRemainingMessage(len(remaining)))
	}
	return nil
}

func (r *Runner) executeQueueGapRecovery(runnerRef any) error {
	r.emitLog("info", "checking queue gaps")
	queueGapLinks := r.tracker.ExpectedButNotQueuedLinks()
	if len(queueGapLinks) == 0 {
		r.emitLog("info", "no queue gaps found")
		return nil
	}

	messages := crawlexecution.BuildQueueGapRecoveryMessages(len(queueGapLinks))
	r.emitLog("info", messages.LogMessage)
	r.emitState(StatusRunning, messages.StateMessage)
	r.enqueueDetailLinksWithMode(queueGapLinks, true)

	if err := r.waitForWorkQueues(); err != nil {
		return err
	}

	if remaining := r.tracker.ExpectedButNotQueuedIDs(); len(remaining) > 0 {
		r.emitLog("info", crawlexecution.BuildQueueGapRemainingMessage(len(remaining)))
	} else {
		r.emitLog("info", "queue gap recovery completed with no remaining gaps")
	}
	return nil
}

func (r *Runner) executeDetailRecovery(runnerRef any) error {
	r.emitLog("info", "checking failed detail pages")
	const totalPasses = 4

	for pass := 1; pass <= totalPasses; pass++ {
		missingLinks := r.missingDetailLinksForRecovery()
		recoverableLinks := r.getRecoverableMissingDetailLinks(missingLinks)
		budgetExhaustedCount := r.countBudgetExhaustedDetails(missingLinks)

		passStart := crawlexecution.ResolveDetailRecoveryPassStart(
			pass,
			len(missingLinks),
			len(recoverableLinks),
			budgetExhaustedCount,
		)
		for _, message := range passStart.LogMessages {
			r.emitLog("info", message)
		}
		if !passStart.ShouldRunPass {
			if passStart.Status == "completed" {
				return nil
			}
			break
		}

		recoverySummary := r.buildRecoveryCategorySummary(recoverableLinks)
		messages := crawlexecution.BuildDetailRecoveryMessages(len(missingLinks), pass, totalPasses, recoverySummary)
		r.emitLog("info", messages.LogMessage)
		r.emitState(StatusRunning, messages.StateMessage)

		for _, link := range recoverableLinks {
			r.incrementDetailRecoveryAttempt(link)
		}
		r.enqueueDetailLinksWithMode(recoverableLinks, false)

		if err := r.waitForWorkQueues(); err != nil {
			return err
		}

		remainingAfterPass := r.missingDetailLinksForRecovery()
		nextRecoverableCount := 0
		if len(remainingAfterPass) >= len(missingLinks) {
			nextRecoverableCount = len(r.getRecoverableMissingDetailLinks(remainingAfterPass))
		}
		passEnd := crawlexecution.ResolveDetailRecoveryPassEnd(
			pass,
			len(missingLinks),
			len(remainingAfterPass),
			nextRecoverableCount,
		)
		if passEnd.LogMessage != "" {
			r.emitLog("info", passEnd.LogMessage)
		}
		if passEnd.StopRecovery {
			if passEnd.Status == "completed" {
				return nil
			}
			break
		}
		if len(remainingAfterPass) == 0 {
			return nil
		}
	}

	if remaining := r.missingDetailLinksForRecovery(); len(remaining) > 0 {
		r.emitLog("info", crawlexecution.BuildDetailRecoveryRemainingMessage(len(remaining)))
	}
	return nil
}

func (r *Runner) executeSecondValidation(runnerRef any) error {
	r.emitLog("info", "running second validation")
	if err := r.writer.Flush(); err != nil {
		return fmt.Errorf("second validation flush failed: %w", err)
	}

	report, err := r.buildValidationReport()
	if err != nil {
		return err
	}
	if err := r.saveValidationReport(report); err != nil {
		return err
	}
	r.validationReport = cloneValidationReport(report)
	r.emitLog("info", report.Summary)
	r.emitState(StatusRunning, "second validation completed")
	return nil
}

func (r *Runner) executeFinalDrain(runnerRef any) error {
	final := r.determineFinalState()
	r.finalizeOutputArtifacts(final)
	r.persistTaskState(final.Status, final.Message, true, "")
	return nil
}

// finalizeOutputArtifacts is the final durable write boundary for a run. If
// runtime status looks correct but `filmData.json`, `magnet-links.txt`, or the
// unfinished report disagree, inspect this stage first.
func (r *Runner) finalizeOutputArtifacts(final FinalStateOutput) {
	r.emitLog("info", "finalizing output artifacts")
	r.writer.SetArtifactMetadata(r.buildArtifactMetadata(final))
	if err := r.writer.Flush(); err != nil {
		r.emitLog("error", "final flush failed: "+err.Error())
	}
	if err := r.writeUnfinishedReport(final); err != nil {
		r.emitLog("error", "write unfinished report failed: "+err.Error())
	}
	r.emitActressCountFilteredSummaryLog()
	r.emitFilmCodeFilteredSummaryLog()
	r.emitFailedDetailSummaryLog()
}

// writeUnfinishedReport is human-facing review output. It prioritizes
// conclusions, gaps, and recovery clues over machine-friendly formatting.
func (r *Runner) writeUnfinishedReport(final FinalStateOutput) error {
	// 未完成报告面向人工复盘，因此优先写结论、缺口、失败详情和可恢复项。
	recon := r.tracker.BuildReconciliation()
	entryCount := r.expectedReviewTotal()
	uncaptured := r.tracker.GetUncapturedItems()
	pageGapLines := r.buildPageGapLines()
	failedDetails, failedDetailsTotal := r.reviewFailedDetails(true)
	duplicateItems := r.reviewDuplicateItems()
	duplicateEntryCount := r.reviewDuplicateEntryCount()
	lowConfidenceCount := 0
	for _, audit := range r.pageAudits {
		if audit.ConfidenceScore < 60 {
			lowConfidenceCount++
		}
	}

	uniqueExpected := common.MaxInt(r.expectedReviewTotal()-duplicateEntryCount, 0)
	lines := []string{
		fmt.Sprintf("# 任务状态：%s", final.Status.Label()),
		fmt.Sprintf("# 状态说明：%s", final.Message),
		fmt.Sprintf("# 已完成：%d", r.effectiveCompletedCount(final.Status)),
	}

	if r.config.Limit > 0 {
		lines = append(lines, fmt.Sprintf("# 目标条数：%d", r.config.Limit))
		lines = append(lines, fmt.Sprintf("# 站点原始条目：%d", entryCount))
		lines = append(lines, fmt.Sprintf("# 站点唯一番号：%d", uniqueExpected))
	}
	if duplicateEntryCount > 0 {
		lines = append(lines, fmt.Sprintf("# 站点重复条目：%d", duplicateEntryCount))
	}
	lines = append(lines, fmt.Sprintf("# 低可信分页：%d", lowConfidenceCount))

	lines = append(lines, "# 已定位未完成番号")
	if len(uncaptured) > 0 {
		lines = append(lines, uncaptured...)
	} else {
		lines = append(lines, "暂无已定位未完成番号。")
	}

	lines = append(lines, "# 已定位重复番号")
	if len(duplicateItems) > 0 {
		lines = append(lines, duplicateItems...)
	} else {
		lines = append(lines, "当前未发现重复番号。")
	}

	lines = append(lines, fmt.Sprintf("# 失败详情页（共 %d 条）", failedDetailsTotal))
	if len(failedDetails) > 0 {
		for _, detail := range failedDetails {
			itemID := firstNonEmptyNonZero(detail.Item, getDetailItemIDFromLink(detail.SourceLink), extractFilmIDFromLink(detail.SourceLink), normalizeSourceLink(detail.SourceLink))
			category := strings.TrimSpace(detail.Category)
			reason := strings.TrimSpace(detail.Reason)
			switch {
			case category != "" && reason != "":
				lines = append(lines, fmt.Sprintf("%s | %s | %s", itemID, category, reason))
			case reason != "":
				lines = append(lines, fmt.Sprintf("%s | %s", itemID, reason))
			default:
				lines = append(lines, itemID)
			}
		}
	} else {
		lines = append(lines, "暂无失败详情页。")
	}

	if len(pageGapLines) > 0 {
		lines = append(lines, "# 未定位分页缺口")
		lines = append(lines, pageGapLines...)
	}

	filteredItems := r.filteredItems()
	if len(filteredItems) > 0 {
		lines = append(lines, "# 过滤影片番号（演员数量或番号过滤）")
		lines = append(lines, filteredItems...)
	}

	if len(recon.ExpectedButNotQueuedIDs) > 0 {
		lines = append(lines, fmt.Sprintf("# 入队缺口：%d", len(recon.ExpectedButNotQueuedIDs)))
	}
	if r.validationReport != nil && strings.TrimSpace(r.validationReport.Summary) != "" {
		lines = append(lines, "# 二次校验摘要："+strings.TrimSpace(r.validationReport.Summary))
	}

	return r.writer.WriteUnfinishedReport(lines)
}

func (r *Runner) buildPageGapLines() []string {
	audits := r.getRecoverableAudits()
	lines := make([]string, 0, len(audits))
	for _, audit := range audits {
		expected := 0
		if audit.ExpectedCount != nil {
			expected = *audit.ExpectedCount
		}
		missing := common.MaxInt(expected-audit.ActualCount, 0)
		lines = append(lines, fmt.Sprintf("page=%d missing=%d actual=%d expected=%d confidence=%d reason=%s", audit.PageNumber, missing, audit.ActualCount, expected, audit.ConfidenceScore, audit.Reason))
	}
	return lines
}

func (r *Runner) getRecoverableAudits() []PageAudit {
	result := make([]PageAudit, 0)
	for _, audit := range r.pageAudits {
		expected := 0
		if audit.ExpectedCount != nil {
			expected = *audit.ExpectedCount
		}
		if audit.ActualCount < expected || audit.ConfidenceScore < 60 {
			result = append(result, audit)
		}
	}
	return result
}

func (r *Runner) replacePageAudit(next PageAudit) {
	for index, audit := range r.pageAudits {
		if audit.PageNumber == next.PageNumber {
			r.pageAudits[index] = next
			return
		}
	}
	r.pageAudits = append(r.pageAudits, next)
}

// determineFinalState summarizes the final execution outcome. It should not
// replay phase logic; it only aggregates what the run ended with.
func (r *Runner) determineFinalState() FinalStateOutput {
	// 最终状态汇总只做数据收束，不再反推执行过程。
	// 这里产出的结论会被质量摘要、未完成报告和前端状态面板同时消费。
	recon := r.tracker.BuildReconciliation()
	_, failedCount := r.reviewFailedDetails(true)
	duplicateItems := r.reviewDuplicateItems()
	duplicateEntryCount := r.reviewDuplicateEntryCount()

	lowConfCount := 0
	for _, audit := range r.getRecoverableAudits() {
		if audit.ConfidenceScore < 60 {
			lowConfCount++
		}
	}

	validationPassed := true
	if r.validationReport != nil {
		validationPassed = r.validationReport.Passed
	}

	return BuildFinalState(FinalStateInput{
		UnresolvedCount:         len(recon.ExpectedButNotPersistedIDs),
		QueueGapCount:           len(recon.ExpectedButNotQueuedIDs),
		ProcessedGapCount:       len(recon.ProcessedButNotPersistedIDs),
		FailedCount:             failedCount,
		LowConfidencePageCount:  lowConfCount,
		DuplicateExpectedCount:  len(duplicateItems),
		DuplicateItemIDs:        duplicateItems,
		DuplicateItemSummary:    BuildDuplicateItemSummary(duplicateItems, 6),
		UnfinishedItems:         r.tracker.GetUncapturedItems(),
		ExpectedEntryCount:      r.expectedReviewTotal(),
		RawDuplicateEntryCount:  duplicateEntryCount,
		DuplicateSummary:        BuildDuplicateItemSummary(duplicateItems, 4),
		ConfiguredTargetCount:   r.config.Limit,
		ValidationPassed:        validationPassed,
		SecondValidationEnabled: r.config.SecondValidation,
		CompletedCount:          r.effectiveCompletedCount(StatusCompleted),
		SkippedByPolicyCount:    len(recon.SkippedItemIDs),
		ExpectedUniqueCount:     len(recon.ExpectedIDs),
	})
}

func (r *Runner) createPageAudit(pageNumber int, pageURL string, expectedCount *int, actualCount int, retryCount int, validationPassed bool, isLastTargetPage bool, duplicateEntryCount int, duplicateItemIDs []string) PageAudit {
	confidence := "high"
	confidenceScore := 100
	reason := "page count normal"

	if expectedCount == nil {
		confidenceScore = 88
		if actualCount <= 0 {
			confidenceScore = 18
		}
		reason = "sample page parsed successfully"
	} else {
		ratio := 1.0
		if *expectedCount > 0 {
			ratio = float64(actualCount) / float64(*expectedCount)
		}
		confidenceScore = int(math.Max(0, math.Min(100, ratio*100)))
		if isLastTargetPage && actualCount > 0 && actualCount < *expectedCount {
			confidenceScore = common.MaxInt(confidenceScore, 72)
		}
		if !validationPassed && !isLastTargetPage {
			confidenceScore -= 12
		}
		if retryCount > 1 {
			confidenceScore -= common.MinInt(20, (retryCount-1)*5)
		}
		confidenceScore = common.MaxInt(0, common.MinInt(100, confidenceScore))

		if validationPassed {
			if actualCount >= *expectedCount {
				reason = "expected count reached"
			} else {
				reason = "accepted as final target page"
			}
		} else if isLastTargetPage && actualCount > 0 {
			reason = "final page below expected but acceptable"
		} else if *expectedCount > 0 && actualCount >= int(math.Ceil(float64(*expectedCount)*0.7)) {
			reason = "below expected, medium confidence"
		} else {
			reason = "significantly below expected, possible blocking or incomplete load"
		}
	}

	if confidenceScore >= 85 {
		confidence = "high"
	} else if confidenceScore >= 60 {
		confidence = "medium"
	} else {
		confidence = "low"
	}

	return PageAudit{
		PageNumber:          pageNumber,
		URL:                 pageURL,
		ExpectedCount:       expectedCount,
		ActualCount:         actualCount,
		RetryCount:          retryCount,
		ValidationPassed:    validationPassed,
		ConfidenceScore:     confidenceScore,
		Confidence:          confidence,
		Reason:              fmt.Sprintf("%s confidence=%d", reason, confidenceScore),
		UpdatedAt:           time.Now().Format(time.RFC3339),
		DuplicateEntryCount: common.MaxInt(duplicateEntryCount, 0),
		DuplicateItemIDs:    normalizePageDuplicateIDs(duplicateItemIDs),
	}
}

func computePageDelay(delaySeconds int, queueRunner *crawlqueue.Runner) time.Duration {
	baseMs := rand.Intn(2001) + delaySeconds*1000
	if queueRunner != nil {
		backlog := queueRunner.DetailBacklog()
		if backlog > 16 {
			baseMs = common.MaxInt(baseMs, 4000)
		}
	}
	return time.Duration(baseMs) * time.Millisecond
}

func computeErrorDelay(delaySeconds int, err error) time.Duration {
	msg := err.Error()
	if retryable, _ := isNetworkError(msg); retryable {
		return computeBackoffDelay(delaySeconds, 1)
	}
	return time.Duration(rand.Intn(5)+5) * time.Second
}

func computeBackoffDelay(delaySeconds int, attempt int) time.Duration {
	baseMs := float64(delaySeconds * 1000)
	exponential := math.Min(baseMs*math.Pow(2, float64(attempt)), 30000)
	jitter := float64(rand.Intn(3000))
	return time.Duration(exponential+jitter) * time.Millisecond
}

// Artifact metadata is the handoff contract for downstream organizer and
// subscription import flows. Keep it stable even when the crawl ended early.
func (r *Runner) buildArtifactMetadata(final FinalStateOutput) crawloutput.ArtifactMetadata {
	recon := r.tracker.BuildReconciliation()
	targetCount := r.config.Limit
	if targetCount <= 0 {
		targetCount = len(recon.ExpectedIDs)
	}
	if targetCount <= 0 {
		targetCount = r.config.Limit
	}

	completedAt := time.Now().Format(time.RFC3339)
	if strings.TrimSpace(r.startedAt) != "" && final.Status == StatusStopped {
		// Stopped tasks still need stable handoff artifacts. We keep the real
		// completion timestamp here, but preserve startedAt in runId generation.
		completedAt = time.Now().Format(time.RFC3339)
	}

	return crawloutput.ArtifactMetadata{
		RunID:          r.buildArtifactRunID(),
		CompletedAt:    completedAt,
		ActressName:    r.detectPrimaryActressName(),
		CrawlURL:       buildIndexPageURL(firstNonEmptyNonZero(r.config.Base, r.config.BaseURL), r.config.Search, "", 1),
		TargetCount:    targetCount,
		CompletedCount: r.effectiveCompletedCount(final.Status),
		ItemsPerPage:   r.config.ItemsPerPage,
		TotalPages:     r.config.TotalPages,
		SiteBase:       firstNonEmptyNonZero(r.config.Base, r.config.BaseURL),
	}
}

func (r *Runner) buildArtifactRunID() string {
	base := strings.TrimSpace(firstNonEmptyNonZero(r.startedAt, time.Now().Format(time.RFC3339)))
	seed := base + "|" + strings.TrimSpace(r.outputDir)
	sum := sha1.Sum([]byte(seed))
	return "crawl-" + hex.EncodeToString(sum[:])[:16]
}

func (r *Runner) detectPrimaryActressName() string {
	// The search field is the user's explicit crawl target. Compilation titles
	// may contain several actresses, so record-frequency inference must never
	// override that target when it is available.
	if target := strings.TrimSpace(r.config.Search); target != "" {
		return target
	}
	records, err := r.loadOutputRecords()
	if err != nil || len(records) == 0 {
		return ""
	}
	return crawloutputDetectPrimaryActress(records)
}

func crawloutputDetectPrimaryActress(records []crawloutput.FilmData) string {
	type actressStat struct {
		name  string
		count int
		order int
	}

	stats := map[string]*actressStat{}
	nextOrder := 0
	for _, record := range records {
		seen := map[string]struct{}{}
		for _, actress := range record.Actress {
			name := strings.TrimSpace(actress)
			key := strings.ToLower(strings.Join(strings.Fields(name), ""))
			if key == "" {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			stat, exists := stats[key]
			if !exists {
				stat = &actressStat{name: name, order: nextOrder}
				stats[key] = stat
				nextOrder++
			}
			stat.count++
		}
	}

	var best *actressStat
	for _, stat := range stats {
		if best == nil || stat.count > best.count || (stat.count == best.count && stat.order < best.order) || (stat.count == best.count && stat.order == best.order && stat.name < best.name) {
			best = stat
		}
	}
	if best == nil {
		return ""
	}
	return strings.TrimSpace(best.name)
}

func isNetworkError(msg string) (bool, string) {
	upper := strings.ToUpper(msg)
	if strings.Contains(upper, "ECONNRESET") || strings.Contains(upper, "ETIMEDOUT") || strings.Contains(upper, "ENOTFOUND") {
		return true, "network"
	}
	return false, ""
}

func cloneIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	next := *value
	return &next
}

// buildSnapshot captures enough state for meaningful resume: queue progress,
// reconciliation state, validation output, and current run config.
func (r *Runner) buildSnapshot(status RunnerStatus, message string, mode crawlexecution.SnapshotMode) crawltaskstate.Snapshot {
	// 快照是恢复闭环的核心：保存当前进度、队列、校验结果和输出状态。
	recon := r.tracker.BuildReconciliation()
	return crawltaskstate.BuildSnapshot(crawltaskstate.BuilderParams{
		AppVersion: "0.4.41",
		Status:     string(status),
		Message:    strings.TrimSpace(message),
		StartedAt:  strings.TrimSpace(r.startedAt),
		Config: crawltaskstate.BuilderConfigInput{
			Base:             r.config.Base,
			Output:           r.config.Output,
			Limit:            r.config.Limit,
			TotalPages:       r.config.TotalPages,
			ItemsPerPage:     r.config.ItemsPerPage,
			Parallel:         r.config.Parallel,
			Delay:            r.config.Delay,
			Timeout:          int(r.config.Timeout / time.Millisecond),
			SecondValidation: r.config.SecondValidation,
			TaskTemplate:     "balanced",
		},
		PageIndex:            r.pageIndex,
		ExpectedItemsPerPage: cloneIntPointer(r.expectedItemsPerPage),
		FilmsQueued:          r.filmsQueued,
		FilmsAttempted:       r.filmsAttempted,
		FilmCount:            r.filmCount,
		ExpectedDetailLinks:  r.tracker.ExpectedDetailLinks(),
		QueuedDetailLinks:    r.tracker.QueuedDetailLinks(),
		ProcessedDetailLinks: r.tracker.ProcessedDetailLinks(),
		PersistedDetailLinks: r.tracker.PersistedDetailLinks(),
		PersistedFilmIDs:     r.tracker.PersistedFilmIDs(),
		SkippedItemIDs:       recon.SkippedItemIDs,
		Reconciliation: crawltaskstate.ReconciliationSnapshot{
			ExpectedIDs:                 append([]string(nil), recon.ExpectedIDs...),
			QueuedIDs:                   append([]string(nil), recon.QueuedIDs...),
			PersistedIDs:                append([]string(nil), recon.PersistedIDs...),
			ExpectedButNotQueuedIDs:     append([]string(nil), recon.ExpectedButNotQueuedIDs...),
			ExpectedButNotPersistedIDs:  append([]string(nil), recon.ExpectedButNotPersistedIDs...),
			ProcessedButNotPersistedIDs: append([]string(nil), recon.ProcessedButNotPersistedIDs...),
			DuplicateExpectedIDs:        append([]string(nil), recon.DuplicateExpectedIDs...),
			ExpectedEntryCount:          recon.ExpectedEntryCount,
			RawDuplicateEntryCount:      recon.RawDuplicateEntryCount,
			RawDuplicateGroups:          convertDuplicateGroups(r.tracker.RawDuplicateGroups()),
		},
		MissingItems:     r.tracker.GetUncapturedItems(),
		FailedDetails:    convertFailedDetailsForSnapshot(r.explicitFailedDetails()),
		PageAudits:       convertPageAuditsForSnapshot(r.pageAudits),
		ValidationReport: cloneValidationReport(r.validationReport),
		Mode:             mode,
		UpdatedAt:        time.Now(),
	})
}

// persistTaskState decides when to write incremental versus fuller snapshots
// and when runtime-only snapshot state can be cleaned up.
func (r *Runner) persistTaskState(status RunnerStatus, message string, force bool, explicitMode crawlexecution.SnapshotMode) {
	// 持久化分两类：运行中的增量快照，以及结束时的收尾快照。
	// 这里不负责业务判断，只根据当前状态决定写哪一份快照。
	manager, err := crawltaskstate.NewManager(r.outputDir, crawltaskstate.ManagerOptions{})
	if err != nil {
		r.emitLog("warn", "task state manager init failed: "+err.Error())
		return
	}

	nowMs := time.Now().UnixMilli()
	if !crawlexecution.ShouldPersistSnapshot(r.lastSnapshotPersistAtMs, nowMs, 1500, force) {
		return
	}

	mode := crawlexecution.ResolveSnapshotMode(string(status), force, explicitMode)
	snapshot := r.buildSnapshot(status, message, mode)
	withBackup := force || mode == crawlexecution.SnapshotModeFull
	if err := manager.SaveSnapshot(snapshot, withBackup); err != nil {
		r.emitLog("warn", "save task snapshot failed: "+err.Error())
		return
	}
	r.lastSnapshotPersistAtMs = nowMs

	finalPlan := crawlexecution.BuildArtifactFinalizationPlan(string(status), len(r.tracker.GetUncapturedItems()), len(r.getRecoverableAudits()))
	if finalPlan.CleanupRuntimeState {
		if err := manager.CleanupRuntimeState(); err != nil {
			r.emitLog("warn", "cleanup task runtime state failed: "+err.Error())
		}
	}
}

// convertFailedDetailsForSnapshot and convertPageAuditsForSnapshot are the
// write-side bridge from live runner structs into the persisted task-state
// schema. Keep them here so snapshot shape changes stay visible in one place.
func convertFailedDetailsForSnapshot(items []FailedDetail) []crawltaskstate.FailedDetailRecord {
	if len(items) == 0 {
		return []crawltaskstate.FailedDetailRecord{}
	}
	result := make([]crawltaskstate.FailedDetailRecord, 0, len(items))
	for _, item := range items {
		recoverable := item.Recoverable
		result = append(result, crawltaskstate.FailedDetailRecord{
			Item:         strings.TrimSpace(item.Item),
			SourceLink:   strings.TrimSpace(item.SourceLink),
			Reason:       strings.TrimSpace(item.Reason),
			Category:     strings.TrimSpace(item.Category),
			RetryCount:   item.RetryCount,
			RetryAdvice:  strings.TrimSpace(item.RetryAdvice),
			Recoverable:  boolPointer(recoverable),
			LastFailedAt: strings.TrimSpace(item.LastFailedAt),
		})
	}
	return result
}

func convertPageAuditsForSnapshot(items []PageAudit) []crawltaskstate.PageAuditRecord {
	if len(items) == 0 {
		return []crawltaskstate.PageAuditRecord{}
	}
	result := make([]crawltaskstate.PageAuditRecord, 0, len(items))
	for _, item := range items {
		result = append(result, crawltaskstate.PageAuditRecord{
			PageNumber:          item.PageNumber,
			URL:                 item.URL,
			ExpectedCount:       item.ExpectedCount,
			ActualCount:         item.ActualCount,
			RetryCount:          item.RetryCount,
			ValidationPassed:    item.ValidationPassed,
			ConfidenceScore:     float64(item.ConfidenceScore),
			Confidence:          item.Confidence,
			Reason:              item.Reason,
			UpdatedAt:           item.UpdatedAt,
			DuplicateEntryCount: item.DuplicateEntryCount,
			DuplicateItemIDs:    append([]string(nil), item.DuplicateItemIDs...),
		})
	}
	return result
}

func convertDuplicateGroups(groups []DuplicateGroup) []crawltaskstate.DuplicateExpectedEntryGroup {
	if len(groups) == 0 {
		return []crawltaskstate.DuplicateExpectedEntryGroup{}
	}
	result := make([]crawltaskstate.DuplicateExpectedEntryGroup, 0, len(groups))
	for _, group := range groups {
		result = append(result, crawltaskstate.DuplicateExpectedEntryGroup{
			ItemID: strings.TrimSpace(group.ItemID),
			Links:  append([]string(nil), group.Links...),
		})
	}
	return result
}
