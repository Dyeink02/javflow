const assert = require('assert');

const progressSchema = require('../desktop/common/progressSchema.js');

describe('progressSchema shared contract', () => {
  it('builds organizer progress wording from structured payloads only', () => {
    const message = progressSchema.buildOrganizerProgressMessage({
      phase: progressSchema.ORGANIZER_PROGRESS_PHASES.scanCompleted,
      waitingTotal: 12,
      deleteTotal: 3,
      introAdTotal: 1
    });

    assert.strictEqual(message, '扫描完成，待整理 12 个，待删除 3 个，含开头广告 1 个。');
  });

  it('normalizes ad file action and builds shared progress payloads', () => {
    assert.strictEqual(progressSchema.normalizeAdFileAction('delete-directly'), 'delete-directly');
    assert.strictEqual(progressSchema.normalizeAdFileAction('anything-else'), 'move-to-delete');

    const payload = progressSchema.createProgress('learning', 'matching', {
      processedVideos: 3
    });

    assert.strictEqual(payload.scope, 'learning');
    assert.strictEqual(payload.phase, 'matching');
    assert.strictEqual(payload.processedVideos, 3);
    assert.ok(payload.timestamp);
  });

  it('routes scope-specific wording through the shared progress dispatcher', () => {
    const learningMessage = progressSchema.buildProgressMessage({
      scope: 'learning',
      phase: progressSchema.LEARNING_PROGRESS_PHASES.completed,
      matchedVideoCount: 5,
      sampleIncrement: 2,
      missingCodeCount: 1,
      hitRate: 80,
      falsePositiveRate: 4.5
    });

    assert.ok(learningMessage.includes('按番号学习完成'));
    assert.ok(learningMessage.includes('命中视频 5'));
    assert.ok(learningMessage.includes('新增样本 2'));
  });

  it('shows finalization sub-progress and the active path', () => {
    const message = progressSchema.buildOrganizerProgressMessage({
      phase: progressSchema.ORGANIZER_PROGRESS_PHASES.finalizeProgress,
      finalizeTotal: 4,
      finalizeProcessed: 1,
      operation: '一次性删除批量临时目录',
      subTotal: 20,
      subProcessed: 8,
      currentPath: 'Z:/media/slow-folder',
      heartbeat: true,
      elapsedMs: 1800
    });

    assert.ok(message.includes('整理收尾进度 1/4'));
    assert.ok(message.includes('当前子任务 8/20'));
    assert.ok(message.includes('Z:/media/slow-folder'));
    assert.ok(message.includes('仍在处理'));
  });
});
