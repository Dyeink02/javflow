// Log controller is the renderer-side projection of backend logs.
// It keeps UI updates cheap by batching, truncating, and filtering visible
// messages without changing the underlying on-disk run log.
//
// Ownership summary:
// 1) batch visible renderer log lines for cheap UI updates
// 2) enforce truncation/filtering/display-level policy
// 3) keep on-screen log rendering separate from on-disk log persistence
//
// Boundary rule:
// - display policy lives here
// - file creation/flush policy lives in backend log writers/bridges
//
// File map for maintainers:
// 1) visible-message shaping and filter policy
// 2) renderer log batch scheduling/flush helpers
// 3) log-context/path UI projection helpers
(function initializeLogController(globalScope) {
  const rendererHelpers = globalScope.desktopRendererHelpers || {};
  const clearChildren = rendererHelpers.clearChildren;

  if (!clearChildren) {
    throw new Error('desktopRendererHelpers must be loaded before logController');
  }

  function createLogController(options) {
    const {
      logView,
      logFilePath,
      maxLines,
      defaultHint,
      logPathPrefix,
      truncatedSuffix = '...(truncated)',
      visibleLevels = ['info', 'warn', 'error'],
      maxVisibleLength = 240,
      hiddenKeywords = []
    } = options;

    let queuedLogs = [];
    let flushScheduled = false;
    let flushHandle = null;
    let flushHandleKind = '';
    let currentLogContextText = '';

    const visibleLevelSet = new Set(visibleLevels.map((level) => String(level).toLowerCase()));

    function normalizeVisibleMessage(message) {
      // Visible log shaping is a UI-only concern. It must not mutate the real
      // run log semantics or on-disk content.
      const singleLineMessage = String(message ?? '').replace(/\s+/g, ' ').trim();
      if (singleLineMessage.length <= maxVisibleLength) {
        return singleLineMessage;
      }

      return `${singleLineMessage.slice(0, maxVisibleLength)} ${truncatedSuffix}`;
    }

    function shouldDisplay(level, message) {
      const normalizedLevel = String(level || '').toLowerCase();
      if (!visibleLevelSet.has(normalizedLevel)) {
        return false;
      }

      return !hiddenKeywords.some((keyword) => String(message || '').includes(keyword));
    }

    function scheduleFlush() {
      // Batching keeps high-frequency crawl logs from forcing synchronous DOM
      // work on every single line.
      if (flushScheduled) {
        return;
      }

      flushScheduled = true;
      if (typeof globalScope.requestAnimationFrame === 'function') {
        flushHandleKind = 'raf';
        flushHandle = globalScope.requestAnimationFrame(flushLogs);
        return;
      }

      flushHandleKind = 'timeout';
      flushHandle = globalScope.setTimeout(flushLogs, 16);
    }

    function appendLog(level, message, timestamp) {
      if (!shouldDisplay(level, message)) {
        return;
      }

      queuedLogs.push({
        level: String(level || 'info').toLowerCase(),
        message: normalizeVisibleMessage(message),
        timestamp
      });

      scheduleFlush();
    }

    function flushLogs() {
      flushScheduled = false;
      flushHandle = null;
      flushHandleKind = '';

      if (queuedLogs.length === 0) {
        return;
      }

      const shouldStickToBottom =
        logView.scrollHeight - logView.scrollTop - logView.clientHeight <= Math.max(logView.clientHeight * 0.25, 48);

      const fragment = document.createDocumentFragment();
      const batch = queuedLogs;
      queuedLogs = [];

      batch.forEach((item) => {
        const line = document.createElement('div');
        const date = item.timestamp ? new Date(item.timestamp) : new Date();

        line.className = `log-line ${item.level}`;
        line.textContent = `[${date.toLocaleString('zh-CN', { hour12: false })}] ${item.message}`;
        fragment.appendChild(line);
      });

      logView.appendChild(fragment);

      while (logView.childElementCount > maxLines) {
        logView.removeChild(logView.firstElementChild);
      }

      if (shouldStickToBottom) {
        logView.scrollTop = logView.scrollHeight;
      }
    }

    function updateLogContext(context = {}) {
      // Log context only projects the current visible paths/hint. It should
      // not infer run state or create file-path business rules in the renderer.
      const nextText =
        context && context.sessionLogPath ? `${logPathPrefix}${context.sessionLogPath}` : defaultHint;

      if (nextText === currentLogContextText) {
        return;
      }

      currentLogContextText = nextText;
      logFilePath.textContent = nextText;
      logFilePath.title = nextText;
    }

    function clearLogView() {
      // Clearing the visible panel must not touch on-disk logs. This controller
      // only resets the renderer projection/buffer.
      queuedLogs = [];
      if (flushHandle != null) {
        if (flushHandleKind === 'raf' && typeof globalScope.cancelAnimationFrame === 'function') {
          globalScope.cancelAnimationFrame(flushHandle);
        } else {
          globalScope.clearTimeout(flushHandle);
        }
        flushHandle = null;
        flushHandleKind = '';
      }
      flushScheduled = false;
      clearChildren(logView);
    }

    return {
      appendLog,
      clearLogView,
      updateLogContext
    };
  }

  globalScope.desktopLogController = {
    createLogController
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
