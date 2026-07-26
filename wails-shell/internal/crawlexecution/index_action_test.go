package crawlexecution

import "testing"

func TestResolveIndexProcessingActionPlanContinueAfterGap(t *testing.T) {
	plan := ResolveIndexProcessingActionPlan(IndexProcessingActionPlanInput{
		Action:             "continue_after_gap",
		ShouldStopIndexing: false,
		CurrentPage:        2,
		TargetTotalPages:   5,
	})

	if !plan.ShouldContinueCurrentLoop || plan.NextPageNumber != 3 || !plan.ShouldPersistState || plan.LogMessage == "" {
		t.Fatalf("unexpected continue-after-gap plan: %#v", plan)
	}
}

func TestResolveIndexProcessingActionPlanStopEmptyPage(t *testing.T) {
	plan := ResolveIndexProcessingActionPlan(IndexProcessingActionPlanInput{
		Action:             "stop_empty_page",
		ShouldStopIndexing: true,
		CurrentPage:        4,
		TargetTotalPages:   4,
	})

	if !plan.ShouldStopIndexing || !plan.ShouldContinueCurrentLoop || plan.NextPageNumber != 4 || !plan.ShouldPersistState {
		t.Fatalf("unexpected stop-empty-page plan: %#v", plan)
	}
}

func TestResolveIndexProcessingActionPlanStopLimitReached(t *testing.T) {
	plan := ResolveIndexProcessingActionPlan(IndexProcessingActionPlanInput{
		Action:             "stop_limit_reached",
		ShouldStopIndexing: true,
		CurrentPage:        6,
		TargetTotalPages:   0,
	})

	if !plan.ShouldStopIndexing || plan.ShouldContinueCurrentLoop || plan.NextPageNumber != 6 || plan.ShouldPersistState || plan.LogMessage == "" {
		t.Fatalf("unexpected stop-limit-reached plan: %#v", plan)
	}
}

func TestResolveIndexProcessingActionPlanDefault(t *testing.T) {
	plan := ResolveIndexProcessingActionPlan(IndexProcessingActionPlanInput{
		Action:             "unknown",
		ShouldStopIndexing: false,
		CurrentPage:        1,
		TargetTotalPages:   0,
	})

	if plan.ShouldContinueCurrentLoop || plan.NextPageNumber != 1 || plan.LogMessage != "" || plan.ShouldPersistState {
		t.Fatalf("unexpected default action plan: %#v", plan)
	}
}
