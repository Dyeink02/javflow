const assert = require('assert');
const fs = require('fs');
const os = require('os');
const path = require('path');

const {
  collectAjaxBaseOrigins,
  loadAjaxAntiBlockBaseOrigins,
  normalizeRequestHandlerBaseOrigin
} = require('../dist/core/requestHandlerBaseOriginUtils');

describe('requestHandlerBaseOriginUtils', () => {
  it('normalizes base origins and keeps unique candidate order', () => {
    const origins = collectAjaxBaseOrigins({
      preferredOrigin: 'https://www.busjav.cyou/star/xix',
      configuredBase: 'https://www.javbus.com/',
      antiBlockOrigins: ['https://www.fanbus.bond/foo', 'invalid-url', 'https://www.javbus.com/bar'],
      knownMirrorOrigins: ['https://www.cdnbus.bond', 'https://www.busjav.cyou']
    });

    assert.deepStrictEqual(origins, [
      'https://www.busjav.cyou',
      'https://www.fanbus.bond',
      'https://www.javbus.com',
      'https://www.cdnbus.bond'
    ]);
    assert.strictEqual(normalizeRequestHandlerBaseOrigin('invalid-url'), null);
  });

  it('prioritizes anti-block mirrors before the official javbus domain when mirrors are available', () => {
    const origins = collectAjaxBaseOrigins({
      configuredBase: 'https://www.javbus.com/star/wc8',
      antiBlockOrigins: ['https://www.fanbus.cyou', 'https://www.busjav.cyou'],
      knownMirrorOrigins: ['https://www.cdnbus.bond']
    });

    assert.deepStrictEqual(origins, [
      'https://www.fanbus.cyou',
      'https://www.busjav.cyou',
      'https://www.javbus.com',
      'https://www.cdnbus.bond'
    ]);
  });

  it('loads valid anti-block origins and drops invalid entries', () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), 'jav-antiblock-'));
    const antiBlockFilePath = path.join(tempDir, '.jav-scrapy-antiblock-urls.json');
    fs.writeFileSync(
      antiBlockFilePath,
      JSON.stringify(['https://www.busjav.cyou/star/xix', 'bad-value', 'https://www.fanbus.bond']),
      'utf8'
    );

    const origins = loadAjaxAntiBlockBaseOrigins({ antiBlockFilePath });
    assert.deepStrictEqual(origins, ['https://www.busjav.cyou', 'https://www.fanbus.bond']);
  });
});
