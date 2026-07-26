package crawlexecution

import "testing"

func TestGetDetailQueueTuningLargeTask(t *testing.T) {
	tuning := GetDetailQueueTuning(4, true)

	if tuning.HighWaterMark != 72 {
		t.Fatalf("expected high water mark 72, got %d", tuning.HighWaterMark)
	}
	if tuning.LowWaterMark != 39 {
		t.Fatalf("expected low water mark 39, got %d", tuning.LowWaterMark)
	}
	if tuning.BatchSize != 24 {
		t.Fatalf("expected batch size 24, got %d", tuning.BatchSize)
	}
	if tuning.ProgressLogStep != 72 {
		t.Fatalf("expected progress log step 72, got %d", tuning.ProgressLogStep)
	}
}

func TestResolveDetailQueueWaitDecision(t *testing.T) {
	decision := ResolveDetailQueueWaitDecision(50, 40, 16)
	if !decision.ShouldPauseEnqueue {
		t.Fatal("expected enqueue to pause when backlog reaches threshold")
	}
	if decision.ShouldResumeEnqueue {
		t.Fatal("did not expect enqueue to resume above resume threshold")
	}

	decision = ResolveDetailQueueWaitDecision(12, 40, 16)
	if decision.ShouldPauseEnqueue {
		t.Fatal("did not expect enqueue pause below threshold")
	}
	if !decision.ShouldResumeEnqueue {
		t.Fatal("expected enqueue to resume below resume threshold")
	}
}

func TestResolveDetailEnqueueBatchPlan(t *testing.T) {
	plan := ResolveDetailEnqueueBatchPlan(18, 40, 12, 50)
	if plan.AvailableCapacity != 22 {
		t.Fatalf("expected available capacity 22, got %d", plan.AvailableCapacity)
	}
	if plan.NextBatchSize != 12 {
		t.Fatalf("expected next batch size 12, got %d", plan.NextBatchSize)
	}

	plan = ResolveDetailEnqueueBatchPlan(39, 40, 12, 50)
	if plan.AvailableCapacity != 1 || plan.NextBatchSize != 1 {
		t.Fatalf("unexpected constrained batch plan: %#v", plan)
	}
}

func TestAdjustIndexPageDelayForBacklog(t *testing.T) {
	if got := AdjustIndexPageDelayForBacklog(5000, 20, 16, false); got != 3000 {
		t.Fatalf("expected regular mode delay 3000, got %d", got)
	}
	if got := AdjustIndexPageDelayForBacklog(5000, 20, 16, true); got != 1750 {
		t.Fatalf("expected large mode delay 1750, got %d", got)
	}
	if got := AdjustIndexPageDelayForBacklog(5000, 10, 16, true); got != 5000 {
		t.Fatalf("expected unchanged delay below low water mark, got %d", got)
	}
}
