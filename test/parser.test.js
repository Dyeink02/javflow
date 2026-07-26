const assert = require('assert');

const Parser = require('../dist/core/parser').default;

describe('Parser', () => {
  it('deduplicates page links and removes empty href values', () => {
    const html = `
      <html>
        <body>
          <a class="movie-box" href="https://www.javbus.com/ABF-001"></a>
          <a class="movie-box" href="https://www.javbus.com/ABF-001"></a>
          <a class="movie-box" href="  https://www.javbus.com/ABF-002  "></a>
          <a class="movie-box"></a>
        </body>
      </html>
    `;

    const links = Parser.parsePageLinks(html);

    assert.deepStrictEqual(links, [
      'https://www.javbus.com/ABF-001',
      'https://www.javbus.com/ABF-002'
    ]);
  });

  it('parses metadata from fallback script source', () => {
    const html = `
      <html>
        <body>
          <h3>SSIS-442 Sample Title</h3>
          <a class="bigImage"><img src="/images/sample.jpg" /></a>
          <script>console.log('noise')</script>
          <script>
            window.__NEXT_DATA__ = {"props":{"pageProps":{"gid":"123456","uc":"0","img":"/images/sample.jpg"}}};
          </script>
        </body>
      </html>
    `;

    const metadata = Parser.parseMetadata(html);

    assert.strictEqual(metadata.gid, '123456');
    assert.strictEqual(metadata.uc, '0');
    assert.strictEqual(metadata.img, '/images/sample.jpg');
    assert.strictEqual(metadata.title, 'SSIS-442 Sample Title');
  });

  it('parses metadata from ajax query fallback and og tags', () => {
    const html = `
      <html>
        <head>
          <title>Fallback Title</title>
          <meta property="og:title" content="REAL-001 Ajax Title" />
          <meta property="og:image" content="https://www.javbus.com/images/cover.jpg" />
        </head>
        <body>
          <script>
            const ajaxUrl = "/ajax/uncledatoolsbyajax.php?gid=654321&lang=zh&img=%2Fimages%2Fcover.jpg&uc=1";
          </script>
        </body>
      </html>
    `;

    const metadata = Parser.parseMetadata(html);

    assert.strictEqual(metadata.gid, '654321');
    assert.strictEqual(metadata.uc, '1');
    assert.strictEqual(metadata.img, '%2Fimages%2Fcover.jpg');
    assert.strictEqual(metadata.title, 'REAL-001 Ajax Title');
  });

  it('keeps cover image on parsed film data', () => {
    const filmData = Parser.parseFilmData(
      {
        title: 'ABF-001 Sample Title',
        gid: '123',
        img: '/images/sample-cover.jpg',
        uc: '0',
        category: ['Drama'],
        actress: ['Actress A']
      },
      'https://www.javbus.com/ABF-001'
    );

    assert.strictEqual(filmData.coverImage, '/images/sample-cover.jpg');
    assert.strictEqual(filmData.sourceLink, 'https://www.javbus.com/ABF-001');
  });

  it('stores actress count on parsed film data', () => {
    const filmData = Parser.parseFilmData(
      {
        title: 'DAZD-277 Sample Title',
        gid: '123',
        img: '/images/sample-cover.jpg',
        uc: '0',
        category: ['Drama'],
        actress: ['A', 'B', 'C', 'D', 'E', 'F']
      },
      'https://www.javbus.com/DAZD-277'
    );

    assert.strictEqual(filmData.actressCount, 6);
  });
});
