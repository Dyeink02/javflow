import type { FilmData, MagnetResult, Metadata } from '../types/interfaces';
import logger from './logger';

export interface MagnetQueueTask {
  sourceLink: string;
  metadata: Metadata;
  filmData: FilmData;
}

export interface MagnetQueueRequestHandler {
  fetchMagnet(
    metadata: Metadata,
    options: { mode: 'http-only' | 'cloudflare-only'; fastFallback?: boolean }
  ): Promise<MagnetResult | null>;
  shouldRouteMagnetTaskToRecoveryQueue(): boolean;
}

export interface MagnetProcessedPayload {
  filmData: FilmData;
  metadata: Metadata;
  sourceLink: string;
}

interface FastMagnetTaskContext {
  task: MagnetQueueTask;
  requestHandler: MagnetQueueRequestHandler;
  useCloudflareBypass: boolean;
  isShuttingDown?: () => boolean;
  routeToRecoveryQueue: (task: MagnetQueueTask) => void;
  emitProcessed: (payload: MagnetProcessedPayload) => void;
}

interface RecoveryMagnetTaskContext {
  task: MagnetQueueTask;
  requestHandler: MagnetQueueRequestHandler;
  isShuttingDown?: () => boolean;
  emitProcessed: (payload: MagnetProcessedPayload) => void;
}

function createProcessedPayload(task: MagnetQueueTask): MagnetProcessedPayload {
  return {
    filmData: task.filmData,
    metadata: task.metadata,
    sourceLink: task.sourceLink
  };
}

export function getFastMagnetQueueConcurrency(parallel: number): number {
  return Math.max(2, Math.min(12, parallel * 4));
}

export function getRecoveryMagnetQueueConcurrency(
  useCloudflareBypass: boolean,
  parallel: number
): number {
  return useCloudflareBypass ? Math.max(1, Math.min(2, parallel)) : 1;
}

function shouldStopOutput(isShuttingDown?: () => boolean): boolean {
  return Boolean(isShuttingDown && isShuttingDown());
}

function isCancellationError(error: unknown): boolean {
  const message = error instanceof Error ? error.message : String(error || '');
  return message === 'Request cancelled' || message === 'Validation cancelled';
}

export async function processFastMagnetTask(context: FastMagnetTaskContext): Promise<void> {
  const { task, requestHandler, useCloudflareBypass, isShuttingDown, routeToRecoveryQueue, emitProcessed } = context;

  logger.debug(`QueueManager: [\u5FEB\u901F\u78C1\u529B] \u5F00\u59CB\u5904\u7406: ${task.filmData.title}`);

  if (shouldStopOutput(isShuttingDown)) {
    logger.info(`QueueManager: [\u5FEB\u901F\u78C1\u529B] \u961F\u5217\u5DF2\u8FDB\u5165\u7EC8\u6B62\u6D41\u7A0B\uFF0C\u8DF3\u8FC7: ${task.filmData.title}`);
    return;
  }

  if (useCloudflareBypass && requestHandler.shouldRouteMagnetTaskToRecoveryQueue()) {
    logger.info(
      `QueueManager: [\u5FEB\u901F\u78C1\u529B] \u5FEB\u901F\u901A\u9053\u5DF2\u7194\u65AD\uFF0C\u76F4\u63A5\u8F6C\u5165 Cloudflare \u8865\u6293: ${task.filmData.title}`
    );
    if (shouldStopOutput(isShuttingDown)) {
      return;
    }
    routeToRecoveryQueue(task);
    return;
  }

  try {
    const magnetResult = await requestHandler.fetchMagnet(task.metadata, {
      mode: 'http-only',
      fastFallback: true
    });

    if (magnetResult?.magnetLinks) {
      task.filmData.magnetLinks = magnetResult.magnetLinks;
      task.filmData.backupMagnetLinks =
        magnetResult.backupMagnetLinks && magnetResult.backupMagnetLinks.length > 0
          ? magnetResult.backupMagnetLinks
          : magnetResult.magnetLinks;
      logger.debug(`QueueManager: [\u5FEB\u901F\u78C1\u529B] \u78C1\u529B\u94FE\u63A5\u83B7\u53D6\u6210\u529F: ${task.filmData.title}`);
      if (shouldStopOutput(isShuttingDown)) {
        return;
      }
      emitProcessed(createProcessedPayload(task));
      return;
    }

    if (useCloudflareBypass) {
      logger.debug(
        `QueueManager: [\u5FEB\u901F\u78C1\u529B] \u672A\u83B7\u53D6\u5230\u6709\u6548\u78C1\u529B\uFF0C\u8F6C\u5165 Cloudflare \u8865\u6293: ${task.filmData.title}`
      );
      if (shouldStopOutput(isShuttingDown)) {
        return;
      }
      routeToRecoveryQueue(task);
      return;
    }

    logger.debug(`QueueManager: [\u5FEB\u901F\u78C1\u529B] \u672A\u83B7\u53D6\u5230\u6709\u6548\u78C1\u529B\uFF0C\u6309\u5F53\u524D\u7ED3\u679C\u7EE7\u7EED\u4FDD\u5B58: ${task.filmData.title}`);
    if (shouldStopOutput(isShuttingDown)) {
      return;
    }
    emitProcessed(createProcessedPayload(task));
  } catch (error) {
    if (shouldStopOutput(isShuttingDown) || isCancellationError(error)) {
      logger.info(`QueueManager: [\u5FEB\u901F\u78C1\u529B] \u4EFB\u52A1\u5DF2\u88AB\u7EC8\u6B62: ${task.filmData.title}`);
      return;
    }

    logger.warn(
      `QueueManager: [\u5FEB\u901F\u78C1\u529B] \u8BF7\u6C42\u5931\u8D25: ${task.filmData.title}\uFF0C\u9519\u8BEF: ${
        error instanceof Error ? error.message : String(error)
      }`
    );

    if (useCloudflareBypass) {
      logger.info(`QueueManager: [\u5FEB\u901F\u78C1\u529B] \u5DF2\u5207\u6362\u5230 Cloudflare \u8865\u6293\u961F\u5217: ${task.filmData.title}`);
      if (shouldStopOutput(isShuttingDown)) {
        return;
      }
      routeToRecoveryQueue(task);
      return;
    }

    if (shouldStopOutput(isShuttingDown)) {
      return;
    }
    emitProcessed(createProcessedPayload(task));
  }
}

export async function processRecoveryMagnetTask(
  context: RecoveryMagnetTaskContext
): Promise<void> {
  const { task, requestHandler, isShuttingDown, emitProcessed } = context;

  logger.debug(`QueueManager: [Cloudflare\u8865\u6293] \u5F00\u59CB\u5904\u7406: ${task.filmData.title}`);

  if (shouldStopOutput(isShuttingDown)) {
    logger.info(`QueueManager: [Cloudflare\u8865\u6293] \u961F\u5217\u5DF2\u8FDB\u5165\u7EC8\u6B62\u6D41\u7A0B\uFF0C\u8DF3\u8FC7: ${task.filmData.title}`);
    return;
  }

  try {
    const magnetResult = await requestHandler.fetchMagnet(task.metadata, {
      mode: 'cloudflare-only'
    });

    if (magnetResult?.magnetLinks) {
      task.filmData.magnetLinks = magnetResult.magnetLinks;
      task.filmData.backupMagnetLinks =
        magnetResult.backupMagnetLinks && magnetResult.backupMagnetLinks.length > 0
          ? magnetResult.backupMagnetLinks
          : magnetResult.magnetLinks;
      logger.debug(`QueueManager: [Cloudflare\u8865\u6293] \u78C1\u529B\u94FE\u63A5\u8865\u6293\u6210\u529F: ${task.filmData.title}`);
    } else {
      logger.debug(
        `QueueManager: [Cloudflare\u8865\u6293] \u8865\u6293\u540E\u4ECD\u65E0\u78C1\u529B\uFF0C\u6309\u5F53\u524D\u7ED3\u679C\u7EE7\u7EED\u4FDD\u5B58: ${task.filmData.title}`
      );
    }
  } catch (error) {
    if (shouldStopOutput(isShuttingDown) || isCancellationError(error)) {
      logger.info(`QueueManager: [Cloudflare\u8865\u6293] \u4EFB\u52A1\u5DF2\u88AB\u7EC8\u6B62: ${task.filmData.title}`);
      return;
    }

    logger.warn(
      `QueueManager: [Cloudflare\u8865\u6293] \u5904\u7406\u5931\u8D25\uFF0C\u6309\u5F53\u524D\u7ED3\u679C\u7EE7\u7EED\u4FDD\u5B58: ${task.filmData.title}\uFF0C\u9519\u8BEF: ${
        error instanceof Error ? error.message : String(error)
      }`
    );
  }

  if (shouldStopOutput(isShuttingDown)) {
    return;
  }
  emitProcessed(createProcessedPayload(task));
}
