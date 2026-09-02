const assert = require('assert');

const { VERSION_HISTORY } = require('../desktop/common/text/versionHistory.js');

describe('version history', () => {
  it('lists the 0.4.40 release immediately before 0.4.41', () => {
    const versions = VERSION_HISTORY.map((entry) => entry.version);
    const version40Index = versions.indexOf('0.4.40');
    const version41Index = versions.indexOf('0.4.41');

    assert.ok(version40Index >= 0, 'expected the 0.4.40 release note');
    assert.strictEqual(version41Index, version40Index + 1);
    assert.strictEqual(versions.includes('0.4.4'), false);
  });
});
