package crawlexecution

import "strings"

// index_validation.go owns policy and retry-delay rules for index-page sample
// validation.
//
// Ownership summary:
// 1) define index-page validation policy and retry timing rules
// 2) centralize strict-page-lock and expectation acceptance policy
// 3) keep validation policy separate from per-attempt and per-iteration planners
//
// File map for maintainers:
// 1) validation phase and policy DTOs
// 2) strict-versus-relaxed acceptance helpers
// 3) retry-delay and expectation normalization helpers

const (
	IndexValidationPhaseInitial  = "initial"
	IndexValidationPhaseRecovery = "recovery"
)

type IndexValidationPolicy struct {
	Phase          string `json:"phase"`
	StrictPageLock bool   `json:"strictPageLock"`
	MaxAttempts    int    `json:"maxAttempts"`
}

type IndexValidationPolicyInput struct {
	FilmLimit                 int
	ExpectedCount             *int
	Phase                     string
	IndexPageRetryLimit       int
	StrictIndexPageRetryLimit int
	LargeTaskMode             bool
}

type IndexValidationRetryDelayInput struct {
	StrictPageLock bool
	Attempt        int
	Phase          string
}

type IndexValidationExpectationInput struct {
	ExpectedCount    *int
	StrictPageLock   bool
	IsLastTargetPage bool
	BestCount        int
}

func NormalizeIndexValidationPhase(phase string) string {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "", IndexValidationPhaseInitial:
		return IndexValidationPhaseInitial
	default:
		return IndexValidationPhaseRecovery
	}
}

func ShouldEnforceExactPageValidation(filmLimit int, expectedCount *int) bool {
	return filmLimit > 0 && expectedCount != nil && *expectedCount > 0
}

func ResolveStrictIndexPageRetryLimit(phase string, strictIndexPageRetryLimit int, largeTaskMode bool) int {
	if strictIndexPageRetryLimit < 1 {
		strictIndexPageRetryLimit = 1
	}

	if NormalizeIndexValidationPhase(phase) == IndexValidationPhaseInitial {
		if largeTaskMode {
			return minInt(strictIndexPageRetryLimit, 2)
		}
		return minInt(strictIndexPageRetryLimit, 3)
	}

	if largeTaskMode {
		return minInt(strictIndexPageRetryLimit, 3)
	}
	return minInt(strictIndexPageRetryLimit, 4)
}

func ResolveIndexValidationPolicy(input IndexValidationPolicyInput) IndexValidationPolicy {
	indexPageRetryLimit := input.IndexPageRetryLimit
	if indexPageRetryLimit < 1 {
		indexPageRetryLimit = 1
	}

	phase := NormalizeIndexValidationPhase(input.Phase)
	strictPageLock := ShouldEnforceExactPageValidation(input.FilmLimit, input.ExpectedCount)
	maxAttempts := indexPageRetryLimit
	if strictPageLock {
		maxAttempts = ResolveStrictIndexPageRetryLimit(phase, input.StrictIndexPageRetryLimit, input.LargeTaskMode)
	}

	return IndexValidationPolicy{
		Phase:          phase,
		StrictPageLock: strictPageLock,
		MaxAttempts:    maxAttempts,
	}
}

func ResolveIndexValidationRetryDelayMs(input IndexValidationRetryDelayInput) int {
	attempt := input.Attempt
	if attempt < 1 {
		attempt = 1
	}

	if !input.StrictPageLock {
		return 1500
	}

	if NormalizeIndexValidationPhase(input.Phase) == IndexValidationPhaseInitial {
		return minInt(2200, 600+attempt*450)
	}

	return minInt(4200, 1200+attempt*700)
}

func ShouldAcceptIndexValidationResult(input IndexValidationExpectationInput) bool {
	if input.ExpectedCount == nil {
		return true
	}
	if !input.StrictPageLock && input.IsLastTargetPage {
		return true
	}
	return input.BestCount >= *input.ExpectedCount
}
