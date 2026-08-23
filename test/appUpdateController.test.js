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
  it('hides status when the current version is already latest', async () => {
    const harness = createHarness();

    await harness.controller.check();

    assert.strictEqual(harness.controller.getState(), 'latest');
    assert.strictEqual(harness.status.hidden, true);
    assert.strictEqual(harness.status.textContent, '');
    assert.strictEqual(harness.button.textContent, '检查更新');
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
