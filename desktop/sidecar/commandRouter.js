// Node sidecar command router used by the Wails compatibility path.
// compatibility-owner: active crawl-compatible sidecar router; marker=compat-sidecar-command-router
// Keep this file thin: it should route protocol commands to services and
// shared legacy modules, not accumulate domain logic.
//
// Current maintenance rule:
// - the sidecar exists primarily for crawler compatibility work
// - organizer / learning / ranking branches here are archived compatibility lanes
// - those archived lanes are disabled by default unless an explicit env flag enables them
// - do not treat this router as the preferred entrypoint for current
//   organizer/subscription product behavior
//
// Ownership summary:
// 1) route protocol commands to the correct sidecar compatibility service
// 2) keep bootstrap/liveness/shutdown handling centralized
// 3) avoid embedding product-domain behavior directly in the router
//
// File map for maintainers:
// 1) packet normalization + action dispatcher helper
// 2) system/crawl active domain handlers
// 3) archived organizer/learning/ranking handlers
// 4) final domain map + packet dispatch

const { initializeRuntimeContext, getRuntimeContext } = require('./runtimePaths.js');
const { createServiceRegistry } = require('./serviceRegistry.js');

function normalizePacket(packet) {
  // Packet normalization is centralized here so domain handlers can stay
  // focused on sidecar verbs instead of repeating transport guards.
  return {
    domain: String(packet.domain || '').trim(),
    action: String(packet.action || '').trim(),
    payload: packet.payload && typeof packet.payload === 'object' ? packet.payload : {}
  };
}

function createActionDispatcher(handlersByAction, unsupportedMessageBuilder) {
  return async function dispatchAction(action, payload = {}) {
    const handler = handlersByAction.get(action);
    if (!handler) {
      throw new Error(unsupportedMessageBuilder(action));
    }
    return handler(payload);
  };
}

function createCommandRouter({ fs, eventBus }) {
  // Lazily create compatibility services by domain so sidecar bootstrap for
  // crawler work does not automatically instantiate organizer/ad-learning
  // legacy lanes or even load those modules unless a matching domain is used.
  const serviceRegistry = createServiceRegistry({ fs, eventBus });

  const handleSystem = createActionDispatcher(
    new Map([
      ['bootstrap', async (payload = {}) => {
        // `system` owns process/bootstrap concerns only:
        // 1) initialize runtime paths
        // 2) expose liveness/diagnostic metadata
        // 3) stop already-created crawl service instances during shutdown
        // Product-domain behavior should stay out of this branch.
        const runtimeContext = initializeRuntimeContext(payload.runtimeContext || {});
        const archivedDomainsEnabled = serviceRegistry.archivedDomainsEnabled === true;
        eventBus.emitLifecycle('ready', 'Node sidecar ready');
        return {
          ready: true,
          pid: process.pid,
          runtimeContext,
          archivedDomainsEnabled
        };
      }],
      ['ping', async () => {
        return {
          ready: true,
          pid: process.pid,
          nodeVersion: process.version,
          runtimeContext: getRuntimeContext(),
          archivedDomainsEnabled: serviceRegistry.archivedDomainsEnabled === true
        };
      }],
      ['shutdown', async () => {
        const { crawlService } = serviceRegistry.getServices();
        if (crawlService) {
          await crawlService.shutdown().catch(() => {});
        }
        eventBus.emitLifecycle('stopped', 'Node sidecar stopped');
        return { ok: true };
      }],
      ['validate-proxy', async (payload = {}) => {
        return serviceRegistry.ensureProxyValidationService().validateProxy(payload.proxyValue, payload.options || {});
      }]
    ]),
    (action) => `Unsupported system action: ${action}`
  );

  // Crawl is the only actively maintained sidecar domain in day-to-day
  // production use. When Cloudflare/compat mode is involved, start/restart/
  // stop should be traced from here into `services/crawlService.js`.
  const handleCrawl = createActionDispatcher(
    new Map([
      ['start', async (payload = {}) => serviceRegistry.ensureCrawlService().start(payload)],
      ['restart', async (payload = {}) => serviceRegistry.ensureCrawlService().restart(payload)],
      ['stop', async () => serviceRegistry.ensureCrawlService().stop()],
      ['fetch-index-page', async (payload = {}) => serviceRegistry.ensureCrawlService().fetchIndexPage(payload)]
    ]),
    (action) => `Unsupported crawl action: ${action}`
  );

  // Organizer remains an archived compatibility/read-model domain here.
  // It is disabled by default; route only into the facade when an explicit
  // compatibility test enables archived sidecar domains.
  const handleOrganizer = createActionDispatcher(
    new Map([
      ['load-codes', async (payload = {}) => serviceRegistry.ensureOrganizerFacade().loadCrawlFilmCodes(payload)],
      ['run', async (payload = {}) => serviceRegistry.ensureOrganizerFacade().runOrganizer(payload)],
      ['resolve-path', async (payload = {}) => ({
        targetPath: serviceRegistry.ensureOrganizerFacade().resolveTargetPath(payload.rootPath, payload.kind)
      })]
    ]),
    (action) => `Unsupported organizer action: ${action}`
  );

  // Ad-learning is likewise an archived compatibility lane and is disabled by
  // default. Keep router ownership at verb dispatch only so future retirement
  // is a file-boundary change.
  const handleLearning = createActionDispatcher(
    new Map([
      ['get-summary', async () => serviceRegistry.ensureAdLearningFacade().getSummary()],
      ['update-model', async (payload = {}) => serviceRegistry.ensureAdLearningFacade().updateModel(payload)],
      ['import-samples', async (payload = {}) => serviceRegistry.ensureAdLearningFacade().importSamples(payload)],
      ['learn-by-codes', async (payload = {}) => serviceRegistry.ensureAdLearningFacade().learnSamplesByCodes(payload)],
      ['evaluate-video-risk', async (payload = {}) => serviceRegistry.ensureAdLearningFacade().evaluateVideoRisk(payload)]
    ]),
    (action) => `Unsupported learning action: ${action}`
  );

  // Ranking/target resolution is retained only as an archived sidecar utility
  // domain. The active Wails app should prefer the Go lookup bridge.
  const handleRanking = createActionDispatcher(
    new Map([
      ['get-rankings', async (payload = {}) => serviceRegistry.ensureRankingFacade().getRankings(payload)],
      ['resolve-target', async (payload = {}) => serviceRegistry.ensureRankingFacade().resolveTarget(payload)],
      ['inspect-target', async (payload = {}) => serviceRegistry.ensureRankingFacade().inspectTarget(payload)]
    ]),
    (action) => `Unsupported ranking action: ${action}`
  );

  // Domain routing is data-driven on purpose: crawl stays active, while the
  // other domains are explicit archived compatibility branches. Keeping the map
  // explicit makes it easier to see which branches are still alive without
  // re-reading a growing if/else chain.
  const domainHandlers = new Map([
    ['system', handleSystem],
    ['crawl', handleCrawl],
    ['organizer', handleOrganizer],
    ['learning', handleLearning],
    ['ranking', handleRanking]
  ]);

  async function handle(packet) {
    // Protocol normalization belongs here; domain logic does not. This keeps
    // future sidecar retirement tractable because all packet-to-domain mapping
    // stays in one place.
    const { domain, action, payload } = normalizePacket(packet || {});
    const domainHandler = domainHandlers.get(domain);
    if (domainHandler) {
      return domainHandler(action, payload);
    }

    // Fail closed on unknown domains so sidecar compatibility does not become
    // a silent fallback for future product features or typoed bridge packets.
    throw new Error(`Unsupported sidecar domain: ${domain}`);
  }

  return {
    handle
  };
}

module.exports = {
  createCommandRouter
};
