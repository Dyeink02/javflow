const assert = require('assert');
const fs = require('fs');
const os = require('os');
const path = require('path');

const { createSettingsStore } = require('../desktop/mainServices/settingsStore.js');

describe('settingsStore ranking-history scaffolds', () => {
  function createTempAppRoot() {
    return fs.mkdtempSync(path.join(os.tmpdir(), 'jav-settings-store-'));
  }

  it('writes utf8 ranking-history guide and example templates in the current single-file format', () => {
    const root = createTempAppRoot();
    const userData = path.join(root, 'userData');
    const tempDir = path.join(root, 'temp');
    const documents = path.join(root, 'documents');
    fs.mkdirSync(userData, { recursive: true });
    fs.mkdirSync(tempDir, { recursive: true });
    fs.mkdirSync(documents, { recursive: true });

    const store = createSettingsStore({
      app: {
        getPath(name) {
          if (name === 'userData') return userData;
          if (name === 'temp') return tempDir;
          if (name === 'documents') return documents;
          throw new Error(`unexpected path request: ${name}`);
        }
      },
      fs,
      path,
      appInfo: {
        defaultBaseUrl: 'https://www.javbus.com',
        outputFolderName: 'JAV输出'
      },
      magnetFilename: 'magnet-links.txt'
    });

    const artifacts = store.ensureRankingHistoryArtifacts();

    const guideText = fs.readFileSync(artifacts.guidePath, 'utf8');
    const monthlyTemplate = fs.readFileSync(artifacts.monthlyTemplatePath, 'utf8');
    const annualTemplate = fs.readFileSync(artifacts.annualTemplatePath, 'utf8');

    assert.ok(guideText.includes('本地历史榜单目录说明'));
    assert.ok(guideText.includes('软件会自动读取'));

    const monthlyPayload = JSON.parse(monthlyTemplate);
    assert.strictEqual(monthlyPayload.mode, 'monthly');
    assert.strictEqual(monthlyPayload.periodMonth, 1);
    assert.strictEqual(monthlyPayload.items.length, 2);
    assert.ok(!Array.isArray(monthlyPayload.rankings));

    const annualPayload = JSON.parse(annualTemplate);
    assert.strictEqual(annualPayload.mode, 'annual');
    assert.strictEqual(annualPayload.periodYear, 2025);
    assert.strictEqual(annualPayload.items.length, 2);
    assert.ok(!Array.isArray(annualPayload.rankings));
  });
});
