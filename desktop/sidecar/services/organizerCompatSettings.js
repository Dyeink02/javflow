// Shared organizer/ad-learning compatibility settings helpers.
// deprecated: archived sidecar helper only; marker=archived-sidecar-organizer-compat-settings
// Keep sidecar-only organizer settings normalization in one place so archived
// compatibility facades do not each hand-maintain the same field rules.
//
// Ownership summary:
// 1) normalize sidecar organizer/ad-learning settings payloads
// 2) build the persisted compatibility settings patch shape
// 3) keep workflow policy and phase behavior out of the compatibility helpers
//
// File map for maintainers:
// 1) keyword/model/threshold coercion helpers
// 2) organizer compatibility payload normalization
// 3) persisted organizer/ad-learning settings patch builders

const progressSchema = require('../../common/progressSchema.js');

function normalizeKeywordList(rawValue) {
  const rawText = String(rawValue || '').trim();
  if (!rawText) {
    return [];
  }

  // Accept commas, Chinese commas, ideographic commas, and any whitespace.
  // A previous migration artifact dropped `\s` here and effectively treated the
  // letter `s` as a separator, which could silently corrupt keyword parsing.
  return Array.from(
    new Set(
      rawText
        .split(/[\s,\uFF0C\u3001]+/)
        .map((item) => item.trim().toLowerCase())
        .filter(Boolean)
    )
  );
}

function normalizeAdModelType(rawValue) {
  const value = String(rawValue || '').trim().toLowerCase();
  if (value === 'squeezenet-fast' || value === 'yolov8n-balanced' || value === 'mobile-net-v3-lite') {
    return value;
  }
  return 'mobile-net-v3-lite';
}

function normalizeAdThreshold(rawValue, fallbackValue = 60) {
  const numericValue = Number(rawValue);
  if (Number.isFinite(numericValue) && numericValue > 0) {
    return numericValue;
  }
  return Number(fallbackValue) > 0 ? Number(fallbackValue) : 60;
}

function normalizeAdFileAction(rawValue) {
  return progressSchema.normalizeAdFileAction(rawValue);
}

function resolveVideoExtensions(rawValue, fallbackValue) {
  return (
    String(rawValue || fallbackValue || '').trim() ||
    'mp4, mkv, avi, mov, flv, wmv, ts, m4v, iso'
  );
}

function resolveOrganizerCompatibilitySettings(currentSettings = {}, options = {}) {
  // This helper owns compatibility-level coercion only. It should not absorb
  // organizer workflow policy or scan/judge behavior.
  //
  // Fast troubleshooting split:
  // 1) archived sidecar settings coercion/default issue -> inspect this file
  // 2) organizer workflow/phase behavior issue -> inspect organizer service or
  //    Go organizer path instead
  const resolvedKeywords = normalizeKeywordList(options.adKeywords || currentSettings.organizerAdKeywords);
  const resolvedAdThreshold = normalizeAdThreshold(
    options.adThreshold,
    currentSettings.organizerAdThreshold || 60
  );
  const adDetectionEnabled = options.adDetectionEnabled !== false;
  const adModelType = normalizeAdModelType(options.adModelType || currentSettings.organizerAdModelType);
  const adFileAction = normalizeAdFileAction(options.adFileAction || currentSettings.organizerAdFileAction);
  const videoExtensions = resolveVideoExtensions(
    options.videoExtensions,
    currentSettings.organizerVideoExtensions
  );

  return {
    resolvedKeywords,
    resolvedAdThreshold,
    adDetectionEnabled,
    adModelType,
    adFileAction,
    videoExtensions
  };
}

function buildOrganizerSettingsPatch(currentSettings = {}, options = {}, resolved = {}) {
  // Persist only the normalized compatibility settings shape expected by the
  // archived organizer/ad-learning lane.
  return {
    ...currentSettings,
    organizerRoot: String(options.rootPath || '').trim(),
    organizerMinSizeMB: Number(options.minSizeMB || currentSettings.organizerMinSizeMB || 100),
    organizerSuffix: String(options.suffix || currentSettings.organizerSuffix || '-A').trim() || '-A',
    organizerVideoExtensions: resolved.videoExtensions,
    organizerAdFileAction: resolved.adFileAction,
    organizerDryRun: Boolean(options.dryRun),
    organizerIncludeSubdirectories: options.includeSubdirectories !== false,
    organizerStrictCodeMatch: options.strictExpectedCodes !== false,
    organizerCrawlOutput: String(options.crawlOutputDir || currentSettings.organizerCrawlOutput || '').trim(),
    organizerAdDetectionEnabled: resolved.adDetectionEnabled,
    organizerAdThreshold: resolved.resolvedAdThreshold,
    organizerAdKeywords: Array.isArray(resolved.resolvedKeywords) ? resolved.resolvedKeywords.join(', ') : '',
    organizerAdModelType: resolved.adModelType
  };
}

function buildAdLearningSettingsPatch(currentSettings = {}, options = {}) {
  // Learning updates intentionally patch only shared persisted knobs and do
  // not try to serialize transient learning-session state into settings.
  return {
    ...currentSettings,
    organizerAdKeywords: Array.isArray(options.keywords)
      ? options.keywords.join(', ')
      : currentSettings.organizerAdKeywords,
    organizerAdThreshold: normalizeAdThreshold(
      options.adScore,
      currentSettings.organizerAdThreshold
    ),
    organizerAdModelType: normalizeAdModelType(
      options.modelType || currentSettings.organizerAdModelType || ''
    )
  };
}

module.exports = {
  normalizeKeywordList,
  normalizeAdModelType,
  normalizeAdThreshold,
  normalizeAdFileAction,
  resolveVideoExtensions,
  resolveOrganizerCompatibilitySettings,
  buildOrganizerSettingsPatch,
  buildAdLearningSettingsPatch
};
