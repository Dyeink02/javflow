const assert = require('assert');
const fs = require('fs');
const vm = require('vm');

const uiTextSource = fs.readFileSync(
  require.resolve('../desktop/renderer/uiText.js'),
  'utf8'
);

describe('ui text fallback templates', () => {
  it('keeps Cloudflare enabled for the balanced fallback template', () => {
    const sandbox = { console };
    vm.runInNewContext(uiTextSource, sandbox, { filename: 'uiText.js' });

    assert.strictEqual(sandbox.desktopUiText.isUsingFallbackBundle, true);
    assert.strictEqual(sandbox.desktopUiText.TASK_TEMPLATES.balanced.cloudflare, true);
  });
});
