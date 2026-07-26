import type { RuntimeOptions } from '../types/interfaces';
import type { FailedDetailRecord, PageAuditRecord, ResultValidationReport } from './taskStateManager';

export interface GoRestoredTaskState {
  shouldRestore?: boolean;
  pageIndex?: number;
  expectedItemsPerPage?: number | null;
  filmsQueued?: number;
  filmsAttempted?: number;
  filmCount?: number;
  pageAudits?: PageAuditRecord[];
  validationReport?: ResultValidationReport | null;
  failedDetails?: FailedDetailRecord[];
  expectedLinks?: string[];
  expectedItemIds?: string[];
  queuedLinks?: string[];
  queuedItemIds?: string[];
  queuedFilmIds?: string[];
  processedLinks?: string[];
  processedItemIds?: string[];
  persistedLinks?: string[];
  persistedItemIds?: string[];
  persistedFilmIds?: string[];
  skippedItemIds?: string[];
  duplicateExpectedIds?: string[];
  pendingDetailLinks?: string[];
  logMessage?: string;
}

export interface GoPersistedFilmRecord {
  title?: string;
  sourceLink?: string;
  actress?: string[];
  actressCount?: number;
  filteredByActressCount?: boolean;
  filterReason?: string;
}

export interface GoPersistedOutputState {
  filmDataPath?: string;
  filmDataExists?: boolean;
  recordCount?: number;
  records?: GoPersistedFilmRecord[];
  logMessage?: string;
}

export interface GoExecutionPlan {
  source?: string;
  phaseKeys?: RunnerExecutionPhase[];
  nextPhaseByKey?: Partial<Record<RunnerExecutionPhase, RunnerExecutionPhase>>;
  stopRedirectPhaseKey?: RunnerExecutionPhase;
  initialPhaseKey?: RunnerExecutionPhase;
  finalPhaseKey?: RunnerExecutionPhase;
  resumePendingFirst?: boolean;
  hasRestoreState?: boolean;
  pendingDetailCount?: number;
  secondValidationEnabled?: boolean;
  logMessage?: string;
}

export interface ScraperRunnerOptions extends Partial<RuntimeOptions> {
  useProgressBars?: boolean;
  handleSignals?: boolean;
  resumeExisting?: boolean;
  outputResolved?: boolean;
  goRestoredTaskState?: GoRestoredTaskState | null;
  goPersistedOutputState?: GoPersistedOutputState | null;
  goExecutionPlan?: GoExecutionPlan | null;
}

export interface RunnerStatePayload {
  status:
    | 'idle'
    | 'starting'
    | 'running'
    | 'stopping'
    | 'completed'
    | 'stopped'
    | 'error'
    | 'incomplete';
  message: string;
  activeItems?: string[];
  activeItemsTotal?: number;
  completedItems?: string[];
  completedItemsTotal?: number;
  pendingItems?: string[];
  pendingItemsTotal?: number;
  duplicateItems?: string[];
  duplicateItemsTotal?: number;
  unfinishedItems?: string[];
  unfinishedItemsTotal?: number;
  missingItems?: string[];
  missingItemsTotal?: number;
  pageGapItems?: string[];
  pageGapItemsTotal?: number;
  failedDetails?: FailedDetailRecord[];
  failedDetailsTotal?: number;
  phaseKey?: RunnerExecutionPhase;
  phasePlanKeys?: RunnerExecutionPhase[];
  stats?: {
    queued: number;
    attempted: number;
    completed: number;
    pageIndex: number;
    filteredByActressCount?: number;
    filteredByFilmCode?: number;
    completedItems?: number;
    filteredByActressCountItemIds?: string[];
    filteredByFilmCodeItemIds?: string[];
    completedItemIds?: string[];
    filteredItemIds?: string[];
  };
}

export type RunnerStatus = RunnerStatePayload['status'];

export interface UpdateAntiBlockResult {
  antiBlockUrls: string[];
  filePath: string;
}

export interface PageFetchResult {
  links: string[];
  audit: PageAuditRecord;
  diagnosticReason?: string;
}

export interface PrefetchedIndexPage {
  pageNumber: number;
  url: string;
  expectedCount: number | null;
  isLastTargetPage: boolean;
  phase: 'initial' | 'recovery';
  promise: Promise<PageFetchResult>;
}

export interface DetailFailurePolicy {
  key: 'blocked' | 'network' | 'empty' | 'parse' | 'unknown' | 'stopped';
  label: string;
  maxRetries: number;
  priority: number;
  advice: string;
}

export type TaskSnapshotMode = 'light' | 'full';

export type RunnerExecutionPhase =
  | 'boot'
  | 'queue_setup'
  | 'resume_pending'
  | 'index_discovery'
  | 'queue_drain'
  | 'page_gap_recovery'
  | 'queue_gap_recovery'
  | 'detail_recovery'
  | 'second_validation'
  | 'final_drain';
