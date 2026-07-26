import type { TaskStateSnapshot } from './taskStateManager';
import type { RunnerStatePayload, TaskSnapshotMode } from './scraperRunnerTypes';

const FINAL_SNAPSHOT_STATUSES = new Set<RunnerStatePayload['status']>([
  'completed',
  'incomplete',
  'stopped',
  'error'
]);

export interface RunnerOutputFileHandlerLike {
  syncUnfinishedItemsReport(lines: string[]): void;
  cleanupLegacyOutputArtifacts(): void;
}

export interface FinalizeRunnerOutputArtifactsInput {
  hasConfig: boolean;
  status: RunnerStatePayload['status'];
  message: string;
  fileHandler?: RunnerOutputFileHandlerLike | null;
  uncapturedItemsTotal: number;
  recoverablePageAuditCount: number;
  buildUnfinishedReportLines: (status: RunnerStatePayload['status'], message: string) => string[];
  cleanupRuntimeState?: (() => void) | null;
  onError?: ((message: string) => void) | null;
}

export function finalizeRunnerOutputArtifacts(input: FinalizeRunnerOutputArtifactsInput): void {
  if (!input.hasConfig) {
    return;
  }

  try {
    const shouldClearUnfinishedReport =
      input.status === 'completed' &&
      input.uncapturedItemsTotal === 0 &&
      input.recoverablePageAuditCount === 0;
    const unfinishedLines = shouldClearUnfinishedReport
      ? []
      : input.buildUnfinishedReportLines(input.status, input.message);

    input.fileHandler?.syncUnfinishedItemsReport(unfinishedLines);
    input.fileHandler?.cleanupLegacyOutputArtifacts();

    if (input.status === 'completed') {
      input.cleanupRuntimeState?.();
    }
  } catch (error) {
    input.onError?.(`整理输出文件时发生异常：${error instanceof Error ? error.message : String(error)}`);
  }
}

export interface RunnerTaskStateManagerLike {
  saveSnapshot(snapshot: TaskStateSnapshot, options?: { withBackup?: boolean }): void;
}

export interface PersistRunnerTaskStateInput {
  taskStateManager?: RunnerTaskStateManagerLike | null;
  reason: string;
  status: RunnerStatePayload['status'];
  message: string;
  force: boolean;
  snapshotMode?: TaskSnapshotMode;
  lastStatePersistAt: number;
  statePersistMinIntervalMs: number;
  buildTaskSnapshot: (
    status: RunnerStatePayload['status'],
    message: string,
    mode: TaskSnapshotMode
  ) => TaskStateSnapshot;
  onPersisted?: ((timestamp: number) => void) | null;
  onMutationReset?: (() => void) | null;
  onDebug?: ((message: string) => void) | null;
  onWarn?: ((message: string) => void) | null;
}

export function resolveRunnerSnapshotMode(
  status: RunnerStatePayload['status'],
  force: boolean,
  snapshotMode?: TaskSnapshotMode
): TaskSnapshotMode {
  if (snapshotMode) {
    return snapshotMode;
  }

  return force || FINAL_SNAPSHOT_STATUSES.has(status) ? 'full' : 'light';
}

export function persistRunnerTaskState(input: PersistRunnerTaskStateInput): void {
  if (!input.taskStateManager) {
    return;
  }

  if (!input.force) {
    const now = Date.now();
    if (now - input.lastStatePersistAt < input.statePersistMinIntervalMs) {
      return;
    }
  }

  try {
    const resolvedMode = resolveRunnerSnapshotMode(input.status, input.force, input.snapshotMode);
    input.taskStateManager.saveSnapshot(input.buildTaskSnapshot(input.status, input.message, resolvedMode), {
      withBackup: resolvedMode === 'full'
    });
    input.onMutationReset?.();
    const persistedAt = Date.now();
    input.onPersisted?.(persistedAt);
    input.onDebug?.(`任务状态已落盘：${input.reason}`);
  } catch (error) {
    input.onWarn?.(`保存任务状态失败：${error instanceof Error ? error.message : String(error)}`);
  }
}
