const assert = require('assert');
const fs = require('fs');
const os = require('os');
const path = require('path');

const { createTaskLogService } = require('../desktop/sidecar/services/taskLogService.js');

describe('sidecar taskLogService compatibility bridge', () => {
  it('fans out renderer log batches and forwards state/log-context onto crawl events', async () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-sidecar-tasklog-'));
    const calls = {
      logs: [],
      states: [],
      contexts: []
    };

    const taskLogService = createTaskLogService({
      fs,
      eventBus: {
        emitLog(scope, entry) {
          calls.logs.push({ scope, entry });
        },
        emitState(scope, payload) {
          calls.states.push({ scope, payload });
        },
        emitLogContext(payload) {
          calls.contexts.push(payload);
        }
      }
    });

    const logBridge = taskLogService.getLogBridge();
    logBridge.initializeTaskLogFiles(tempDir, {
      base: 'https://www.javbus.com',
      demoLabel: 'AED'
    });
    logBridge.queueRendererLogEntry({
      level: 'info',
      message: 'compat-log-one',
      timestamp: '2026-05-08T00:00:00.000Z'
    });
    logBridge.queueRendererLogEntry({
      level: 'warn',
      message: 'compat-log-two',
      timestamp: '2026-05-08T00:00:01.000Z'
    });
    logBridge.queueRendererState({
      status: 'running',
      message: 'compat-state',
      timestamp: '2026-05-08T00:00:02.000Z'
    });

    await taskLogService.flush();

    assert.deepStrictEqual(
      calls.logs.map((item) => ({
        scope: item.scope,
        level: item.entry.level,
        message: item.entry.message,
        timestamp: item.entry.timestamp
      })),
      [
        {
          scope: 'crawl',
          level: 'info',
          message: 'compat-log-one',
          timestamp: '2026-05-08T00:00:00.000Z'
        },
        {
          scope: 'crawl',
          level: 'warn',
          message: 'compat-log-two',
          timestamp: '2026-05-08T00:00:01.000Z'
        }
      ]
    );

    assert.deepStrictEqual(
      calls.states.map((item) => ({
        scope: item.scope,
        status: item.payload.status,
        message: item.payload.message,
        timestamp: item.payload.timestamp
      })),
      [
        {
          scope: 'crawl',
          status: 'running',
          message: 'compat-state',
          timestamp: '2026-05-08T00:00:02.000Z'
        }
      ]
    );

    assert.strictEqual(calls.contexts.length, 1);
    assert.ok(String(calls.contexts[0].logDir || '').endsWith(path.join('logs')));
    assert.ok(String(calls.contexts[0].sessionLogPath || '').endsWith('.txt'));
    assert.ok(String(calls.contexts[0].latestLogPath || '').endsWith('latest-log.txt'));
  });
});
