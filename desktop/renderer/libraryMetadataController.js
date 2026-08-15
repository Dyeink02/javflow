// Media library metadata controller.
// This controller owns the library-metadata workspace only:
// 1) scan local video directories
// 2) scrape metadata by code via the embedded metatube-sdk-go
// 3) generate NFO / download cover images
//
// The controller stays decoupled from crawler/organizer/subscription logic so
// the workspace can be moved or replaced independently.
//
// Ownership summary:
//   This controller owns the library-metadata workspace and UI lifecycle.
//
// File map for maintainers:
// 1) state and formatting helpers
// 2) provider / historical crawl source loading
// 3) test-scrape, resolve, write, and NFO/image actions
// 4) result list rendering and selection helpers
// 5) event binding and bootstrap
(function initializeLibraryMetadataController(globalScope) {
  const rendererHelpers = globalScope.desktopRendererHelpers || {};
  const getErrorMessage = rendererHelpers.getErrorMessage;
  const clearChildren = rendererHelpers.clearChildren;
  const appendTimestampedLogLine = rendererHelpers.appendTimestampedLogLine;
  const bindAsyncClick = rendererHelpers.bindAsyncClick || null;
  const createBufferedLogAppender = rendererHelpers.createBufferedLogAppender || null;

  if (!getErrorMessage || !clearChildren || !appendTimestampedLogLine) {
    throw new Error('desktopRendererHelpers must be loaded before libraryMetadataController');
  }

  function createLibraryMetadataController(options) {
    const { elements, desktopApi, uiText, switchWorkspace } = options || {};

    const state = {
      providers: [],
      crawlSources: [],
      results: [],
      localScanResults: [],
      missingScanResults: [],
      resultScope: 'local',
      selectedIds: new Set(),
      autoSubscriptionSources: new Set(),
      busy: false,
      cancelRequested: false,
      activeJobId: '',
      artifactCountToken: 0,
      currentLogRootPath: '',
      compactLayout: false
    };

    let eventsBound = false;
    let bootstrapCompleted = false;
    let hydrationPromise = null;

    const COMPACT_LAYOUT_STORAGE_KEY = 'jav.librarymetadata.compactLayout';
    const SCRAPE_CONCURRENCY_STORAGE_KEY = 'jav.librarymetadata.scrapeConcurrency';
    const AUTO_SUBSCRIBE_STORAGE_KEY = 'jav.librarymetadata.autoSubscribeLeadActor';
    const SHOW_HIDDEN_FILES_STORAGE_KEY = 'jav.librarymetadata.showHiddenFiles';
    const AUTO_RETRY_DELAYS_MS = [4000, 10000];

    const proxyValidationState = {
      timerId: null,
      autoTimerId: null,
      countdownTimerId: null,
      requestToken: 0,
      lastValue: '',
      lastStatus: 'empty',
      countdownValue: 0,
      autoValidationStopped: false
    };

    const PROXY_AUTO_CHECK_INTERVAL_MS = 5000;

    const PROXY_STATUS_TEXT = {
      empty: '未检测',
      checking: '检测中...',
      valid: '代理正常',
      invalid: '代理失败'
    };

    const PROXY_DETAIL_TEXT = {
      empty: '媒体库刮削默认使用 127.0.0.1:7897，可手动修改；留空时自动使用爬虫设置中的全局代理。',
      checking: '正在检测媒体库代理连通性，请稍候。',
      valid: '检测通过，可继续使用当前媒体库代理。',
      invalid: '当前媒体库代理不可用，请检查代理地址或代理软件状态。'
    };

    const logBuffer =
      typeof createBufferedLogAppender === 'function'
        ? createBufferedLogAppender({
            logView: elements.libraryLogView,
            tagName: 'p'
          })
        : null;

    function appendLog(level, message) {
      const text = String(message || '').trim();
      if (!text) {
        return;
      }

      if (logBuffer) {
        logBuffer.append(level, text, null);
      } else {
        appendTimestampedLogLine(elements.libraryLogView, level, text, null, { tagName: 'p' });
      }

      // 同时写入隐藏日志目录，失败静默处理以免阻塞主流程
      const rootPath = normalizeText(state.currentLogRootPath);
      if (rootPath && desktopApi && typeof desktopApi.appendLibraryMetadataLog === 'function') {
        const line = `[${String(level || 'info').toUpperCase()}] ${text}`;
        desktopApi.appendLibraryMetadataLog({ rootPath, line }).catch(() => {});
      }
    }

    function setStatus(status, message) {
      const normalizedStatus =
        status === 'running' || status === 'starting' || status === 'completed' || status === 'error'
          ? status
          : 'idle';
      if (!elements.libraryStatusPill) {
        return;
      }
      elements.libraryStatusPill.className = `status-pill ${normalizedStatus}`;
      elements.libraryStatusPill.textContent = String(message || '等待扫描').trim();
    }

    function setProgressStatus(status, message) {
      const normalizedStatus =
        status === 'running' || status === 'starting' || status === 'completed' || status === 'error'
          ? status
          : 'idle';
      if (!elements.libraryProgressPill) {
        return;
      }
      elements.libraryProgressPill.className = `status-pill ${normalizedStatus}`;
      elements.libraryProgressPill.textContent = String(message || '等待扫描').trim();
    }

    // 刮削是批量任务；弹窗只在整批（包括自动补刮）进入终态后显示一次。
    // 保留 alert 回退，便于在没有 Wails bridge 的浏览器预览中验证流程。
    async function showLibraryMetadataCompletionAlert({ status = 'completed', message = '' } = {}) {
      const normalizedStatus = ['completed', 'error', 'stopped'].includes(String(status || '').trim().toLowerCase())
        ? String(status || '').trim().toLowerCase()
        : 'completed';
      const titleMap = {
        completed: '媒体库刮削完成',
        error: '媒体库刮削完成，但存在失败',
        stopped: '媒体库刮削已停止'
      };
      const title = titleMap[normalizedStatus];
      const body = String(message || `${title}，请查看刮削日志了解详情。`).trim();
      const type = normalizedStatus === 'completed' ? 'success' : 'warning';

      if (desktopApi && typeof desktopApi.showAlert === 'function') {
        try {
          await desktopApi.showAlert({
            type,
            title,
            message: body,
            confirmText: '知道了'
          });
        } catch (_) {
          // 弹窗失败不应影响刮削结果和收尾清理。
        }
        return;
      }
      if (typeof globalScope.alert === 'function') {
        globalScope.alert(`${title}\n${body}`);
      }
    }

    function normalizeText(value) {
      return String(value || '').trim();
    }

    function isRecoverableScrapeError(error) {
      const message = normalizeText(error).toLocaleLowerCase();
      if (!message) {
        return true;
      }
      return !/(?:info not found|movie not found|未找到影片信息|找不到影片信息|没有找到影片信息)/i.test(message);
    }

    async function waitForAutoRetry(delayMs) {
      const deadline = Date.now() + delayMs;
      while (!state.cancelRequested && Date.now() < deadline) {
        await new Promise((resolve) => setTimeout(resolve, Math.min(250, deadline - Date.now())));
      }
      return !state.cancelRequested;
    }

    async function refreshCrawlArtifactCountFeedback(outputDir) {
      const feedback = elements.crawlArtifactCountFeedback;
      const badge = elements.crawlArtifactCountBadge;
      if (!feedback && !badge) {
        return;
      }

      const normalizedDir = normalizeText(outputDir);
      state.artifactCountToken += 1;
      const requestToken = state.artifactCountToken;

      function updateFeedback(status, badgeText, detailText) {
        if (requestToken !== state.artifactCountToken) {
          return;
        }
        if (badge) {
          badge.className = `artifact-count-badge ${status}`;
          badge.textContent = badgeText;
        }
        if (feedback) {
          feedback.textContent = detailText;
        }
      }

      if (!normalizedDir) {
        updateFeedback('idle', '尚未读取', '用于把本地视频与已抓结果关联，提升刮削命中率。');
        return;
      }

      updateFeedback('loading', '读取中', '正在读取爬虫结果目录...');
      try {
        if (typeof desktopApi.countLibraryMetadataCrawlArtifacts !== 'function') {
          updateFeedback('idle', '尚未读取', '目录计数接口尚未就绪。');
          return;
        }
        const result = await desktopApi.countLibraryMetadataCrawlArtifacts({ outputDir: normalizedDir });
        const count = result && Number.isFinite(Number(result.count)) ? Number(result.count) : 0;
        updateFeedback(
          'ready',
          `已获取 ${count} 部影片`,
          `已读取 ${count} 部影片，用于与本地视频关联并提升刮削命中率。`
        );
      } catch (error) {
        updateFeedback('error', '读取失败', `读取失败：${getErrorMessage(error)}`);
      }
    }

    function applyLayoutMode() {
      const layout = elements.libraryMetadataLayout;
      const button = elements.toggleLibraryLayoutButton;
      if (!layout) {
        return;
      }

      if (state.compactLayout) {
        layout.classList.add('compact-layout');
        if (button) {
          button.textContent = '切换全宽布局';
          button.title = '当前为紧凑并排布局，点击恢复全宽';
        }
      } else {
        layout.classList.remove('compact-layout');
        if (button) {
          button.textContent = '切换紧凑布局';
          button.title = '当前为全宽布局，点击恢复配置与清单并排显示';
        }
      }
    }

    function hasSavedWorkspacePreferences(settings) {
      return Number(settings && settings.libraryWorkspacePreferencesVersion) >= 1;
    }

    function persistWorkspacePreferences(preferences) {
      if (!desktopApi || typeof desktopApi.saveWorkspacePreferences !== 'function') {
        return;
      }
      void desktopApi.saveWorkspacePreferences(preferences).catch(() => {});
    }

    function toggleLayoutMode() {
      state.compactLayout = !state.compactLayout;
      try {
        if (globalScope.localStorage && typeof globalScope.localStorage.setItem === 'function') {
          globalScope.localStorage.setItem(COMPACT_LAYOUT_STORAGE_KEY, state.compactLayout ? '1' : '0');
        }
      } catch (storageError) {
        // 写入失败不影响界面切换
      }
      persistWorkspacePreferences({ libraryCompactLayout: state.compactLayout });
      applyLayoutMode();
      appendLog('info', state.compactLayout ? '已切换为紧凑并排布局' : '已切换为全宽布局');
    }

    function restoreLayoutMode(settings) {
      let saved = '';
      try {
        if (globalScope.localStorage && typeof globalScope.localStorage.getItem === 'function') {
          saved = globalScope.localStorage.getItem(COMPACT_LAYOUT_STORAGE_KEY) || '';
        }
      } catch (storageError) {
        // 读取失败时使用默认全宽布局
      }
      state.compactLayout = hasSavedWorkspacePreferences(settings)
        ? Boolean(settings.libraryCompactLayout)
        : saved === '1';
      applyLayoutMode();
    }

    function normalizeScrapeConcurrency(value) {
      const parsed = Number.parseInt(String(value || ''), 10);
      return Number.isFinite(parsed) ? Math.min(5, Math.max(1, parsed)) : 3;
    }

    function getScrapeConcurrency() {
      return normalizeScrapeConcurrency(elements.libraryScrapeConcurrency && elements.libraryScrapeConcurrency.value);
    }

    function restoreScrapePreferences(settings) {
      let savedConcurrency = '';
      let savedAutoSubscribe = '';
      try {
        if (globalScope.localStorage && typeof globalScope.localStorage.getItem === 'function') {
          savedConcurrency = globalScope.localStorage.getItem(SCRAPE_CONCURRENCY_STORAGE_KEY) || '';
          savedAutoSubscribe = globalScope.localStorage.getItem(AUTO_SUBSCRIBE_STORAGE_KEY) || '';
        }
      } catch (storageError) {
        // 浏览器存储不可用时保留界面默认值。
      }
      if (elements.libraryScrapeConcurrency) {
        const concurrency = hasSavedWorkspacePreferences(settings)
          ? settings.libraryScrapeConcurrency
          : savedConcurrency || 3;
        elements.libraryScrapeConcurrency.value = String(normalizeScrapeConcurrency(concurrency));
      }
      if (elements.libraryAutoSubscribeLeadActor) {
        elements.libraryAutoSubscribeLeadActor.checked = hasSavedWorkspacePreferences(settings)
          ? Boolean(settings.libraryAutoSubscribeFromOutput)
          : savedAutoSubscribe === '1';
      }
    }

    function isShowHiddenFilesEnabled() {
      return Boolean(elements.showHiddenFilesCheckbox && elements.showHiddenFilesCheckbox.checked);
    }

    function saveShowHiddenFilesPreference(enabled) {
      try {
        if (globalScope.localStorage && typeof globalScope.localStorage.setItem === 'function') {
          globalScope.localStorage.setItem(SHOW_HIDDEN_FILES_STORAGE_KEY, enabled ? '1' : '0');
        }
      } catch (storageError) {
        // 写入失败不影响主流程
      }
      persistWorkspacePreferences({ libraryShowHiddenFiles: enabled });
    }

    function restoreShowHiddenFilesPreference(settings) {
      let saved = '';
      try {
        if (globalScope.localStorage && typeof globalScope.localStorage.getItem === 'function') {
          saved = globalScope.localStorage.getItem(SHOW_HIDDEN_FILES_STORAGE_KEY) || '';
        }
      } catch (storageError) {
        // 读取失败时使用默认显示，避免爬虫产物被隐藏
      }
      if (elements.showHiddenFilesCheckbox) {
        // 默认启用“显示隐藏路径文件”，这样爬虫/整理后的产物能直接显示出来。
        elements.showHiddenFilesCheckbox.checked = hasSavedWorkspacePreferences(settings)
          ? Boolean(settings.libraryShowHiddenFiles)
          : saved === '' || saved === '1';
      }
    }

    function getLibraryProxy() {
      return normalizeText(elements.libraryProxyUrl && elements.libraryProxyUrl.value);
    }

    async function initLibraryLog(rootPath) {
      const normalizedRoot = normalizeText(rootPath);
      if (!normalizedRoot) {
        return;
      }
      state.currentLogRootPath = normalizedRoot;
      if (desktopApi && typeof desktopApi.initLibraryMetadataLog === 'function') {
        try {
          const result = await desktopApi.initLibraryMetadataLog({ rootPath: normalizedRoot });
          if (result && result.logDir) {
            appendLog('info', `日志目录：${result.logDir}`);
          }
        } catch (error) {
          appendLog('warn', `初始化日志目录失败：${getErrorMessage(error)}`);
        }
      }
    }

    function setProxyStatus(status, detailText = '') {
      const normalized = status === 'checking' || status === 'valid' || status === 'invalid' ? status : 'empty';
      proxyValidationState.lastStatus = normalized;

      if (elements.libraryProxyStatus) {
        elements.libraryProxyStatus.className = `proxy-status-chip ${normalized}`;
        elements.libraryProxyStatus.textContent = PROXY_STATUS_TEXT[normalized];
      }
      if (elements.libraryProxyStatusDetail) {
        const detail =
          typeof detailText === 'string' && detailText.trim() ? detailText.trim() : PROXY_DETAIL_TEXT[normalized];
        elements.libraryProxyStatusDetail.textContent = detail;
      }

      // 若当前正在显示倒计时，刷新状态时把倒计时叠加回详情文本。
      if (proxyValidationState.countdownValue > 0 && normalized !== 'checking') {
        updateProxyCountdownDisplay();
      }
    }

    function clearProxyValidationTimer() {
      if (proxyValidationState.timerId) {
        clearTimeout(proxyValidationState.timerId);
        proxyValidationState.timerId = null;
      }
    }

    function clearProxyAutoValidationTimer() {
      if (proxyValidationState.autoTimerId) {
        clearTimeout(proxyValidationState.autoTimerId);
        proxyValidationState.autoTimerId = null;
      }
    }

    function clearProxyCountdownTimer() {
      if (proxyValidationState.countdownTimerId) {
        clearInterval(proxyValidationState.countdownTimerId);
        proxyValidationState.countdownTimerId = null;
      }
    }

    function stopProxyAutoValidation() {
      proxyValidationState.autoValidationStopped = true;
      clearProxyValidationTimer();
      clearProxyAutoValidationTimer();
      clearProxyCountdownTimer();
    }

    function updateProxyCountdownDisplay() {
      if (!elements.libraryProxyStatusDetail) {
        return;
      }
      const baseDetail = PROXY_DETAIL_TEXT[proxyValidationState.lastStatus] || PROXY_DETAIL_TEXT.empty;
      if (proxyValidationState.countdownValue > 0 && proxyValidationState.lastStatus !== 'checking') {
        elements.libraryProxyStatusDetail.textContent = `${baseDetail}（${proxyValidationState.countdownValue} 秒后刷新）`;
      } else {
        elements.libraryProxyStatusDetail.textContent = baseDetail;
      }
    }

    function startProxyCountdown() {
      clearProxyCountdownTimer();
      proxyValidationState.countdownValue = Math.ceil(PROXY_AUTO_CHECK_INTERVAL_MS / 1000);
      updateProxyCountdownDisplay();
      proxyValidationState.countdownTimerId = setInterval(() => {
        proxyValidationState.countdownValue -= 1;
        if (proxyValidationState.countdownValue <= 0) {
          proxyValidationState.countdownValue = 0;
          clearProxyCountdownTimer();
        }
        updateProxyCountdownDisplay();
      }, 1000);
    }

    async function validateProxyValue(proxyValue) {
      const trimmedValue = String(proxyValue || '').trim();
      clearProxyValidationTimer();
      proxyValidationState.requestToken += 1;
      const requestToken = proxyValidationState.requestToken;
      proxyValidationState.lastValue = trimmedValue;

      if (!trimmedValue) {
        setProxyStatus('empty');
        return { status: 'empty', detail: PROXY_DETAIL_TEXT.empty };
      }

      setProxyStatus('checking');

      try {
        const result = await desktopApi.validateProxy(trimmedValue, {
          targetUrl: 'https://www.javbus.com'
        });

        if (requestToken !== proxyValidationState.requestToken) {
          return result;
        }

        if (result && result.status === 'valid') {
          setProxyStatus('valid', result.detail);
          return result;
        }

        setProxyStatus('invalid', result && result.detail);
        return result || { status: 'invalid', detail: PROXY_DETAIL_TEXT.invalid };
      } catch (error) {
        const message = getErrorMessage(error);
        if (requestToken === proxyValidationState.requestToken) {
          setProxyStatus('invalid', message);
        }
        return { status: 'invalid', detail: message };
      }
    }

    function scheduleProxyValidation(delayMs = 650) {
      clearProxyValidationTimer();
      clearProxyCountdownTimer();
      const trimmedValue = elements.libraryProxyUrl ? elements.libraryProxyUrl.value.trim() : '';

      if (!trimmedValue) {
        proxyValidationState.requestToken += 1;
        proxyValidationState.lastValue = '';
        setProxyStatus('empty');
        return;
      }

      setProxyStatus('checking');
      proxyValidationState.timerId = setTimeout(() => {
        proxyValidationState.timerId = null;
        void validateProxyValue(trimmedValue);
      }, delayMs);
    }

    function scheduleProxyAutoValidation(delayMs = PROXY_AUTO_CHECK_INTERVAL_MS) {
      if (proxyValidationState.autoValidationStopped) {
        return;
      }

      clearProxyAutoValidationTimer();
      clearProxyCountdownTimer();

      proxyValidationState.autoTimerId = setTimeout(async () => {
        proxyValidationState.autoTimerId = null;
        if (proxyValidationState.autoValidationStopped) {
          return;
        }

        const trimmedValue = elements.libraryProxyUrl ? elements.libraryProxyUrl.value.trim() : '';
        if (!trimmedValue) {
          setProxyStatus('empty');
          scheduleProxyAutoValidation();
          return;
        }

        await validateProxyValue(trimmedValue);
        scheduleProxyAutoValidation();
      }, Math.max(1000, delayMs));

      startProxyCountdown();
    }

    function initializeProxyDetection() {
      if (!elements.libraryProxyUrl || !elements.libraryProxyStatus || !elements.libraryProxyStatusDetail) {
        return;
      }

      if (!elements.libraryProxyUrl.value.trim()) {
        elements.libraryProxyUrl.value = '127.0.0.1:7897';
      }

      // 输入时防抖检测（倒计时 5 秒），失去焦点时立即检测，并启动 5 秒轮询。
      elements.libraryProxyUrl.addEventListener('input', () => {
        scheduleProxyValidation(PROXY_AUTO_CHECK_INTERVAL_MS);
      });

      elements.libraryProxyUrl.addEventListener('change', () => {
        scheduleProxyValidation(0);
      });

      elements.libraryProxyUrl.addEventListener('blur', () => {
        scheduleProxyValidation(0);
      });

      // 首次异步检测并启动自动轮询，失败不影响其他功能。
      void validateProxyValue(elements.libraryProxyUrl.value).then(() => {
        scheduleProxyAutoValidation();
      });
    }

    function extractCodeFromInput(rawValue) {
      const text = normalizeText(rawValue);
      if (!text) {
        return '';
      }

      // Patterns mirror code_extract.go so FC2-PPV, HEYZO, KIN8 and generic
      // studio-number codes are recognized consistently across frontend and backend.
      const patterns = [
        /(FC2(?:PPV)?[-_]?\d{6,})/i,
        /(HEYZO[-_]?\d{3,})/i,
        /(KIN8[-_]?\d{3,})/i,
        /(\d{3}[A-Z]{2,4}[-_]?\d{2,4})/i,
        /([A-Z]{2,6}[-_]?\d{1,4})/i
      ];

      for (const pattern of patterns) {
        const match = text.match(pattern);
        if (match && match[1]) {
          return match[1].toUpperCase().replace(/_/g, '-');
        }
      }

      // Fallback: return the trimmed uppercase text as-is for direct code entry.
      return text.toUpperCase();
    }

    function bindAsyncButton(button, handler, onError) {
      if (typeof bindAsyncClick === 'function') {
        bindAsyncClick(button, handler, {
          onError,
          fallbackErrorHandler: (error) => appendLog('error', getErrorMessage(error))
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
          appendLog('error', getErrorMessage(error));
        }
      });
    }

    async function loadProviders() {
      if (!desktopApi || typeof desktopApi.getLibraryMetadataProviders !== 'function') {
        appendLog('warn', '刮削源 API 尚未就绪');
        return;
      }

      try {
        const response = await desktopApi.getLibraryMetadataProviders();
        state.providers = Array.isArray(response && response.providers) ? response.providers : [];
        populateSourcePrioritySelect(state.providers);
        appendLog('info', `已加载 ${state.providers.length} 个刮削源：${state.providers.join(', ') || '无'}`);
      } catch (error) {
        appendLog('error', `加载刮削源失败：${getErrorMessage(error)}`);
      }
    }

    function populateSourcePrioritySelect(providers) {
      const select = elements.metadataSourcePriority;
      if (!select) {
        return;
      }

      const previousValue = select.value;
      clearChildren(select);

      const defaultOption = document.createElement('option');
      defaultOption.value = '';
      defaultOption.textContent = '默认（自动选择最佳结果）';
      select.appendChild(defaultOption);

      (providers || []).forEach((provider) => {
        const option = document.createElement('option');
        option.value = String(provider);
        option.textContent = String(provider);
        select.appendChild(option);
      });

      if (previousValue && Array.from(select.options).some((opt) => opt.value === previousValue)) {
        select.value = previousValue;
      } else {
        select.value = '';
      }
    }

    function collectKnownCrawlSourceRoots() {
      const knownRoots = [];
      const currentPath = normalizeText(elements.crawlArtifactPath && elements.crawlArtifactPath.value);
      if (currentPath) {
        knownRoots.push(currentPath);
      }
      try {
        const rawValue =
          globalScope.localStorage && typeof globalScope.localStorage.getItem === 'function'
            ? globalScope.localStorage.getItem('jav.crawl.result.history.v2')
            : '';
        const parsed = rawValue ? JSON.parse(rawValue) : [];
        (Array.isArray(parsed) ? parsed : []).forEach((entry) => {
          const outputDir = normalizeText(entry && entry.outputDir);
          if (outputDir) {
            knownRoots.push(outputDir);
          }
        });
      } catch (historyRootError) {
        // 后端仍会从设置目录和隐藏产物目录重建历史索引。
      }
      return knownRoots;
    }

    async function loadCrawlSources(preloadedResponse) {
      if (!preloadedResponse && (!desktopApi || typeof desktopApi.getLibraryMetadataCrawlSources !== 'function')) {
        appendLog('warn', '历史快照 API 尚未就绪');
        return;
      }

      try {
        const response =
          preloadedResponse ||
          (await desktopApi.getLibraryMetadataCrawlSources({ roots: collectKnownCrawlSourceRoots() }));
        const apiSources = Array.isArray(response && response.sources) ? response.sources : [];

        // 同时读取 localStorage 中的爬虫结果历史（与订阅板块保持一致），
        // 这样用户过往手动保存的抓取记录也能在这里选择。
        let historySources = [];
        try {
          const rawValue =
            globalScope.localStorage && typeof globalScope.localStorage.getItem === 'function'
              ? globalScope.localStorage.getItem('jav.crawl.result.history.v2')
              : '';
          const parsed = rawValue ? JSON.parse(rawValue) : [];
          historySources = (Array.isArray(parsed) ? parsed : [])
            .filter((entry) => entry && entry.outputDir)
            .map((entry) => ({
              outputDir: String(entry.outputDir),
              label: String(entry.title || entry.outputDir),
              cacheKey: '',
              updatedAt: String(entry.updatedAt || '')
            }));
        } catch (historyError) {
          // localStorage 读取失败不影响主流程
        }

        state.crawlSources = mergeCrawlSources(apiSources, historySources);
        populateCrawlSourcesSelect(state.crawlSources);
        const defaultOutputDir = normalizeText(response && response.defaultOutputDir);
        const currentOutputDir = normalizeText(elements.crawlArtifactPath && elements.crawlArtifactPath.value);
        const automaticOutputDir = currentOutputDir || defaultOutputDir || normalizeText(state.crawlSources[0] && state.crawlSources[0].outputDir);
        if (automaticOutputDir && elements.crawlArtifactPath) {
          elements.crawlArtifactPath.value = automaticOutputDir;
          if (elements.crawlSourceSnapshot) {
            elements.crawlSourceSnapshot.value = Array.from(elements.crawlSourceSnapshot.options).some(
              (option) => option.value === automaticOutputDir
            )
              ? automaticOutputDir
              : '';
          }
          void refreshCrawlArtifactCountFeedback(automaticOutputDir);
        }
        appendLog('info', `已加载 ${state.crawlSources.length} 个历史抓取快照`);
      } catch (error) {
        appendLog('error', `加载历史快照失败：${getErrorMessage(error)}`);
      }
    }

    async function hydrateLibraryMetadataData() {
      if (desktopApi && typeof desktopApi.getSettings === 'function') {
        try {
          const settings = await desktopApi.getSettings();
          restoreLayoutMode(settings);
          restoreScrapePreferences(settings);
          restoreShowHiddenFilesPreference(settings);
          if (!hasSavedWorkspacePreferences(settings)) {
            persistWorkspacePreferences({
              libraryCompactLayout: state.compactLayout,
              libraryShowHiddenFiles: isShowHiddenFilesEnabled(),
              libraryScrapeConcurrency: getScrapeConcurrency(),
              libraryAutoSubscribeFromOutput: Boolean(
                elements.libraryAutoSubscribeLeadActor && elements.libraryAutoSubscribeLeadActor.checked
              )
            });
          }
        } catch (error) {
          appendLog('warn', `读取媒体库偏好失败，继续使用本地设置：${getErrorMessage(error)}`);
        }
      }
      if (desktopApi && typeof desktopApi.getLibraryMetadataBootstrap === 'function') {
        try {
          const response = await desktopApi.getLibraryMetadataBootstrap({ roots: collectKnownCrawlSourceRoots() });
          state.providers = Array.isArray(response && response.providers) ? response.providers : [];
          populateSourcePrioritySelect(state.providers);
          appendLog('info', `已加载 ${state.providers.length} 个刮削源`);
          await loadCrawlSources(response);
          return;
        } catch (error) {
          appendLog('warn', `聚合初始化失败，改用兼容读取：${getErrorMessage(error)}`);
        }
      }
      await Promise.all([loadProviders(), loadCrawlSources()]);
    }

    function mergeCrawlSources(apiSources, historySources) {
      const seen = new Set();
      const merged = [];
      (Array.isArray(apiSources) ? apiSources : [])
        .concat(Array.isArray(historySources) ? historySources : [])
        .forEach((source) => {
          const outputDir = normalizeText(source && source.outputDir);
          if (!outputDir) {
            return;
          }
          const key = outputDir.toLowerCase();
          if (seen.has(key)) {
            return;
          }
          seen.add(key);
          merged.push(source);
        });
      return merged;
    }

    function populateCrawlSourcesSelect(sources) {
      const select = elements.crawlSourceSnapshot;
      if (!select) {
        return;
      }

      const previousValue = select.value;
      clearChildren(select);

      const defaultOption = document.createElement('option');
      defaultOption.value = '';
      defaultOption.textContent = '不指定历史快照';
      select.appendChild(defaultOption);

      // 按更新时间降序排序，最新的快照显示在最上面
      const sortedSources = [...(sources || [])].sort((a, b) => {
        const timeA = new Date(a && a.updatedAt ? a.updatedAt : '').getTime();
        const timeB = new Date(b && b.updatedAt ? b.updatedAt : '').getTime();
        return timeB - timeA;
      });

      sortedSources.forEach((source) => {
        const option = document.createElement('option');
        option.value = String(source.outputDir || '');
        const label = String(source.label || source.cacheKey || '未命名快照');
        const updatedAt = normalizeText(source.updatedAt);
        option.textContent = updatedAt
          ? `${label} ${updatedAt.slice(0, 16).replace('T', ' ')}`
          : label;
        option.dataset.cacheKey = String(source.cacheKey || '');
        select.appendChild(option);
      });

      if (previousValue && Array.from(select.options).some((opt) => opt.value === previousValue)) {
        select.value = previousValue;
      } else {
        select.value = '';
      }
    }

    function withTimeout(promise, timeoutMs, timeoutMessage) {
      return new Promise((resolve, reject) => {
        const timer = setTimeout(() => {
          reject(new Error(timeoutMessage || `操作超时（${timeoutMs}ms）`));
        }, timeoutMs);

        Promise.resolve(promise)
          .then((value) => {
            clearTimeout(timer);
            resolve(value);
          })
          .catch((error) => {
            clearTimeout(timer);
            reject(error);
          });
      });
    }

    async function handleTestScrape() {
      let code = extractCodeFromInput(elements.crawlFallbackInput && elements.crawlFallbackInput.value);
      if (!code) {
        code = 'BBAN-452';
        appendLog('info', '未输入番号，使用默认测试番号：BBAN-452');
      }

      const provider = normalizeText(elements.metadataSourcePriority && elements.metadataSourcePriority.value);
      appendLog('info', `正在测试刮削：${code}${provider ? `（源：${provider}）` : ''}`);
      setStatus('running', '刮削中...');

      const proxy = getLibraryProxy();

      try {
        const result = await desktopApi.scrapeLibraryMetadata({
          number: code,
          provider: provider,
          proxy: proxy
        });

        if (result && result.error) {
          appendLog('error', `刮削失败：${result.error}`);
          setStatus('error', '刮削失败');
          return;
        }

        if (!result || !result.info) {
          appendLog('warn', '未找到影片信息');
          setStatus('idle', '未找到');
          return;
        }

        appendLog('info', `刮削成功：${result.info.number} - ${result.info.title || '无标题'}`);
        appendLog('info', `提供者：${result.info.provider || '自动'} | 演员：${(result.info.actors || []).join(', ') || '无'} | 日期：${result.info.releaseDate || '无'}`);
        appendLog('info', `封面：${result.info.coverUrl || '无'} | 背景：${result.info.backdropUrl || '无'}`);
        setStatus('completed', '刮削成功');
      } catch (error) {
        appendLog('error', `测试刮削失败：${getErrorMessage(error)}`);
        setStatus('error', '刮削失败');
      }
    }

    function handleRehydrateCode() {
      const rawValue = normalizeText(elements.crawlFallbackInput && elements.crawlFallbackInput.value);
      if (!rawValue) {
        appendLog('warn', '请先输入番号或网页地址');
        return;
      }

      const code = extractCodeFromInput(rawValue);
      if (!code) {
        appendLog('warn', '无法从输入中提取有效番号');
        return;
      }

      // 填充到 JAV 爬虫输入框
      const crawlerCodeInput = document.getElementById('crawlerCodeInput');
      if (crawlerCodeInput) {
        crawlerCodeInput.value = code;
      }

      appendLog('info', `已将番号 ${code} 路由回 JAV 爬虫`);

      if (typeof switchWorkspace === 'function') {
        switchWorkspace('crawler');
      } else {
        appendLog('warn', '工作区切换接口不可用，请手动切换到 JAV 爬虫工作区');
      }
    }

    // Use a display-preserving name for the request, but collapse harmless
    // spacing/punctuation differences for the in-memory de-duplication key.
    // This prevents one crawl from scheduling the same actress twice when two
    // providers format her name differently (for example, with a full-width
    // space or a middle dot).
    function normalizeAutoSubscriptionSourceKey(value) {
      return normalizeText(value)
        .toLocaleLowerCase()
        .replace(/[\s\u3000・·]/g, '');
    }

    function queueCrawlArtifactAutoSubscription(crawlOutputDir) {
      if (
        !elements.libraryAutoSubscribeLeadActor ||
        !elements.libraryAutoSubscribeLeadActor.checked ||
        !desktopApi ||
        typeof desktopApi.scanAvSubscriptionsFromOutput !== 'function'
      ) {
        return;
      }
      const artifactInput = normalizeText(crawlOutputDir);
      if (!artifactInput) {
        appendLog('warn', '自动订阅已跳过：请先选择包含 crawl-profile.json 或 filmData.json 的爬虫结果。');
        return;
      }
      const sourceKey = normalizeAutoSubscriptionSourceKey(artifactInput);
      if (!sourceKey) {
        return;
      }
      if (state.autoSubscriptionSources.has(sourceKey)) {
        return;
      }
      state.autoSubscriptionSources.add(sourceKey);
      appendLog('info', '后台自动订阅：正在从爬虫 JSON 导入订阅基线...');
      void desktopApi
        .scanAvSubscriptionsFromOutput({ artifactInput })
        .then((result) => {
          const actresses = Array.isArray(result && result.scannedActressList)
            ? result.scannedActressList.map((item) => normalizeText(item)).filter(Boolean)
            : [];
          const actressLabel = actresses.join('、') || '目标女优';
          const addedCount = Number(result && result.addedCount) || 0;
          appendLog('info', addedCount > 0 ? `已自动订阅：${actressLabel}` : `已更新订阅基线：${actressLabel}`);
        })
        .catch((error) => {
          appendLog('warn', `自动订阅失败：${getErrorMessage(error)}`);
        });
    }

    async function handleScrapeSelected(mode) {
      const writeMode = typeof mode === 'string' ? mode : 'all';
      const isRetryMode = writeMode === 'retry-all';
      const effectiveWriteMode = isRetryMode ? 'all' : writeMode;
      state.autoSubscriptionSources.clear();
      if (state.selectedIds.size === 0) {
        appendLog('warn', '请先选择要刮削的影片');
        return;
      }
      if (state.busy) {
        appendLog('warn', '当前正在执行刮削，请等待完成');
        return;
      }

      const provider = normalizeText(elements.metadataSourcePriority && elements.metadataSourcePriority.value);
      const crawlOutputDir = normalizeText(elements.crawlArtifactPath && elements.crawlArtifactPath.value);
      const proxy = getLibraryProxy();
      const root = normalizeText(elements.libraryRootPath && elements.libraryRootPath.value);
      await initLibraryLog(root);
      state.busy = true;
      state.cancelRequested = false;
      state.activeJobId = `library-${Date.now()}-${Math.random().toString(16).slice(2)}`;
      const jobId = state.activeJobId;
      if (elements.stopLibraryScrapeButton) {
        elements.stopLibraryScrapeButton.disabled = false;
        elements.stopLibraryScrapeButton.textContent = '停止刮削';
      }
      setStatus('running', '刮削中...');
      setProgressStatus('running', `准备刮削 ${state.selectedIds.size} 部影片`);

      try {
        if (desktopApi && typeof desktopApi.startLibraryMetadataJob === 'function') {
          await desktopApi.startLibraryMetadataJob({ jobId });
        }

        const sortedIndexes = Array.from(state.selectedIds).sort((a, b) => a - b);
        const selectedCount = sortedIndexes.length;
        let successCount = 0;
        let failCount = 0;
        let skipCount = 0;
        let processedCount = 0;
        let totalCount = sortedIndexes.length;
        const failureReasons = new Map();
        let activeAutoRetryRound = 0;
        // A/B split files keep independent output paths, but share one metadata
        // lookup. This avoids duplicate provider requests for the same base code.
        const sharedResolveCache = new Map();
        const sharedResolveInflight = new Map();

        function buildResolveCacheKey(code, attempt) {
          const firstAttempt = activeAutoRetryRound === 0 && attempt === 1;
          return [
            normalizeText(code).toUpperCase(),
            firstAttempt ? normalizeText(provider).toLowerCase() : '',
            firstAttempt ? 'auto' : 'online',
            normalizeText(proxy).toLowerCase(),
            normalizeText(crawlOutputDir).toLowerCase()
          ].join('|');
        }

        async function resolveMetadataOnce(code, attempt) {
          const key = buildResolveCacheKey(code, attempt);
          if (sharedResolveCache.has(key)) {
            appendLog('info', `复用 ${code} 的共享刮削结果，分集文件仍分别写入`);
            return sharedResolveCache.get(key);
          }

          const inflight = sharedResolveInflight.get(key);
          if (inflight) {
            appendLog('info', `等待 ${code} 的共享刮削结果`);
            return inflight;
          }

          const firstAttempt = activeAutoRetryRound === 0 && attempt === 1;
          const promise = withTimeout(
            desktopApi.resolveLibraryMetadata({
              number: code,
              provider: firstAttempt ? provider : '',
              proxy: proxy,
              crawlOutputDir: crawlOutputDir,
              preferSource: firstAttempt ? 'auto' : 'online',
              maxAttempts: 3,
              jobId
            }),
            120000,
            `解析 ${code} 超时`
          )
            .then((result) => {
              if (result && !result.error && result.info) {
                sharedResolveCache.set(key, result);
              }
              return result;
            })
            .finally(() => {
              sharedResolveInflight.delete(key);
            });

          sharedResolveInflight.set(key, promise);
          return promise;
        }

      function updateProgressPill() {
        const current = processedCount;
        if (current >= totalCount) {
          return;
        }
        setProgressStatus('running', `正在刮削 ${current + 1}/${totalCount}`);
      }

      function syncLocalScanResult(item) {
        if (!item || !item.mediaPath) {
          return;
        }
        const localIndex = state.localScanResults.findIndex((candidate) => candidate.mediaPath === item.mediaPath);
        if (localIndex >= 0) {
          state.localScanResults[localIndex] = item;
        }
      }

      function markItemFailed(index, reason, rawError) {
        const item = state.results[index];
        if (!item) {
          return;
        }
        state.results[index] = updateItemAfterWrite(item, null, {}, '', true);
        syncLocalScanResult(state.results[index]);
        failureReasons.set(index, normalizeText(rawError) || normalizeText(reason));
        updateResultRow(index, state.results[index]);
        appendLog('error', reason);
      }

      async function processOne(index) {
        if (state.cancelRequested) {
          return;
        }
        const item = state.results[index];
        // 缺视频行只用于提示，不参与任何刮削写入。
        if (!item || !item.hasMedia) {
          return;
        }

        const code = normalizeText(item.code);
        if (!code) {
          return;
        }

        // 补齐当前模式：NFO、封面、背景、横图均已存在则直接跳过
        const isFullyComplete = item.hasNfo && item.hasPoster && item.hasBackdrop && item.hasLandscape;
        if (effectiveWriteMode === 'all' && isFullyComplete) {
          appendLog('info', `[${index + 1}/${state.results.length}] 已完整，跳过：${code}`);
          skipCount += 1;
          processedCount += 1;
          updateProgressPill();
          return;
        }

        // 补刮未完成模式：只补充缺失的内容；之前整体失败的全部重试。
        const isIncrementalRetry = (isRetryMode || activeAutoRetryRound > 0) && !item.failed;

        updateProgressPill();

        // 每轮解析本身已包含多源回退，外层只保留少量重试以避免重复请求拖慢整批任务。
        const maxRetries = activeAutoRetryRound > 0 ? 1 : isRetryMode ? 3 : 2;
        let lastError = '';

        for (let attempt = 1; attempt <= maxRetries; attempt += 1) {
          if (state.cancelRequested) {
            return;
          }
          appendLog('info', `[${index + 1}/${state.results.length}] 第 ${attempt}/${maxRetries} 次解析：${code}`);

          try {
            const resolveResult = await resolveMetadataOnce(code, attempt);

            if (resolveResult && resolveResult.error) {
              lastError = resolveResult.error;
              appendLog('warn', `第 ${attempt} 次解析 ${code} 失败：${lastError}，准备换源重试`);
              continue;
            }
            if (!resolveResult || !resolveResult.info) {
              lastError = '未找到影片信息';
              appendLog('warn', `第 ${attempt} 次解析 ${code} 未找到信息，准备换源重试`);
              continue;
            }

            const sourceLabel = resolveResult.source || resolveResult.info.provider || '自动';
            appendLog(
              'info',
              `解析成功 ${code}：${resolveResult.info.title || '无标题'}（来源：${sourceLabel}）`
            );

            const writePayload = {
              item: item,
              info: resolveResult.info,
              fallbackInfos: resolveResult.fallbackInfos || [],
              proxy: proxy,
              libraryRoot: root,
              skipNfo: isIncrementalRetry ? item.hasNfo : effectiveWriteMode === 'images-only',
              skipPoster: isIncrementalRetry ? item.hasPoster : false,
              skipBackdrop: isIncrementalRetry ? item.hasBackdrop : false,
              skipLandscape: isIncrementalRetry ? item.hasLandscape : false,
              jobId
            };

            if (effectiveWriteMode === 'nfo-only') {
              writePayload.skipImages = true;
            }

            const writeResult = await withTimeout(
              desktopApi.writeLibraryMetadata(writePayload),
              180000,
              `写入 ${code} 超时`
            );

            if (writeResult && writeResult.error) {
              lastError = writeResult.error;
              appendLog('warn', `第 ${attempt} 次写入 ${code} 失败：${lastError}，准备换源重试`);
              continue;
            }

            const wroteAnything =
              writeResult.nfoPath || writeResult.posterPath || writeResult.backdropPath || writeResult.landscapePath;
            if (wroteAnything) {
              appendLog(
                'info',
                `写入成功 ${getItemDisplayCode(item)}：NFO=${writeResult.nfoPath || '-'} 封面=${writeResult.posterPath || '-'} 背景=${writeResult.backdropPath || '-'} 横图=${writeResult.landscapePath || '-'}`
              );
              successCount += 1;
            } else {
              appendLog('info', `无需写入 ${code}：所有文件已存在或无可用图片 URL`);
              skipCount += 1;
            }

            state.results[index] = updateItemAfterWrite(item, resolveResult.info, writeResult, resolveResult.source);
            syncLocalScanResult(state.results[index]);
            failureReasons.delete(index);
            updateResultRow(index, state.results[index]);
            queueCrawlArtifactAutoSubscription(crawlOutputDir);
            processedCount += 1;
            return;
          } catch (error) {
            lastError = getErrorMessage(error);
            appendLog('warn', `第 ${attempt} 次处理 ${code} 失败：${lastError}，准备换源重试`);
          }
        }

        // 所有重试用尽仍失败
        failCount += 1;
        processedCount += 1;
        markItemFailed(index, `刮削失败 ${code}：${lastError}`, lastError);
      }

      const CONCURRENCY = getScrapeConcurrency();
      const REQUEST_JITTER_MIN_MS = 100;
      const REQUEST_JITTER_MAX_MS = 350;
      let activeWorkers = 0;

      function getJitterDelay() {
        return Math.floor(
          Math.random() * (REQUEST_JITTER_MAX_MS - REQUEST_JITTER_MIN_MS + 1) + REQUEST_JITTER_MIN_MS
        );
      }

      async function runQueue(indexes) {
        const queue = [...indexes];
        const workers = [];
        for (let i = 0; i < Math.min(CONCURRENCY, queue.length); i++) {
          workers.push(
            (async () => {
            while (queue.length > 0 && !state.cancelRequested) {
              const index = queue.shift();
              if (index === undefined) {
                break;
              }
              const item = state.results[index];
              const code = getItemDisplayCode(item) || `#${index + 1}`;
              activeWorkers += 1;
              appendLog('info', `[并发 ${activeWorkers}/${CONCURRENCY}] 开始处理 ${code}，队列剩余 ${queue.length} 个`);
              try {
                await new Promise((resolve) => setTimeout(resolve, getJitterDelay()));
                await processOne(index);
              } finally {
                activeWorkers -= 1;
                appendLog('info', `[并发 ${activeWorkers}/${CONCURRENCY}] 完成 ${code}，队列剩余 ${queue.length} 个`);
              }
            }
            })()
          );
        }
        await Promise.all(workers);
      }

      await runQueue(sortedIndexes);

      if (effectiveWriteMode === 'all' && !state.cancelRequested) {
        for (let round = 0; round < AUTO_RETRY_DELAYS_MS.length; round += 1) {
          const retryIndexes = sortedIndexes.filter((index) => {
            const item = state.results[index];
            return item && item.failed && isRecoverableScrapeError(failureReasons.get(index));
          });
          if (retryIndexes.length === 0) {
            break;
          }

          const delayMs = AUTO_RETRY_DELAYS_MS[round];
          appendLog('info', `检测到 ${retryIndexes.length} 部可恢复失败，${Math.round(delayMs / 1000)} 秒后自动补刮第 ${round + 1} 轮`);
          setProgressStatus('running', `等待自动补刮 ${retryIndexes.length} 部影片`);
          if (!(await waitForAutoRetry(delayMs))) {
            break;
          }

          activeAutoRetryRound = round + 1;
          processedCount = 0;
          totalCount = retryIndexes.length;
          retryIndexes.forEach((index) => {
            failCount = Math.max(0, failCount - 1);
            const item = state.results[index];
            state.results[index] = { ...item, failed: false };
            syncLocalScanResult(state.results[index]);
          });
          appendLog('info', `开始自动补刮第 ${activeAutoRetryRound}/${AUTO_RETRY_DELAYS_MS.length} 轮，共 ${retryIndexes.length} 部`);
          await runQueue(retryIndexes);
        }
      }

      renderCurrentResultScope();

      const summaryParts = [];
      if (successCount > 0) summaryParts.push(`完成 ${successCount} 部`);
      if (failCount > 0) summaryParts.push(`失败 ${failCount} 部`);
      if (skipCount > 0) summaryParts.push(`跳过 ${skipCount} 部`);
      const summaryText = summaryParts.length > 0 ? summaryParts.join('，') : '没有需要处理的影片';

      if (state.cancelRequested) {
        setStatus('idle', '刮削已停止');
        setProgressStatus('idle', `${summaryText}，任务已停止`);
      } else if (failCount === 0) {
        setStatus('completed', '刮削完成');
        setProgressStatus('completed', summaryText);
      } else {
        setStatus('error', '刮削结束，存在失败');
        setProgressStatus('error', summaryText);
      }
      appendLog('info', `任务结束：${summaryText}`);
      void showLibraryMetadataCompletionAlert({
        status: state.cancelRequested ? 'stopped' : failCount === 0 ? 'completed' : 'error',
        message: `本次刮削：共选择 ${selectedCount} 部影片。\n${summaryText}`
      });
      } finally {
        if (desktopApi && typeof desktopApi.finishLibraryMetadataJob === 'function') {
          await desktopApi.finishLibraryMetadataJob({ jobId }).catch(() => {});
        }
        if (state.activeJobId === jobId) {
          state.activeJobId = '';
          state.busy = false;
          state.cancelRequested = false;
        }
        if (elements.stopLibraryScrapeButton) {
          elements.stopLibraryScrapeButton.disabled = true;
          elements.stopLibraryScrapeButton.textContent = '停止刮削';
        }
      }
    }

    async function handleStopLibraryScrape() {
      const jobId = normalizeText(state.activeJobId);
      if (!state.busy || !jobId) {
        return;
      }
      state.cancelRequested = true;
      setStatus('running', '正在停止...');
      setProgressStatus('running', '正在取消剩余任务');
      if (elements.stopLibraryScrapeButton) {
        elements.stopLibraryScrapeButton.disabled = true;
        elements.stopLibraryScrapeButton.textContent = '停止中...';
      }
      appendLog('warn', '已请求停止媒体库刮削，正在取消当前网络请求...');
      if (desktopApi && typeof desktopApi.cancelLibraryMetadataJob === 'function') {
        await desktopApi.cancelLibraryMetadataJob({ jobId });
      }
    }

    function handleGenerateNfoOnly() {
      return handleScrapeSelected('nfo-only');
    }

    function handleDownloadImagesOnly() {
      return handleScrapeSelected('images-only');
    }

    // 一键扫描 + 自动刮削所有本地视频条目；缺视频只读行不会被选中。
    async function handleScanAndScrapeAll() {
      if (state.busy) {
        appendLog('warn', '当前正在执行，请等待完成');
        return;
      }
      state.busy = true;
      setStatus('running', '扫描中...');
      appendLog('info', '开始扫描本地媒体库并自动刮削...');

      const scanOk = await performScan();
      if (!scanOk) {
        state.busy = false;
        return;
      }

      // 自动选中所有本地视频条目（排除缺视频只读行）
      state.selectedIds.clear();
      state.results.forEach((item, index) => {
        if (item.hasMedia) {
          state.selectedIds.add(index);
        }
      });

      if (state.selectedIds.size === 0) {
        appendLog('warn', '未找到可刮削的本地视频');
        setStatus('idle', '无本地视频');
        state.busy = false;
        return;
      }

      appendLog('info', `扫描完成，自动开始刮削 ${state.selectedIds.size} 部影片`);
      state.busy = false;
      await handleScrapeSelected('all');
    }

    // 只重新处理状态不是“已完整”的本地视频条目；缺视频行和已完整项均跳过。
    async function handleScrapeUnfinished() {
      if (state.busy) {
        appendLog('warn', '当前正在执行，请等待完成');
        return;
      }
      if (state.results.length === 0) {
        appendLog('warn', '请先扫描媒体库');
        return;
      }

      state.selectedIds.clear();
      state.results.forEach((item, index) => {
        if (item.hasMedia && item.status !== '已完整') {
          state.selectedIds.add(index);
        }
      });

      if (state.selectedIds.size === 0) {
        appendLog('info', '没有需要补刮的未完成条目');
        return;
      }

      appendLog('info', `选中 ${state.selectedIds.size} 部未完成影片，开始补刮`);
      await handleScrapeSelected('retry-all');
    }

    function updateItemAfterWrite(item, info, writeResult, source, failed) {
      const next = { ...item };
      // 本次写入成功时要清除之前的失败标记；只有显式失败才置位。
      next.failed = failed === true;
      if (info && info.title) {
        next.title = info.title;
      }
      if (source === 'local-crawler') {
        next.crawlMatch = true;
        next.metadataSource = info.provider || 'local-crawler';
      }
      if (writeResult.nfoPath) {
        next.hasNfo = true;
      }
      if (writeResult.posterPath) {
        next.hasPoster = true;
      }
      if (writeResult.backdropPath) {
        next.hasBackdrop = true;
      }
      if (writeResult.landscapePath) {
        next.hasLandscape = true;
      }
      next.status = computeStatusFromItem(next);
      return next;
    }

    function computeStatusFromItem(item) {
      if (item.hasNfo && item.hasPoster && item.hasBackdrop && item.hasLandscape) {
        return '已完整';
      }
      if (!item.hasNfo) {
        return '缺NFO';
      }
      if (!item.hasPoster) {
        return '缺封面';
      }
      if (!item.hasBackdrop) {
        return '缺背景';
      }
      if (!item.hasLandscape) {
        return '缺横图';
      }
      return '待刮削';
    }

    function resolveStatusLightClass(item) {
      if (!item.hasMedia) {
        return 'no-media';
      }
      if (item.failed) {
        return 'failed';
      }
      if (item.status === '已完整') {
        return 'complete';
      }
      return 'missing';
    }

    function createStatusLight(item) {
      const span = document.createElement('span');
      span.className = `librarymetadata-status-light ${resolveStatusLightClass(item)}`;
      const titles = {
        complete: '已完整',
        missing: '缺某项',
        failed: '刮削失败',
        'no-media': '缺失本地视频'
      };
      span.title = titles[resolveStatusLightClass(item)] || item.status || '待刮削';
      return span;
    }

    // renderResultList renders the merged list of local media rows and missing-video rows.
    // Missing rows (hasMedia === false) are read-only and excluded from scraping.
    function getItemDisplayCode(item) {
      return normalizeText(item && (item.displayCode || item.code || item.number)) || '-';
    }

    function updateResultScopeButton() {
      const button = elements.toggleLibraryResultScopeButton;
      if (!button) {
        return;
      }
      const localOnly = state.resultScope !== 'all';
      button.textContent = localOnly ? '仅显示刮削内容' : '显示全部';
      button.classList.toggle('active', localOnly);
      button.setAttribute('aria-pressed', localOnly ? 'true' : 'false');
      button.title = localOnly
        ? '当前仅显示本地已下载并可刮削的影片'
        : '当前显示所选 JSON 中的全部番号';
    }

    function renderCurrentResultScope() {
      const visibleResults = state.resultScope === 'all'
        ? [...state.localScanResults, ...state.missingScanResults]
        : state.localScanResults;
      renderResultList(visibleResults);
      updateResultScopeButton();
    }

    function renderResultList(mergedResults) {
      const listView = elements.libraryResultList;
      if (!listView) {
        return;
      }

      clearChildren(listView);
      state.results = Array.isArray(mergedResults) ? mergedResults : [];
      state.selectedIds.clear();

      if (state.results.length === 0) {
        const row = document.createElement('tr');
        row.className = 'librarymetadata-empty-row';
        const cell = document.createElement('td');
        cell.colSpan = 10;
        cell.textContent = '点击“扫描库”后在此处显示影片列表。';
        row.appendChild(cell);
        listView.appendChild(row);
        return;
      }

      state.results.forEach((item, index) => {
        const isMissing = !item.hasMedia;
        const row = document.createElement('tr');
        row.dataset.index = String(index);
        if (isMissing) {
          row.classList.add('librarymetadata-missing-row');
          row.title = '爬虫产物中存在该番号，但本地未找到对应视频文件';
        }

        const checkboxCell = document.createElement('td');
        const checkbox = document.createElement('input');
        checkbox.type = 'checkbox';
        checkbox.checked = !isMissing;
        checkbox.disabled = isMissing;
        checkbox.addEventListener('change', () => {
          if (checkbox.checked) {
            state.selectedIds.add(index);
          } else {
            state.selectedIds.delete(index);
          }
        });
        checkboxCell.appendChild(checkbox);
        if (!isMissing) {
          state.selectedIds.add(index);
        }
        row.appendChild(checkboxCell);

        const statusLightCell = document.createElement('td');
        statusLightCell.appendChild(createStatusLight(item));
        row.appendChild(statusLightCell);

        const numberCell = document.createElement('td');
        numberCell.textContent = getItemDisplayCode(item);
        row.appendChild(numberCell);

        const titleCell = document.createElement('td');
        const titleText = item.title || item.mediaStem || '（待刮削后显示标题）';
        titleCell.className = 'librarymetadata-title-cell';
        titleCell.textContent = titleText;
        titleCell.title = titleText;
        row.appendChild(titleCell);

        const matchCell = document.createElement('td');
        if (item.crawlMatch) {
          const badge = document.createElement('span');
          badge.className = 'librarymetadata-source-badge local';
          badge.textContent = item.metadataSource || '本地';
          badge.title = item.metadataSource || '本地爬虫产物';
          matchCell.appendChild(badge);
        } else {
          matchCell.appendChild(createYesNoBadge(false));
        }
        row.appendChild(matchCell);

        const statusCell = document.createElement('td');
        const badge = document.createElement('span');
        badge.className = `librarymetadata-status-badge librarymetadata-status-${normalizeStatusClass(item.status)}`;
        badge.textContent = item.status || '待刮削';
        statusCell.appendChild(badge);
        row.appendChild(statusCell);

        const nfoCell = document.createElement('td');
        nfoCell.appendChild(createYesNoBadge(Boolean(item.hasNfo)));
        row.appendChild(nfoCell);

        const posterCell = document.createElement('td');
        posterCell.appendChild(createYesNoBadge(Boolean(item.hasPoster)));
        row.appendChild(posterCell);

        const backdropCell = document.createElement('td');
        backdropCell.appendChild(createYesNoBadge(Boolean(item.hasBackdrop)));
        row.appendChild(backdropCell);

        const landscapeCell = document.createElement('td');
        landscapeCell.appendChild(createYesNoBadge(Boolean(item.hasLandscape)));
        row.appendChild(landscapeCell);

        listView.appendChild(row);
      });
    }

    // 仅更新指定行，避免整表重绘导致滚动位置与勾选状态丢失
    function updateResultRow(index, item) {
      const listView = elements.libraryResultList;
      if (!listView) {
        return;
      }
      const row = listView.querySelector(`tr[data-index="${index}"]`);
      if (!row) {
        return;
      }

      const cells = row.querySelectorAll('td');
      if (cells.length < 10) {
        return;
      }

      const statusLightCell = cells[1];
      clearChildren(statusLightCell);
      statusLightCell.appendChild(createStatusLight(item));

      cells[2].textContent = getItemDisplayCode(item);
      const titleText = item.title || item.mediaStem || '（待刮削后显示标题）';
      cells[3].classList.add('librarymetadata-title-cell');
      cells[3].textContent = titleText;
      cells[3].title = titleText;

      const matchCell = cells[4];
      clearChildren(matchCell);
      if (item.crawlMatch) {
        const badge = document.createElement('span');
        badge.className = 'librarymetadata-source-badge local';
        badge.textContent = item.metadataSource || '本地';
        badge.title = item.metadataSource || '本地爬虫产物';
        matchCell.appendChild(badge);
      } else {
        matchCell.appendChild(createYesNoBadge(false));
      }

      const statusCell = cells[5];
      clearChildren(statusCell);
      const statusBadge = document.createElement('span');
      statusBadge.className = `librarymetadata-status-badge librarymetadata-status-${normalizeStatusClass(item.status)}`;
      statusBadge.textContent = item.status || '待刮削';
      statusCell.appendChild(statusBadge);

      const updateYesNoCell = (cell, value) => {
        clearChildren(cell);
        cell.appendChild(createYesNoBadge(Boolean(value)));
      };
      updateYesNoCell(cells[6], item.hasNfo);
      updateYesNoCell(cells[7], item.hasPoster);
      updateYesNoCell(cells[8], item.hasBackdrop);
      updateYesNoCell(cells[9], item.hasLandscape);
    }

    function createYesNoBadge(yes) {
      const span = document.createElement('span');
      span.className = yes ? 'librarymetadata-yes' : 'librarymetadata-no';
      span.textContent = yes ? '✓' : '-';
      return span;
    }

    function normalizeStatusClass(status) {
      const normalized = String(status || '').trim();
      if (normalized === '已完整') {
        return 'complete';
      }
      if (normalized === '未下载') {
        return 'unrecognized';
      }
      if (normalized === '缺NFO' || normalized === '缺封面' || normalized === '缺背景' || normalized === '缺横图') {
        return 'missing';
      }
      return 'pending';
    }

    // performScan returns true when the scan produced usable results.
    async function performScan() {
      const root = normalizeText(elements.libraryRootPath && elements.libraryRootPath.value);
      if (!root) {
        appendLog('warn', '请先选择媒体库根目录');
        setStatus('idle', '等待扫描');
        setProgressStatus('idle', '等待扫描');
        return false;
      }

      await initLibraryLog(root);
      setProgressStatus('running', '正在扫描...');

      const extensionsRaw = normalizeText(elements.scanFileTypes && elements.scanFileTypes.value);
      const extensions = extensionsRaw
        ? extensionsRaw.split(/[,，]/).map((ext) => ext.trim()).filter(Boolean)
        : [];

      const outputMode = normalizeText(elements.libraryOutputMode && elements.libraryOutputMode.value) || 'inplace';
      const ignoreDirsRaw = normalizeText(elements.hiddenArtifactPath && elements.hiddenArtifactPath.value);
      const showHiddenFiles = isShowHiddenFilesEnabled();
      const ignoreDirs = showHiddenFiles
        ? []
        : (ignoreDirsRaw
            ? ignoreDirsRaw.split(/[,，]/).map((dir) => dir.trim()).filter(Boolean)
            : []);
      if (showHiddenFiles) {
        appendLog('info', '已启用“显示隐藏路径文件”，扫描将包含忽略目录中的视频。');
      }
      const crawlOutputDir = normalizeText(elements.crawlArtifactPath && elements.crawlArtifactPath.value);

      try {
        const result = await desktopApi.scanLibraryMetadata({
          root,
          extensions,
          ignoreDirs,
          outputMode,
          crawlOutputDir
        });

        if (result && result.error) {
          appendLog('error', `扫描失败：${result.error}`);
          setStatus('error', '扫描失败');
          return false;
        }

        const items = Array.isArray(result && result.items) ? result.items : [];
        const missingItems = Array.isArray(result && result.missingItems) ? result.missingItems : [];
        state.localScanResults = items;
        state.missingScanResults = missingItems;
        renderCurrentResultScope();
        const missingCount = missingItems.length;
        const logMessage = missingCount > 0
          ? `扫描完成，本地发现 ${items.length} 部影片，爬虫产物中另有 ${missingCount} 部未找到本地视频`
          : `扫描完成，共发现 ${items.length} 部影片`;
        appendLog('info', logMessage);
        setStatus('completed', '扫描完成');
        setProgressStatus('completed', `共 ${items.length} 部影片${missingCount > 0 ? `，缺视频 ${missingCount} 部` : ''}`);
        return true;
      } catch (error) {
        appendLog('error', `扫描失败：${getErrorMessage(error)}`);
        setStatus('error', '扫描失败');
        setProgressStatus('error', '扫描失败');
        return false;
      }
    }

    function bindEvents() {
      if (eventsBound) {
        return;
      }
      eventsBound = true;

      if (elements.browseLibraryRootButton) {
        bindAsyncButton(elements.browseLibraryRootButton, async () => {
          const selected = await desktopApi.chooseOutput();
          if (selected && elements.libraryRootPath) {
            elements.libraryRootPath.value = selected;
          }
        });
      }

      if (elements.browseCrawlArtifactsButton) {
        bindAsyncButton(elements.browseCrawlArtifactsButton, async () => {
          const selected = await desktopApi.chooseOutput();
          if (selected && elements.crawlArtifactPath) {
            elements.crawlArtifactPath.value = selected;
            appendLog('info', `已选择爬虫结果目录：${selected}`);
            await refreshCrawlArtifactCountFeedback(selected);
          }
        });
      }

      if (elements.browseHiddenArtifactsButton) {
        bindAsyncButton(elements.browseHiddenArtifactsButton, async () => {
          const selected = await desktopApi.chooseOutput();
          if (selected && elements.hiddenArtifactPath) {
            elements.hiddenArtifactPath.value = selected;
          }
        });
      }

      if (elements.refreshCrawlSourcesButton) {
        bindAsyncButton(elements.refreshCrawlSourcesButton, () => loadCrawlSources());
      }

      if (elements.crawlSourceSnapshot) {
        elements.crawlSourceSnapshot.addEventListener('change', () => {
          const selectedOutputDir = normalizeText(elements.crawlSourceSnapshot.value);
          if (selectedOutputDir && elements.crawlArtifactPath) {
            elements.crawlArtifactPath.value = selectedOutputDir;
            appendLog('info', `已选择历史快照：${selectedOutputDir}`);
            void refreshCrawlArtifactCountFeedback(selectedOutputDir);
          }
        });
      }

      if (elements.crawlArtifactPath) {
        const refreshSelectedArtifactCount = () => {
          void refreshCrawlArtifactCountFeedback(elements.crawlArtifactPath.value);
        };
        elements.crawlArtifactPath.addEventListener('change', refreshSelectedArtifactCount);
        elements.crawlArtifactPath.addEventListener('blur', refreshSelectedArtifactCount);
      }

      if (elements.libraryScrapeConcurrency) {
        elements.libraryScrapeConcurrency.addEventListener('change', () => {
          const concurrency = getScrapeConcurrency();
          elements.libraryScrapeConcurrency.value = String(concurrency);
          try {
            globalScope.localStorage.setItem(SCRAPE_CONCURRENCY_STORAGE_KEY, String(concurrency));
          } catch (storageError) {}
          persistWorkspacePreferences({ libraryScrapeConcurrency: concurrency });
        });
      }

      if (elements.libraryAutoSubscribeLeadActor) {
        elements.libraryAutoSubscribeLeadActor.addEventListener('change', () => {
          try {
            globalScope.localStorage.setItem(
              AUTO_SUBSCRIBE_STORAGE_KEY,
              elements.libraryAutoSubscribeLeadActor.checked ? '1' : '0'
            );
          } catch (storageError) {}
          persistWorkspacePreferences({
            libraryAutoSubscribeFromOutput: elements.libraryAutoSubscribeLeadActor.checked
          });
        });
      }

      if (elements.showHiddenFilesCheckbox) {
        elements.showHiddenFilesCheckbox.addEventListener('change', () => {
          const enabled = isShowHiddenFilesEnabled();
          saveShowHiddenFilesPreference(enabled);
          appendLog('info', enabled ? '已启用显示隐藏路径文件，下次扫描将包含忽略目录。' : '已关闭显示隐藏路径文件，下次扫描将忽略指定目录。');
        });
      }

      if (elements.toggleLibraryLayoutButton) {
        bindAsyncButton(elements.toggleLibraryLayoutButton, toggleLayoutMode);
      }

      bindAsyncButton(elements.testScrapeButton, handleTestScrape);
      bindAsyncButton(elements.rehydrateCodeButton, handleRehydrateCode);
      bindAsyncButton(elements.generateNfoOnlyButton, handleGenerateNfoOnly);
      bindAsyncButton(elements.downloadImagesOnlyButton, handleDownloadImagesOnly);
      bindAsyncButton(elements.scanAndScrapeAllButton, handleScanAndScrapeAll);
      bindAsyncButton(elements.scrapeUnfinishedButton, handleScrapeUnfinished);
      bindAsyncButton(elements.stopLibraryScrapeButton, handleStopLibraryScrape);
      bindAsyncButton(elements.scanLibraryButton, async () => {
        setStatus('running', '扫描中...');
        appendLog('info', '开始扫描本地媒体库...');
        await performScan();
      });

      if (elements.openLibraryLogButton) {
        bindAsyncButton(elements.openLibraryLogButton, async () => {
          if (!desktopApi || typeof desktopApi.openLibraryMetadataLogFolder !== 'function') {
            appendLog('warn', '打开日志目录 API 尚未就绪');
            return;
          }
          const root = normalizeText(elements.libraryRootPath && elements.libraryRootPath.value);
          if (!root) {
            appendLog('warn', '请先选择媒体库根目录');
            return;
          }
          await initLibraryLog(root);
          const opened = await desktopApi.openLibraryMetadataLogFolder({ rootPath: root });
          if (opened) {
            appendLog('info', `已打开日志目录：${opened}`);
          }
        });
      }

      if (elements.scrapeSelectedButton) {
        bindAsyncButton(elements.scrapeSelectedButton, handleScrapeSelected);
      }

      if (elements.toggleLibraryResultScopeButton) {
        elements.toggleLibraryResultScopeButton.addEventListener('click', () => {
          if (state.busy) {
            appendLog('warn', '刮削进行中，任务结束后再切换影片清单范围');
            return;
          }
          state.resultScope = state.resultScope === 'all' ? 'local' : 'all';
          renderCurrentResultScope();
        });
      }

      if (desktopApi && typeof desktopApi.onLibraryMetadataScanProgress === 'function') {
        desktopApi.onLibraryMetadataScanProgress((progress) => {
          const scanned = Number.isFinite(Number(progress && progress.scannedFiles)) ? Number(progress.scannedFiles) : 0;
          const matched = Number.isFinite(Number(progress && progress.matchedItems)) ? Number(progress.matchedItems) : 0;
          setProgressStatus('running', `正在扫描：已扫 ${scanned} 个文件，命中 ${matched} 部`);
        });
      }
    }

    function bootstrap() {
      if (bootstrapCompleted) {
        return Promise.resolve();
      }
      bootstrapCompleted = true;
      appendLog('info', '媒体库刮削模块已就绪');
      restoreLayoutMode();
      restoreScrapePreferences();
      restoreShowHiddenFilesPreference();
      setProgressStatus('idle', '等待扫描');
      bindEvents();
      initializeProxyDetection();

      hydrationPromise = hydrateLibraryMetadataData().catch((error) => {
        appendLog('error', `媒体库后台数据加载失败：${getErrorMessage(error)}`);
      });
      return Promise.resolve();
    }

    return {
      bootstrap,
      loadCrawlSources,
      hydrateLibraryMetadataData
    };
  }

  globalScope.desktopLibraryMetadataController = {
    createLibraryMetadataController
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
