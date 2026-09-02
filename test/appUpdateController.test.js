const assert = require('assert');
const fs = require('fs');
const vm = require('vm');

const controllerSource = fs.readFileSync(
  require.resolve('../desktop/renderer/appUpdateController.js'),
  'utf8'
);

function createNode() {
  const listeners = {};
  const classes = new Set();
  return {
    textContent: '',
    hidden: false,
    disabled: false,
    classList: {
      add: (...names) => names.forEach((name) => classes.add(name)),
      remove: (...names) => names.forEach((name) => classes.delete(name)),
      contains: (name) => classes.has(name)
    },
    addEventListener: (name, callback) => {
      listeners[name] = callback;
    },
    click: () => listeners.click && listeners.click(),
    get listeners() {
      return listeners;
    }
  };
}

function createHarness() {
  const sandbox = {
    console,
    setTimeout,
    clearTimeout,
    confirm: () => false
  };
  vm.runInNewContext(controllerSource, sandbox, {
    filename: 'appUpdateController.js'
  });

  const status = createNode();
  const button = createNode();
  const calls = [];
  let checkResult = { updateAvailable: false, latestVersion: '0.4.40' };
  const desktopApi = {
    checkAppUpdate: async () => {
      calls.push('check');
      return checkResult;
    },
    downloadAppUpdate: async (payload) => {
      calls.push(['download', payload]);
      return { updateAvailable: true, latestVersion: payload.version, downloadReady: true };
    },
    showAlert: async (options) => {
      calls.push(['alert', options]);
      return { selection: options.buttons[0] };
    },
    applyAppUpdate: async () => {
      calls.push('apply');
      return { applied: true, restartRequired: true };
    }
  };
  const controller = sandbox.desktopAppUpdateController.createAppUpdateController({
    elements: {
      crawlerUpdateStatus: status,
      crawlerCheckUpdateButton: button
    },
    desktopApi,
    autoCheckDelayMs: null
  });
  controller.bootstrap();
  return {
    controller,
    status,
    button,
    calls,
    setCheckResult: (next) => {
      checkResult = next;
    }
  };
}

describe('portable app update controller', () => {
  it('stays silent on auto-check when already latest, shows message on manual check', async () => {
    const harness = createHarness();

    // 启动自动检查：已是最新时不显示任何状态。
    await harness.controller.check();
    assert.strictEqual(harness.controller.getState(), 'latest');
    assert.strictEqual(harness.status.textContent, '');
    assert.strictEqual(harness.button.textContent, '检查更新');

    // 手动点击检查更新：显示“当前已是最新版本”。
    await harness.controller.check({ manual: true });
    assert.strictEqual(harness.status.hidden, false);
    assert.strictEqual(harness.status.textContent, '当前已是最新版本');
    assert.strictEqual(harness.button.textContent, '检查更新');
  });

  it('keeps auto-check silent for portable, prompts via dialog on manual check', async () => {
    const harness = createHarness();
    harness.setCheckResult({
      inAppUpdateSupported: false,
      installKind: 'portable',
      message: '便携版暂不支持在线升级，请前往 GitHub Release 页面下载最新便携包。'
    });

    // 启动自动检查：便携版完全静默。
    await harness.controller.check();
    assert.strictEqual(harness.controller.getState(), 'portable');
    assert.strictEqual(harness.status.textContent, '');
    assert.strictEqual(harness.button.textContent, '检查更新');
    assert.ok(!harness.calls.some((call) => Array.isArray(call) && call[0] === 'alert'));

    // 手动点击检查更新：弹窗提示便携版无法升级。
    await harness.controller.check({ manual: true });
    const alertCall = harness.calls.find((call) => Array.isArray(call) && call[0] === 'alert');
    assert.ok(alertCall, 'expected a portable guidance dialog');
    assert.ok(alertCall[1].message.includes('便携版暂不支持在线升级'));
    assert.strictEqual(harness.status.textContent, '');
    assert.strictEqual(harness.button.textContent, '检查更新');
    assert.ok(!harness.calls.some((call) => Array.isArray(call) && call[0] === 'download'));
  });

  it('moves from pending update to download and confirmed apply', async () => {
    const harness = createHarness();
    harness.setCheckResult({ updateAvailable: true, latestVersion: '0.4.40' });

    await harness.controller.check();
    assert.strictEqual(harness.controller.getState(), 'available');
    assert.strictEqual(harness.status.textContent, '待更新 v0.4.40');
    assert.strictEqual(harness.button.textContent, '下载更新');

    await harness.controller.download();
    assert.strictEqual(harness.controller.getState(), 'downloaded');
    assert.strictEqual(harness.status.textContent, '已下载 v0.4.40');
    assert.strictEqual(harness.button.textContent, '立即更新');

    await harness.controller.apply();
    assert.deepStrictEqual(harness.calls.map((item) => (Array.isArray(item) ? item[0] : item)), [
      'check',
      'download',
      'alert',
      'apply'
    ]);
  });
});
