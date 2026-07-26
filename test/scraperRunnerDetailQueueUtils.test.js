const assert = require('assert');

const {
  getDetailQueueTuning,
  resolveDetailQueueWaitDecision,
  resolveDetailEnqueueBatchPlan,
  adjustIndexPageDelayForBacklog
} = require('../dist/core/scraperRunnerDetailQueueUtils');

describe('scraperRunnerDetailQueueUtils', () => {
  it('builds large-task queue tuning from parallelism', () => {
    assert.deepStrictEqual(
      getDetailQueueTuning({
        parallel: 4,
        largeTaskMode: true
      }),
      {
        highWaterMark: 72,
        lowWaterMark: 39,
        batchSize: 24,
        progressLogStep: 72
      }
    );
  });

  it('decides when enqueueing should pause and resume', () => {
    assert.deepStrictEqual(
      resolveDetailQueueWaitDecision({
        backlog: 50,
        threshold: 40,
        resumeThreshold: 16
      }),
      {
        shouldPauseEnqueue: true,
        shouldResumeEnqueue: false
      }
    );

    assert.deepStrictEqual(
      resolveDetailQueueWaitDecision({
        backlog: 12,
        threshold: 40,
        resumeThreshold: 16
      }),
      {
        shouldPauseEnqueue: false,
        shouldResumeEnqueue: true
      }
    );
  });

  it('builds the next enqueue batch plan from backlog and remaining work', () => {
    assert.deepStrictEqual(
      resolveDetailEnqueueBatchPlan({
        backlog: 18,
        highWaterMark: 40,
        batchSize: 12,
        remainingCount: 50
      }),
      {
        availableCapacity: 22,
        nextBatchSize: 12
      }
    );

    assert.deepStrictEqual(
      resolveDetailEnqueueBatchPlan({
        backlog: 39,
        highWaterMark: 40,
        batchSize: 12,
        remainingCount: 50
      }),
      {
        availableCapacity: 1,
        nextBatchSize: 1
      }
    );
  });

  it('shortens the next index-page delay when detail backlog is still high', () => {
    assert.strictEqual(
      adjustIndexPageDelayForBacklog({
        baseDelayMs: 5000,
        backlog: 20,
        lowWaterMark: 16,
        largeTaskMode: false
      }),
      3000
    );

    assert.strictEqual(
      adjustIndexPageDelayForBacklog({
        baseDelayMs: 5000,
        backlog: 20,
        lowWaterMark: 16,
        largeTaskMode: true
      }),
      1750
    );

    assert.strictEqual(
      adjustIndexPageDelayForBacklog({
        baseDelayMs: 5000,
        backlog: 10,
        lowWaterMark: 16,
        largeTaskMode: true
      }),
      5000
    );
  });
});
