// Shared crawl-panel normalization model for the active desktop renderer.
// This file converts mixed backend/runtime payloads into stable UI-facing panel
// shapes so renderer controllers do not each reimplement the same mapping.
//
// Ownership summary:
// 1) normalize task snapshots/runtime payloads into stable stage/result panels
// 2) derive history/review/quality helper payloads from mixed state shapes
// 3) keep fallback heuristics in one place instead of spreading them across
//    controllers
//
// This file does not fetch data, dispatch commands, or touch the DOM directly.
//
// File map for maintainers:
// 1) low-level text/path/status normalization helpers
// 2) stage/result panel fallback builders
// 3) review/history/quality payload normalization helpers

(function registerCrawlPanelModel(globalScope) {
  function cleanText(value) {
    return String(value || '').trim();
  }

  function firstNonEmpty() {
    for (let index = 0; index < arguments.length; index += 1) {
      const value = cleanText(arguments[index]);
      if (value) {
        return value;
      }
    }
    return '';
  }

  function pathLeaf(pathValue) {
    const segments = cleanText(pathValue).split(/[/\\]/).filter(Boolean);
    return segments.length > 0 ? segments[segments.length - 1] : '';
  }

  function isActiveStatus(status) {
    return ['starting', 'running', 'stopping'].includes(cleanText(status).toLowerCase());
  }

  function normalizeTaskSnapshotStatus(snapshot) {
    // Startup status should describe the current controller lifecycle. Historical
    // final states still belong to result/review panels, but must not repaint
    // the top idle pill as error/completed before a new crawl begins.
    const controllerStatus = cleanText(snapshot && snapshot.controllerStatus).toLowerCase();
    const runtimeStatus = cleanText(snapshot && snapshot.lastCrawlStatus).toLowerCase();
    const running = Boolean(snapshot && snapshot.isRunning);

    if (isActiveStatus(controllerStatus)) {
      return controllerStatus;
    }
    if (running && isActiveStatus(runtimeStatus)) {
      return runtimeStatus;
    }
    return 'idle';
  }

  function resolveOutputDir(source, fallbackOutputDir) {
    // Output-dir fallback priority is centralized here because multiple
    // generations of runtime payloads still expose slightly different fields.
    return firstNonEmpty(
      source && source.outputDir,
      source && source.currentTaskOutputDir,
      source && source.lastTaskOutputDir,
      source && source.targetOutput,
      fallbackOutputDir
    );
  }

  function buildStagePanelFromTaskSnapshot(snapshot, options) {
    if (!snapshot) {
      return null;
    }

    // Resume-stage fallback shaping lives here so renderer controllers do not
    // each invent their own "last known crawl state" projection rules.
    const settings = options && typeof options === 'object' ? options : {};
    const defaultMessage = cleanText(settings.defaultMessage) || '等待开始抓取。';
    const status = normalizeTaskSnapshotStatus(snapshot);
    const outputDir = firstNonEmpty(snapshot.currentTaskOutputDir, snapshot.lastTaskOutputDir);
    const message = cleanText(snapshot.lastCrawlMessage) || defaultMessage;

    if (status === 'idle' && !outputDir) {
      return null;
    }

    const isRunning = Boolean(snapshot.isRunning);
    return {
      status,
      message,
      phaseKey: isRunning ? 'resume-running' : 'resume-latest',
      phaseTitle: isRunning ? '已恢复抓取任务' : '已加载最近任务',
      phaseDescription: isRunning
        ? '当前任务状态已从 Go 主控恢复，等待实时事件继续更新。'
        : '最近一次抓取状态已从 Go 主控加载。',
      phaseProgressText: isRunning ? '恢复中' : '最近记录',
      phaseIndex: 0,
      phaseTotal: 0,
      outputDir,
      stats: {
        pageIndex: 0,
        queued: 0,
        attempted: 0,
        completed: 0
      }
    };
  }

  function buildResultPanelFromTaskSnapshot(snapshot, options) {
    if (!snapshot) {
      return null;
    }

    // Result-panel fallback follows the same rule: one shared snapshot-to-panel
    // mapping, rather than separate controller-specific interpretations.
    const settings = options && typeof options === 'object' ? options : {};
    const statusLabels = settings.statusLabels && typeof settings.statusLabels === 'object' ? settings.statusLabels : {};
    const status = normalizeTaskSnapshotStatus(snapshot);
    const outputDir = firstNonEmpty(snapshot.lastTaskOutputDir, snapshot.currentTaskOutputDir);
    const logDir = cleanText(snapshot.logDir);
    const latestLogPath = firstNonEmpty(snapshot.latestLogPath, snapshot.sessionLogPath);
    const message =
      cleanText(snapshot.lastCrawlMessage) || '已恢复最近一次抓取记录，可从结果入口继续查看。';

    if (!outputDir && !logDir && !latestLogPath && status === 'idle') {
      return null;
    }

    return {
      status,
      message,
      outputDir,
      outputDirExists: Boolean(outputDir),
      logDir,
      logDirExists: Boolean(logDir),
      latestLogPath,
      latestLogExists: Boolean(latestLogPath),
      qualityStatus: status,
      qualityStatusText: status === 'completed' ? '已恢复最近结果' : statusLabels[status] || '最近记录',
      qualitySummaryLine: message
    };
  }

  function buildResultHistoryIdentity(panel) {
    // History identity is derived from stable artifact anchors so the renderer
    // can reconcile completed runs without depending on live in-memory state.
    const outputDir = cleanText(panel && panel.outputDir);
    const latestLogPath = cleanText(panel && panel.latestLogPath);
    const filmDataPath = cleanText(panel && panel.filmDataPath);
    const magnetPath = cleanText(panel && panel.magnetPath);
    const reportPath = cleanText(panel && panel.reportPath);
    const logDir = cleanText(panel && panel.logDir);

    if (!outputDir && !latestLogPath && !filmDataPath && !magnetPath && !reportPath) {
      return null;
    }

    const stableAnchor = firstNonEmpty(outputDir, filmDataPath, latestLogPath, logDir, magnetPath, reportPath);
    const titleSource = firstNonEmpty(outputDir, filmDataPath, latestLogPath, logDir);

    return {
      historyKey: stableAnchor,
      title: pathLeaf(titleSource) || '最近结果',
      outputDir,
      latestLogPath,
      filmDataPath,
      magnetPath,
      reportPath,
      logDir
    };
  }

  function buildRunQualitySummaryRequest(state, fallbackOutputDir) {
    // The quality-summary request intentionally carries only stable lookup data.
    // Summary generation remains a Go-side concern.
    const outputDir = resolveOutputDir(state, fallbackOutputDir);
    const message = cleanText(state && state.message);
    return {
      outputDir,
      signature: `${outputDir}|${message}`
    };
  }

  function buildQualitySummaryEventSignature(summary) {
    // Signatures stay small and deterministic so controllers can cheaply detect
    // when a new summary event is meaningfully different from the last one.
    const outputDir = cleanText(summary && summary.outputDir);
    const reportPath = cleanText(summary && summary.reportPath);
    const status = cleanText(summary && summary.status);
    return `${outputDir}|${reportPath}|${status}`;
  }

  function buildReviewPanelFallbackFromState(state) {
    if (!state) {
      return null;
    }

    // Review fallback is intentionally tolerant because it bridges multiple
    // historical state shapes into one stable panel contract for the renderer.
    return {
      status: state.status,
      message: state.message,
      duplicateItems: state.duplicateItems || [],
      duplicateItemsTotal:
        state.duplicateItemsTotal ?? (Array.isArray(state.duplicateItems) ? state.duplicateItems.length : 0),
      unfinishedItems: state.unfinishedItems || state.missingItems || [],
      unfinishedItemsTotal:
        state.unfinishedItemsTotal ??
        state.missingItemsTotal ??
        (Array.isArray(state.unfinishedItems) ? state.unfinishedItems.length : 0),
      pageGapItems: state.pageGapItems || [],
      filteredItems: state.filteredItems || state.filteredItemIds || (state.stats && state.stats.filteredItemIds) || [],
      filteredItemsTotal:
        state.filteredItemsTotal ??
        state.filteredByActressCount ??
        (state.stats && state.stats.filteredItemsTotal) ??
        (state.stats && state.stats.filteredByActressCount) ??
        0,
      completedItems:
        state.completedItems || state.completedItemIds || (state.stats && state.stats.completedItemIds) || [],
      completedItemsTotal:
        state.completedItemsTotal ??
        state.completedItems ??
        (state.stats && state.stats.completedItemsTotal) ??
        (state.stats && state.stats.completedItems) ??
        0,
      failedDetails: state.failedDetails || [],
      failedDetailsTotal:
        state.failedDetailsTotal ?? (Array.isArray(state.failedDetails) ? state.failedDetails.length : 0)
    };
  }

  function resolveQualitySummaryLevel(summary) {
    const explicitLevel = cleanText(summary && summary.noticeLevel);
    if (explicitLevel) {
      return explicitLevel;
    }

    const status = cleanText(summary && summary.status).toLowerCase();
    if (status === 'error') {
      return 'error';
    }
    if (status === 'warning') {
      return 'warn';
    }
    return 'info';
  }

  function buildQualitySuggestionLines(summary) {
    if (summary && Array.isArray(summary.topSuggestionLines) && summary.topSuggestionLines.length > 0) {
      return summary.topSuggestionLines.map((line, index) => ({
        line,
        level:
          summary && Array.isArray(summary.issues) && summary.issues[index] && summary.issues[index].level === 'error'
            ? 'error'
            : summary && Array.isArray(summary.issues) && summary.issues[index] && summary.issues[index].level === 'warning'
              ? 'warn'
              : 'info'
      }));
    }

    if (summary && Array.isArray(summary.issues)) {
      return summary.issues
        .slice(0, 3)
        .map((issue) => ({
          line: issue && issue.message,
          level: issue && issue.level === 'error' ? 'error' : issue && issue.level === 'warning' ? 'warn' : 'info'
        }))
        .filter((item) => cleanText(item.line));
    }

    return [];
  }

  globalScope.desktopCrawlPanelModel = {
    normalizeTaskSnapshotStatus,
    resolveOutputDir,
    buildStagePanelFromTaskSnapshot,
    buildResultPanelFromTaskSnapshot,
    buildResultHistoryIdentity,
    buildRunQualitySummaryRequest,
    buildQualitySummaryEventSignature,
    buildReviewPanelFallbackFromState,
    resolveQualitySummaryLevel,
    buildQualitySuggestionLines
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
