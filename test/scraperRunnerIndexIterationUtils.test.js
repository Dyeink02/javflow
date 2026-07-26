const assert = require('assert');

const {
  isRetryableIndexNetworkError,
  resolveIndexLoopSuccessPlan,
  resolveIndexLoopErrorPlan
} = require('../dist/core/scraperRunnerIndexIterationUtils');

describe('scraperRunnerIndexIterationUtils', () => {
  it('builds a success plan that prefetches the next page and warns on sparse pages', () => {
    const plan = resolveIndexLoopSuccessPlan({
      currentPage: 2,
      nextPageDelayMs: 4200,
      shouldStopIndexing: false,
      isStopping: false,
      targetTotalPages: 4,
      expectedItemsPerPage: 30,
      isLastTargetPage: false,
      linksCount: 12
    });

    assert.deepStrictEqual(plan, {
      shouldPrefetchNextPage: true,
      nextPrefetchPageNumber: 3,
      shouldWarnSparsePage: true,
      sparseWarningMessage: '第 2 页条数偏低（12/30）。当前为普通模式，仅记录异常并继续尝试后续页面。',
      nextPageDelayMs: 4200,
      nextPageNumber: 3,
      stateReason: '索引页处理完成',
      stateMessage: '第 2 页已处理完成。',
      delayLogMessage: '下一页抓取前等待 4 秒...'
    });
  });

  it('stops prefetch and sparse warnings when indexing has reached a stop state', () => {
    const plan = resolveIndexLoopSuccessPlan({
      currentPage: 3,
      nextPageDelayMs: 800,
      shouldStopIndexing: true,
      isStopping: false,
      targetTotalPages: 3,
      expectedItemsPerPage: 30,
      isLastTargetPage: true,
      linksCount: 12
    });

    assert.strictEqual(plan.shouldPrefetchNextPage, false);
    assert.strictEqual(plan.shouldWarnSparsePage, false);
    assert.strictEqual(plan.sparseWarningMessage, '');
  });

  it('classifies retryable network errors for index loop backoff', () => {
    assert.strictEqual(isRetryableIndexNetworkError('socket ETIMEDOUT while requesting page'), true);
    assert.strictEqual(isRetryableIndexNetworkError('unexpected parse failure'), false);
  });

  it('builds error wait plans for network and generic failures', () => {
    assert.deepStrictEqual(
      resolveIndexLoopErrorPlan({
        currentPage: 6,
        message: 'ETIMEDOUT',
        networkBackoffDelayMs: 12000,
        genericDelayMs: 7000
      }),
      {
        stateReason: '索引页抓取失败',
        stateMessage: '第 6 页抓取失败: ETIMEDOUT',
        delayMs: 12000,
        retryLogMessage: '检测到网络异常，将在 12 秒后重试...'
      }
    );

    assert.deepStrictEqual(
      resolveIndexLoopErrorPlan({
        currentPage: 6,
        message: 'parse failed',
        networkBackoffDelayMs: 12000,
        genericDelayMs: 7000
      }),
      {
        stateReason: '索引页抓取失败',
        stateMessage: '第 6 页抓取失败: parse failed',
        delayMs: 7000,
        retryLogMessage: ''
      }
    );
  });
});
