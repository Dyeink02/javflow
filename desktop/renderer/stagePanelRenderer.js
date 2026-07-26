// Stage panel renderer owns only the live crawl-stage card.
// It should stay read-only: translate normalized panel payload into DOM without
// reconstructing crawler facts.
//
// Ownership summary:
// 1) render the live stage card from normalized panel payloads
// 2) dedupe noisy runtime updates before they touch the DOM
// 3) keep progress/text fallback shaping local to the stage card only
//
// It does not fetch runtime state or decide crawl policy.
//
// File map for maintainers:
// 1) stage progress percent helpers
// 2) stage-panel signature dedupe
// 3) stage card DOM projection
(function initializeStagePanelRenderer(globalScope) {
  function createStagePanelRenderer(options) {
    const stateHelpers = globalScope.desktopStateHelpers || null;
    if (!stateHelpers) {
      throw new Error('desktopStateHelpers is required before stagePanelRenderer');
    }

    const { applyMiniStatus, normalizePathText } = stateHelpers;
    const {
      crawlStageStatus,
      crawlStageProgress,
      crawlStageTitle,
      crawlStageDescription,
      crawlStageMessage,
      crawlStageBarFill,
      crawlStageBar,
      crawlStageOutput,
      crawlStagePage,
      crawlStageQueued,
      crawlStageAttempted,
      crawlStageCompleted,
      statusLabels = {},
      defaultMessage = '等待开始抓取。'
    } = options || {};

    // Stage panel render is signature-deduped so noisy runtime updates can be
    // pushed freely without repainting the stage card when nothing meaningful
    // changed.
    let lastSignature = '';

    function computeStageProgressPercent(panel = {}) {
      const status = String(panel.status || '').trim().toLowerCase();
      if (status === 'completed') {
        return 100;
      }

      const phaseIndex = Number(panel.phaseIndex || 0);
      const phaseTotal = Number(panel.phaseTotal || 0);
      if (Number.isFinite(phaseIndex) && Number.isFinite(phaseTotal) && phaseIndex > 0 && phaseTotal > 0) {
        return Math.max(8, Math.min(100, Math.round((phaseIndex / phaseTotal) * 100)));
      }

      const stats = panel && panel.stats && typeof panel.stats === 'object' ? panel.stats : {};
      const attempted = Number(stats.attempted || 0);
      const completed = Number(stats.completed || 0);
      if (attempted > 0 && completed >= 0) {
        return Math.max(8, Math.min(100, Math.round((completed / attempted) * 100)));
      }

      if (status === 'starting' || status === 'running') {
        return 12;
      }

      return 0;
    }

    function applyPanel(panel = {}) {
      // The signature covers only view-relevant fields so controllers can emit
      // high-frequency panel events without forcing redundant repaints.
      const signature = JSON.stringify({
        status: panel.status || '',
        message: panel.message || '',
        phaseKey: panel.phaseKey || '',
        phaseTitle: panel.phaseTitle || '',
        phaseDescription: panel.phaseDescription || '',
        phaseProgressText: panel.phaseProgressText || '',
        phaseIndex: panel.phaseIndex || 0,
        phaseTotal: panel.phaseTotal || 0,
        outputDir: panel.outputDir || '',
        stats: panel.stats || {}
      });

      if (signature === lastSignature) {
        return;
      }

      lastSignature = signature;
      applyMiniStatus(
        crawlStageStatus,
        panel.status,
        statusLabels[String(panel.status || 'idle').toLowerCase()] || '待机',
        statusLabels
      );

      if (crawlStageProgress) {
        crawlStageProgress.textContent = String(panel.phaseProgressText || '阶段 1/10').trim();
      }
      if (crawlStageTitle) {
        crawlStageTitle.textContent = String(panel.phaseTitle || '等待开始抓取').trim();
      }
      if (crawlStageDescription) {
        crawlStageDescription.textContent = String(panel.phaseDescription || defaultMessage).trim();
      }
      if (crawlStageMessage) {
        crawlStageMessage.textContent = String(panel.message || defaultMessage).trim();
      }
      if (crawlStageOutput) {
        crawlStageOutput.textContent = normalizePathText(panel.outputDir);
        crawlStageOutput.title = String(panel.outputDir || '').trim();
      }

      const stageProgressPercent = computeStageProgressPercent(panel);
      if (crawlStageBarFill) {
        crawlStageBarFill.style.width = `${stageProgressPercent}%`;
      }

      if (crawlStageBar) {
        const status = String(panel.status || '').trim().toLowerCase();
        const isWaiting = (status === 'starting' || status === 'running') && stageProgressPercent === 0;
        crawlStageBar.classList.toggle('is-waiting', isWaiting);
      }

      const stats = panel && panel.stats && typeof panel.stats === 'object' ? panel.stats : {};
      if (crawlStagePage) {
        crawlStagePage.textContent = String(stats.pageIndex ?? 0);
      }
      if (crawlStageQueued) {
        crawlStageQueued.textContent = String(stats.queued ?? 0);
      }
      if (crawlStageAttempted) {
        crawlStageAttempted.textContent = String(stats.attempted ?? 0);
      }
      if (crawlStageCompleted) {
        crawlStageCompleted.textContent = String(stats.completed ?? 0);
      }
    }

    return {
      applyPanel
    };
  }

  globalScope.desktopStagePanelRenderer = {
    createStagePanelRenderer
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
