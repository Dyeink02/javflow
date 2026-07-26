package crawlexecution

import "testing"

func intPointer(value int) *int {
	return &value
}

func TestMergePageGapRecoveryResult(t *testing.T) {
	result := CalculatePageGapRecoveryResult(MergePageGapRecoveryInput{
		ExpectedCount:      30,
		CurrentActualCount: 18,
		FetchedActualCount: 24,
		NewLinksCount:      7,
	})

	if result.MergedActualCount != 25 {
		t.Fatalf("expected merged count 25, got %d", result.MergedActualCount)
	}
	if result.ValidationPassed {
		t.Fatalf("expected validation to remain false")
	}
}

func TestEvaluatePageValidationRetryStopsEarlyWhenStrictAndStagnant(t *testing.T) {
	result := EvaluatePageValidationRetry(PageValidationRetryInput{
		Tracker: PageLockRetryTracker{
			LastSampleCount:  18,
			HasLastSample:    true,
			StagnantAttempts: 1,
		},
		StrictPageLock:    true,
		ExpectedCount:     intPointer(30),
		PageNumber:        12,
		Attempt:           2,
		MaxAttempts:       5,
		SampleCount:       18,
		MergedCount:       20,
		PreviousBestCount: 20,
	})

	if !result.ShouldStopEarly {
		t.Fatalf("expected early stop")
	}
	if result.Tracker.StagnantAttempts != 2 {
		t.Fatalf("expected stagnant attempts 2, got %d", result.Tracker.StagnantAttempts)
	}
}

func TestBuildDetailBudgetStopMessages(t *testing.T) {
	messages := BuildDetailBudgetStopMessages(3)
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
}
