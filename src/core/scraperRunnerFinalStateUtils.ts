import type { RunnerStatePayload, RunnerStatus } from './scraperRunnerTypes';

export const RUNNER_STATUS_LABELS: Record<RunnerStatus, string> = {
  idle: '待机',
  starting: '启动中',
  running: '运行中',
  stopping: '终止中',
  completed: '已完成',
  stopped: '已终止',
  error: '异常',
  incomplete: '未完成'
};

export function getRunnerStatusLabel(status: RunnerStatus): string {
  return RUNNER_STATUS_LABELS[status] || status;
}

export interface FinalStateBuildInput {
  unresolvedCount: number;
  queueGapCount: number;
  processedGapCount: number;
  failedCount: number;
  lowConfidencePageCount: number;
  duplicateExpectedCount: number;
  duplicateItemIds: string[];
  duplicateItemSummary: string;
  unfinishedItems: string[];
  expectedEntryCount: number;
  rawDuplicateEntryCount: number;
  duplicateSummary: string;
  configuredTargetCount: number;
  validationPassed: boolean;
  secondValidationEnabled: boolean;
  completedCount: number;
  skippedByPolicyCount: number;
  expectedUniqueCount: number;
}

function buildSkippedMessage(skippedByPolicyCount: number): string {
  if (skippedByPolicyCount <= 0) {
    return '';
  }

  return `；按当前配置跳过无磁力影片 ${skippedByPolicyCount} 条`;
}

function buildFinishedMessage(input: FinalStateBuildInput): string {
  const {
    secondValidationEnabled,
    skippedByPolicyCount,
    rawDuplicateEntryCount,
    expectedEntryCount,
    duplicateSummary,
    expectedUniqueCount
  } = input;

  const validationText = secondValidationEnabled ? '，已二次校验完成' : '';
  const skippedText = buildSkippedMessage(skippedByPolicyCount);

  if (rawDuplicateEntryCount > 0) {
    return (
      `抓取任务已完成${validationText}。` +
      `站点原始条目 ${expectedEntryCount} 条，其中重复番号 ${rawDuplicateEntryCount} 条（${duplicateSummary}），` +
      `按唯一番号完成 ${expectedUniqueCount} 条${skippedText}。`
    );
  }

  return `抓取任务已完成${validationText}${skippedText}。`;
}

function pushMessage(target: string[], message: string): void {
  const normalized = String(message || '').trim();
  if (normalized) {
    target.push(normalized);
  }
}

export function buildFinalRunnerState(
  input: FinalStateBuildInput
): Pick<RunnerStatePayload, 'status' | 'message'> {
  const {
    unresolvedCount,
    queueGapCount,
    processedGapCount,
    failedCount,
    lowConfidencePageCount,
    duplicateExpectedCount,
    duplicateItemIds,
    duplicateItemSummary,
    unfinishedItems,
    expectedEntryCount,
    rawDuplicateEntryCount,
    duplicateSummary,
    configuredTargetCount,
    validationPassed,
    secondValidationEnabled,
    completedCount,
    skippedByPolicyCount,
    expectedUniqueCount
  } = input;

  const unfinishedPreview = unfinishedItems.slice(0, 6).join('、');
  const targetShortfall =
    configuredTargetCount > 0 ? Math.max(configuredTargetCount - expectedEntryCount, 0) : 0;
  const completionTargetCount =
    configuredTargetCount > 0 ? Math.max(0, configuredTargetCount - rawDuplicateEntryCount) : expectedUniqueCount;
  const resolvedCount = completedCount + skippedByPolicyCount;
  const completionShortfall = Math.max(completionTargetCount - resolvedCount, 0);
  const hasGap =
    unresolvedCount > 0 ||
    queueGapCount > 0 ||
    processedGapCount > 0 ||
    failedCount > 0 ||
    lowConfidencePageCount > 0 ||
    targetShortfall > 0 ||
    completionShortfall > 0 ||
    !validationPassed;

  if (!hasGap) {
    return {
      status: 'completed',
      message: buildFinishedMessage(input)
    };
  }

  const messages: string[] = [];

  if (validationPassed && secondValidationEnabled) {
    pushMessage(messages, '输出结果已通过二次校验，但目标条数仍未补齐');
  }

  if (targetShortfall > 0) {
    if (rawDuplicateEntryCount > 0 && duplicateSummary) {
      pushMessage(
        messages,
        `站点原始分页仅解析到 ${expectedEntryCount} 条，较目标 ${configuredTargetCount} 条少 ${targetShortfall} 条；其中重复番号 ${rawDuplicateEntryCount} 条（${duplicateSummary}）`
      );
    } else {
      pushMessage(
        messages,
        `站点原始分页仅解析到 ${expectedEntryCount} 条，较目标 ${configuredTargetCount} 条少 ${targetShortfall} 条`
      );
    }
  } else if (rawDuplicateEntryCount > 0 && duplicateSummary) {
    pushMessage(messages, `站点原始分页存在 ${rawDuplicateEntryCount} 条重复番号（${duplicateSummary}）`);
  }

  if (completionShortfall > 0) {
    if (configuredTargetCount > 0) {
      const skipHint =
        skippedByPolicyCount > 0 ? `，按配置跳过 ${skippedByPolicyCount} 条` : '';
      pushMessage(
        messages,
        `按唯一番号计算理论应完成 ${completionTargetCount} 条，当前已完成 ${completedCount} 条${skipHint}，仍少 ${completionShortfall} 条`
      );
    } else {
      pushMessage(messages, `完成结果仍比理论目标少 ${completionShortfall} 条`);
    }
  } else if (skippedByPolicyCount > 0) {
    pushMessage(messages, `已按当前配置跳过 ${skippedByPolicyCount} 条无磁力影片`);
  }

  if (unfinishedItems.length > 0) {
    const previewSuffix =
      unfinishedPreview.length > 0
        ? `（${unfinishedPreview}${unfinishedItems.length > 6 ? ' 等' : ''}）`
        : '';
    pushMessage(messages, `已定位 ${unfinishedItems.length} 条未完成番号${previewSuffix}`);
  } else if (unresolvedCount > 0 || completionShortfall > 0) {
    pushMessage(messages, '剩余缺口暂未定位到具体番号，请结合分页缺口与失败详情页核对');
  }

  if (queueGapCount > 0) {
    pushMessage(messages, `存在 ${queueGapCount} 条入队缺口`);
  }

  if (processedGapCount > 0) {
    pushMessage(messages, `存在 ${processedGapCount} 条已处理但未落盘项目`);
  }

  if (failedCount > 0) {
    pushMessage(messages, `存在 ${failedCount} 条失败详情页`);
  }

  if (lowConfidencePageCount > 0) {
    pushMessage(messages, `存在 ${lowConfidencePageCount} 个低可信分页`);
  }

  if (!validationPassed) {
    pushMessage(messages, '输出结果二次校验未通过');
  }

  if (duplicateItemIds.length > 0) {
    pushMessage(messages, `发现 ${duplicateItemIds.length} 条重复番号（${duplicateItemSummary}）`);
  } else if (duplicateExpectedCount > 0 && rawDuplicateEntryCount === 0) {
    pushMessage(messages, `发现 ${duplicateExpectedCount} 条重复分页编号`);
  }

  return {
    status: 'incomplete',
    message: `任务未完成：${messages.join('；')}。`
  };
}
