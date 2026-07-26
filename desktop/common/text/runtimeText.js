// Active runtime text source for log/state wording shared by renderer and
// compatibility helpers. Keep larger static page copy in uiTextSource.js.
//
// Ownership summary:
// 1) define shared runtime/log/status wording used across desktop surfaces
// 2) keep runtime phrasing separate from larger page-copy text sources
// 3) provide one source for log-level/prefix label mapping
//
// File map for maintainers:
// 1) runtime/log wording registry
// 2) log-level and prefix label maps
// 3) noisy-vs-key log filter keyword sets
(function registerDesktopRuntimeText(globalScope) {
  // Runtime text is reserved for log/runtime phrasing shared by renderer and
  // compatibility helpers. Keep larger page/UI copy in `uiTextSource.js`.
  const MAIN_TEXT = {
    taskLogTitleSuffix: '任务日志',
    taskLogCreatedPrefix: '本次运行日志已创建：',
    reminderFallback: '暂无提醒信息',
    magnetFileMissingPrefix: '未找到磁力链接文档：',
    continueRecovery: '正在继续补爬未完成内容...',
    runnerBusy: '当前已有抓取任务正在运行',
    restartFailedPrefix: '重新爬取失败：',
    versionLabel: '版本',
    startTimeLabel: '开始时间',
    outputLabel: '输出目录',
    baseLabel: '起始地址',
    runtimeSchemeLabel: '运行方案',
    keyLogPrefix: '[重点] ',
    demoTitleSeparator: ' · ',
    stateLogPrefix: '状态',
    logLevelLabels: {
      info: '信息',
      warn: '警告',
      error: '错误',
      debug: '调试'
    },
    logPrefixLabels: {
      'QueueManager:': '队列管理：',
      'FileHandler:': '文件处理：',
      'RequestHandler:': '请求处理：',
      'Parser:': '解析器：',
      'fetchMagnet:': '磁力抓取：',
      'parseMetadata:': '元数据解析：',
      'getPage:': '页面抓取：',
      'executeAjax:': 'AJAX 请求：',
      'executeAjaxWithCloudflare:': 'Cloudflare AJAX：',
      'CloudflareBypass:': 'Cloudflare 绕过：',
      'CloudflareAjaxWorkerClient:': 'Cloudflare Worker：',
      'PuppeteerPool:': '浏览器池：',
      'ResourceMonitor:': '资源监控：',
      'handleGenericError:': '异常处理：',
      'ResultValidator:': '结果校验：'
    }
  };

  // Keep runtime wording centralized here when it is shared by renderer and
  // compatibility helpers. UI-page copy and structural panel labels belong in
  // `uiTextSource.js`, not in this runtime/log wording registry.
  const LOG_FILTER_PATTERNS = {
    noisy: ['QueueManager: [索引页] 任务开始', 'QueueManager: [详情页] 任务开始', 'ResourceMonitor:'],
    key: [
      '抓取任务完成',
      '抓取任务已完成',
      '任务未完成',
      '结果二次校验完成',
      '补爬结束',
      '分页缺口',
      '失败详情页',
      'Cloudflare',
      '已发送终止指令',
      '重新爬取'
    ]
  };

  const payload = {
    MAIN_TEXT,
    LOG_FILTER_PATTERNS
  };

  const registry = (globalScope.__desktopTextModules = globalScope.__desktopTextModules || {});
  Object.assign(registry, payload);

  if (typeof module !== 'undefined' && module.exports) {
    module.exports = payload;
  }
})(typeof globalThis !== 'undefined' ? globalThis : window);
