// Primary frontend bridge for the current Wails desktop runtime.
// Renderer controllers should treat this file as the default command/event
// entrypoint; sidecar-specific behavior is a compatibility path, not the first
// place to debug ordinary Wails UI issues.
//
// Maintenance boundary:
// - command invocation and event subscription helpers live here
// - renderer controllers consume this bridge, but do not shape transport details
// - historical Electron/runtime quirks should stay behind compatibility helpers
//
// File map for maintainers:
// 1) Wails binding readiness and retry helpers
// 2) command invoke helpers and API wrappers
// 3) runtime event subscribe/unsubscribe helpers
(function registerWailsPlatformBridge(globalScope) {
  // Ownership summary:
  // 1) invoke Wails bridge commands with normalized payload conventions
  // 2) subscribe/unsubscribe runtime events for renderer controllers
  // 3) keep startup/retry transport concerns centralized and out of feature UI
  const bridgeProtocol = globalScope.desktopBridgeProtocol || null;
  const bridgeEvents = bridgeProtocol && bridgeProtocol.BRIDGE_EVENTS ? bridgeProtocol.BRIDGE_EVENTS : null;
  const RETRYABLE_COMMANDS = new Set([
    'app:get-crawl-run-context',
    'app:get-crawl-stage-panel',
    'app:get-crawl-result-panel',
    'app:get-crawl-task-snapshot',
    'app:get-log-context',
    'app:validate-proxy',
    'app:get-integration-context',
	'app:resolve-actress-crawl-output',
    'app:get-ad-learning-summary',
    'app:get-dependency-status',
    'app:get-actress-rankings',
    'app:load-crawl-film-codes',
    'app:discover-organizer-codes',
    'app:list-av-subscriptions',
    'app:scan-av-subscriptions-from-output',
    'app:add-av-subscription',
    'app:refresh-av-subscriptions',
    'app:remove-av-subscription',
    'app:clear-av-subscriptions',
    'app:mark-av-subscription-synced',
    'app:list-av-subscriptions-v2',
    'app:scan-av-subscriptions-v2-from-output',
    'app:add-av-subscription-v2-manual',
    'app:refresh-av-subscriptions-v2',
    'app:refresh-av-subscription-v2',
    'app:remove-av-subscription-v2',
    'app:clear-av-subscriptions-v2',
    'app:mark-av-subscription-v2-synced',
    'app:prepare-av-subscription-v2-crawl',
    'app:start-av-subscription-v2-crawl',
    'app:stop-av-subscription-v2-crawl',
    'app:av-subscription-v2-crawl-status',
    'app:update-ad-learning-model',
    'app:import-ad-learning-samples',
    'app:learn-ad-samples-by-codes',
	'app:install-dependency',
	'app:resolve-actress-alias',
	'app:remember-actress-alias',
	'app:resolve-actress-crawl-target',
    'app:run-organizer',
    'app:start-crawl',
    'app:restart-crawl',
    'app:stop-crawl',
    'app:update-antiblock'
  ]);

  function delay(ms) {
    return new Promise((resolve) => setTimeout(resolve, ms));
  }

  function resolveBridgeEvent(eventKey, fallbackEventName) {
    return bridgeEvents && bridgeEvents[eventKey] ? bridgeEvents[eventKey] : fallbackEventName;
  }

  function waitForWailsBinding(maxAttempts = 60, intervalMs = 150) {
    return new Promise((resolve, reject) => {
      let attempts = 0;

      function poll() {
        attempts += 1;
        const binding = globalScope.go && globalScope.go.main && globalScope.go.main.App;
        const runtime = globalScope.runtime;

        if (binding && typeof binding.Call === 'function' && runtime) {
          resolve({ binding, runtime });
          return;
        }

        if (attempts >= maxAttempts) {
          reject(new Error('Wails binding is not ready.'));
          return;
        }

        setTimeout(poll, intervalMs);
      }

      poll();
    });
  }

  function shouldRetryInvoke(command, error) {
    if (!RETRYABLE_COMMANDS.has(command)) {
      return false;
    }

    const message = error instanceof Error ? error.message : String(error || '');
    return /sidecar|startup|starting|deadline|timed out|eof|broken pipe/i.test(message);
  }

  async function invoke(command, payload) {
    const { binding } = await waitForWailsBinding();
    const normalizedCommand = String(command || '').trim();
    const normalizedPayload =
      payload && typeof payload === 'object' ? payload : payload == null ? {} : { value: payload };
    const maxAttempts = RETRYABLE_COMMANDS.has(normalizedCommand) ? 6 : 1;
    let lastError = null;

    // Retry is reserved for startup/bridge-readiness style queries and commands.
    // Business retries belong in the owning Go/feature service, not here, so a
    // transport retry does not quietly become a domain-level retry policy.
    for (let attempt = 0; attempt < maxAttempts; attempt += 1) {
      try {
        const rawResult = await binding.Call(normalizedCommand, normalizedPayload);

        if (typeof rawResult !== 'string' || !rawResult.trim()) {
          return null;
        }

        return JSON.parse(rawResult);
      } catch (error) {
        lastError = error;
        if (!shouldRetryInvoke(normalizedCommand, error) || attempt >= maxAttempts - 1) {
          throw error;
        }

        await delay(200 * (attempt + 1));
      }
    }

    throw lastError;
  }

  function subscribe(eventName, callback) {
    let disposed = false;
    let unsubscribe = null;

    // Subscription is intentionally lazy because renderer bootstrap may ask for
    // listeners before Wails runtime is fully ready. Keep the delayed attach
    // here instead of pushing readiness polling into each controller.
    void waitForWailsBinding()
      .then(({ runtime }) => {
        if (disposed || !runtime || typeof runtime.EventsOn !== 'function') {
          return;
        }

        unsubscribe = runtime.EventsOn(eventName, callback);
      })
      .catch(() => {});

    return () => {
      disposed = true;
      if (typeof unsubscribe === 'function') {
        unsubscribe();
      }
    };
  }

  function noPayloadCommand(command) {
    return () => invoke(command);
  }

  function optionsCommand(command) {
    return (options) => invoke(command, options);
  }

  function singleValueCommand(command, key) {
    return (value) => invoke(command, { [key]: value });
  }

  function twoValueCommand(command, firstKey, secondKey) {
    return (firstValue, secondValue) => invoke(command, { [firstKey]: firstValue, [secondKey]: secondValue });
  }

  function directEventSubscription(eventName) {
    return (callback) => subscribe(eventName, callback);
  }

  function bridgeEventSubscription(eventKey, fallbackEventName) {
    return (callback) => subscribe(resolveBridgeEvent(eventKey, fallbackEventName), callback);
  }

  function buildResolvedArtifactInput(value, type, sourceKind, sourceOrigin) {
    // Keep artifact-input identity explicit so downstream controllers know
    // whether they received a direct artifact file or only a crawl-output-dir
    // fallback.
    const normalizedSourceKind = String(sourceKind || '').trim();
    return {
      value: String(value || '').trim(),
      type: String(type || '').trim(),
      sourceKind: normalizedSourceKind,
      sourceOrigin: String(sourceOrigin || '').trim(),
      isFallbackOutputDir: normalizedSourceKind === 'crawlOutputDir'
    };
  }

  function emptyResolvedArtifactInput() {
    return buildResolvedArtifactInput('', '', '', '');
  }

  function resolveArtifactInputFromOutputDir(outputDir, sourceOrigin) {
    const normalizedOutputDir = String(outputDir || '').trim();
    if (!normalizedOutputDir) {
      return null;
    }

    return buildResolvedArtifactInput(
      normalizedOutputDir,
      'crawlOutputDir',
      'crawlOutputDir',
      sourceOrigin
    );
  }

  function resolveArtifactInputFromContext(runContext, artifactKey, artifactType) {
    const artifactPath = String(runContext && artifactKey ? runContext[artifactKey] : '').trim();
    if (artifactPath) {
      return buildResolvedArtifactInput(artifactPath, artifactType, 'artifact', 'runContextArtifact');
    }

    const preferredOutputDir = String(
      runContext && runContext.preferredOutputDir ? runContext.preferredOutputDir : ''
    ).trim();
    return resolveArtifactInputFromOutputDir(preferredOutputDir, 'runContextOutputDir');
  }

  function resolveArtifactInputFromResultPanel(resultPanel) {
    return resolveArtifactInputFromOutputDir(
      resultPanel && resultPanel.outputDir ? resultPanel.outputDir : '',
      'resultPanelOutputDir'
    );
  }

  function buildDesktopCommandApi() {
    return {
      getSettings: noPayloadCommand('app:get-settings'),
      getCrawlRunContext: noPayloadCommand('app:get-crawl-run-context'),
      getCrawlStagePanel: noPayloadCommand('app:get-crawl-stage-panel'),
      getCrawlResultPanel: noPayloadCommand('app:get-crawl-result-panel'),
      getCrawlReviewPanel: noPayloadCommand('app:get-crawl-review-panel'),
      getCrawlTaskSnapshot: noPayloadCommand('app:get-crawl-task-snapshot'),
      getLogContext: noPayloadCommand('app:get-log-context'),
      showAlert: optionsCommand('app:show-alert'),
      validateProxy: twoValueCommand('app:validate-proxy', 'proxyValue', 'options'),
	      saveActressAtlasProxy: optionsCommand('app:save-actress-atlas-proxy'),
	      saveWorkspacePreferences: optionsCommand('app:save-workspace-preferences'),
      listCrawlCacheSnapshots: noPayloadCommand('app:list-crawl-cache-snapshots'),
      removeCrawlCacheSnapshot: singleValueCommand('app:remove-crawl-cache-snapshot', 'cacheKey'),
      clearCrawlCacheSnapshots: noPayloadCommand('app:clear-crawl-cache-snapshots'),
      chooseOutput: noPayloadCommand('app:choose-output'),
      chooseBackgroundImage: noPayloadCommand('app:choose-background-image'),
      clearBackgroundImage: noPayloadCommand('app:clear-background-image'),
      chooseOrganizerRoot: noPayloadCommand('app:choose-organizer-root'),
      chooseOrganizerCrawlFile: noPayloadCommand('app:choose-organizer-crawl-file'),
      chooseLearningSamples: noPayloadCommand('app:choose-learning-samples'),
      getIntegrationContext: noPayloadCommand('app:get-integration-context'),
	      resolveActressCrawlOutput: optionsCommand('app:resolve-actress-crawl-output'),
      getRunQualitySummary: optionsCommand('app:get-run-quality-summary'),
      getAdLearningSummary: noPayloadCommand('app:get-ad-learning-summary'),
      getDependencyStatus: noPayloadCommand('app:get-dependency-status'),
      installDependency: twoValueCommand('app:install-dependency', 'name', 'downloadUrl'),
      uninstallDependency: singleValueCommand('app:uninstall-dependency', 'name'),
      updateAdLearningModel: optionsCommand('app:update-ad-learning-model'),
      importAdLearningSamples: optionsCommand('app:import-ad-learning-samples'),
      learnAdSamplesByCodes: optionsCommand('app:learn-ad-samples-by-codes'),
      loadCrawlFilmCodes: optionsCommand('app:load-crawl-film-codes'),
      discoverOrganizerCodes: optionsCommand('app:discover-organizer-codes'),
      listAvSubscriptions: noPayloadCommand('app:list-av-subscriptions-v2'),
      // Preferred field is `artifactInput`; `outputDir` remains accepted only as
      // a compatibility alias while subscription import is being decoupled from
      // older "pick any crawl output path" wording.
      scanAvSubscriptionsFromOutput: optionsCommand('app:scan-av-subscriptions-v2-from-output'),
      addAvSubscription: optionsCommand('app:add-av-subscription-v2-manual'),
      refreshAvSubscriptions: optionsCommand('app:refresh-av-subscriptions-v2'),
      refreshAvSubscription: optionsCommand('app:refresh-av-subscription-v2'),
      removeAvSubscription: singleValueCommand('app:remove-av-subscription-v2', 'id'),
      clearAvSubscriptions: noPayloadCommand('app:clear-av-subscriptions-v2'),
      markAvSubscriptionSynced: singleValueCommand('app:mark-av-subscription-v2-synced', 'id'),
      patchAvSubscription: optionsCommand('app:patch-av-subscription-v2'),
      patchAllAvSubscriptionActressFilter: optionsCommand('app:patch-all-av-subscriptions-actress-filter-v2'),
      reorderAvSubscriptions: optionsCommand('app:reorder-av-subscriptions-v2'),
      enrichAvSubscriptionMedia: optionsCommand('app:enrich-av-subscription-v2-media'),
      prepareAvSubscriptionCrawl: singleValueCommand('app:prepare-av-subscription-v2-crawl', 'id'),
      startSubscriptionCrawl: optionsCommand('app:start-av-subscription-v2-crawl'),
      startSubscriptionBatchCrawl: optionsCommand('app:start-av-subscription-v2-batch-crawl'),
      finalizeAvSubscriptionCrawl: optionsCommand('app:finalize-av-subscription-v2-crawl'),
      stopSubscriptionCrawl: noPayloadCommand('app:stop-av-subscription-v2-crawl'),
      getSubscriptionCrawlStatus: noPayloadCommand('app:av-subscription-v2-crawl-status'),
      openPath: singleValueCommand('app:open-path', 'targetPath'),
      openOrganizerPath: twoValueCommand('app:open-organizer-path', 'rootPath', 'kind'),
      openOutputDir: singleValueCommand('app:open-output-dir', 'targetPath'),
      openExternal: singleValueCommand('app:open-external', 'targetUrl'),
      openLogFolder: noPayloadCommand('app:open-log-folder'),
      openMagnetFile: singleValueCommand('app:open-magnet-file', 'targetOutput'),
      getActressRankings: optionsCommand('app:get-actress-rankings'),
      resolveActressAlias: optionsCommand('app:resolve-actress-alias'),
	      rememberActressAlias: optionsCommand('app:remember-actress-alias'),
      resolveActressCrawlTarget: optionsCommand('app:resolve-actress-crawl-target'),
	      inspectActressTarget: optionsCommand('app:inspect-actress-target'),
	      resolveActressMedia: optionsCommand('app:resolve-actress-media'),
	      cacheActressWorkCovers: optionsCommand('app:cache-actress-work-covers'),
	      loadActressWorksPage: optionsCommand('app:load-actress-works-page'),
      runOrganizer: optionsCommand('app:run-organizer'),
      rescueOrganizerNames: optionsCommand('app:rescue-organizer-names'),
      startCrawl: optionsCommand('app:start-crawl'),
      restartCrawl: optionsCommand('app:restart-crawl'),
      stopCrawl: noPayloadCommand('app:stop-crawl'),
      updateAntiBlock: optionsCommand('app:update-antiblock'),

      // Media library metadata scraping commands.
      // These must stay in sync with wails-shell/internal/bridge/api_librarymetadata.go.
      getLibraryMetadataProviders: noPayloadCommand('app:librarymetadata-providers'),
      getLibraryMetadataBootstrap: optionsCommand('app:librarymetadata-bootstrap'),
      getLibraryMetadataCrawlSources: optionsCommand('app:librarymetadata-crawl-sources'),
      countLibraryMetadataCrawlArtifacts: optionsCommand('app:librarymetadata-count-crawl-artifacts'),
      scanLibraryMetadata: optionsCommand('app:librarymetadata-scan'),
      scrapeLibraryMetadata: optionsCommand('app:librarymetadata-scrape'),
      buildLibraryMetadataNFO: optionsCommand('app:librarymetadata-build-nfo'),
      writeLibraryMetadata: optionsCommand('app:librarymetadata-write'),
      resolveLibraryMetadata: optionsCommand('app:librarymetadata-resolve'),
      startLibraryMetadataJob: optionsCommand('app:librarymetadata-job-start'),
      cancelLibraryMetadataJob: optionsCommand('app:librarymetadata-job-cancel'),
      finishLibraryMetadataJob: optionsCommand('app:librarymetadata-job-finish'),
      autoSubscribeLibraryMetadataActor: optionsCommand('app:librarymetadata-auto-subscribe'),
      initLibraryMetadataLog: optionsCommand('app:librarymetadata-init-log'),
      appendLibraryMetadataLog: optionsCommand('app:librarymetadata-append-log'),
      openLibraryMetadataLogFolder: optionsCommand('app:librarymetadata-open-log-folder')
    };
  }

  function buildDesktopEventApi() {
    return {
      // `runner:*` topics are the long-lived raw compatibility feeds.
      // `crawl.*` topics below are the preferred Go-derived UI panel contracts.
      onLog: directEventSubscription('runner:log'),
      onState: directEventSubscription('runner:state'),
      onUiState: bridgeEventSubscription('crawlUiState', 'crawl.ui-state'),
      onStagePanel: bridgeEventSubscription('crawlStagePanel', 'crawl.stage-panel'),
      onResultPanel: bridgeEventSubscription('crawlResultPanel', 'crawl.result-panel'),
      onRunContext: bridgeEventSubscription('crawlRunContext', 'crawl.run-context'),
      onReviewPanel: bridgeEventSubscription('crawlReviewPanel', 'crawl.review-panel'),
      onQualitySummary: bridgeEventSubscription('crawlQualitySummary', 'crawl.quality-summary'),
      onLogContext: directEventSubscription('runner:log-context'),
      onOrganizerLog: directEventSubscription('organizer:log'),
      onOrganizerState: directEventSubscription('organizer:state'),
      onDependencyInstallProgress: directEventSubscription('dependency.install-progress'),
      onSubcrawlState: directEventSubscription('subcrawlv2.state'),
      onSubcrawlLog: directEventSubscription('subcrawlv2.log'),
      onAvSubscriptionV2Log: directEventSubscription('avsubscriptionv2.log'),
      onAvSubscriptionV2ListUpdated: directEventSubscription('avsubscriptionv2.list-updated'),
      onLibraryMetadataScanProgress: directEventSubscription('librarymetadata.scan.progress'),
      onSidecarLifecycle: bridgeEventSubscription('sidecarLifecycle', 'sidecar.lifecycle'),
      onAppNotice: bridgeEventSubscription('appNotice', 'app.notice'),
      onBridgeEvent: bridgeEventSubscription('generic', 'bridge:event')
    };
  }

  function createArtifactInputResolver() {
    async function resolveLatestInput(desktopApi, options = {}) {
      const api = desktopApi || null;
      if (!api) {
        return emptyResolvedArtifactInput();
      }

      const artifactKey = String(options.artifactKey || '').trim();
      const artifactType = String(options.artifactType || artifactKey || 'artifact').trim();

      if (typeof api.getCrawlRunContext === 'function') {
        const runContext = await invokeOptionalQuery(() => api.getCrawlRunContext());
        const resolvedFromRunContext = resolveArtifactInputFromContext(runContext, artifactKey, artifactType);
        if (resolvedFromRunContext) {
          return resolvedFromRunContext;
        }
      }

      if (typeof api.getCrawlResultPanel === 'function') {
        const resultPanel = await invokeOptionalQuery(() => api.getCrawlResultPanel());
        const resolvedFromResultPanel = resolveArtifactInputFromResultPanel(resultPanel);
        if (resolvedFromResultPanel) {
          return resolvedFromResultPanel;
        }
      }

      return emptyResolvedArtifactInput();
    }

    // Renderer controllers use the "safe" variant when autofill is optional.
    // If run-context/result-panel lookups fail, callers should receive the same
    // normalized empty object instead of each controller carrying its own catch
    // branch and empty-state shape.
    async function resolveLatestInputSafe(desktopApi, options = {}) {
      const resolved = await invokeOptionalQuery(() => resolveLatestInput(desktopApi, options));
      if (resolved && typeof resolved === 'object') {
        return resolved;
      }
      return emptyResolvedArtifactInput();
    }

    function describeResolvedInput(action, resolvedInput, labels = {}) {
      const mode = String(action || '').trim();
      const resolved = resolvedInput && typeof resolvedInput === 'object' ? resolvedInput : {};
      const value = String(resolved.value || '').trim();
      const type = String(resolved.type || '').trim();
      const sourceKind = String(resolved.sourceKind || '').trim();
      if (!value) {
        return '';
      }

      const snapshotLabel = String(labels.snapshot || '订阅快照').trim();
      const fallbackLabel = String(labels.fallback || '抓取结果输入').trim();
      const verb = mode === 'filled' ? '已填入最近' : '自动回填最近';

      const usesFallbackLabel =
        Boolean(resolved.isFallbackOutputDir) ||
        sourceKind === 'crawlOutputDir' ||
        type === 'crawlOutputDir';

      if (!usesFallbackLabel && (sourceKind || type)) {
        return `${verb}${snapshotLabel}：${value}`;
      }
      return `${verb}${fallbackLabel}：${value}`;
    }

    function describeResolvedInputOrFallback(action, resolvedInput, labels = {}, fallbackValue = '') {
      const resolvedMessage = describeResolvedInput(action, resolvedInput, labels);
      if (resolvedMessage) {
        return resolvedMessage;
      }

      const normalizedFallbackValue = String(fallbackValue || '').trim();
      if (!normalizedFallbackValue) {
        return '';
      }

      const fallbackLabel = String(labels.fallback || '抓取结果输入').trim();
      const verb = String(action || '').trim() === 'filled' ? '已回填最近' : '自动回填最近';
      return `${verb}${fallbackLabel}：${normalizedFallbackValue}`;
    }

    return {
      resolveLatestInput,
      resolveLatestInputSafe,
      emptyResolvedInput: emptyResolvedArtifactInput,
      describeResolvedInput,
      describeResolvedInputOrFallback
    };
  }

  function createDesktopApi() {
    return Object.assign({}, buildDesktopCommandApi(), buildDesktopEventApi());
  }

  async function invokeOptionalQuery(queryFn) {
    if (typeof queryFn !== 'function') {
      return null;
    }

    const resolved = await queryFn().catch(() => null);
    return resolved && typeof resolved === 'object' ? resolved : null;
  }

  const artifactInputResolver = createArtifactInputResolver();
  globalScope.desktopWailsPlatformBridge = {
    createDesktopApi,
    invokeOptionalQuery,
    getArtifactInputResolver: () => artifactInputResolver
  };

  // Shared resolver for "latest crawl-derived input" on the frontend side.
  // Downstream modules should reuse this helper instead of each carrying their
  // own run-context/result-panel fallback chain.
  //
  // Return contract:
  // - value: the path the caller should reuse
  // - type: semantic source label
  // - sourceKind: whether the reused value is a preferred artifact path or a
  //   crawl-output-directory fallback
  // - sourceOrigin: which panel/context produced the value, so future logging
  //   or diagnostics can explain where the autofill came from
  //
  // Important: even when the best fallback is only a crawl output directory,
  // we still label it as `crawlOutputDir` rather than a generic `outputDir`.
  // That keeps renderer-side terminology aligned with organizer/subscription
  // artifact-import boundaries and avoids spreading older Electron wording.
  globalScope.desktopArtifactInputResolver = artifactInputResolver;
})(typeof globalThis !== 'undefined' ? globalThis : window);
