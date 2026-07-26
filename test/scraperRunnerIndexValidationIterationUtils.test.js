const assert = require('assert');

const {
  resolveIndexValidationIterationPlan
} = require('../dist/core/scraperRunnerIndexValidationIterationUtils');
const {
  createPageLockRetryTracker
} = require('../dist/core/scraperRunnerRecoveryPipelineUtils');

describe('scraperRunnerIndexValidationIterationUtils', () => {
  it('promotes the merged sample and returns an accepted plan when the expectation is met', () => {
    const plan = resolveIndexValidationIterationPlan({
      previousBestCount: 0,
      mergedCount: 30,
      bestDiagnosticReason: '',
      sampleDiagnosticReason: 'sample reason',
      tracker: createPageLockRetryTracker(),
      strictPageLock: true,
      expectedCount: 30,
      isLastTargetPage: false,
      pageNumber: 2,
      attempt: 1,
      maxAttempts: 4,
      phase: 'initial',
      sampleCount: 30
    });

    assert.strictEqual(plan.currentBestCount, 30);
    assert.strictEqual(plan.shouldPromoteMergedLinks, true);
    assert.strictEqual(plan.bestDiagnosticReason, 'sample reason');
    assert.ok(plan.acceptedReturnPlan);
    assert.strictEqual(plan.acceptedReturnPlan.validationPassed, true);
    assert.strictEqual(plan.acceptedReturnPlan.actualCount, 30);
    assert.strictEqual(plan.logMessages.length, 0);
    assert.strictEqual(plan.shouldRetry, false);
    assert.strictEqual(plan.shouldStopEarly, false);
  });

  it('returns retry guidance when validation should continue', () => {
    const plan = resolveIndexValidationIterationPlan({
      previousBestCount: 0,
      mergedCount: 28,
      bestDiagnosticReason: '',
      sampleDiagnosticReason: '',
      tracker: createPageLockRetryTracker(),
      strictPageLock: true,
      expectedCount: 30,
      isLastTargetPage: false,
      pageNumber: 2,
      attempt: 1,
      maxAttempts: 4,
      phase: 'initial',
      sampleCount: 28
    });

    assert.strictEqual(plan.currentBestCount, 28);
    assert.strictEqual(plan.acceptedReturnPlan, null);
    assert.strictEqual(plan.shouldRetry, true);
    assert.strictEqual(plan.retryDelayMs, 1050);
    assert.strictEqual(plan.shouldStopEarly, false);
    assert.strictEqual(plan.logMessages.length, 1);
  });

  it('returns an early-stop plan when the page lock stalls', () => {
    const plan = resolveIndexValidationIterationPlan({
      previousBestCount: 20,
      mergedCount: 20,
      bestDiagnosticReason: 'best reason',
      sampleDiagnosticReason: '',
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
      sampleCount: 18
    });

    assert.strictEqual(plan.currentBestCount, 20);
    assert.strictEqual(plan.shouldPromoteMergedLinks, false);
    assert.strictEqual(plan.acceptedReturnPlan, null);
    assert.strictEqual(plan.shouldRetry, false);
    assert.strictEqual(plan.shouldStopEarly, true);
    assert.strictEqual(plan.logMessages.length, 2);
  });
});
