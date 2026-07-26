const assert = require('assert');

const {
  INDEX_VALIDATION_PHASE_INITIAL,
  INDEX_VALIDATION_PHASE_RECOVERY,
  normalizeIndexValidationPhase,
  shouldEnforceExactPageValidation,
  resolveStrictIndexPageRetryLimit,
  resolveIndexValidationPolicy,
  resolveIndexValidationRetryDelayMs,
  shouldAcceptIndexValidationResult
} = require('../dist/core/scraperRunnerIndexValidationUtils');

describe('scraperRunnerIndexValidationUtils', () => {
  it('normalizes index validation phases', () => {
    assert.strictEqual(normalizeIndexValidationPhase(''), INDEX_VALIDATION_PHASE_INITIAL);
    assert.strictEqual(normalizeIndexValidationPhase('initial'), INDEX_VALIDATION_PHASE_INITIAL);
    assert.strictEqual(normalizeIndexValidationPhase('recovery'), INDEX_VALIDATION_PHASE_RECOVERY);
    assert.strictEqual(normalizeIndexValidationPhase('anything'), INDEX_VALIDATION_PHASE_RECOVERY);
  });

  it('builds strict validation policy with phase-aware retry limits', () => {
    assert.strictEqual(shouldEnforceExactPageValidation(250, 30), true);
    assert.strictEqual(resolveStrictIndexPageRetryLimit('initial', 5, true), 2);
    assert.strictEqual(resolveStrictIndexPageRetryLimit('recovery', 5, false), 4);

    assert.deepStrictEqual(resolveIndexValidationPolicy({
      limit: 250,
      expectedCount: 30,
      phase: 'initial',
      indexPageRetryLimit: 3,
      strictIndexPageRetryLimit: 5,
      largeTaskMode: true
    }), {
      phase: 'initial',
      strictPageLock: true,
      maxAttempts: 2
    });
  });

  it('falls back to loose retry policy when exact page validation is not required', () => {
    assert.deepStrictEqual(resolveIndexValidationPolicy({
      limit: 0,
      expectedCount: null,
      phase: 'recovery',
      indexPageRetryLimit: 3,
      strictIndexPageRetryLimit: 5,
      largeTaskMode: false
    }), {
      phase: 'recovery',
      strictPageLock: false,
      maxAttempts: 3
    });
  });

  it('returns phase-aware retry delays and result acceptance', () => {
    assert.strictEqual(resolveIndexValidationRetryDelayMs({
      strictPageLock: true,
      attempt: 2,
      phase: 'initial'
    }), 1500);
    assert.strictEqual(resolveIndexValidationRetryDelayMs({
      strictPageLock: true,
      attempt: 3,
      phase: 'recovery'
    }), 3300);
    assert.strictEqual(resolveIndexValidationRetryDelayMs({
      strictPageLock: false,
      attempt: 3,
      phase: 'initial'
    }), 1500);

    assert.strictEqual(shouldAcceptIndexValidationResult({
      expectedCount: 30,
      strictPageLock: false,
      isLastTargetPage: true,
      bestCount: 18
    }), true);
    assert.strictEqual(shouldAcceptIndexValidationResult({
      expectedCount: 30,
      strictPageLock: true,
      isLastTargetPage: false,
      bestCount: 29
    }), false);
  });
});
