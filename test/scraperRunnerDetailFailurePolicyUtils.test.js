const assert = require('assert');

const {
  classifyDetailFailure,
  getDetailRecoveryBudget,
  buildRecoveryCategorySummary,
  getRecoverableMissingDetailLinks
} = require('../dist/core/scraperRunnerDetailFailurePolicyUtils');

describe('scraperRunnerDetailFailurePolicyUtils', () => {
  it('classifies blocked and stopped detail failures', () => {
    assert.deepStrictEqual(classifyDetailFailure('Cloudflare challenge 403').key, 'blocked');
    assert.deepStrictEqual(classifyDetailFailure('用户主动终止').key, 'stopped');
  });

  it('calculates detail recovery budgets by policy and task size', () => {
    const blockedPolicy = classifyDetailFailure('age verification');
    const unknownPolicy = classifyDetailFailure('other reason');

    assert.strictEqual(getDetailRecoveryBudget(blockedPolicy, false), 4);
    assert.strictEqual(getDetailRecoveryBudget(blockedPolicy, true), 3);
    assert.strictEqual(getDetailRecoveryBudget(unknownPolicy, true), 2);
  });

  it('builds a recovery summary grouped by failure category', () => {
    const summary = buildRecoveryCategorySummary(
      ['a', 'b', 'c'],
      (link) =>
        ({
          a: 'Cloudflare challenge',
          b: 'timeout',
          c: 'timeout'
        })[link] || ''
    );

    assert.strictEqual(summary, '验证拦截 1 条，网络超时 2 条');
  });

  it('keeps only recoverable links and sorts them by priority and attempts', () => {
    const links = getRecoverableMissingDetailLinks({
      links: ['c', 'b', 'a', 'd'],
      failedDetailMap: new Map([
        ['a', { reason: 'timeout', retryCount: 1 }],
        ['b', { reason: 'Cloudflare challenge', retryCount: 1 }],
        ['c', { reason: 'parse metadata', retryCount: 3 }],
        ['d', { reason: '用户主动终止', retryCount: 0 }]
      ]),
      detailRecoveryAttemptMap: new Map([
        ['a', 1],
        ['b', 0],
        ['c', 1],
        ['d', 0]
      ]),
      getPolicy: classifyDetailFailure,
      getRecoveryBudget: (policy) => getDetailRecoveryBudget(policy, false)
    });

    assert.deepStrictEqual(links, ['b', 'a']);
  });
});
