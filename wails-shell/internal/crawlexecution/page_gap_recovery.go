package crawlexecution

import "fmt"

// Page-gap recovery decisions own the second-pass planning used after index
// traversal when expected counts and persisted results still diverge.
//
// Ownership summary:
// 1) decide whether page-gap recovery passes should run or stop
// 2) centralize page-gap follow-up actions and messages
// 3) keep page-gap recovery policy separate from index fetch/validation execution
//
// File map for maintainers:
// 1) page-gap pass decision DTOs
// 2) follow-up action and stop/continue helpers
// 3) page-level recovery message builders

type PageGapPassStartDecision struct {
	ShouldRunPass bool   `json:"shouldRunPass"`
	StopRecovery  bool   `json:"stopRecovery"`
	LogMessage    string `json:"logMessage,omitempty"`
}

type PageGapPassEndDecision struct {
	Status       string `json:"status"`
	StopRecovery bool   `json:"stopRecovery"`
	LogMessage   string `json:"logMessage,omitempty"`
}

type PageGapAuditFollowUpDecision struct {
	Action     string `json:"action"`
	LogMessage string `json:"logMessage"`
}

func ResolvePageGapPassStart(pendingCount int, pass int) PageGapPassStartDecision {
	if pendingCount > 0 {
		return PageGapPassStartDecision{
			ShouldRunPass: true,
			StopRecovery:  false,
		}
	}

	logMessage := ""
	if pass > 1 {
		logMessage = "分页缺口补查完成，所有已知页面均达到预期条数。"
	}

	return PageGapPassStartDecision{
		ShouldRunPass: false,
		StopRecovery:  true,
		LogMessage:    logMessage,
	}
}

func ResolvePageGapAuditFollowUp(pageNumber int, expectedCount int, mergedActualCount int, newLinksCount int, validationPassed bool) PageGapAuditFollowUpDecision {
	switch {
	case newLinksCount > 0:
		return PageGapAuditFollowUpDecision{
			Action:     "enqueue_new_links",
			LogMessage: fmt.Sprintf("第 %d 页补查新增 %d 个影片链接，已加入详情队列。", pageNumber, newLinksCount),
		}
	case validationPassed:
		return PageGapAuditFollowUpDecision{
			Action:     "validated",
			LogMessage: fmt.Sprintf("第 %d 页分页缺口补查通过。", pageNumber),
		}
	default:
		return PageGapAuditFollowUpDecision{
			Action:     "incomplete",
			LogMessage: fmt.Sprintf("第 %d 页补查后仍为 %d/%d。", pageNumber, mergedActualCount, expectedCount),
		}
	}
}

func ResolvePageGapPassEnd(remainingCount int, recoveredCount int) PageGapPassEndDecision {
	switch {
	case remainingCount <= 0:
		return PageGapPassEndDecision{
			Status:       "completed",
			StopRecovery: true,
			LogMessage:   "分页缺口补查已完成，当前所有目标页面均达到预期条数。",
		}
	case recoveredCount <= 0:
		return PageGapPassEndDecision{
			Status:       "stagnant",
			StopRecovery: true,
			LogMessage:   "本轮分页缺口补查未提升结果，停止继续重复补查，避免影响抓取体验。",
		}
	default:
		return PageGapPassEndDecision{
			Status:       "continue",
			StopRecovery: false,
		}
	}
}
