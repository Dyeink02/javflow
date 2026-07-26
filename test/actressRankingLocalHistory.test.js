const assert = require('assert');
const fs = require('fs');
const os = require('os');
const path = require('path');

const { getActressRankings } = require('../desktop/common/actressRankingService.js');

describe('actressRanking local history import', () => {
  it('loads local history monthly and annual records from a writable directory', async () => {
    const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-ranking-history-'));
    const historyDir = path.join(tempRoot, 'ranking-history');
    const cacheFilePath = path.join(tempRoot, 'actress-ranking-cache.json');
    fs.mkdirSync(historyDir, { recursive: true });

    fs.writeFileSync(
      path.join(historyDir, '2026-01-monthly.json'),
      JSON.stringify(
        {
          mode: 'monthly',
          sourceName: '本地历史导入',
          periodYear: 2026,
          periodMonth: 1,
          items: [
            { rank: 1, actressName: '示例女优A' },
            { rank: 2, actressName: '示例女优B' }
          ]
        },
        null,
        2
      ),
      'utf8'
    );

    fs.writeFileSync(
      path.join(historyDir, '2025-annual.json'),
      JSON.stringify(
        {
          mode: 'annual',
          sourceName: '本地历史导入',
          periodYear: 2025,
          items: [
            { rank: 1, actressName: '示例女优A' },
            { rank: 2, actressName: '示例女优B' }
          ]
        },
        null,
        2
      ),
      'utf8'
    );

    const monthly = await getActressRankings({
      mode: 'monthly',
      year: 2026,
      month: 1,
      source: 'local',
      cacheFilePath,
      historyDirectories: [historyDir]
    });

    const annual = await getActressRankings({
      mode: 'annual',
      year: 2025,
      source: 'local',
      cacheFilePath,
      historyDirectories: [historyDir]
    });

    assert.strictEqual(monthly.periodYear, 2026);
    assert.strictEqual(monthly.periodMonth, 1);
    assert.strictEqual(monthly.items.length, 2);
    assert.strictEqual(monthly.resolvedSource, 'local');
    assert.ok(monthly.notice.includes('本地历史'));

    assert.strictEqual(annual.periodYear, 2025);
    assert.strictEqual(annual.items.length, 2);
    assert.strictEqual(annual.resolvedSource, 'local');
    assert.ok(annual.availableYears.includes(2025));
  });
});
