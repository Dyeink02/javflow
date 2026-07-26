const assert = require('assert');

const {
  countBudgetExhaustedDetailEntries,
  resolveDetailRecoveryPassStart,
  resolveDetailRecoveryPassEnd
} = require('../dist/core/scraperRunnerDetailRecoveryUtils');

describe('scraperRunnerDetailRecoveryUtils', () => {
  it('counts budget exhausted detail entries', () => {
    const count = countBudgetExhaustedDetailEntries([
      { attemptsUsed: 0, budget: 0 },
      { attemptsUsed: 1, budget: 2 },
      { attemptsUsed: 2, budget: 2 },
      { attemptsUsed: 3, budget: 1 }
    ]);

    assert.strictEqual(count, 3);
  });

  it('stops the pass early when all missing details are already completed', () => {
    const decision = resolveDetailRecoveryPassStart({
      pass: 2,
      missingCount: 0,
      recoverableCount: 0,
      budgetExhaustedCount: 0
    });

    assert.deepStrictEqual(decision, {
      status: 'completed',
      shouldRunPass: false,
      stopRecovery: true,
      logMessages: ['补爬校验通过，所有已入队影片均已处理完成。']
    });
  });

  it('stops the pass early when no recoverable detail links remain', () => {
    const decision = resolveDetailRecoveryPassStart({
      pass: 1,
      missingCount: 4,
      recoverableCount: 0,
      budgetExhaustedCount: 3
    });

    assert.deepStrictEqual(decision, {
      status: 'budget_exhausted',
      shouldRunPass: false,
      stopRecovery: true,
      logMessages: [
        '剩余未完成详情页已无可用重试预算，停止继续重复补爬。',
        '约 3 个未完成影片已达到补爬预算，本轮不再继续重复请求。'
      ]
    });
  });

  it('narrows the next pass to high-priority failures when remaining count stalls', () => {
    const decision = resolveDetailRecoveryPassEnd({
      pass: 2,
      previousMissingCount: 5,
      remainingCount: 5,
      nextRecoverableCount: 2
    });

    assert.deepStrictEqual(decision, {
      status: 'high_priority_retry',
      stopRecovery: false,
      logMessage: '第 2 轮补爬后未完成数量没有下降，但仍存在可恢复失败项，下一轮将仅重试高优先级失败详情页。'
    });
  });

  it('stops recovery when stalled items have also exhausted their recovery budget', () => {
    const decision = resolveDetailRecoveryPassEnd({
      pass: 3,
      previousMissingCount: 4,
      remainingCount: 4,
      nextRecoverableCount: 0
    });

    assert.deepStrictEqual(decision, {
      status: 'budget_exhausted',
      stopRecovery: true,
      logMessage: '当前未完成影片已全部耗尽重试预算，停止继续补爬。'
    });
  });
});
