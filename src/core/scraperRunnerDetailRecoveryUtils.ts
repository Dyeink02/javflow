import { buildDetailBudgetStopMessages } from './scraperRunnerRecoveryPipelineUtils';

export interface DetailRecoveryBudgetEntry {
  attemptsUsed: number;
  budget: number;
}

export interface DetailRecoveryPassStartInput {
  pass: number;
  missingCount: number;
  recoverableCount: number;
  budgetExhaustedCount: number;
}

export interface DetailRecoveryPassStartDecision {
  status: 'completed' | 'budget_exhausted' | 'continue';
  shouldRunPass: boolean;
  stopRecovery: boolean;
  logMessages: string[];
}

export interface DetailRecoveryPassEndInput {
  pass: number;
  previousMissingCount: number;
  remainingCount: number;
  nextRecoverableCount: number;
}

export interface DetailRecoveryPassEndDecision {
  status: 'completed' | 'budget_exhausted' | 'high_priority_retry' | 'continue';
  stopRecovery: boolean;
  logMessage: string;
}

export function countBudgetExhaustedDetailEntries(entries: DetailRecoveryBudgetEntry[]): number {
  return entries.filter((entry) => entry.attemptsUsed >= Math.max(entry.budget, 0)).length;
}

export function resolveDetailRecoveryPassStart(
  input: DetailRecoveryPassStartInput
): DetailRecoveryPassStartDecision {
  const { pass, missingCount, recoverableCount, budgetExhaustedCount } = input;

  if (missingCount <= 0) {
    return {
      status: 'completed',
      shouldRunPass: false,
      stopRecovery: true,
      logMessages: pass > 1 ? ['补爬校验通过，所有已入队影片均已处理完成。'] : []
    };
  }

  if (recoverableCount <= 0) {
    return {
      status: 'budget_exhausted',
      shouldRunPass: false,
      stopRecovery: true,
      logMessages: buildDetailBudgetStopMessages(budgetExhaustedCount)
    };
  }

  return {
    status: 'continue',
    shouldRunPass: true,
    stopRecovery: false,
    logMessages: []
  };
}

export function resolveDetailRecoveryPassEnd(
  input: DetailRecoveryPassEndInput
): DetailRecoveryPassEndDecision {
  const { pass, previousMissingCount, remainingCount, nextRecoverableCount } = input;

  if (remainingCount <= 0) {
    return {
      status: 'completed',
      stopRecovery: true,
      logMessage: ''
    };
  }

  if (remainingCount >= previousMissingCount) {
    if (nextRecoverableCount <= 0) {
      return {
        status: 'budget_exhausted',
        stopRecovery: true,
        logMessage: '当前未完成影片已全部耗尽重试预算，停止继续补爬。'
      };
    }

    return {
      status: 'high_priority_retry',
      stopRecovery: false,
      logMessage: `第 ${pass} 轮补爬后未完成数量没有下降，但仍存在可恢复失败项，下一轮将仅重试高优先级失败详情页。`
    };
  }

  return {
    status: 'continue',
    stopRecovery: false,
    logMessage: ''
  };
}
