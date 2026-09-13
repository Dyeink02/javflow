const assert = require('assert');
const fs = require('fs');
const os = require('os');
const path = require('path');

const FileHandler = require('../dist/core/fileHandler').default;

describe('FileHandler', () => {
  const originalBackupDir = process.env.JAV_SCRAPY_FILE_BACKUP_DIR;
  let testBackupRoot = '';

  before(() => {
    testBackupRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-file-handler-backups-'));
    process.env.JAV_SCRAPY_FILE_BACKUP_DIR = testBackupRoot;
  });

  after(() => {
    if (originalBackupDir) {
      process.env.JAV_SCRAPY_FILE_BACKUP_DIR = originalBackupDir;
    } else {
      delete process.env.JAV_SCRAPY_FILE_BACKUP_DIR;
    }

    if (testBackupRoot) {
      fs.rmSync(testBackupRoot, { recursive: true, force: true });
    }
  });

  it('merges duplicate film data without polluting output directory', async () => {
    const outputDir = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-file-handler-'));
    const handler = new FileHandler(outputDir);

    await handler.writeFilmDataToFile({
      title: 'ABF-001 第一条',
      sourceLink: 'https://www.javbus.com/ABF-001',
      coverImage: '/images/abf-001.jpg',
      category: ['单体作品'],
      actress: ['测试演员'],
      magnetLinks: [{ link: 'magnet:?xt=urn:btih:first', size: '1GB' }]
    });

    await handler.writeFilmDataToFile({
      title: 'ABF-001 第一条完整版',
      sourceLink: 'https://www.javbus.com/abf-001/',
      coverImage: 'https://www.javbus.com/images/abf-001-full.jpg',
      category: ['单体作品', '高清'],
      actress: ['测试演员', '第二演员'],
      magnetLinks: [{ link: 'magnet:?xt=urn:btih:second', size: '2GB' }]
    });
    await handler.flush(true);

    const records = JSON.parse(fs.readFileSync(path.join(outputDir, 'filmData.json'), 'utf8'));
    const magnets = fs.readFileSync(path.join(outputDir, 'magnet-links.txt'), 'utf8').trim().split('\n');

    assert.strictEqual(records.length, 1);
    assert.strictEqual(records[0].coverImage, 'https://www.javbus.com/images/abf-001-full.jpg');
    assert.strictEqual(records[0].magnetLinks.length, 2);
    assert.strictEqual(records[0].category.length, 2);
    assert.strictEqual(records[0].actress.length, 2);
    assert.strictEqual(magnets.length, 2);
    assert.strictEqual(fs.existsSync(path.join(outputDir, 'backups')), false);

    fs.rmSync(outputDir, { recursive: true, force: true });
  });

  it('persists magnet display names while merging duplicate records', async () => {
    const outputDir = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-file-handler-display-name-'));
    const handler = new FileHandler(outputDir);
    const link = 'magnet:?xt=urn:btih:display-name&dn=ABA-250-c';

    await handler.writeFilmDataToFile({
      title: 'ABA-250 第一条',
      sourceLink: 'https://www.javbus.com/ABA-250',
      category: [],
      actress: [],
      magnetLinks: [{ link, size: '2GB', displayName: 'ABA-250-c' }]
    });
    await handler.writeFilmDataToFile({
      title: 'ABA-250 第二条',
      sourceLink: 'https://www.javbus.com/aba-250/',
      category: [],
      actress: [],
      magnetLinks: [{ link, size: '2GB' }]
    });
    await handler.flush(true);

    const records = JSON.parse(fs.readFileSync(path.join(outputDir, 'filmData.json'), 'utf8'));
    assert.strictEqual(records[0].magnetLinks[0].displayName, 'ABA-250-c');

    fs.rmSync(outputDir, { recursive: true, force: true });
  });

  it('deduplicates records by film id from title or sourceLink', async () => {
    const outputDir = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-file-handler-film-id-'));
    const handler = new FileHandler(outputDir);

    await handler.writeFilmDataToFile({
      title: 'HMN-776 第一版标题',
      sourceLink: 'https://www.javbus.com/HMN-776',
      category: ['剧情'],
      actress: ['演员A'],
      magnetLinks: [{ link: 'magnet:?xt=urn:btih:first-hmn776', size: '1GB' }]
    });

    await handler.writeFilmDataToFile({
      title: 'HMN-776 第二版标题',
      sourceLink: 'https://www.javbus.com/hmn-776/',
      category: ['高清'],
      actress: ['演员B'],
      magnetLinks: [{ link: 'magnet:?xt=urn:btih:second-hmn776', size: '2GB' }]
    });
    await handler.flush(true);

    const records = JSON.parse(fs.readFileSync(path.join(outputDir, 'filmData.json'), 'utf8'));
    const magnets = fs.readFileSync(path.join(outputDir, 'magnet-links.txt'), 'utf8').trim().split('\n');

    assert.strictEqual(records.length, 1);
    assert.strictEqual(records[0].sourceLink, 'https://www.javbus.com/HMN-776');
    assert.strictEqual(records[0].magnetLinks.length, 2);
    assert.strictEqual(magnets.length, 2);

    fs.rmSync(outputDir, { recursive: true, force: true });
  });

  it('buffers writes in memory before flush', async () => {
    const outputDir = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-file-handler-buffer-'));
    const handler = new FileHandler(outputDir);

    await handler.writeFilmDataToFile({
      title: 'ABP-001 Buffer Test',
      sourceLink: 'https://www.javbus.com/ABP-001',
      category: ['剧情'],
      actress: ['演员C'],
      magnetLinks: [{ link: 'magnet:?xt=urn:btih:buffer-one', size: '1GB' }]
    });

    assert.strictEqual(fs.existsSync(path.join(outputDir, 'filmData.json')), false);

    await handler.flush(true);

    const records = JSON.parse(fs.readFileSync(path.join(outputDir, 'filmData.json'), 'utf8'));
    assert.strictEqual(records.length, 1);

    fs.rmSync(outputDir, { recursive: true, force: true });
  });

  it('keeps filtered films in filmData.json but excludes their magnets from magnet-links.txt', async () => {
    const outputDir = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-file-handler-actress-filter-'));
    const handler = new FileHandler(outputDir, {
      actressCountFilterThreshold: 5
    });

    await handler.writeFilmDataToFile({
      title: 'DAZD-277 Sample',
      sourceLink: 'https://www.javbus.com/DAZD-277',
      category: ['合集'],
      actress: ['A', 'B', 'C', 'D', 'E', 'F'],
      actressCount: 6,
      magnetLinks: [{ link: 'magnet:?xt=urn:btih:dazd277', size: '1GB' }]
    });

    await handler.writeFilmDataToFile({
      title: 'ABP-001 Sample',
      sourceLink: 'https://www.javbus.com/ABP-001',
      category: ['单体作品'],
      actress: ['A'],
      actressCount: 1,
      magnetLinks: [{ link: 'magnet:?xt=urn:btih:abp001', size: '2GB' }]
    });

    await handler.flush(true);

    const records = JSON.parse(fs.readFileSync(path.join(outputDir, 'filmData.json'), 'utf8'));
    const magnets = fs.readFileSync(path.join(outputDir, 'magnet-links.txt'), 'utf8').trim().split('\n');
    const filteredRecord = records.find((item) => item.sourceLink === 'https://www.javbus.com/DAZD-277');

    assert.strictEqual(records.length, 2);
    assert.ok(filteredRecord);
    assert.strictEqual(filteredRecord.filteredByActressCount, true);
    assert.strictEqual(filteredRecord.actressCount, 6);
    assert.deepStrictEqual(magnets, ['magnet:?xt=urn:btih:abp001']);

    fs.rmSync(outputDir, { recursive: true, force: true });
  });

  it('excludes film-code filtered films from magnet-links.txt', async () => {
    const outputDir = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-file-handler-film-code-filter-'));
    const handler = new FileHandler(outputDir, {
      filmCodeFilterThreshold: 'VR'
    });

    await handler.writeFilmDataToFile({
      title: 'MDVR-393 Sample',
      sourceLink: 'https://www.javbus.com/MDVR-393',
      category: ['VR'],
      actress: ['A'],
      magnetLinks: [{ link: 'magnet:?xt=urn:btih:mdvr393', size: '1GB' }]
    });

    await handler.writeFilmDataToFile({
      title: 'ABP-001 Sample',
      sourceLink: 'https://www.javbus.com/ABP-001',
      category: ['单体作品'],
      actress: ['B'],
      magnetLinks: [{ link: 'magnet:?xt=urn:btih:abp001', size: '2GB' }]
    });

    await handler.flush(true);

    const records = JSON.parse(fs.readFileSync(path.join(outputDir, 'filmData.json'), 'utf8'));
    const magnets = fs.readFileSync(path.join(outputDir, 'magnet-links.txt'), 'utf8').trim().split('\n');
    const filteredRecord = records.find((item) => item.sourceLink === 'https://www.javbus.com/MDVR-393');

    assert.strictEqual(records.length, 2);
    assert.ok(filteredRecord);
    assert.strictEqual(filteredRecord.filteredByFilmCode, true);
    assert.deepStrictEqual(magnets, ['magnet:?xt=urn:btih:abp001']);

    fs.rmSync(outputDir, { recursive: true, force: true });
  });

  it('supports Chinese separators in the film-code filter', async () => {
    const outputDir = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-file-handler-film-code-filter-cn-'));
    // 用户实际输入习惯：顿号/中文逗号/分号/空格混用，都必须正确拆分。
    const handler = new FileHandler(outputDir, {
      filmCodeFilterThreshold: 'VR、OFJE，ABC; DEF D'
    });

    await handler.writeFilmDataToFile({
      title: 'SIVR-370 【VR】Sample',
      sourceLink: 'https://www.javbus.com/SIVR-370',
      category: ['VR'],
      actress: ['A'],
      magnetLinks: [{ link: 'magnet:?xt=urn:btih:sivr370', size: '3GB' }]
    });

    await handler.writeFilmDataToFile({
      title: 'ABP-002 Sample',
      sourceLink: 'https://www.javbus.com/ABP-002',
      category: ['单体作品'],
      actress: ['B'],
      magnetLinks: [{ link: 'magnet:?xt=urn:btih:abp002', size: '2GB' }]
    });

    await handler.flush(true);

    const records = JSON.parse(fs.readFileSync(path.join(outputDir, 'filmData.json'), 'utf8'));
    const magnets = fs.readFileSync(path.join(outputDir, 'magnet-links.txt'), 'utf8').trim().split('\n');
    const filteredRecord = records.find((item) => item.sourceLink === 'https://www.javbus.com/SIVR-370');

    // 被过滤的影片保留在 filmData.json 作审计，但磁力不会写入 magnet-links.txt。
    assert.strictEqual(records.length, 2);
    assert.ok(filteredRecord);
    assert.strictEqual(filteredRecord.filteredByFilmCode, true);
    assert.deepStrictEqual(magnets, ['magnet:?xt=urn:btih:abp002']);

    fs.rmSync(outputDir, { recursive: true, force: true });
  });
});
