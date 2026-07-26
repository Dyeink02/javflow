import {
  resolveIndexProcessingDecision,
  resolveIndexQueueLimitDecision,
  type IndexProcessingDecision
} from './scraperRunnerIndexUtils';

export interface IndexPageExecutionPlanInput {
  currentPage: number;
  targetTotalPages: number;
  expectedCount: number | null;
  linksCount: number;
  trackedLinksCount: number;
  newLinksCount: number;
  filmsQueued: number;
  filmLimit: number;
  resumeExisting: boolean;
  currentExpectedItemsPerPage: number | null;
}

export interface IndexPageExecutionPlan {
  shouldSetExpectedItemsPerPage: boolean;
  expectedItemsPerPageValue: number | null;
  logMessages: string[];
  preQueueDecision: IndexProcessingDecision | null;
  queueCount: number;
  postQueueDecision: IndexProcessingDecision | null;
}

export function resolveIndexPageExecutionPlan(
  input: IndexPageExecutionPlanInput
): IndexPageExecutionPlan {
  const logMessages: string[] = [];
  const shouldSetExpectedItemsPerPage =
    input.currentExpectedItemsPerPage === null && input.linksCount > 0;

  const skippedPersistedCount = input.trackedLinksCount - input.newLinksCount;
  if (skippedPersistedCount > 0) {
    logMessages.push(`宸茶烦杩?${skippedPersistedCount} 涓凡瀹屾垚褰辩墖锛屼粎琛ユ姄鏈畬鎴愬唴瀹广€俙`);
  }

  if (input.trackedLinksCount > 0 && input.trackedLinksCount < input.linksCount) {
    logMessages.push(
      `褰撳墠涓洪檺閲忔ā寮忥紝鏈〉浠呰拷韪墠 ${input.trackedLinksCount} 涓摼鎺ワ紝鍓╀綑 ${input.linksCount - input.trackedLinksCount} 涓摼鎺ヤ笉璁″叆鏈浠诲姟銆俙`
    );
  }

  const expectedItemsPerPageValue = shouldSetExpectedItemsPerPage
    ? input.linksCount
    : input.currentExpectedItemsPerPage;

  const buildDecision = (newLinksCount: number, filmsQueued: number): IndexProcessingDecision =>
    resolveIndexProcessingDecision({
      currentPage: input.currentPage,
      targetTotalPages: input.targetTotalPages,
      expectedCount: input.expectedCount,
      linksCount: input.linksCount,
      newLinksCount,
      resumeExisting: input.resumeExisting,
      filmLimit: input.filmLimit,
      filmsQueued
    });

  if (input.linksCount === 0) {
    return {
      shouldSetExpectedItemsPerPage: false,
      expectedItemsPerPageValue,
      logMessages,
      preQueueDecision: buildDecision(0, input.filmsQueued),
      queueCount: 0,
      postQueueDecision: null
    };
  }

  if (input.newLinksCount === 0) {
    return {
      shouldSetExpectedItemsPerPage,
      expectedItemsPerPageValue,
      logMessages,
      preQueueDecision: buildDecision(0, input.filmsQueued),
      queueCount: 0,
      postQueueDecision: null
    };
  }

  if (input.filmLimit > 0 && input.filmsQueued >= input.filmLimit) {
    return {
      shouldSetExpectedItemsPerPage,
      expectedItemsPerPageValue,
      logMessages,
      preQueueDecision: buildDecision(input.newLinksCount, input.filmsQueued),
      queueCount: 0,
      postQueueDecision: null
    };
  }

  const queueDecision = resolveIndexQueueLimitDecision({
    filmLimit: input.filmLimit,
    filmsQueued: input.filmsQueued,
    newLinksCount: input.newLinksCount
  });
  const queueCount = queueDecision.queueCount;
  const filmsQueuedAfterQueue = input.filmsQueued + queueCount;

  return {
    shouldSetExpectedItemsPerPage,
    expectedItemsPerPageValue,
    logMessages,
    preQueueDecision: null,
    queueCount,
    postQueueDecision: buildDecision(queueCount, filmsQueuedAfterQueue)
  };
}
