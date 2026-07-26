const assert = require('assert');

const {
  resolveIndexValidationAttemptDecision,
  finalizeIndexValidationAttempts
} = require('../dist/core/scraperRunnerIndexAttemptUtils');
const {
  createPageLockRetryTracker
} = require('../dist/core/scraperRunnerRecoveryPipelineUtils');

describe('scraperRunnerIndexAttemptUtils', () => {
  it('accepts an attempt immediately when the merged sample satisfies the expectation', () => {
    const decision = resolveIndexValidationAttemptDecision({
      tracker: createPageLockRetryTracker(),
      strictPageLock: true,
      expectedCount: 30,
      isLastTargetPage: false,
      pageNumber: 2,
      attempt: 1,
      maxAttempts: 4,
      phase: 'initial',
      sampleCount: 30,
      mergedCount: 30,
      previousBestCount: 0
    });

    assert.deepStrictEqual(decision, {
      tracker: {
        lastSampleCount: null,
        stagnantAttempts: 0
      },
      accepted: true,
      stoppedEarly: false,
      shouldRetry: false,
      retryDelayMs: 0,
      logMessages: []
    });
  });

  it('returns retry guidance and delay when validation should continue', () => {
    const decision = resolveIndexValidationAttemptDecision({
      tracker: createPageLockRetryTracker(),
      strictPageLock: true,
      expectedCount: 30,
      isLastTargetPage: false,
      pageNumber: 2,
      attempt: 1,
      maxAttempts: 4,
      phase: 'initial',
      sampleCount: 28,
      mergedCount: 28,
      previousBestCount: 0
    });

    assert.strictEqual(decision.accepted, false);
    assert.strictEqual(decision.stoppedEarly, false);
    assert.strictEqual(decision.shouldRetry, true);
    assert.strictEqual(decision.retryDelayMs, 1050);
    assert.strictEqual(decision.logMessages.length, 1);
  });

  it('returns an early-stop decision when page lock stalls', () => {
    const decision = resolveIndexValidationAttemptDecision({
      tracker: {
        lastSampleCount: 18,
        stagnantAttempts: 1
      },
      strictPageLock: true,
      expectedCount: 30,
      isLastTargetPage: false,
      pageNumber: 12,
      attempt: 2,
      maxAttempts: 5,
      phase: 'recovery',
      sampleCount: 18,
      mergedCount: 20,
      previousBestCount: 20
    });

    assert.strictEqual(decision.accepted, false);
    assert.strictEqual(decision.stoppedEarly, true);
    assert.strictEqual(decision.shouldRetry, false);
    assert.strictEqual(decision.retryDelayMs, 0);
    assert.strictEqual(decision.logMessages.length, 2);
  });

  it('builds final attempt log messages after retries are exhausted', () => {
    const finalization = finalizeIndexValidationAttempts({
      strictPageLock: true,
      expectedCount: 30,
      pageNumber: 4,
      attemptsUsed: 3,
      maxAttempts: 4,
      bestCount: 29,
      stoppedEarly: true
    });

    assert.strictEqual(finalization.logMessages.length, 2);
    assert.ok(finalization.logMessages[0].includes('第 4 页'));
    assert.strictEqual(finalization.logMessages[1], '第 4 页重试后仍未达到预期条数，使用本轮最佳结果 29 条。');
  });
});
