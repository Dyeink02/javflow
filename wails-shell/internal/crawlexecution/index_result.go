package crawlexecution

// index_result.go owns the normalized return payload after index-page
// validation finishes.
//
// Ownership summary:
// 1) build the normalized return payload after index validation completes
// 2) centralize accepted/rejected return log and retry metadata
// 3) keep return-shaping separate from attempt and sample-progress decisions
//
// File map for maintainers:
// 1) validation return input/output DTOs
// 2) accepted/rejected return payload builders
// 3) retry exhaustion and diagnostic reason shaping

type IndexValidationReturnPlanInput struct {
	Accepted                 bool   `json:"accepted"`
	StrictPageLock           bool   `json:"strictPageLock"`
	ExpectedCount            *int   `json:"expectedCount"`
	PageNumber               int    `json:"pageNumber"`
	AttemptsUsed             int    `json:"attemptsUsed"`
	MaxAttempts              int    `json:"maxAttempts"`
	ActualCount              int    `json:"actualCount"`
	StoppedEarly             bool   `json:"stoppedEarly"`
	BestDiagnosticReason     string `json:"bestDiagnosticReason"`
	FallbackDiagnosticReason string `json:"fallbackDiagnosticReason"`
}

type IndexValidationReturnPlan struct {
	ValidationPassed          bool     `json:"validationPassed"`
	ActualCount               int      `json:"actualCount"`
	RetryCount                int      `json:"retryCount"`
	EffectiveDiagnosticReason string   `json:"effectiveDiagnosticReason"`
	LogMessages               []string `json:"logMessages,omitempty"`
}

func ResolveIndexValidationReturnPlan(input IndexValidationReturnPlanInput) IndexValidationReturnPlan {
	effectiveDiagnosticReason := ResolveIndexValidationEffectiveDiagnosticReason(
		input.BestDiagnosticReason,
		input.FallbackDiagnosticReason,
	)

	if input.Accepted {
		return IndexValidationReturnPlan{
			ValidationPassed:          true,
			ActualCount:               input.ActualCount,
			RetryCount:                input.AttemptsUsed,
			EffectiveDiagnosticReason: effectiveDiagnosticReason,
			LogMessages:               []string{},
		}
	}

	finalization := FinalizeIndexValidationAttempts(
		input.StrictPageLock,
		input.ExpectedCount,
		input.PageNumber,
		input.AttemptsUsed,
		input.MaxAttempts,
		input.ActualCount,
		input.StoppedEarly,
	)

	return IndexValidationReturnPlan{
		ValidationPassed:          false,
		ActualCount:               input.ActualCount,
		RetryCount:                input.AttemptsUsed,
		EffectiveDiagnosticReason: effectiveDiagnosticReason,
		LogMessages:               finalization.LogMessages,
	}
}
