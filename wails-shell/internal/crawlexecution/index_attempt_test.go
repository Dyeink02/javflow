package crawlexecution

import "testing"

func TestResolveIndexValidationAttemptDecisionAcceptsSatisfiedSample(t *testing.T) {
	decision := ResolveIndexValidationAttemptDecision(IndexValidationAttemptDecisionInput{
		Tracker:           NewPageLockRetryTracker(),
		StrictPageLock:    true,
		ExpectedCount:     intPointer(30),
		IsLastTargetPage:  false,
		PageNumber:        2,
		Attempt:           1,
		MaxAttempts:       4,
		Phase:             "initial",
		SampleCount:       30,
		MergedCount:       30,
		PreviousBestCount: 0,
	})

	if !decision.Accepted || decision.ShouldRetry || decision.StoppedEarly {
		t.Fatalf("expected accepted decision, got %#v", decision)
	}
}

func TestResolveIndexValidationAttemptDecisionContinuesRetry(t *testing.T) {
	decision := ResolveIndexValidationAttemptDecision(IndexValidationAttemptDecisionInput{
		Tracker:           NewPageLockRetryTracker(),
		StrictPageLock:    true,
		ExpectedCount:     intPointer(30),
		IsLastTargetPage:  false,
		PageNumber:        2,
		Attempt:           1,
		MaxAttempts:       4,
		Phase:             "initial",
		SampleCount:       28,
		MergedCount:       28,
		PreviousBestCount: 0,
	})

	if decision.Accepted || !decision.ShouldRetry || decision.RetryDelayMs != 1050 {
		t.Fatalf("expected retry decision, got %#v", decision)
	}
}

func TestResolveIndexValidationAttemptDecisionStopsEarly(t *testing.T) {
	decision := ResolveIndexValidationAttemptDecision(IndexValidationAttemptDecisionInput{
		Tracker: PageLockRetryTracker{
			LastSampleCount:  18,
			HasLastSample:    true,
			StagnantAttempts: 1,
		},
		StrictPageLock:    true,
		ExpectedCount:     intPointer(30),
		IsLastTargetPage:  false,
		PageNumber:        12,
		Attempt:           2,
		MaxAttempts:       5,
		Phase:             "recovery",
		SampleCount:       18,
		MergedCount:       20,
		PreviousBestCount: 20,
	})

	if decision.Accepted || !decision.StoppedEarly || decision.ShouldRetry {
		t.Fatalf("expected early stop decision, got %#v", decision)
	}
	if len(decision.LogMessages) != 2 {
		t.Fatalf("expected two log messages, got %#v", decision.LogMessages)
	}
}

func TestFinalizeIndexValidationAttempts(t *testing.T) {
	finalization := FinalizeIndexValidationAttempts(true, intPointer(30), 4, 3, 4, 29, true)
	if len(finalization.LogMessages) != 2 {
		t.Fatalf("expected two finalization messages, got %#v", finalization.LogMessages)
	}
}
