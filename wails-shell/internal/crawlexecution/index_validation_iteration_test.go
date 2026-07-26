package crawlexecution

import "testing"

func TestResolveIndexValidationIterationPlanAccepted(t *testing.T) {
	plan := ResolveIndexValidationIterationPlan(IndexValidationIterationPlanInput{
		PreviousBestCount:      0,
		MergedCount:            30,
		BestDiagnosticReason:   "",
		SampleDiagnosticReason: "sample reason",
		Tracker:                NewPageLockRetryTracker(),
		StrictPageLock:         true,
		ExpectedCount:          intPointer(30),
		IsLastTargetPage:       false,
		PageNumber:             2,
		Attempt:                1,
		MaxAttempts:            4,
		Phase:                  "initial",
		SampleCount:            30,
	})

	if plan.CurrentBestCount != 30 || !plan.ShouldPromoteMerged || plan.BestDiagnosticReason != "sample reason" {
		t.Fatalf("unexpected accepted iteration plan sample state: %#v", plan)
	}
	if plan.AcceptedReturnPlan == nil || !plan.AcceptedReturnPlan.ValidationPassed {
		t.Fatalf("expected accepted return plan, got %#v", plan.AcceptedReturnPlan)
	}
	if plan.ShouldRetry || plan.ShouldStopEarly || len(plan.LogMessages) != 0 {
		t.Fatalf("unexpected accepted iteration control state: %#v", plan)
	}
}

func TestResolveIndexValidationIterationPlanRetry(t *testing.T) {
	plan := ResolveIndexValidationIterationPlan(IndexValidationIterationPlanInput{
		PreviousBestCount:      0,
		MergedCount:            28,
		BestDiagnosticReason:   "",
		SampleDiagnosticReason: "",
		Tracker:                NewPageLockRetryTracker(),
		StrictPageLock:         true,
		ExpectedCount:          intPointer(30),
		IsLastTargetPage:       false,
		PageNumber:             2,
		Attempt:                1,
		MaxAttempts:            4,
		Phase:                  "initial",
		SampleCount:            28,
	})

	if plan.AcceptedReturnPlan != nil || !plan.ShouldRetry || plan.RetryDelayMs != 1050 || plan.ShouldStopEarly {
		t.Fatalf("unexpected retry iteration plan: %#v", plan)
	}
	if len(plan.LogMessages) != 1 {
		t.Fatalf("expected one retry log message, got %#v", plan.LogMessages)
	}
}

func TestResolveIndexValidationIterationPlanStopsEarly(t *testing.T) {
	plan := ResolveIndexValidationIterationPlan(IndexValidationIterationPlanInput{
		PreviousBestCount:      20,
		MergedCount:            20,
		BestDiagnosticReason:   "best reason",
		SampleDiagnosticReason: "",
		Tracker: PageLockRetryTracker{
			LastSampleCount:  18,
			HasLastSample:    true,
			StagnantAttempts: 1,
		},
		StrictPageLock:   true,
		ExpectedCount:    intPointer(30),
		IsLastTargetPage: false,
		PageNumber:       12,
		Attempt:          2,
		MaxAttempts:      5,
		Phase:            "recovery",
		SampleCount:      18,
	})

	if plan.AcceptedReturnPlan != nil || plan.ShouldRetry || !plan.ShouldStopEarly {
		t.Fatalf("unexpected early-stop iteration plan: %#v", plan)
	}
	if len(plan.LogMessages) != 2 {
		t.Fatalf("expected two early-stop log messages, got %#v", plan.LogMessages)
	}
}
