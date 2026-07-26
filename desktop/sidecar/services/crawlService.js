// Compatibility crawl service for the Node sidecar.
// compatibility-owner: active crawl-compatible service; marker=compat-sidecar-crawl-service
// This module bridges the sidecar protocol to shared legacy desktop services
// and, when enabled, the JS goRunnerHost wrapper.
//
// Ownership summary:
// 1) route sidecar crawl start/restart/stop/shutdown calls
// 2) keep Go-owned and legacy-JS-owned crawl runtime branches explicit
// 3) keep sidecar diagnostic logging close to compatibility launch decisions
//
// Maintenance boundary:
// - current Wails crawl orchestration lives in Go
// - this file exists for sidecar compatibility and Cloudflare-era fallback
// - avoid adding organizer/subscription behavior here
// - keep runtime ownership comments close to launch/stop paths so crawler bugs
//   are easier to classify into Go main path vs JS compatibility path
//
// Compatibility-lane rule:
// if a current product bug reproduces only through this file, fix it here
// narrowly instead of broadening sidecar ownership back into the primary Go
// runtime.
//
// File map for maintainers:
// 1) runtime factory bootstrap
// 2) start/restart ownership split
// 3) stop/shutdown compatibility cleanup

const { createCrawlRuntimeFactory } = require('./crawlRuntimeFactory.js');
const parserModule = require('../../../dist/core/parser.js');
const requestHandlerModule = require('../../../dist/core/requestHandler.js');
const Parser = parserModule.default || parserModule;
const RequestHandler = requestHandlerModule.default || requestHandlerModule;

function createCrawlService({ fs, eventBus }) {
  const runtimeFactory = createCrawlRuntimeFactory({ fs, eventBus });

  function queueSidecarDiagnosticLog(logBridge, message, timestamp = new Date().toISOString()) {
    if (!logBridge) {
      return;
    }

    logBridge.queueRendererLog('info', message, timestamp);
  }

  function ensureRuntime() {
    return runtimeFactory.ensureRuntime();
  }

  function formatDiagnosticSettings(settings) {
    const safeSettings = settings || {};
    return `cloudflare=${Boolean(safeSettings.cloudflare)} goTaskController=${Boolean(safeSettings.goTaskController)} actressCountFilterThreshold=${Number(safeSettings.actressCountFilterThreshold ?? 0)} output=${String(safeSettings.output || '')}`;
  }

  async function start(settings) {
    const context = ensureRuntime();
    const safeSettings = settings || {};
    // Branch rule:
    // - `goTaskController=true` means Go owns task lifecycle/state and the
    //   sidecar is only launching a compatibility runner host
    // - otherwise the older JS runner service still owns task lifecycle
    if (safeSettings.goTaskController) {
      queueSidecarDiagnosticLog(
        context.logBridge,
        `sidecar start settings: ${formatDiagnosticSettings(safeSettings)}`
      );
      return context.goRunnerHost.start(safeSettings);
    }
    queueSidecarDiagnosticLog(
      context.logBridge,
      `legacy sidecar start settings: ${formatDiagnosticSettings(safeSettings)}`
    );
    return context.ensureLegacyRuntime().runnerService.startRunner(safeSettings);
  }

  async function restart(settings) {
    const context = ensureRuntime();
    const safeSettings = settings || {};
    // Restart mirrors the same ownership split as start so debugging can stay
    // symmetric across launch/restart incidents.
    if (safeSettings.goTaskController) {
      return context.goRunnerHost.restart(safeSettings);
    }
    return context.ensureLegacyRuntime().runnerService.restartRunner(safeSettings);
  }

  async function stop() {
    const context = ensureRuntime();
    // Stop checks the Go-owned compatibility host first because current product
    // traffic prefers that branch whenever `goTaskController` is enabled.
    if (context.goRunnerHost.isRunning()) {
      return context.goRunnerHost.stop();
    }

    const legacyRuntime = context.getLegacyRuntime();
    if (legacyRuntime?.runnerService) {
      return legacyRuntime.runnerService.stopRunner();
    }

    return { ok: true };
  }

  async function shutdown() {
    const context = ensureRuntime();
    // Shutdown is intentionally defensive: both possible runtime branches are
    // asked to stop, then logs are flushed once at the sidecar boundary.
    await context.goRunnerHost.shutdown().catch(() => {});
    const legacyRuntime = context.getLegacyRuntime();
    if (legacyRuntime?.runnerService) {
      await legacyRuntime.runnerService.stopRunner({ preserveRestart: false }).catch(() => {});
    }
    await context.taskLogService.flush();
    return { ok: true };
  }

  async function fetchIndexPage(payload = {}) {
    const pageUrl = String(payload.pageUrl || payload.url || '').trim();
    if (!pageUrl) {
      throw new Error('pageUrl is required');
    }

    const requestHandler = new RequestHandler({
      retryCount: Number(payload.retryCount || 3),
      retryDelay: Number(payload.retryDelay || 1000),
      BASE_URL: String(payload.baseUrl || payload.base || pageUrl),
      baseUrl: String(payload.baseUrl || payload.base || pageUrl),
      searchUrl: '',
      parallel: Math.max(1, Number(payload.parallel || 2)),
      headers: {
        Referer: String(payload.referer || payload.base || pageUrl),
        Cookie: String(payload.cookie || 'existmag=mag; age=verified; dv=1; age_verified=1; adult_verified=1; age_verification=1; age_verification_passed=true; is_adult=true; javbus_age=1')
      },
      output: String(payload.output || process.cwd()),
      search: null,
      base: String(payload.base || payload.baseUrl || pageUrl),
      nomag: false,
      allmag: false,
      nopic: true,
      timeout: Math.max(10000, Number(payload.timeout || 30000)),
      limit: 0,
      totalPages: 0,
      itemsPerPage: 30,
      delay: Number(payload.delay || 2),
      strictSSL: payload.strictSSL !== false,
      proxy: String(payload.proxy || '').trim() || undefined,
      useCloudflareBypass: Boolean(payload.cloudflare),
      secondValidation: Boolean(payload.secondValidation),
      taskTemplate: 'balanced',
      magnetExcludeKeywords: '',
      magnetContentValidation: false,
      supplementMagnetTopN: 3,
      actressCountFilterThreshold: 0,
      demoMode: 'base',
      demoLabel: '',
      productDisplayName: 'JavFlow',
      puppeteerPool: {
        maxSize: 1,
        maxIdleTime: 3 * 60 * 1000,
        healthCheckInterval: 30 * 1000,
        requestTimeout: Math.max(30000, Number(payload.timeout || 30000) + 15000),
        retryAttempts: 3
      }
    });

    try {
      const response = await requestHandler.getPage(pageUrl);
      const body = response && response.body ? String(response.body) : '';
      const links = Parser.parsePageLinks(body);
      return {
        ok: true,
        pageUrl,
        statusCode: response && response.statusCode ? response.statusCode : 200,
        links,
        linkCount: links.length,
        bodyLength: body.length
      };
    } finally {
      await requestHandler.close().catch(() => {});
    }
  }

  return {
    start,
    restart,
    stop,
    shutdown,
    fetchIndexPage
  };
}

module.exports = {
  createCrawlService
};
