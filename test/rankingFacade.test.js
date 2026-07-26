const assert = require('assert');
const fs = require('fs');
const os = require('os');
const path = require('path');

const { initializeRuntimeContext } = require('../desktop/sidecar/runtimePaths.js');
const { createRankingFacade } = require('../desktop/sidecar/services/rankingFacade.js');

describe('sidecar rankingFacade compatibility helper', () => {
  it('injects runtime-local cache/history defaults for ranking queries', async () => {
    const base = path.join(os.tmpdir(), 'jav-ranking-facade-runtime');
    const historyDir = path.join(base, 'userdata', 'ranking-history');
    fs.mkdirSync(historyDir, { recursive: true });
    fs.writeFileSync(
      path.join(historyDir, '2026-01-monthly.json'),
      JSON.stringify(
        {
          mode: 'monthly',
          sourceName: 'local-history',
          periodYear: 2026,
          periodMonth: 1,
          items: [{ rank: 1, actressName: 'Actress A' }]
        },
        null,
        2
      ),
      'utf8'
    );

    const context = initializeRuntimeContext({
      repoRoot: base,
      appPath: base,
      resourcesPath: base,
      userData: path.join(base, 'userdata'),
      documents: path.join(base, 'documents'),
      temp: path.join(base, 'temp')
    });

    const facade = createRankingFacade();
    const result = await facade.getRankings({
      mode: 'monthly',
      source: 'local',
      year: 2026,
      month: 1
    });

    assert.strictEqual(result.mode, 'monthly');
    assert.strictEqual(result.periodYear, 2026);
    assert.strictEqual(result.periodMonth, 1);
    assert.strictEqual(result.items.length, 1);
    assert.strictEqual(result.resolvedSource, 'local');
    assert.strictEqual(result.sourceChannel, 'local');
    assert.ok(String(result.sourceName || '').includes('local-history'));
    assert.deepStrictEqual(result.availableYears, [2026]);
    assert.strictEqual(result.fallbackUsed, false);
    assert.ok(String(result.notice || '').includes('本地历史'));
  });
});
