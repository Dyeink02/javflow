package crawlexecution

import "testing"

func TestResolveIndexPageExecutionPlanWithQueueLimit(t *testing.T) {
	plan := ResolveIndexPageExecutionPlan(IndexPageExecutionPlanInput{
		CurrentPage:                 1,
		TargetTotalPages:            3,
		ExpectedCount:               intPointer(30),
		LinksCount:                  30,
		TrackedLinksCount:           20,
		NewLinksCount:               18,
		FilmsQueued:                 8,
		FilmLimit:                   10,
		ResumeExisting:              false,
		CurrentExpectedItemsPerPage: nil,
	})

	if !plan.ShouldSetExpectedItemsPerPage || plan.ExpectedItemsPerPageValue == nil || *plan.ExpectedItemsPerPageValue != 30 {
		t.Fatalf("unexpected expected-items plan: %#v", plan)
	}
	if len(plan.LogMessages) != 2 || plan.PreQueueDecision != nil || plan.QueueCount != 2 || plan.PostQueueDecision == nil || plan.PostQueueDecision.Action != "stop_limit_reached" {
		t.Fatalf("unexpected queue plan: %#v", plan)
	}
}

func TestResolveIndexPageExecutionPlanEmptyPage(t *testing.T) {
	plan := ResolveIndexPageExecutionPlan(IndexPageExecutionPlanInput{
		CurrentPage:                 2,
		TargetTotalPages:            4,
		ExpectedCount:               intPointer(30),
		LinksCount:                  0,
		TrackedLinksCount:           0,
		NewLinksCount:               0,
		FilmsQueued:                 0,
		FilmLimit:                   0,
		ResumeExisting:              false,
		CurrentExpectedItemsPerPage: nil,
	})

	if plan.PreQueueDecision == nil || plan.PreQueueDecision.Action != "continue_after_gap" || plan.QueueCount != 0 || plan.PostQueueDecision != nil {
		t.Fatalf("unexpected empty-page plan: %#v", plan)
	}
}

func TestResolveIndexPageExecutionPlanNoNewLinks(t *testing.T) {
	plan := ResolveIndexPageExecutionPlan(IndexPageExecutionPlanInput{
		CurrentPage:                 3,
		TargetTotalPages:            0,
		ExpectedCount:               nil,
		LinksCount:                  20,
		TrackedLinksCount:           20,
		NewLinksCount:               0,
		FilmsQueued:                 0,
		FilmLimit:                   0,
		ResumeExisting:              false,
		CurrentExpectedItemsPerPage: intPointer(20),
	})

	if plan.PreQueueDecision == nil || plan.PreQueueDecision.Action != "stop_no_new_links" || plan.QueueCount != 0 || plan.PostQueueDecision != nil {
		t.Fatalf("unexpected no-new-links plan: %#v", plan)
	}
}

func TestResolveIndexPageExecutionPlanLimitAlreadyReached(t *testing.T) {
	plan := ResolveIndexPageExecutionPlan(IndexPageExecutionPlanInput{
		CurrentPage:                 5,
		TargetTotalPages:            0,
		ExpectedCount:               intPointer(30),
		LinksCount:                  30,
		TrackedLinksCount:           30,
		NewLinksCount:               6,
		FilmsQueued:                 10,
		FilmLimit:                   10,
		ResumeExisting:              false,
		CurrentExpectedItemsPerPage: intPointer(30),
	})

	if plan.PreQueueDecision == nil || plan.PreQueueDecision.Action != "stop_limit_reached" || plan.QueueCount != 0 || plan.PostQueueDecision != nil {
		t.Fatalf("unexpected pre-limit-stop plan: %#v", plan)
	}
}
