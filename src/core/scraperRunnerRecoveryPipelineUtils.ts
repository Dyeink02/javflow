import type { PageAuditRecord } from './taskStateManager';

export interface RecoveryQueueDrainTarget {
  areWorkQueuesFinished(): boolean;
  waitForDelays(): Promise<void>;
}

export interface RecoveryStatusMessages {
  logMessage: string;
  stateMessage: string;
  stateReason: string;
}

export interface PageLockRetryTracker {
  lastSampleCount: number | null;
  stagnantAttempts: number;
}

export interface PageValidationRetryEvaluationInput {
  tracker: PageLockRetryTracker;
  strictPageLock: boolean;
  expectedCount: number | null;
  pageNumber: number;
  attempt: number;
  maxAttempts: number;
  sampleCount: number;
  mergedCount: number;
  previousBestCount: number;
}

export interface PageValidationRetryEvaluationResult {
  tracker: PageLockRetryTracker;
  logMessage: string;
  shouldStopEarly: boolean;
  earlyStopMessage: string;
}

export interface PageGapMergeInput {
  expectedCount: number;
  currentActualCount: number;
  fetchedActualCount: number;
  newLinksCount: number;
}

export interface PageGapMergeResult {
  mergedActualCount: number;
  recoveredCount: number;
  validationPassed: boolean;
  reason: string;
}

export function createPageLockRetryTracker(): PageLockRetryTracker {
  return {
    lastSampleCount: null,
    stagnantAttempts: 0
  };
}

export async function waitForRecoveryQueueDrain(params: {
  target: RecoveryQueueDrainTarget;
  shouldStop: () => boolean;
  intervalMs?: number;
}): Promise<void> {
  const { target, shouldStop, intervalMs = 500 } = params;

  while (!target.areWorkQueuesFinished() && !shouldStop()) {
    await new Promise((resolve) => setTimeout(resolve, intervalMs));
  }

  if (!shouldStop()) {
    await target.waitForDelays();
  }
}

export function buildQueueGapRecoveryMessages(queueGapCount: number): RecoveryStatusMessages {
  const stateMessage = `\u53D1\u73B0 ${queueGapCount} \u4E2A\u5165\u961F\u7F3A\u53E3\uFF0C\u6B63\u5728\u8865\u52A0\u5165\u961F\u3002`;
  return {
    logMessage: `\u68C0\u6D4B\u5230 ${queueGapCount} \u4E2A\u5206\u9875\u89E3\u6790\u7ED3\u679C\u5C1A\u672A\u5165\u961F\uFF0C\u5F00\u59CB\u6267\u884C\u5168\u91CF\u5BF9\u8D26\u8865\u52A0\u5165\u961F\u3002`,
    stateMessage,
    stateReason: '\u6267\u884C\u5165\u961F\u7F3A\u53E3\u8865\u9F50'
  };
}

export function buildQueueGapRemainingMessage(remainingCount: number): string {
  return `\u5165\u961F\u7F3A\u53E3\u8865\u9F50\u540E\u4ECD\u6709 ${remainingCount} \u4E2A\u756A\u53F7\u672A\u6210\u529F\u5165\u961F\uFF0C\u8BF7\u67E5\u770B\u672A\u6293\u53D6\u756A\u53F7\u9762\u677F\u3002`;
}

export function buildPageGapRecoveryMessages(params: {
  pendingCount: number;
  pass: number;
  totalPasses: number;
}): RecoveryStatusMessages {
  const { pendingCount, pass, totalPasses } = params;
  const stateMessage = `\u68C0\u6D4B\u5230 ${pendingCount} \u4E2A\u5206\u9875\u7F3A\u53E3\uFF0C\u6B63\u5728\u8FDB\u884C\u7B2C ${pass} \u8F6E\u8865\u67E5\u3002`;
  return {
    logMessage: `\u68C0\u6D4B\u5230 ${pendingCount} \u4E2A\u5206\u9875\u7F3A\u53E3\uFF0C\u5F00\u59CB\u7B2C ${pass}/${totalPasses} \u8F6E\u8865\u67E5\u3002`,
    stateMessage,
    stateReason: '\u6267\u884C\u5206\u9875\u7F3A\u53E3\u8865\u67E5'
  };
}

export function buildPageGapRemainingMessage(remainingCount: number): string {
  return `\u5206\u9875\u7F3A\u53E3\u8865\u67E5\u7ED3\u675F\u540E\u4ECD\u6709 ${remainingCount} \u9875\u672A\u8FBE\u5230\u9884\u671F\u6761\u6570\uFF0C\u8BF7\u67E5\u770B\u201C\u5206\u9875\u7F3A\u53E3\u201D\u4E0E\u201C\u672A\u6293\u53D6\u756A\u53F7\u201D\u9762\u677F\u3002`;
}

export function buildPageGapActiveLabel(
  audit: Pick<PageAuditRecord, 'pageNumber' | 'actualCount' | 'expectedCount'>
): string {
  return `\u5206\u9875\u8865\u67E5\u7B2C ${audit.pageNumber} \u9875\uFF08\u5F53\u524D ${audit.actualCount}/${audit.expectedCount || 0}\uFF09`;
}

export function buildPageGapActiveStateMessage(pageNumber: number): string {
  return `\u6B63\u5728\u8865\u67E5\u7B2C ${pageNumber} \u9875\u5206\u9875\u7F3A\u53E3\u3002`;
}

export function mergePageGapRecoveryResult(input: PageGapMergeInput): PageGapMergeResult {
  const { expectedCount, currentActualCount, fetchedActualCount, newLinksCount } = input;
  const mergedActualCount = Math.min(
    expectedCount,
    Math.max(currentActualCount, fetchedActualCount, currentActualCount + newLinksCount)
  );
  const validationPassed = mergedActualCount >= expectedCount;
  const recoveredCount = Math.max(0, mergedActualCount - currentActualCount);

  let reason = '\u5206\u9875\u8865\u67E5\u540E\u7ED3\u679C\u5DF2\u66F4\u65B0\u3002';
  if (newLinksCount > 0 && validationPassed) {
    reason = `\u8865\u67E5\u6210\u529F\uFF0C\u65B0\u589E ${newLinksCount} \u6761\u5E76\u8FBE\u5230\u9884\u671F\u6761\u6570\u3002`;
  } else if (newLinksCount > 0) {
    reason = `\u8865\u67E5\u540E\u65B0\u589E ${newLinksCount} \u6761\uFF0C\u4F46\u4ECD\u7F3A\u5C11 ${expectedCount - mergedActualCount} \u6761\u3002`;
  } else if (validationPassed) {
    reason = '\u8865\u67E5\u540E\u5DF2\u8FBE\u5230\u9884\u671F\u6761\u6570\u3002';
  } else {
    reason = `\u8865\u67E5\u540E\u4ECD\u7F3A\u5C11 ${expectedCount - mergedActualCount} \u6761\u3002`;
  }

  return {
    mergedActualCount,
    recoveredCount,
    validationPassed,
    reason
  };
}

export function evaluatePageValidationRetry(
  input: PageValidationRetryEvaluationInput
): PageValidationRetryEvaluationResult {
  const {
    tracker,
    strictPageLock,
    expectedCount,
    pageNumber,
    attempt,
    maxAttempts,
    sampleCount,
    mergedCount,
    previousBestCount
  } = input;

  const mergedImproved = mergedCount > previousBestCount;
  const sampleChanged = tracker.lastSampleCount === null || sampleCount !== tracker.lastSampleCount;
  const nextTracker: PageLockRetryTracker = {
    lastSampleCount: sampleCount,
    stagnantAttempts: mergedImproved || sampleChanged ? 0 : tracker.stagnantAttempts + 1
  };

  if (!strictPageLock) {
    return {
      tracker: nextTracker,
      logMessage: `\u7B2C ${pageNumber} \u9875\u7B2C ${attempt}/${maxAttempts} \u6B21\u6821\u9A8C\u672A\u901A\u8FC7\uFF1A\u671F\u671B ${expectedCount ?? 0} \u6761\uFF0C\u5B9E\u9645 ${sampleCount} \u6761\uFF0C\u5DF2\u5408\u5E76 ${mergedCount} \u6761\uFF0C\u51C6\u5907\u91CD\u8BD5...`,
      shouldStopEarly: false,
      earlyStopMessage: ''
    };
  }

  const logMessage = `\u7B2C ${pageNumber} \u9875\u7B2C ${attempt}/${maxAttempts} \u6B21\u9875\u9501\u5B9A\u6821\u9A8C\u672A\u901A\u8FC7\uFF1A\u671F\u671B\u81F3\u5C11 ${expectedCount} \u6761\uFF0C\u5F53\u524D\u5355\u6B21 ${sampleCount} \u6761\uFF0C\u5408\u5E76\u540E ${mergedCount} \u6761\uFF0C\u7EE7\u7EED\u9501\u5B9A\u5F53\u524D\u9875\u91CD\u6293\u3002`;
  const shouldStopEarly =
    expectedCount !== null && attempt < maxAttempts && nextTracker.stagnantAttempts >= 2;
  const earlyStopMessage = shouldStopEarly
    ? `\u7B2C ${pageNumber} \u9875\u9875\u9501\u5B9A\u8865\u67E5\u8FDE\u7EED 2 \u6B21\u65E0\u63D0\u5347\uFF1A\u671F\u671B\u81F3\u5C11 ${expectedCount} \u6761\uFF0C\u5F53\u524D\u5355\u6B21\u7A33\u5B9A\u5728 ${sampleCount} \u6761\uFF0C\u5408\u5E76\u540E\u7A33\u5B9A\u5728 ${mergedCount} \u6761\u3002\u5DF2\u63D0\u524D\u505C\u635F\uFF0C\u8BB0\u5F55\u4E3A\u5206\u9875\u7F3A\u53E3\u5E76\u7EE7\u7EED\u540E\u7EED\u6293\u53D6\u3002`
    : '';

  return {
    tracker: nextTracker,
    logMessage,
    shouldStopEarly,
    earlyStopMessage
  };
}

export function buildPageValidationExhaustedMessage(params: {
  strictPageLock: boolean;
  expectedCount: number | null;
  pageNumber: number;
  attemptsUsed: number;
  maxAttempts: number;
  bestCount: number;
  stoppedEarly: boolean;
}): string {
  const { strictPageLock, expectedCount, pageNumber, attemptsUsed, maxAttempts, bestCount, stoppedEarly } =
    params;

  if (!strictPageLock || expectedCount === null) {
    return '';
  }

  const missingCount = Math.max(expectedCount - bestCount, 0);
  if (stoppedEarly) {
    return `\u7B2C ${pageNumber} \u9875\u9875\u9501\u5B9A\u8865\u67E5\u5DF2\u89E6\u53D1\u505C\u635F\u65E9\u505C\uFF1A\u671F\u671B\u81F3\u5C11 ${expectedCount} \u6761\uFF0C${attemptsUsed}/${maxAttempts} \u6B21\u91CD\u6293\u540E\u4ECD\u4EC5 ${bestCount} \u6761\uFF0C\u4ECD\u7F3A\u5C11 ${missingCount} \u6761\u3002\u5DF2\u8BB0\u5F55\u4E3A\u5206\u9875\u7F3A\u53E3\u5E76\u7EE7\u7EED\u540E\u7EED\u6293\u53D6\uFF0C\u4EFB\u52A1\u7ED3\u675F\u540E\u4F1A\u518D\u6B21\u6C47\u603B\u539F\u56E0\u3002`;
  }

  return `\u7B2C ${pageNumber} \u9875\u9875\u9501\u5B9A\u6821\u9A8C\u5DF2\u8FBE\u5230\u6700\u5927\u91CD\u6293\u6B21\u6570\uFF1A\u671F\u671B\u81F3\u5C11 ${expectedCount} \u6761\uFF0C${attemptsUsed} \u6B21\u91CD\u6293\u5408\u5E76\u540E\u4EC5 ${bestCount} \u6761\uFF0C\u4ECD\u7F3A\u5C11 ${missingCount} \u6761\u3002\u5DF2\u8BB0\u5F55\u4E3A\u5206\u9875\u7F3A\u53E3\u5E76\u7EE7\u7EED\u540E\u7EED\u6293\u53D6\uFF0C\u4EFB\u52A1\u7ED3\u675F\u540E\u4F1A\u518D\u6B21\u8865\u67E5\u3002`;
}

export function buildDetailRecoveryMessages(params: {
  missingCount: number;
  pass: number;
  totalPasses: number;
  summary: string;
}): RecoveryStatusMessages {
  const { missingCount, pass, totalPasses, summary } = params;
  const stateMessage = `\u68C0\u6D4B\u5230 ${missingCount} \u4E2A\u5F71\u7247\u672A\u5B8C\u6210\uFF0C\u6B63\u5728\u8FDB\u884C\u7B2C ${pass} \u8F6E\u8865\u722C\u3002`;
  return {
    logMessage: `\u68C0\u6D4B\u5230 ${missingCount} \u4E2A\u5DF2\u5165\u961F\u5F71\u7247\u5C1A\u672A\u5B8C\u6210\uFF0C\u5F00\u59CB\u7B2C ${pass}/${totalPasses} \u8F6E\u8865\u722C\uFF08${summary}\uFF09\u3002`,
    stateMessage,
    stateReason: '\u6267\u884C\u8865\u722C'
  };
}

export function buildDetailBudgetStopMessages(budgetExhaustedCount: number): string[] {
  const messages = ['\u5269\u4F59\u672A\u5B8C\u6210\u8BE6\u60C5\u9875\u5DF2\u65E0\u53EF\u7528\u91CD\u8BD5\u9884\u7B97\uFF0C\u505C\u6B62\u7EE7\u7EED\u91CD\u590D\u8865\u722C\u3002'];
  if (budgetExhaustedCount > 0) {
    messages.push(`\u7EA6 ${budgetExhaustedCount} \u4E2A\u672A\u5B8C\u6210\u5F71\u7247\u5DF2\u8FBE\u5230\u8865\u722C\u9884\u7B97\uFF0C\u672C\u8F6E\u4E0D\u518D\u7EE7\u7EED\u91CD\u590D\u8BF7\u6C42\u3002`);
  }

  return messages;
}

export function buildDetailRecoveryRemainingMessage(remainingCount: number): string {
  return `\u8865\u722C\u7ED3\u675F\u540E\u4ECD\u6709 ${remainingCount} \u4E2A\u5F71\u7247\u672A\u5B8C\u6210\uFF0C\u8BF7\u67E5\u770B\u65E5\u5FD7\u786E\u8BA4\u8FD9\u4E9B\u94FE\u63A5\u662F\u5426\u6301\u7EED\u8BF7\u6C42\u5931\u8D25\u3002`;
}
