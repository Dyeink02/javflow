// Bridges legacy/sidecar runner logs into the shared renderer-facing log
// stream. This service should reuse the event bus helpers so protocol event
// names stay defined in one place.
// compatibility-owner: active crawl-compatible log adapter; marker=compat-sidecar-task-log-service
//
// Maintenance rule:
// - log file naming and summary rules should stay aligned with the shared
//   logBridge implementation
// - if current Wails logs are wrong but sidecar is not involved, debug the Go
//   bridge/log path before touching this compatibility adapter
//
// Ownership summary:
// 1) adapt sidecar crawl logs onto the shared renderer-facing log stream
// 2) reuse shared logBridge naming/buffering behavior in the compatibility lane
// 3) keep sidecar log adaptation separate from protocol and crawl business logic
//
// File map for maintainers:
// 1) sidecar log payload emit helpers
// 2) shared logBridge bootstrap
// 3) exported task-log compatibility surface
const path = require('path');

const { createLogBridge } = require('../../mainServices/logBridge.js');
const { APP_INFO, FILE_NAMES, MAIN_TEXT, LOG_FILTER_PATTERNS, STATUS_LABELS } = require('../../common/appText.js');
const runtimePackage = require('../../../package.json');

function createTaskLogService({ fs, eventBus }) {
  const appTitle = runtimePackage.productDisplayName || APP_INFO.title;
  const appVersion = APP_INFO.version || runtimePackage.productDisplayVersion || runtimePackage.version;
  const appDemoLabel = runtimePackage.demoLabel || '';

  // Fast troubleshooting split:
  // 1) sidecar mode UI log/event shape issue -> inspect this adapter first
  // 2) compatibility task/session file naming or UTF-8 issue -> inspect the
  //    shared `desktop/mainServices/logBridge.js` implementation next
  // 3) Go-native main-path logging issue without sidecar -> do not start here

  // The sidecar emits one logical crawl-log event at a time. Keep that shape
  // stable so renderer consumers do not care whether logs came from Go or the
  // legacy Node compatibility path.
  function emitCrawlLogPayload(payload) {
    const entries = Array.isArray(payload) ? payload : [payload];
    entries.forEach((entry) => {
      if (!entry || typeof entry !== 'object') {
        return;
      }
      eventBus.emitLog('crawl', entry);
    });
  }

  // Reuse the shared logBridge so file naming, buffering, and summary rules are
  // not reimplemented separately in the sidecar compatibility lane.
  const logBridge = createLogBridge({
    fs,
    path,
    sendToRenderer(channel, payload) {
      if (channel === 'runner:log') {
        // `logBridge` flushes renderer logs in batches. Forward each entry so
        // the sidecar compatibility path preserves the same per-log event shape
        // as the Go-native bridge instead of collapsing a whole batch into one
        // malformed event object.
        emitCrawlLogPayload(payload);
        return;
      }

      if (channel === 'runner:state') {
        eventBus.emitState('crawl', payload);
        return;
      }

      if (channel === 'runner:log-context') {
        eventBus.emitLogContext(payload);
      }
    },
    mainText: MAIN_TEXT,
    statusLabels: STATUS_LABELS,
    logFilterPatterns: LOG_FILTER_PATTERNS,
    fileNames: FILE_NAMES,
    appTitle,
    appVersion,
    appDemoLabel
  });

  // Expose only the shared logBridge surface the sidecar runtime actually
  // needs. Higher-level lifecycle ownership stays in crawlService/runtimeFactory.
  return {
    getLogBridge: () => logBridge,
    getLogContext: () => logBridge.getLogContext(),
    flush: () => logBridge.flushDesktopPipelines()
  };
}

module.exports = {
  createTaskLogService
};
