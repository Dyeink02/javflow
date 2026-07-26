/**
 * 结果二次校验器：
 * 仅针对本地输出结果做静态校验，不再次访问网站。
 * 目标是尽快发现重复、缺失、低可信分页与异常记录。
 */
import fs from 'fs';
import path from 'path';
import { FilmData } from '../types/interfaces';
import { createFilmIdentityKey, extractFilmId } from './filmIdentity';
import { ResultValidationReport, TaskStateSnapshot } from './taskStateManager';

interface SnapshotReconciliation {
  expectedIds: string[];
  persistedIds: string[];
  processedIds: string[];
  expectedButNotQueuedIds: string[];
  expectedButNotPersistedIds: string[];
  processedButNotPersistedIds: string[];
}

class ResultValidator {
  /**
   * 汇总输出目录中的 filmData.json 与任务快照，生成一次可落盘的校验报告。
   */
  public static validateOutput(
    outputDir: string,
    snapshot: TaskStateSnapshot | null = null
  ): ResultValidationReport {
    const filmDataPath = path.join(outputDir, 'filmData.json');
    const records = this.loadRecords(filmDataPath);
    const uniqueKeys = new Set<string>();
    const uniqueMagnets = new Set<string>();
    let invalidRecordCount = 0;
    let duplicateCount = 0;

    for (const record of records) {
      const key = this.createRecordKey(record);
      if (uniqueKeys.has(key)) {
        duplicateCount += 1;
      } else {
        uniqueKeys.add(key);
      }

      if (!record.title || typeof record.title !== 'string') {
        invalidRecordCount += 1;
      }

      for (const magnet of record.magnetLinks || []) {
        const link = magnet.link?.trim();
        if (link) {
          uniqueMagnets.add(link);
        }
      }
    }

    const reconciliation = this.getSnapshotReconciliation(snapshot);
    const missingItems = reconciliation.expectedButNotPersistedIds;
    const lowConfidencePages =
      snapshot?.pageAudits
        ?.filter((item) => {
          const score =
            typeof item.confidenceScore === 'number'
              ? item.confidenceScore
              : item.confidence === 'high'
                ? 92
                : item.confidence === 'medium'
                  ? 72
                  : 40;
          return score < 60;
        })
        .map((item) => item.pageNumber) || [];

    const passed =
      duplicateCount === 0 &&
      invalidRecordCount === 0 &&
      missingItems.length === 0 &&
      reconciliation.expectedButNotQueuedIds.length === 0 &&
      reconciliation.processedButNotPersistedIds.length === 0 &&
      lowConfidencePages.length === 0;

    return {
      generatedAt: new Date().toISOString(),
      totalRecords: records.length,
      uniqueRecords: uniqueKeys.size,
      duplicateCount,
      invalidRecordCount,
      expectedItemCount: reconciliation.expectedIds.length,
      persistedItemCount: reconciliation.persistedIds.length,
      missingFromQueueCount: missingItems.length,
      expectedButNotQueuedCount: reconciliation.expectedButNotQueuedIds.length,
      processedButNotPersistedCount: reconciliation.processedButNotPersistedIds.length,
      uniqueMagnetCount: uniqueMagnets.size,
      lowConfidencePageCount: lowConfidencePages.length,
      missingItems,
      expectedButNotQueuedItems: reconciliation.expectedButNotQueuedIds,
      processedButNotPersistedItems: reconciliation.processedButNotPersistedIds,
      lowConfidencePages,
      passed,
      summary: passed
        ? '\u7ed3\u679c\u4e8c\u6b21\u6821\u9a8c\u901a\u8fc7\uff1a\u8f93\u51fa\u7ed3\u679c\u5185\u90e8\u4e00\u81f4\u6027\u6b63\u5e38\uff1b\u76ee\u6807\u662f\u5426\u8865\u9f50\u8bf7\u4ee5\u6700\u7ec8\u4efb\u52a1\u6c47\u603b\u4e3a\u51c6\u3002'
        : `\u7ed3\u679c\u4e8c\u6b21\u6821\u9a8c\u5b8c\u6210\uff1a\u91cd\u590d ${duplicateCount} \u6761\uff0c\u5f02\u5e38 ${invalidRecordCount} \u6761\uff0c\u7f3a\u5931 ${missingItems.length} \u6761\uff0c\u5165\u961f\u7f3a\u53e3 ${reconciliation.expectedButNotQueuedIds.length} \u6761\uff0c\u5df2\u5904\u7406\u672a\u843d\u76d8 ${reconciliation.processedButNotPersistedIds.length} \u6761\uff0c\u4f4e\u53ef\u4fe1\u5206\u9875 ${lowConfidencePages.length} \u9875\u3002`
    };
  }

  private static loadRecords(filePath: string): FilmData[] {
    if (!fs.existsSync(filePath)) {
      return [];
    }

    try {
      const parsed = JSON.parse(fs.readFileSync(filePath, 'utf8'));
      return Array.isArray(parsed) ? parsed : [];
    } catch {
      return [];
    }
  }

  private static createRecordKey(record: FilmData): string {
    return createFilmIdentityKey(record);
  }

  private static getSnapshotReconciliation(snapshot: TaskStateSnapshot | null): SnapshotReconciliation {
    if (!snapshot) {
      return {
        expectedIds: [],
        persistedIds: [],
        processedIds: [],
        expectedButNotQueuedIds: [],
        expectedButNotPersistedIds: [],
        processedButNotPersistedIds: []
      };
    }

    if (snapshot.reconciliation) {
      return {
        expectedIds: snapshot.reconciliation.expectedIds || [],
        persistedIds: snapshot.reconciliation.persistedIds || [],
        processedIds: snapshot.reconciliation.processedIds || [],
        expectedButNotQueuedIds: snapshot.reconciliation.expectedButNotQueuedIds || [],
        expectedButNotPersistedIds: snapshot.reconciliation.expectedButNotPersistedIds || [],
        processedButNotPersistedIds: snapshot.reconciliation.processedButNotPersistedIds || []
      };
    }

    const expectedIds = snapshot.links?.expectedIds || snapshot.links?.queuedIds || snapshot.links?.queued || [];
    const queuedIds = snapshot.links?.queuedIds || snapshot.links?.queued || [];
    const processedIds = snapshot.links?.processedIds || snapshot.links?.processed || [];
    const persistedIds =
      snapshot.links?.persistedIds || snapshot.links?.persistedFilmIds || snapshot.links?.persisted || [];
    const queued = new Set(queuedIds);
    const persisted = new Set(persistedIds);
    const processed = new Set(processedIds);

    return {
      expectedIds,
      persistedIds,
      processedIds,
      expectedButNotQueuedIds: Array.from(new Set(expectedIds)).filter((item) => !queued.has(item)),
      expectedButNotPersistedIds: Array.from(new Set(expectedIds)).filter((item) => !persisted.has(item)),
      processedButNotPersistedIds: Array.from(new Set(processedIds)).filter((item) => !persisted.has(item))
    };
  }

  private static getMissingItems(snapshot: TaskStateSnapshot | null): string[] {
    if (!snapshot) {
      return [];
    }

    const reconciliation = this.getSnapshotReconciliation(snapshot);
    if (reconciliation.expectedButNotPersistedIds.length > 0) {
      return reconciliation.expectedButNotPersistedIds;
    }

    const queued = new Set(snapshot.links?.queued || []);
    const processed = new Set(snapshot.links?.processed || []);
    return Array.from(queued)
      .filter((item) => !processed.has(item))
      .map((item) => extractFilmId(item) || item);
  }
}

export default ResultValidator;
