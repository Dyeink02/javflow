const assert = require('assert');

const { SERVICE_TEXT } = require('../desktop/common/text/serviceText.js');

describe('serviceText shared ranking/lookup wording', () => {
  it('keeps ranking fallback wording deterministic', () => {
    assert.strictEqual(SERVICE_TEXT.actressRanking.sourceName, 'AVfan');
    assert.ok(SERVICE_TEXT.actressRanking.browserMissing.includes('Chrome / Edge 浏览器'));
    assert.ok(SERVICE_TEXT.actressRanking.monthlyHistoryUnavailable('2026-01').includes('2026-01'));
  });

  it('keeps lookup error builders transport-neutral and data-driven', () => {
    assert.ok(SERVICE_TEXT.actressLookup.missingName.includes('女优名称'));
    assert.ok(SERVICE_TEXT.actressLookup.ambiguousCandidates(['A', 'B']).includes('A、B'));
    assert.ok(SERVICE_TEXT.actressLookup.resolveFailed(['x', 'y']).includes('x；y'));
  });
});
