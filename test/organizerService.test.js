const assert = require('assert');
const fs = require('fs');
const os = require('os');
const path = require('path');

const { createOrganizerService } = require('../desktop/mainServices/organizerService');

describe('organizerService video extension and root safety', () => {
  function makeTempRoot(prefix) {
    return fs.mkdtempSync(path.join(os.tmpdir(), prefix));
  }

  afterEach(function cleanupTempRoot() {
    if (!this.currentTest) {
      return;
    }

    const tempRoots = Array.isArray(this.currentTest.tempRoots)
      ? this.currentTest.tempRoots
      : this.currentTest.tempRoot
        ? [this.currentTest.tempRoot]
        : [];
    tempRoots.forEach((tempRoot) => {
      fs.rmSync(tempRoot, { recursive: true, force: true });
    });
  });

  it('honors the missing-magnet retry switch in the archived report path', async function testRetryMissingMagnetsSwitch() {
    const disabledRoot = makeTempRoot('jav-organizer-missing-disabled-');
    const enabledRoot = makeTempRoot('jav-organizer-missing-enabled-');
    this.test.tempRoots = [disabledRoot, enabledRoot];

    async function runCase(rootPath, retryMissingMagnets) {
      fs.writeFileSync(path.join(rootPath, 'ABF-003.mp4'), Buffer.alloc(2 * 1024 * 1024));
      const service = createOrganizerService({ fs, path });
      return service.runOrganizer({
        rootPath,
        minSizeMB: 1,
        suffix: '-A',
        adFileAction: 'delete-directly',
        dryRun: false,
        includeSubdirectories: true,
        strictExpectedCodes: true,
        expectedCodes: ['ABF-003', 'ABF-004'],
        expectedCodeEntries: [
          { code: 'ABF-004', magnets: [{ link: 'magnet:?xt=urn:btih:ABF004' }] }
        ],
        videoExtensions: 'mp4',
        adDetectionEnabled: false,
        retryMissingMagnets
      });
    }

    const disabledResult = await runCase(disabledRoot, 'false');
    const disabledReport = fs.readFileSync(disabledResult.reportMap.missingMagnets, 'utf8');
    assert.strictEqual(disabledResult.summary.missingCodeCount, 1);
    assert.strictEqual(disabledResult.summary.missingMagnetCount, 0);
    assert.match(disabledReport, /补抓磁力总数：0/);
    assert.match(disabledReport, /未生成遗漏番号补抓磁力/);

    const enabledResult = await runCase(enabledRoot, true);
    const enabledReport = fs.readFileSync(enabledResult.reportMap.missingMagnets, 'utf8');
    assert.strictEqual(enabledResult.summary.missingCodeCount, 1);
    assert.strictEqual(enabledResult.summary.missingMagnetCount, 1);
    assert.match(enabledReport, /magnet:\?xt=urn:btih:ABF004/);
  });

  it('treats configured ISO files as valid large video candidates', async function testIsoExtension() {
    const rootPath = makeTempRoot('jav-organizer-iso-');
    this.test.tempRoot = rootPath;
    const sourcePath = path.join(rootPath, 'ABF-001.iso');
    fs.writeFileSync(sourcePath, Buffer.alloc(2 * 1024 * 1024));

    const service = createOrganizerService({ fs, path });
    const result = await service.runOrganizer({
      rootPath,
      minSizeMB: 1,
      suffix: '-A',
      adFileAction: 'move-to-delete',
      dryRun: false,
      includeSubdirectories: true,
      strictExpectedCodes: true,
      expectedCodes: ['ABF-001'],
      videoExtensions: 'mp4, mkv, iso',
      adDetectionEnabled: false
    });

    assert.strictEqual(result.summary.videoTotal, 1);
    assert.strictEqual(result.summary.movedToWaiting, 1);
    assert.ok(fs.existsSync(path.join(rootPath, '待整理', 'ABF-001.iso')));
    assert.ok(!fs.existsSync(sourcePath));
  });

  it('uses the expected-code fallback for noisy and zero-padded names', async function testExpectedCodeFallback() {
    const rootPath = makeTempRoot('jav-organizer-code-fallback-');
    this.test.tempRoot = rootPath;
    const filenames = [
      'mxgs01121.mp4',
      'hhd800.com@MXGS-1183.mp4',
      'kfa55.com@MXGS1358.mp4',
      'TL-1.mp4'
    ];

    filenames.forEach((filename) => {
      fs.writeFileSync(path.join(rootPath, filename), Buffer.alloc(2 * 1024 * 1024));
    });

    const service = createOrganizerService({ fs, path });
    const result = await service.runOrganizer({
      rootPath,
      minSizeMB: 1,
      suffix: '-A',
      adFileAction: 'move-to-delete',
      dryRun: true,
      includeSubdirectories: true,
      strictExpectedCodes: true,
      expectedCodes: ['MXGS-1121', 'MXGS-1183', 'MXGS-1358', 'TL-001'],
      videoExtensions: 'mp4',
      adDetectionEnabled: false
    });

    const renamedNames = result.preview.renameRecords.map((record) => record.newName).sort();
    assert.deepStrictEqual(renamedNames, ['MXGS-1121.mp4', 'MXGS-1183.mp4', 'MXGS-1358.mp4', 'TL-001.mp4']);
    assert.strictEqual(result.summary.movedToWaiting, 4);
    assert.strictEqual(result.preview.unmatchedRecords.length, 0);
  });

  it('does not delete root-level files that are below the minimum size', async function testRootFilePreserve() {
    const rootPath = makeTempRoot('jav-organizer-root-preserve-');
    this.test.tempRoot = rootPath;
    const rootVideoPath = path.join(rootPath, 'ROOT-001.mp4');
    fs.writeFileSync(rootVideoPath, Buffer.from('small-root-video'));

    const service = createOrganizerService({ fs, path });
    const result = await service.runOrganizer({
      rootPath,
      minSizeMB: 1000,
      suffix: '-A',
      adFileAction: 'delete-directly',
      dryRun: false,
      includeSubdirectories: true,
      strictExpectedCodes: false,
      expectedCodes: [],
      videoExtensions: 'mp4, mkv, iso',
      adDetectionEnabled: false
    });

    assert.strictEqual(result.summary.deletedDirectly, 0);
    assert.ok(fs.existsSync(rootVideoPath));
  });

  it('preserves a source folder when a qualified video cannot be moved', async function testMoveFailurePreserve() {
    const rootPath = makeTempRoot('jav-organizer-move-fail-');
    this.test.tempRoot = rootPath;
    const sourceDir = path.join(rootPath, 'ABF-002');
    const videoPath = path.join(sourceDir, 'ABF-002.mp4');
    const adPath = path.join(sourceDir, 'ad.txt');

    fs.mkdirSync(sourceDir, { recursive: true });
    fs.writeFileSync(videoPath, Buffer.alloc(2 * 1024 * 1024));
    fs.writeFileSync(adPath, Buffer.from('ad'));

    const failingFs = Object.create(fs);
    failingFs.promises = Object.create(fs.promises);
    failingFs.promises.rename = async (src, dest) => {
      if (path.resolve(src) === path.resolve(videoPath)) {
        throw new Error('simulated move failure');
      }
      return fs.promises.rename(src, dest);
    };
    failingFs.createReadStream = (src, options) => {
      if (path.resolve(src) === path.resolve(videoPath)) {
        throw new Error('simulated copy failure');
      }
      return fs.createReadStream(src, options);
    };
    failingFs.createWriteStream = (...args) => fs.createWriteStream(...args);

    const service = createOrganizerService({ fs: failingFs, path });
    const result = await service.runOrganizer({
      rootPath,
      minSizeMB: 1,
      suffix: '-A',
      adFileAction: 'delete-directly',
      dryRun: false,
      includeSubdirectories: true,
      strictExpectedCodes: true,
      expectedCodes: ['ABF-002'],
      videoExtensions: 'mp4, mkv, iso',
      adDetectionEnabled: false
    });

    assert.strictEqual(result.summary.failedOperations, 1);
    assert.ok(fs.existsSync(sourceDir));
    assert.ok(fs.existsSync(videoPath));
    assert.ok(!fs.existsSync(adPath));
    assert.ok(!fs.existsSync(path.join(rootPath, '待整理', 'ABF-002.mp4')));
  });
});
