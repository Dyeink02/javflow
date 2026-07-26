const assert = require('assert');
const fs = require('fs');
const os = require('os');
const path = require('path');

const QueueManager = require('../dist/core/queueManager').default;

describe('QueueManager shutdown handling', () => {
  it('resolves an in-flight index page request when shutdown is requested', async () => {
    const outputDir = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-queue-stop-'));
    const manager = new QueueManager({
      output: outputDir,
      parallel: 1,
      useCloudflareBypass: false,
      base: 'https://www.javbus.com/star/test',
      BASE_URL: 'https://www.javbus.com/star/test',
      timeout: 30000,
      delay: 0,
      headers: {
        Cookie: ''
      }
    });

    let releaseRequest = null;
    manager.requestHandler = {
      getPage: async () => {
        await new Promise((resolve) => {
          releaseRequest = resolve;
        });
        return { body: '<html></html>' };
      },
      close: async () => {
        if (releaseRequest) {
          releaseRequest();
        }
      }
    };

    try {
      const pendingFetch = manager.fetchIndexPageLinks('https://www.javbus.com/star/test');
      await new Promise((resolve) => setTimeout(resolve, 20));
      await manager.shutdown();

      const result = await Promise.race([
        pendingFetch,
        new Promise((_, reject) => setTimeout(() => reject(new Error('index fetch did not resolve after shutdown')), 500))
      ]);

      assert.deepStrictEqual(result, []);
    } finally {
      await manager.shutdown().catch(() => {});
      fs.rmSync(outputDir, { recursive: true, force: true });
    }
  });
});
