const assert = require('assert');
const fs = require('fs');
const vm = require('vm');

const controllerSource = fs.readFileSync(
  require.resolve('../desktop/renderer/subscriptionController.js'),
  'utf8'
);

function createControllerHarness() {
  let resolveSubscriptions;
  const subscriptionsReady = new Promise((resolve) => {
    resolveSubscriptions = resolve;
  });
  const sandbox = {
    console,
    setTimeout,
    clearTimeout,
    desktopRendererHelpers: {
      getErrorMessage: (error) => (error && error.message ? error.message : String(error || '')),
      toSafeInteger: (value, fallback) => {
        const parsed = Number.parseInt(String(value), 10);
        return Number.isFinite(parsed) ? parsed : fallback;
      },
      clearChildren: () => {},
      appendTimestampedLogLine: () => {}
    },
    desktopSubscriptionListView: {
      renderSubscriptionList: () => {}
    },
    desktopArtifactInputHelper: {
      createArtifactInputHelper: () => ({})
    }
  };
  vm.runInNewContext(controllerSource, sandbox, {
    filename: 'subscriptionController.js'
  });

  const controller = sandbox.desktopSubscriptionController.createSubscriptionController({
    elements: {},
    desktopApi: {
      getSettings: async () => ({}),
      listAvSubscriptions: () => subscriptionsReady
    },
    subscriptionCrawlSessionBridge: null
  });

  return { controller, resolveSubscriptions };
}

describe('subscription bootstrap', () => {
  it('returns the first initialization promise until subscriptions are loaded', async () => {
    const { controller, resolveSubscriptions } = createControllerHarness();
    const initialization = controller.bootstrap();
    let settled = false;
    initialization.then(() => {
      settled = true;
    });

    await Promise.resolve();
    assert.strictEqual(settled, false);

    resolveSubscriptions({ subscriptions: [] });
    await initialization;
    assert.strictEqual(settled, true);
    assert.strictEqual(controller.bootstrap(), initialization);
  });
});
