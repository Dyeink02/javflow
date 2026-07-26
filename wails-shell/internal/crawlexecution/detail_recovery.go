package crawlexecution

import "fmt"

// Detail-recovery decisions own the retry-budget pass for detail-page failures
// after the main crawl loop has already queued work.
//
// Ownership summary:
// 1) decide whether a post-loop detail-recovery pass should run
// 2) centralize retry-budget exhaustion and pass start/end decisions
// 3) keep detail-recovery pass policy separate from runner queue execution
//
// File map for maintainers:
// 1) detail-recovery budget and pass DTOs
// 2) pass start/end decision helpers
// 3) retry budget summary and stop-recovery helpers

// Detail-recovery structs keep the post-main-loop retry-budget pass explicit.
type DetailRecoveryBudgetEntry struct {
	AttemptsUsed int `json:"attemptsUsed"`
	Budget       int `json:"budget"`
}

type DetailRecoveryPassStartDecision struct {
	Status        string   `json:"status"`
	ShouldRunPass bool     `json:"shouldRunPass"`
	StopRecovery  bool     `json:"stopRecovery"`
	LogMessages   []string `json:"logMessages,omitempty"`
}

type DetailRecoveryPassEndDecision struct {
	Status       string `json:"status"`
	StopRecovery bool   `json:"stopRecovery"`
	LogMessage   string `json:"logMessage,omitempty"`
}

func CountBudgetExhaustedDetailEntries(entries []DetailRecoveryBudgetEntry) int {
	count := 0
	for _, entry := range entries {
		if entry.AttemptsUsed >= maxInt(entry.Budget, 0) {
			count += 1
		}
	}
	return count
}

// ResolveDetailRecoveryPassStart decides whether one detail-recovery pass should
// begin and how it should be described.
func ResolveDetailRecoveryPassStart(pass int, missingCount int, recoverableCount int, budgetExhaustedCount int) DetailRecoveryPassStartDecision {
	if missingCount <= 0 {
		logMessages := []string{}
		if pass > 1 {
			logMessages = append(logMessages, "补爬校验通过，所有已入队影片均已处理完成。")
		}
		return DetailRecoveryPassStartDecision{
			Status:        "completed",
			ShouldRunPass: false,
			StopRecovery:  true,
			LogMessages:   logMessages,
		}
	}

	if recoverableCount <= 0 {
		return DetailRecoveryPassStartDecision{
			Status:        "budget_exhausted",
			ShouldRunPass: false,
			StopRecovery:  true,
			LogMessages:   BuildDetailBudgetStopMessages(budgetExhaustedCount),
		}
	}

	return DetailRecoveryPassStartDecision{
		Status:        "continue",
		ShouldRunPass: true,
		StopRecovery:  false,
	}
}

// ResolveDetailRecoveryPassEnd summarizes one pass and decides whether another
// pass is worthwhile.
func ResolveDetailRecoveryPassEnd(pass int, previousMissingCount int, remainingCount int, nextRecoverableCount int) DetailRecoveryPassEndDecision {
	if remainingCount <= 0 {
		return DetailRecoveryPassEndDecision{
			Status:       "completed",
			StopRecovery: true,
		}
	}

	if remainingCount >= previousMissingCount {
		if nextRecoverableCount <= 0 {
			return DetailRecoveryPassEndDecision{
				Status:       "budget_exhausted",
				StopRecovery: true,
				LogMessage:   "当前未完成影片已全部耗尽重试预算，停止继续补爬。",
			}
		}

		return DetailRecoveryPassEndDecision{
			Status:       "high_priority_retry",
			StopRecovery: false,
			LogMessage:   fmt.Sprintf("第 %d 轮补爬后未完成数量没有下降，但仍存在可恢复失败项，下一轮将仅重试高优先级失败详情页。", pass),
		}
	}

	return DetailRecoveryPassEndDecision{
		Status:       "continue",
		StopRecovery: false,
	}
}
