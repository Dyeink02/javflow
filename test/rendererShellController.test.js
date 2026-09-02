const assert = require('assert');
const fs = require('fs');
const vm = require('vm');

const controllerSource = fs.readFileSync(
  require.resolve('../desktop/renderer/rendererShellController.js'),
  'utf8'
);

function createNode() {
  const classes = new Set();
  return {
    classList: {
      toggle(name, enabled) {
        if (enabled) {
          classes.add(name);
        } else {
          classes.delete(name);
        }
      },
      contains: (name) => classes.has(name)
    },
    addEventListener: () => {}
  };
}

function createHarness() {
  const sandbox = {
    desktopRendererShellView: {
      normalizeCrawlerOpsCopy: () => {},
      applySubscriptionHeroCopy: () => {},
      promoteCrawlOpsPanel: () => {},
      toggleInfoCard: () => {}
    }
  };
  vm.runInNewContext(controllerSource, sandbox, {
    filename: 'rendererShellController.js'
  });

  const updateControls = createNode();
  const elements = {
    crawlerUpdateControls: updateControls,
    crawlerTopbarVersion: createNode(),
    navActressAtlasButton: createNode(),
    navCrawlerButton: createNode(),
    navOrganizerButton: createNode(),
    navSubscriptionButton: createNode(),
    navLibraryMetadataButton: createNode(),
    actressAtlasWorkspace: createNode(),
    crawlerWorkspace: createNode(),
    organizerWorkspace: createNode(),
    subscriptionWorkspace: createNode(),
    libraryMetadataWorkspace: createNode()
  };
  const controller = sandbox.desktopRendererShellController.createRendererShellController({ elements });

  return { controller, elements, updateControls };
}

describe('renderer shell controller', () => {
  it('keeps global update controls visible while switching workspaces', () => {
    const harness = createHarness();

    ['crawler', 'actressatlas', 'organizer', 'librarymetadata', 'subscription'].forEach((workspace) => {
      harness.controller.setWorkspace(workspace);
      assert.strictEqual(
        harness.updateControls.classList.contains('hidden'),
        false,
        `update controls should remain visible in ${workspace}`
      );
    });
  });
});
