const assert = require('assert');

const {
  parseOfficialMonthlyRankingHtml
} = require('../desktop/common/actressRankingOfficialSource.js');
const { getCurrentJapanYearMonth } = require('../desktop/common/actressRankingShared.js');

describe('actressRanking official source parser', () => {
  it('parses monthly rows and resolves relative resource URLs', () => {
    const current = getCurrentJapanYearMonth();
    const html = `
      <html>
        <head>
          <title>官方榜单</title>
        </head>
        <body>
          <div class="area-rank">
            <table>
              <tr class="bd-b">
                <td>1</td>
                <td>
                  <a href="/star/alice">
                    <img src="/images/alice.jpg" />
                    Alice
                  </a>
                </td>
              </tr>
              <tr class="bd-b">
                <td>invalid</td>
                <td>
                  <a href="https://www.dmm.co.jp/star/bella">
                    <img src="https://cdn.example.com/bella.jpg" />
                    Bella
                  </a>
                </td>
              </tr>
            </table>
          </div>
        </body>
      </html>
    `;

    const result = parseOfficialMonthlyRankingHtml(html, { requestedChannel: 'dmm' });

    assert.strictEqual(result.mode, 'monthly');
    assert.strictEqual(result.sourceChannel, 'dmm');
    assert.strictEqual(result.sourceName, 'DMM 官方');
    assert.strictEqual(result.title, '官方榜单');
    assert.strictEqual(result.periodYear, current.year);
    assert.strictEqual(result.periodMonth, current.month);
    assert.strictEqual(result.periodLabel, `${current.year}年${String(current.month).padStart(2, '0')}月`);
    assert.strictEqual(result.items.length, 2);
    assert.deepStrictEqual(result.items[0], {
      rank: 1,
      actressName: 'Alice',
      profileUrl: 'https://www.dmm.co.jp/star/alice',
      imageUrl: 'https://www.dmm.co.jp/images/alice.jpg'
    });
    assert.deepStrictEqual(result.items[1], {
      rank: 2,
      actressName: 'Bella',
      profileUrl: 'https://www.dmm.co.jp/star/bella',
      imageUrl: 'https://cdn.example.com/bella.jpg'
    });
  });

  it('throws a parse-empty ranking error when no valid rows exist', () => {
    assert.throws(
      () => parseOfficialMonthlyRankingHtml('<html><body><div class="area-rank"></div></body></html>'),
      (error) => {
        assert.strictEqual(error.code, 'parse-empty');
        assert.ok(error.message.includes('未从官方月榜页面解析到有效内容'));
        return true;
      }
    );
  });
});
