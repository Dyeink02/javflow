// Organizer workspace controller for the current desktop renderer.
// It owns organizer-page input, progress projection, and action dispatch, but
// it should not absorb artifact-import helpers or dependency-install logic.
//
// Ownership summary:
// 1) own organizer workspace steady-state UI and action dispatch
// 2) hold organizer-page state only
// 3) delegate crawl-artifact import, dependency, and learning concerns to child
//    controllers instead of inlining them here
//
// File map for maintainers:
// 1) organizer input normalization helpers
// 2) organizer workspace state/bootstrap
// 3) child-controller coordination and organizer action handlers

(function initializeOrganizerController(globalScope) {
  const rendererHelpers = globalScope.desktopRendererHelpers || {};
  const getErrorMessage = rendererHelpers.getErrorMessage;
  const toSafeInteger = rendererHelpers.toSafeInteger;
  const appendTimestampedLogLine = rendererHelpers.appendTimestampedLogLine;
  const clearChildren = rendererHelpers.clearChildren;
  const safeLocalStorageGet = rendererHelpers.safeLocalStorageGet;
  const safeLocalStorageSet = rendererHelpers.safeLocalStorageSet;
  const bindAsyncClickHelper = rendererHelpers.bindAsyncClick || null;
  const createBufferedLogAppender = rendererHelpers.createBufferedLogAppender || null;

  if (
    !getErrorMessage ||
    !toSafeInteger ||
    !appendTimestampedLogLine ||
    !clearChildren ||
    !safeLocalStorageGet ||
    !safeLocalStorageSet
  ) {
    throw new Error('desktopRendererHelpers must be loaded before organizerController');
  }

  const ORGANIZER_SUFFIX_OPTIONS = Object.freeze(['-A', '-1', '_DUP1']);

  function normalizeOrganizerSuffix(rawValue) {
    const normalized = String(rawValue || '').trim().toUpperCase();
    return ORGANIZER_SUFFIX_OPTIONS.includes(normalized) ? normalized : '-A';
  }

  function normalizeKeywordText(rawValue) {
    const rawText = String(rawValue || '').trim();
    if (!rawText) {
      return [];
    }

    return Array.from(
      new Set(
        rawText
          .split(/[\r\n,，;；\s]+/)
          .map((item) => item.trim().toLowerCase())
          .filter(Boolean)
      )
    );
  }

  function normalizeAdModelType(rawValue) {
    const value = String(rawValue || '').trim().toLowerCase();
    if (value === 'squeezenet-fast' || value === 'yolov8n-balanced' || value === 'mobile-net-v3-lite') {
      return value;
    }
    return 'mobile-net-v3-lite';
  }

  function appendLogLine(logView, level, message, timestamp) {
    appendTimestampedLogLine(logView, level, message, timestamp, { tagName: 'p' });
  }

  function setDisabled(element, disabled) {
    if (element) {
      element.disabled = disabled;
    }
  }

  // createOrganizerController owns organizer-page state/action wiring only.
  // Artifact-import and dependency/ad-learning concerns stay delegated to the
  // child controllers created below.
  function createOrganizerController(options) {
    const { elements, desktopApi } = options;
    const progressSchema = globalScope.desktopProgressSchema || null;
    const organizerReviewView = globalScope.desktopOrganizerReviewView || null;
    const dependencyControllerFactory = globalScope.desktopOrganizerDependencyController || null;
    const learningControllerFactory = globalScope.desktopOrganizerLearningController || null;
    const crawlOutputControllerFactory = globalScope.desktopOrganizerCrawlOutputController || null;
    if (!organizerReviewView) {
      throw new Error('desktopOrganizerReviewView is required before organizerController');
    }
    // Organizer workflow map:
    // 1) collect organizer-page inputs
    // 2) coordinate child controllers for artifact import / learning /
    //    dependency state
    // 3) dispatch organizer actions
    // 4) project organizer-only runtime state/logs/results
    //
    // File-organization rules belong to the backend organizer service. This
    // controller should stay on page-level coordination and projection.

    const STORAGE_KEYS = {
      organizerGuideShown: 'jav.organizer.guide.v1.shown'
    };
    const ORGANIZER_TOTAL_STAGES = 7;
    const messages = {
      idle: '等待开始整理。',
      running: '正在整理视频文件，请稍候...',
      complete: '视频整理完成。',
      previewComplete: '预览扫描完成，未移动任何文件。',
      ready: '视频整理模块已就绪。',
      rootRequired: '请先选择要整理的根目录。',
      minSizeInvalid: '最小体积必须大于等于 1MB。',
      codeRequired: '请先加载爬虫番号名单，再执行严格匹配整理。',
      crawlOutputRequired: '请先填写或自动带入整理快照、filmData.json 或爬虫结果目录。'
    };

    // Organizer renderer state owns only organizer-page runtime projection.
    // Crawl artifacts may be imported into preloadedExpected, but crawler form
    // state and crawler runtime state must remain outside this controller.
    const state = {
      running: false,
      activeTask: '',
      preloadedExpected: null,
      adSummary: null,
      latestResult: null,
      dependencyStatus: null,
      dependencyInstalling: '',
      dependencyProgressMessage: ''
    };
    const organizerHeroText = {
      subtitle: '全自动改名、整理、删除、排查开头广告视频。',
      tip: '建议通过CloudDrive2将网盘挂载至本地进行整理更加方便。或将所有jav爬虫视频下载本地磁盘进行整理'
    };
    // Child controllers split organizer UI by concern:
    // - learningController: AI/ad-detection settings + summary
    // - crawlOutputController: crawl-artifact import boundary
    // - dependencyController: optional dependency install/probe surface
    // Keep cross-calls explicit so organizer debugging can isolate the failing
    // area before stepping into business logic.
    let dependencyController = null;
    let learningController = null;
    let crawlOutputController = null;
    let eventsBound = false;
    let ipcEventsBound = false;
    let bootstrapCompleted = false;
    let bootstrapPromise = null;
    const organizerLogBuffer =
      typeof createBufferedLogAppender === 'function'
        ? createBufferedLogAppender({
            logView: elements.organizerLogView,
            tagName: 'p'
          })
        : null;

    function appendOrganizerLog(level, message, timestamp) {
      // Organizer UI keeps one buffered log projection. Child controllers should
      // write through this helper instead of building parallel log surfaces.
      if (organizerLogBuffer) {
        organizerLogBuffer.append(level, message, timestamp);
        return;
      }
      appendLogLine(elements.organizerLogView, level, message, timestamp);
    }

    function getOrganizerRootPath() {
      return String(elements.organizerRoot && elements.organizerRoot.value ? elements.organizerRoot.value : '').trim();
    }

    function bindAsyncClick(button, handler, onError) {
      if (typeof bindAsyncClickHelper === 'function') {
        bindAsyncClickHelper(button, handler, {
          onError,
          fallbackErrorHandler: (error) => appendOrganizerLog('error', getErrorMessage(error))
        });
        return;
      }

      if (!button) {
        return;
      }

      button.addEventListener('click', async () => {
        try {
          await handler();
        } catch (error) {
          if (typeof onError === 'function') {
            onError(error);
            return;
          }
          appendOrganizerLog('error', getErrorMessage(error));
        }
      });
    }

    function bindOpenOrganizerPathButton(button, targetKind) {
      // Open-path actions stay thin and local: validate root, then delegate to
      // desktopApi without mixing organizer task semantics into path buttons.
      bindAsyncClick(button, async () => {
        const rootPath = getOrganizerRootPath();
        if (!rootPath) {
          appendOrganizerLog('warn', messages.rootRequired);
          return;
        }

        await desktopApi.openOrganizerPath(rootPath, targetKind);
      });
    }

    function setStatus(status, message = '') {
      const normalizedStatus =
        status === 'running' || status === 'starting' || status === 'completed' || status === 'error'
          ? status
          : 'idle';
      if (!elements.organizerStatusPill) {
        return;
      }

      elements.organizerStatusPill.className = `status-pill ${normalizedStatus}`;
      elements.organizerStatusPill.textContent = message || messages.idle;
    }

    function setSummaryCounts(summary = {}) {
      // Summary counters are presentation state derived from the latest result
      // envelope. They should not become a second source of organizer truth.
      if (elements.organizerScanned) {
        elements.organizerScanned.textContent = String(summary.scannedTotal ?? 0);
      }
      if (elements.organizerVideoTotal) {
        elements.organizerVideoTotal.textContent = String(summary.videoTotal ?? 0);
      }
      if (elements.organizerMatched) {
        elements.organizerMatched.textContent = String(summary.qualifiedVideo ?? 0);
      }
      if (elements.organizerMovedUnmatched) {
        elements.organizerMovedUnmatched.textContent = String(summary.movedToUnmatched ?? 0);
      }
      if (elements.organizerMovedDelete) {
        elements.organizerMovedDelete.textContent = String(summary.movedToDelete ?? 0);
      }
      if (elements.organizerFailed) {
        elements.organizerFailed.textContent = String(summary.failedOperations ?? 0);
      }
    }

    function bindOpenReport(button, filePath) {
      bindAsyncClick(button, async () => {
        await desktopApi.openPath(filePath);
      });
    }

    function setSummaryMessage(message) {
      if (elements.organizerSummaryMessage) {
        elements.organizerSummaryMessage.textContent = message || messages.idle;
      }
    }

    function toSafeNumber(value, fallback = 0) {
      const parsed = Number(value);
      return Number.isFinite(parsed) ? parsed : fallback;
    }

    function clampNumber(value, minValue, maxValue) {
      return Math.min(maxValue, Math.max(minValue, value));
    }

    function buildRatio(processed, total, fallback = 0) {
      if (total <= 0) {
        return fallback;
      }
      return clampNumber(processed / total, 0, 1);
    }

    function buildProgressHeadline(percent, stageIndex) {
      return `已整理：${percent}% 当前阶段：${stageIndex}/${ORGANIZER_TOTAL_STAGES}`;
    }

    function shortenProgressPath(value, maxLength = 88) {
      if (progressSchema && typeof progressSchema.shortenProgressPath === 'function') {
        return progressSchema.shortenProgressPath(value, maxLength);
      }
      const path = String(value || '').trim();
      return path.length > maxLength ? `${path.slice(0, maxLength - 28)}...${path.slice(-24)}` : path;
    }

    function buildIdleSummary() {
      return `已整理：0% 当前阶段：0/${ORGANIZER_TOTAL_STAGES}\n正在执行：等待开始整理`;
    }

    function buildCompletedSummary(summary = {}, dryRun = false) {
      const completionLabel = dryRun ? '预览完成' : '整理完成';
      return `${buildProgressHeadline(100, ORGANIZER_TOTAL_STAGES)}\n${completionLabel}：扫描 ${
        summary.scannedTotal || 0
      } 个，视频 ${summary.videoTotal || 0} 个，命中番号 ${summary.qualifiedVideo || 0} 个，未命中番号 ${
        summary.movedToUnmatched || 0
      } 个，待删除文件 ${summary.movedToDelete || 0} 个，失败 ${summary.failedOperations || 0} 个。`;
    }

    function buildRunningSummary(percent, stageIndex, actionLabel) {
      return `${buildProgressHeadline(percent, stageIndex)}\n正在执行：${actionLabel}`;
    }

    function buildOrganizerProgressView(progress = {}) {
      const phase = String(progress.phase || '').trim();
      const organizerPhases =
        progressSchema && progressSchema.ORGANIZER_PROGRESS_PHASES ? progressSchema.ORGANIZER_PROGRESS_PHASES : {};
      const total = toSafeNumber(progress.total, 0);
      const processed = toSafeNumber(progress.processed, 0);
      const adFileAction = normalizeAdFileAction(progress.adFileAction);

      let completedStagesBefore = 0;
      let stageIndex = 0;
      let stageProgress = 0;
      let actionLabel = '';

      if (phase === organizerPhases.starting || phase === 'starting') {
        completedStagesBefore = 0;
        stageIndex = 1;
        stageProgress = 0.25;
        actionLabel = '读取整理配置、准备目录';
      } else if (phase === organizerPhases.scanStart || phase === organizerPhases.scanProgress || phase === 'scan-start' || phase === 'scan-progress') {
        completedStagesBefore = 1;
        stageIndex = 2;
        stageProgress = buildRatio(processed, total, phase === organizerPhases.scanStart || phase === 'scan-start' ? 0 : 0.2);
        actionLabel = '扫描目录、识别有效视频';
      } else if (phase === organizerPhases.scanCompleted || phase === 'scan-completed') {
        completedStagesBefore = 1;
        stageIndex = 2;
        stageProgress = 1;
        actionLabel = '扫描完成、生成待整理名单';
      } else if (phase === organizerPhases.waitingStart || phase === organizerPhases.waitingProgress || phase === 'waiting-start' || phase === 'waiting-progress') {
        const ratio = buildRatio(processed, total, phase === organizerPhases.waitingStart || phase === 'waiting-start' ? 0 : 0.2);
        if (ratio < 0.5) {
          completedStagesBefore = 2;
          stageIndex = 3;
          stageProgress = ratio * 2;
          actionLabel = '视频改名';
        } else {
          completedStagesBefore = 3;
          stageIndex = 4;
          stageProgress = (ratio - 0.5) * 2;
          actionLabel = '移入待整理';
        }
      } else if (phase === organizerPhases.deleteStart || phase === organizerPhases.deleteProgress || phase === 'delete-start' || phase === 'delete-progress') {
        completedStagesBefore = 4;
        stageIndex = 5;
        stageProgress = buildRatio(processed, total, phase === organizerPhases.deleteStart || phase === 'delete-start' ? 0 : 0.25);
        actionLabel = adFileAction === 'delete-directly' ? '删除视频、清理广告文件' : '移入待删除、归档广告文件';
      } else if (phase === organizerPhases.introAdStart || phase === organizerPhases.introAdProgress || phase === 'intro-ad-start' || phase === 'intro-ad-progress') {
        completedStagesBefore = 5;
        stageIndex = 6;
        stageProgress = buildRatio(processed, total, phase === organizerPhases.introAdStart || phase === 'intro-ad-start' ? 0 : 0.25);
        actionLabel = '识别开头广告、复核并输出报告';
      } else if (phase === organizerPhases.finalizeStart || phase === organizerPhases.finalizeProgress || phase === 'finalize-start' || phase === 'finalize-progress') {
        const finalizeTotal = toSafeNumber(progress.finalizeTotal, 4);
        const finalizeProcessed = toSafeNumber(progress.finalizeProcessed, 0);
        completedStagesBefore = 6;
        stageIndex = 7;
        stageProgress = buildRatio(finalizeProcessed, finalizeTotal, phase === organizerPhases.finalizeStart || phase === 'finalize-start' ? 0.05 : 0.1);
        const operation = String(progress.operation || '').trim() || '整理收尾';
        const currentPath = shortenProgressPath(progress.currentPath);
        actionLabel = currentPath ? `${operation}：${currentPath}` : operation;
      } else if (phase === organizerPhases.completed || phase === 'completed') {
        completedStagesBefore = 6;
        stageIndex = 7;
        stageProgress = 1;
        actionLabel = '整理完成';
      } else {
        return null;
      }

      const percent = clampNumber(
        Math.round(((completedStagesBefore + clampNumber(stageProgress, 0, 1)) / ORGANIZER_TOTAL_STAGES) * 100),
        stageIndex > 0 ? 1 : 0,
        100
      );

      return {
        percent,
        stageIndex,
        actionLabel,
        headline: buildProgressHeadline(percent, stageIndex),
        summaryText: buildRunningSummary(percent, stageIndex, actionLabel)
      };
    }

    function applyOrganizerHeroCopy() {
      // Hero copy remains a shell concern so feature controllers and backend
      // services do not have to carry display-only onboarding text.
      if (elements.organizerHeroSubtitle) {
        elements.organizerHeroSubtitle.textContent = organizerHeroText.subtitle;
      }

      if (elements.organizerHeroTip) {
        elements.organizerHeroTip.textContent = organizerHeroText.tip;
      }
    }

    function buildProgressMessage(progress = {}) {
      if (progressSchema && typeof progressSchema.buildProgressMessage === 'function') {
        return progressSchema.buildProgressMessage(progress);
      }
      return '';
    }

    function renderReviewPanel(result = null) {
      // Review rendering stays delegated to the dedicated view so this
      // controller keeps orchestration/state ownership rather than DOM detail.
      organizerReviewView.renderReviewPanel(elements.organizerReviewPanel, result, {
        onBindOpenReport: bindOpenReport
      });
    }

    async function showFirstLaunchGuideIfNeeded() {
      safeLocalStorageSet(STORAGE_KEYS.organizerGuideShown, '1');
    }

    // Progress text is now schema-driven. Keep only the current implementation
    // so organizer and learning progress do not maintain duplicate renderers.
    function applyRuntimeProgress(progress = {}) {
      const scope = String(progress.scope || '');
      const phase = String(progress.phase || '');

      if (scope === 'organizer') {
        const isScanPhase = phase === 'scan-start' || phase === 'scan-progress' || phase === 'scan-completed';
        if (isScanPhase && elements.organizerScanned && Number.isFinite(Number(progress.processed))) {
          elements.organizerScanned.textContent = String(progress.processed);
        }
        if (isScanPhase && elements.organizerVideoTotal && Number.isFinite(Number(progress.videoTotal))) {
          elements.organizerVideoTotal.textContent = String(progress.videoTotal);
        }
        if (isScanPhase && elements.organizerMatched && Number.isFinite(Number(progress.qualifiedVideo))) {
          elements.organizerMatched.textContent = String(progress.qualifiedVideo);
        }
        if (elements.organizerMovedDelete && Number.isFinite(Number(progress.processed)) && phase.startsWith('delete')) {
          elements.organizerMovedDelete.textContent = String(progress.processed);
        }
      }

      const progressView = buildOrganizerProgressView(progress);
      if (progressView) {
        setStatus('running', progressView.headline);
        setSummaryMessage(progressView.summaryText);
        return;
      }

      const summaryText = buildProgressMessage(progress);
      if (summaryText) {
        setSummaryMessage(summaryText);
      }
    }

    function setActionButtonState() {
      const disabled = state.running;
      [
        elements.organizerStartButton,
        elements.organizerPreviewButton,
        elements.organizerRescueNamesButton,
        elements.organizerLoadCodesButton,
        elements.organizerDiscoverCodesButton,
        elements.organizerUseLatestOutputButton,
        elements.organizerLearningCodes,
        elements.organizerImportAdSamplesButton,
        elements.organizerHelpImportAdButton,
        elements.organizerImportNormalSamplesButton,
        elements.organizerHelpImportNormalButton,
        elements.organizerLearnAdByCodesButton,
        elements.organizerHelpLearnAdButton,
        elements.organizerLearnNormalByCodesButton,
        elements.organizerHelpLearnNormalButton,
        elements.organizerRefreshLearningSummaryButton,
        elements.organizerRefreshDependencyButton,
        elements.organizerAdFileActionMove,
        elements.organizerAdFileActionDelete,
        elements.organizerAdDetectionEnabled,
        elements.organizerAdDetectionEnable,
        elements.organizerAdDetectionDisable,
        elements.organizerAdModelType
      ].forEach((element) => setDisabled(element, disabled));
      if (dependencyController) {
        dependencyController.syncDependencyButtonState();
      }
      applyAdDetectionUiState();
    }

    function isAdDetectionEnabled() {
      return learningController.isAdDetectionEnabled();
    }
    function applyAdDetectionUiState() {
      learningController.applyAdDetectionUiState();
    }
    function getLearningConfig() {
      return learningController.getLearningConfig();
    }
    function getSelectedAdFileAction() {
      if (elements.organizerAdFileActionDelete && elements.organizerAdFileActionDelete.checked) {
        return 'delete-directly';
      }
      if (elements.organizerAdFileActionMove && elements.organizerAdFileActionMove.checked) {
        return 'move-to-delete';
      }
      return 'move-to-delete';
    }

    function normalizeAdFileAction(rawValue) {
      if (progressSchema && typeof progressSchema.normalizeAdFileAction === 'function') {
        return progressSchema.normalizeAdFileAction(rawValue);
      }
      return String(rawValue || '').trim() === 'delete-directly' ? 'delete-directly' : 'move-to-delete';
    }

    function getAdFileActionLabel(action) {
      return action === 'delete-directly' ? '直接删除广告文件' : '移入待删除';
    }

    function buildOrganizerProgressSummary(progress = {}) {
      const progressView = buildOrganizerProgressView(progress);
      return progressView ? progressView.summaryText : buildProgressMessage(progress);
    }

    // Organizer still keeps one compatibility textbox for imported crawl artifacts.
    // The value may be an organizer snapshot, a filmData.json file, or a legacy
    // crawl output directory. Bridge-side parsing owns the path resolution rules.
    //
    // Important boundary:
    // - preloadedExpected is the renderer-side source of truth
    // - crawlOutputDir is the fallback read-only import location
    // Renderer should forward the snapshot bundle directly instead of
    // re-expanding duplicate expectedCodes fields on the main Wails path.
    function getSettings(dryRun) {
      const learningConfig = getLearningConfig();
      const inputState =
        crawlOutputController && typeof crawlOutputController.getCurrentOrganizerInputState === 'function'
          ? crawlOutputController.getCurrentOrganizerInputState()
          : {
              crawlOutputDir: String(
                elements.organizerCrawlOutput && elements.organizerCrawlOutput.value
                  ? elements.organizerCrawlOutput.value
                  : ''
              ).trim(),
              preloadedExpected: null
            };

      return {
        rootPath: String(elements.organizerRoot && elements.organizerRoot.value ? elements.organizerRoot.value : '').trim(),
        minSizeMB: toSafeInteger(elements.organizerMinSize && elements.organizerMinSize.value, 100, 1),
        suffix: normalizeOrganizerSuffix(elements.organizerSuffix && elements.organizerSuffix.value),
        videoExtensions:
          String(
            elements.organizerVideoExtensions && elements.organizerVideoExtensions.value
              ? elements.organizerVideoExtensions.value
              : ''
          ).trim() || 'mp4, mkv, avi, mov, flv, wmv, ts, m4v, iso',
        adFileAction: normalizeAdFileAction(getSelectedAdFileAction()),
        dryRun,
        includeSubdirectories: Boolean(
          elements.organizerIncludeSubdirectories && elements.organizerIncludeSubdirectories.checked
        ),
        strictExpectedCodes: Boolean(elements.organizerStrictCodeMatch && elements.organizerStrictCodeMatch.checked),
        retryMissingMagnets: Boolean(elements.organizerRetryMissingMagnets && elements.organizerRetryMissingMagnets.checked),
        preloadedExpected: inputState.preloadedExpected,
        crawlOutputDir: inputState.crawlOutputDir,
        adDetectionEnabled: learningConfig.adDetectionEnabled,
        adModelType: learningConfig.adModelType,
        adThreshold: learningConfig.adThreshold,
        adKeywords: learningConfig.adKeywords,
        alistBaseURL: String(elements.organizerAlistUrl && elements.organizerAlistUrl.value ? elements.organizerAlistUrl.value : '').trim(),
        batchDelete: Boolean(elements.organizerBatchDelete && elements.organizerBatchDelete.checked),
        deleteIntervalMs: toSafeInteger(elements.organizerDeleteInterval && elements.organizerDeleteInterval.value, 500, 100),
        organizeIntervalMs: toSafeInteger(elements.organizerOrganizeInterval && elements.organizerOrganizeInterval.value, 500, 100)
      };
    }

    function validateSettings(settings) {
      if (!settings.rootPath) {
        throw new Error(messages.rootRequired);
      }

      if (!Number.isFinite(settings.minSizeMB) || settings.minSizeMB < 1) {
        throw new Error(messages.minSizeInvalid);
      }

      if (!settings.crawlOutputDir && !settings.rootPath) {
        throw new Error(messages.crawlOutputRequired);
      }

      if (
        settings.strictExpectedCodes &&
        !settings.preloadedExpected &&
        (!settings.crawlOutputDir || !String(settings.crawlOutputDir).trim()) &&
        (!settings.rootPath || !String(settings.rootPath).trim())
      ) {
        throw new Error(messages.codeRequired);
      }
    }

    async function runOrganizerTask(dryRun) {
      // Organizer 页面总控边界：
      // 1) 收集并校验页面设置
      // 2) 在需要时同步 AI 学习模型 / 依赖状态
      // 3) 调用 desktopApi.runOrganizer()
      // 4) 用返回结果刷新摘要、日志、复盘面板
      if (state.running) {
        return;
      }

      try {
        const settings = getSettings(dryRun);
        validateSettings(settings);
        if (!dryRun && settings.adFileAction === 'delete-directly') {
          let confirmMessage = '你已选择”直接删除广告文件”。此操作不可撤销，是否继续？';

          // 批量删除模式的额外确认
          if (settings.batchDelete) {
            confirmMessage = '你已启用”批量删除”模式。\n\n' +
              '将保留以下内容：\n' +
              '• 待整理/ (合格视频)\n' +
              '• 未命中/ (无法由番号名单唯一确认的视频)\n' +
              '• 待删除/ (已有待复核内容)\n' +
              '• 含开头广告/ (广告风险视频)\n' +
              '• logs/ (运行日志)\n' +
              '• 更改前后对照.txt 等6个报告文件\n\n' +
              '只会删除扫描阶段明确确认的广告/小文件；未知内容、隐藏目录和软件产物都会保留，不会创建临时删除目录。\n\n' +
              '此操作不可逆，是否继续？';
          }

          const confirmed = globalThis.confirm(confirmMessage);
          if (!confirmed) {
            appendLogLine(elements.organizerLogView, 'warn', '用户取消操作（未确认直接删除）。');
            return;
          }
        }
        if (settings.adDetectionEnabled) {
          await syncAdLearningModel({ logSuccess: false });
          await dependencyController.ensureAiDependenciesReady();
        }

        state.running = true;
        state.activeTask = 'organizer';
        setActionButtonState();
        setStatus('running', buildProgressHeadline(1, 1));
        setSummaryMessage(buildRunningSummary(1, 1, '读取整理配置、准备目录'));
        appendLogLine(
          elements.organizerLogView,
          'info',
          `${
            dryRun ? '开始预览扫描（不移动文件）...' : '开始执行整理（将处理文件）...'
          } 广告处理方式：${getAdFileActionLabel(settings.adFileAction)}。`
        );

        const result = await desktopApi.runOrganizer(settings);
        state.latestResult = result || null;
        const summary = result && result.summary ? result.summary : {};
        setSummaryCounts(summary);
        organizerReviewView.renderReportFiles(elements.organizerReportPaths, result && result.reportFiles ? result.reportFiles : [], {
          onBindOpenReport: bindOpenReport
        });
        renderReviewPanel(result);

        const finishMessage = dryRun ? messages.previewComplete : messages.complete;
        setSummaryMessage(buildCompletedSummary(summary, dryRun));
        appendLogLine(elements.organizerLogView, 'info', finishMessage);
        setStatus('completed', buildProgressHeadline(100, ORGANIZER_TOTAL_STAGES));
      } catch (error) {
        const message = getErrorMessage(error);
        appendLogLine(elements.organizerLogView, 'error', message);
        setSummaryMessage(message);
        setStatus('error', '整理异常');
      } finally {
        state.running = false;
        state.activeTask = '';
        setActionButtonState();
      }
    }

    async function runNameRescue() {
      if (state.running) {
        return;
      }

      const settings = getSettings(false);
      validateSettings(settings);
      const confirmed = globalThis.confirm(
        '番号名称抢救只使用已加载的番号名单和现有改名前后报告。\n\n' +
          '唯一命中的视频会按标准番号重新命名并移入“待整理”；无法唯一确认的会移入“未命中”。\n' +
          '该功能不会删除文件。是否继续？'
      );
      if (!confirmed) {
        return;
      }

      state.running = true;
      state.activeTask = 'name-rescue';
      setActionButtonState();
      setStatus('running', '正在抢救番号名称...');
      setSummaryMessage('正在读取番号名单、历史改名报告和视频路径。');
      appendOrganizerLog('info', '开始抢救番号名称：严格限定为已加载番号名单，不进行自由猜测。');

      try {
        const result = await desktopApi.rescueOrganizerNames(settings);
        const summary = {
          scannedTotal: result && result.scannedTotal,
          videoTotal: result && result.scannedTotal,
          qualifiedVideo: result && result.matchedTotal,
          movedToWaiting: result && result.matchedTotal,
          movedToUnmatched: result && result.unmatchedTotal,
          failedOperations: result && result.failedTotal
        };
        setSummaryCounts(summary);
        const reportFiles = result && result.reportPath ? [result.reportPath] : [];
        organizerReviewView.renderReportFiles(elements.organizerReportPaths, reportFiles, {
          onBindOpenReport: bindOpenReport
        });
        setSummaryMessage(
          `番号名称抢救完成：扫描 ${result.scannedTotal || 0}，恢复 ${result.matchedTotal || 0}，未命中 ${
            result.unmatchedTotal || 0
          }，失败 ${result.failedTotal || 0}。`
        );
        appendOrganizerLog(
          result.failedTotal > 0 ? 'warn' : 'info',
          `番号名称抢救完成：成功恢复 ${result.matchedTotal || 0} 个，移入未命中 ${result.unmatchedTotal || 0} 个，失败 ${
            result.failedTotal || 0
          } 个。`
        );
        setStatus(result.failedTotal > 0 ? 'error' : 'completed', result.failedTotal > 0 ? '抢救完成，存在失败' : '番号名称抢救完成');
      } catch (error) {
        const message = getErrorMessage(error);
        appendOrganizerLog('error', message);
        setSummaryMessage(message);
        setStatus('error', '番号名称抢救失败');
      } finally {
        state.running = false;
        state.activeTask = '';
        setActionButtonState();
      }
    }

    function bindEvents() {
      // 本文件只绑定 organizer 页面上的跨域动作。
      // 更细的子域行为分别留在 dependency / learning / crawl-output 子控制器。
      if (eventsBound) {
        return;
      }

      eventsBound = true;
      bindAsyncClick(elements.organizerBrowseRootButton, async () => {
        const selected = await desktopApi.chooseOrganizerRoot();
        if (!selected) {
          return;
        }

        elements.organizerRoot.value = selected;
        appendOrganizerLog('info', `已选择目标目录：${selected}`);
      });

      bindAsyncClick(elements.organizerRefreshDependencyButton, async () => {
        await dependencyController.refreshDependencyStatus(true);
      });

      bindAsyncClick(
        elements.organizerInstallFfmpegButton,
        async () => {
          await dependencyController.installDependency('ffmpeg');
        },
        () => {
          // Error already logged by installDependency.
        }
      );

      bindAsyncClick(
        elements.organizerInstallOnnxButton,
        async () => {
          await dependencyController.installDependency('onnx');
        },
        () => {
          // Error already logged by installDependency.
        }
      );

      bindAsyncClick(elements.organizerDeleteDependencyButton, async () => {
        await dependencyController.deleteDependencyState();
      });

      bindOpenOrganizerPathButton(elements.organizerOpenWaitingButton, 'waiting');
      bindOpenOrganizerPathButton(elements.organizerOpenOrganizedButton, 'root');
      bindOpenOrganizerPathButton(elements.organizerOpenUnmatchedButton, 'unmatched');
      bindOpenOrganizerPathButton(elements.organizerOpenDeleteButton, 'delete');

      bindAsyncClick(elements.organizerStartButton, async () => {
        await runOrganizerTask(Boolean(elements.organizerDryRun && elements.organizerDryRun.checked));
      });

      bindAsyncClick(elements.organizerPreviewButton, async () => {
        await runOrganizerTask(true);
      });

      bindAsyncClick(elements.organizerRescueNamesButton, runNameRescue);

      if (elements.organizerClearLogButton) {
        elements.organizerClearLogButton.addEventListener('click', () => {
          if (organizerLogBuffer) {
            organizerLogBuffer.clear();
          } else {
            clearChildren(elements.organizerLogView);
          }
          appendOrganizerLog('info', '整理日志已清空。');
        });
      }

      if (elements.organizerAlistUrl) {
        elements.organizerAlistUrl.addEventListener('change', () => {
          safeLocalStorageSet('jav.organizer.alist.url', elements.organizerAlistUrl.value);
        });
      }

      // 批量删除选项联动：根据广告处理方式启用/禁用批量删除勾选框
      function updateBatchDeleteState() {
        const isDeleteDirectly = elements.organizerAdFileActionDelete && elements.organizerAdFileActionDelete.checked;
        if (elements.organizerBatchDelete) {
          elements.organizerBatchDelete.disabled = !isDeleteDirectly;
          if (!isDeleteDirectly) {
            elements.organizerBatchDelete.checked = false;
          }
        }
        // 显示/隐藏速率配置
        const rateGrid = elements.organizerRateLimitGrid;
        if (rateGrid) {
          rateGrid.style.display = (elements.organizerBatchDelete && elements.organizerBatchDelete.checked) ? 'none' : '';
        }
      }

      if (elements.organizerAdFileActionMove) {
        elements.organizerAdFileActionMove.addEventListener('change', updateBatchDeleteState);
      }
      if (elements.organizerAdFileActionDelete) {
        elements.organizerAdFileActionDelete.addEventListener('change', updateBatchDeleteState);
      }
      if (elements.organizerBatchDelete) {
        elements.organizerBatchDelete.addEventListener('change', updateBatchDeleteState);
      }

      updateBatchDeleteState();
    }

    function bindIpcEvents() {
      if (ipcEventsBound) {
        return;
      }

      ipcEventsBound = true;
      if (typeof desktopApi.onDependencyInstallProgress === 'function') {
        desktopApi.onDependencyInstallProgress((payload) => {
          dependencyController.applyInstallProgress(payload);
        });
      }

      desktopApi.onOrganizerLog((payload) => {
        if (!payload || !payload.message) {
          return;
        }

        appendLogLine(elements.organizerLogView, payload.level || 'info', payload.message, payload.timestamp);
      });

      desktopApi.onOrganizerState((payload) => {
        if (!payload) {
          return;
        }

        if (payload.progress) {
          applyRuntimeProgress(payload.progress);
        }

        const nextMessage = String(payload.message || '').trim();
        if (payload.status === 'error') {
          setStatus('error', '整理异常');
          if (nextMessage) {
            setSummaryMessage(nextMessage);
          }
        } else if (payload.status === 'completed') {
          const payloadSummary =
            payload.summary || (state.latestResult && state.latestResult.summary) || {};
          setStatus('completed', buildProgressHeadline(100, ORGANIZER_TOTAL_STAGES));
          setSummaryMessage(buildCompletedSummary(payloadSummary, Boolean(state.latestResult && state.latestResult.dryRun)));
        } else if (!payload.progress && payload.status === 'idle') {
          setStatus('idle', messages.idle);
          setSummaryMessage(buildIdleSummary());
        }

        if (payload.status === 'completed') {
          const mergedResult = {
            ...(state.latestResult && typeof state.latestResult === 'object' ? state.latestResult : {}),
            summary: payload.summary || (state.latestResult && state.latestResult.summary) || {},
            reportMap: payload.reportMap || (state.latestResult && state.latestResult.reportMap) || {},
            reportFiles: payload.reportFiles || (state.latestResult && state.latestResult.reportFiles) || [],
            missingDownload: payload.missingDownload || (state.latestResult && state.latestResult.missingDownload) || {},
            adRisk: payload.adRisk || (state.latestResult && state.latestResult.adRisk) || {}
          };
          state.latestResult = mergedResult;
          renderReviewPanel(mergedResult);
        }
      });
    }

    function bootstrap() {
      if (bootstrapCompleted) {
        return Promise.resolve();
      }

      if (bootstrapPromise) {
        return Promise.resolve();
      }

      if (!dependencyControllerFactory || !learningControllerFactory || !crawlOutputControllerFactory) {
        throw new Error('Organizer controller dependencies are not ready.');
      }
      learningController = learningControllerFactory.createOrganizerLearningController({
        elements,
        desktopApi,
        state,
        messages,
        appendLogLine,
        getErrorMessage,
        normalizeKeywordText,
        normalizeAdModelType,
        toSafeInteger,
        setStatus,
        setSummaryMessage,
        requestSyncActionButtons: () => setActionButtonState(),
        requestSyncDependencyStatus: () => {
          if (dependencyController && typeof dependencyController.renderDependencyStatus === 'function') {
            dependencyController.renderDependencyStatus(state.dependencyStatus);
          }
        }
      });
      crawlOutputController = crawlOutputControllerFactory.createOrganizerCrawlOutputController({
        elements,
        desktopApi,
        state,
        messages,
        appendLogLine,
        getErrorMessage
      });
      dependencyController = dependencyControllerFactory.createOrganizerDependencyController({
        elements,
        desktopApi,
        state,
        appendLogLine,
        getErrorMessage,
        isAdDetectionEnabled: () => learningController.isAdDetectionEnabled(),
        getLogView: () => elements.organizerLogView,
        requestRenderAdDetectionUiState: () => learningController.applyAdDetectionUiState()
      });

      applyOrganizerHeroCopy();
      bindEvents();
      learningController.bindEvents();
      crawlOutputController.bindEvents();
      bindIpcEvents();
      setSummaryCounts({});
      organizerReviewView.renderReportFiles(elements.organizerReportPaths, [], {
        onBindOpenReport: bindOpenReport
      });
      renderReviewPanel(null);
      dependencyController.renderDependencyStatus(null);
      setSummaryMessage(buildIdleSummary());
      setStatus('idle', messages.idle);
      crawlOutputController.resetCodeMetaView();
      learningController.renderLearningSummary(null);
      appendLogLine(elements.organizerLogView, 'info', messages.ready);
      learningController.applyAdDetectionUiState();
      bootstrapCompleted = true;

      // Restore persisted values and optional dependency state in the background.
      // The organizer controls above are already usable while this promise runs.
      bootstrapPromise = Promise.resolve()
        .then(async () => {
          const settings = await desktopApi.getSettings();
          if (elements.organizerRoot) {
            elements.organizerRoot.value = settings.organizerRoot || '';
          }
          if (elements.organizerMinSize) {
            elements.organizerMinSize.value = String(toSafeInteger(settings.organizerMinSizeMB, 100, 1));
          }
          if (elements.organizerSuffix) {
            elements.organizerSuffix.value = normalizeOrganizerSuffix(settings.organizerSuffix);
          }
          if (elements.organizerVideoExtensions) {
            elements.organizerVideoExtensions.value = String(
              settings.organizerVideoExtensions || 'mp4, mkv, avi, mov, flv, wmv, ts, m4v, iso'
            );
          }
          const normalizedAdFileAction = normalizeAdFileAction(settings.organizerAdFileAction);
          if (elements.organizerAdFileActionMove) {
            elements.organizerAdFileActionMove.checked = normalizedAdFileAction !== 'delete-directly';
          }
          if (elements.organizerAdFileActionDelete) {
            elements.organizerAdFileActionDelete.checked = normalizedAdFileAction === 'delete-directly';
          }
          if (elements.organizerDryRun) {
            elements.organizerDryRun.checked = Boolean(settings.organizerDryRun);
          }
          if (elements.organizerIncludeSubdirectories) {
            elements.organizerIncludeSubdirectories.checked = settings.organizerIncludeSubdirectories !== false;
          }
          if (elements.organizerStrictCodeMatch) {
            elements.organizerStrictCodeMatch.checked = settings.organizerStrictCodeMatch !== false;
          }
          if (elements.organizerRetryMissingMagnets) {
            elements.organizerRetryMissingMagnets.checked = Boolean(settings.organizerRetryMissingMagnets);
          }
          if (elements.organizerCrawlOutput) {
            elements.organizerCrawlOutput.value =
              String(settings.organizerCrawlOutput || '').trim() || String(settings.output || '').trim();
          }
          // Keep organizer's loaded code snapshot aligned with the restored
          // crawl-output path before the user starts a new run.
          if (crawlOutputController && typeof crawlOutputController.getLoadedSnapshotForOutput === 'function') {
            crawlOutputController.getLoadedSnapshotForOutput(
              elements.organizerCrawlOutput ? elements.organizerCrawlOutput.value : ''
            );
          }
          learningController.applySettings(settings);
          if (elements.organizerFfmpegDownloadUrl) {
            elements.organizerFfmpegDownloadUrl.value = String(settings.organizerFfmpegDownloadUrl || '');
          }
          if (elements.organizerOnnxDownloadUrl) {
            elements.organizerOnnxDownloadUrl.value = String(settings.organizerOnnxDownloadUrl || '');
          }
          if (elements.organizerAlistUrl) {
            elements.organizerAlistUrl.value = String(settings.organizerAlistBaseURL || '');
          }
          if (elements.organizerBatchDelete) {
            elements.organizerBatchDelete.checked = Boolean(settings.organizerBatchDelete);
          }
          if (elements.organizerDeleteInterval) {
            elements.organizerDeleteInterval.value = String(toSafeInteger(settings.organizerDeleteIntervalMs, 500, 100));
          }
          if (elements.organizerOrganizeInterval) {
            elements.organizerOrganizeInterval.value = String(toSafeInteger(settings.organizerOrganizeIntervalMs, 500, 100));
          }

          // Restore custom URLs from localStorage only as a compatibility
          // fallback. Long-term ownership remains the Go settings snapshot.
          const savedFfmpegUrl = safeLocalStorageGet('jav.organizer.ffmpeg.download.url', '');
          const savedOnnxUrl = safeLocalStorageGet('jav.organizer.onnx.download.url', '');
          const savedAlistUrl = safeLocalStorageGet('jav.organizer.alist.url', '');
          if (!elements.organizerFfmpegDownloadUrl.value && savedFfmpegUrl) {
            elements.organizerFfmpegDownloadUrl.value = savedFfmpegUrl;
          }
          if (!elements.organizerOnnxDownloadUrl.value && savedOnnxUrl) {
            elements.organizerOnnxDownloadUrl.value = savedOnnxUrl;
          }
          if (!elements.organizerAlistUrl.value && savedAlistUrl) {
            elements.organizerAlistUrl.value = savedAlistUrl;
          }

          learningController.applyAdDetectionUiState();

          if (learningController.isAdDetectionEnabled()) {
            await learningController.refreshAdLearningSummary(false).catch(() => {});
            await dependencyController.refreshDependencyStatus(false).catch(() => {});
          }
          await crawlOutputController.applyLatestCrawlOutput().catch(() => {});
          await showFirstLaunchGuideIfNeeded().catch(() => {});
        })
        .catch((error) => {
          appendOrganizerLog('error', `视频整理后台初始化失败：${getErrorMessage(error)}`);
        });

      return Promise.resolve();
    }

    async function refreshLatestCrawlOutput() {
      if (crawlOutputController && typeof crawlOutputController.applyLatestCrawlOutput === 'function') {
        await crawlOutputController.applyLatestCrawlOutput();
      }
    }

    return {
      bootstrap,
      refreshLatestCrawlOutput
    };
  }

  globalScope.desktopOrganizerController = {
    createOrganizerController
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
