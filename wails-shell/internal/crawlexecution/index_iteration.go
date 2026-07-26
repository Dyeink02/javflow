package crawlexecution

import (
	"fmt"
	"strings"
)

// index_iteration.go owns next-step planning for the steady-state index-page
// discovery loop.
//
// Ownership summary:
// 1) plan the next steady-state index-loop step after one successful/error iteration
// 2) centralize sparse-page warnings, delay messaging, and next-page selection
// 3) keep loop-step planning separate from lower-level validation helpers
//
// File map for maintainers:
// 1) success/error loop plan DTOs
// 2) next-page, delay, and stop decision helpers
// 3) sparse-page warning and iteration log builders

type IndexLoopSuccessPlanInput struct {
	CurrentPage          int  `json:"currentPage"`
	NextPageDelayMs      int  `json:"nextPageDelayMs"`
	ShouldStopIndexing   bool `json:"shouldStopIndexing"`
	IsStopping           bool `json:"isStopping"`
	TargetTotalPages     int  `json:"targetTotalPages"`
	ExpectedItemsPerPage *int `json:"expectedItemsPerPage"`
	IsLastTargetPage     bool `json:"isLastTargetPage"`
	LinksCount           int  `json:"linksCount"`
}

type IndexLoopSuccessPlan struct {
	ShouldPrefetchNextPage bool   `json:"shouldPrefetchNextPage"`
	NextPrefetchPageNumber int    `json:"nextPrefetchPageNumber"`
	ShouldWarnSparsePage   bool   `json:"shouldWarnSparsePage"`
	SparseWarningMessage   string `json:"sparseWarningMessage,omitempty"`
	NextPageNumber         int    `json:"nextPageNumber"`
	StateReason            string `json:"stateReason"`
	StateMessage           string `json:"stateMessage"`
	DelayLogMessage        string `json:"delayLogMessage"`
}

type IndexLoopErrorPlan struct {
	StateReason     string `json:"stateReason"`
	StateMessage    string `json:"stateMessage"`
	DelayMs         int    `json:"delayMs"`
	RetryLogMessage string `json:"retryLogMessage,omitempty"`
}

func IsRetryableIndexNetworkError(message string) bool {
	normalized := strings.ToUpper(strings.TrimSpace(message))
	return strings.Contains(normalized, "ECONNRESET") ||
		strings.Contains(normalized, "ETIMEDOUT") ||
		strings.Contains(normalized, "ENOTFOUND")
}

func ResolveIndexLoopSuccessPlan(input IndexLoopSuccessPlanInput) IndexLoopSuccessPlan {
	shouldPrefetchNextPage := !input.ShouldStopIndexing &&
		!input.IsStopping &&
		(input.TargetTotalPages <= 0 || input.CurrentPage+1 <= input.TargetTotalPages)

	shouldWarnSparsePage := false
	sparseWarningMessage := ""
	if input.ExpectedItemsPerPage != nil &&
		*input.ExpectedItemsPerPage > 0 &&
		!input.ShouldStopIndexing &&
		!input.IsLastTargetPage &&
		input.LinksCount > 0 &&
		input.LinksCount < *input.ExpectedItemsPerPage {
		shouldWarnSparsePage = true
		sparseWarningMessage = fmt.Sprintf("第 %d 页条数偏低（%d/%d）。当前为普通模式，仅记录异常并继续尝试后续页面。", input.CurrentPage, input.LinksCount, *input.ExpectedItemsPerPage)
	}

	return IndexLoopSuccessPlan{
		ShouldPrefetchNextPage: shouldPrefetchNextPage,
		NextPrefetchPageNumber: input.CurrentPage + 1,
		ShouldWarnSparsePage:   shouldWarnSparsePage,
		SparseWarningMessage:   sparseWarningMessage,
		NextPageNumber:         input.CurrentPage + 1,
		StateReason:            "索引页处理完成",
		StateMessage:           fmt.Sprintf("第 %d 页已处理完成。", input.CurrentPage),
		DelayLogMessage:        fmt.Sprintf("下一页抓取前等待 %d 秒...", roundToNearestSecond(input.NextPageDelayMs)),
	}
}

func ResolveIndexLoopErrorPlan(currentPage int, message string, networkBackoffDelayMs int, genericDelayMs int) IndexLoopErrorPlan {
	delayMs := genericDelayMs
	retryLogMessage := ""
	if IsRetryableIndexNetworkError(message) {
		delayMs = networkBackoffDelayMs
		retryLogMessage = fmt.Sprintf("检测到网络异常，将在 %d 秒后重试...", roundToNearestSecond(networkBackoffDelayMs))
	}

	return IndexLoopErrorPlan{
		StateReason:     "索引页抓取失败",
		StateMessage:    fmt.Sprintf("第 %d 页抓取失败: %s", currentPage, message),
		DelayMs:         delayMs,
		RetryLogMessage: retryLogMessage,
	}
}

func roundToNearestSecond(delayMs int) int {
	if delayMs <= 0 {
		return 0
	}
	return int((delayMs + 500) / 1000)
}
