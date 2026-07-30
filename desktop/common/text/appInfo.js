// Shared app identity text for the active desktop frontend/runtime bundle.
// Product title, subtitle, default output names, and log-name prefixes should
// stay owned here so renderer fallback text and build artifacts do not drift.
//
// Ownership summary:
// 1) define shared app identity text and default output naming
// 2) keep renderer/runtime bundles on one app-info source
// 3) centralize display-oriented app metadata away from controllers
//
// File map for maintainers:
// 1) product title/version/base-url defaults
// 2) shared output/log filename defaults
// 3) exported app-info payload registry
(function registerDesktopAppInfo(globalScope) {
  const APP_VERSION = '0.4.1';
  const APP_TITLE = 'JavFlow';
  const DEFAULT_BASE_URL = 'https://www.javbus.com';

  const APP_INFO = {
    title: APP_TITLE,
    version: APP_VERSION,
    subtitle: '基于开源项目：raawaa',
    eyebrow: 'Windows EXE',
    defaultBaseUrl: DEFAULT_BASE_URL,
    outputFolderName: `${APP_TITLE}输出`
  };

  const FILE_NAMES = {
    magnetFilename: 'magnet-links.txt',
    latestLogFilename: 'latest-log.txt',
    taskLogPrefix: '运行日志'
  };

  const URL_SUGGESTIONS = [
    'https://www.javbus.com/',
    'https://www.busjav.cyou',
    'https://www.fanbus.bond',
    'https://www.cdnbus.bond'
  ];

  const payload = {
    APP_INFO,
    FILE_NAMES,
    URL_SUGGESTIONS
  };

  const registry = (globalScope.__desktopTextModules = globalScope.__desktopTextModules || {});
  Object.assign(registry, payload);

  if (typeof module !== 'undefined' && module.exports) {
    module.exports = payload;
  }
})(typeof globalThis !== 'undefined' ? globalThis : window);
