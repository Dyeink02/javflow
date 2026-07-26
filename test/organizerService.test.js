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
    if (this.currentTest && this.currentTest.tempRoot) {
      fs.rmSync(this.currentTest.tempRoot, { recursive: true, force: true });
    }
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
