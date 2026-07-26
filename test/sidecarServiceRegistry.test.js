const assert = require('assert');

const { createServiceRegistry } = require('../desktop/sidecar/serviceRegistry.js');

describe('sidecar serviceRegistry archived-domain gate', () => {
  const originalArchivedEnv = process.env.JAV_ENABLE_ARCHIVED_SIDECAR_DOMAINS;

  afterEach(() => {
    if (typeof originalArchivedEnv === 'undefined') {
      delete process.env.JAV_ENABLE_ARCHIVED_SIDECAR_DOMAINS;
    } else {
      process.env.JAV_ENABLE_ARCHIVED_SIDECAR_DOMAINS = originalArchivedEnv;
    }
  });

  function createEventBusStub() {
    return {
      emitLifecycle() {},
      emitLog() {},
      emitState() {},
      emitLogContext() {}
    };
  }

  it('keeps archived domains disabled by default while keeping proxy validation available', () => {
    delete process.env.JAV_ENABLE_ARCHIVED_SIDECAR_DOMAINS;
    const registry = createServiceRegistry({
      fs: require('fs'),
      eventBus: createEventBusStub()
    });

    assert.strictEqual(registry.archivedDomainsEnabled, false);
    assert.ok(registry.ensureProxyValidationService());

    assert.throws(() => registry.ensureAdLearningFacade(), /archived and disabled by default/);
    assert.throws(() => registry.ensureOrganizerFacade(), /archived and disabled by default/);
    assert.throws(() => registry.ensureRankingFacade(), /archived and disabled by default/);
  });

  it('enables archived domains only when the explicit env gate is set', () => {
    process.env.JAV_ENABLE_ARCHIVED_SIDECAR_DOMAINS = 'yes';
    const registry = createServiceRegistry({
      fs: require('fs'),
      eventBus: createEventBusStub()
    });

    assert.strictEqual(registry.archivedDomainsEnabled, true);
    assert.doesNotThrow(() => registry.ensureRankingFacade());
  });
});
