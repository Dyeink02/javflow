const assert = require('assert');
const fs = require('fs');

const source = fs.readFileSync(
  require.resolve('../scripts/patch-exe-metadata.mjs'),
  'utf8'
);

describe('EXE metadata patch scope', () => {
  it('does not rewrite timestamped historical release artifacts', () => {
    assert.strictEqual(source.includes('fallbackExes'), false);
    assert.strictEqual(source.includes('javflow-\\d{8}-\\d{6}'), false);
    assert.strictEqual(source.includes("path.join(releaseDir, 'javflow.exe')"), true);
  });
});
