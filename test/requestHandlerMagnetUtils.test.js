const assert = require('assert');

const magnetUtils = require('../dist/core/requestHandlerMagnetUtils');

describe('requestHandler magnet display names', () => {
  it('reads case, suffix, domain prefix, and HTML-escaped query variants', () => {
    const html = [
      'magnet:?xt=urn:btih:AAA&dn=ABA-250',
      'magnet:?xt=urn:btih:BBB&dn=aba-250',
      'magnet:?xt=urn:btih:CCC&dn=ABA-250-c',
      'magnet:?dn=ABA-250-u&xt=urn:btih:DDD',
      'magnet:?xt=urn:btih:EEE&amp;dn=1818.com%40ABA-250',
    ].join(' ');

    const links = magnetUtils.extractMagnetLinks(html);
    const candidates = magnetUtils.buildParsedMagnetCandidates(links, []);

    assert.deepStrictEqual(
      candidates.map((candidate) => candidate.displayName),
      ['ABA-250', 'aba-250', 'ABA-250-c', 'ABA-250-u', '1818.com@ABA-250']
    );
  });

  it('accepts a BTIH magnet without a display name', () => {
    const links = magnetUtils.extractMagnetLinks(
      'magnet:?xt=urn:btih:0123456789ABCDEF0123456789ABCDEF01234567'
    );

    assert.strictEqual(links.length, 1);
    const [candidate] = magnetUtils.buildParsedMagnetCandidates(links, []);
    assert.strictEqual(candidate.displayName, links[0]);
  });

  it('persists displayName on selected and backup output entries', () => {
    const result = magnetUtils.buildMagnetResult(
      [
        { magnetLink: 'magnet:?xt=urn:btih:AAA&dn=ABA-250', size: 1024, displayName: 'ABA-250' },
        { magnetLink: 'magnet:?xt=urn:btih:BBB&dn=ABA-250-c', size: 2048, displayName: 'ABA-250-c' },
      ],
      false,
      magnetUtils.formatFileSize,
      2
    );

    assert.strictEqual(result.magnetLinks[0].displayName, 'ABA-250-c');
    assert.deepStrictEqual(
      result.backupMagnetLinks.map((entry) => entry.displayName),
      ['ABA-250-c', 'ABA-250']
    );
  });
});
