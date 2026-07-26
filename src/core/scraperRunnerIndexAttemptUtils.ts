// @ts-nocheck
const scraperRunnerRecoveryPipelineUtils_1 = require('./scraperRunnerRecoveryPipelineUtils');
const scraperRunnerIndexValidationUtils_1 = require('./scraperRunnerIndexValidationUtils');

export interface PageLockRetryTracker {
  lastSampleCount: number | null;
  stagnantAttempts: number;
}

export interface IndexValidationAttemptDecisionInput {
  tracker: PageLockRetryTracker;
  strictPageLock: boolean;
  expectedCount: number | null;
  isLastTargetPage: boolean;
  pageNumber: number;
  attempt: number;
  maxAttempts: number;
  phase: string;
  sampleCount: number;
  mergedCount: number;
  previousBestCount: number;
}

export interface IndexValidationAttemptDecision {
  tracker: PageLockRetryTracker;
  accepted: boolean;
  stoppedEarly: boolean;
  shouldRetry: boolean;
  retryDelayMs: number;
  logMessages: string[];
}

export interface IndexValidationAttemptFinalizationInput {
  strictPageLock: boolean;
  expectedCount: number | null;
  pageNumber: number;
  attemptsUsed: number;
  maxAttempts: number;
  bestCount: number;
  stoppedEarly: boolean;
}

export interface IndexValidationAttemptFinalization {
  logMessages: string[];
}

export function resolveIndexValidationAttemptDecision(
  input: IndexValidationAttemptDecisionInput
): IndexValidationAttemptDecision {
  const accepted = (0, scraperRunnerIndexValidationUtils_1.shouldAcceptIndexValidationResult)({
    expectedCount: input.expectedCount,
    strictPageLock: input.strictPageLock,
    isLastTargetPage: input.isLastTargetPage,
    bestCount: input.mergedCount
  });

  if (accepted) {
    return {
      tracker: input.tracker,
      accepted: true,
      stoppedEarly: false,
      shouldRetry: false,
      retryDelayMs: 0,
      logMessages: []
    };
  }

  const retryEvaluation = (0, scraperRunnerRecoveryPipelineUtils_1.evaluatePageValidationRetry)({
    tracker: input.tracker,
    strictPageLock: input.strictPageLock,
    expectedCount: input.expectedCount,
    pageNumber: input.pageNumber,
    attempt: input.attempt,
    maxAttempts: input.maxAttempts,
    sampleCount: input.sampleCount,
    mergedCount: input.mergedCount,
    previousBestCount: input.previousBestCount
  });

  const logMessages = [retryEvaluation.logMessage];
  if (retryEvaluation.shouldStopEarly && retryEvaluation.earlyStopMessage) {
    logMessages.push(retryEvaluation.earlyStopMessage);
  }

  const shouldRetry = !retryEvaluation.shouldStopEarly && input.attempt < input.maxAttempts;

  return {
    tracker: retryEvaluation.tracker,
    accepted: false,
    stoppedEarly: retryEvaluation.shouldStopEarly,
    shouldRetry,
    retryDelayMs: shouldRetry
      ? (0, scraperRunnerIndexValidationUtils_1.resolveIndexValidationRetryDelayMs)({
          strictPageLock: input.strictPageLock,
          attempt: input.attempt,
          phase: input.phase
        })
      : 0,
    logMessages
  };
}

export function finalizeIndexValidationAttempts(
  input: IndexValidationAttemptFinalizationInput
): IndexValidationAttemptFinalization {
  const exhaustedMessage = (0, scraperRunnerRecoveryPipelineUtils_1.buildPageValidationExhaustedMessage)({
    strictPageLock: input.strictPageLock,
    expectedCount: input.expectedCount,
    pageNumber: input.pageNumber,
    attemptsUsed: input.attemptsUsed,
    maxAttempts: input.maxAttempts,
    bestCount: input.bestCount,
    stoppedEarly: input.stoppedEarly
  });

  const logMessages = [];
  if (exhaustedMessage) {
    logMessages.push(exhaustedMessage);
  }
  logMessages.push(`第 ${input.pageNumber} 页重试后仍未达到预期条数，使用本轮最佳结果 ${input.bestCount} 条。`);

  return {
    logMessages
  };
}
