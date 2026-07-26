const assert = require('assert');
const fs = require('fs');

const { createSidecarSettingsStore } = require('../desktop/sidecar/services/sharedFacadeRuntime.js');

describe('sidecar sharedFacadeRuntime compatibility pair', () => {
  it('builds the shared app-shim and settings-store pair for archived facades', () => {
    const runtime = createSidecarSettingsStore({ fs });

    assert.ok(runtime.app);
    assert.ok(runtime.settingsStore);
    assert.strictEqual(typeof runtime.app.getPath, 'function');
    assert.strictEqual(typeof runtime.app.getAppPath, 'function');
    assert.strictEqual(typeof runtime.settingsStore.loadSettings, 'function');
    assert.strictEqual(typeof runtime.settingsStore.saveSettings, 'function');
    assert.ok(runtime.settingsStore.getSettingsPath().endsWith('desktop-settings.json'));
    assert.ok(runtime.settingsStore.getRankingCachePath().endsWith('actress-ranking-cache.json'));
  });
});
