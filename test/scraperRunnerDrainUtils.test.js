const assert = require('assert');

const {
  inspectWorkQueueDrain,
  resolveWorkQueueDrainPollIntervalMs,
  buildFinalDrainPlan
} = require('../dist/core/scraperRunnerDrainUtils');

describe('scraperRunnerDrainUtils', () => {
  it('marks work queues finished when every tracked queue is empty', () => {
    assert.deepStrictEqual(
      inspectWorkQueueDrain({
        indexPageQueue: { waiting: 0, running: 0 },
        detailPageQueue: { waiting: 0, running: 0 },
        magnetFastQueue: { waiting: 0, running: 0 },
        magnetRecoveryQueue: { waiting: 0, running: 0 },
        fileWriteQueue: { waiting: 0, running: 0 },
        imageDownloadQueue: { waiting: 0, running: 0 }
      }),
      {
        workQueuesFinished: true,
        activeQueueCount: 0,
        pendingWorkCount: 0
      }
    );
  });

  it('counts active queues and pending work across all tracked buckets', () => {
    assert.deepStrictEqual(
      inspectWorkQueueDrain({
        indexPageQueue: { waiting: 2, running: 1 },
        detailPageQueue: { waiting: 0, running: 3 },
        magnetFastQueue: { waiting: 0, running: 0 },
        magnetRecoveryQueue: { waiting: 4, running: 0 },
        fileWriteQueue: { waiting: 0, running: 0 },
        imageDownloadQueue: { waiting: 0, running: 0 }
      }),
      {
        workQueuesFinished: false,
        activeQueueCount: 3,
        pendingWorkCount: 10
      }
    );
  });

  it('adjusts drain poll interval by backlog pressure', () => {
    assert.strictEqual(
      resolveWorkQueueDrainPollIntervalMs({
        workQueuesFinished: true,
        activeQueueCount: 0,
        pendingWorkCount: 0
      }),
      0
    );

    assert.strictEqual(
      resolveWorkQueueDrainPollIntervalMs({
        workQueuesFinished: false,
        activeQueueCount: 1,
        pendingWorkCount: 3
      }),
      250
    );

    assert.strictEqual(
      resolveWorkQueueDrainPollIntervalMs({
        workQueuesFinished: false,
        activeQueueCount: 2,
        pendingWorkCount: 12
      }),
      350
    );

    assert.strictEqual(
      resolveWorkQueueDrainPollIntervalMs({
        workQueuesFinished: false,
        activeQueueCount: 4,
        pendingWorkCount: 20
      }),
      650
    );

    assert.strictEqual(
      resolveWorkQueueDrainPollIntervalMs({
        workQueuesFinished: false,
        activeQueueCount: 2,
        pendingWorkCount: 20
      }),
      500
    );
  });

  it('builds a final drain plan from the active delay state', () => {
    assert.deepStrictEqual(
      buildFinalDrainPlan({
        hasActiveDelays: true
      }),
      {
        waitForDelays: true,
        flushOutputs: true
      }
    );

    assert.deepStrictEqual(
      buildFinalDrainPlan({
        hasActiveDelays: false
      }),
      {
        waitForDelays: false,
        flushOutputs: true
      }
    );
  });
});
