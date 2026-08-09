// Package crawlrunner owns the current Go-native crawler runtime.
//
// This file owns end-of-run diagnostic assembly, quality-summary inputs, and
// unfinished/duplicate reporting support for the runner.
//
// Ownership summary:
// 1) assemble immutable end-of-run diagnostic snapshots
// 2) shape quality/report inputs from live runner state
// 3) keep summary/report derivation separate from live execution flow
//
// File map for maintainers:
// 1) diagnostic snapshot clone helpers
// 2) end-of-run summary DTO assembly
// 3) unfinished/duplicate/validation report shaping
package crawlrunner

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"javflow/internal/common"
	"javflow/internal/contracts/crawlartifact"
	"javflow/internal/crawlexecution"
	"javflow/internal/crawloutput"
	"javflow/internal/crawltaskstate"
)

// If final summary output looks wrong, inspect this file before changing the
// final-state or UI projection code.

// cloneValidationReport keeps the diagnostic summary detached from live state.
func cloneValidationReport(report *crawltaskstate.ResultValidationReport) *crawltaskstate.ResultValidationReport {
	if report == nil {
		return nil
	}

	next := *report
	next.MissingItems = append([]string(nil), report.MissingItems...)
	next.ExpectedButNotQueuedItems = append([]string(nil), report.ExpectedButNotQueuedItems...)
	next.ProcessedButNotPersistedItems = append([]string(nil), report.ProcessedButNotPersistedItems...)
	next.LowConfidencePages = append([]int(nil), report.LowConfidencePages...)
	return &next
}

// cloneFailedDetails keeps failure diagnostics immutable after collection.
func cloneFailedDetails(items []FailedDetail) []FailedDetail {
	if len(items) == 0 {
		return []FailedDetail{}
	}

	result := make([]FailedDetail, len(items))
	copy(result, items)
	return result
}

// boolPointer is a small helper for optional diagnostic flags.
func boolPointer(value bool) *bool {
	next := value
	return &next
}

// isFinalRunnerStatus tells diagnostics whether a status is terminal.
func isFinalRunnerStatus(status RunnerStatus) bool {
	switch status {
	case StatusCompleted, StatusIncomplete, StatusStopped, StatusError:
		return true
	default:
		return false
	}
}

// isLargeTaskMode is used for recovery budget and summary heuristics.
func (r *Runner) isLargeTaskMode() bool {
	return r.config.Limit >= 180 || r.config.TotalPages >= 8
}

// restoreFailedDetails rebuilds the runner's failure map from persisted state.
func (r *Runner) restoreFailedDetails(items []crawltaskstate.FailedDetailRecord) {
	if len(items) == 0 {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.failedDetailMap == nil {
		r.failedDetailMap = map[string]FailedDetail{}
	}

	for _, item := range items {
		link := strings.TrimSpace(item.SourceLink)
		if link == "" {
			continue
		}

		recoverable := true
		if item.Recoverable != nil {
			recoverable = *item.Recoverable
		}

		r.failedDetailMap[link] = FailedDetail{
			Item:         strings.TrimSpace(item.Item),
			SourceLink:   link,
			Reason:       strings.TrimSpace(item.Reason),
			Category:     strings.TrimSpace(item.Category),
			RetryCount:   item.RetryCount,
			RetryAdvice:  strings.TrimSpace(item.RetryAdvice),
			Recoverable:  recoverable,
			LastFailedAt: strings.TrimSpace(item.LastFailedAt),
		}
	}
}

// restorePersistedOutputState rehydrates the tracker from disk-backed output.
func (r *Runner) restorePersistedOutputState() {
	records, err := r.loadOutputRecords()
	if err != nil || len(records) == 0 {
		return
	}

	// `filmData.json` is the durable truth for already persisted films. During
	// resume we rebuild tracker state from it so dedupe, filtered-item counters,
	// and second-pass reconciliation remain correct even if the last runtime
	// snapshot was missing or stale.
	for _, record := range records {
		link := strings.TrimSpace(record.SourceLink)
		filmID := extractFilmIDFromLink(link)
		r.tracker.MarkPersisted(link, filmID)
		if itemID := getDetailItemIDFromLink(link); itemID != "" {
			r.tracker.MarkPersisted("", itemID)
		}
		if record.FilteredByActressCount {
			r.recordActressCountFiltered(link)
		}
		if record.FilteredByFilmCode {
			r.recordFilmCodeFiltered(link)
		}
	}

	if len(records) > r.filmCount {
		r.filmCount = len(records)
	}
}

// recordDetailFailure stores the latest failure classification for one link.
func (r *Runner) recordDetailFailure(link string, reason string) {
	trimmedLink := strings.TrimSpace(link)
	if trimmedLink == "" {
		return
	}

	policy := crawlexecution.ClassifyDetailFailure(reason)
	recoverable := crawlexecution.GetDetailRecoveryBudget(policy, r.isLargeTaskMode()) > 0
	itemID := getDetailItemIDFromLink(trimmedLink)
	if itemID == "" {
		itemID = extractFilmIDFromLink(trimmedLink)
	}
	if itemID == "" {
		itemID = normalizeSourceLink(trimmedLink)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.failedDetailMap == nil {
		r.failedDetailMap = map[string]FailedDetail{}
	}

	next := r.failedDetailMap[trimmedLink]
	next.Item = firstNonEmptyNonZero(itemID, next.Item)
	next.SourceLink = trimmedLink
	next.Reason = strings.TrimSpace(reason)
	next.Category = policy.Label
	next.RetryCount = maxInt(next.RetryCount+1, 1)
	next.RetryAdvice = policy.Advice
	next.Recoverable = recoverable
	next.LastFailedAt = time.Now().Format(time.RFC3339)
	r.failedDetailMap[trimmedLink] = next
}

// clearDetailFailure removes a link from the explicit failure map once it is
// persisted successfully.
func (r *Runner) clearDetailFailure(link string) {
	trimmedLink := strings.TrimSpace(link)
	if trimmedLink == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.failedDetailMap, trimmedLink)
}

// incrementDetailRecoveryAttempt counts recovery retries for one link.
func (r *Runner) incrementDetailRecoveryAttempt(link string) {
	trimmedLink := strings.TrimSpace(link)
	if trimmedLink == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.detailRecoveryAttemptMap == nil {
		r.detailRecoveryAttemptMap = map[string]int{}
	}
	r.detailRecoveryAttemptMap[trimmedLink] = r.detailRecoveryAttemptMap[trimmedLink] + 1
}

// detailRecoveryAttemptCount reads the tracked recovery attempt count.
func (r *Runner) detailRecoveryAttemptCount(link string) int {
	trimmedLink := strings.TrimSpace(link)
	if trimmedLink == "" {
		return 0
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	return r.detailRecoveryAttemptMap[trimmedLink]
}

// explicitFailedDetails returns the operator-authored failure set in a stable
// order for reporting.
func (r *Runner) explicitFailedDetails() []FailedDetail {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.failedDetailMap) == 0 {
		return []FailedDetail{}
	}

	result := make([]FailedDetail, 0, len(r.failedDetailMap))
	for _, detail := range r.failedDetailMap {
		result = append(result, detail)
	}
	sort.Slice(result, func(i int, j int) bool {
		left := strings.ToLower(firstNonEmptyNonZero(result[i].Item, result[i].SourceLink))
		right := strings.ToLower(firstNonEmptyNonZero(result[j].Item, result[j].SourceLink))
		if left == right {
			return strings.ToLower(result[i].Reason) < strings.ToLower(result[j].Reason)
		}
		return left < right
	})
	return result
}

// buildInferredFailedDetails derives diagnostic-only failures from tracker
// reconciliation gaps.
func (r *Runner) buildInferredFailedDetails() []FailedDetail {
	recon := r.tracker.BuildReconciliation()
	expectedButNotQueued := makeStringSet(recon.ExpectedButNotQueuedIDs)
	processedNotPersisted := makeStringSet(recon.ProcessedButNotPersistedIDs)

	explicitItems := map[string]struct{}{}
	for _, detail := range r.explicitFailedDetails() {
		item := diagnosticItemID(detail.Item, detail.SourceLink)
		if item != "" {
			explicitItems[item] = struct{}{}
		}
	}

	unfinishedItems := r.tracker.GetUncapturedItems()
	result := make([]FailedDetail, 0, len(unfinishedItems))
	for _, item := range unfinishedItems {
		if _, exists := explicitItems[item]; exists {
			continue
		}

		// Inferred failed details are generated only from reconciliation gaps.
		// They are review diagnostics, not replacement runtime errors, and should
		// therefore remain derived data instead of being written back into the
		// explicit failure map.
		detail := FailedDetail{
			Item:        item,
			SourceLink:  strings.TrimSpace(r.tracker.GetExpectedLink(item)),
			Recoverable: true,
		}

		switch {
		case processedNotPersisted[item]:
			detail.Category = "已处理未落盘"
			detail.Reason = "该番号详情已抓取，但结果未成功写入输出文件。"
			detail.RetryAdvice = "建议优先检查输出目录权限或写盘错误后再重试。"
		case r.tracker.IsQueued(item):
			detail.Category = "排队未执行"
			detail.Reason = "该番号已进入详情队列，但在任务结束前尚未处理完成。"
			detail.RetryAdvice = "建议重新爬取，让队列继续补全剩余项目。"
		case expectedButNotQueued[item]:
			detail.Category = "入队缺口"
			detail.Reason = "索引页已识别到该番号，但未成功进入详情队列，请优先复查分页缺口与站点条数。"
			detail.RetryAdvice = "建议结合分页缺口提示，重新抓取缺失页后再补爬。"
		default:
			detail.Category = "未完成"
			detail.Reason = "任务结束时该番号仍未完成，请结合分页缺口与失败详情继续补抓。"
			detail.RetryAdvice = "建议使用重新爬取，仅补抓未完成番号。"
		}

		result = append(result, detail)
	}

	sort.Slice(result, func(i int, j int) bool {
		return strings.ToLower(result[i].Item) < strings.ToLower(result[j].Item)
	})
	return result
}

func (r *Runner) failedDetails(includeInferred bool) []FailedDetail {
	explicit := r.explicitFailedDetails()
	if !includeInferred {
		return explicit
	}

	result := append([]FailedDetail{}, explicit...)
	result = append(result, r.buildInferredFailedDetails()...)
	return result
}

// pageGapFailureDetails turns an unresolved index-page shortfall into a
// visible failure entry. These entries are page-level diagnostics, not film
// IDs, so they are deliberately kept out of failedItemIDs() to avoid
// subtracting a guessed code from completion accounting.
func (r *Runner) pageGapFailureDetails() ([]FailedDetail, int) {
	items := make([]FailedDetail, 0)
	missingTotal := 0
	for _, audit := range r.pageAudits {
		if audit.ExpectedCount == nil {
			continue
		}
		missing := common.MaxInt(*audit.ExpectedCount-audit.ActualCount, 0)
		if missing == 0 {
			continue
		}
		missingTotal += missing
		duplicateHint := ""
		if len(audit.DuplicateItemIDs) > 0 {
			duplicateHint = "；本页重复番号：" + strings.Join(audit.DuplicateItemIDs, "、")
		}
		items = append(items, FailedDetail{
			Item:         fmt.Sprintf("第 %d 页缺口（%d 条）", audit.PageNumber, missing),
			SourceLink:   strings.TrimSpace(audit.URL),
			Reason:       fmt.Sprintf("预计 %d 条，实际 %d 条%s。", *audit.ExpectedCount, audit.ActualCount, duplicateHint),
			Category:     "分页缺口",
			RetryCount:   common.MaxInt(audit.RetryCount, 1),
			RetryAdvice:  "建议重新爬取，优先复查该页是否被拦截或重复展示。",
			Recoverable:  true,
			LastFailedAt: strings.TrimSpace(audit.UpdatedAt),
		})
	}
	return items, missingTotal
}

// reviewFailedDetails merges item-level failures and page-level gaps for the
// review panel. The displayed total uses missing film slots for page gaps,
// while the visible list stays compact at one entry per affected page.
func (r *Runner) reviewFailedDetails(includeInferred bool) ([]FailedDetail, int) {
	details := r.failedDetails(includeInferred)
	pageGapDetails, pageGapTotal := r.pageGapFailureDetails()
	details = append(details, pageGapDetails...)
	total := len(r.failedItemIDs(includeInferred)) + pageGapTotal
	if total < len(details) {
		total = len(details)
	}
	return details, total
}

// reviewDuplicateItems joins tracker-level duplicates with duplicate evidence
// found before the parser removes repeated links from the detail queue.
func (r *Runner) reviewDuplicateItems() []string {
	seen := map[string]struct{}{}
	for _, item := range r.tracker.DuplicateItemIDs() {
		if normalized := strings.TrimSpace(item); normalized != "" {
			seen[normalized] = struct{}{}
		}
	}
	for _, audit := range r.pageAudits {
		for _, item := range audit.DuplicateItemIDs {
			if normalized := strings.TrimSpace(item); normalized != "" {
				seen[normalized] = struct{}{}
			}
		}
	}
	items := make([]string, 0, len(seen))
	for item := range seen {
		items = append(items, item)
	}
	sort.Strings(items)
	return items
}

// reviewDuplicateEntryCount combines two non-overlapping sources of duplicate
// evidence: repeated links discovered inside one index page and repeated film
// IDs discovered across different pages. A retry of the same page is excluded
// before it reaches Tracker, so it cannot inflate this count.
func (r *Runner) reviewDuplicateEntryCount() int {
	total := r.tracker.RawDuplicateEntryCount()
	for _, audit := range r.pageAudits {
		total += common.MaxInt(audit.DuplicateEntryCount, 0)
	}
	return total
}

// expectedReviewTotal is the denominator used by the visible completion
// formula. A configured limit is the user's requested total; otherwise page
// expectations are more trustworthy than the de-duplicated detail queue.
func (r *Runner) expectedReviewTotal() int {
	if r.config.Limit > 0 {
		return r.config.Limit
	}

	total := 0
	for _, audit := range r.pageAudits {
		if audit.ExpectedCount != nil && *audit.ExpectedCount > 0 {
			total += *audit.ExpectedCount
			continue
		}
		total += common.MaxInt(audit.ActualCount, 0)
	}
	if total > 0 {
		return total
	}

	recon := r.tracker.BuildReconciliation()
	return common.MaxInt(recon.ExpectedEntryCount, common.MaxInt(r.filmsQueued, r.filmCount))
}

func normalizePageDuplicateIDs(items []string) []string {
	seen := map[string]struct{}{}
	for _, item := range items {
		normalized := strings.TrimSpace(item)
		if normalized != "" {
			seen[normalized] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for item := range seen {
		result = append(result, item)
	}
	sort.Strings(result)
	return result
}

// failedItemIDs converts failure details into one stable item-level set for
// completion math. A detail page may have several error records over retries,
// but it must subtract only once from the final total.
func (r *Runner) failedItemIDs(includeInferred bool) []string {
	seen := map[string]struct{}{}
	for _, detail := range r.failedDetails(includeInferred) {
		itemID := diagnosticItemID(detail.Item, detail.SourceLink)
		if itemID != "" {
			seen[itemID] = struct{}{}
		}
	}
	items := make([]string, 0, len(seen))
	for itemID := range seen {
		items = append(items, itemID)
	}
	sort.Strings(items)
	return items
}

// diagnosticItemID normalizes restored and live failure records to the same
// identity used by Tracker. Older snapshots may contain a lowercase item or a
// title instead of a canonical code; extracting the film code first prevents
// those records from bypassing completed-item exclusion or being counted twice.
func diagnosticItemID(values ...string) string {
	for _, value := range values {
		if id := extractFilmIDFromLink(value); id != "" {
			return id
		}
	}
	for _, value := range values {
		if id := getDetailItemIDFromLink(value); id != "" {
			return strings.TrimSpace(id)
		}
	}
	return ""
}

type completionBreakdown struct {
	// Total is the original index-entry count, so it intentionally includes
	// repeated links discovered across pages.
	Total int
	// Filtered and Failed are unique item-level counts. Duplicates is the
	// number of extra raw entries beyond the first occurrence of each item.
	// Keeping these units explicit prevents future callers from subtracting a
	// list length that represents a different population.
	Filtered   int
	Duplicates int
	Failed     int
}

// completionBreakdown is the canonical accounting view shared by live stats,
// terminal reports, and crawl-profile metadata.
func (r *Runner) completionBreakdown(includeInferredFailures bool) completionBreakdown {
	return completionBreakdown{
		Total:      r.expectedReviewTotal(),
		Filtered:   len(r.filteredItems()),
		Duplicates: r.reviewDuplicateEntryCount(),
		Failed:     reviewFailureCount(r, includeInferredFailures),
	}
}

func reviewFailureCount(r *Runner, includeInferredFailures bool) int {
	_, total := r.reviewFailedDetails(includeInferredFailures)
	return total
}

func (r *Runner) effectiveCompletedCount(status RunnerStatus) int {
	final := isFinalRunnerStatus(status)
	breakdown := r.completionBreakdown(final)
	return calculateCompletedCount(status, breakdown, r.completedItemIDsFor(final))
}

// calculateCompletedCount is the single source of truth for the number shown
// in live/terminal runner stats and persisted summaries. Terminal states use
// the product rule requested by the UI; active states use the durable item IDs
// already written so the counter does not claim work that is still in flight.
func calculateCompletedCount(status RunnerStatus, breakdown completionBreakdown, completedIDs []string) int {
	if isFinalRunnerStatus(status) && breakdown.Total > 0 {
		return common.MaxInt(breakdown.Total-breakdown.Filtered-breakdown.Duplicates-breakdown.Failed, 0)
	}
	return len(completedIDs)
}

// missingDetailLinksForRecovery returns deduplicated unresolved links for
// later recovery phases.
func (r *Runner) missingDetailLinksForRecovery() []string {
	missing := r.tracker.GetMissingDetailLinks()
	result := make([]string, 0, len(missing))
	seen := map[string]struct{}{}
	for _, link := range missing {
		trimmed := strings.TrimSpace(link)
		if trimmed == "" {
			continue
		}
		normalized := normalizeDetailLink(trimmed)
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, trimmed)
	}
	sort.Strings(result)
	return result
}

// getRecoverableMissingDetailLinks applies recovery-budget rules to the
// unresolved link set.
func (r *Runner) getRecoverableMissingDetailLinks(links []string) []string {
	candidates := make([]crawlexecution.DetailRecoveryCandidate, 0, len(links))
	for _, link := range links {
		record, hasRecord := r.failedDetailRecord(link)
		candidates = append(candidates, crawlexecution.DetailRecoveryCandidate{
			Link:             link,
			Reason:           record.Reason,
			RetryCount:       record.RetryCount,
			RecoveryAttempts: r.detailRecoveryAttemptCount(link),
			HasFailureRecord: hasRecord,
		})
	}
	return crawlexecution.GetRecoverableMissingDetailLinks(candidates, r.isLargeTaskMode())
}

// countBudgetExhaustedDetails counts links that have spent their recovery
// budget.
func (r *Runner) countBudgetExhaustedDetails(links []string) int {
	entries := make([]crawlexecution.DetailRecoveryBudgetEntry, 0, len(links))
	for _, link := range links {
		record, _ := r.failedDetailRecord(link)
		policy := crawlexecution.ClassifyDetailFailure(record.Reason)
		entries = append(entries, crawlexecution.DetailRecoveryBudgetEntry{
			AttemptsUsed: r.detailRecoveryAttemptCount(link),
			Budget:       crawlexecution.GetDetailRecoveryBudget(policy, r.isLargeTaskMode()),
		})
	}
	return crawlexecution.CountBudgetExhaustedDetailEntries(entries)
}

// buildRecoveryCategorySummary groups unresolved links by recovery reason for
// operator review.
func (r *Runner) buildRecoveryCategorySummary(links []string) string {
	entries := make([]crawlexecution.DetailRecoverySummaryEntry, 0, len(links))
	for _, link := range links {
		record, _ := r.failedDetailRecord(link)
		entries = append(entries, crawlexecution.DetailRecoverySummaryEntry{Reason: record.Reason})
	}
	return crawlexecution.BuildRecoveryCategorySummary(entries)
}

func (r *Runner) failedDetailRecord(link string) (FailedDetail, bool) {
	trimmedLink := strings.TrimSpace(link)
	if trimmedLink == "" {
		return FailedDetail{}, false
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	record, ok := r.failedDetailMap[trimmedLink]
	return record, ok
}

func (r *Runner) emitFailedDetailSummaryLog() {
	failedDetails, failedDetailsTotal := r.reviewFailedDetails(true)
	if len(failedDetails) == 0 {
		return
	}

	summary := map[string]int{}
	order := []string{}
	for _, detail := range failedDetails {
		category := strings.TrimSpace(detail.Category)
		if category == "" {
			category = "未完成"
		}
		if _, exists := summary[category]; !exists {
			order = append(order, category)
		}
		summary[category]++
	}

	parts := make([]string, 0, len(order))
	for _, category := range order {
		parts = append(parts, fmt.Sprintf("%s %d 条", category, summary[category]))
	}
	r.emitLog("info", fmt.Sprintf("失败原因汇总：%s。失败总数 %d 条，详细条目已写入%s。", strings.Join(parts, "，"), failedDetailsTotal, crawlartifact.DefaultUnfinishedTxt))
}

func (r *Runner) stateDetails(status RunnerStatus) map[string]any {
	includeInferred := isFinalRunnerStatus(status) || status == StatusStopping
	duplicateItems := r.reviewDuplicateItems()
	unfinishedItems := r.tracker.GetUncapturedItems()
	pageGapItems := r.buildPageGapLines()
	filteredItems := r.filteredItems()
	completedItems := r.completedItemIDsFor(includeInferred)
	failedDetails, failedDetailsTotal := r.reviewFailedDetails(includeInferred)
	breakdown := r.completionBreakdown(includeInferred)
	completedCount := r.effectiveCompletedCount(status)

	// Keep these keys aligned with crawlreview/crawluistate services and the
	// frontend state controller. They are the structured replacement for the old
	// JS-side "read logs and guess state" flow.
	return map[string]any{
		"duplicateItems":       duplicateItems,
		"duplicateItemsTotal":  len(duplicateItems),
		"duplicateCount":       breakdown.Duplicates,
		"unfinishedItems":      unfinishedItems,
		"unfinishedItemsTotal": len(unfinishedItems),
		"missingItems":         unfinishedItems,
		"missingItemsTotal":    len(unfinishedItems),
		"pageGapItems":         pageGapItems,
		"pageGapItemsTotal":    len(pageGapItems),
		"filteredItems":        filteredItems,
		"filteredItemsTotal":   len(filteredItems),
		"filteredItemIds":      filteredItems,
		"filteredCount":        breakdown.Filtered,
		"completedItems":       completedItems,
		"completedItemsTotal":  completedCount,
		"completedItemIds":     completedItems,
		"failedDetails":        cloneFailedDetails(failedDetails),
		"failedDetailsTotal":   failedDetailsTotal,
		"failedCount":          breakdown.Failed,
		"totalItems":           breakdown.Total,
		"completedCount":       completedCount,
	}
}

func (r *Runner) buildValidationReport() (*crawltaskstate.ResultValidationReport, error) {
	records, err := r.loadOutputRecords()
	if err != nil {
		return nil, err
	}

	uniqueKeys := map[string]struct{}{}
	uniqueMagnets := map[string]struct{}{}
	duplicateCount := 0
	invalidRecordCount := 0

	for _, record := range records {
		key := record.IdentityKey()
		if key == "" {
			invalidRecordCount++
		} else {
			if _, exists := uniqueKeys[key]; exists {
				duplicateCount++
			} else {
				uniqueKeys[key] = struct{}{}
			}
		}

		if strings.TrimSpace(record.Title) == "" {
			invalidRecordCount++
		}

		for _, link := range splitMagnetLines(record.Magnet) {
			uniqueMagnets[link] = struct{}{}
		}
		for _, magnet := range record.MagnetLinks {
			link := strings.TrimSpace(magnet.Link)
			if link != "" {
				uniqueMagnets[link] = struct{}{}
			}
		}
	}

	recon := r.tracker.BuildReconciliation()
	lowConfidencePages := make([]int, 0)
	for _, audit := range r.pageAudits {
		if audit.ConfidenceScore < 60 {
			lowConfidencePages = append(lowConfidencePages, audit.PageNumber)
		}
	}

	_, failedDetailsTotal := r.reviewFailedDetails(true)
	passed := duplicateCount == 0 &&
		invalidRecordCount == 0 &&
		len(recon.ExpectedButNotPersistedIDs) == 0 &&
		len(recon.ExpectedButNotQueuedIDs) == 0 &&
		len(recon.ProcessedButNotPersistedIDs) == 0 &&
		len(lowConfidencePages) == 0 &&
		failedDetailsTotal == 0

	summary := "结果二次校验通过：输出结果内部一致性正常；目标是否补齐请以最终任务汇总为准。"
	if !passed {
		summary = fmt.Sprintf(
			"结果二次校验失败：重复 %d 条，异常 %d 条，缺失 %d 条，入队缺口 %d 条，已处理未落盘 %d 条，低可信分页 %d 页，失败详情 %d 条。",
			duplicateCount,
			invalidRecordCount,
			len(recon.ExpectedButNotPersistedIDs),
			len(recon.ExpectedButNotQueuedIDs),
			len(recon.ProcessedButNotPersistedIDs),
			len(lowConfidencePages),
			failedDetailsTotal,
		)
	}

	return &crawltaskstate.ResultValidationReport{
		GeneratedAt:                   time.Now().Format(time.RFC3339),
		TotalRecords:                  len(records),
		UniqueRecords:                 len(uniqueKeys),
		DuplicateCount:                duplicateCount,
		InvalidRecordCount:            invalidRecordCount,
		ExpectedItemCount:             len(recon.ExpectedIDs),
		PersistedItemCount:            len(recon.PersistedIDs),
		MissingFromQueueCount:         len(recon.ExpectedButNotPersistedIDs),
		ExpectedButNotQueuedCount:     len(recon.ExpectedButNotQueuedIDs),
		ProcessedButNotPersistedCount: len(recon.ProcessedButNotPersistedIDs),
		UniqueMagnetCount:             len(uniqueMagnets),
		LowConfidencePageCount:        len(lowConfidencePages),
		MissingItems:                  append([]string(nil), recon.ExpectedButNotPersistedIDs...),
		ExpectedButNotQueuedItems:     append([]string(nil), recon.ExpectedButNotQueuedIDs...),
		ProcessedButNotPersistedItems: append([]string(nil), recon.ProcessedButNotPersistedIDs...),
		LowConfidencePages:            append([]int(nil), lowConfidencePages...),
		Passed:                        passed,
		Summary:                       summary,
	}, nil
}

func (r *Runner) saveValidationReport(report *crawltaskstate.ResultValidationReport) error {
	if report == nil {
		return nil
	}

	manager, err := crawltaskstate.NewManager(r.outputDir, crawltaskstate.ManagerOptions{})
	if err != nil {
		return err
	}
	return manager.SaveValidationReport(*report)
}

func (r *Runner) loadOutputRecords() ([]crawloutput.FilmData, error) {
	filePath := crawlartifact.ResolveCrawlOutputPaths(r.outputDir).FilmDataPath
	payload, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []crawloutput.FilmData{}, nil
		}
		return nil, err
	}

	var records []crawloutput.FilmData
	if err := json.Unmarshal(payload, &records); err != nil {
		return nil, err
	}
	return records, nil
}

func splitMagnetLines(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	result := make([]string, 0)
	for _, line := range strings.Split(value, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func makeStringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			result[trimmed] = true
		}
	}
	return result
}

func firstNonEmptyNonZero(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
