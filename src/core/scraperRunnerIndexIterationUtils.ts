import { shouldWarnSparseIndexPage } from './scraperRunnerIndexUtils';

export interface IndexLoopSuccessPlanInput {
  currentPage: number;
  nextPageDelayMs: number;
  shouldStopIndexing: boolean;
  isStopping: boolean;
  targetTotalPages: number;
  expectedItemsPerPage: number | null;
  isLastTargetPage: boolean;
  linksCount: number;
}

export interface IndexLoopSuccessPlan {
  shouldPrefetchNextPage: boolean;
  nextPrefetchPageNumber: number;
  shouldWarnSparsePage: boolean;
  sparseWarningMessage: string;
  nextPageDelayMs: number;
  nextPageNumber: number;
  stateReason: string;
  stateMessage: string;
  delayLogMessage: string;
}

export interface IndexLoopErrorPlanInput {
  currentPage: number;
  message: string;
  networkBackoffDelayMs: number;
  genericDelayMs: number;
}

export interface IndexLoopErrorPlan {
  stateReason: string;
  stateMessage: string;
  delayMs: number;
  retryLogMessage: string;
}

export function isRetryableIndexNetworkError(message: string): boolean {
  const normalized = String(message || '').toUpperCase();
  return (
    normalized.includes('ECONNRESET') ||
    normalized.includes('ETIMEDOUT') ||
    normalized.includes('ENOTFOUND')
  );
}

export function resolveIndexLoopSuccessPlan(
  input: IndexLoopSuccessPlanInput
): IndexLoopSuccessPlan {
  const {
    currentPage,
    nextPageDelayMs,
    shouldStopIndexing,
    isStopping,
    targetTotalPages,
    expectedItemsPerPage,
    isLastTargetPage,
    linksCount
  } = input;

  const shouldPrefetchNextPage =
    !shouldStopIndexing && !isStopping && (targetTotalPages <= 0 || currentPage + 1 <= targetTotalPages);
  const shouldWarnSparsePage = shouldWarnSparseIndexPage({
    shouldStopIndexing,
    expectedItemsPerPage,
    isLastTargetPage,
    linksCount
  });

  return {
    shouldPrefetchNextPage,
    nextPrefetchPageNumber: currentPage + 1,
    shouldWarnSparsePage,
    sparseWarningMessage: shouldWarnSparsePage
      ? `第 ${currentPage} 页条数偏低（${linksCount}/${expectedItemsPerPage}）。当前为普通模式，仅记录异常并继续尝试后续页面。`
      : '',
    nextPageDelayMs,
    nextPageNumber: currentPage + 1,
    stateReason: '索引页处理完成',
    stateMessage: `第 ${currentPage} 页已处理完成。`,
    delayLogMessage: `下一页抓取前等待 ${Math.round(nextPageDelayMs / 1000)} 秒...`
  };
}

export function resolveIndexLoopErrorPlan(input: IndexLoopErrorPlanInput): IndexLoopErrorPlan {
  const { currentPage, message, networkBackoffDelayMs, genericDelayMs } = input;
  const retryableNetworkError = isRetryableIndexNetworkError(message);
  const delayMs = retryableNetworkError ? networkBackoffDelayMs : genericDelayMs;

  return {
    stateReason: '索引页抓取失败',
    stateMessage: `第 ${currentPage} 页抓取失败: ${message}`,
    delayMs,
    retryLogMessage: retryableNetworkError
      ? `检测到网络异常，将在 ${Math.round(networkBackoffDelayMs / 1000)} 秒后重试...`
      : ''
  };
}
