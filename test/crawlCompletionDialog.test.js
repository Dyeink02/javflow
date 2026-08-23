const assert = require('assert');
const fs = require('fs');
const vm = require('vm');

const controllerSource = fs.readFileSync(
  require.resolve('../desktop/renderer/crawlRuntimeController.js'),
  'utf8'
);

function createControllerHarness({ withQualitySummary = false } = {}) {
  const handlers = {};
  const alerts = [];
  const sandbox = {
    console,
    setTimeout,
    clearTimeout,
    window: {
      addEventListener() {}
    }
  };
  vm.runInNewContext(controllerSource, sandbox, {
    filename: 'crawlRuntimeController.js'
  });

  const desktopApi = {
    showAlert: async (options) => {
      alerts.push(options);
      return { selection: options.buttons[0] };
    },
    openMagnetFile: async () => 'opened',
    onLog: (callback) => {
      handlers.log = callback;
      return () => {};
    },
    onState: (callback) => {
      handlers.state = callback;
      return () => {};
    },
    onLogContext: (callback) => {
      handlers.logContext = callback;
      return () => {};
    }
  };
  if (withQualitySummary) {
    desktopApi.getCrawlRunContext = async () => ({
      preferredOutputDir: 'C:\\JavFlow\\output'
    });
    desktopApi.onQualitySummary = (callback) => {
      handlers.qualitySummary = callback;
      return () => {};
    };
  }

  const noop = () => {};
  const controller = sandbox.desktopCrawlRuntimeController.createCrawlRuntimeController({
    desktopApi,
    platformBridge: null,
    elements: { output: { value: '' } },
    uiText: { UI_TEXT: { state: { defaultMessage: '' } }, STATUS_LABELS: {} },
    logController: { appendLog: noop, updateLogContext: noop },
    stateController: {
      setStatus: noop,
      enqueueUiState: noop,
      applyStagePanel: noop,
      applyResultPanel: noop,
      enqueueReviewPanel: noop,
      enqueueState: noop,
      clearResultHistory: noop
    },
    crawlPanelModel: null,
    subscriptionCrawlSessionBridge: { getSession: () => null },
    subscriptionController: null,
    organizerController: null,
    libraryMetadataController: null
  });

  controller.bindEventFeeds();
  return { alerts, handlers };
}

function flushNotifications() {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

describe('crawl completion dialogs', () => {
  it('shows the magnet-file question for a completed quality summary without a path', async () => {
    const harness = createControllerHarness({ withQualitySummary: true });

    harness.handlers.qualitySummary({
      status: 'ok',
      completed: true,
      summaryLine: '抓取任务已完成',
      reportPath: 'C:\\JavFlow\\output\\quality-summary.txt'
    });
    await flushNotifications();

    assert.strictEqual(harness.alerts.length, 2);
    assert.strictEqual(harness.alerts[0].title, '抓取完成');
    assert.strictEqual(harness.alerts[1].title, '打开磁力链接文件');
  });

  it('shows the dialogs again when the same output is crawled again', async () => {
    const harness = createControllerHarness();
    const outputDir = 'C:\\JavFlow\\output';

    harness.handlers.state({ status: 'starting', message: '开始' });
    harness.handlers.state({ status: 'running', message: '运行中', outputDir });
    harness.handlers.state({ status: 'completed', message: '完成', outputDir });
    await flushNotifications();

    harness.handlers.state({ status: 'starting', message: '重新开始' });
    harness.handlers.state({ status: 'running', message: '重新运行', outputDir });
    harness.handlers.state({ status: 'completed', message: '完成', outputDir });
    await flushNotifications();

    assert.strictEqual(harness.alerts.length, 4);
    assert.deepStrictEqual(
      harness.alerts.map((item) => item.title),
      ['抓取完成', '打开磁力链接文件', '抓取完成', '打开磁力链接文件']
    );
  });

  it('asks to open the magnet file when the run ends incomplete with output', async () => {
    const harness = createControllerHarness();
    const outputDir = 'C:\\JavFlow\\output';

    harness.handlers.state({ status: 'starting', message: '开始' });
    harness.handlers.state({ status: 'running', message: '运行中', outputDir });
    harness.handlers.state({
      status: 'incomplete',
      message: '抓取完成，但有部分番号失败',
      outputDir
    });
    await flushNotifications();

    assert.strictEqual(harness.alerts.length, 2);
    assert.strictEqual(harness.alerts[0].title, '抓取未完全完成');
    assert.strictEqual(harness.alerts[1].title, '打开磁力链接文件');
  });

  it('does not ask to open the magnet file for error or stopped runs', async () => {
    const harness = createControllerHarness();
    const outputDir = 'C:\\JavFlow\\output';

    harness.handlers.state({ status: 'starting', message: '开始' });
    harness.handlers.state({ status: 'running', message: '运行中', outputDir });
    harness.handlers.state({ status: 'error', message: '出错', outputDir });
    await flushNotifications();

    harness.handlers.state({ status: 'starting', message: '再次开始' });
    harness.handlers.state({ status: 'running', message: '运行中', outputDir });
    harness.handlers.state({ status: 'stopped', message: '已停止', outputDir });
    await flushNotifications();

    assert.strictEqual(harness.alerts.length, 2);
    assert.deepStrictEqual(
      harness.alerts.map((item) => item.title),
      ['抓取出错', '抓取已停止']
    );
  });
});
