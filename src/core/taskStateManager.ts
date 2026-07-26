/**
 * 任务状态落盘层：
 * - 将 task-state / validation-report 写入用户目录下的运行态目录，避免污染用户输出目录。
 * - 输出目录只保留用户关心的 filmData.json / magnet-links.txt / 日志文本。
 */
import fs from 'fs';
import path from 'path';
import crypto from 'crypto';

export interface PageAuditRecord {
  pageNumber: number;
  url: string;
  expectedCount: number | null;
  actualCount: number;
  retryCount: number;
  validationPassed: boolean;
  confidenceScore: number;
  confidence: 'high' | 'medium' | 'low';
  reason: string;
  updatedAt: string;
}

export interface ResultValidationReport {
  generatedAt: string;
  totalRecords: number;
  uniqueRecords: number;
  duplicateCount: number;
  invalidRecordCount: number;
  expectedItemCount: number;
  persistedItemCount: number;
  missingFromQueueCount: number;
  expectedButNotQueuedCount: number;
  processedButNotPersistedCount: number;
  uniqueMagnetCount: number;
  lowConfidencePageCount: number;
  missingItems: string[];
  expectedButNotQueuedItems: string[];
  processedButNotPersistedItems: string[];
  lowConfidencePages: number[];
  passed: boolean;
  summary: string;
}

export interface FailedDetailRecord {
  item: string;
  sourceLink: string;
  reason: string;
  category?: string;
  retryCount?: number;
  retryAdvice?: string;
  recoverable?: boolean;
  lastFailedAt: string;
}

export interface DuplicateExpectedEntryGroup {
  itemId: string;
  links: string[];
}

export interface TaskReconciliationSnapshot {
  expectedIds: string[];
  queuedIds: string[];
  processedIds: string[];
  persistedIds: string[];
  expectedButNotQueuedIds: string[];
  expectedButNotPersistedIds: string[];
  processedButNotPersistedIds: string[];
  duplicateExpectedIds: string[];
  expectedEntryCount?: number;
  rawDuplicateEntryCount?: number;
  rawDuplicateGroups?: DuplicateExpectedEntryGroup[];
}

export interface TaskStateSnapshot {
  schemaVersion: number;
  appVersion: string;
  status: string;
  message: string;
  updatedAt: string;
  startedAt: string;
  config: {
    base: string | null;
    output: string;
    limit: number;
    totalPages: number;
    itemsPerPage: number;
    parallel: number;
    delay: number;
    timeout: number;
    secondValidation: boolean;
    taskTemplate: string;
  };
  progress: {
    nextPageIndex: number;
    expectedItemsPerPage: number | null;
    queued: number;
    attempted: number;
    completed: number;
    skipped: number;
  };
  links: {
    expected?: string[];
    expectedIds?: string[];
    queued?: string[];
    queuedIds?: string[];
    processed?: string[];
    processedIds?: string[];
    persisted?: string[];
    persistedIds?: string[];
    persistedFilmIds?: string[];
    skippedIds?: string[];
  };
  completedItemIds?: string[];
  reconciliation?: TaskReconciliationSnapshot;
  missingItems?: string[];
  failedDetails?: FailedDetailRecord[];
  pageAudits?: PageAuditRecord[];
  validationReport?: ResultValidationReport | null;
}

interface SaveJsonOptions {
  withBackup?: boolean;
}

class TaskStateManager {
  private outputDir: string;
  private runtimeDir: string;
  private taskStatePath: string;
  private validationReportPath: string;
  private backupDir: string;

  constructor(outputDir: string) {
    this.outputDir = outputDir;
    this.runtimeDir = this.resolveRuntimeDir(outputDir);
    this.taskStatePath = path.join(this.runtimeDir, 'task-state.json');
    this.validationReportPath = path.join(this.runtimeDir, 'validation-report.json');
    this.backupDir = path.join(this.runtimeDir, 'backups');
    this.ensureDirectories();
  }

  public loadSnapshot(): TaskStateSnapshot | null {
    if (!fs.existsSync(this.taskStatePath)) {
      return null;
    }

    try {
      const snapshot = JSON.parse(fs.readFileSync(this.taskStatePath, 'utf8')) as TaskStateSnapshot;
      return snapshot && typeof snapshot === 'object' ? snapshot : null;
    } catch {
      return null;
    }
  }

  public saveSnapshot(snapshot: TaskStateSnapshot, options: SaveJsonOptions = {}): void {
    this.writeJson(this.taskStatePath, snapshot, {
      withBackup: options.withBackup ?? true
    });
  }

  public saveValidationReport(report: ResultValidationReport): void {
    this.writeJson(this.validationReportPath, report, { withBackup: true });
  }

  public getTaskStatePath(): string {
    return this.taskStatePath;
  }

  public cleanupRuntimeState(): void {
    if (!fs.existsSync(this.runtimeDir)) {
      return;
    }

    fs.rmSync(this.runtimeDir, { recursive: true, force: true });
  }

  private ensureDirectories(): void {
    fs.mkdirSync(this.outputDir, { recursive: true });
    fs.mkdirSync(this.runtimeDir, { recursive: true });
    fs.mkdirSync(this.backupDir, { recursive: true });
  }

  private resolveRuntimeDir(outputDir: string): string {
    const homeDir =
      (process.platform === 'win32' ? process.env.USERPROFILE : process.env.HOME) || process.cwd();
    const stateRoot = process.env.JAV_SCRAPY_STATE_DIR || path.join(homeDir, '.jav-scrapy', 'runtime-state');
    const normalizedOutput = path.resolve(outputDir || process.cwd()).toLowerCase();
    const stateId = crypto.createHash('sha1').update(normalizedOutput).digest('hex').slice(0, 16);
    return path.join(stateRoot, stateId);
  }

  private writeJson(filePath: string, data: unknown, options: SaveJsonOptions = {}): void {
    this.ensureDirectories();
    if (options.withBackup !== false) {
      this.createLatestBackup(filePath);
    }
    fs.writeFileSync(filePath, JSON.stringify(data, null, 2), 'utf8');
  }

  private createLatestBackup(filePath: string): void {
    if (!fs.existsSync(filePath)) {
      return;
    }

    const backupPath = path.join(this.backupDir, `${path.basename(filePath)}.bak`);
    fs.copyFileSync(filePath, backupPath);
  }
}

export default TaskStateManager;
