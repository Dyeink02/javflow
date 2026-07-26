// Shared sidecar compatibility service registry.
// compatibility-owner: active crawl-compatible sidecar registry; marker=compat-sidecar-service-registry
// Keep sidecar service construction centralized here so command routing stays
// focused on protocol dispatch instead of mixing lazy-instantiation logic for
// crawl and the remaining archived compatibility domains in one file.
//
// Ownership summary:
// 1) lazily construct sidecar compatibility services by domain
// 2) keep sidecar service lifetime/instantiation policy centralized
// 3) avoid pushing product behavior into the registry itself
// 4) make it obvious which sidecar domains are still active vs archived-only
//
// File map for maintainers:
// 1) archived-domain enable gate
// 2) lazy service/facade constructors
// 3) exported registry surface

const { createProxyValidationService } = require('../mainServices/proxyValidationService.js');

function isArchivedSidecarDomainEnabled() {
  const rawValue = String(process.env.JAV_ENABLE_ARCHIVED_SIDECAR_DOMAINS || '').trim().toLowerCase();
  return rawValue === '1' || rawValue === 'true' || rawValue === 'yes' || rawValue === 'on';
}

function createServiceRegistry({ fs, eventBus }) {
  // Keep the registry surface intentionally small. Each entry that survives
  // here is one more legacy JS service maintainers must reason about during
  // sidecar incidents, so new product logic should not be introduced through
  // this registry.
  //
  // Fast troubleshooting split:
  // 1) sidecar bootstrap/router issue -> start in `commandRouter.js`
  // 2) crawl compatibility launch/stop issue -> start in `services/crawlService.js`
  // 3) archived organizer/learning/ranking issue -> start in the matching
  //    facade below, only after confirming the archived-domain env gate is on
  const services = {
    crawlService: null,
    adLearningFacade: null,
    organizerFacade: null,
    proxyValidationService: null,
    rankingFacade: null
  };
  const archivedDomainsEnabled = isArchivedSidecarDomainEnabled();

  function assertArchivedDomainEnabled(domainLabel) {
    if (archivedDomainsEnabled) {
      return;
    }
    throw new Error(
      `${domainLabel} sidecar compatibility domain is archived and disabled by default. ` +
        `Use the Go/Wails path instead, or set JAV_ENABLE_ARCHIVED_SIDECAR_DOMAINS=1 for explicit compatibility testing.`
    );
  }

  function ensureProxyValidationService() {
    if (!services.proxyValidationService) {
      services.proxyValidationService = createProxyValidationService();
    }
    return services.proxyValidationService;
  }

  function ensureCrawlService() {
    if (!services.crawlService) {
      // Crawl is the only actively exercised sidecar domain in current
      // production, so keep its construction explicit and isolated here.
      const { createCrawlService } = require('./services/crawlService.js');
      services.crawlService = createCrawlService({ fs, eventBus });
    }
    return services.crawlService;
  }

  function ensureAdLearningFacade() {
    assertArchivedDomainEnabled('learning');
    if (!services.adLearningFacade) {
      // Ad-learning remains compatibility-only in this registry. Lazy loading
      // avoids paying for organizer-related legacy modules in crawl-only runs.
      const { createAdLearningFacade } = require('./services/adLearningFacade.js');
      services.adLearningFacade = createAdLearningFacade({ fs, eventBus });
    }
    return services.adLearningFacade;
  }

  function ensureOrganizerFacade() {
    assertArchivedDomainEnabled('organizer');
    if (!services.organizerFacade) {
      // Organizer stays behind a facade in the sidecar so current Wails-side
      // organizer ownership does not leak back into command wiring here.
      const { createOrganizerFacade } = require('./services/organizerFacade.js');
      services.organizerFacade = createOrganizerFacade({
        fs,
        eventBus,
        adLearningFacade: ensureAdLearningFacade()
      });
    }
    return services.organizerFacade;
  }

  function ensureRankingFacade() {
    assertArchivedDomainEnabled('ranking');
    if (!services.rankingFacade) {
      // Ranking stays separate from crawl construction so target lookup/cache
      // fallbacks do not become implicit dependencies of the crawl lane.
      const { createRankingFacade } = require('./services/rankingFacade.js');
      services.rankingFacade = createRankingFacade();
    }
    return services.rankingFacade;
  }

  return {
    ensureProxyValidationService,
    ensureCrawlService,
    ensureAdLearningFacade,
    ensureOrganizerFacade,
    ensureRankingFacade,
    archivedDomainsEnabled,
    getServices() {
      return services;
    }
  };
}

module.exports = {
  createServiceRegistry
};
