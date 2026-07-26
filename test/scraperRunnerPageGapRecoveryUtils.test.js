const assert = require('assert');

const {
  resolvePageGapPassStart,
  resolvePageGapAuditFollowUp,
  resolvePageGapPassEnd
} = require('../dist/core/scraperRunnerPageGapRecoveryUtils');

describe('scraperRunnerPageGapRecoveryUtils', () => {
  it('stops a recovery pass early when there are no pending audits', () => {
    const decision = resolvePageGapPassStart(0, 2);
    assert.deepStrictEqual(decision, {
      shouldRunPass: false,
      stopRecovery: true,
      logMessage: '分页缺口补查完成，所有已知页面均达到预期条数。'
    });
  });

  it('builds follow-up actions for page gap audit results', () => {
    assert.deepStrictEqual(resolvePageGapAuditFollowUp({
      pageNumber: 4,
      expectedCount: 30,
      mergedActualCount: 30,
      newLinksCount: 2,
      validationPassed: true
    }), {
      action: 'enqueue_new_links',
      logMessage: '第 4 页补查新增 2 个影片链接，已加入详情队列。'
    });

    assert.deepStrictEqual(resolvePageGapAuditFollowUp({
      pageNumber: 4,
      expectedCount: 30,
      mergedActualCount: 30,
      newLinksCount: 0,
      validationPassed: true
    }), {
      action: 'validated',
      logMessage: '第 4 页分页缺口补查通过。'
    });

    assert.deepStrictEqual(resolvePageGapAuditFollowUp({
      pageNumber: 4,
      expectedCount: 30,
      mergedActualCount: 27,
      newLinksCount: 0,
      validationPassed: false
    }), {
      action: 'incomplete',
      logMessage: '第 4 页补查后仍为 27/30。'
    });
  });

  it('resolves pass end state for completed, stagnant and continuing passes', () => {
    assert.deepStrictEqual(resolvePageGapPassEnd(0, 3), {
      status: 'completed',
      stopRecovery: true,
      logMessage: '分页缺口补查已完成，当前所有目标页面均达到预期条数。'
    });

    assert.deepStrictEqual(resolvePageGapPassEnd(2, 0), {
      status: 'stagnant',
      stopRecovery: true,
      logMessage: '本轮分页缺口补查未提升结果，停止继续重复补查，避免影响抓取体验。'
    });

    assert.deepStrictEqual(resolvePageGapPassEnd(2, 1), {
      status: 'continue',
      stopRecovery: false,
      logMessage: ''
    });
  });
});
