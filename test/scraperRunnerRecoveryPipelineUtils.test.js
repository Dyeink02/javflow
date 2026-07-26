const assert = require('assert');

const {
  createPageLockRetryTracker,
  evaluatePageValidationRetry,
  mergePageGapRecoveryResult,
  buildPageValidationExhaustedMessage
} = require('../dist/core/scraperRunnerRecoveryPipelineUtils');

describe('scraperRunnerRecoveryPipelineUtils', () => {
  it('triggers early stop after two stagnant strict page-lock retries', () => {
    let tracker = createPageLockRetryTracker();

    let result = evaluatePageValidationRetry({
      tracker,
      strictPageLock: true,
      expectedCount: 30,
      pageNumber: 1,
      attempt: 1,
      maxAttempts: 4,
      sampleCount: 29,
      mergedCount: 28,
      previousBestCount: 28
    });
    tracker = result.tracker;
    assert.strictEqual(result.shouldStopEarly, false);

    result = evaluatePageValidationRetry({
      tracker,
      strictPageLock: true,
      expectedCount: 30,
      pageNumber: 1,
      attempt: 2,
      maxAttempts: 4,
      sampleCount: 29,
      mergedCount: 28,
      previousBestCount: 28
    });
    tracker = result.tracker;
    assert.strictEqual(result.shouldStopEarly, false);

    result = evaluatePageValidationRetry({
      tracker,
      strictPageLock: true,
      expectedCount: 30,
      pageNumber: 1,
      attempt: 3,
      maxAttempts: 4,
      sampleCount: 29,
      mergedCount: 28,
      previousBestCount: 28
    });

    assert.strictEqual(result.shouldStopEarly, true);
    assert.ok(result.earlyStopMessage.length > 0);
  });

  it('merges page gap recovery result without exceeding the expected page size', () => {
    const result = mergePageGapRecoveryResult({
      expectedCount: 30,
      currentActualCount: 28,
      fetchedActualCount: 29,
      newLinksCount: 2
    });

    assert.deepStrictEqual(result, {
      mergedActualCount: 30,
      recoveredCount: 2,
      validationPassed: true,
      reason: '\u8865\u67E5\u6210\u529F\uFF0C\u65B0\u589E 2 \u6761\u5E76\u8FBE\u5230\u9884\u671F\u6761\u6570\u3002'
    });
  });

  it('builds a summary message when strict page validation stops early', () => {
    const message = buildPageValidationExhaustedMessage({
      strictPageLock: true,
      expectedCount: 30,
      pageNumber: 4,
      attemptsUsed: 3,
      maxAttempts: 4,
      bestCount: 29,
      stoppedEarly: true
    });

    assert.ok(message.includes('4'));
    assert.ok(message.includes('29'));
  });
});
