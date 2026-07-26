const assert = require('assert');

const {
  inspectActressTarget,
  resolveActressCrawlTarget
} = require('../desktop/common/javBusActressLookupService.js');

const DEFAULT_ORIGINS = [
  'https://www.javbus.com',
  'https://www.busjav.cyou',
  'https://www.fanbus.bond',
  'https://www.cdnbus.bond'
];

function createFetchResponse(body, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    async text() {
      return body;
    }
  };
}

function createSearchHtml(candidates) {
  return `
    <html>
      <body>
        ${candidates
          .map(
            (candidate) => `
              <a class="avatar-box" href="${candidate.href}">
                <img title="${candidate.name}" />
                <span class="mleft">${candidate.name}</span>
              </a>
            `
          )
          .join('\n')}
      </body>
    </html>
  `;
}

function createStarHtml(options = {}) {
  const actressName = options.actressName || 'Alice';
  const magnetCount = Number.isFinite(options.magnetCount) ? options.magnetCount : 12;
  const allCount = Number.isFinite(options.allCount) ? options.allCount : 14;
  const movieCount = Number.isFinite(options.movieCount) ? options.movieCount : 2;
  return `
    <html>
      <head>
        <title>${actressName} - JAVBus</title>
      </head>
      <body>
        <div class="star-box">
          <span class="star-name">${actressName}</span>
        </div>
        <div class="count-summary">已有磁力 ${magnetCount} 部 全部影片 ${allCount} 部</div>
        ${Array.from({ length: movieCount }, (_, index) => `<a class="movie-box" href="/movie/${index + 1}">movie</a>`).join('\n')}
      </body>
    </html>
  `;
}

describe('javBusActressLookupService public lookup contract', () => {
  const originalFetch = global.fetch;

  afterEach(() => {
    global.fetch = originalFetch;
  });

  function installFetchMap(routeMap) {
    global.fetch = async (url) => {
      const key = String(url);
      if (!Object.prototype.hasOwnProperty.call(routeMap, key)) {
        throw new Error(`unexpected url: ${key}`);
      }
      const value = routeMap[key];
      if (value instanceof Error) {
        throw value;
      }
      if (typeof value === 'object' && value !== null && Object.prototype.hasOwnProperty.call(value, 'body')) {
        return createFetchResponse(value.body, value.status || 200);
      }
      return createFetchResponse(String(value));
    };
  }

  it('resolves actress targets from search results and star pages', async () => {
    installFetchMap({
      'https://www.javbus.com/searchstar/Alice': createSearchHtml([
        { name: 'Alice', href: '/star/alice' }
      ]),
      'https://www.javbus.com/star/alice': createStarHtml({
        actressName: 'Alice',
        magnetCount: 12,
        allCount: 14,
        movieCount: 2
      })
    });

    const result = await resolveActressCrawlTarget({ actressName: 'Alice' });

    assert.strictEqual(result.actressName, 'Alice');
    assert.strictEqual(result.resolvedActressName, 'Alice');
    assert.strictEqual(result.resolvedBase, 'https://www.javbus.com/star/alice');
    assert.strictEqual(result.lookupBaseOrigin, 'https://www.javbus.com');
    assert.strictEqual(result.matchMode, 'exact');
    assert.strictEqual(result.candidateCount, 1);
    assert.strictEqual(result.magnetCount, 12);
    assert.strictEqual(result.allCount, 14);
    assert.strictEqual(result.fillCount, 12);
    assert.strictEqual(result.preferredCount, 12);
    assert.strictEqual(result.itemsPerPage, 2);
    assert.strictEqual(result.totalPages, 6);
    assert.deepStrictEqual(result.candidatePreview, [
      {
        actressName: 'Alice',
        href: 'https://www.javbus.com/star/alice'
      }
    ]);
  });

  it('returns direct-url mode when a direct star page can be inspected', async () => {
    installFetchMap({
      'https://www.javbus.com/star/alice': createStarHtml({
        actressName: 'Alice',
        magnetCount: 9,
        allCount: 10,
        movieCount: 3
      })
    });

    const result = await inspectActressTarget({
      targetUrl: 'https://www.javbus.com/star/alice'
    });

    assert.strictEqual(result.matchMode, 'direct-url');
    assert.strictEqual(result.lookupBaseOrigin, 'https://www.javbus.com');
    assert.strictEqual(result.fillCount, 9);
    assert.strictEqual(result.itemsPerPage, 3);
    assert.strictEqual(result.totalPages, 3);
  });

  it('falls back to actress-name lookup when direct inspection fails but actressName is present', async () => {
    installFetchMap({
      notaurl: '<html><body>invalid star page</body></html>',
      'https://www.javbus.com/searchstar/Alice': createSearchHtml([
        { name: 'Alice', href: '/star/alice' }
      ]),
      'https://www.javbus.com/star/alice': createStarHtml({
        actressName: 'Alice',
        magnetCount: 8,
        allCount: 11,
        movieCount: 4
      })
    });

    const result = await inspectActressTarget({
      targetUrl: 'notaurl',
      actressName: 'Alice'
    });

    assert.strictEqual(result.resolvedBase, 'https://www.javbus.com/star/alice');
    assert.strictEqual(result.matchMode, 'exact');
    assert.strictEqual(result.fillCount, 8);
    assert.strictEqual(result.itemsPerPage, 4);
    assert.strictEqual(result.totalPages, 2);
  });

  it('keeps ambiguity details in the final resolve error', async () => {
    const ambiguousHtml = createSearchHtml([
      { name: 'Alice', href: '/star/alice' },
      { name: 'Amy', href: '/star/amy' }
    ]);
    const routeMap = {};
    for (const origin of DEFAULT_ORIGINS) {
      routeMap[`${origin}/searchstar/A`] = ambiguousHtml;
    }
    installFetchMap(routeMap);

    await assert.rejects(
      () => resolveActressCrawlTarget({ actressName: 'A' }),
      (error) => {
        assert.ok(error.message.includes('未能定位女优目录'));
        assert.ok(error.message.includes('找到多个匹配目录：Alice、Amy'));
        return true;
      }
    );
  });
});
