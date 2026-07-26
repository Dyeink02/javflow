export interface QueueBucketStats {
  waiting: number;
  running: number;
}

export interface WorkQueueStats {
  indexPageQueue?: QueueBucketStats | null;
  detailPageQueue?: QueueBucketStats | null;
  magnetFastQueue?: QueueBucketStats | null;
  magnetRecoveryQueue?: QueueBucketStats | null;
  fileWriteQueue?: QueueBucketStats | null;
  imageDownloadQueue?: QueueBucketStats | null;
}

export interface WorkQueueDrainInspection {
  workQueuesFinished: boolean;
  activeQueueCount: number;
  pendingWorkCount: number;
}

const WORK_QUEUE_KEYS = [
  'indexPageQueue',
  'detailPageQueue',
  'magnetFastQueue',
  'magnetRecoveryQueue',
  'fileWriteQueue',
  'imageDownloadQueue'
] as const;

function normalizeBucket(bucket: QueueBucketStats | null | undefined): QueueBucketStats {
  return {
    waiting: Math.max(Number(bucket?.waiting || 0), 0),
    running: Math.max(Number(bucket?.running || 0), 0)
  };
}

export function inspectWorkQueueDrain(stats: WorkQueueStats): WorkQueueDrainInspection {
  let activeQueueCount = 0;
  let pendingWorkCount = 0;

  for (const key of WORK_QUEUE_KEYS) {
    const bucket = normalizeBucket(stats[key]);
    const total = bucket.waiting + bucket.running;
    if (total > 0) {
      activeQueueCount += 1;
      pendingWorkCount += total;
    }
  }

  return {
    workQueuesFinished: pendingWorkCount === 0,
    activeQueueCount,
    pendingWorkCount
  };
}

export function resolveWorkQueueDrainPollIntervalMs(inspection: WorkQueueDrainInspection): number {
  if (inspection.pendingWorkCount <= 0) {
    return 0;
  }

  if (inspection.pendingWorkCount <= 4) {
    return 250;
  }

  if (inspection.pendingWorkCount <= 12) {
    return 350;
  }

  if (inspection.pendingWorkCount >= 40 || inspection.activeQueueCount >= 4) {
    return 650;
  }

  return 500;
}

export interface FinalDrainPlan {
  waitForDelays: boolean;
  flushOutputs: boolean;
}

export function buildFinalDrainPlan(params: {
  hasActiveDelays: boolean;
}): FinalDrainPlan {
  return {
    waitForDelays: Boolean(params.hasActiveDelays),
    flushOutputs: true
  };
}
