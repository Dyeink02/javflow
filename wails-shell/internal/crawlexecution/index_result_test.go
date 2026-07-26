package crawlexecution

import "testing"

func TestResolveIndexValidationReturnPlanAccepted(t *testing.T) {
	plan := ResolveIndexValidationReturnPlan(IndexValidationReturnPlanInput{
		Accepted:                 true,
		StrictPageLock:           true,
		ExpectedCount:            intPointer(30),
		PageNumber:               2,
		AttemptsUsed:             2,
		MaxAttempts:              4,
		ActualCount:              30,
		StoppedEarly:             false,
		BestDiagnosticReason:     "best reason",
		FallbackDiagnosticReason: "sample reason",
	})

	if !plan.ValidationPassed || plan.ActualCount != 30 || plan.RetryCount != 2 || plan.EffectiveDiagnosticReason != "best reason" || len(plan.LogMessages) != 0 {
		t.Fatalf("unexpected accepted return plan: %#v", plan)
	}
}

func TestResolveIndexValidationReturnPlanRejected(t *testing.T) {
	plan := ResolveIndexValidationReturnPlan(IndexValidationReturnPlanInput{
		Accepted:                 false,
		StrictPageLock:           true,
		ExpectedCount:            intPointer(30),
		PageNumber:               4,
		AttemptsUsed:             3,
		MaxAttempts:              4,
		ActualCount:              29,
		StoppedEarly:             true,
		BestDiagnosticReason:     "",
		FallbackDiagnosticReason: "fallback reason",
	})

	if plan.ValidationPassed || plan.ActualCount != 29 || plan.RetryCount != 3 || plan.EffectiveDiagnosticReason != "fallback reason" {
		t.Fatalf("unexpected rejected return plan: %#v", plan)
	}
	if len(plan.LogMessages) != 2 {
		t.Fatalf("expected two finalization log messages, got %#v", plan.LogMessages)
	}
}
