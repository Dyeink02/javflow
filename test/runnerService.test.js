const assert = require('assert');
const fs = require('fs');
const os = require('os');
const path = require('path');

const { createRunnerService } = require('../desktop/mainServices/runnerService');

function createRuntimeModules(tempRoot) {
  const coreDir = path.join(tempRoot, 'dist', 'core');
  fs.mkdirSync(coreDir, { recursive: true });

  fs.writeFileSync(
    path.join(coreDir, 'scraperRunner.js'),
    `
const { EventEmitter } = require('events');

class FakeRunner extends EventEmitter {
  constructor(options) {
    super();
    global.__runnerServiceTestState = global.__runnerServiceTestState || {};
    global.__runnerServiceTestState.lastRunnerOptions = options;
  }

  async run() {
    return new Promise(() => {});
  }

  async stop() {
    global.__runnerServiceTestState = global.__runnerServiceTestState || {};
    global.__runnerServiceTestState.stopCalled = true;
  }

  static async updateAntiBlockUrls(options) {
    return options;
  }
}

module.exports = {
  default: FakeRunner
};
`,
    'utf8'
  );

  fs.writeFileSync(
    path.join(coreDir, 'outputRuntimeUtils.js'),
    `
module.exports = {
  resolveRunOutputDirectory(options) {
    global.__runnerServiceTestState = global.__runnerServiceTestState || {};
    global.__runnerServiceTestState.outputResolutionCalls =
      (global.__runnerServiceTestState.outputResolutionCalls || 0) + 1;
    return {
      outputDir: options.outputDir + '/run-1',
      createdRunDir: true
    };
  }
};
`,
    'utf8'
  );
}

function createBaseDependencies(tempRoot) {
  const callState = {
    saveSettingsCalls: [],
    initializeTaskLogFilesCalls: [],
    writeTaskLogCalls: [],
    rendererLogs: [],
    rendererStates: [],
    flushCalls: 0
  };

  const state = {
    activeRunner: null,
    currentTaskOutputDir: null,
    lastTaskOutputDir: null,
    quittingAfterStop: false,
    pendingRestartSettings: null
  };

  const logBridge = {
    async flushDesktopPipelines() {
      callState.flushCalls += 1;
    },
    initializeTaskLogFiles(outputDir, settings) {
      callState.initializeTaskLogFilesCalls.push({ outputDir, settings });
    },
    writeTaskLog(level, message) {
      callState.writeTaskLogCalls.push({ level, message });
    },
    queueRendererLogEntry(entry) {
      callState.rendererLogs.push(entry);
    },
    queueRendererLog(level, message, timestamp) {
      callState.rendererLogs.push({ level, message, timestamp });
    },
    queueRendererState(entry) {
      callState.rendererStates.push(entry);
    },
    getLogContext() {
      return {
        sessionLogPath: path.join(tempRoot, 'logs', 'latest-log.txt')
      };
    }
  };

  const settingsStore = {
    saveSettings(settings) {
      callState.saveSettingsCalls.push(settings);
    }
  };

  const runnerService = createRunnerService({
    state,
    app: {
      quit() {}
    },
    dialog: {
      async showMessageBox() {
        return { response: 0 };
      }
    },
    Notification: {
      isSupported() {
        return false;
      }
    },
    path,
    desktopRoot: path.join(tempRoot, 'desktop'),
    runtimePackage: {
      demoMode: 'aed',
      demoLabel: 'AED',
      productDisplayName: 'JavFlow'
    },
    appTitle: 'JavFlow',
    appVersion: '0.27.0',
    appDemoLabel: 'AED',
    mainText: {
      reminderFallback: '任务已完成',
      runnerBusy: '抓取器忙碌中',
      taskLogCreatedPrefix: '本次运行日志已创建：',
      continueRecovery: '继续抓取任务启动中...',
      restartFailedPrefix: '继续抓取失败：'
    },
    windowService: {
      getWindow() {
        return null;
      }
    },
    settingsStore,
    logBridge
  });

  return {
    runnerService,
    state,
    callState
  };
}

describe('runnerService Go task controller mode', () => {
  beforeEach(() => {
    global.__runnerServiceTestState = {};
  });

  afterEach(() => {
    delete global.__runnerServiceTestState;
  });

  it('skips local settings persistence and task log initialization when Go owns the crawl lifecycle', async () => {
    const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-runner-go-'));
    createRuntimeModules(tempRoot);
    const { runnerService, state, callState } = createBaseDependencies(tempRoot);

    await runnerService.startRunner({
      output: 'C:/temp/output',
      outputResolved: true,
      goTaskController: true
    });

    assert.strictEqual(callState.flushCalls, 1);
    assert.strictEqual(callState.saveSettingsCalls.length, 0);
    assert.strictEqual(callState.initializeTaskLogFilesCalls.length, 0);
    assert.strictEqual(callState.writeTaskLogCalls.length, 0);
    assert.strictEqual(state.currentTaskOutputDir, 'C:/temp/output');
    assert.strictEqual(state.lastTaskOutputDir, 'C:/temp/output');
    assert.strictEqual(global.__runnerServiceTestState.outputResolutionCalls || 0, 0);
  });

  it('keeps the local desktop lifecycle in legacy mode', async () => {
    const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-runner-legacy-'));
    createRuntimeModules(tempRoot);
    const { runnerService, state, callState } = createBaseDependencies(tempRoot);

    await runnerService.startRunner({
      output: 'C:/temp/output',
      outputResolved: false,
      resumeExisting: false,
      goTaskController: false
    });

    assert.strictEqual(callState.saveSettingsCalls.length, 1);
    assert.strictEqual(callState.initializeTaskLogFilesCalls.length, 1);
    assert.strictEqual(callState.writeTaskLogCalls.length, 2);
    assert.strictEqual(callState.initializeTaskLogFilesCalls[0].outputDir, 'C:/temp/output/run-1');
    assert.strictEqual(state.currentTaskOutputDir, 'C:/temp/output/run-1');
    assert.strictEqual(state.lastTaskOutputDir, 'C:/temp/output/run-1');
    assert.strictEqual(global.__runnerServiceTestState.outputResolutionCalls, 1);
  });
});
