package crawlexecution

import "math"

// detail_queue.go owns queue-watermark tuning and enqueue pacing for detail
// tasks.
//
// Ownership summary:
// 1) calculate detail-queue watermarks and pacing decisions
// 2) centralize enqueue pause/resume/batch-size policy
// 3) keep queue tuning separate from queue implementation and runner logic
//
// File map for maintainers:
// 1) queue tuning DTOs and defaults
// 2) pause/resume decision helpers
// 3) enqueue batch sizing helpers

type DetailQueueTuning struct {
	HighWaterMark   int `json:"highWaterMark"`
	LowWaterMark    int `json:"lowWaterMark"`
	BatchSize       int `json:"batchSize"`
	ProgressLogStep int `json:"progressLogStep"`
}

type DetailQueueWaitDecision struct {
	ShouldPauseEnqueue  bool `json:"shouldPauseEnqueue"`
	ShouldResumeEnqueue bool `json:"shouldResumeEnqueue"`
}

type DetailEnqueueBatchPlan struct {
	AvailableCapacity int `json:"availableCapacity"`
	NextBatchSize     int `json:"nextBatchSize"`
}

func GetDetailQueueTuning(parallel int, largeTaskMode bool) DetailQueueTuning {
	if parallel < 1 {
		parallel = 1
	}

	highWaterMultiplier := 12
	highWaterMax := 90
	if largeTaskMode {
		highWaterMultiplier = 18
		highWaterMax = 160
	}
	highWaterMark := maxInt(24, minInt(highWaterMax, parallel*highWaterMultiplier))

	lowWaterRatio := 0.4
	if largeTaskMode {
		lowWaterRatio = 0.55
	}
	lowWaterMark := maxInt(8, int(math.Floor(float64(highWaterMark)*lowWaterRatio)))

	batchMultiplier := 4
	batchMax := 24
	batchMin := 8
	if largeTaskMode {
		batchMultiplier = 6
		batchMax = 48
		batchMin = 12
	}
	batchSize := maxInt(batchMin, minInt(batchMax, parallel*batchMultiplier))

	progressLogStep := maxInt(batchSize*2, 16)
	if largeTaskMode {
		progressLogStep = maxInt(batchSize*3, 36)
	}

	return DetailQueueTuning{
		HighWaterMark:   highWaterMark,
		LowWaterMark:    lowWaterMark,
		BatchSize:       batchSize,
		ProgressLogStep: progressLogStep,
	}
}

func ResolveDetailQueueWaitDecision(backlog int, threshold int, resumeThreshold int) DetailQueueWaitDecision {
	if backlog < 0 {
		backlog = 0
	}
	if threshold < 0 {
		threshold = 0
	}
	if resumeThreshold < 0 {
		resumeThreshold = 0
	}
	if resumeThreshold == 0 {
		resumeThreshold = threshold
	}

	return DetailQueueWaitDecision{
		ShouldPauseEnqueue:  backlog >= threshold,
		ShouldResumeEnqueue: backlog <= resumeThreshold,
	}
}

func ResolveDetailEnqueueBatchPlan(backlog int, highWaterMark int, batchSize int, remainingCount int) DetailEnqueueBatchPlan {
	if backlog < 0 {
		backlog = 0
	}
	if highWaterMark < 1 {
		highWaterMark = 1
	}
	if batchSize < 1 {
		batchSize = 1
	}
	if remainingCount < 0 {
		remainingCount = 0
	}

	availableCapacity := maxInt(highWaterMark-backlog, 1)
	nextBatchSize := 0
	if remainingCount > 0 {
		nextBatchSize = maxInt(1, minInt(batchSize, minInt(availableCapacity, remainingCount)))
	}

	return DetailEnqueueBatchPlan{
		AvailableCapacity: availableCapacity,
		NextBatchSize:     nextBatchSize,
	}
}

func AdjustIndexPageDelayForBacklog(baseDelayMs int, backlog int, lowWaterMark int, largeTaskMode bool) int {
	if baseDelayMs < 0 {
		baseDelayMs = 0
	}
	if backlog < 0 {
		backlog = 0
	}
	if lowWaterMark < 0 {
		lowWaterMark = 0
	}
	if backlog < lowWaterMark {
		return baseDelayMs
	}

	multiplier := 0.6
	if largeTaskMode {
		multiplier = 0.35
	}

	return maxInt(0, int(math.Round(float64(baseDelayMs)*multiplier)))
}
