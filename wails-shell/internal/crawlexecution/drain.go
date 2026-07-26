package crawlexecution

// drain.go owns final queue-drain inspection and artifact flush planning near
// crawl shutdown.
//
// Ownership summary:
// 1) inspect queue drain status near crawl shutdown
// 2) decide final wait/flush behavior for the shutdown tail
// 3) keep drain planning separate from queue implementation details
//
// File map for maintainers:
// 1) queue bucket and drain inspection DTOs
// 2) drain wait/flush planning helpers
// 3) shutdown-tail readiness checks

type QueueBucketStats struct {
	Waiting int `json:"waiting"`
	Running int `json:"running"`
}

type WorkQueueStats struct {
	IndexPageQueue      *QueueBucketStats `json:"indexPageQueue,omitempty"`
	DetailPageQueue     *QueueBucketStats `json:"detailPageQueue,omitempty"`
	MagnetFastQueue     *QueueBucketStats `json:"magnetFastQueue,omitempty"`
	MagnetRecoveryQueue *QueueBucketStats `json:"magnetRecoveryQueue,omitempty"`
	FileWriteQueue      *QueueBucketStats `json:"fileWriteQueue,omitempty"`
	ImageDownloadQueue  *QueueBucketStats `json:"imageDownloadQueue,omitempty"`
}

type WorkQueueDrainInspection struct {
	WorkQueuesFinished bool `json:"workQueuesFinished"`
	ActiveQueueCount   int  `json:"activeQueueCount"`
	PendingWorkCount   int  `json:"pendingWorkCount"`
}

type FinalDrainPlan struct {
	WaitForDelays bool `json:"waitForDelays"`
	FlushOutputs  bool `json:"flushOutputs"`
}

func normalizeQueueBucketStats(bucket *QueueBucketStats) QueueBucketStats {
	if bucket == nil {
		return QueueBucketStats{}
	}

	return QueueBucketStats{
		Waiting: maxInt(bucket.Waiting, 0),
		Running: maxInt(bucket.Running, 0),
	}
}

func InspectWorkQueueDrain(stats WorkQueueStats) WorkQueueDrainInspection {
	buckets := []QueueBucketStats{
		normalizeQueueBucketStats(stats.IndexPageQueue),
		normalizeQueueBucketStats(stats.DetailPageQueue),
		normalizeQueueBucketStats(stats.MagnetFastQueue),
		normalizeQueueBucketStats(stats.MagnetRecoveryQueue),
		normalizeQueueBucketStats(stats.FileWriteQueue),
		normalizeQueueBucketStats(stats.ImageDownloadQueue),
	}

	activeQueueCount := 0
	pendingWorkCount := 0

	for _, bucket := range buckets {
		total := bucket.Waiting + bucket.Running
		if total > 0 {
			activeQueueCount++
			pendingWorkCount += total
		}
	}

	return WorkQueueDrainInspection{
		WorkQueuesFinished: pendingWorkCount == 0,
		ActiveQueueCount:   activeQueueCount,
		PendingWorkCount:   pendingWorkCount,
	}
}

func ResolveWorkQueueDrainPollIntervalMs(inspection WorkQueueDrainInspection) int {
	if inspection.PendingWorkCount <= 0 {
		return 0
	}
	if inspection.PendingWorkCount <= 4 {
		return 250
	}
	if inspection.PendingWorkCount <= 12 {
		return 350
	}
	if inspection.PendingWorkCount >= 40 || inspection.ActiveQueueCount >= 4 {
		return 650
	}
	return 500
}

func BuildFinalDrainPlan(hasActiveDelays bool) FinalDrainPlan {
	return FinalDrainPlan{
		WaitForDelays: hasActiveDelays,
		FlushOutputs:  true,
	}
}
