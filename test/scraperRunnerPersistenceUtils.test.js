const assert = require('assert');

const {
  finalizeRunnerOutputArtifacts,
  persistRunnerTaskState,
  resolveRunnerSnapshotMode
} = require('../dist/core/scraperRunnerPersistenceUtils');

describe('scraperRunnerPersistenceUtils', () => {
  it('clears unfinished report after a clean completed run', () => {
    const calls = [];
    let cleaned = false;

    finalizeRunnerOutputArtifacts({
      hasConfig: true,
      status: 'completed',
      message: '抓取任务已完成。',
      fileHandler: {
        syncUnfinishedItemsReport(lines) {
          calls.push(lines);
        },
        cleanupLegacyOutputArtifacts() {
          calls.push('cleanup');
        }
      },
      uncapturedItemsTotal: 0,
      recoverablePageAuditCount: 0,
      buildUnfinishedReportLines: () => ['should-not-be-written'],
      cleanupRuntimeState: () => {
        cleaned = true;
      }
    });

    assert.deepStrictEqual(calls[0], []);
    assert.strictEqual(calls[1], 'cleanup');
    assert.strictEqual(cleaned, true);
  });

  it('persists snapshots with resolved mode and updates callbacks', () => {
    let savedSnapshot = null;
    let savedOptions = null;
    let persistedAt = 0;
    let mutationResetCalled = false;
    let debugMessage = '';

    persistRunnerTaskState({
      taskStateManager: {
        saveSnapshot(snapshot, options) {
          savedSnapshot = snapshot;
          savedOptions = options;
        }
      },
      reason: '测试落盘',
      status: 'running',
      message: '任务仍在运行',
      force: false,
      lastStatePersistAt: 0,
      statePersistMinIntervalMs: 1,
      buildTaskSnapshot: (status, message, mode) => ({ status, message, mode }),
      onPersisted: (timestamp) => {
        persistedAt = timestamp;
      },
      onMutationReset: () => {
        mutationResetCalled = true;
      },
      onDebug: (message) => {
        debugMessage = message;
      }
    });

    assert.deepStrictEqual(savedSnapshot, {
      status: 'running',
      message: '任务仍在运行',
      mode: 'light'
    });
    assert.deepStrictEqual(savedOptions, { withBackup: false });
    assert.ok(persistedAt > 0);
    assert.strictEqual(mutationResetCalled, true);
    assert.strictEqual(debugMessage, '任务状态已落盘：测试落盘');
    assert.strictEqual(resolveRunnerSnapshotMode('completed', false), 'full');
  });
});
