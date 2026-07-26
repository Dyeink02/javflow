package crawlexecution

import "fmt"

// Shared recovery message/merge helpers live here so queue-gap, page-gap, and
// detail-recovery flows reuse one vocabulary when summarizing unfinished work.
//
// Ownership summary:
// 1) centralize recovery/retry wording used across execution recovery paths
// 2) merge recovery inputs into consistent gap-analysis outcomes
// 3) keep recovery message policy separate from runner orchestration
//
// File map for maintainers:
// 1) shared queue/page/detail recovery DTOs
// 2) recovery message builder helpers
// 3) merge and retry tracker helpers

// Recovery message structs keep retry/recovery wording centralized so review,
// logs, and UI use the same vocabulary.
type QueueGapRecoveryMessages struct {
	LogMessage   string
	StateMessage string
	StateReason  string
}

type PageGapRecoveryMessages struct {
	LogMessage   string
	StateMessage string
	StateReason  string
}

type DetailRecoveryMessages struct {
	LogMessage   string
	StateMessage string
	StateReason  string
}

type PageLockRetryTracker struct {
	LastSampleCount int
	HasLastSample   bool
	StagnantAttempts int
}

type MergePageGapRecoveryInput struct {
	ExpectedCount      int
	CurrentActualCount int
	FetchedActualCount int
	NewLinksCount      int
}

type MergePageGapRecoveryResult struct {
	MergedActualCount int
	RecoveredCount    int
	ValidationPassed  bool
	Reason            string
}

type PageValidationRetryInput struct {
	Tracker           PageLockRetryTracker
	StrictPageLock    bool
	ExpectedCount     *int
	PageNumber        int
	Attempt           int
	MaxAttempts       int
	SampleCount       int
	MergedCount       int
	PreviousBestCount int
}

type PageValidationRetryResult struct {
	Tracker          PageLockRetryTracker
	LogMessage       string
	ShouldStopEarly  bool
	EarlyStopMessage string
}

type PageValidationExhaustedInput struct {
	StrictPageLock bool
	ExpectedCount  *int
	PageNumber     int
	AttemptsUsed   int
	MaxAttempts    int
	BestCount      int
	StoppedEarly   bool
}

func NewPageLockRetryTracker() PageLockRetryTracker {
	return PageLockRetryTracker{}
}

func BuildQueueGapRecoveryMessages(queueGapCount int) QueueGapRecoveryMessages {
	stateMessage := fmt.Sprintf("发现 %d 个入队缺口，正在补加入队。", queueGapCount)
	return QueueGapRecoveryMessages{
		LogMessage:   fmt.Sprintf("检测到 %d 个分页解析结果尚未入队，开始执行全量对账补加入队。", queueGapCount),
		StateMessage: stateMessage,
		StateReason:  "执行入队缺口补齐",
	}
}

func BuildQueueGapRemainingMessage(remainingCount int) string {
	return fmt.Sprintf("入队缺口补齐后仍有 %d 个番号未成功入队，请查看未抓取番号面板。", remainingCount)
}

func BuildPageGapRecoveryMessages(pendingCount int, pass int, totalPasses int) PageGapRecoveryMessages {
	stateMessage := fmt.Sprintf("检测到 %d 个分页缺口，正在进行第 %d 轮补查。", pendingCount, pass)
	return PageGapRecoveryMessages{
		LogMessage:   fmt.Sprintf("检测到 %d 个分页缺口，开始第 %d/%d 轮补查。", pendingCount, pass, totalPasses),
		StateMessage: stateMessage,
		StateReason:  "执行分页缺口补查",
	}
}

func BuildPageGapRemainingMessage(remainingCount int) string {
	return fmt.Sprintf("分页缺口补查结束后仍有 %d 页未达到预期条数，请查看“分页缺口”与“未抓取番号”面板。", remainingCount)
}

func BuildPageGapActiveLabel(pageNumber int, actualCount int, expectedCount int) string {
	return fmt.Sprintf("分页补查第 %d 页（当前 %d/%d）", pageNumber, actualCount, expectedCount)
}

func BuildPageGapActiveStateMessage(pageNumber int) string {
	return fmt.Sprintf("正在补查第 %d 页分页缺口。", pageNumber)
}

// CalculatePageGapRecoveryResult merges one page-gap pass outcome into the next
// iteration plan.
func CalculatePageGapRecoveryResult(input MergePageGapRecoveryInput) MergePageGapRecoveryResult {
	mergedActualCount := minInt(input.ExpectedCount, maxInt(input.CurrentActualCount, input.FetchedActualCount, input.CurrentActualCount+input.NewLinksCount))
	validationPassed := mergedActualCount >= input.ExpectedCount
	recoveredCount := maxInt(0, mergedActualCount-input.CurrentActualCount)

	reason := "分页补查后结果已更新。"
	switch {
	case input.NewLinksCount > 0 && validationPassed:
		reason = fmt.Sprintf("补查成功，新增 %d 条并达到预期条数。", input.NewLinksCount)
	case input.NewLinksCount > 0:
		reason = fmt.Sprintf("补查后新增 %d 条，但仍缺少 %d 条。", input.NewLinksCount, input.ExpectedCount-mergedActualCount)
	case validationPassed:
		reason = "补查后已达到预期条数。"
	default:
		reason = fmt.Sprintf("补查后仍缺少 %d 条。", input.ExpectedCount-mergedActualCount)
	}

	return MergePageGapRecoveryResult{
		MergedActualCount: mergedActualCount,
		RecoveredCount:    recoveredCount,
		ValidationPassed:  validationPassed,
		Reason:            reason,
	}
}

// EvaluatePageValidationRetry is the single retry-budget decision point for
// sparse/invalid page validation passes.
func EvaluatePageValidationRetry(input PageValidationRetryInput) PageValidationRetryResult {
	mergedImproved := input.MergedCount > input.PreviousBestCount
	sampleChanged := !input.Tracker.HasLastSample || input.SampleCount != input.Tracker.LastSampleCount

	nextTracker := PageLockRetryTracker{
		LastSampleCount:  input.SampleCount,
		HasLastSample:    true,
		StagnantAttempts: 0,
	}
	if !mergedImproved && !sampleChanged {
		nextTracker.StagnantAttempts = input.Tracker.StagnantAttempts + 1
	}

	expectedCount := 0
	if input.ExpectedCount != nil {
		expectedCount = *input.ExpectedCount
	}

	if !input.StrictPageLock {
		return PageValidationRetryResult{
			Tracker:         nextTracker,
			LogMessage:      fmt.Sprintf("第 %d 页第 %d/%d 次校验未通过：期望 %d 条，实际 %d 条，已合并 %d 条，准备重试...", input.PageNumber, input.Attempt, input.MaxAttempts, expectedCount, input.SampleCount, input.MergedCount),
			ShouldStopEarly: false,
		}
	}

	logMessage := fmt.Sprintf("第 %d 页第 %d/%d 次页锁定校验未通过：期望至少 %d 条，当前单次 %d 条，合并后 %d 条，继续锁定当前页重抓。", input.PageNumber, input.Attempt, input.MaxAttempts, expectedCount, input.SampleCount, input.MergedCount)
	shouldStopEarly := input.ExpectedCount != nil && input.Attempt < input.MaxAttempts && nextTracker.StagnantAttempts >= 2
	earlyStopMessage := ""
	if shouldStopEarly {
		earlyStopMessage = fmt.Sprintf("第 %d 页页锁定补查连续 2 次无提升：期望至少 %d 条，当前单次稳定在 %d 条，合并后稳定在 %d 条。已提前停损，记录为分页缺口并继续后续抓取。", input.PageNumber, expectedCount, input.SampleCount, input.MergedCount)
	}

	return PageValidationRetryResult{
		Tracker:          nextTracker,
		LogMessage:       logMessage,
		ShouldStopEarly:  shouldStopEarly,
		EarlyStopMessage: earlyStopMessage,
	}
}

func BuildPageValidationExhaustedMessage(input PageValidationExhaustedInput) string {
	if !input.StrictPageLock || input.ExpectedCount == nil {
		return ""
	}

	expectedCount := *input.ExpectedCount
	missingCount := maxInt(expectedCount-input.BestCount, 0)
	if input.StoppedEarly {
		return fmt.Sprintf("第 %d 页页锁定补查已触发停损早停：期望至少 %d 条，%d/%d 次重抓后仍仅 %d 条，仍缺少 %d 条。已记录为分页缺口并继续后续抓取，任务结束后会再次汇总原因。", input.PageNumber, expectedCount, input.AttemptsUsed, input.MaxAttempts, input.BestCount, missingCount)
	}

	return fmt.Sprintf("第 %d 页页锁定校验已达到最大重抓次数：期望至少 %d 条，%d 次重抓合并后仅 %d 条，仍缺少 %d 条。已记录为分页缺口并继续后续抓取，任务结束后会再次补查。", input.PageNumber, expectedCount, input.AttemptsUsed, input.BestCount, missingCount)
}

func BuildDetailRecoveryMessages(missingCount int, pass int, totalPasses int, summary string) DetailRecoveryMessages {
	stateMessage := fmt.Sprintf("检测到 %d 个影片未完成，正在进行第 %d 轮补爬。", missingCount, pass)
	return DetailRecoveryMessages{
		LogMessage:   fmt.Sprintf("检测到 %d 个已入队影片尚未完成，开始第 %d/%d 轮补爬（%s）。", missingCount, pass, totalPasses, summary),
		StateMessage: stateMessage,
		StateReason:  "执行补爬",
	}
}

func BuildDetailBudgetStopMessages(budgetExhaustedCount int) []string {
	messages := []string{"剩余未完成详情页已无可用重试预算，停止继续重复补爬。"}
	if budgetExhaustedCount > 0 {
		messages = append(messages, fmt.Sprintf("约 %d 个未完成影片已达到补爬预算，本轮不再继续重复请求。", budgetExhaustedCount))
	}
	return messages
}

func BuildDetailRecoveryRemainingMessage(remainingCount int) string {
	return fmt.Sprintf("补爬结束后仍有 %d 个影片未完成，请查看日志确认这些链接是否持续请求失败。", remainingCount)
}

func maxInt(values ...int) int {
	if len(values) == 0 {
		return 0
	}
	best := values[0]
	for _, value := range values[1:] {
		if value > best {
			best = value
		}
	}
	return best
}

func minInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
}
