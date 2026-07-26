import {
  resolveIndexValidationAttemptDecision,
  type PageLockRetryTracker
} from './scraperRunnerIndexAttemptUtils';
import {
  resolveIndexValidationReturnPlan,
  type IndexValidationReturnPlan
} from './scraperRunnerIndexResultUtils';
import { resolveIndexValidationSampleProgress } from './scraperRunnerIndexSampleUtils';

export interface IndexValidationIterationPlanInput {
  previousBestCount: number;
  mergedCount: number;
  bestDiagnosticReason: string;
  sampleDiagnosticReason: string;
  tracker: PageLockRetryTracker;
  strictPageLock: boolean;
  expectedCount: number | null;
  isLastTargetPage: boolean;
  pageNumber: number;
  attempt: number;
  maxAttempts: number;
  phase: string;
  sampleCount: number;
}

export interface IndexValidationIterationPlan {
  currentBestCount: number;
  shouldPromoteMergedLinks: boolean;
  bestDiagnosticReason: string;
  tracker: PageLockRetryTracker;
  acceptedReturnPlan: IndexValidationReturnPlan | null;
  logMessages: string[];
  shouldStopEarly: boolean;
  shouldRetry: boolean;
  retryDelayMs: number;
}

export function resolveIndexValidationIterationPlan(
  input: IndexValidationIterationPlanInput
): IndexValidationIterationPlan {
  const sampleProgress = resolveIndexValidationSampleProgress({
    previousBestCount: input.previousBestCount,
    mergedCount: input.mergedCount,
    bestDiagnosticReason: input.bestDiagnosticReason,
    sampleDiagnosticReason: input.sampleDiagnosticReason
  });

  const attemptDecision = resolveIndexValidationAttemptDecision({
    tracker: input.tracker,
    strictPageLock: input.strictPageLock,
    expectedCount: input.expectedCount,
    isLastTargetPage: input.isLastTargetPage,
    pageNumber: input.pageNumber,
    attempt: input.attempt,
    maxAttempts: input.maxAttempts,
    phase: input.phase,
    sampleCount: input.sampleCount,
    mergedCount: sampleProgress.currentBestCount,
    previousBestCount: input.previousBestCount
  });

  return {
    currentBestCount: sampleProgress.currentBestCount,
    shouldPromoteMergedLinks: sampleProgress.shouldPromoteMergedLinks,
    bestDiagnosticReason: sampleProgress.bestDiagnosticReason,
    tracker: attemptDecision.tracker,
    acceptedReturnPlan: attemptDecision.accepted
      ? resolveIndexValidationReturnPlan({
          accepted: true,
          strictPageLock: input.strictPageLock,
          expectedCount: input.expectedCount,
          pageNumber: input.pageNumber,
          attemptsUsed: input.attempt,
          maxAttempts: input.maxAttempts,
          actualCount: sampleProgress.currentBestCount,
          stoppedEarly: false,
          bestDiagnosticReason: sampleProgress.bestDiagnosticReason,
          fallbackDiagnosticReason: input.sampleDiagnosticReason
        })
      : null,
    logMessages: attemptDecision.logMessages,
    shouldStopEarly: attemptDecision.stoppedEarly,
    shouldRetry: attemptDecision.shouldRetry,
    retryDelayMs: attemptDecision.retryDelayMs
  };
}
