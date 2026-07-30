// State controller owns UI-facing aggregation of crawl/task/runtime state.
// It should remain transport-agnostic: the controller renders normalized
// payloads and should not care whether they arrived through Wails or a legacy
// compatibility path.
//
// Ownership summary:
// 1) coalesce normalized crawl/task/runtime payloads for renderer projection
// 2) own summary stats, status pill, and button enablement updates
// 3) delegate panel-specific DOM work to dedicated renderers instead of inlining
//    stage/result/review rendering here
//
// File map for maintainers:
// 1) renderer dependency wiring for state sub-renderers
// 2) normalized crawl/task/runtime projection state
// 3) top-level state/result/review apply pipeline
(function initializeStateController(globalScope) {
  // createStateController is the pure crawl-panel projection layer. It should
  // render normalized runtime/read-model payloads, not dispatch crawl commands.
  function createStateController(options) {
    const stateHelpers = globalScope.desktopStateHelpers || null;
    const stagePanelRendererFactory = globalScope.desktopStagePanelRenderer || null;
    const resultPanelRendererFactory = globalScope.desktopResultPanelRenderer || null;
    const reviewPanelRendererFactory = globalScope.desktopReviewPanelRenderer || null;

    if (!stateHelpers || !stagePanelRendererFactory || !resultPanelRendererFactory || !reviewPanelRendererFactory) {
      throw new Error('State controller dependencies are not available.');
    }

    const { normalizeItems, sanitizeReadyItems } = stateHelpers;
    const {
      statusPill,
      stateMessage,
      startButton,
      stopButton,
      restartButton,
      statPage,
      statQueued,
      statAttempted,
      statCompleted,
      currentBox,
      currentTotalView,
      currentItemsView,
      unfinishedItemsView,
      unfinishedTotalView,
      duplicateBox,
      duplicateItemsView,
      duplicateTotalView,
      pageGapBox,
      pageGapItemsView,
      failedTotalView,
      failedItemsView,
      filteredItemsView,
      filteredTotalView,
      completedItemsView,
      completedTotalView,
      crawlStageStatus,
      crawlStageProgress,
      crawlStageTitle,
      crawlStageDescription,
      crawlStageMessage,
      crawlStageBarFill,
      crawlStageBar,
      crawlStageOutput,
      crawlStagePage,
      crawlStageQueued,
      crawlStageAttempted,
      crawlStageCompleted,
      crawlResultQuality,
      crawlResultSummary,
      resultHistoryController = null,
      statusLabels = {},
      defaultMessage = '等待开始抓取。',
      emptyTexts = {},
      maxPanelItems = 12,
      renderInterval = 180,
      failureCategoryLabels = {},
      stateTexts = {}
    } = options || {};

    const stagePanelRenderer = stagePanelRendererFactory.createStagePanelRenderer({
      crawlStageStatus,
      crawlStageProgress,
      crawlStageTitle,
      crawlStageDescription,
      crawlStageMessage,
      crawlStageBarFill,
      crawlStageBar,
      crawlStageOutput,
      crawlStagePage,
      crawlStageQueued,
      crawlStageAttempted,
      crawlStageCompleted,
      statusLabels,
      defaultMessage
    });
    const resultPanelRenderer = resultPanelRendererFactory.createResultPanelRenderer({
      crawlResultQuality,
      crawlResultSummary,
      resultHistoryController,
      statusLabels
    });
    const reviewPanelRenderer = reviewPanelRendererFactory.createReviewPanelRenderer({
      currentBox,
      currentTotalView,
      currentItemsView,
      unfinishedItemsView,
      unfinishedTotalView,
      duplicateBox,
      duplicateItemsView,
      duplicateTotalView,
      pageGapBox,
      pageGapItemsView,
      failedTotalView,
      failedItemsView,
      filteredItemsView,
      filteredTotalView,
      completedItemsView,
      completedTotalView,
      emptyTexts,
      maxPanelItems,
      failureCategoryLabels,
      stateTexts
    });

    // State-controller responsibilities:
    // 1) coalesce transport payloads into one UI-facing render cadence
    // 2) update controller-owned summary stats and status buttons
    // 3) delegate panel-specific DOM work to dedicated renderers
    //
    // This layer should not invent new crawler facts. If a field is missing,
    // prefer a neutral fallback over reconstructing business meaning here.

    let pendingState = null;
    let pendingUiState = null;
    let pendingReviewPanel = null;
    let stateFlushTimer = null;

    const lastRenderSignature = {
      status: '',
      stats: ''
    };

    function setStatus(status, message = defaultMessage) {
      // Status projection is renderer-local state only. Button enabling and
      // pill text should follow upstream state, not invent new task semantics.
      const nextStatus = String(status || 'idle').trim() || 'idle';
      const nextMessage = String(message || defaultMessage).trim() || defaultMessage;
      const signature = `${nextStatus}##${nextMessage}`;

      if (signature === lastRenderSignature.status) {
        return;
      }

      lastRenderSignature.status = signature;
      reviewPanelRenderer.setPreserveMode(isFinalStatus(nextStatus));
      if (statusPill) {
        statusPill.className = `status-pill ${nextStatus}`;
        statusPill.textContent = statusLabels[nextStatus] || nextStatus;
      }
      if (stateMessage) {
        stateMessage.textContent = nextMessage;
      }

      const isStartingOrStopping = ['starting', 'stopping'].includes(nextStatus);
      const isRunning = ['starting', 'running', 'stopping'].includes(nextStatus);

      if (startButton) {
        startButton.disabled = isRunning;
      }
      if (stopButton) {
        stopButton.disabled = !isRunning;
      }
      if (restartButton) {
        restartButton.disabled = isStartingOrStopping;
      }
    }

    function updateStats(stats = {}) {
      const nextPage = String(stats.pageIndex ?? 1);
      const nextQueued = String(stats.queued ?? 0);
      const nextAttempted = String(stats.attempted ?? 0);
      const nextCompleted = String(stats.completed ?? 0);
      // Filtered-item state should arrive already normalized from the bridge/
      // Go read model. The renderer only trims/presents it and should not
      // infer actress-filter business rules on its own.
      const filteredItems = normalizeItems(stats.filteredItemIds || stats.filteredItems || [], maxPanelItems);
      const filteredTotal = Number.isFinite(stats.filteredByActressCount)
        ? stats.filteredByActressCount
        : filteredItems.length;
      const completedItems = normalizeItems(stats.completedItemIds || stats.completedItems || [], maxPanelItems);
      const completedTotal = Number.isFinite(stats.completedMagnetCount)
        ? stats.completedMagnetCount
        : (Number.isFinite(stats.completedItems)
          ? stats.completedItems
          : completedItems.length);
      const signature = `${nextPage}|${nextQueued}|${nextAttempted}|${nextCompleted}|${filteredTotal}|${filteredItems.join(',')}|${completedTotal}|${completedItems.join(',')}`;

      if (signature === lastRenderSignature.stats) {
        return;
      }

      lastRenderSignature.stats = signature;
      if (statPage) {
        statPage.textContent = nextPage;
      }
      if (statQueued) {
        statQueued.textContent = nextQueued;
      }
      if (statAttempted) {
        statAttempted.textContent = nextAttempted;
      }
      if (statCompleted) {
        statCompleted.textContent = nextCompleted;
      }

      reviewPanelRenderer.renderFilteredItems(filteredItems, filteredTotal);
      reviewPanelRenderer.renderCompletedItems(completedItems, completedTotal);
    }

    function flushPendingState() {
      if (stateFlushTimer) {
        clearTimeout(stateFlushTimer);
        stateFlushTimer = null;
      }

      // Rendering is intentionally batched so rapid event bursts from bridge
      // updates do not thrash the DOM. Final states bypass the timer and flush
      // immediately so operators see the terminal result without delay.
      if (!pendingState && !pendingUiState && !pendingReviewPanel) {
        return;
      }

      const state = pendingUiState || pendingState;
      const reviewPanel = pendingReviewPanel || (state && state.reviewPanel ? state.reviewPanel : null);
      pendingState = null;
      pendingUiState = null;
      pendingReviewPanel = null;

      if (state) {
        setStatus(state.status, state.message || defaultMessage);
        updateStats(state.stats);
        if (state.__goUiState) {
          const activeItems = sanitizeReadyItems(state.activeItems, maxPanelItems);
          reviewPanelRenderer.renderActiveItems(
            activeItems,
            Number.isFinite(state.activeItemsTotal) ? state.activeItemsTotal : activeItems.length
          );
        } else {
          reviewPanelRenderer.updateActiveItems(state.activeItems, state.activeItemsTotal);
        }
      }

      if (reviewPanel) {
        // Review-panel status is diagnostic metadata for list cards. The top
        // lifecycle pill should keep following task/ui state so stale failed
        // review snapshots do not mark the idle startup screen as abnormal.
        reviewPanelRenderer.applyPanel(reviewPanel);
        return;
      }

      if (!state) {
        return;
      }

      reviewPanelRenderer.updateDuplicateItems(state.duplicateItems, state.duplicateItemsTotal);
      reviewPanelRenderer.updateUnfinishedItems(state.unfinishedItems, state.unfinishedItemsTotal);
      reviewPanelRenderer.updatePageGapItems(state.pageGapItems);
      reviewPanelRenderer.updateCompletedItems(
        state.completedItems || state.completedItemIds,
        Number.isFinite(state.completedMagnetCount)
          ? state.completedMagnetCount
          : (Number.isFinite(state.completedItemsTotal) ? state.completedItemsTotal : state.completedItems)
      );
      reviewPanelRenderer.updateFailedDetails(state.failedDetails, state.failedDetailsTotal);
    }

    function isFinalStatus(status) {
      return ['completed', 'error', 'stopped', 'incomplete'].includes(String(status || '').toLowerCase());
    }

    function scheduleFlush(nextStatus) {
      if (isFinalStatus(nextStatus)) {
        flushPendingState();
        return;
      }

      if (!stateFlushTimer) {
        stateFlushTimer = setTimeout(flushPendingState, renderInterval);
      }
    }

    function enqueueState(state) {
      pendingState = state;
      scheduleFlush(state && state.status);
    }

    function enqueueUiState(state) {
      // Go UI state is already a renderer-ready read model. Mark it so the
      // flush path can avoid re-running legacy item shaping rules.
      pendingUiState = {
        ...state,
        __goUiState: true
      };

      const nextStatus = (state && state.status) || (pendingState && pendingState.status) || '';
      scheduleFlush(nextStatus);
    }

    function enqueueReviewPanel(panel) {
      pendingReviewPanel = panel;
      const nextStatus = (panel && panel.status) || (pendingState && pendingState.status) || '';
      scheduleFlush(nextStatus);
    }

    return {
      enqueueState,
      enqueueUiState,
      enqueueReviewPanel,
      // Stage/result/review panels remain delegated read models. This
      // controller only decides render cadence and handoff boundaries.
      applyStagePanel: stagePanelRenderer.applyPanel,
      applyResultPanel: resultPanelRenderer.applyPanel,
      setStatus,
      clearResultHistory: resultPanelRenderer.clearHistory
    };
  }

  globalScope.desktopStateController = {
    createStateController
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
