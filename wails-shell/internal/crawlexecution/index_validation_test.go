package crawlexecution

import "testing"

func TestResolveIndexValidationPolicyForInitialStrictMode(t *testing.T) {
	expectedCount := 30
	policy := ResolveIndexValidationPolicy(IndexValidationPolicyInput{
		FilmLimit:                 250,
		ExpectedCount:             &expectedCount,
		Phase:                     "initial",
		IndexPageRetryLimit:       3,
		StrictIndexPageRetryLimit: 5,
		LargeTaskMode:             true,
	})

	if policy.Phase != IndexValidationPhaseInitial {
		t.Fatalf("expected initial phase, got %q", policy.Phase)
	}
	if !policy.StrictPageLock {
		t.Fatalf("expected strict page lock")
	}
	if policy.MaxAttempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", policy.MaxAttempts)
	}
}

func TestResolveIndexValidationPolicyFallsBackToLooseRetryLimit(t *testing.T) {
	policy := ResolveIndexValidationPolicy(IndexValidationPolicyInput{
		FilmLimit:                 0,
		ExpectedCount:             nil,
		Phase:                     "recovery",
		IndexPageRetryLimit:       3,
		StrictIndexPageRetryLimit: 5,
		LargeTaskMode:             false,
	})

	if policy.StrictPageLock {
		t.Fatalf("did not expect strict page lock")
	}
	if policy.MaxAttempts != 3 {
		t.Fatalf("expected loose retry limit 3, got %d", policy.MaxAttempts)
	}
	if policy.Phase != IndexValidationPhaseRecovery {
		t.Fatalf("expected recovery phase, got %q", policy.Phase)
	}
}

func TestResolveIndexValidationRetryDelayMs(t *testing.T) {
	initialDelay := ResolveIndexValidationRetryDelayMs(IndexValidationRetryDelayInput{
		StrictPageLock: true,
		Attempt:        2,
		Phase:          "initial",
	})
	if initialDelay != 1500 {
		t.Fatalf("expected initial strict delay 1500, got %d", initialDelay)
	}

	recoveryDelay := ResolveIndexValidationRetryDelayMs(IndexValidationRetryDelayInput{
		StrictPageLock: true,
		Attempt:        3,
		Phase:          "recovery",
	})
	if recoveryDelay != 3300 {
		t.Fatalf("expected recovery strict delay 3300, got %d", recoveryDelay)
	}

	looseDelay := ResolveIndexValidationRetryDelayMs(IndexValidationRetryDelayInput{
		StrictPageLock: false,
		Attempt:        3,
		Phase:          "initial",
	})
	if looseDelay != 1500 {
		t.Fatalf("expected loose delay 1500, got %d", looseDelay)
	}
}

func TestShouldAcceptIndexValidationResult(t *testing.T) {
	expectedCount := 30
	if !ShouldAcceptIndexValidationResult(IndexValidationExpectationInput{
		ExpectedCount:    &expectedCount,
		StrictPageLock:   false,
		IsLastTargetPage: true,
		BestCount:        18,
	}) {
		t.Fatalf("expected relaxed last-page validation to pass")
	}

	if ShouldAcceptIndexValidationResult(IndexValidationExpectationInput{
		ExpectedCount:    &expectedCount,
		StrictPageLock:   true,
		IsLastTargetPage: false,
		BestCount:        29,
	}) {
		t.Fatalf("expected strict validation to fail when below target")
	}
}
