package crawlexecution

import "testing"

func intPtr(value int) *int {
	return &value
}

func TestResolveIndexLoopSuccessPlan(t *testing.T) {
	plan := ResolveIndexLoopSuccessPlan(IndexLoopSuccessPlanInput{
		CurrentPage:          2,
		NextPageDelayMs:      4200,
		ShouldStopIndexing:   false,
		IsStopping:           false,
		TargetTotalPages:     4,
		ExpectedItemsPerPage: intPtr(30),
		IsLastTargetPage:     false,
		LinksCount:           12,
	})

	if !plan.ShouldPrefetchNextPage || plan.NextPrefetchPageNumber != 3 {
		t.Fatalf("expected prefetch plan for next page, got %#v", plan)
	}
	if !plan.ShouldWarnSparsePage {
		t.Fatalf("expected sparse page warning, got %#v", plan)
	}
	if plan.DelayLogMessage != "下一页抓取前等待 4 秒..." {
		t.Fatalf("unexpected delay log message %q", plan.DelayLogMessage)
	}
}

func TestResolveIndexLoopErrorPlan(t *testing.T) {
	networkPlan := ResolveIndexLoopErrorPlan(6, "ETIMEDOUT", 12000, 7000)
	if networkPlan.DelayMs != 12000 || networkPlan.RetryLogMessage == "" {
		t.Fatalf("expected network retry plan, got %#v", networkPlan)
	}

	genericPlan := ResolveIndexLoopErrorPlan(6, "parse failed", 12000, 7000)
	if genericPlan.DelayMs != 7000 || genericPlan.RetryLogMessage != "" {
		t.Fatalf("expected generic delay plan, got %#v", genericPlan)
	}
}
