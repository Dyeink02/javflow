const assert = require('assert');

const {
  resolveIndexValidationEffectiveDiagnosticReason,
  resolveIndexValidationSampleProgress
} = require('../dist/core/scraperRunnerIndexSampleUtils');

describe('scraperRunnerIndexSampleUtils', () => {
  it('promotes the merged sample when it improves the best count', () => {
    const result = resolveIndexValidationSampleProgress({
      previousBestCount: 24,
      mergedCount: 28,
      bestDiagnosticReason: '',
      sampleDiagnosticReason: 'sample improved'
    });

    assert.deepStrictEqual(result, {
      previousBestCount: 24,
      mergedCount: 28,
      currentBestCount: 28,
      shouldPromoteMergedLinks: true,
      bestDiagnosticReason: 'sample improved',
      effectiveDiagnosticReason: 'sample improved'
    });
  });

  it('keeps the previous best diagnostic when the merged sample does not improve', () => {
    const result = resolveIndexValidationSampleProgress({
      previousBestCount: 30,
      mergedCount: 30,
      bestDiagnosticReason: 'existing best',
      sampleDiagnosticReason: 'new sample'
    });

    assert.deepStrictEqual(result, {
      previousBestCount: 30,
      mergedCount: 30,
      currentBestCount: 30,
      shouldPromoteMergedLinks: false,
      bestDiagnosticReason: 'existing best',
      effectiveDiagnosticReason: 'existing best'
    });
  });

  it('backfills the best diagnostic when the first non-empty sample reason arrives', () => {
    const result = resolveIndexValidationSampleProgress({
      previousBestCount: 30,
      mergedCount: 30,
      bestDiagnosticReason: '',
      sampleDiagnosticReason: 'duplicate ids detected'
    });

    assert.deepStrictEqual(result, {
      previousBestCount: 30,
      mergedCount: 30,
      currentBestCount: 30,
      shouldPromoteMergedLinks: false,
      bestDiagnosticReason: 'duplicate ids detected',
      effectiveDiagnosticReason: 'duplicate ids detected'
    });
  });

  it('falls back to the sample diagnostic only when no best diagnostic exists', () => {
    assert.strictEqual(resolveIndexValidationEffectiveDiagnosticReason('best', 'sample'), 'best');
    assert.strictEqual(resolveIndexValidationEffectiveDiagnosticReason('', 'sample'), 'sample');
    assert.strictEqual(resolveIndexValidationEffectiveDiagnosticReason('', ''), '');
  });
});
