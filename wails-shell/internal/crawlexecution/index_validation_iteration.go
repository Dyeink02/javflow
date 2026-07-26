package crawlexecution

// index_validation_iteration.go owns one combined iteration plan across sample
// promotion, retry policy, and accepted return state.
//
// Ownership summary:
// 1) combine sample progress and attempt decisions into one iteration plan
// 2) decide merged-count promotion and accepted return payloads
// 3) keep one-iteration planning separate from lower-level attempt helpers
//
// File map for maintainers:
// 1) iteration-level plan DTOs
// 2) sample-progress and attempt-decision composition
// 3) accepted return-plan selection helpers

type IndexValidationIterationPlanInput struct {
	PreviousBestCount      int                  `json:"previousBestCount"`
	MergedCount            int                  `json:"mergedCount"`
	BestDiagnosticReason   string               `json:"bestDiagnosticReason"`
	SampleDiagnosticReason string               `json:"sampleDiagnosticReason"`
	Tracker                PageLockRetryTracker `json:"tracker"`
	StrictPageLock         bool                 `json:"strictPageLock"`
	ExpectedCount          *int                 `json:"expectedCount"`
	IsLastTargetPage       bool                 `json:"isLastTargetPage"`
	PageNumber             int                  `json:"pageNumber"`
	Attempt                int                  `json:"attempt"`
	MaxAttempts            int                  `json:"maxAttempts"`
	Phase                  string               `json:"phase"`
	SampleCount            int                  `json:"sampleCount"`
}

type IndexValidationIterationPlan struct {
	CurrentBestCount     int                        `json:"currentBestCount"`
	ShouldPromoteMerged  bool                       `json:"shouldPromoteMergedLinks"`
	BestDiagnosticReason string                     `json:"bestDiagnosticReason"`
	Tracker              PageLockRetryTracker       `json:"tracker"`
	AcceptedReturnPlan   *IndexValidationReturnPlan `json:"acceptedReturnPlan,omitempty"`
	LogMessages          []string                   `json:"logMessages,omitempty"`
	ShouldStopEarly      bool                       `json:"shouldStopEarly"`
	ShouldRetry          bool                       `json:"shouldRetry"`
	RetryDelayMs         int                        `json:"retryDelayMs"`
}

func ResolveIndexValidationIterationPlan(input IndexValidationIterationPlanInput) IndexValidationIterationPlan {
	sampleProgress := ResolveIndexValidationSampleProgress(IndexValidationSampleProgressInput{
		PreviousBestCount:      input.PreviousBestCount,
		MergedCount:            input.MergedCount,
		BestDiagnosticReason:   input.BestDiagnosticReason,
		SampleDiagnosticReason: input.SampleDiagnosticReason,
	})

	attemptDecision := ResolveIndexValidationAttemptDecision(IndexValidationAttemptDecisionInput{
		Tracker:           input.Tracker,
		StrictPageLock:    input.StrictPageLock,
		ExpectedCount:     input.ExpectedCount,
		IsLastTargetPage:  input.IsLastTargetPage,
		PageNumber:        input.PageNumber,
		Attempt:           input.Attempt,
		MaxAttempts:       input.MaxAttempts,
		Phase:             input.Phase,
		SampleCount:       input.SampleCount,
		MergedCount:       sampleProgress.CurrentBestCount,
		PreviousBestCount: input.PreviousBestCount,
	})

	var acceptedReturnPlan *IndexValidationReturnPlan
	if attemptDecision.Accepted {
		plan := ResolveIndexValidationReturnPlan(IndexValidationReturnPlanInput{
			Accepted:                 true,
			StrictPageLock:           input.StrictPageLock,
			ExpectedCount:            input.ExpectedCount,
			PageNumber:               input.PageNumber,
			AttemptsUsed:             input.Attempt,
			MaxAttempts:              input.MaxAttempts,
			ActualCount:              sampleProgress.CurrentBestCount,
			StoppedEarly:             false,
			BestDiagnosticReason:     sampleProgress.BestDiagnosticReason,
			FallbackDiagnosticReason: input.SampleDiagnosticReason,
		})
		acceptedReturnPlan = &plan
	}

	return IndexValidationIterationPlan{
		CurrentBestCount:     sampleProgress.CurrentBestCount,
		ShouldPromoteMerged:  sampleProgress.ShouldPromoteMergedLinks,
		BestDiagnosticReason: sampleProgress.BestDiagnosticReason,
		Tracker:              attemptDecision.Tracker,
		AcceptedReturnPlan:   acceptedReturnPlan,
		LogMessages:          attemptDecision.LogMessages,
		ShouldStopEarly:      attemptDecision.StoppedEarly,
		ShouldRetry:          attemptDecision.ShouldRetry,
		RetryDelayMs:         attemptDecision.RetryDelayMs,
	}
}
