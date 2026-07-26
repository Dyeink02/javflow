import type { Config } from '../types/interfaces';
import type {
  FailedDetailRecord,
  PageAuditRecord,
  ResultValidationReport,
  TaskReconciliationSnapshot,
  TaskStateSnapshot
} from './taskStateManager';
import type { TaskSnapshotMode } from './scraperRunnerTypes';

export function buildTaskSnapshot(params: {
  appVersion: string;
  status: string;
  message: string;
  startedAt: string;
  config: Config | null;
  pageIndex: number;
  expectedItemsPerPage: number | null;
  filmsQueued: number;
  filmsAttempted: number;
  filmCount: number;
  expectedDetailLinks: Set<string>;
  queuedDetailLinks: Set<string>;
  processedDetailLinks: Set<string>;
  persistedDetailLinks: Set<string>;
  persistedFilmIds: Set<string>;
  skippedItemIds: Set<string>;
  completedItemIds: Set<string>;
  reconciliation: TaskReconciliationSnapshot;
  missingItems: string[];
  failedDetails: FailedDetailRecord[];
  pageAudits: PageAuditRecord[];
  validationReport: ResultValidationReport | null;
  mode: TaskSnapshotMode;
}): TaskStateSnapshot {
  const {
    appVersion,
    status,
    message,
    startedAt,
    config,
    pageIndex,
    expectedItemsPerPage,
    filmsQueued,
    filmsAttempted,
    filmCount,
    expectedDetailLinks,
    queuedDetailLinks,
    processedDetailLinks,
    persistedDetailLinks,
    persistedFilmIds,
    skippedItemIds,
    completedItemIds,
    reconciliation,
    missingItems,
    failedDetails,
    pageAudits,
    validationReport,
    mode
  } = params;

  const includeFullState = mode === 'full';
  const links: TaskStateSnapshot['links'] = {
    expected: Array.from(expectedDetailLinks),
    queued: Array.from(queuedDetailLinks),
    processed: Array.from(processedDetailLinks),
    persisted: Array.from(persistedDetailLinks),
    persistedFilmIds: Array.from(persistedFilmIds),
    skippedIds: Array.from(skippedItemIds)
  };

  if (includeFullState) {
    links.expectedIds = reconciliation.expectedIds;
    links.queuedIds = reconciliation.queuedIds;
    links.processedIds = reconciliation.processedIds;
    links.persistedIds = reconciliation.persistedIds;
  }

  return {
    schemaVersion: 2,
    appVersion,
    status,
    message,
    updatedAt: new Date().toISOString(),
    startedAt,
    config: {
      base: config?.base || config?.BASE_URL || null,
      output: config?.output || '',
      limit: config?.limit || 0,
      totalPages: config?.totalPages || 0,
      itemsPerPage: config?.itemsPerPage || expectedItemsPerPage || 0,
      parallel: config?.parallel || 0,
      delay: config?.delay || 0,
      timeout: config?.timeout || 0,
      secondValidation: Boolean(config?.secondValidation),
      taskTemplate: config?.taskTemplate || 'balanced'
    },
    progress: {
      nextPageIndex: pageIndex,
      expectedItemsPerPage,
      queued: filmsQueued,
      attempted: filmsAttempted,
      completed: filmCount,
      skipped: skippedItemIds.size
    },
    links,
    completedItemIds: Array.from(completedItemIds),
    reconciliation: includeFullState ? reconciliation : undefined,
    missingItems: includeFullState ? missingItems : undefined,
    failedDetails,
    pageAudits,
    validationReport: includeFullState ? validationReport : undefined
  };
}
