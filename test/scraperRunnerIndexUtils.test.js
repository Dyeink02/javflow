const assert = require('assert');

const {
  getInferredTotalPages,
  resolveIndexTargetPageState,
  resolveIndexQueueLimitDecision,
  resolveIndexProcessingDecision,
  shouldWarnSparseIndexPage
} = require('../dist/core/scraperRunnerIndexUtils');

describe('scraperRunnerIndexUtils index decisions', () => {
  it('infers target pages from the configured limit and expected page size', () => {
    assert.strictEqual(
      getInferredTotalPages({
        filmLimit: 65,
        expectedItemsPerPage: 30
      }),
      3
    );

    assert.deepStrictEqual(
      resolveIndexTargetPageState({
        currentPage: 3,
        configuredTotalPages: 0,
        filmLimit: 65,
        expectedItemsPerPage: 30
      }),
      {
        inferredTotalPages: 3,
        targetTotalPages: 3,
        isLastTargetPage: true
      }
    );
  });

  it('caps detail queueing at the remaining limit window', () => {
    assert.deepStrictEqual(
      resolveIndexQueueLimitDecision({
        filmLimit: 10,
        filmsQueued: 8,
        newLinksCount: 5
      }),
      {
        queueCount: 2,
        remainingSlots: 2,
        shouldStopBeforeQueue: false,
        shouldStopAfterQueue: true
      }
    );
  });

  it('continues after a recoverable empty gap before the target end', () => {
    assert.deepStrictEqual(
      resolveIndexProcessingDecision({
        currentPage: 2,
        targetTotalPages: 4,
        expectedCount: 30,
        linksCount: 0,
        newLinksCount: 0,
        resumeExisting: false,
        filmLimit: 0,
        filmsQueued: 0
      }),
      {
        action: 'continue_after_gap',
        shouldAdvancePage: true,
        shouldStopIndexing: false
      }
    );
  });

  it('keeps scanning later pages during resume when the current page is already complete', () => {
    assert.deepStrictEqual(
      resolveIndexProcessingDecision({
        currentPage: 5,
        targetTotalPages: 0,
        expectedCount: null,
        linksCount: 20,
        newLinksCount: 0,
        resumeExisting: true,
        filmLimit: 0,
        filmsQueued: 0
      }),
      {
        action: 'continue_resume_completed_page',
        shouldAdvancePage: true,
        shouldStopIndexing: false
      }
    );
  });

  it('warns on sparse non-terminal pages only when indexing is still active', () => {
    assert.strictEqual(
      shouldWarnSparseIndexPage({
        shouldStopIndexing: false,
        expectedItemsPerPage: 30,
        isLastTargetPage: false,
        linksCount: 12
      }),
      true
    );

    assert.strictEqual(
      shouldWarnSparseIndexPage({
        shouldStopIndexing: true,
        expectedItemsPerPage: 30,
        isLastTargetPage: false,
        linksCount: 12
      }),
      false
    );
  });
});
