const assert = require('assert');
const fs = require('fs');
const os = require('os');
const path = require('path');

const { createCommandRouter } = require('../desktop/sidecar/commandRouter');
const { createCrawlService } = require('../desktop/sidecar/services/crawlService');
const { initializeRuntimeContext } = require('../desktop/sidecar/runtimePaths');

function createEventBusStub() {
  return {
    events: [],
    emitEvent() {},
    emitLifecycle() {},
    emitLog() {},
    emitState() {},
    emitLogContext() {}
  };
}

function createFakeRunnerRuntime(tempRoot) {
  const coreDir = path.join(tempRoot, 'dist', 'core');
  fs.mkdirSync(coreDir, { recursive: true });

  fs.writeFileSync(
    path.join(coreDir, 'scraperRunner.js'),
    `
const { EventEmitter } = require('events');

class FakeRunner extends EventEmitter {
  constructor(options) {
    super();
    global.__sidecarRunnerState = global.__sidecarRunnerState || {};
    global.__sidecarRunnerState.lastRunnerOptions = options;
  }

  async run() {
    global.__sidecarRunnerState = global.__sidecarRunnerState || {};
    global.__sidecarRunnerState.runCalls = (global.__sidecarRunnerState.runCalls || 0) + 1;
    this.emit('log', {
      level: 'info',
      message: 'runner-started',
      timestamp: new Date().toISOString()
    });
    this.emit('state', {
      status: 'running',
      message: '正在抓取'
    });
    return new Promise((resolve) => {
      this.__resolveRun = resolve;
    });
  }

  async stop() {
    global.__sidecarRunnerState = global.__sidecarRunnerState || {};
    if (this.__stopped) {
      return;
    }
    this.__stopped = true;
    global.__sidecarRunnerState.stopCalls = (global.__sidecarRunnerState.stopCalls || 0) + 1;
    this.emit('state', {
      status: 'stopping',
      message: '正在停止'
    });
    this.emit('state', {
      status: 'stopped',
      message: '已停止'
    });
    if (this.__resolveRun) {
      this.__resolveRun();
      this.__resolveRun = null;
    }
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
    return {
      outputDir: options.outputDir,
      createdRunDir: false
    };
  }
};
`,
    'utf8'
  );
}

describe('sidecar crawl service slimming', () => {
  const originalArchivedSidecarDomainsEnv = process.env.JAV_ENABLE_ARCHIVED_SIDECAR_DOMAINS;

  beforeEach(() => {
    global.__sidecarRunnerState = {};
    delete process.env.JAV_ENABLE_ARCHIVED_SIDECAR_DOMAINS;
  });

  afterEach(() => {
    delete global.__sidecarRunnerState;
    if (typeof originalArchivedSidecarDomainsEnv === 'undefined') {
      delete process.env.JAV_ENABLE_ARCHIVED_SIDECAR_DOMAINS;
    } else {
      process.env.JAV_ENABLE_ARCHIVED_SIDECAR_DOMAINS = originalArchivedSidecarDomainsEnv;
    }
  });

  it('does not create desktop settings files in Go task controller mode', async () => {
    const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-sidecar-go-'));
    const userDataDir = path.join(tempRoot, 'userData');
    createFakeRunnerRuntime(tempRoot);
    initializeRuntimeContext({
      repoRoot: tempRoot,
      appPath: tempRoot,
      resourcesPath: tempRoot,
      userData: userDataDir,
      documents: path.join(tempRoot, 'documents'),
      temp: path.join(tempRoot, 'temp')
    });

    const service = createCrawlService({
      fs,
      eventBus: createEventBusStub()
    });

    await service.start({
      output: path.join(tempRoot, 'output'),
      outputResolved: true,
      goTaskController: true
    });

    const settingsPath = path.join(userDataDir, 'desktop-settings.json');
    assert.strictEqual(fs.existsSync(settingsPath), false);
    assert.strictEqual(global.__sidecarRunnerState.runCalls, 1);
    assert.strictEqual(global.__sidecarRunnerState.lastRunnerOptions.output, path.join(tempRoot, 'output'));
  });

  it('uses the slim go runner host for start and stop in Go task controller mode', async () => {
    const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-sidecar-go-host-'));
    createFakeRunnerRuntime(tempRoot);
    initializeRuntimeContext({
      repoRoot: tempRoot,
      appPath: tempRoot,
      resourcesPath: tempRoot,
      userData: path.join(tempRoot, 'userData'),
      documents: path.join(tempRoot, 'documents'),
      temp: path.join(tempRoot, 'temp')
    });

    const service = createCrawlService({
      fs,
      eventBus: createEventBusStub()
    });

    const started = await service.start({
      output: path.join(tempRoot, 'output'),
      outputResolved: true,
      goTaskController: true
    });
    assert.deepStrictEqual(started, { ok: true });

    const stopped = await service.stop();
    assert.deepStrictEqual(stopped, { ok: true });

    await service.shutdown();

    assert.strictEqual(global.__sidecarRunnerState.runCalls, 1);
    assert.strictEqual(global.__sidecarRunnerState.stopCalls, 1);
  });

  it('routes restart through the slim go runner host in Go task controller mode', async () => {
    const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-sidecar-go-restart-'));
    createFakeRunnerRuntime(tempRoot);
    initializeRuntimeContext({
      repoRoot: tempRoot,
      appPath: tempRoot,
      resourcesPath: tempRoot,
      userData: path.join(tempRoot, 'userData'),
      documents: path.join(tempRoot, 'documents'),
      temp: path.join(tempRoot, 'temp')
    });

    const service = createCrawlService({
      fs,
      eventBus: createEventBusStub()
    });

    const started = await service.start({
      output: path.join(tempRoot, 'output'),
      outputResolved: true,
      goTaskController: true
    });
    assert.deepStrictEqual(started, { ok: true });

    const restarted = await service.restart({
      output: path.join(tempRoot, 'output'),
      outputResolved: true,
      goTaskController: true
    });
    assert.deepStrictEqual(restarted, { ok: true, restarting: true });

    const stopped = await service.stop();
    assert.deepStrictEqual(stopped, { ok: true });

    await service.shutdown();

    assert.strictEqual(global.__sidecarRunnerState.runCalls, 2);
    assert.strictEqual(global.__sidecarRunnerState.stopCalls, 2);
    assert.strictEqual(global.__sidecarRunnerState.lastRunnerOptions.resumeExisting, true);
  });

  it('rejects removed legacy crawl sidecar actions', async () => {
    const router = createCommandRouter({
      fs,
      eventBus: createEventBusStub()
    });

    await assert.rejects(
      router.handle({
        domain: 'crawl',
        action: 'get-log-context',
        payload: {}
      }),
      /Unsupported crawl action/
    );

    await assert.rejects(
      router.handle({
        domain: 'crawl',
        action: 'get-integration-context',
        payload: {}
      }),
      /Unsupported crawl action/
    );
  });

  it('keeps archived ranking sidecar domain disabled by default', async () => {
    const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-sidecar-ranking-'));
    initializeRuntimeContext({
      repoRoot: tempRoot,
      appPath: tempRoot,
      resourcesPath: tempRoot,
      userData: path.join(tempRoot, 'userData'),
      documents: path.join(tempRoot, 'documents'),
      temp: path.join(tempRoot, 'temp')
    });

    const router = createCommandRouter({
      fs,
      eventBus: createEventBusStub()
    });

    await assert.rejects(
      router.handle({
        domain: 'ranking',
        action: 'inspect-target',
        payload: {
          targetUrl: 'not-a-valid-target'
        }
      }),
      /archived and disabled by default/
    );
  });

  it('keeps archived organizer sidecar domain disabled by default', async () => {
    const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-sidecar-organizer-'));
    initializeRuntimeContext({
      repoRoot: tempRoot,
      appPath: tempRoot,
      resourcesPath: tempRoot,
      userData: path.join(tempRoot, 'userData'),
      documents: path.join(tempRoot, 'documents'),
      temp: path.join(tempRoot, 'temp')
    });

    const router = createCommandRouter({
      fs,
      eventBus: createEventBusStub()
    });

    await assert.rejects(
      router.handle({
        domain: 'organizer',
        action: 'load-codes',
        payload: {
          outputDir: tempRoot
        }
      }),
      /archived and disabled by default/
    );
  });

  it('keeps archived learning sidecar domain disabled by default', async () => {
    const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-sidecar-learning-'));
    initializeRuntimeContext({
      repoRoot: tempRoot,
      appPath: tempRoot,
      resourcesPath: tempRoot,
      userData: path.join(tempRoot, 'userData'),
      documents: path.join(tempRoot, 'documents'),
      temp: path.join(tempRoot, 'temp')
    });

    const router = createCommandRouter({
      fs,
      eventBus: createEventBusStub()
    });

    await assert.rejects(
      router.handle({
        domain: 'learning',
        action: 'get-summary',
        payload: {}
      }),
      /archived and disabled by default/
    );
  });

  it('reports archived sidecar domains as enabled only with explicit env flag', async () => {
    const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-sidecar-bootstrap-'));
    initializeRuntimeContext({
      repoRoot: tempRoot,
      appPath: tempRoot,
      resourcesPath: tempRoot,
      userData: path.join(tempRoot, 'userData'),
      documents: path.join(tempRoot, 'documents'),
      temp: path.join(tempRoot, 'temp')
    });

    const router = createCommandRouter({
      fs,
      eventBus: createEventBusStub()
    });

    const defaultBootstrap = await router.handle({
      domain: 'system',
      action: 'bootstrap',
      payload: {
        runtimeContext: {}
      }
    });
    assert.strictEqual(defaultBootstrap.archivedDomainsEnabled, false);

    process.env.JAV_ENABLE_ARCHIVED_SIDECAR_DOMAINS = '1';
    const enabledRouter = createCommandRouter({
      fs,
      eventBus: createEventBusStub()
    });
    const enabledBootstrap = await enabledRouter.handle({
      domain: 'system',
      action: 'bootstrap',
      payload: {
        runtimeContext: {}
      }
    });
    assert.strictEqual(enabledBootstrap.archivedDomainsEnabled, true);
  });

  it('reports archived sidecar domains in ping responses too', async () => {
    const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-sidecar-ping-'));
    initializeRuntimeContext({
      repoRoot: tempRoot,
      appPath: tempRoot,
      resourcesPath: tempRoot,
      userData: path.join(tempRoot, 'userData'),
      documents: path.join(tempRoot, 'documents'),
      temp: path.join(tempRoot, 'temp')
    });

    const defaultRouter = createCommandRouter({
      fs,
      eventBus: createEventBusStub()
    });
    const defaultPing = await defaultRouter.handle({
      domain: 'system',
      action: 'ping',
      payload: {}
    });
    assert.strictEqual(defaultPing.archivedDomainsEnabled, false);

    process.env.JAV_ENABLE_ARCHIVED_SIDECAR_DOMAINS = 'true';
    const enabledRouter = createCommandRouter({
      fs,
      eventBus: createEventBusStub()
    });
    const enabledPing = await enabledRouter.handle({
      domain: 'system',
      action: 'ping',
      payload: {}
    });
    assert.strictEqual(enabledPing.archivedDomainsEnabled, true);
  });
});
