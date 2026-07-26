const assert = require('assert');

const {
  getFastMagnetQueueConcurrency,
  getRecoveryMagnetQueueConcurrency,
  processFastMagnetTask,
  processRecoveryMagnetTask
} = require('../dist/core/queueManagerMagnetQueueUtils');

function createTask() {
  return {
    sourceLink: 'https://example.com/ABP-001',
    metadata: {
      title: 'ABP-001',
      gid: '1',
      img: 'cover.jpg',
      uc: '0',
      category: [],
      actress: []
    },
    filmData: {
      title: 'ABP-001',
      category: [],
      actress: []
    }
  };
}

describe('queueManagerMagnetQueueUtils', () => {
  it('keeps concurrency within the configured bounds', () => {
    assert.strictEqual(getFastMagnetQueueConcurrency(1), 4);
    assert.strictEqual(getFastMagnetQueueConcurrency(10), 12);
    assert.strictEqual(getRecoveryMagnetQueueConcurrency(true, 5), 2);
    assert.strictEqual(getRecoveryMagnetQueueConcurrency(false, 5), 1);
  });

  it('emits processed data on fast magnet success', async () => {
    const task = createTask();
    const processed = [];
    const routed = [];

    await processFastMagnetTask({
      task,
      requestHandler: {
        async fetchMagnet() {
          return {
            magnet: 'magnet:?xt=urn:btih:abc',
            magnetLinks: [{ link: 'magnet:?xt=urn:btih:abc', size: '1GB' }]
          };
        },
        shouldRouteMagnetTaskToRecoveryQueue() {
          return false;
        }
      },
      useCloudflareBypass: true,
      routeToRecoveryQueue(nextTask) {
        routed.push(nextTask);
      },
      emitProcessed(payload) {
        processed.push(payload);
      }
    });

    assert.strictEqual(routed.length, 0);
    assert.strictEqual(processed.length, 1);
    assert.strictEqual(processed[0].filmData.magnetLinks.length, 1);
  });

  it('routes fast magnet tasks to recovery when no magnet is returned', async () => {
    const task = createTask();
    const processed = [];
    const routed = [];

    await processFastMagnetTask({
      task,
      requestHandler: {
        async fetchMagnet() {
          return { magnet: '' };
        },
        shouldRouteMagnetTaskToRecoveryQueue() {
          return false;
        }
      },
      useCloudflareBypass: true,
      routeToRecoveryQueue(nextTask) {
        routed.push(nextTask);
      },
      emitProcessed(payload) {
        processed.push(payload);
      }
    });

    assert.strictEqual(processed.length, 0);
    assert.strictEqual(routed.length, 1);
  });

  it('still emits processed data when recovery fetch fails', async () => {
    const task = createTask();
    const processed = [];

    await processRecoveryMagnetTask({
      task,
      requestHandler: {
        async fetchMagnet() {
          throw new Error('network down');
        },
        shouldRouteMagnetTaskToRecoveryQueue() {
          return false;
        }
      },
      emitProcessed(payload) {
        processed.push(payload);
      }
    });

    assert.strictEqual(processed.length, 1);
  });
});
