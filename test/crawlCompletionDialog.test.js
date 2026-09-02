const assert = require('assert');
const fs = require('fs');
const vm = require('vm');

const controllerSource = fs.readFileSync(
  require.resolve('../desktop/renderer/crawlRuntimeController.js'),
  'utf8'
);

function createControllerHarness({ withQualitySummary = false } = {}) {
  const handlers = {
    stateCallbacks: [],
    qualitySummaryCallbacks: [],
    state: (payload) => handlers.stateCallbacks.slice().forEach((callback) => callback(payload)),
    qualitySummary: (payload) => handlers.qualitySummaryCallbacks.slice().forEach((callback) => callback(payload))
  };
  const alerts = [];
  const openedMagnets = [];
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
    openMagnetFile: async (target) => {
      openedMagnets.push(target);
      return 'opened';
    },
    onLog: (callback) => {
      handlers.log = callback;
      return () => {};
    },
    onState: (callback) => {
      handlers.stateCallbacks.push(callback);
      return () => {
        const index = handlers.stateCallbacks.indexOf(callback);
        if (index >= 0) handlers.stateCallbacks.splice(index, 1);
      };
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
      handlers.qualitySummaryCallbacks.push(callback);
      return () => {
        const index = handlers.qualitySummaryCallbacks.indexOf(callback);
        if (index >= 0) handlers.qualitySummaryCallbacks.splice(index, 1);
      };
    };
  }

  const noop = () => {};
  function createController() {
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
    return controller;
  }

  createController();
  return { alerts, handlers, openedMagnets, createController };
}

function flushNotifications() {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

describe('crawl completion dialogs', () => {
  it('merges report and magnet question into one dialog for a completed run', async () => {
    const harness = createControllerHarness({ withQualitySummary: true });

    harness.handlers.qualitySummary({
      status: 'ok',
      completed: true,
      summaryLine: '抓取任务已完成',
      reportPath: 'C:\\JavFlow\\output\\quality-summary.txt'
    });
    await flushNotifications();

    assert.strictEqual(harness.alerts.length, 1);
    assert.strictEqual(harness.alerts[0].title, '抓取完成');
    assert.ok(harness.alerts[0].message.includes('是否打开磁力链接文件？'));
    assert.deepStrictEqual([...harness.alerts[0].buttons], ['打开磁力链接文件', '关闭']);
    assert.deepStrictEqual(harness.openedMagnets, ['C:\\JavFlow\\output']);
  });

  it('keeps one dialog per crawl when the same output is crawled again', async () => {
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

    assert.strictEqual(harness.alerts.length, 2);
    assert.deepStrictEqual(
      harness.alerts.map((item) => item.title),
      ['抓取完成', '抓取完成']
    );
    harness.alerts.forEach((item) => {
      assert.ok(item.message.includes('是否打开磁力链接文件？'));
      assert.deepStrictEqual([...item.buttons], ['打开磁力链接文件', '关闭']);
    });
  });

  it('merges the magnet question into the incomplete dialog too', async () => {
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

    assert.strictEqual(harness.alerts.length, 1);
    assert.strictEqual(harness.alerts[0].title, '抓取未完全完成');
    assert.ok(harness.alerts[0].message.includes('是否打开磁力链接文件？'));
    assert.deepStrictEqual([...harness.alerts[0].buttons], ['打开磁力链接文件', '关闭']);
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
    harness.alerts.forEach((item) => {
      assert.deepStrictEqual([...item.buttons], ['确定']);
      assert.ok(!item.message.includes('是否打开磁力链接文件'));
    });
  });

  it('deduplicates quality-summary and state completion for the same run', async () => {
    const harness = createControllerHarness({ withQualitySummary: true });
    const outputDir = 'C:\\JavFlow\\output';

    harness.handlers.state({ status: 'starting', message: '开始' });
    harness.handlers.state({ status: 'running', message: '运行中', outputDir });
    // 质量摘要事件先到（携带报告路径，目录与输出目录一致）
    harness.handlers.qualitySummary({
      status: 'ok',
      completed: true,
      summaryLine: '抓取任务已完成',
      reportPath: outputDir + '\\crawl-quality-summary.txt'
    });
    // 状态终态事件后到（同一输出目录、同一完成态）——必须被跨源去重
    harness.handlers.state({ status: 'completed', message: '完成', outputDir });
    await flushNotifications();

    assert.strictEqual(harness.alerts.length, 1);
    assert.strictEqual(harness.alerts[0].title, '抓取完成');
    assert.strictEqual(harness.openedMagnets.length, 1);
  });

  it('deduplicates completed and incomplete terminal statuses from the same run', async () => {
    const harness = createControllerHarness({ withQualitySummary: true });
    const outputDir = 'C:\\JavFlow\\output';

    harness.handlers.state({ status: 'starting', message: '开始', outputDir });
    harness.handlers.state({ status: 'running', message: '运行中', outputDir });
    harness.handlers.qualitySummary({
      status: 'warning',
      completed: true,
      summaryLine: '存在告警但已结束',
      outputDir
    });
    harness.handlers.state({ status: 'incomplete', message: '部分完成', outputDir });
    await flushNotifications();

    assert.strictEqual(harness.alerts.length, 1);
  });

  it('shares the completion lock across duplicate controller instances', async () => {
    const harness = createControllerHarness();
    harness.createController();
    const outputDir = 'C:\\JavFlow\\output';

    harness.handlers.state({ status: 'starting', message: '开始', outputDir });
    harness.handlers.state({ status: 'running', message: '运行中', outputDir });
    harness.handlers.state({ status: 'completed', message: '完成', outputDir });
    await flushNotifications();

    assert.strictEqual(harness.alerts.length, 1);
  });
});
