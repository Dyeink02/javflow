package crawlexecution

import "testing"

func TestInspectWorkQueueDrainEmpty(t *testing.T) {
	inspection := InspectWorkQueueDrain(WorkQueueStats{
		IndexPageQueue:      &QueueBucketStats{Waiting: 0, Running: 0},
		DetailPageQueue:     &QueueBucketStats{Waiting: 0, Running: 0},
		MagnetFastQueue:     &QueueBucketStats{Waiting: 0, Running: 0},
		MagnetRecoveryQueue: &QueueBucketStats{Waiting: 0, Running: 0},
		FileWriteQueue:      &QueueBucketStats{Waiting: 0, Running: 0},
		ImageDownloadQueue:  &QueueBucketStats{Waiting: 0, Running: 0},
	})

	if !inspection.WorkQueuesFinished {
		t.Fatal("expected work queues to be marked finished")
	}
	if inspection.ActiveQueueCount != 0 {
		t.Fatalf("expected 0 active queues, got %d", inspection.ActiveQueueCount)
	}
	if inspection.PendingWorkCount != 0 {
		t.Fatalf("expected 0 pending work items, got %d", inspection.PendingWorkCount)
	}
}

func TestInspectWorkQueueDrainCountsBacklog(t *testing.T) {
	inspection := InspectWorkQueueDrain(WorkQueueStats{
		IndexPageQueue:      &QueueBucketStats{Waiting: 2, Running: 1},
		DetailPageQueue:     &QueueBucketStats{Waiting: 0, Running: 3},
		MagnetFastQueue:     &QueueBucketStats{Waiting: 0, Running: 0},
		MagnetRecoveryQueue: &QueueBucketStats{Waiting: 4, Running: 0},
	})

	if inspection.WorkQueuesFinished {
		t.Fatal("did not expect work queues to be finished")
	}
	if inspection.ActiveQueueCount != 3 {
		t.Fatalf("expected 3 active queues, got %d", inspection.ActiveQueueCount)
	}
	if inspection.PendingWorkCount != 10 {
		t.Fatalf("expected 10 pending work items, got %d", inspection.PendingWorkCount)
	}
}

func TestResolveWorkQueueDrainPollIntervalMs(t *testing.T) {
	if got := ResolveWorkQueueDrainPollIntervalMs(WorkQueueDrainInspection{
		WorkQueuesFinished: true,
		ActiveQueueCount:   0,
		PendingWorkCount:   0,
	}); got != 0 {
		t.Fatalf("expected finished drain poll interval 0, got %d", got)
	}

	if got := ResolveWorkQueueDrainPollIntervalMs(WorkQueueDrainInspection{
		WorkQueuesFinished: false,
		ActiveQueueCount:   1,
		PendingWorkCount:   3,
	}); got != 250 {
		t.Fatalf("expected light backlog poll interval 250, got %d", got)
	}

	if got := ResolveWorkQueueDrainPollIntervalMs(WorkQueueDrainInspection{
		WorkQueuesFinished: false,
		ActiveQueueCount:   2,
		PendingWorkCount:   12,
	}); got != 350 {
		t.Fatalf("expected medium backlog poll interval 350, got %d", got)
	}

	if got := ResolveWorkQueueDrainPollIntervalMs(WorkQueueDrainInspection{
		WorkQueuesFinished: false,
		ActiveQueueCount:   4,
		PendingWorkCount:   20,
	}); got != 650 {
		t.Fatalf("expected high pressure poll interval 650, got %d", got)
	}

	if got := ResolveWorkQueueDrainPollIntervalMs(WorkQueueDrainInspection{
		WorkQueuesFinished: false,
		ActiveQueueCount:   2,
		PendingWorkCount:   20,
	}); got != 500 {
		t.Fatalf("expected default poll interval 500, got %d", got)
	}
}

func TestBuildFinalDrainPlan(t *testing.T) {
	plan := BuildFinalDrainPlan(true)
	if !plan.WaitForDelays {
		t.Fatal("expected wait-for-delays to be enabled when delays are active")
	}
	if !plan.FlushOutputs {
		t.Fatal("expected outputs to always flush during final drain")
	}

	plan = BuildFinalDrainPlan(false)
	if plan.WaitForDelays {
		t.Fatal("did not expect wait-for-delays when no delays are active")
	}
	if !plan.FlushOutputs {
		t.Fatal("expected outputs to always flush during final drain")
	}
}
