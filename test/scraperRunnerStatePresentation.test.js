const assert = require('assert');

const {
  buildFinalRunnerState,
  getRunnerStatusLabel
} = require('../dist/core/scraperRunnerFinalStateUtils');
const {
  buildUnfinishedReportLines,
  buildPageGapItems,
  buildRawDuplicateSummary
} = require('../dist/core/scraperRunnerStateUtils');

describe('scraperRunner state presentation', () => {
  it('returns localized runner status labels', () => {
    assert.strictEqual(getRunnerStatusLabel('completed'), '已完成');
    assert.strictEqual(getRunnerStatusLabel('stopping'), '终止中');
  });

  it('builds a clean completed final message', () => {
    const result = buildFinalRunnerState({
      unresolvedCount: 0,
      queueGapCount: 0,
      processedGapCount: 0,
      failedCount: 0,
      lowConfidencePageCount: 0,
      duplicateExpectedCount: 0,
      duplicateItemIds: [],
      duplicateItemSummary: '',
      unfinishedItems: [],
      expectedEntryCount: 120,
      rawDuplicateEntryCount: 0,
      duplicateSummary: '',
      configuredTargetCount: 120,
      validationPassed: true,
      secondValidationEnabled: true,
      completedCount: 120,
      skippedByPolicyCount: 0,
      expectedUniqueCount: 120
    });

    assert.strictEqual(result.status, 'completed');
    assert.ok(result.message.includes('抓取任务已完成'));
    assert.ok(result.message.includes('已二次校验完成'));
  });

  it('builds an incomplete final message with actionable hints', () => {
    const result = buildFinalRunnerState({
      unresolvedCount: 2,
      queueGapCount: 1,
      processedGapCount: 1,
      failedCount: 1,
      lowConfidencePageCount: 1,
      duplicateExpectedCount: 0,
      duplicateItemIds: ['ABP-001'],
      duplicateItemSummary: 'ABP-001',
      unfinishedItems: ['ABP-002', 'ABP-003'],
      expectedEntryCount: 58,
      rawDuplicateEntryCount: 0,
      duplicateSummary: '',
      configuredTargetCount: 60,
      validationPassed: false,
      secondValidationEnabled: true,
      completedCount: 56,
      skippedByPolicyCount: 0,
      expectedUniqueCount: 58
    });

    assert.strictEqual(result.status, 'incomplete');
    assert.ok(result.message.includes('任务未完成'));
    assert.ok(result.message.includes('已定位 2 条未完成番号'));
    assert.ok(result.message.includes('输出结果二次校验未通过'));
  });

  it('builds unfinished report lines in clean Chinese', () => {
    const pageGapLines = buildPageGapItems([
      {
        pageNumber: 3,
        actualCount: 24,
        expectedCount: 30,
        confidence: 'medium',
        confidenceScore: 72,
        reason: '尾页不足'
      }
    ]);

    const lines = buildUnfinishedReportLines({
      status: 'incomplete',
      message: '任务未完成：存在分页缺口。',
      filmCount: 56,
      configuredTargetCount: 60,
      expectedEntryCount: 58,
      rawDuplicateGroups: [
        {
          itemId: 'ABP-001',
          links: ['https://www.javbus.com/ABP-001', 'https://www.javbus.com/ABP-001?dup=1']
        }
      ],
      rawDuplicateEntryCount: 1,
      unfinishedItems: ['ABP-002', 'ABP-003'],
      pageGapLines,
      failedDetails: [
        {
          item: 'ABP-002',
          category: '详情页失败',
          reason: '请求超时',
          retryAdvice: '稍后重试'
        }
      ],
      skippedByPolicyCount: 0
    });

    assert.ok(lines.includes('# 任务状态：未完成'));
    assert.ok(lines.includes('# 已定位未完成番号'));
    assert.ok(lines.includes('ABP-002'));
    assert.ok(lines.some((line) => line.includes('失败详情页')));
    assert.ok(lines.some((line) => line.includes('第 3 页缺少 6 条')));
    assert.strictEqual(buildRawDuplicateSummary([{ itemId: 'ABP-001', links: ['a', 'b'] }]), 'ABP-001');
  });
});
