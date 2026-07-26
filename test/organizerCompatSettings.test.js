const assert = require('assert');

const {
  normalizeKeywordList,
  normalizeAdModelType,
  normalizeAdThreshold,
  normalizeAdFileAction,
  resolveVideoExtensions,
  resolveOrganizerCompatibilitySettings,
  buildOrganizerSettingsPatch,
  buildAdLearningSettingsPatch
} = require('../desktop/sidecar/services/organizerCompatSettings.js');

describe('sidecar organizerCompatSettings compatibility helpers', () => {
  it('normalizes keywords, model type, thresholds, and compatibility payload defaults', () => {
    assert.deepStrictEqual(normalizeKeywordList('  Foo,bar  TBA  \nfoo  '), ['foo', 'bar', 'tba']);
    assert.deepStrictEqual(normalizeKeywordList(' Foo\uFF0Cbar\u3001TBA \nfoo '), ['foo', 'bar', 'tba']);
    assert.deepStrictEqual(normalizeKeywordList('boss,glass\uFF0Cmass'), ['boss', 'glass', 'mass']);
    assert.strictEqual(normalizeAdModelType('YOLOV8N-balanced'), 'yolov8n-balanced');
    assert.strictEqual(normalizeAdModelType('unknown-model'), 'mobile-net-v3-lite');
    assert.strictEqual(normalizeAdThreshold('88', 60), 88);
    assert.strictEqual(normalizeAdThreshold('', 60), 60);
    assert.strictEqual(normalizeAdFileAction('delete-directly'), 'delete-directly');
    assert.strictEqual(resolveVideoExtensions('', 'mp4, mkv'), 'mp4, mkv');

    const resolved = resolveOrganizerCompatibilitySettings(
      {
        organizerAdKeywords: 'old-one',
        organizerAdThreshold: 60,
        organizerAdModelType: 'mobile-net-v3-lite',
        organizerAdFileAction: 'move-to-delete',
        organizerVideoExtensions: 'mp4, mkv'
      },
      {
        adKeywords: 'One, Two one',
        adThreshold: '75',
        adModelType: 'squeezenet-fast',
        adFileAction: 'delete-directly',
        videoExtensions: 'mp4,avi'
      }
    );

    assert.deepStrictEqual(resolved, {
      resolvedKeywords: ['one', 'two'],
      resolvedAdThreshold: 75,
      adDetectionEnabled: true,
      adModelType: 'squeezenet-fast',
      adFileAction: 'delete-directly',
      videoExtensions: 'mp4,avi'
    });
  });

  it('builds persisted organizer and ad-learning settings patches from normalized values', () => {
    const organizerPatch = buildOrganizerSettingsPatch(
      {
        organizerMinSizeMB: 120,
        organizerSuffix: '-C',
        organizerCrawlOutput: 'D:/old',
        organizerAdKeywords: 'old'
      },
      {
        rootPath: 'D:/videos',
        minSizeMB: 256,
        suffix: '-A',
        dryRun: true,
        includeSubdirectories: false,
        strictExpectedCodes: false,
        crawlOutputDir: 'D:/crawl-output'
      },
      {
        resolvedKeywords: ['one', 'two'],
        resolvedAdThreshold: 81,
        adDetectionEnabled: true,
        adModelType: 'mobile-net-v3-lite',
        adFileAction: 'move-to-delete',
        videoExtensions: 'mp4,mkv'
      }
    );

    assert.deepStrictEqual(organizerPatch, {
      organizerMinSizeMB: 256,
      organizerSuffix: '-A',
      organizerCrawlOutput: 'D:/crawl-output',
      organizerAdKeywords: 'one, two',
      organizerRoot: 'D:/videos',
      organizerVideoExtensions: 'mp4,mkv',
      organizerAdFileAction: 'move-to-delete',
      organizerDryRun: true,
      organizerIncludeSubdirectories: false,
      organizerStrictCodeMatch: false,
      organizerAdDetectionEnabled: true,
      organizerAdThreshold: 81,
      organizerAdModelType: 'mobile-net-v3-lite'
    });

    const learningPatch = buildAdLearningSettingsPatch(
      {
        organizerAdKeywords: 'old',
        organizerAdThreshold: 60,
        organizerAdModelType: 'mobile-net-v3-lite'
      },
      {
        keywords: ['alpha', 'beta'],
        adScore: '92',
        modelType: 'yolov8n-balanced'
      }
    );

    assert.deepStrictEqual(learningPatch, {
      organizerAdKeywords: 'alpha, beta',
      organizerAdThreshold: 92,
      organizerAdModelType: 'yolov8n-balanced'
    });
  });
});
