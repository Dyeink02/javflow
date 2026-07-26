package crawlexecution

import "strconv"

// index_attempt.go owns retry/acceptance decisions for one index-page
// validation attempt.
//
// Ownership summary:
// 1) decide whether one index-page validation attempt is accepted or retried
// 2) centralize per-attempt retry delay and stop-early policy
// 3) keep attempt-level validation decisions separate from outer loop planning
//
// File map for maintainers:
// 1) attempt-level validation input/output DTOs
// 2) acceptance and retry decision helpers
// 3) stop-early and retry-delay reasoning helpers

type IndexValidationAttemptDecisionInput struct {
	Tracker           PageLockRetryTracker `json:"tracker"`
	StrictPageLock    bool                 `json:"strictPageLock"`
	ExpectedCount     *int                 `json:"expectedCount"`
	IsLastTargetPage  bool                 `json:"isLastTargetPage"`
	PageNumber        int                  `json:"pageNumber"`
	Attempt           int                  `json:"attempt"`
	MaxAttempts       int                  `json:"maxAttempts"`
	Phase             string               `json:"phase"`
	SampleCount       int                  `json:"sampleCount"`
	MergedCount       int                  `json:"mergedCount"`
	PreviousBestCount int                  `json:"previousBestCount"`
}

type IndexValidationAttemptDecision struct {
	Tracker      PageLockRetryTracker `json:"tracker"`
	Accepted     bool                 `json:"accepted"`
	StoppedEarly bool                 `json:"stoppedEarly"`
	ShouldRetry  bool                 `json:"shouldRetry"`
	RetryDelayMs int                  `json:"retryDelayMs"`
	LogMessages  []string             `json:"logMessages,omitempty"`
}

type IndexValidationAttemptFinalization struct {
	LogMessages []string `json:"logMessages"`
}

func ResolveIndexValidationAttemptDecision(input IndexValidationAttemptDecisionInput) IndexValidationAttemptDecision {
	accepted := ShouldAcceptIndexValidationResult(IndexValidationExpectationInput{
		ExpectedCount:    input.ExpectedCount,
		StrictPageLock:   input.StrictPageLock,
		IsLastTargetPage: input.IsLastTargetPage,
		BestCount:        input.MergedCount,
	})
	if accepted {
		return IndexValidationAttemptDecision{
			Tracker:      input.Tracker,
			Accepted:     true,
			StoppedEarly: false,
			ShouldRetry:  false,
			RetryDelayMs: 0,
			LogMessages:  []string{},
		}
	}

	retryEvaluation := EvaluatePageValidationRetry(PageValidationRetryInput{
		Tracker:           input.Tracker,
		StrictPageLock:    input.StrictPageLock,
		ExpectedCount:     input.ExpectedCount,
		PageNumber:        input.PageNumber,
		Attempt:           input.Attempt,
		MaxAttempts:       input.MaxAttempts,
		SampleCount:       input.SampleCount,
		MergedCount:       input.MergedCount,
		PreviousBestCount: input.PreviousBestCount,
	})

	logMessages := []string{retryEvaluation.LogMessage}
	if retryEvaluation.ShouldStopEarly && retryEvaluation.EarlyStopMessage != "" {
		logMessages = append(logMessages, retryEvaluation.EarlyStopMessage)
	}

	shouldRetry := !retryEvaluation.ShouldStopEarly && input.Attempt < input.MaxAttempts
	retryDelayMs := 0
	if shouldRetry {
		retryDelayMs = ResolveIndexValidationRetryDelayMs(IndexValidationRetryDelayInput{
			StrictPageLock: input.StrictPageLock,
			Attempt:        input.Attempt,
			Phase:          input.Phase,
		})
	}

	return IndexValidationAttemptDecision{
		Tracker:      retryEvaluation.Tracker,
		Accepted:     false,
		StoppedEarly: retryEvaluation.ShouldStopEarly,
		ShouldRetry:  shouldRetry,
		RetryDelayMs: retryDelayMs,
		LogMessages:  logMessages,
	}
}

func FinalizeIndexValidationAttempts(strictPageLock bool, expectedCount *int, pageNumber int, attemptsUsed int, maxAttempts int, bestCount int, stoppedEarly bool) IndexValidationAttemptFinalization {
	logMessages := []string{}
	exhaustedMessage := BuildPageValidationExhaustedMessage(PageValidationExhaustedInput{
		StrictPageLock: strictPageLock,
		ExpectedCount:  expectedCount,
		PageNumber:     pageNumber,
		AttemptsUsed:   attemptsUsed,
		MaxAttempts:    maxAttempts,
		BestCount:      bestCount,
		StoppedEarly:   stoppedEarly,
	})
	if exhaustedMessage != "" {
		logMessages = append(logMessages, exhaustedMessage)
	}
	logMessages = append(logMessages, "第 "+strconv.Itoa(pageNumber)+" 页重试后仍未达到预期条数，使用本轮最佳结果 "+strconv.Itoa(bestCount)+" 条。")

	return IndexValidationAttemptFinalization{
		LogMessages: logMessages,
	}
}
