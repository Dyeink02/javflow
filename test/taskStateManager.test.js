const assert = require('assert');
const fs = require('fs');
const os = require('os');
const path = require('path');

const TaskStateManager = require('../dist/core/taskStateManager').default;

describe('TaskStateManager', () => {
  const originalStateDir = process.env.JAV_SCRAPY_STATE_DIR;
  let tempStateRoot = '';

  before(() => {
    tempStateRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-task-state-root-'));
    process.env.JAV_SCRAPY_STATE_DIR = tempStateRoot;
  });

  after(() => {
    if (originalStateDir) {
      process.env.JAV_SCRAPY_STATE_DIR = originalStateDir;
    } else {
      delete process.env.JAV_SCRAPY_STATE_DIR;
    }

    if (tempStateRoot) {
      fs.rmSync(tempStateRoot, { recursive: true, force: true });
    }
  });

  it('skips backup creation for lightweight snapshots and keeps backup for full snapshots', () => {
    const outputDir = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-task-state-output-'));
    const manager = new TaskStateManager(outputDir);
    const taskStatePath = manager.getTaskStatePath();
    const backupPath = path.join(path.dirname(taskStatePath), 'backups', 'task-state.json.bak');

    const baseSnapshot = {
      schemaVersion: 2,
      appVersion: '1.1.17',
      status: 'running',
      message: 'testing',
      updatedAt: new Date().toISOString(),
      startedAt: new Date().toISOString(),
      config: {
        base: 'https://www.javbus.com',
        output: outputDir,
        limit: 10,
        totalPages: 1,
        itemsPerPage: 30,
        parallel: 2,
        delay: 2,
        timeout: 30000,
        secondValidation: false,
        taskTemplate: 'balanced'
      },
      progress: {
        nextPageIndex: 1,
        expectedItemsPerPage: 30,
        queued: 1,
        attempted: 0,
        completed: 0
      },
      links: {
        expected: ['https://www.javbus.com/ABP-001'],
        queued: ['https://www.javbus.com/ABP-001'],
        processed: [],
        persisted: [],
        persistedFilmIds: []
      }
    };

    manager.saveSnapshot(baseSnapshot);
    assert.strictEqual(fs.existsSync(backupPath), false);

    manager.saveSnapshot(
      {
        ...baseSnapshot,
        progress: {
          ...baseSnapshot.progress,
          queued: 2
        }
      },
      { withBackup: false }
    );
    assert.strictEqual(fs.existsSync(backupPath), false);

    manager.saveSnapshot(
      {
        ...baseSnapshot,
        progress: {
          ...baseSnapshot.progress,
          queued: 3
        }
      },
      { withBackup: true }
    );
    assert.strictEqual(fs.existsSync(backupPath), true);

    fs.rmSync(outputDir, { recursive: true, force: true });
  });
});
