const assert = require('assert');
const fs = require('fs');

const { createCrawlRuntimeFactory } = require('../desktop/sidecar/services/crawlRuntimeFactory.js');

describe('sidecar crawlRuntimeFactory compatibility runtime', () => {
  function createEventBusStub() {
    return {
      emitLifecycle() {},
      emitLog() {},
      emitState() {},
      emitLogContext() {}
    };
  }

  it('reuses one runtime per process and keeps the legacy runtime lazy', () => {
    const factory = createCrawlRuntimeFactory({
      fs,
      eventBus: createEventBusStub()
    });

    const runtimeA = factory.ensureRuntime();
    const runtimeB = factory.ensureRuntime();

    assert.strictEqual(runtimeA, runtimeB);
    assert.strictEqual(typeof runtimeA.taskLogService.flush, 'function');
    assert.strictEqual(typeof runtimeA.goRunnerHost.start, 'function');
    assert.strictEqual(typeof runtimeA.ensureLegacyRuntime, 'function');
    assert.strictEqual(typeof runtimeA.getLegacyRuntime, 'function');
    assert.strictEqual(runtimeA.getLegacyRuntime(), null);

    const legacyA = runtimeA.ensureLegacyRuntime();
    const legacyB = runtimeA.ensureLegacyRuntime();

    assert.strictEqual(legacyA, legacyB);
    assert.strictEqual(runtimeA.getLegacyRuntime(), legacyA);
    assert.ok(legacyA.state);
    assert.ok(legacyA.runnerService);
    assert.strictEqual(typeof legacyA.settingsStore.loadSettings, 'function');
    assert.strictEqual(typeof legacyA.settingsStore.saveSettings, 'function');
  });
});
