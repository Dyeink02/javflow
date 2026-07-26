// Result panel renderer owns the post-run quality summary card and its handoff
// into the dedicated result-history controller.
//
// Ownership summary:
// 1) render the normalized crawl result/quality summary panel
// 2) hand off completed result snapshots to the result-history controller
// 3) keep result DOM projection separate from result read-model assembly
//
// File map for maintainers:
// 1) result-panel signature dedupe
// 2) quality/result summary DOM projection
// 3) completed-result history handoff
(function initializeResultPanelRenderer(globalScope) {
  function createResultPanelRenderer(options) {
    const stateHelpers = globalScope.desktopStateHelpers || null;
    if (!stateHelpers) {
      throw new Error('desktopStateHelpers is required before resultPanelRenderer');
    }

    const { applyMiniStatus } = stateHelpers;
    const {
      crawlResultQuality,
      crawlResultSummary,
      resultHistoryController = null,
      statusLabels = {}
    } = options || {};

    // Result panel is a pure projection of the latest normalized result/quality
    // payload. Keep one render signature here so callers can push frequent
    // updates without re-rendering the DOM or duplicating result-history writes.
    let lastSignature = '';

    function buildSummaryText(panel = {}) {
      // Keep the top result card focused on operator guidance. The detailed
      // quality summary already appears in the result-history card list.
      if (panel.outputDirExists || panel.filmDataExists || panel.magnetExists || panel.logDirExists || panel.reportExists) {
        return '抓取产物已同步到下方入口，复盘摘要请查看历史记录。';
      }

      return String(panel.message || '等待抓取完成后生成结果入口。').trim();
    }

    function applyPanel(panel = {}) {
      const signature = JSON.stringify({
        status: panel.status || '',
        message: panel.message || '',
        outputDir: panel.outputDir || '',
        filmDataPath: panel.filmDataPath || '',
        magnetPath: panel.magnetPath || '',
        logDir: panel.logDir || '',
        latestLogPath: panel.latestLogPath || '',
        reportPath: panel.reportPath || '',
        qualityStatus: panel.qualityStatus || '',
        qualityStatusText: panel.qualityStatusText || '',
        qualitySummaryLine: panel.qualitySummaryLine || '',
        qualityCompletedAt: panel.qualityCompletedAt || '',
        qualityDurationSec: panel.qualityDurationSec || 0,
        flags: [
          panel.outputDirExists,
          panel.filmDataExists,
          panel.magnetExists,
          panel.logDirExists,
          panel.latestLogExists,
          panel.reportExists
        ]
      });

      if (signature === lastSignature) {
        return;
      }

      lastSignature = signature;
      applyMiniStatus(
        crawlResultQuality,
        panel.qualityStatus || panel.status || 'idle',
        String(panel.qualityStatusText || '尚未生成复盘摘要').trim(),
        statusLabels
      );

      if (crawlResultSummary) {
        crawlResultSummary.textContent = buildSummaryText(panel);
      }

      if (resultHistoryController && typeof resultHistoryController.applyPanel === 'function') {
        resultHistoryController.applyPanel(panel);
      }
    }

    function clearHistory() {
      if (resultHistoryController && typeof resultHistoryController.clearHistory === 'function') {
        resultHistoryController.clearHistory();
      }
    }

    return {
      applyPanel,
      clearHistory
    };
  }

  globalScope.desktopResultPanelRenderer = {
    createResultPanelRenderer
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
