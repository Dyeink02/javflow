const assert = require('assert');

describe('ConfigManager proxy precedence', () => {
  it('keeps the detected system proxy when the manual proxy input is invalid', async () => {
    const systemProxyModule = require('../dist/utils/systemProxy');
    const originalGetSystemProxy = systemProxyModule.getSystemProxy;

    systemProxyModule.getSystemProxy = async () => ({
      enabled: true,
      server: '127.0.0.1:7897'
    });

    delete require.cache[require.resolve('../dist/core/config')];
    const ConfigManager = require('../dist/core/config').default;

    try {
      const manager = new ConfigManager();
      await manager.updateFromOptions({
        proxy: '213'
      });

      const config = manager.getConfig();
      assert.strictEqual(config.proxy, 'http://127.0.0.1:7897');
    } finally {
      systemProxyModule.getSystemProxy = originalGetSystemProxy;
      delete require.cache[require.resolve('../dist/core/config')];
    }
  });

  it('supports enabling second validation from runtime options', async () => {
    const systemProxyModule = require('../dist/utils/systemProxy');
    const originalGetSystemProxy = systemProxyModule.getSystemProxy;

    systemProxyModule.getSystemProxy = async () => ({
      enabled: false,
      server: ''
    });

    delete require.cache[require.resolve('../dist/core/config')];
    const ConfigManager = require('../dist/core/config').default;

    try {
      const manager = new ConfigManager();
      await manager.updateFromOptions({
        secondValidation: true
      });

      const config = manager.getConfig();
      assert.strictEqual(config.secondValidation, true);
    } finally {
      systemProxyModule.getSystemProxy = originalGetSystemProxy;
      delete require.cache[require.resolve('../dist/core/config')];
    }
  });

  it('supports actress-count filter threshold from runtime options', async () => {
    const systemProxyModule = require('../dist/utils/systemProxy');
    const originalGetSystemProxy = systemProxyModule.getSystemProxy;

    systemProxyModule.getSystemProxy = async () => ({
      enabled: false,
      server: ''
    });

    delete require.cache[require.resolve('../dist/core/config')];
    const ConfigManager = require('../dist/core/config').default;

    try {
      const manager = new ConfigManager();
      await manager.updateFromOptions({
        actressCountFilterThreshold: '5'
      });

      const config = manager.getConfig();
      assert.strictEqual(config.actressCountFilterThreshold, 5);
    } finally {
      systemProxyModule.getSystemProxy = originalGetSystemProxy;
      delete require.cache[require.resolve('../dist/core/config')];
    }
  });
});
