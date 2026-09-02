package organizer

import (
	"testing"
	"time"
)

// TestWaitDeleteIntervalBurstCountsBatchOperations verifies the checked-batch
// burst limiter: per-target operations count up and the configured pause fires
// exactly at each batchDeleteBurstSize boundary. This is the throttle wired
// into executeBatchDelete; without it a cloud mount sees one dense request
// burst.
func TestWaitDeleteIntervalBurstCountsBatchOperations(t *testing.T) {
	ctx := &organizerRunContext{
		batchDelete:  true,
		adFileAction: adFileActionDeleteDirectly,
	}

	for i := 1; i < batchDeleteBurstSize; i++ {
		ctx.waitDeleteInterval()
	}
	if ctx.batchDeleteOps != batchDeleteBurstSize-1 {
		t.Fatalf("ops before boundary = %d, want %d", ctx.batchDeleteOps, batchDeleteBurstSize-1)
	}

	started := time.Now()
	ctx.waitDeleteInterval()
	elapsed := time.Since(started)
	if ctx.batchDeleteOps != batchDeleteBurstSize {
		t.Fatalf("ops at boundary = %d, want %d", ctx.batchDeleteOps, batchDeleteBurstSize)
	}
	if elapsed < batchDeleteBurstPause {
		t.Fatalf("boundary operation returned in %s, expected at least the %s burst pause", elapsed, batchDeleteBurstPause)
	}
}

func TestWaitDeleteIntervalSkipsDryRun(t *testing.T) {
	ctx := &organizerRunContext{
		batchDelete:  true,
		adFileAction: adFileActionDeleteDirectly,
		dryRun:       true,
	}

	started := time.Now()
	for i := 0; i < batchDeleteBurstSize*2; i++ {
		ctx.waitDeleteInterval()
	}
	if elapsed := time.Since(started); elapsed >= batchDeleteBurstPause {
		t.Fatalf("dry-run operations must not pause, elapsed %s", elapsed)
	}
}

func TestWaitDeleteIntervalNonBatchUsesConfiguredInterval(t *testing.T) {
	ctx := &organizerRunContext{
		adFileAction:     adFileActionDeleteDirectly,
		deleteIntervalMs: 300,
	}

	started := time.Now()
	ctx.waitDeleteInterval()
	if elapsed := time.Since(started); elapsed >= 300*time.Millisecond {
		t.Fatalf("first operation must not wait, elapsed %s", elapsed)
	}

	started = time.Now()
	ctx.waitDeleteInterval()
	if elapsed := time.Since(started); elapsed < 200*time.Millisecond {
		t.Fatalf("second operation should wait the remaining interval, elapsed %s", elapsed)
	}
}
