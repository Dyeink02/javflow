import type {
  DuplicateExpectedEntryGroup,
  FailedDetailRecord,
  PageAuditRecord
} from './taskStateManager';
import { getRunnerStatusLabel } from './scraperRunnerFinalStateUtils';
import type { RunnerStatus } from './scraperRunnerTypes';

export function pushPreviewItem(target: string[], item: string | null | undefined, limit = 160): void {
  const normalizedItem = String(item || '').trim();
  if (!normalizedItem) {
    return;
  }

  const existingIndex = target.indexOf(normalizedItem);
  if (existingIndex >= 0) {
    target.splice(existingIndex, 1);
  }

  target.push(normalizedItem);

  if (target.length > limit) {
    target.splice(0, target.length - limit);
  }
}

export function getRecentPreview(
  source: string[],
  predicate: (item: string) => boolean,
  limit: number
): string[] {
  const preview: string[] = [];
  const seen = new Set<string>();

  for (let index = source.length - 1; index >= 0 && preview.length < limit; index -= 1) {
    const item = source[index];
    if (!item || seen.has(item) || !predicate(item)) {
      continue;
    }

    seen.add(item);
    preview.push(item);
  }

  return preview;
}

export function buildRawDuplicateSummary(
  groups: DuplicateExpectedEntryGroup[],
  limit = 4
): string {
  if (groups.length === 0) {
    return '';
  }

  const preview = groups.slice(0, limit).map((group) => group.itemId).join('、');
  return groups.length > limit ? `${preview} 等 ${groups.length} 个番号` : preview;
}

export function buildRawDuplicateReportLines(groups: DuplicateExpectedEntryGroup[]): string[] {
  return groups.map((group) => {
    const linkCounts = new Map<string, number>();

    for (const link of group.links) {
      const normalizedLink = String(link || '').trim();
      if (!normalizedLink) {
        continue;
      }

      linkCounts.set(normalizedLink, (linkCounts.get(normalizedLink) || 0) + 1);
    }

    const summarizedLinks = Array.from(linkCounts.entries())
      .map(([link, count]) => (count > 1 ? `${link} (x${count})` : link))
      .join(' | ');

    return `${group.itemId} | 出现 ${group.links.length} 次 | ${summarizedLinks}`;
  });
}

export function buildRawDuplicateItemIds(groups: DuplicateExpectedEntryGroup[]): string[] {
  return groups.map((group) => group.itemId).sort((left, right) => left.localeCompare(right, 'zh-CN'));
}

export function buildPageGapItems(audits: PageAuditRecord[]): string[] {
  return audits.map((audit) => {
    const expectedCount = audit.expectedCount || 0;
    const missingCount = Math.max(expectedCount - audit.actualCount, 0);
    const confidenceScore =
      audit.confidenceScore ??
      (audit.confidence === 'high' ? 92 : audit.confidence === 'medium' ? 72 : 40);
    const reason = String(audit.reason || '').trim();

    return `第 ${audit.pageNumber} 页缺少 ${missingCount} 条（当前 ${audit.actualCount}/${expectedCount}，可信度 ${confidenceScore}${
      reason ? `，说明：${reason}` : ''
    }）`;
  });
}

export interface UnfinishedReportInput {
  status: RunnerStatus;
  message: string;
  filmCount: number;
  configuredTargetCount: number;
  expectedEntryCount: number;
  rawDuplicateGroups: DuplicateExpectedEntryGroup[];
  rawDuplicateEntryCount: number;
  unfinishedItems: string[];
  pageGapLines: string[];
  failedDetails: FailedDetailRecord[];
  skippedByPolicyCount?: number;
}

export function buildUnfinishedReportLines(input: UnfinishedReportInput): string[] {
  const {
    status,
    message,
    filmCount,
    configuredTargetCount,
    expectedEntryCount,
    rawDuplicateGroups,
    rawDuplicateEntryCount,
    unfinishedItems,
    pageGapLines,
    failedDetails,
    skippedByPolicyCount = 0
  } = input;

  const lines: string[] = [];
  const uniqueExpectedCount = Math.max(expectedEntryCount - rawDuplicateEntryCount, 0);
  const completionTargetCount =
    configuredTargetCount > 0 ? Math.max(0, configuredTargetCount - rawDuplicateEntryCount) : 0;
  const unknownCompletionShortfall =
    completionTargetCount > 0
      ? Math.max(completionTargetCount - filmCount - skippedByPolicyCount - unfinishedItems.length, 0)
      : 0;
  const targetShortfall = configuredTargetCount > 0 ? Math.max(configuredTargetCount - expectedEntryCount, 0) : 0;
  const duplicateItemIds = buildRawDuplicateItemIds(rawDuplicateGroups);

  lines.push(`# 任务状态：${getRunnerStatusLabel(status)}`);
  lines.push(`# 状态说明：${message}`);
  lines.push(`# 已完成：${filmCount}`);

  if (skippedByPolicyCount > 0) {
    lines.push(`# 按配置跳过无磁力：${skippedByPolicyCount}`);
  }

  if (configuredTargetCount > 0) {
    lines.push(`# 目标条数：${configuredTargetCount}`);
    lines.push(`# 站点原始条目：${expectedEntryCount}`);
    lines.push(`# 站点唯一番号：${uniqueExpectedCount}`);
  }

  if (rawDuplicateEntryCount > 0) {
    lines.push(`# 站点重复条目：${rawDuplicateEntryCount}`);
  }

  lines.push('# 已定位未完成番号');
  if (unfinishedItems.length > 0) {
    lines.push(...unfinishedItems);
  } else {
    lines.push('暂无已定位未完成番号。');
  }

  lines.push('# 已定位重复番号');
  if (rawDuplicateGroups.length > 0) {
    lines.push(...buildRawDuplicateReportLines(rawDuplicateGroups));
  } else {
    lines.push('当前未发现重复番号。');
  }

  if (failedDetails.length > 0) {
    lines.push('# 失败详情页');
    for (const detail of failedDetails) {
      const advice = detail.retryAdvice ? `；建议：${detail.retryAdvice}` : '';
      lines.push(`${detail.item} | ${detail.category || '失败'} | ${detail.reason}${advice}`);
    }
  }

  if (pageGapLines.length > 0) {
    lines.push('# 未定位分页缺口');
    lines.push(...pageGapLines);
  }

  if (unknownCompletionShortfall > 0) {
    lines.push(
      `# 提示：仍有 ${unknownCompletionShortfall} 条目标结果未定位到具体番号，请优先检查“未定位分页缺口”和“失败详情页”部分。`
    );
  }

  if (targetShortfall > 0) {
    lines.push(
      `# 提示：站点原始分页较目标少 ${targetShortfall} 条，请结合“已定位重复番号”和“未定位分页缺口”一起核对。`
    );
  }

  if (duplicateItemIds.length > 0 && rawDuplicateEntryCount === 0) {
    lines.push(`# 提示：当前检测到 ${duplicateItemIds.length} 个重复番号，但未形成额外条目差值。`);
  }

  return lines;
}
