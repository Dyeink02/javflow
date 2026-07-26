// Crawl result-history controller owns:
// 1) localStorage-backed result entry persistence
// 2) result-history card rendering
// 3) open-path actions for output/log/report artifacts
//
// Keep this separate from stateController so crawl state rendering and
// persisted result-entry UI do not keep growing inside the same large file.
//
// Boundary rule:
// this controller reflects completed run artifacts only. Live crawl-state
// rendering continues to belong to crawlRuntime/state controllers.
//
// Ownership summary:
// 1) persist recent crawl result-history entries in renderer storage
// 2) render/open/delete result-history cards and linked artifacts
// 3) keep completed-run artifact history out of live crawl-state controllers
//
// File map for maintainers:
// 1) renderer-local history load/save helpers
// 2) result-history entry shaping
// 3) history card rendering and artifact open actions
(function initializeCrawlResultHistoryController(globalScope) {
  const rendererHelpers = globalScope.desktopRendererHelpers || {};
  const historyViewFactory = globalScope.desktopCrawlResultHistoryView || null;
  const safeLocalStorageGet = rendererHelpers.safeLocalStorageGet;
  const safeLocalStorageSet = rendererHelpers.safeLocalStorageSet;

  if (!historyViewFactory || !safeLocalStorageGet || !safeLocalStorageSet) {
    throw new Error('crawlResultHistoryController requires desktopCrawlResultHistoryView and desktopRendererHelpers');
  }

  function createCrawlResultHistoryController(options) {
    const {
      crawlPanelModel,
      historyView,
      openPath,
      storageKey = 'jav.crawl.result.history.v2'
    } = options || {};

    let resultHistory = [];
    let bootstrapCompleted = false;

    function loadResultHistory() {
      // History persistence is renderer-local convenience only. Broken or stale
      // localStorage should never block current crawl state from rendering.
      const rawValue = safeLocalStorageGet(storageKey, '');
      if (!rawValue) {
        return [];
      }
      try {
        const parsed = JSON.parse(rawValue);
        return Array.isArray(parsed) ? parsed : [];
      } catch {
        return [];
      }
    }

    function saveResultHistory(items) {
      safeLocalStorageSet(storageKey, JSON.stringify(items));
    }

    // Keep result-history identity stable by anchoring it to the run/output
    // itself, not to whichever file paths happen to be populated first.
    function buildResultHistoryEntry(panel = {}) {
      const normalized =
        crawlPanelModel && typeof crawlPanelModel.buildResultHistoryIdentity === 'function'
          ? crawlPanelModel.buildResultHistoryIdentity(panel)
          : null;
      if (!normalized) {
        return null;
      }

      return {
        historyKey: normalized.historyKey,
        title: normalized.title,
        updatedAt: new Date().toISOString(),
        qualityStatus: String(panel.qualityStatus || panel.status || 'idle').trim() || 'idle',
        qualityStatusText: String(panel.qualityStatusText || '').trim(),
        summary: String(panel.qualitySummaryLine || panel.message || '').trim(),
        outputDir: normalized.outputDir,
        outputDirExists: Boolean(panel.outputDirExists),
        filmDataPath: normalized.filmDataPath,
        filmDataExists: Boolean(panel.filmDataExists),
        magnetPath: normalized.magnetPath,
        magnetExists: Boolean(panel.magnetExists),
        logDir: normalized.logDir,
        logDirExists: Boolean(panel.logDirExists),
        latestLogPath: normalized.latestLogPath,
        latestLogExists: Boolean(panel.latestLogExists),
        reportPath: normalized.reportPath,
        reportExists: Boolean(panel.reportExists)
      };
    }

    function syncResultHistory(panel = {}) {
      const nextEntry = buildResultHistoryEntry(panel);
      if (!nextEntry) {
        return;
      }

      const existingItems = Array.isArray(resultHistory) ? resultHistory : [];
      const filtered = existingItems.filter((item) => item && item.historyKey !== nextEntry.historyKey);
      resultHistory = [nextEntry, ...filtered].slice(0, 12);
      saveResultHistory(resultHistory);
    }

    function bindAsyncClick(button, handler) {
      if (!button) {
        return;
      }

      button.addEventListener('click', async () => {
        await handler();
      });
    }

    function deleteHistoryEntry(historyKey) {
      if (!historyKey) {
        return;
      }
      const items = Array.isArray(resultHistory) ? resultHistory : [];
      resultHistory = items.filter((entry) => entry && entry.historyKey !== historyKey);
      saveResultHistory(resultHistory);
      renderHistory();
    }

    function clearHistory() {
      resultHistory = [];
      saveResultHistory(resultHistory);
      renderHistory();
    }

    function renderHistory() {
      if (!historyView) {
        return;
      }

      // 按更新时间降序排序，最新的历史记录显示在最前面
      const sortedHistory = [...(Array.isArray(resultHistory) ? resultHistory : [])].sort((a, b) => {
        const timeA = new Date(a && a.updatedAt ? a.updatedAt : '').getTime();
        const timeB = new Date(b && b.updatedAt ? b.updatedAt : '').getTime();
        return timeB - timeA;
      });
      resultHistory = sortedHistory;

      // View rendering stays delegated so this controller owns persistence and
      // actions, while the card DOM structure remains in the view module.
      historyViewFactory.renderHistory(historyView, sortedHistory, {
        emptyMessage: '等待抓取完成后生成最近结果入口。',
        onBindOpenPath: (button, pathValue, exists) => {
          button.disabled = !pathValue || !exists || typeof openPath !== 'function';
          bindAsyncClick(button, async () => {
            if (!button.disabled) {
              await openPath(pathValue);
            }
          });
        },
        onDelete: (item) => {
          if (!item || !item.historyKey) {
            return;
          }
          if (globalScope.confirm) {
            if (!globalScope.confirm('确定要删除此记录吗？仅删除软件内记录，不会删除本地磁力和日志文件。')) {
              return;
            }
          }
          deleteHistoryEntry(item.historyKey);
        }
      });
    }

    function applyPanel(panel = {}) {
      syncResultHistory(panel);
      renderHistory();
    }

    function bootstrap() {
      if (bootstrapCompleted) {
        return;
      }

      // Result history only mirrors persisted localStorage state. Once loaded,
      // ongoing panel updates mutate the in-memory list directly, so bootstrap
      // must stay one-shot and never reload an older snapshot over newer entries.
      resultHistory = loadResultHistory();
      renderHistory();
      bootstrapCompleted = true;
    }

    return {
      bootstrap,
      applyPanel,
      clearHistory
    };
  }

  globalScope.desktopCrawlResultHistoryController = {
    createCrawlResultHistoryController
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
