const assert = require('assert');

const {
  resolveIndexProcessingActionPlan
} = require('../dist/core/scraperRunnerIndexActionUtils');

describe('scraperRunnerIndexActionUtils', () => {
  it('advances to the next page when continuing after a page gap', () => {
    const plan = resolveIndexProcessingActionPlan({
      action: 'continue_after_gap',
      shouldStopIndexing: false,
      currentPage: 2,
      targetTotalPages: 5
    });

    assert.strictEqual(plan.shouldContinueCurrentLoop, true);
    assert.strictEqual(plan.nextPageNumber, 3);
    assert.strictEqual(plan.shouldPersistState, true);
    assert.ok(plan.logMessage.length > 0);
  });

  it('keeps the current page when stopping on an empty page', () => {
    const plan = resolveIndexProcessingActionPlan({
      action: 'stop_empty_page',
      shouldStopIndexing: true,
      currentPage: 4,
      targetTotalPages: 4
    });

    assert.strictEqual(plan.shouldStopIndexing, true);
    assert.strictEqual(plan.shouldContinueCurrentLoop, true);
    assert.strictEqual(plan.nextPageNumber, 4);
    assert.strictEqual(plan.shouldPersistState, true);
  });

  it('logs the limit-reached action without forcing an early continue', () => {
    const plan = resolveIndexProcessingActionPlan({
      action: 'stop_limit_reached',
      shouldStopIndexing: true,
      currentPage: 6,
      targetTotalPages: 0
    });

    assert.strictEqual(plan.shouldStopIndexing, true);
    assert.strictEqual(plan.shouldContinueCurrentLoop, false);
    assert.strictEqual(plan.nextPageNumber, 6);
    assert.strictEqual(plan.shouldPersistState, false);
    assert.ok(plan.logMessage.length > 0);
  });

  it('falls back to a no-op plan for unrecognized actions', () => {
    const plan = resolveIndexProcessingActionPlan({
      action: 'unknown',
      shouldStopIndexing: false,
      currentPage: 1,
      targetTotalPages: 0
    });

    assert.deepStrictEqual(plan, {
      shouldStopIndexing: false,
      shouldContinueCurrentLoop: false,
      nextPageNumber: 1,
      logMessage: '',
      shouldPersistState: false,
      stateReason: '',
      stateMessage: ''
    });
  });
});
