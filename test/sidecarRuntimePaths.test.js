const assert = require('assert');
const os = require('os');
const path = require('path');

const {
  buildDefaultContext,
  initializeRuntimeContext,
  getRuntimeContext,
  getPathByName,
  createAppShim
} = require('../desktop/sidecar/runtimePaths.js');

describe('sidecar runtimePaths compatibility contract', () => {
  it('builds deterministic repo-local defaults for compatibility contexts', () => {
    const defaults = buildDefaultContext();

    assert.ok(path.isAbsolute(defaults.repoRoot));
    assert.strictEqual(defaults.appPath, defaults.repoRoot);
    assert.strictEqual(defaults.resourcesPath, defaults.repoRoot);
    assert.strictEqual(defaults.userData, path.join(defaults.repoRoot, '.wails-dev', 'userData'));
    assert.strictEqual(defaults.documents, path.join(os.homedir(), 'Documents'));
    assert.strictEqual(defaults.temp, os.tmpdir());
  });

  it('normalizes provided paths and exposes them through the app shim', () => {
    const context = initializeRuntimeContext({
      repoRoot: '.',
      appPath: '.\\app',
      resourcesPath: '.\\resources',
      userData: '.\\userdata',
      documents: '.\\documents',
      temp: '.\\temp'
    });

    assert.strictEqual(context.repoRoot, path.resolve('.'));
    assert.strictEqual(context.appPath, path.resolve('.\\app'));
    assert.strictEqual(context.resourcesPath, path.resolve('.\\resources'));
    assert.strictEqual(context.userData, path.resolve('.\\userdata'));
    assert.strictEqual(context.documents, path.resolve('.\\documents'));
    assert.strictEqual(context.temp, path.resolve('.\\temp'));

    assert.deepStrictEqual(getRuntimeContext(), context);
    assert.strictEqual(getPathByName('userData'), context.userData);
    assert.strictEqual(getPathByName('documents'), context.documents);
    assert.strictEqual(getPathByName('temp'), context.temp);
    assert.strictEqual(getPathByName('unknown'), context.documents);

    const app = createAppShim();
    assert.strictEqual(app.getPath('userData'), context.userData);
    assert.strictEqual(app.getAppPath(), context.appPath);
    assert.doesNotThrow(() => app.quit());
  });
});
