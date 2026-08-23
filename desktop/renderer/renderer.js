// Main renderer bootstrap for the current desktop UI.
// This entrypoint should normally run against the Wails platform bridge. Legacy
// compatibility behavior may still exist underneath, but renderer boot issues
// should first be debugged from this file and `platformBridge*.js`.
// Renderer assembly root for the current desktop frontend.
// This file wires controllers/views/helpers together, but it should keep
// business logic inside those modules instead of becoming another mega-controller.
//
// Ownership summary:
// 1) collect DOM elements and shared dependencies
// 2) assemble workspace controllers around the active desktop bridge
// 3) kick off bootstrap in the correct order
//
// File map for maintainers:
// 1) dependency resolution and readiness guards
// 2) controller option builders
// 3) top-level renderer assembly and bootstrap handoff
(function initializeRenderer(globalScope) {
  function resolveRendererDependencies(scope) {
    const platformBridge = scope.desktopPlatformBridge || null;
    return {
      platformBridge,
      desktopApi:
        platformBridge && typeof platformBridge.createDesktopApi === 'function'
          ? platformBridge.createDesktopApi()
          : null,
      uiText: scope.desktopUiText,
      appUpdateControllerFactory: scope.desktopAppUpdateController,
      logControllerFactory: scope.desktopLogController,
      heroBorderFlowControllerFactory: scope.desktopHeroBorderFlowController || null,
      rendererElementsFactory: scope.desktopRendererElements || null,
      rendererShellControllerFactory: scope.desktopRendererShellController,
      rendererBootstrapControllerFactory: scope.desktopRendererBootstrapController,
      crawlResultHistoryControllerFactory: scope.desktopCrawlResultHistoryController,
      stateControllerFactory: scope.desktopStateController,
      formControllerFactory: scope.desktopFormController,
      actressAtlasControllerFactory: scope.desktopActressAtlasController,
      rankingControllerFactory: scope.desktopRankingController,
      organizerControllerFactory: scope.desktopOrganizerController,
      subscriptionControllerFactory: scope.desktopSubscriptionController,
      libraryMetadataControllerFactory: scope.desktopLibraryMetadataController,
      crawlRuntimeControllerFactory: scope.desktopCrawlRuntimeController,
      crawlPanelModel: scope.desktopCrawlPanelModel || null
    };
  }

  function getMissingRendererDependencies(deps) {
    const required = {
      desktopApi: deps.desktopApi,
      uiText: deps.uiText,
      appUpdateControllerFactory: deps.appUpdateControllerFactory,
      logControllerFactory: deps.logControllerFactory,
      heroBorderFlowControllerFactory: deps.heroBorderFlowControllerFactory,
      rendererElementsFactory: deps.rendererElementsFactory,
      rendererShellControllerFactory: deps.rendererShellControllerFactory,
      rendererBootstrapControllerFactory: deps.rendererBootstrapControllerFactory,
      crawlResultHistoryControllerFactory: deps.crawlResultHistoryControllerFactory,
      stateControllerFactory: deps.stateControllerFactory,
      formControllerFactory: deps.formControllerFactory,
      actressAtlasControllerFactory: deps.actressAtlasControllerFactory,
      rankingControllerFactory: deps.rankingControllerFactory,
      organizerControllerFactory: deps.organizerControllerFactory,
      subscriptionControllerFactory: deps.subscriptionControllerFactory,
      libraryMetadataControllerFactory: deps.libraryMetadataControllerFactory,
      crawlRuntimeControllerFactory: deps.crawlRuntimeControllerFactory
    };
    return Object.entries(required)
      .filter(([, value]) => !value)
      .map(([key]) => key);
  }

  function hasRequiredRendererDependencies(deps) {
    return getMissingRendererDependencies(deps).length === 0;
  }

  function buildRendererRequirementSnapshot(deps) {
    return {
      desktopApi: deps.desktopApi,
      uiText: deps.uiText,
      appUpdateControllerFactory: deps.appUpdateControllerFactory,
      logControllerFactory: deps.logControllerFactory,
      heroBorderFlowControllerFactory: deps.heroBorderFlowControllerFactory,
      rendererElementsFactory: deps.rendererElementsFactory,
      rendererShellControllerFactory: deps.rendererShellControllerFactory,
      rendererBootstrapControllerFactory: deps.rendererBootstrapControllerFactory,
      crawlResultHistoryControllerFactory: deps.crawlResultHistoryControllerFactory,
      stateControllerFactory: deps.stateControllerFactory,
      formControllerFactory: deps.formControllerFactory,
	      actressAtlasControllerFactory: deps.actressAtlasControllerFactory,
      rankingControllerFactory: deps.rankingControllerFactory,
      organizerControllerFactory: deps.organizerControllerFactory,
      subscriptionControllerFactory: deps.subscriptionControllerFactory,
      libraryMetadataControllerFactory: deps.libraryMetadataControllerFactory,
      crawlRuntimeControllerFactory: deps.crawlRuntimeControllerFactory
    };
  }

  function buildLogControllerOptions(elements, UI_TEXT) {
    return {
      logView: elements.logView,
      logFilePath: elements.logFilePath,
      maxLines: UI_TEXT.limits.maxLogLines,
      defaultHint: UI_TEXT.log.defaultHint,
      logPathPrefix: UI_TEXT.log.createdPrefix,
      truncatedSuffix: UI_TEXT.log.truncatedSuffix,
      visibleLevels: UI_TEXT.log.visibleLevels,
      maxVisibleLength: UI_TEXT.log.maxVisibleLength,
      hiddenKeywords: UI_TEXT.log.hiddenKeywords
    };
  }

  function buildStateControllerOptions(
    elements,
    UI_TEXT,
    STATUS_LABELS,
    FAILURE_CATEGORY_LABELS,
    crawlResultHistoryController
  ) {
    return {
      statusPill: elements.statusPill,
      stateMessage: elements.stateMessage,
      startButton: elements.startButton,
      stopButton: elements.stopButton,
      restartButton: elements.restartButton,
      statPage: elements.statPage,
      statQueued: elements.statQueued,
      statAttempted: elements.statAttempted,
      statCompleted: elements.statCompleted,
      currentBox: elements.currentBox,
      currentTotalView: elements.currentTotalView,
      currentItemsView: elements.currentItemsView,
      unfinishedBox: elements.unfinishedBox,
      unfinishedItemsView: elements.unfinishedItemsView,
      unfinishedTotalView: elements.unfinishedTotalView,
      duplicateBox: elements.duplicateBox,
      duplicateItemsView: elements.duplicateItemsView,
      duplicateTotalView: elements.duplicateTotalView,
      pageGapBox: elements.pageGapBox,
      pageGapItemsView: elements.pageGapItemsView,
      failedBox: elements.failedBox,
      failedTotalView: elements.failedTotalView,
      failedItemsView: elements.failedItemsView,
      filteredBox: elements.filteredBox,
      filteredItemsView: elements.filteredItemsView,
      filteredTotalView: elements.filteredTotalView,
      completedBox: elements.completedBox,
      completedItemsView: elements.completedItemsView,
      completedTotalView: elements.completedTotalView,
      crawlStageStatus: elements.crawlStageStatus,
      crawlStageProgress: elements.crawlStageProgress,
      crawlStageTitle: elements.crawlStageTitle,
      crawlStageDescription: elements.crawlStageDescription,
      crawlStageMessage: elements.crawlStageMessage,
      crawlStageBarFill: elements.crawlStageBarFill,
      crawlStageBar: elements.crawlStageBar,
      crawlStageOutput: elements.crawlStageOutput,
      crawlStagePage: elements.crawlStagePage,
      crawlStageQueued: elements.crawlStageQueued,
      crawlStageAttempted: elements.crawlStageAttempted,
      crawlStageCompleted: elements.crawlStageCompleted,
      crawlResultQuality: elements.crawlResultQuality,
      crawlResultSummary: elements.crawlResultSummary,
      resultHistoryController: crawlResultHistoryController,
      statusLabels: STATUS_LABELS,
      defaultMessage: UI_TEXT.state.defaultMessage,
      emptyTexts: {
        active: UI_TEXT.state.activeEmpty,
        unfinished: UI_TEXT.state.unfinishedEmpty,
        duplicate: UI_TEXT.state.duplicateEmpty,
        pageGap: UI_TEXT.state.pageGapEmpty,
        filtered: UI_TEXT.state.filteredEmpty,
        completed: UI_TEXT.state.completedEmpty,
        failed: UI_TEXT.state.failedEmpty
      },
      maxPanelItems: UI_TEXT.limits.maxPanelItems,
      renderInterval: Math.max(UI_TEXT.limits.stateRenderInterval || 0, 180),
      failureCategoryLabels: FAILURE_CATEGORY_LABELS,
      stateTexts: {
        failedSummaryPrefix: UI_TEXT.state.failedSummaryPrefix,
        failedSummaryMiddle: UI_TEXT.state.failedSummaryMiddle,
        failedSummarySuffix: UI_TEXT.state.failedSummarySuffix,
        unknownItem: UI_TEXT.state.unknownItem,
        defaultFailureReason: UI_TEXT.state.defaultFailureReason,
        failureCategoryPrefix: UI_TEXT.state.failureCategoryPrefix,
        failureRetryPrefix: UI_TEXT.state.failureRetryPrefix,
        failureManualReview: UI_TEXT.state.failureManualReview,
        failureAdvicePrefix: UI_TEXT.state.failureAdvicePrefix,
        failureTimePrefix: UI_TEXT.state.failureTimePrefix
      }
    };
  }

  function buildBootstrapControllerOptions(
    rendererShellController,
    heroBorderFlowController,
    appUpdateController,
    crawlRuntimeController,
    formController,
    actressAtlasController,
    rankingController,
    organizerController,
    subscriptionController,
    libraryMetadataController,
    stateController,
    elements,
    uiText
  ) {
    return {
      shellController: rendererShellController,
      heroBorderFlowController,
      appUpdateController,
      crawlRuntimeController,
      formController,
      actressAtlasController,
      rankingController,
      organizerController,
      subscriptionController,
      libraryMetadataController,
      stateController,
      sourceLink: elements.sourceLink,
      uiText,
      retryDelays: [0, 200, 400, 800, 1200, 1800, 2800, 4000]
    };
  }

  function buildCrawlResultHistoryControllerOptions(crawlPanelModel, elements, desktopApi) {
    return {
      crawlPanelModel,
      historyView: elements.crawlResultHistoryView,
      openPath: (targetPath) => desktopApi.openPath(targetPath)
    };
  }

  function handleWorkspaceChange(targetWorkspace, previousWorkspace) {
    // 审计 H-06/H-07：离开需要持续动画或轮询的工作区时释放资源，
    // 并在进入新工作区后重新初始化需要持续运行的控制器。
    if (previousWorkspace === 'crawler' && targetWorkspace !== 'crawler') {
      if (formController && typeof formController.dispose === 'function') {
        formController.dispose();
      }
    }
    if (previousWorkspace === 'subscription' && targetWorkspace !== 'subscription') {
      if (subscriptionController && typeof subscriptionController.dispose === 'function') {
        subscriptionController.dispose();
      }
    }

    // 每个工作区都有对应的 hero 流动条主题，切换后重新 bootstrap，
    // 让目标工作区的 hero 边框继续流动；先 stop 再 bootstrap 可避免重复动画帧。
    if (heroBorderFlowController && typeof heroBorderFlowController.stop === 'function') {
      heroBorderFlowController.stop();
    }
    if (heroBorderFlowController && typeof heroBorderFlowController.bootstrap === 'function') {
      heroBorderFlowController.bootstrap();
    }

    if (targetWorkspace === 'crawler') {
      if (formController && typeof formController.bootstrap === 'function') {
        formController.bootstrap();
      }
    }
    if (targetWorkspace === 'actressatlas') {
      // The first navigation into Actor Atlas explains its proxy requirement
      // once. The controller owns the notice state so shell routing remains
      // presentational and does not read crawler settings directly.
      if (actressAtlasController && typeof actressAtlasController.ensureProxyNotice === 'function') {
        void actressAtlasController.ensureProxyNotice();
      }
      if (actressAtlasController && typeof actressAtlasController.bootstrap === 'function') {
        void actressAtlasController.bootstrap();
      }
      if (actressAtlasController && typeof actressAtlasController.refresh === 'function') {
        void actressAtlasController.refresh();
      }
    }
  }

  function buildRendererShellControllerOptions(elements) {
    return {
      elements,
      initialWorkspace: 'crawler',
      onWorkspaceChange: handleWorkspaceChange
    };
  }

  function buildFormControllerOptions(elements, desktopApi, logController, stateController, uiText) {
    return {
      elements,
      desktopApi,
      logController,
      stateController,
      uiText
    };
  }

  function buildRankingControllerOptions(elements, desktopApi, logController, uiText, formController) {
    return {
      elements,
      desktopApi,
      logController,
      uiText,
      formController
    };
  }

  function buildOrganizerControllerOptions(elements, desktopApi, uiText) {
    return {
      elements,
      desktopApi,
      uiText
    };
  }

  function buildSubscriptionControllerOptions(
    elements,
    desktopApi,
    uiText,
    formController,
    rendererShellController,
    subscriptionCrawlSessionBridge
  ) {
    return {
      elements,
      desktopApi,
      uiText,
      formController,
      switchWorkspace: (workspaceKey) => rendererShellController.setWorkspace(workspaceKey),
      subscriptionCrawlSessionBridge
    };
  }

  function buildLibraryMetadataControllerOptions(elements, desktopApi, uiText, rendererShellController) {
    return {
      elements,
      desktopApi,
      uiText,
      switchWorkspace: (workspaceKey) => rendererShellController.setWorkspace(workspaceKey)
    };
  }

  function buildCrawlRuntimeControllerOptions(
    desktopApi,
    platformBridge,
    elements,
    uiText,
    logController,
    stateController,
    crawlPanelModel,
    subscriptionCrawlSessionBridge,
    subscriptionController,
    organizerController,
    libraryMetadataController
  ) {
    return {
      desktopApi,
      platformBridge,
      elements,
      uiText,
      logController,
      stateController,
      crawlPanelModel,
      subscriptionCrawlSessionBridge,
      subscriptionController,
      organizerController,
      libraryMetadataController
    };
  }

  const rendererDependencies = resolveRendererDependencies(globalScope);
  const {
    platformBridge,
    desktopApi,
    uiText,
    appUpdateControllerFactory,
    logControllerFactory,
    heroBorderFlowControllerFactory,
    rendererElementsFactory,
    rendererShellControllerFactory,
    rendererBootstrapControllerFactory,
    crawlResultHistoryControllerFactory,
    stateControllerFactory,
    formControllerFactory,
    actressAtlasControllerFactory,
    rankingControllerFactory,
    organizerControllerFactory,
    subscriptionControllerFactory,
    libraryMetadataControllerFactory,
    crawlRuntimeControllerFactory,
    crawlPanelModel
  } = rendererDependencies;

  const missingDependencies = getMissingRendererDependencies(buildRendererRequirementSnapshot(rendererDependencies));
  if (missingDependencies.length > 0) {
    const fallbackMessage =
      uiText && uiText.UI_TEXT && uiText.UI_TEXT.runtime
        ? uiText.UI_TEXT.runtime.missingDependencies
        : 'Desktop runtime dependencies are not available.';
    throw new Error(`${fallbackMessage} [missing: ${missingDependencies.join(', ')}]`);
  }

  const { UI_TEXT, STATUS_LABELS, FAILURE_CATEGORY_LABELS, applyStaticText } = uiText;

  // Renderer responsibilities in the current architecture:
  // 1) wire desktop bridge events into UI controllers
  // 2) hydrate startup state from Go snapshots/panels
  // 3) keep compatibility fallbacks narrow and visibly secondary
  //
  // This file should not own crawler business rules. When debugging repeated
  // logs or panel duplication, first separate:
  // - duplicate upstream events
  // - duplicate renderer fallback paths
  // before touching crawler/runtime code.
  //
  // Structural rule for maintenance:
  // - this file wires factories together
  // - controllers own module state/actions
  // - views/renderers own DOM construction
  // If a future change needs business branching here, prefer moving that branch
  // into the corresponding controller first.

  const elements = rendererElementsFactory.collectRendererElements(document);
  const heroBorderFlowController = heroBorderFlowControllerFactory.createHeroBorderFlowController();
  const appUpdateController = appUpdateControllerFactory.createAppUpdateController({
    elements,
    desktopApi,
    uiText
  });

  applyStaticText(document);

  const logController = logControllerFactory.createLogController(buildLogControllerOptions(elements, UI_TEXT));

  const crawlResultHistoryController = crawlResultHistoryControllerFactory.createCrawlResultHistoryController(
    buildCrawlResultHistoryControllerOptions(crawlPanelModel, elements, desktopApi)
  );
  crawlResultHistoryController.bootstrap();

  const rendererShellController = rendererShellControllerFactory.createRendererShellController(
    buildRendererShellControllerOptions(elements)
  );

  const stateController = stateControllerFactory.createStateController(
    buildStateControllerOptions(elements, UI_TEXT, STATUS_LABELS, FAILURE_CATEGORY_LABELS, crawlResultHistoryController)
  );

  const formController = formControllerFactory.createFormController(
    buildFormControllerOptions(elements, desktopApi, logController, stateController, uiText)
  );

  const actressAtlasController = actressAtlasControllerFactory.createActressAtlasController({
    elements,
    desktopApi,
    formController,
    switchWorkspace: (workspaceKey) => rendererShellController.setWorkspace(workspaceKey)
  });

  // Renderer assembly keeps the three workspaces intentionally loose:
  // 1) crawler owns the editable crawl form plus runtime panels
  // 2) ranking/subscription may only prefill crawler input or switch back to
  //    the crawler workspace for the next manual run
  // 3) organizer stays on artifact-import contracts and should not call into
  //    crawler form/runtime internals directly
  //
  // If a future change needs a new cross-workspace interaction, prefer adding
  // one explicit bridge/handoff here instead of letting controllers reach into
  // each other ad hoc.
  // Workspace controller construction remains explicit here so cross-workspace
  // dependencies stay visible during maintenance review.
  //
  // 审计 H-09：订阅抓取 session 通过显式 bridge 共享，避免隐式全局变量。
  const subscriptionCrawlSessionBridge = {
    getSession: () => null,
    setSession: () => {},
    onSessionChange: null
  };

  const rankingController = rankingControllerFactory.createRankingController(
    buildRankingControllerOptions(elements, desktopApi, logController, uiText, formController)
  );

  const organizerController = organizerControllerFactory.createOrganizerController(
    buildOrganizerControllerOptions(elements, desktopApi, uiText)
  );
  const subscriptionController = subscriptionControllerFactory.createSubscriptionController(
    buildSubscriptionControllerOptions(
      elements,
      desktopApi,
      uiText,
      formController,
      rendererShellController,
      subscriptionCrawlSessionBridge
    )
  );
  subscriptionCrawlSessionBridge.getSession = () =>
    subscriptionController && typeof subscriptionController.getActiveCrawlSession === 'function'
      ? subscriptionController.getActiveCrawlSession()
      : null;

  const libraryMetadataController = libraryMetadataControllerFactory.createLibraryMetadataController(
    buildLibraryMetadataControllerOptions(elements, desktopApi, uiText, rendererShellController)
  );
  const crawlRuntimeController = crawlRuntimeControllerFactory.createCrawlRuntimeController(
    buildCrawlRuntimeControllerOptions(
      desktopApi,
      platformBridge,
      elements,
      uiText,
      logController,
      stateController,
      crawlPanelModel,
      subscriptionCrawlSessionBridge,
      subscriptionController,
      organizerController,
      libraryMetadataController
    )
  );

  // 将工作区控制器暴露到全局作用域，便于订阅抓取完成后触发跨工作区刷新。
  globalScope.organizerController = organizerController;
  globalScope.libraryMetadataController = libraryMetadataController;
  globalScope.subscriptionController = subscriptionController;

  const rendererBootstrapController = rendererBootstrapControllerFactory.createRendererBootstrapController(
    buildBootstrapControllerOptions(
      rendererShellController,
      heroBorderFlowController,
      appUpdateController,
      crawlRuntimeController,
      formController,
      actressAtlasController,
      rankingController,
      organizerController,
      subscriptionController,
      libraryMetadataController,
      stateController,
      elements,
      uiText
    )
  );

  // Startup handoff ends here: renderer.js assembles controllers, then the
  // bootstrap controller owns all warmup sequencing and retry behavior.
  void rendererBootstrapController.bootstrap();
})(typeof globalThis !== 'undefined' ? globalThis : window);
