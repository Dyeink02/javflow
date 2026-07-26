const assert = require('assert');
const fs = require('fs');
const os = require('os');
const path = require('path');

const { resolveRunOutputDirectory } = require('../dist/core/outputRuntimeUtils');

describe('outputRuntimeUtils', () => {
  it('keeps the base directory when resuming an existing task', () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-output-resume-'));
    fs.writeFileSync(path.join(tempDir, 'filmData.json'), '[]', 'utf8');

    const result = resolveRunOutputDirectory({
      outputDir: tempDir,
      resumeExisting: true,
      now: new Date('2026-04-12T16:00:00Z')
    });

    assert.strictEqual(result.outputDir, tempDir);
    assert.strictEqual(result.createdRunDir, false);
    assert.strictEqual(result.reason, 'resume-existing');
  });

  it('creates a fresh run directory when the base output already has historical artifacts', () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-output-isolate-'));
    fs.writeFileSync(path.join(tempDir, 'filmData.json'), '[]', 'utf8');
    fs.writeFileSync(path.join(tempDir, 'magnet-links.txt'), 'magnet:?xt=urn:btih:test', 'utf8');

    const result = resolveRunOutputDirectory({
      outputDir: tempDir,
      resumeExisting: false,
      now: new Date(2026, 3, 12, 16, 0, 0)
    });

    assert.strictEqual(result.baseOutputDir, tempDir);
    assert.strictEqual(result.createdRunDir, true);
    assert.strictEqual(result.reason, 'isolated-existing-output');
    assert.ok(result.outputDir.startsWith(tempDir));
    assert.strictEqual(path.basename(result.outputDir), 'run-20260412-160000');
  });
});
