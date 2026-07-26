// @ts-nocheck
const scraperRunnerIndexAttemptUtils_1 = require('./scraperRunnerIndexAttemptUtils');
const scraperRunnerIndexSampleUtils_1 = require('./scraperRunnerIndexSampleUtils');

export interface IndexValidationReturnPlanInput {
  accepted: boolean;
  strictPageLock: boolean;
  expectedCount: number | null;
  pageNumber: number;
  attemptsUsed: number;
  maxAttempts: number;
  actualCount: number;
  stoppedEarly: boolean;
  bestDiagnosticReason: string;
  fallbackDiagnosticReason: string;
}

export interface IndexValidationReturnPlan {
  validationPassed: boolean;
  actualCount: number;
  retryCount: number;
  effectiveDiagnosticReason: string;
  logMessages: string[];
}

export function resolveIndexValidationReturnPlan(
  input: IndexValidationReturnPlanInput
): IndexValidationReturnPlan {
  const effectiveDiagnosticReason = (0, scraperRunnerIndexSampleUtils_1.resolveIndexValidationEffectiveDiagnosticReason)(
    input.bestDiagnosticReason,
    input.fallbackDiagnosticReason
  );

  if (input.accepted) {
    return {
      validationPassed: true,
      actualCount: input.actualCount,
      retryCount: input.attemptsUsed,
      effectiveDiagnosticReason,
      logMessages: []
    };
  }

  const finalization = (0, scraperRunnerIndexAttemptUtils_1.finalizeIndexValidationAttempts)({
    strictPageLock: input.strictPageLock,
    expectedCount: input.expectedCount,
    pageNumber: input.pageNumber,
    attemptsUsed: input.attemptsUsed,
    maxAttempts: input.maxAttempts,
    bestCount: input.actualCount,
    stoppedEarly: input.stoppedEarly
  });

  return {
    validationPassed: false,
    actualCount: input.actualCount,
    retryCount: input.attemptsUsed,
    effectiveDiagnosticReason,
    logMessages: finalization.logMessages
  };
}
