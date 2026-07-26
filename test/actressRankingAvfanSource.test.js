const assert = require('assert');

const { parseAvfanRankingHtml } = require('../desktop/common/actressRankingAvfanSource.js');

describe('actressRanking AVfan source sanitization', () => {
  it('prefers decoded actress names from profile URLs and rebuilds safe labels', () => {
    const html = `
      <html>
        <head>
          <title>2026.03 mocked ranking</title>
        </head>
        <body>
          <div class="ranking-year-link">
            <a href="?year=2025">2025</a>
            <a href="?year=2024">2024</a>
          </div>
          <ul class="rankBox">
            <li>
              <div class="rankNum">1</div>
              <a href="/actress/%E7%9F%B3%E5%B7%9D%E6%BE%AA.html">mojibake-name</a>
              <img src="https://example.com/mio.jpg" alt="wrong name" />
            </li>
          </ul>
        </body>
      </html>
    `;

    const result = parseAvfanRankingHtml(html, {
      mode: 'monthly',
      sourceUrl: 'https://av-fan.tokyo/ranking/fanza-dvd-actress-monthly.php'
    });

    assert.strictEqual(result.title, '2026.03 AVfan FANZA DVD Actress Monthly Ranking');
    assert.strictEqual(result.periodLabel, '\u0032\u0030\u0032\u0036\u5e74\u0030\u0033\u6708');
    assert.strictEqual(result.items[0].actressName, '\u77f3\u5ddd\u6faa');
    assert.strictEqual(result.items[0].rank, 1);
    assert.strictEqual(result.sourceChannel, 'avfan');
  });
});
