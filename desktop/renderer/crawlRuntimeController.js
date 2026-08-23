// Crawl runtime controller owns crawler-specific renderer hydration:
// 1) subscribe to Go/compat crawl events
// 2) hydrate stage/result/review panels at startup
// 3) keep run-context, quality-summary, and compatibility fallbacks together
//
// It should not own generic workspace switching or crawler form input logic.
//
// Ownership summary:
// 1) own crawl runtime event consumption and panel hydration
// 2) keep startup fallback queries together for crawl-only read models
// 3) stay separate from editable crawler form state and workspace shell logic
//
// File map for maintainers:
// 1) runtime capability detection and bootstrap guards
// 2) event-feed binding and live payload handlers
// 3) startup panel hydration and fallback query helpers
(function initializeCrawlRuntimeController(globalScope) {
  // createCrawlRuntimeController owns live event consumption and runtime-panel
  // hydration. It should stay separate from editable form state.
  function createCrawlRuntimeController(options) {
    const {
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
    } = options;
    const { UI_TEXT, STATUS_LABELS } = uiText;
    const supportsGoUiState = typeof desktopApi.onUiState === 'function';
    const supportsGoStagePanel = typeof desktopApi.onStagePanel === 'function';
    const supportsGoResultPanel = typeof desktopApi.onResultPanel === 'function';
    const supportsGoRunContext = typeof desktopApi.onRunContext === 'function';
    const supportsGoReviewPanel = typeof desktopApi.onReviewPanel === 'function';
    const supportsGoQualitySummary = typeof desktopApi.onQualitySummary === 'function';

    // Runtime-controller workflow map:
    // 1) bind live feeds once
    // 2) hydrate startup panels once
    // 3) normalize incoming log/state/panel payloads
    // 4) hand UI-facing state updates to stateController/logController
    //
    // If a bug is about "what the crawler did", inspect backend services first.
    // If a bug is about "why the page did not reflect backend state", start here.

    // Crawl runtime renderer state is intentionally feed-oriented:
    // - one-time feed binding guards
    // - one-time startup panel hydration guards
    // - last-seen dedupe markers for log-path and quality-summary output
    // Crawler form state stays outside this controller.
    let lastAnnouncedLogPath = '';
    let lastQualitySummarySignature = '';
    let lastResultPanel = null;
    let lastAutoRefreshSignature = '';
    let lastNotificationSignature = '';
    let previousCrawlStatus = 'idle';
    let feedsBound = false;
    let clearHistoryBound = false;
    let panelsBootstrapped = false;
    let bootstrapPanelsPromise = null;
    let feedDisposers = [];

    function isSubscriptionBridgeSessionActive() {
      // 审计 H-09：通过显式 bridge 读取订阅抓取 session，不再访问隐式全局变量。
      const getSession =
        subscriptionCrawlSessionBridge && typeof subscriptionCrawlSessionBridge.getSession === 'function'
          ? subscriptionCrawlSessionBridge.getSession
          : null;
      const session = typeof getSession === 'function' ? getSession() : null;
      return Boolean(session && typeof session === 'object' && session.subscriptionId);
    }

    // 爬虫完成后自动刷新依赖抓取产物的工作区，避免用户找不到刚刮削的内容。
    function autoRefreshWorkspacesOnCrawlCompleted(outputDir) {
      const signature = String(outputDir || '').trim();
      if (!signature || signature === lastAutoRefreshSignature) {
        return;
      }
      lastAutoRefreshSignature = signature;
      appendLog('info', '爬虫已完成，正在自动刷新视频整理、刮削、订阅工作区...');

      if (organizerController && typeof organizerController.refreshLatestCrawlOutput === 'function') {
        organizerController.refreshLatestCrawlOutput().catch(() => {});
      }
      if (libraryMetadataController && typeof libraryMetadataController.loadCrawlSources === 'function') {
        libraryMetadataController.loadCrawlSources().catch(() => {});
      }
      if (subscriptionController && typeof subscriptionController.loadRecentCrawlOptionsFromHistory === 'function') {
        subscriptionController.loadRecentCrawlOptionsFromHistory().catch(() => {});
      }
      if (subscriptionController && typeof subscriptionController.loadSubscriptions === 'function') {
        subscriptionController.loadSubscriptions().catch(() => {});
      }
    }

    // 质量摘要使用 ok/warning 等分析状态，而生命周期状态使用 completed/
    // stopped 等运行状态。这里统一成弹窗需要的生命周期状态，避免摘要
    // 已完成但被误判为“不是完成态”。
    function normalizeCompletionStatus(status, details = {}) {
      const normalizedStatus = String(status || '').trim().toLowerCase();
      if (
        ['ok', 'success', 'successful'].includes(normalizedStatus) ||
        (['warning', 'warn'].includes(normalizedStatus) && details.completed === true)
      ) {
        return 'completed';
      }
      return normalizedStatus;
    }

    function resolveNotificationIdentity(details = {}) {
      return String(
        details.runId ||
          details.taskId ||
          details.sessionLogPath ||
          details.latestLogPath ||
          details.reportPath ||
          details.completedAt ||
          details.outputDir ||
          details.currentTaskOutputDir ||
          details.lastTaskOutputDir ||
          ''
      ).trim();
    }

    // 抓取完成时通过弹窗强提醒用户，避免只依赖日志/状态 pill 的弱反馈。
    async function notifyCrawlCompletion(status, message, details = {}) {
      const normalizedStatus = normalizeCompletionStatus(status, details);
      if (!['completed', 'error', 'stopped', 'incomplete'].includes(normalizedStatus)) {
        return;
      }
      const notificationIdentity = resolveNotificationIdentity(details);
      const signature = notificationIdentity
        ? `${normalizedStatus}##${notificationIdentity}`
        : `${normalizedStatus}##${String(message || '').trim()}`;
      if (signature === lastNotificationSignature) {
        return;
      }
      lastNotificationSignature = signature;

      const titleMap = {
        completed: '抓取完成',
        incomplete: '抓取未完全完成',
        stopped: '抓取已停止',
        error: '抓取出错'
      };
      const title = titleMap[normalizedStatus] || '抓取结束';
      const body = String(message || `${title}，请查看运行日志了解详情。`).trim();
      let magnetTarget = String(
        (details && details.magnetPath) ||
          (details && details.preferredMagnetPath) ||
          (details && details.outputDir) ||
          (details && details.currentTaskOutputDir) ||
          (details && details.lastTaskOutputDir) ||
          (lastResultPanel && (lastResultPanel.magnetPath || lastResultPanel.outputDir)) ||
          ''
      ).trim();

      if (desktopApi && typeof desktopApi.showAlert === 'function') {
        try {
          await desktopApi.showAlert({
            type: normalizedStatus === 'completed' ? 'success' : 'warning',
            title,
            message: body,
            buttons: ['确定']
          });

          if (normalizedStatus === 'completed' || normalizedStatus === 'incomplete') {
            // 质量摘要和生命周期事件可能先于结果面板到达。即使此时还没有
            // 磁力路径，也必须显示询问；打开动作会由 Go 桥接按当前运行目录
            // 再次解析，不能让路径暂时为空吞掉第二次弹窗。
            // incomplete（存在失败项/分页缺口的运行）同样已产出磁力文件，
            // 完成提示后也应询问是否打开，否则真实抓取几乎永远看不到询问。
            if (!magnetTarget && typeof desktopApi.getCrawlRunContext === 'function') {
              try {
                const runContext = await desktopApi.getCrawlRunContext();
                magnetTarget = String(
                  (runContext && runContext.preferredMagnetPath) ||
                    (runContext && runContext.preferredOutputDir) ||
                    ''
                ).trim();
              } catch (_) {
                // 打开询问仍需显示，路径解析失败交给打开动作统一反馈。
              }
            }

            const magnetResponse = await desktopApi.showAlert({
              type: 'question',
              title: '打开磁力链接文件',
              message: '是否打开磁力链接文件？',
              buttons: ['打开磁力链接文件', '关闭']
            });
            const magnetSelection = String(
              magnetResponse && magnetResponse.selection ? magnetResponse.selection : ''
            ).trim();
            if (
              magnetSelection.includes('打开') &&
              typeof desktopApi.openMagnetFile === 'function'
            ) {
              await desktopApi.openMagnetFile(magnetTarget);
            }
          }
        } catch (_) {
          // 完成提示失败不能影响抓取结果回收。
        }
      } else if (typeof globalScope.alert === 'function') {
        globalScope.alert(`${title}\n${body}`);
      }
    }

    function appendLog(level, message, timestamp = new Date().toISOString()) {
      logController.appendLog(level, message, timestamp);
    }

    function appendEntry(entry) {
      if (!entry) {
        return;
      }
      appendLog(entry.level, entry.message, entry.timestamp);
    }

    function queryOptionalPanel(queryFn) {
      // Optional panel queries stay behind one helper so startup hydration does
      // not duplicate "catch and continue" logic in several branches.
      if (platformBridge && typeof platformBridge.invokeOptionalQuery === 'function') {
        return platformBridge.invokeOptionalQuery(queryFn);
      }
      return typeof queryFn === 'function' ? queryFn().catch(() => null) : Promise.resolve(null);
    }

    // Panel hydration helpers below are intentionally tolerant:
    // missing optional feeds should degrade to "skip this panel" instead of
    // crashing the whole renderer bootstrap path.

    // Crawl log events can arrive from two compatibility shapes:
    // 1) legacy flat entries: { level, message, timestamp }
    // 2) bridge-style envelopes: { event, domain, data: { ...entry } }
    // Keep the renderer tolerant to both so sidecar/bridge refactors do not
    // silently blank the visible log panel.
    function unwrapLogEntry(entry) {
      if (!entry) {
        return null;
      }

      const candidate =
        entry.data && typeof entry.data === 'object' && !Array.isArray(entry.data) ? entry.data : entry;
      const message = typeof candidate.message === 'string' ? candidate.message : '';
      if (!message.trim()) {
        return null;
      }

      return {
        level: typeof candidate.level === 'string' ? candidate.level : 'info',
        message,
        timestamp: candidate.timestamp || entry.timestamp || new Date().toISOString()
      };
    }

    function bindExternalLink(linkElement) {
      if (!linkElement) {
        return;
      }

      linkElement.addEventListener('click', async (event) => {
        const targetUrl = linkElement.href || linkElement.getAttribute('href');
        if (!targetUrl) {
          return;
        }

        event.preventDefault();
        await desktopApi.openExternal(targetUrl);
      });
    }

    function formatQualitySummary(summary) {
      if (summary && typeof summary.summaryLine === 'string' && summary.summaryLine.trim()) {
        return summary.summaryLine.trim();
      }

      const target = Number(summary && summary.targetLimit) || 0;
      const targetText = target > 0 ? String(target) : '未设目标';
      const validationText = summary && summary.secondValidationPassed ? '通过' : '未通过';
      const reportText = summary && summary.reportPath ? `；报告 ${summary.reportPath}` : '';

      return [
        `Go复盘摘要：${(summary && summary.statusText) || '已生成'}`,
        `目标 ${targetText}`,
        `唯一磁力 ${Number(summary && summary.magnetUnique) || 0}`,
        `影片记录 ${Number(summary && summary.filmRecordTotal) || 0}`,
        `二次校验 ${validationText}${reportText}`
      ].join('；');
    }

    function appendQualitySummaryLogs(summary) {
      const level =
        crawlPanelModel && typeof crawlPanelModel.resolveQualitySummaryLevel === 'function'
          ? crawlPanelModel.resolveQualitySummaryLevel(summary)
          : summary && typeof summary.noticeLevel === 'string' && summary.noticeLevel.trim()
            ? summary.noticeLevel.trim()
            : summary && summary.status === 'error'
              ? 'error'
              : summary && summary.status === 'warning'
                ? 'warn'
                : 'info';
      appendLog(level, formatQualitySummary(summary));

      const suggestionLines =
        crawlPanelModel && typeof crawlPanelModel.buildQualitySuggestionLines === 'function'
          ? crawlPanelModel.buildQualitySuggestionLines(summary)
          : [];
      suggestionLines.forEach((item) => {
        appendLog(item.level || 'info', `Go复盘建议：${item.line}`);
      });
    }

    // Quality-summary helpers form one closed loop:
    // fetch summary -> log summary/suggestions -> merge summary into result
    // panel. When crawl output counts look inconsistent, debug this section
    // before inspecting generic state or review panel rendering.
    function mergeQualitySummaryIntoResultPanel(summary) {
      if (!summary || !lastResultPanel) {
        return;
      }

      lastResultPanel = {
        ...lastResultPanel,
        qualityStatus: summary.status || lastResultPanel.qualityStatus || lastResultPanel.status || 'idle',
        qualityStatusText:
          summary.statusText || lastResultPanel.qualityStatusText || '尚未生成复盘摘要',
        qualityNoticeLevel: summary.noticeLevel || lastResultPanel.qualityNoticeLevel || 'info',
        qualitySummaryLine:
          summary.summaryLine || lastResultPanel.qualitySummaryLine || lastResultPanel.message || '',
        qualityCompletedAt: summary.completedAt || lastResultPanel.qualityCompletedAt || '',
        qualityDurationSec:
          Number.isFinite(Number(summary.durationSeconds))
            ? Number(summary.durationSeconds)
            : lastResultPanel.qualityDurationSec || 0,
        reportPath: summary.reportPath || lastResultPanel.reportPath || '',
        latestLogPath: summary.latestLogPath || lastResultPanel.latestLogPath || '',
        logDir: summary.logDir || lastResultPanel.logDir || '',
        outputDir: summary.outputDir || lastResultPanel.outputDir || '',
        filmDataPath: summary.filmDataPath || lastResultPanel.filmDataPath || '',
        magnetPath: summary.magnetPath || lastResultPanel.magnetPath || '',
        reportExists:
          typeof lastResultPanel.reportExists === 'boolean'
            ? lastResultPanel.reportExists
            : Boolean(summary.reportPath),
        latestLogExists:
          typeof lastResultPanel.latestLogExists === 'boolean'
            ? lastResultPanel.latestLogExists
            : Boolean(summary.latestLogPath),
        logDirExists:
          typeof lastResultPanel.logDirExists === 'boolean'
            ? lastResultPanel.logDirExists
            : Boolean(summary.logDir),
        outputDirExists:
          typeof lastResultPanel.outputDirExists === 'boolean'
            ? lastResultPanel.outputDirExists
            : Boolean(summary.outputDir),
        filmDataExists:
          typeof lastResultPanel.filmDataExists === 'boolean'
            ? lastResultPanel.filmDataExists
            : Boolean(summary.filmDataPath),
        magnetExists:
          typeof lastResultPanel.magnetExists === 'boolean'
            ? lastResultPanel.magnetExists
            : Boolean(summary.magnetPath)
      };

      stateController.applyResultPanel(lastResultPanel);
    }

    // Quality-summary refresh is a post-run read-model query. Keep it separate
    // from live event feeds so "crawl execution failed" and "summary/report
    // rendering failed" can be diagnosed as two different layers.
    async function refreshRunQualitySummary(state) {
      if (!desktopApi || typeof desktopApi.getRunQualitySummary !== 'function') {
        return;
      }

      const requestModel =
        crawlPanelModel && typeof crawlPanelModel.buildRunQualitySummaryRequest === 'function'
          ? crawlPanelModel.buildRunQualitySummaryRequest(state, elements.output.value)
          : {
              outputDir: String(
                (state && (state.outputDir || state.currentTaskOutputDir || state.targetOutput)) ||
                  elements.output.value ||
                  ''
              ).trim(),
              signature: `${String(
                (state && (state.outputDir || state.currentTaskOutputDir || state.targetOutput)) ||
                  elements.output.value ||
                  ''
              ).trim()}|${state && state.message ? state.message : ''}`
            };
      const outputDir = String(requestModel && requestModel.outputDir ? requestModel.outputDir : '').trim();
      const signature = String(requestModel && requestModel.signature ? requestModel.signature : '').trim();
      // Signature-based dedupe keeps quality-summary refresh tied to meaningful
      // crawl-state changes instead of every transient UI event.
      if (signature === lastQualitySummarySignature) {
        return;
      }
      lastQualitySummarySignature = signature;

      try {
        const summary = await desktopApi.getRunQualitySummary({
          outputDir,
          writeReport: true
        });
        appendQualitySummaryLogs(summary);
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error || '未知错误');
        appendLog('warn', `Go复盘摘要生成失败：${message}`);
      }
    }

    function applyRunContext(context) {
      // Run-context updates only project visible log paths and related metadata.
      // They should not be treated as crawl-state transitions.
      if (!context) {
        return;
      }

      logController.updateLogContext(context);

      if (context.sessionLogPath && context.sessionLogPath !== lastAnnouncedLogPath) {
        lastAnnouncedLogPath = context.sessionLogPath;
        appendLog('info', `${UI_TEXT.log.createdEventPrefix}${context.sessionLogPath}`);
      }
    }

    // Task-snapshot hydration is the startup fallback when the live event feeds
    // are not yet replaying. Keep this section side-effect light so bootstrap
    // bugs can be separated from real-time event duplication.
    function normalizeTaskSnapshotStatus(snapshot) {
      if (crawlPanelModel && typeof crawlPanelModel.normalizeTaskSnapshotStatus === 'function') {
        return crawlPanelModel.normalizeTaskSnapshotStatus(snapshot);
      }
      return String((snapshot && (snapshot.controllerStatus || snapshot.lastCrawlStatus)) || 'idle').trim().toLowerCase();
    }

    function buildStagePanelFromTaskSnapshot(snapshot) {
      // Snapshot fallback shaping stays behind the shared panel model so this
      // controller does not fork startup panel contracts from renderer peers.
      if (crawlPanelModel && typeof crawlPanelModel.buildStagePanelFromTaskSnapshot === 'function') {
        return crawlPanelModel.buildStagePanelFromTaskSnapshot(snapshot, {
          defaultMessage: UI_TEXT.state.defaultMessage
        });
      }
      return null;
    }

    function buildResultPanelFromTaskSnapshot(snapshot) {
      if (crawlPanelModel && typeof crawlPanelModel.buildResultPanelFromTaskSnapshot === 'function') {
        return crawlPanelModel.buildResultPanelFromTaskSnapshot(snapshot, {
          statusLabels: STATUS_LABELS
        });
      }
      return null;
    }

    function applyTaskSnapshot(snapshot) {
      if (!snapshot) {
        return;
      }

      const status = normalizeTaskSnapshotStatus(snapshot);
      const message = String(snapshot.lastCrawlMessage || UI_TEXT.state.defaultMessage).trim() || UI_TEXT.state.defaultMessage;

      applyRunContext({
        logDir: snapshot.logDir || '',
        sessionLogPath: snapshot.sessionLogPath || '',
        latestLogPath: snapshot.latestLogPath || ''
      });
      stateController.setStatus(status, message);

      const stagePanel = buildStagePanelFromTaskSnapshot(snapshot);
      if (stagePanel) {
        stateController.applyStagePanel(stagePanel);
      }

      const resultPanel = buildResultPanelFromTaskSnapshot(snapshot);
      if (resultPanel) {
        stateController.applyResultPanel(resultPanel);
      }
    }

    function buildReviewPanelFallbackFromState(state) {
      if (
        crawlPanelModel &&
        typeof crawlPanelModel.buildReviewPanelFallbackFromState === 'function'
      ) {
        // Legacy raw `crawl.state` payloads remain only as a compatibility
        // fallback. The preferred path is the dedicated Go `crawl.review-panel`
        // contract, so keep this branch narrow and side-effect free.
        return crawlPanelModel.buildReviewPanelFallbackFromState(state);
      }
      return null;
    }

    // Event-feed ownership is split deliberately:
    // 1) Go primary feeds are the preferred contracts
    // 2) compatibility fallbacks only patch missing contracts
    // 3) log/sidecar lifecycle feeds stay orthogonal
    // If the renderer shows duplicate state, first identify which feed family
    // produced it before changing panel logic.
    function bindGoPrimaryCrawlEvents() {
      if (supportsGoUiState) {
        trackSubscription(desktopApi.onUiState((state) => {
          if (isSubscriptionBridgeSessionActive()) {
            return;
          }
          stateController.enqueueUiState(state || {});
        }));
      }

      if (supportsGoStagePanel) {
        trackSubscription(desktopApi.onStagePanel((panel) => {
          if (isSubscriptionBridgeSessionActive()) {
            return;
          }
          stateController.applyStagePanel(panel || {});
        }));
      }

      if (supportsGoResultPanel) {
        trackSubscription(desktopApi.onResultPanel((panel) => {
          if (isSubscriptionBridgeSessionActive()) {
            return;
          }
          stateController.applyResultPanel(panel || {});
        }));
      }

      if (supportsGoRunContext) {
        trackSubscription(desktopApi.onRunContext((context) => {
          if (isSubscriptionBridgeSessionActive()) {
            return;
          }
          applyRunContext(context || {});
        }));
      }

      if (supportsGoReviewPanel) {
        trackSubscription(desktopApi.onReviewPanel((panel) => {
          if (isSubscriptionBridgeSessionActive()) {
            return;
          }
          stateController.enqueueReviewPanel(panel || {});
        }));
      }

      if (supportsGoQualitySummary) {
        trackSubscription(desktopApi.onQualitySummary((summary) => {
          if (isSubscriptionBridgeSessionActive()) {
            return;
          }
          const signature =
            crawlPanelModel && typeof crawlPanelModel.buildQualitySummaryEventSignature === 'function'
              ? crawlPanelModel.buildQualitySummaryEventSignature(summary)
              : `${String((summary && summary.outputDir) || '').trim()}|${String(
                  (summary && summary.reportPath) || ''
                ).trim()}|${summary && summary.status ? summary.status : ''}|${String(
                  (summary && summary.completedAt) || ''
                ).trim()}|${String((summary && summary.durationSeconds) || '')}|${String(
                  (summary && summary.summaryLine) || ''
                ).trim()}`;
          if (signature === lastQualitySummarySignature) {
            return;
          }
          lastQualitySummarySignature = signature;
          appendQualitySummaryLogs(summary);

          const outputDir = String((summary && summary.outputDir) || '').trim();
          if (outputDir) {
            autoRefreshWorkspacesOnCrawlCompleted(outputDir);
          }
          if (summary && (summary.status || summary.completed === true)) {
            void notifyCrawlCompletion(summary.status, summary.summaryLine || summary.message, summary);
          }
        }));
      }
    }

    function consumeLegacyStateFallback(state) {
      // 每次新的抓取运行时重置自动刷新标记，确保同一输出目录的后续完成仍能刷新。
      const currentStatus = String((state && state.status) || '').toLowerCase();
      if (['starting', 'running'].includes(currentStatus)) {
        // 同一输出目录可以被重新爬取。新任务开始时清除上一轮通知标记，
        // 但仍保留同一轮事件源之间的去重能力。
        lastNotificationSignature = '';
      }
      if (currentStatus !== 'completed') {
        lastAutoRefreshSignature = '';
      }

      // 仅在从运行态首次进入终止态时弹出完成通知，避免重连/重复事件反复弹窗。
      const terminalStatuses = ['completed', 'error', 'stopped', 'incomplete'];
      const wasRunning = ['starting', 'running'].includes(previousCrawlStatus);
      if (wasRunning && terminalStatuses.includes(currentStatus)) {
        void notifyCrawlCompletion(
          currentStatus,
          state && state.message,
          Object.assign({}, lastResultPanel || {}, state || {})
        );
      }
      previousCrawlStatus = currentStatus;

      if (!supportsGoUiState) {
        const baseState = {
          status: state.status,
          message: state.message,
          stats: state.stats,
          activeItems: state.activeItems || []
        };

        if (!supportsGoReviewPanel) {
          const reviewFallback = buildReviewPanelFallbackFromState(state);
          if (reviewFallback) {
            baseState.duplicateItems = reviewFallback.duplicateItems;
            baseState.duplicateItemsTotal = reviewFallback.duplicateItemsTotal;
            baseState.unfinishedItems = reviewFallback.unfinishedItems;
            baseState.unfinishedItemsTotal = reviewFallback.unfinishedItemsTotal;
            baseState.pageGapItems = reviewFallback.pageGapItems;
            baseState.failedDetails = reviewFallback.failedDetails;
            baseState.failedDetailsTotal = reviewFallback.failedDetailsTotal;
          }
        }

        stateController.enqueueState(baseState);
      } else if (!supportsGoReviewPanel && state) {
        const reviewFallback = buildReviewPanelFallbackFromState(state);
        if (reviewFallback) {
          stateController.enqueueReviewPanel(reviewFallback);
        }
      }

      if (!supportsGoQualitySummary && state && state.status === 'completed') {
        void refreshRunQualitySummary(state);
        const outputDir = String(
          (state && (state.outputDir || state.currentTaskOutputDir || state.targetOutput)) ||
            elements.output.value ||
            ''
        ).trim();
        if (outputDir) {
          autoRefreshWorkspacesOnCrawlCompleted(outputDir);
        }
      }
    }

    function bindCompatibilityCrawlFallbacks() {
      trackSubscription(desktopApi.onState((state) => {
        if (isSubscriptionBridgeSessionActive()) {
          return;
        }
        consumeLegacyStateFallback(state || {});
      }));

      trackSubscription(desktopApi.onLogContext((context) => {
        if (supportsGoRunContext) {
          return;
        }

        applyRunContext(context || {});
      }));
    }

    function bindLogFeed() {
      trackSubscription(desktopApi.onLog((payload) => {
        if (isSubscriptionBridgeSessionActive()) {
          return;
        }
        const entries = Array.isArray(payload) ? payload : [payload];

        entries.forEach((entry) => {
          const normalizedEntry = unwrapLogEntry(entry);
          if (!normalizedEntry) {
            return;
          }

          appendEntry(normalizedEntry);
        });
      }));
    }

    function bindSidecarLifecycleFeeds() {
      if (typeof desktopApi.onSidecarLifecycle === 'function') {
        trackSubscription(desktopApi.onSidecarLifecycle((payload) => {
          if (isSubscriptionBridgeSessionActive()) {
            return;
          }
          if (!payload || !payload.message) {
            return;
          }

          const level = payload.state === 'error' ? 'error' : payload.state === 'stopped' ? 'warn' : 'info';
          appendLog(level, payload.message);

          if (payload.state === 'error') {
            stateController.setStatus('idle', payload.message);
          }
        }));
      }

      if (typeof desktopApi.onAppNotice === 'function') {
        trackSubscription(desktopApi.onAppNotice((payload) => {
          if (isSubscriptionBridgeSessionActive()) {
            return;
          }
          if (!payload || !payload.message) {
            return;
          }

          const normalizedLevel = ['info', 'warn', 'error'].includes(payload.level) ? payload.level : 'info';
          appendLog(normalizedLevel, payload.message, payload.timestamp || new Date().toISOString());

          if (normalizedLevel === 'error') {
            stateController.setStatus('idle', payload.message);
          }
        }));
      }
    }

    function disposeFeeds() {
      feedDisposers.forEach((dispose) => {
        try {
          dispose();
        } catch (error) {
          // 取消订阅失败不应阻塞关闭流程。
        }
      });
      feedDisposers = [];
    }

    function trackSubscription(dispose) {
      if (typeof dispose === 'function') {
        feedDisposers.push(dispose);
      }
    }

    // Bootstrap applies one-time snapshots first, then subscribes to ongoing
    // feeds. That ordering keeps startup deterministic and avoids mixing partial
    // live payloads into empty panels before baseline hydration finishes.
    // Feed binding is one-shot. Once live feeds are connected, later panel
    // changes should come from events or explicit snapshot queries, not from
    // rebinding duplicate listeners.
    function bindEventFeeds() {
      if (feedsBound) {
        return;
      }

      feedsBound = true;
      bindLogFeed();
      bindGoPrimaryCrawlEvents();
      bindCompatibilityCrawlFallbacks();
      bindSidecarLifecycleFeeds();
      window.addEventListener('beforeunload', disposeFeeds);
    }

    function bootstrapCrawlPanels(taskSnapshot, stagePanel, resultPanel, reviewPanel) {
      // Startup hydration applies snapshot first, then richer panel payloads.
      // The state controller already dedupes equivalent signatures, so bootstrap
      // can stay explicit without inventing a second precedence model here.
      if (taskSnapshot) {
        applyTaskSnapshot(taskSnapshot);
      }
      if (stagePanel) {
        stateController.applyStagePanel(stagePanel);
      }
      if (resultPanel) {
        stateController.applyResultPanel(resultPanel);
      }
      if (reviewPanel) {
        stateController.enqueueReviewPanel(reviewPanel);
      }
    }

    function bindClearHistoryButton() {
      if (clearHistoryBound) {
        return;
      }

      clearHistoryBound = true;
      const clearHistoryButton = elements.crawlClearHistoryButton;
      if (!clearHistoryButton) {
        return;
      }

      clearHistoryButton.addEventListener('click', () => {
        if (globalThis.confirm && !globalThis.confirm('确定要清空所有结果入口历史记录吗？仅删除软件内记录，不会删除本地磁力和日志文件。')) {
          return;
        }
        stateController.clearResultHistory();
        appendLog('info', '结果入口历史记录已清空。');
      });
    }

    function bootstrapPanels() {
      if (panelsBootstrapped) {
        bindClearHistoryButton();
        return Promise.resolve();
      }

      if (bootstrapPanelsPromise) {
        return bootstrapPanelsPromise;
      }

      // Startup panel hydration is a read-only snapshot pass. Once it succeeds,
      // live feeds become the only authority; re-running bootstrap later would
      // risk replaying stale snapshots over newer runtime state.
      bootstrapPanelsPromise = Promise.all([
        queryOptionalPanel(typeof desktopApi.getCrawlTaskSnapshot === 'function' ? () => desktopApi.getCrawlTaskSnapshot() : null),
        queryOptionalPanel(typeof desktopApi.getCrawlStagePanel === 'function' ? () => desktopApi.getCrawlStagePanel() : null),
        queryOptionalPanel(typeof desktopApi.getCrawlResultPanel === 'function' ? () => desktopApi.getCrawlResultPanel() : null),
        queryOptionalPanel(typeof desktopApi.getCrawlReviewPanel === 'function' ? () => desktopApi.getCrawlReviewPanel() : null)
      ])
        .then(([taskSnapshot, stagePanel, resultPanel, reviewPanel]) => {
          bootstrapCrawlPanels(taskSnapshot, stagePanel, resultPanel, reviewPanel);
          bindClearHistoryButton();
          panelsBootstrapped = true;
        })
        .catch((error) => {
          bootstrapPanelsPromise = null;
          throw error;
        });

      return bootstrapPanelsPromise;
    }

    return {
      appendLog,
      bindExternalLink,
      bindEventFeeds,
      bootstrapPanels
    };
  }

  globalScope.desktopCrawlRuntimeController = {
    createCrawlRuntimeController
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
