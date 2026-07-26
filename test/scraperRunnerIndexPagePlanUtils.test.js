const assert = require('assert');

const {
  resolveIndexPageExecutionPlan
} = require('../dist/core/scraperRunnerIndexPagePlanUtils');

describe('scraperRunnerIndexPagePlanUtils', () => {
  it('builds a queue plan with log messages and a post-queue stop decision', () => {
    const plan = resolveIndexPageExecutionPlan({
      currentPage: 1,
      targetTotalPages: 3,
      expectedCount: 30,
      linksCount: 30,
      trackedLinksCount: 20,
      newLinksCount: 18,
      filmsQueued: 8,
      filmLimit: 10,
      resumeExisting: false,
      currentExpectedItemsPerPage: null
    });

    assert.strictEqual(plan.shouldSetExpectedItemsPerPage, true);
    assert.strictEqual(plan.expectedItemsPerPageValue, 30);
    assert.strictEqual(plan.logMessages.length, 2);
    assert.strictEqual(plan.preQueueDecision, null);
    assert.strictEqual(plan.queueCount, 2);
    assert.strictEqual(plan.postQueueDecision.action, 'stop_limit_reached');
  });

  it('returns a page-gap pre-queue decision when the page sample is empty', () => {
    const plan = resolveIndexPageExecutionPlan({
      currentPage: 2,
      targetTotalPages: 4,
      expectedCount: 30,
      linksCount: 0,
      trackedLinksCount: 0,
      newLinksCount: 0,
      filmsQueued: 0,
      filmLimit: 0,
      resumeExisting: false,
      currentExpectedItemsPerPage: null
    });

    assert.strictEqual(plan.preQueueDecision.action, 'continue_after_gap');
    assert.strictEqual(plan.queueCount, 0);
    assert.strictEqual(plan.postQueueDecision, null);
  });

  it('returns a no-new-links stop decision before queueing', () => {
    const plan = resolveIndexPageExecutionPlan({
      currentPage: 3,
      targetTotalPages: 0,
      expectedCount: null,
      linksCount: 20,
      trackedLinksCount: 20,
      newLinksCount: 0,
      filmsQueued: 0,
      filmLimit: 0,
      resumeExisting: false,
      currentExpectedItemsPerPage: 20
    });

    assert.strictEqual(plan.preQueueDecision.action, 'stop_no_new_links');
    assert.strictEqual(plan.queueCount, 0);
    assert.strictEqual(plan.postQueueDecision, null);
  });

  it('stops before queueing when the crawl limit is already exhausted', () => {
    const plan = resolveIndexPageExecutionPlan({
      currentPage: 5,
      targetTotalPages: 0,
      expectedCount: 30,
      linksCount: 30,
      trackedLinksCount: 30,
      newLinksCount: 6,
      filmsQueued: 10,
      filmLimit: 10,
      resumeExisting: false,
      currentExpectedItemsPerPage: 30
    });

    assert.strictEqual(plan.preQueueDecision.action, 'stop_limit_reached');
    assert.strictEqual(plan.queueCount, 0);
    assert.strictEqual(plan.postQueueDecision, null);
  });
});
