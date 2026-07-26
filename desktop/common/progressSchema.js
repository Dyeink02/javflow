// Shared organizer/learning progress vocabulary.
// Backend publishers and frontend renderers should both use these phase names
// so progress text and exported reports do not drift into separate schemas.
//
// Maintenance boundary:
// - phase constants and progress-message wording live here
// - organizer/ad-learning services publish structured progress only
// - UI panels render the normalized schema produced here
//
// File map for maintainers:
// 1) shared organizer/learning phase constants
// 2) shared progress payload construction helpers
// 3) centralized organizer/learning progress wording builders
(function registerDesktopProgressSchema(globalScope) {
  // Ownership summary:
  // 1) define shared progress phase enums for organizer/ad-learning flows
  // 2) build centralized human-readable progress copy from structured payloads
  // 3) keep progress wording/payload shape out of per-feature controllers
  const ORGANIZER_PROGRESS_PHASES = Object.freeze({
    starting: 'starting',
    scanStart: 'scan-start',
    scanProgress: 'scan-progress',
    scanCompleted: 'scan-completed',
    waitingStart: 'waiting-start',
    waitingProgress: 'waiting-progress',
    deleteStart: 'delete-start',
    deleteProgress: 'delete-progress',
    introAdStart: 'intro-ad-start',
    introAdProgress: 'intro-ad-progress',
    completed: 'completed'
  });

  const LEARNING_PROGRESS_PHASES = Object.freeze({
    starting: 'starting',
    scanReady: 'scan-ready',
    matching: 'matching',
    learning: 'learning',
    completed: 'completed'
  });

  function normalizeAdFileAction(rawValue) {
    // Keep action normalization here so backend and renderer do not fork their
    // own tiny enums and later disagree on report/panel wording branches.
    return String(rawValue || '').trim() === 'delete-directly' ? 'delete-directly' : 'move-to-delete';
  }

  function toSafeNumber(value, fallback = 0) {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : fallback;
  }

  function createProgress(scope, phase, payload = {}) {
    // Progress payloads should carry numbers and phase identity only. Domain
    // services publish structured state; wording is derived centrally here.
    return {
      ...payload,
      scope: String(scope || payload.scope || '').trim(),
      phase: String(phase || payload.phase || '').trim(),
      timestamp: payload.timestamp || new Date().toISOString()
    };
  }

  // Keep these summary builders deterministic; they feed the organizer panel
  // and should match any exported progress wording used for diagnosis.
  function buildOrganizerProgressMessage(progress = {}) {
    // Message shaping stays centralized here so report wording and live panel
    // wording do not drift apart across modules.
    const phase = String(progress.phase || '');
    const total = toSafeNumber(progress.total, 0);
    const processed = toSafeNumber(progress.processed, 0);
    const adFileAction = normalizeAdFileAction(progress.adFileAction);

    if (phase === ORGANIZER_PROGRESS_PHASES.waitingProgress || phase === ORGANIZER_PROGRESS_PHASES.waitingStart) {
      return `待整理总数 ${total} 个，已执行 ${processed} 个。`;
    }

    if (phase === ORGANIZER_PROGRESS_PHASES.deleteProgress || phase === ORGANIZER_PROGRESS_PHASES.deleteStart) {
      return adFileAction === 'delete-directly'
        ? `广告处理总数 ${total} 个，已直接删除 ${processed} 个。`
        : `待删除总数 ${total} 个，已执行 ${processed} 个。`;
    }

    if (phase === ORGANIZER_PROGRESS_PHASES.introAdProgress || phase === ORGANIZER_PROGRESS_PHASES.introAdStart) {
      return `含开头广告总数 ${total} 个，已归档 ${processed} 个。`;
    }

    if (phase === ORGANIZER_PROGRESS_PHASES.scanProgress || phase === ORGANIZER_PROGRESS_PHASES.scanStart) {
      return `扫描进度 ${processed}/${total}。`;
    }

    if (phase === ORGANIZER_PROGRESS_PHASES.scanCompleted) {
      const waitingTotal = toSafeNumber(progress.waitingTotal, 0);
      const deleteTotal = toSafeNumber(progress.deleteTotal, 0);
      const introAdTotal = toSafeNumber(progress.introAdTotal, 0);
      return `扫描完成，待整理 ${waitingTotal} 个，待删除 ${deleteTotal} 个，含开头广告 ${introAdTotal} 个。`;
    }

    if (phase === ORGANIZER_PROGRESS_PHASES.completed) {
      const waitingTotal = toSafeNumber(progress.waitingTotal, 0);
      const deleteTotal = toSafeNumber(progress.deleteTotal, 0);
      const introAdTotal = toSafeNumber(progress.introAdTotal, 0);
      const deletedDirectly = toSafeNumber(progress.deletedDirectly, 0);
      return `整理完成，待整理 ${waitingTotal} 个，待删除 ${deleteTotal} 个，含开头广告 ${introAdTotal} 个，直接删除 ${deletedDirectly} 个。`;
    }

    return '';
  }

  function buildLearningProgressMessage(progress = {}) {
    // Same rule for learning progress: lower services should emit numbers and
    // phases, while user-facing wording stays in one shared place.
    const phase = String(progress.phase || '');
    const totalVideos = toSafeNumber(progress.totalVideos, 0);
    const processedVideos = toSafeNumber(progress.processedVideos, 0);
    const matchedVideoCount = toSafeNumber(progress.matchedVideoCount, 0);
    const importedSampleCount = toSafeNumber(progress.importedSampleCount, 0);
    const requestedCodeCount = toSafeNumber(progress.requestedCodeCount, 0);

    if (phase === LEARNING_PROGRESS_PHASES.starting) {
      return `按番号学习启动：目标番号 ${requestedCodeCount} 个。`;
    }

    if (phase === LEARNING_PROGRESS_PHASES.scanReady) {
      return `学习扫描完成：发现视频 ${totalVideos} 个，开始按番号匹配。`;
    }

    if (phase === LEARNING_PROGRESS_PHASES.matching) {
      return `按番号匹配中：${processedVideos}/${totalVideos}，已命中 ${matchedVideoCount}，新增样本 ${importedSampleCount}。`;
    }

    if (phase === LEARNING_PROGRESS_PHASES.learning) {
      return `抓帧学习中：已命中 ${matchedVideoCount}，新增样本 ${importedSampleCount}。`;
    }

    if (phase === LEARNING_PROGRESS_PHASES.completed) {
      const missingCodeCount = toSafeNumber(progress.missingCodeCount, 0);
      const hitRate = toSafeNumber(progress.hitRate, 0);
      const falsePositiveRate = toSafeNumber(progress.falsePositiveRate, 0);
      const sampleIncrement = toSafeNumber(progress.sampleIncrement, importedSampleCount);
      return `按番号学习完成：命中视频 ${matchedVideoCount}，新增样本 ${sampleIncrement}，未命中番号 ${missingCodeCount}，命中率 ${hitRate.toFixed(
        2
      )}% ，误判率 ${falsePositiveRate.toFixed(2)}%。`;
    }

    return '';
  }

  function buildProgressMessage(progress = {}) {
    // Scope dispatch stays here so renderer/service code does not branch on
    // message wording rules in multiple places.
    if (String(progress.scope || '') === 'learning') {
      return buildLearningProgressMessage(progress);
    }
    return buildOrganizerProgressMessage(progress);
  }

  const payload = {
    ORGANIZER_PROGRESS_PHASES,
    LEARNING_PROGRESS_PHASES,
    normalizeAdFileAction,
    createProgress,
    buildOrganizerProgressMessage,
    buildLearningProgressMessage,
    buildProgressMessage
  };

  globalScope.desktopProgressSchema = payload;

  if (typeof module !== 'undefined' && module.exports) {
    module.exports = payload;
  }
})(typeof globalThis !== 'undefined' ? globalThis : window);
