const assert = require('assert');

const {
  resolveIndexValidationReturnPlan
} = require('../dist/core/scraperRunnerIndexResultUtils');

describe('scraperRunnerIndexResultUtils', () => {
  it('builds a success return plan without finalization logs', () => {
    const plan = resolveIndexValidationReturnPlan({
      accepted: true,
      strictPageLock: true,
      expectedCount: 30,
      pageNumber: 2,
      attemptsUsed: 2,
      maxAttempts: 4,
      actualCount: 30,
      stoppedEarly: false,
      bestDiagnosticReason: 'best reason',
      fallbackDiagnosticReason: 'sample reason'
    });

    assert.deepStrictEqual(plan, {
      validationPassed: true,
      actualCount: 30,
      retryCount: 2,
      effectiveDiagnosticReason: 'best reason',
      logMessages: []
    });
  });

  it('builds a failure return plan and reuses attempt finalization logs', () => {
    const plan = resolveIndexValidationReturnPlan({
      accepted: false,
      strictPageLock: true,
      expectedCount: 30,
      pageNumber: 4,
      attemptsUsed: 3,
      maxAttempts: 4,
      actualCount: 29,
      stoppedEarly: true,
      bestDiagnosticReason: '',
      fallbackDiagnosticReason: 'fallback reason'
    });

    assert.strictEqual(plan.validationPassed, false);
    assert.strictEqual(plan.actualCount, 29);
    assert.strictEqual(plan.retryCount, 3);
    assert.strictEqual(plan.effectiveDiagnosticReason, 'fallback reason');
    assert.strictEqual(plan.logMessages.length, 2);
    assert.ok(plan.logMessages[0].length > 0);
  });
});
