// Shared text registry adapter for the bundled browser runtime and Node-side
// compatibility helpers. Browser bundles populate `__desktopTextModules`
// first; Node fallback resolves the same source modules lazily by file path.
//
// appText.js is the shared text aggregator, not an independent wording source.
// When a desktop label changes, update the underlying text modules first and
// keep this file as the adapter that exposes them to browser and Node callers.
//
// Ownership summary:
// 1) aggregate shared text modules into one normalized runtime registry
// 2) adapt browser-bundled and Node-side text loading behind one surface
// 3) keep wording sources in dedicated text modules rather than here
//
// File map for maintainers:
// 1) browser-vs-Node text module resolution
// 2) normalized shared text registry assembly
// 3) flat export surface for active runtime callers
(function initializeDesktopAppText(globalScope) {
  const registry = (globalScope.__desktopTextModules = globalScope.__desktopTextModules || {});
  const isNode = typeof module !== 'undefined' && module.exports;
  const loadNodeModule = (relativePath) => (isNode ? require(relativePath) : {});
  const resolveTextModule = (requiredKey, relativePath) =>
    registry[requiredKey] ? registry : loadNodeModule(relativePath);

  const appInfoModule = resolveTextModule('APP_INFO', './text/appInfo.js');
  const taskConfigModule = resolveTextModule('STATUS_LABELS', './text/taskConfig.js');
  const versionHistoryModule = resolveTextModule('VERSION_HISTORY', './text/versionHistory.js');
  const uiTextModule = resolveTextModule('UI_TEXT_SOURCE', './text/uiTextSource.js');
  const runtimeTextModule = resolveTextModule('MAIN_TEXT', './text/runtimeText.js');

  // Keep the export surface intentionally flat. Callers across browser bundle,
  // sidecar, and archived helpers should read one normalized registry instead
  // of each importing raw text modules with their own assumptions.
  const api = {
    APP_INFO: appInfoModule.APP_INFO || {},
    FILE_NAMES: appInfoModule.FILE_NAMES || {},
    STATUS_LABELS: taskConfigModule.STATUS_LABELS || {},
    FAILURE_CATEGORY_LABELS: taskConfigModule.FAILURE_CATEGORY_LABELS || {},
    TASK_TEMPLATES: taskConfigModule.TASK_TEMPLATES || {},
    URL_SUGGESTIONS: appInfoModule.URL_SUGGESTIONS || [],
    VERSION_HISTORY: versionHistoryModule.VERSION_HISTORY || [],
    UI_TEXT_SOURCE: uiTextModule.UI_TEXT_SOURCE || {},
    MAIN_TEXT: runtimeTextModule.MAIN_TEXT || {},
    LOG_FILTER_PATTERNS: runtimeTextModule.LOG_FILTER_PATTERNS || {}
  };

  // Export one normalized text registry so browser bundle, Wails bridge, and
  // remaining compatibility helpers do not each invent their own text-module
  // wiring rules.
  globalScope.desktopAppText = api;

  if (isNode) {
    module.exports = api;
  }
})(typeof globalThis !== 'undefined' ? globalThis : window);
