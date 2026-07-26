export interface DetailQueueTuning {
  highWaterMark: number;
  lowWaterMark: number;
  batchSize: number;
  progressLogStep: number;
}

export function getDetailQueueTuning(params: {
  parallel: number;
  largeTaskMode: boolean;
}): DetailQueueTuning {
  const parallel = Math.max(params.parallel || 1, 1);
  const largeTaskMode = Boolean(params.largeTaskMode);

  const highWaterMultiplier = largeTaskMode ? 18 : 12;
  const highWaterMax = largeTaskMode ? 160 : 90;
  const highWaterMark = Math.max(24, Math.min(highWaterMax, parallel * highWaterMultiplier));

  const lowWaterMark = Math.max(8, Math.floor(highWaterMark * (largeTaskMode ? 0.55 : 0.4)));

  const batchMultiplier = largeTaskMode ? 6 : 4;
  const batchMax = largeTaskMode ? 48 : 24;
  const batchMin = largeTaskMode ? 12 : 8;
  const batchSize = Math.max(batchMin, Math.min(batchMax, parallel * batchMultiplier));

  const progressLogStep = largeTaskMode ? Math.max(batchSize * 3, 36) : Math.max(batchSize * 2, 16);

  return {
    highWaterMark,
    lowWaterMark,
    batchSize,
    progressLogStep
  };
}

export interface DetailQueueWaitDecision {
  shouldPauseEnqueue: boolean;
  shouldResumeEnqueue: boolean;
}

export function resolveDetailQueueWaitDecision(params: {
  backlog: number;
  threshold: number;
  resumeThreshold?: number;
}): DetailQueueWaitDecision {
  const backlog = Math.max(params.backlog || 0, 0);
  const threshold = Math.max(params.threshold || 0, 0);
  const resumeThreshold = Math.max(params.resumeThreshold ?? threshold, 0);

  return {
    shouldPauseEnqueue: backlog >= threshold,
    shouldResumeEnqueue: backlog <= resumeThreshold
  };
}

export interface DetailEnqueueBatchPlan {
  availableCapacity: number;
  nextBatchSize: number;
}

export function resolveDetailEnqueueBatchPlan(params: {
  backlog: number;
  highWaterMark: number;
  batchSize: number;
  remainingCount: number;
}): DetailEnqueueBatchPlan {
  const backlog = Math.max(params.backlog || 0, 0);
  const highWaterMark = Math.max(params.highWaterMark || 0, 1);
  const batchSize = Math.max(params.batchSize || 0, 1);
  const remainingCount = Math.max(params.remainingCount || 0, 0);
  const availableCapacity = Math.max(highWaterMark - backlog, 1);
  const nextBatchSize = remainingCount > 0 ? Math.max(1, Math.min(batchSize, availableCapacity, remainingCount)) : 0;

  return {
    availableCapacity,
    nextBatchSize
  };
}

export function adjustIndexPageDelayForBacklog(params: {
  baseDelayMs: number;
  backlog: number;
  lowWaterMark: number;
  largeTaskMode: boolean;
}): number {
  const baseDelayMs = Math.max(params.baseDelayMs || 0, 0);
  const backlog = Math.max(params.backlog || 0, 0);
  const lowWaterMark = Math.max(params.lowWaterMark || 0, 0);

  if (backlog >= lowWaterMark) {
    return Math.max(0, Math.round(baseDelayMs * (params.largeTaskMode ? 0.35 : 0.6)));
  }

  return baseDelayMs;
}
