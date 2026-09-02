// AV subscription controller.
// Subscription controller manages lightweight crawl seeds plus refresh results.
// This controller owns the subscription workspace only:
// 1) import / manual baseline creation
// 2) update detection
// 3) pending-code crawl planning and execution handoff
//
// The workflow keeps the subscription UI in one place and lets the Go bridge
// own the durable state.
//
// Ownership summary:
//   This controller owns the subscription workspace UI and event lifecycle.
//
// File map for maintainers:
// 1) state projection and formatting helpers
// 2) subscription import / manual create / refresh / plan actions
// 3) recent crawl list projection and proxy/status helpers
// 4) event binding and bootstrap
(function initializeSubscriptionController(globalScope) {
  const rendererHelpers = globalScope.desktopRendererHelpers || {};
  const getErrorMessage = rendererHelpers.getErrorMessage;
  const toSafeInteger = rendererHelpers.toSafeInteger;
  const clearChildren = rendererHelpers.clearChildren;
  const createElement = rendererHelpers.createElement;
  const createOption = rendererHelpers.createOption;
  const getElementById = rendererHelpers.getElementById;
  const select = rendererHelpers.querySelector;
  const selectAll = rendererHelpers.querySelectorAll;
  const appendTimestampedLogLine = rendererHelpers.appendTimestampedLogLine;
  const bindAsyncClickHelper = rendererHelpers.bindAsyncClick || null;
  const createBufferedLogAppender = rendererHelpers.createBufferedLogAppender || null;
  const SUBSCRIPTION_DEFAULT_PARALLEL = 2;
  const SUBSCRIPTION_DEFAULT_DELAY = 2;
  const SUBSCRIPTION_DEFAULT_TIMEOUT = 30000;
  const SUBSCRIPTION_PROXY_AUTO_CHECK_INTERVAL_MS = 30000;

  if (!getErrorMessage || !toSafeInteger || !clearChildren || !appendTimestampedLogLine) {
    throw new Error('desktopRendererHelpers must be loaded before subscriptionController');
  }

  function createSubscriptionController(options) {
    const { elements, desktopApi, uiText, formController, subscriptionCrawlSessionBridge } = options || {};
    const subscriptionListView = globalScope.desktopSubscriptionListView || null;
    const artifactInputHelperFactory = globalScope.desktopArtifactInputHelper || null;

    if (!subscriptionListView) {
      throw new Error('desktopSubscriptionListView is required before subscriptionController');
    }
    if (!artifactInputHelperFactory) {
      throw new Error('desktopArtifactInputHelper is required before subscriptionController');
    }

    const state = {
      subscriptions: [],
      recentCrawlOptions: [],
      selectedId: '',
      logs: [],
      pendingImportedJson: null,
      antiBlockReady: false,
      activeCrawlSession: null,
      preparedDraft: null,
      defaultOutputDir: '',
      mediaHydrationRunning: false,
      reorderRunning: false,
      pendingReorderIDs: null,
      activeBatchCrawl: null,
      listSearch: '',
      listSearchCanonical: '',
      listSearchRequestToken: 0,
      listFilter: 'all',
      listSort: 'updated',
      globalProxy: ''
    };
    const subscriptionStageRenderer =
      globalScope.desktopStagePanelRenderer && typeof globalScope.desktopStagePanelRenderer.createStagePanelRenderer === 'function'
        ? globalScope.desktopStagePanelRenderer.createStagePanelRenderer({
            crawlStageStatus: elements.subscriptionCrawlStageStatus,
            crawlStageProgress: elements.subscriptionCrawlStageProgress,
            crawlStageTitle: elements.subscriptionCrawlStageTitle,
            crawlStageDescription: elements.subscriptionCrawlStageDescription,
            crawlStageMessage: elements.subscriptionCrawlStageMessage,
            crawlStageBarFill: elements.subscriptionCrawlStageBarFill,
            crawlStageBar: elements.subscriptionCrawlStageBar,
            crawlStageOutput: elements.subscriptionCrawlStageOutput,
            crawlStagePage: elements.subscriptionCrawlStagePage,
            crawlStageQueued: elements.subscriptionCrawlStageQueued,
            crawlStageAttempted: elements.subscriptionCrawlStageAttempted,
            crawlStageCompleted: elements.subscriptionCrawlStageCompleted,
            statusLabels: {
              idle: '待机',
              starting: '准备中',
              running: '抓取中',
              completed: '已完成',
              incomplete: '未完全完成',
              stopped: '已停止',
              error: '失败'
            },
            defaultMessage: '等待开始抓取。'
          })
        : null;
    let eventsBound = false;
    let bootstrapCompleted = false;
    let bootstrapPromise = null;
    let hydrationPromise = null;
    let subscriptionSearchResolveTimerId = null;

    function publishActiveSubscriptionCrawlSession(session) {
      // Let the shared crawler runtime view know when the public crawl feed is
      // temporarily owned by the AV-subscription bridge. The bridge still
      // reuses the main crawler execution path, but the visible logs/status
      // should stay inside the subscription workspace while this session lives.
      //
      // 审计 H-09：通过显式 bridge 共享 session，不再读写隐式全局变量。
      const normalizedSession = session && typeof session === 'object' ? { ...session } : null;
      if (subscriptionCrawlSessionBridge && typeof subscriptionCrawlSessionBridge.setSession === 'function') {
        subscriptionCrawlSessionBridge.setSession(normalizedSession);
      }
      if (subscriptionCrawlSessionBridge && typeof subscriptionCrawlSessionBridge.onSessionChange === 'function') {
        subscriptionCrawlSessionBridge.onSessionChange(normalizedSession);
      }
    }

    const proxyValidationState = {
      timerId: null,
      autoTimerId: null,
      requestToken: 0,
      lastValue: '',
      lastStatus: 'empty',
      autoValidationStopped: false,
      autoValidationCount: 0
    };

    const subscriptionLogBuffer =
      typeof createBufferedLogAppender === 'function'
        ? createBufferedLogAppender({
            logView: elements.subscriptionLogView,
            tagName: 'p'
          })
        : null;

    const artifactInputHelper = artifactInputHelperFactory.createArtifactInputHelper({
      labels: {
        snapshot: '订阅快照',
        fallback: '抓取结果输入'
      },
      latestInputOptions: {
        artifactKey: 'preferredCrawlProfilePath',
        artifactType: 'crawlProfile'
      },
      readCurrentValue: () => getCurrentSubscriptionArtifactInput(),
      writeCurrentValue: (artifactInput) => applySubscriptionArtifactInputValue(artifactInput)
    });

    function appendLog(level, message) {
      const text = String(message || '').trim();
      if (!text) {
        return;
      }

      state.logs.unshift({
        level: String(level || 'info').trim() || 'info',
        message: text,
        timestamp: new Date().toISOString()
      });
      state.logs = state.logs.slice(0, 120);

      if (subscriptionLogBuffer) {
        subscriptionLogBuffer.append(level, text, null);
        return;
      }
      appendTimestampedLogLine(elements.subscriptionLogView, level, text, null, { tagName: 'p' });
    }

    function setSummaryMessage(message) {
      if (elements.subscriptionSummaryMessage) {
        elements.subscriptionSummaryMessage.textContent = String(message || '等待检测订阅更新。').trim();
      }
    }

    function normalizeCount(value, fallback = 0) {
      const parsed = toSafeInteger(value, fallback);
      return parsed < 0 ? fallback : Math.max(0, parsed);
    }

    function normalizeActressFilterThreshold(value) {
      const parsed = Number.parseInt(String(value == null ? '' : value).trim(), 10);
      return Number.isFinite(parsed) ? Math.max(0, parsed) : 0;
    }

    function parseTimestamp(value) {
      const raw = String(value || '').trim();
      if (!raw) {
        return 0;
      }
      const parsed = Date.parse(raw);
      return Number.isFinite(parsed) ? parsed : 0;
    }

    function normalizeText(value) {
      return String(value || '').trim();
    }

    function normalizeCodes(values) {
      const seen = new Set();
      const output = [];
      (Array.isArray(values) ? values : []).forEach((value) => {
        const code = normalizeText(value).toUpperCase();
        if (!code || seen.has(code)) {
          return;
        }
        seen.add(code);
        output.push(code);
      });
      return output;
    }

    function renderCodePreview(values, emptyText = '无内容', limit = 12) {
      const codes = normalizeCodes(values);
      if (codes.length === 0) {
        return `<span class="subscription-code-preview">${escapeHtml(emptyText)}</span>`;
      }
      const visibleCodes = codes.slice(0, Math.max(1, limit));
      const remaining = Math.max(0, codes.length - visibleCodes.length);
      const summary = `${visibleCodes.join('、')}${remaining > 0 ? ` 等 ${remaining} 个` : ''}`;
      const fullText = codes.join('、');
      return `<span class="subscription-code-preview${remaining > 0 ? ' is-truncated' : ''}" title="${escapeHtml(fullText)}">${escapeHtml(summary)}</span>`;
    }

    function resolveSubscriptionBaselineCount(item) {
      // For manual subscriptions, show user's declared total as "基数"
      if (item && item.sourceType === 'manual') {
        const declared = normalizeCount(item.manualDeclaredTotal, -1);
        if (declared >= 0) {
          return declared;
        }
      }

      const directValue = normalizeCount(item && item.baselineCount, -1);
      if (directValue >= 0) {
        return directValue;
      }

      const syncedValue = normalizeCount(item && item.syncedCount, -1);
      if (syncedValue >= 0) {
        return syncedValue;
      }

      return normalizeCodes(item && item.baselineCodes).length;
    }

    function resolveSubscriptionCurrentCount(item) {
      const directValue = normalizeCount(item && item.currentObservedCount, -1);
      if (directValue >= 0) {
        return directValue;
      }

      const currentValue = normalizeCount(item && item.currentCount, -1);
      if (currentValue >= 0) {
        return currentValue;
      }

      return resolveSubscriptionBaselineCount(item) + normalizeCount(item && item.pendingCount, 0);
    }

    function resolveSubscriptionCurrentTotal(item) {
      const directValue = normalizeCount(item && item.currentTotal, -1);
      if (directValue >= 0) {
        return directValue;
      }

      return resolveSubscriptionBaselineCount(item);
    }

    function sortSubscriptions(items) {
      return (Array.isArray(items) ? items : []).slice().sort((left, right) => {
        const leftOrder = normalizeCount(left && left.sortOrder, 0);
        const rightOrder = normalizeCount(right && right.sortOrder, 0);
        if (leftOrder > 0 || rightOrder > 0) {
          if (leftOrder <= 0) return 1;
          if (rightOrder <= 0) return -1;
          if (leftOrder !== rightOrder) return leftOrder - rightOrder;
        }
        const pendingDiff = normalizeCount(right && right.pendingCount, 0) - normalizeCount(left && left.pendingCount, 0);
        if (pendingDiff !== 0) {
          return pendingDiff;
        }
        const updatedDiff = parseTimestamp(right && right.lastUpdatedAt) - parseTimestamp(left && left.lastUpdatedAt);
        if (updatedDiff !== 0) {
          return updatedDiff;
        }
        const leftName = normalizeText(left && left.actressName);
        const rightName = normalizeText(right && right.actressName);
        if (leftName !== rightName) {
          return leftName.localeCompare(rightName, 'zh-CN');
        }
        return normalizeText(left && left.id).localeCompare(normalizeText(right && right.id), 'zh-CN');
      });
    }

    function sortSubscriptionItems(items, mode = state.listSort) {
      const list = (Array.isArray(items) ? items : []).slice();
      if (mode === 'manual') {
        return sortSubscriptions(list);
      }

      const timestamp = (item, field) => parseTimestamp(item && item[field]);
      return list.sort((left, right) => {
        const leftPending = normalizeCount(left && left.pendingCount, 0);
        const rightPending = normalizeCount(right && right.pendingCount, 0);
        const leftDetected = timestamp(left, 'lastUpdateDetectedAt');
        const rightDetected = timestamp(right, 'lastUpdateDetectedAt');
        const leftChecked = timestamp(left, 'lastCheckedAt');
        const rightChecked = timestamp(right, 'lastCheckedAt');

        if (mode === 'updated' || mode === 'pending') {
          if (rightPending !== leftPending) return rightPending - leftPending;
        }
        if (mode === 'updated' || mode === 'detected') {
          if (rightDetected !== leftDetected) return rightDetected - leftDetected;
        }
        if (mode === 'checked' && rightChecked !== leftChecked) {
          return rightChecked - leftChecked;
        }
        if (mode === 'updated' && rightChecked !== leftChecked) {
          return rightChecked - leftChecked;
        }
        if (mode === 'name') {
          const nameOrder = normalizeText(left && left.actressName).localeCompare(
            normalizeText(right && right.actressName),
            'zh-CN'
          );
          if (nameOrder !== 0) return nameOrder;
        }

        if (rightPending !== leftPending) return rightPending - leftPending;
        if (rightDetected !== leftDetected) return rightDetected - leftDetected;
        return normalizeText(left && left.actressName).localeCompare(normalizeText(right && right.actressName), 'zh-CN');
      });
    }

    function subscriptionSearchTerms() {
      const terms = new Set();
      for (const value of [state.listSearch, state.listSearchCanonical]) {
        const normalized = normalizeText(value).toLocaleLowerCase('zh-CN');
        if (normalized) {
          terms.add(normalized);
        }
      }
      return Array.from(terms);
    }

    function refreshSubscriptionListForSearch() {
      setSummaryCounts();
      renderSubscriptionList();
    }

    function scheduleSubscriptionAliasResolution(search) {
      if (subscriptionSearchResolveTimerId !== null) {
        clearTimeout(subscriptionSearchResolveTimerId);
        subscriptionSearchResolveTimerId = null;
      }
      if (!search || typeof desktopApi.resolveActressAlias !== 'function') {
        return;
      }

      const requestToken = state.listSearchRequestToken;
      subscriptionSearchResolveTimerId = setTimeout(() => {
        subscriptionSearchResolveTimerId = null;
        void desktopApi
          .resolveActressAlias({ actorName: search, localOnly: true })
          .then((alias) => {
            if (requestToken !== state.listSearchRequestToken || state.listSearch !== search) {
              return;
            }
            state.listSearchCanonical = normalizeText(alias && alias.name);
            refreshSubscriptionListForSearch();
          })
          // A missing or ambiguous alias keeps the original-text search active.
          .catch(() => {});
      }, 140);
    }

    function updateSubscriptionListSearch(value) {
      state.listSearch = normalizeText(value);
      state.listSearchCanonical = '';
      state.listSearchRequestToken += 1;
      refreshSubscriptionListForSearch();
      scheduleSubscriptionAliasResolution(state.listSearch);
    }

    function isSubscriptionVisible(item) {
      const searchTerms = subscriptionSearchTerms();
      const name = normalizeText(item && item.actressName).toLocaleLowerCase('zh-CN');
      if (searchTerms.length > 0 && !searchTerms.some((search) => name.includes(search))) {
        return false;
      }

      const pending = normalizeCount(item && item.pendingCount, 0);
      const hasError = Boolean(normalizeText(item && item.lastError));
      if (state.listFilter === 'updated' && pending <= 0) return false;
      if (state.listFilter === 'pending' && pending <= 0) return false;
      if (state.listFilter === 'error' && !hasError) return false;
      if (state.listFilter === 'unchecked' && normalizeText(item && item.lastCheckedAt)) return false;
      return true;
    }

    function visibleSubscriptionList() {
      return sortSubscriptionItems(state.subscriptions).filter(isSubscriptionVisible);
    }

    function setRefreshSummary(text) {
      if (elements.subscriptionRefreshSummary) {
        elements.subscriptionRefreshSummary.textContent = normalizeText(text) || '尚未执行批量检测。';
      }
    }

    function setSummaryCounts() {
      const list = Array.isArray(state.subscriptions) ? state.subscriptions : [];
      const total = list.length;
      const updated = list.filter((item) => normalizeCount(item.pendingCount, 0) > 0).length;
      const checked = list.filter((item) => normalizeText(item.lastCheckedAt)).length;

      if (elements.subscriptionStatTotal) elements.subscriptionStatTotal.textContent = String(total);
      if (elements.subscriptionStatUpdated) elements.subscriptionStatUpdated.textContent = String(updated);
      if (elements.subscriptionStatChecked) elements.subscriptionStatChecked.textContent = String(checked);
      if (elements.subscriptionListResultCount) {
        elements.subscriptionListResultCount.textContent = `显示 ${visibleSubscriptionList().length} / ${total} 位`;
      }
    }

    function applySubscriptionStagePanel(panel = {}) {
      if (subscriptionStageRenderer && typeof subscriptionStageRenderer.applyPanel === 'function') {
        subscriptionStageRenderer.applyPanel(panel || {});
      }
    }

    function setSubscriptionCrawlerStatus(message) {
      if (elements.subscriptionCrawlerStatus) {
        elements.subscriptionCrawlerStatus.textContent = String(message || '待启动').trim() || '待启动';
      }
    }

    function clearActiveCrawlSession() {
      state.activeCrawlSession = null;
      publishActiveSubscriptionCrawlSession(null);
      renderSubscriptionList();
    }

    function setActiveCrawlSession(session) {
      state.activeCrawlSession = session && typeof session === 'object' ? { ...session } : null;
      publishActiveSubscriptionCrawlSession(state.activeCrawlSession);
      renderSubscriptionList();
    }

    function getActiveCrawlSession() {
      return state.activeCrawlSession && typeof state.activeCrawlSession === 'object'
        ? { ...state.activeCrawlSession }
        : null;
    }

    function getBridgeCrawlSession() {
      const active = getActiveCrawlSession();
      if (active) {
        return active;
      }
      if (state.activeBatchCrawl) {
        return {
          subscriptionId: '__subscription_batch__',
          kind: 'batch',
          outputDir: normalizeText(state.activeBatchCrawl.outputDir)
        };
      }
      return null;
    }

    async function resolveSubscriptionDefaultOutputDir() {
      if (!desktopApi || typeof desktopApi.getIntegrationContext !== 'function') {
        return '';
      }

      try {
        const context = await desktopApi.getIntegrationContext();
        const appPath = normalizeText(context && context.appPath);
        return appPath ? `${appPath}\\AV订阅` : '';
      } catch (error) {
        appendLog('warn', `订阅默认输出目录初始化失败: ${getErrorMessage(error)}`);
        return '';
      }
    }

    function setSubscriptionProxyStatus(status, detailText = '') {
      const normalized = status === 'checking' || status === 'valid' || status === 'global' || status === 'invalid' ? status : 'empty';
      proxyValidationState.lastStatus = normalized;

      const statusTextMap = {
        empty: '未检测',
        checking: '检测中...',
        valid: '代理正常',
        global: '已连接',
        invalid: '代理失败'
      };
      const detailMap = {
        empty: normalizeText(state.globalProxy)
          ? '正在使用 JAV 爬虫中保存的代理。'
          : '尚未填写代理地址。请先在 JAV 爬虫中填写，或在此临时输入。',
        checking: '正在检测订阅代理连通性，请稍候。',
        valid: '检测通过，可继续使用当前订阅代理。',
        global: `已连接：${state.globalProxy}。可直接修改此处地址，仅临时覆盖当前订阅操作。`,
        invalid: '当前订阅代理不可用，请检查代理地址或代理软件状态。'
      };

      if (elements.subscriptionProxyStatus) {
        elements.subscriptionProxyStatus.className = `proxy-status-chip ${normalized}`;
        elements.subscriptionProxyStatus.textContent = statusTextMap[normalized];
      }
      if (elements.subscriptionProxyStatusDetail) {
        elements.subscriptionProxyStatusDetail.textContent =
          normalizeText(detailText) || detailMap[normalized];
      }
    }

    function applyDefaultSubscriptionProxy() {
      if (!elements.subscriptionCrawlerProxy) {
        return normalizeText(state.globalProxy);
      }
      const resolved = normalizeText(elements.subscriptionCrawlerProxy.value) || normalizeText(state.globalProxy);
      elements.subscriptionCrawlerProxy.value = resolved;
      return resolved;
    }

    function applyGlobalSubscriptionProxy(settings) {
      const previousGlobalProxy = normalizeText(state.globalProxy);
      state.globalProxy = normalizeText(settings && settings.proxy);
      if (elements.subscriptionCrawlerProxy) {
        const currentValue = normalizeText(elements.subscriptionCrawlerProxy.value);
        // Preserve an explicit per-module override. Empty values and values
        // inherited from the previous global setting track the latest crawler
        // proxy automatically.
        if (!currentValue || currentValue === previousGlobalProxy) {
          elements.subscriptionCrawlerProxy.value = state.globalProxy;
        }
      }
      return resolveSubscriptionProxyValue();
    }

    function resolveSubscriptionProxyValue() {
      return normalizeText(elements.subscriptionCrawlerProxy && elements.subscriptionCrawlerProxy.value) || normalizeText(state.globalProxy);
    }

    function isUsingGlobalSubscriptionProxy(proxyValue) {
      const globalProxy = normalizeText(state.globalProxy);
      return Boolean(globalProxy && normalizeText(proxyValue) === globalProxy);
    }

    async function loadGlobalSubscriptionProxy() {
      if (!desktopApi || typeof desktopApi.getSettings !== 'function') {
        return '';
      }
      try {
        const settings = await desktopApi.getSettings();
        return applyGlobalSubscriptionProxy(settings);
      } catch (error) {
        appendLog('warn', `读取已保存代理失败：${getErrorMessage(error)}`);
        return resolveSubscriptionProxyValue();
      }
    }

    function applySubscriptionCrawlerDefaults() {
      if (elements.subscriptionCrawlerParallel) {
        elements.subscriptionCrawlerParallel.value = String(
          normalizeCount(elements.subscriptionCrawlerParallel.value, SUBSCRIPTION_DEFAULT_PARALLEL) || SUBSCRIPTION_DEFAULT_PARALLEL
        );
      }
      if (elements.subscriptionCrawlerDelay) {
        elements.subscriptionCrawlerDelay.value = String(
          normalizeCount(elements.subscriptionCrawlerDelay.value, SUBSCRIPTION_DEFAULT_DELAY)
        );
      }
      if (elements.subscriptionCrawlerTimeout) {
        elements.subscriptionCrawlerTimeout.value = String(
          Math.max(1000, normalizeCount(elements.subscriptionCrawlerTimeout.value, SUBSCRIPTION_DEFAULT_TIMEOUT) || SUBSCRIPTION_DEFAULT_TIMEOUT)
        );
      }
    }

    function resolveSubscriptionCrawlerParallel() {
      return Math.max(1, normalizeCount(elements.subscriptionCrawlerParallel && elements.subscriptionCrawlerParallel.value, SUBSCRIPTION_DEFAULT_PARALLEL) || SUBSCRIPTION_DEFAULT_PARALLEL);
    }

    function resolveSubscriptionCrawlerDelay() {
      return Math.max(0, normalizeCount(elements.subscriptionCrawlerDelay && elements.subscriptionCrawlerDelay.value, SUBSCRIPTION_DEFAULT_DELAY));
    }

    function resolveSubscriptionCrawlerTimeout() {
      return Math.max(1000, normalizeCount(elements.subscriptionCrawlerTimeout && elements.subscriptionCrawlerTimeout.value, SUBSCRIPTION_DEFAULT_TIMEOUT) || SUBSCRIPTION_DEFAULT_TIMEOUT);
    }

    function resolveSubscriptionCloudflareEnabled() {
      if (!elements.subscriptionCrawlerCloudflare) {
        return true;
      }
      return Boolean(elements.subscriptionCrawlerCloudflare.checked);
    }

    function buildSubscriptionRuntimePayload(extra = {}) {
      return {
        ...extra,
        proxy: resolveSubscriptionProxyValue(),
        parallel: resolveSubscriptionCrawlerParallel(),
        delay: resolveSubscriptionCrawlerDelay(),
        timeout: resolveSubscriptionCrawlerTimeout(),
        cloudflare: resolveSubscriptionCloudflareEnabled(),
        secondValidation: true
      };
    }

    function clearSubscriptionProxyValidationTimer() {
      if (!proxyValidationState.timerId) {
        return;
      }
      clearTimeout(proxyValidationState.timerId);
      proxyValidationState.timerId = null;
    }

    function clearSubscriptionProxyAutoValidationTimer() {
      if (!proxyValidationState.autoTimerId) {
        return;
      }
      clearTimeout(proxyValidationState.autoTimerId);
      proxyValidationState.autoTimerId = null;
    }

    async function validateSubscriptionProxyValue(proxyValue, options = {}) {
      const trimmedValue = normalizeText(proxyValue);
      clearSubscriptionProxyValidationTimer();
      proxyValidationState.requestToken += 1;
      const requestToken = proxyValidationState.requestToken;
      proxyValidationState.lastValue = trimmedValue;

      if (!trimmedValue) {
        setSubscriptionProxyStatus('empty');
        return { status: 'empty', detail: '尚未填写代理地址。' };
      }

      if (!desktopApi || typeof desktopApi.validateProxy !== 'function') {
        setSubscriptionProxyStatus('empty');
        return { status: 'empty', detail: '当前版本未提供代理检测能力。' };
      }

      setSubscriptionProxyStatus('checking');
      try {
        const result = await desktopApi.validateProxy(trimmedValue, {
          targetUrl:
            options.targetUrl ||
            normalizeText(elements.subscriptionCrawlerUrl && elements.subscriptionCrawlerUrl.value) ||
            'https://www.javbus.com/'
        });

        if (requestToken !== proxyValidationState.requestToken) {
          return result;
        }

        if (result && result.status === 'valid') {
          const usingGlobalProxy = isUsingGlobalSubscriptionProxy(trimmedValue);
          setSubscriptionProxyStatus(usingGlobalProxy ? 'global' : 'valid', usingGlobalProxy ? '' : result.detail);
          return result;
        }

        setSubscriptionProxyStatus('invalid', result && result.detail);
        return result || { status: 'invalid', detail: '代理检测失败。' };
      } catch (error) {
        const message = getErrorMessage(error);
        if (requestToken === proxyValidationState.requestToken) {
          setSubscriptionProxyStatus('invalid', message);
        }
        return { status: 'invalid', detail: message };
      }
    }

    function scheduleSubscriptionProxyValidation(delayMs = 450) {
      if (!elements.subscriptionCrawlerProxy) {
        return;
      }
      clearSubscriptionProxyValidationTimer();
      const trimmedValue = normalizeText(elements.subscriptionCrawlerProxy.value);
      if (!trimmedValue) {
        setSubscriptionProxyStatus('empty');
        return;
      }
      setSubscriptionProxyStatus('checking');
      proxyValidationState.timerId = setTimeout(() => {
        proxyValidationState.timerId = null;
        void validateSubscriptionProxyValue(trimmedValue);
      }, delayMs);
    }

    function scheduleSubscriptionProxyAutoValidation(delayMs = SUBSCRIPTION_PROXY_AUTO_CHECK_INTERVAL_MS) {
      // 审计 H-07：代理自动验证形成无限递归定时器链。
      // 这里限制最大自动验证次数，并在控制器 dispose 时彻底停止。
      if (proxyValidationState.autoValidationStopped || proxyValidationState.autoValidationCount >= 100) {
        return;
      }

      if (!elements.subscriptionCrawlerProxy) {
        return;
      }
      clearSubscriptionProxyAutoValidationTimer();
      proxyValidationState.autoTimerId = setTimeout(async () => {
        proxyValidationState.autoTimerId = null;
        if (proxyValidationState.autoValidationStopped) {
          return;
        }
        proxyValidationState.autoValidationCount += 1;
        const proxyValue = applyDefaultSubscriptionProxy();
        await validateSubscriptionProxyValue(proxyValue);
        scheduleSubscriptionProxyAutoValidation();
      }, Math.max(1000, delayMs));
    }

    function stopSubscriptionProxyAutoValidation() {
      proxyValidationState.autoValidationStopped = true;
      clearSubscriptionProxyAutoValidationTimer();
      clearSubscriptionProxyValidationTimer();
    }

    async function ensureSubscriptionProxyReady() {
      const proxyValue = await loadGlobalSubscriptionProxy();
      if (!proxyValue) {
        setSubscriptionProxyStatus('empty');
        return '';
      }
      const result = await validateSubscriptionProxyValue(proxyValue);
      if (!result || result.status !== 'valid') {
        throw new Error('当前订阅代理检测失败，请修正后再继续联网操作。');
      }
      return proxyValue;
    }

    async function updateSubscriptionAntiBlock() {
      const proxy = await ensureSubscriptionProxyReady();
      const cloudflareEnabled = resolveSubscriptionCloudflareEnabled();
      if (!desktopApi || typeof desktopApi.updateAntiBlock !== 'function') {
        throw new Error('当前版本未提供反屏蔽更新能力。');
      }
      appendLog('info', `[diagnostic] 手动更新反屏蔽 proxy=${proxy || 'none'} cloudflare=${cloudflareEnabled}`);
      const result = await desktopApi.updateAntiBlock({
        proxy,
        base: 'https://www.javbus.com/',
        cloudflare: cloudflareEnabled
      });
      state.antiBlockReady = true;
      const count = Array.isArray(result && result.antiBlockUrls) ? result.antiBlockUrls.length : 0;
      appendLog('info', `反屏蔽已更新：${count} 条规则。`);
      setSubscriptionCrawlerStatus(cloudflareEnabled ? '准备就绪' : '可抓取');
      return result;
    }

    function getCurrentSubscriptionArtifactInput() {
      return normalizeText(elements.subscriptionOutput && elements.subscriptionOutput.value);
    }

    function applySubscriptionArtifactInputValue(artifactInput) {
      const normalized = normalizeText(artifactInput);
      if (elements.subscriptionOutput) {
        elements.subscriptionOutput.value = normalized;
      }
      return normalized;
    }

    function parseRecentCrawlSnapshot(snapshot, index) {
      const actressName = normalizeText(snapshot && snapshot.actressName) || `最近爬取 ${index + 1}`;
      const outputDir = normalizeText(snapshot && snapshot.outputDir);
      const crawlProfilePath = normalizeText(snapshot && snapshot.crawlProfilePath);
      const filmDataPath = normalizeText(snapshot && snapshot.filmDataPath);
      const updatedAt = normalizeText(snapshot && snapshot.updatedAt);
	  const displayName = normalizeText(snapshot && snapshot.displayName);
      const targetCount = normalizeCount(snapshot && (snapshot.completedCount || snapshot.targetCount || snapshot.syncedCount), 0);
      return {
        actressName,
        crawlUrl: normalizeText(snapshot && snapshot.crawlUrl),
        preferredBase: normalizeText(snapshot && snapshot.siteBase),
        outputDir,
        artifactInput: crawlProfilePath || filmDataPath || outputDir,
        filmDataPath,
        crawlProfilePath,
        itemsPerPage: normalizeCount(snapshot && snapshot.itemsPerPage, 30) || 30,
        totalPages: normalizeCount(snapshot && snapshot.totalPages, 1) || 1,
        targetCount,
		label: displayName || (updatedAt
          ? `${actressName} | ${updatedAt.slice(0, 16).replace('T', ' ')} | ${targetCount} 部`
		  : `${actressName} | ${targetCount} 部`)
      };
    }

    function parseRecentCrawlResultEntry(entry, index) {
      const outputDir = normalizeText(entry && entry.outputDir);
      const filmDataPath = normalizeText(entry && entry.filmDataPath);
      const crawlProfilePath = outputDir ? `${outputDir.replace(/[\\/]+$/, '')}\\crawl-profile.json` : '';
      const actressName = normalizeText(entry && entry.title) || `最近爬取 ${index + 1}`;
      const updatedAt = normalizeText(entry && entry.updatedAt);
      return {
        actressName,
        crawlUrl: '',
        outputDir,
        artifactInput: crawlProfilePath || filmDataPath || outputDir,
        filmDataPath,
        updatedAt,
        targetCount: 0,
        label: updatedAt ? `${actressName} | ${updatedAt.slice(0, 16).replace('T', ' ')}` : actressName
      };
    }

    function mergeRecentCrawlOptions(primaryItems, fallbackItems) {
      const merged = [];
      const seenInput = new Set();
      const seenDir = new Set();
      const inputItems = []
        .concat(Array.isArray(primaryItems) ? primaryItems : [])
        .concat(Array.isArray(fallbackItems) ? fallbackItems : []);

      inputItems.forEach((item) => {
        if (!item || typeof item !== 'object') {
          return;
        }
        const artifactInput = normalizeText(item.artifactInput);
        const outputDir = normalizeText(item.outputDir);
        // 按输入文件或输出目录去重，避免同一批抓取在缓存与本地历史中重复出现。
        if (!artifactInput || seenInput.has(artifactInput) || (outputDir && seenDir.has(outputDir))) {
          return;
        }
        seenInput.add(artifactInput);
        if (outputDir) {
          seenDir.add(outputDir);
        }
        merged.push(item);
      });

      return merged;
    }

    async function loadRecentCrawlOptionsFromHistory() {
      attachSnapshotHistoryManager();
      let snapshotItems = [];
      if (desktopApi && typeof desktopApi.listCrawlCacheSnapshots === 'function') {
        try {
          const result = await desktopApi.listCrawlCacheSnapshots();
          const items = Array.isArray(result && result.items) ? result.items : [];
          snapshotItems = items.map((entry, index) => parseRecentCrawlSnapshot(entry, index));
        } catch (error) {
          appendLog('error', `读取内部抓取缓存失败: ${getErrorMessage(error)}`);
        }
      }

      try {
        const rawValue =
          globalScope.localStorage && typeof globalScope.localStorage.getItem === 'function'
            ? globalScope.localStorage.getItem('jav.crawl.result.history.v2')
            : '';
        const parsed = rawValue ? JSON.parse(rawValue) : [];
        const historyItems = (Array.isArray(parsed) ? parsed : []).map((entry, index) =>
          parseRecentCrawlResultEntry(entry, index)
        );
        state.recentCrawlOptions = mergeRecentCrawlOptions(snapshotItems, historyItems);
      } catch {
        state.recentCrawlOptions = mergeRecentCrawlOptions(snapshotItems, []);
      }

      renderRecentCrawlOptions();
    }

    // 快照下拉改为带逐条删除按钮的自定义组件；订阅下拉是索引取值，选中走专用映射。
    function attachSnapshotHistoryManager() {
      const manager = globalScope.desktopSnapshotHistoryManager;
      if (!manager || !elements.subscriptionRecentCrawlSelect) {
        return;
      }
      const select = elements.subscriptionRecentCrawlSelect;
      manager.attach({
        select,
        desktopApi: () => desktopApi,
        reload: loadRecentCrawlOptionsFromHistory,
        log: appendLog,
        errorMessage: getErrorMessage,
        onSelectItem: (item) => {
          const artifactInput = String(
            (item && (item.crawlProfilePath || item.filmDataPath || item.outputDir)) || ''
          ).trim();
          const options = Array.isArray(state.recentCrawlOptions) ? state.recentCrawlOptions : [];
          const matchedIndex = options.findIndex(
            (entry) => String((entry && entry.artifactInput) || '').trim() === artifactInput
          );
          if (matchedIndex >= 0) {
            select.value = String(matchedIndex);
            select.dispatchEvent(new Event('change', { bubbles: true }));
          }
        }
      });
    }

    function renderRecentCrawlOptions() {
      const select = elements.subscriptionRecentCrawlSelect;
      if (!select) {
        return;
      }

      clearChildren(select);
      select.appendChild(createOption('', '请选择最近抓取记录'));

      // 按时间戳降序排序，最新的记录显示在最上面
      const sortedOptions = [...(Array.isArray(state.recentCrawlOptions) ? state.recentCrawlOptions : [])].sort((a, b) => {
        const timeA = new Date(a && a.updatedAt ? a.updatedAt : '').getTime();
        const timeB = new Date(b && b.updatedAt ? b.updatedAt : '').getTime();
        return timeB - timeA;
      });

      sortedOptions.forEach((item, index) => {
        select.appendChild(createOption(String(index), normalizeText(item.label) || `最近爬取 ${index + 1}`));
      });

      state.recentCrawlOptions = sortedOptions;
    }

    function getSelectedRecentCrawlOption() {
      const select = elements.subscriptionRecentCrawlSelect;
      const index = Number(select && select.value ? select.value : -1);
      const options = Array.isArray(state.recentCrawlOptions) ? state.recentCrawlOptions : [];
      if (!Number.isFinite(index) || index < 0 || index >= options.length) {
        return null;
      }
      return options[index];
    }

    function applyRecentCrawlOption(optionItem) {
      if (!optionItem || typeof optionItem !== 'object') {
        return;
      }
      applySubscriptionArtifactInputValue(optionItem.artifactInput);
      if (elements.subscriptionCrawlerUrl) elements.subscriptionCrawlerUrl.value = normalizeText(optionItem.crawlUrl);
      if (elements.subscriptionCrawlerCount) elements.subscriptionCrawlerCount.value = String(normalizeCount(optionItem.targetCount, 0));
      if (elements.subscriptionCrawlerActressName) elements.subscriptionCrawlerActressName.value = normalizeText(optionItem.actressName);
      if (elements.subscriptionItemsPerPage && optionItem.itemsPerPage) {
        elements.subscriptionItemsPerPage.value = String(optionItem.itemsPerPage);
      }
      if (elements.subscriptionTotalPages && optionItem.totalPages) {
        elements.subscriptionTotalPages.value = String(optionItem.totalPages);
      }
      setSummaryMessage(`已载入最近抓取输入：${optionItem.actressName || optionItem.outputDir || '最近记录'}。`);
      appendLog('info', `已载入最近抓取输入：${optionItem.artifactInput || optionItem.outputDir || ''}`);
    }

    function currentSubscriptionList() {
      return sortSubscriptionItems(state.subscriptions);
    }

    function updateSubscriptionState(nextItems) {
      state.subscriptions = Array.isArray(nextItems) ? nextItems.slice() : [];
      if (!state.selectedId || !state.subscriptions.some((item) => item.id === state.selectedId)) {
        const firstVisible = visibleSubscriptionList()[0];
        state.selectedId = firstVisible ? firstVisible.id : '';
      }
      setSummaryCounts();
      renderSubscriptionList();
      renderSubscriptionDetail();
    }

    function syncGlobalActressFilterInput(items) {
      if (!elements.subscriptionGlobalActressFilter) {
        return;
      }
      const values = Array.from(new Set(
        (Array.isArray(items) ? items : []).map((item) => normalizeActressFilterThreshold(item && item.actressCountFilterThreshold))
      ));
      if (values.length === 1) {
        elements.subscriptionGlobalActressFilter.value = String(values[0]);
        elements.subscriptionGlobalActressFilter.placeholder = '0';
        return;
      }
      elements.subscriptionGlobalActressFilter.value = '';
      elements.subscriptionGlobalActressFilter.placeholder = values.length > 1 ? '混合' : '0';
    }

    function findSubscriptionById(id) {
      return (Array.isArray(state.subscriptions) ? state.subscriptions : []).find((item) => item.id === id) || null;
    }

    function assignSubscriptionOrder(items) {
      return (Array.isArray(items) ? items : []).map((item, index) => ({
        ...item,
        sortOrder: index + 1
      }));
    }

    async function persistSubscriptionOrder(items) {
      if (!desktopApi || typeof desktopApi.reorderAvSubscriptions !== 'function') {
        return;
      }
      state.pendingReorderIDs = items.map((item) => normalizeText(item && item.id)).filter(Boolean);
      if (state.reorderRunning) {
        return;
      }
      state.reorderRunning = true;
      try {
        while (state.pendingReorderIDs) {
          const ids = state.pendingReorderIDs;
          state.pendingReorderIDs = null;
          const result = await desktopApi.reorderAvSubscriptions({ ids });
          if (!state.pendingReorderIDs && result && result.subscriptions) {
            updateSubscriptionState(result.subscriptions);
          }
        }
      } catch (error) {
        state.pendingReorderIDs = null;
        appendLog('warn', `订阅排序保存失败：${getErrorMessage(error)}`);
        await loadSubscriptions();
      } finally {
        state.reorderRunning = false;
      }
    }

    function resolveSubscriptionActorOutputDir(item) {
      if (!item || typeof item !== 'object') {
        return '';
      }
      const root = normalizeText(state.defaultOutputDir).replace(/[\\/]+$/, '');
      const actressName = normalizeText(item && item.actressName)
        .replace(/[\\/:*?"<>|]/g, '_')
        .replace(/[. ]+$/, '') || '未命名订阅';
      if (root) {
        return `${root}\\${actressName}`;
      }
      return normalizeText(item && item.lastCrawlOutputDir) || normalizeText(item && item.preferredOutputDir);
    }

    function moveSubscription(item, direction) {
      if (state.listSort !== 'manual') {
        setSummaryMessage('请先将排序切换为“手动顺序”，再调整订阅位置。');
        return;
      }
      const list = currentSubscriptionList();
      const currentIndex = list.findIndex((entry) => entry.id === (item && item.id));
      const nextIndex = currentIndex + Number(direction || 0);
      if (currentIndex < 0 || nextIndex < 0 || nextIndex >= list.length) {
        return;
      }
      const reordered = list.slice();
      const [moved] = reordered.splice(currentIndex, 1);
      reordered.splice(nextIndex, 0, moved);
      state.subscriptions = assignSubscriptionOrder(reordered);
      renderSubscriptionList();
      renderSubscriptionDetail();
      void persistSubscriptionOrder(state.subscriptions);
    }

    function dropSubscription(draggedID, targetItem) {
      if (state.listSort !== 'manual') {
        setSummaryMessage('请先将排序切换为“手动顺序”，再拖拽调整订阅位置。');
        return;
      }
      const sourceID = normalizeText(draggedID);
      const targetID = normalizeText(targetItem && targetItem.id);
      if (!sourceID || !targetID || sourceID === targetID) {
        return;
      }
      const list = currentSubscriptionList();
      const sourceIndex = list.findIndex((item) => item.id === sourceID);
      const targetIndex = list.findIndex((item) => item.id === targetID);
      if (sourceIndex < 0 || targetIndex < 0) {
        return;
      }
      const reordered = list.slice();
      const [moved] = reordered.splice(sourceIndex, 1);
      const insertIndex = reordered.findIndex((item) => item.id === targetID);
      reordered.splice(Math.max(0, insertIndex), 0, moved);
      state.subscriptions = assignSubscriptionOrder(reordered);
      renderSubscriptionList();
      renderSubscriptionDetail();
      void persistSubscriptionOrder(state.subscriptions);
    }

    async function saveActressFilter(item, rawValue, button) {
      if (!item || !item.id || !desktopApi || typeof desktopApi.patchAvSubscription !== 'function') {
        return;
      }
      const parsed = Number.parseInt(String(rawValue || '').trim(), 10);
      const threshold = Number.isFinite(parsed) ? Math.max(0, parsed) : 0;
      if (button) {
        button.disabled = true;
      }
      try {
        const updated = await desktopApi.patchAvSubscription({
          id: item.id,
          actressCountFilterThreshold: threshold
        });
        if (updated && updated.id) {
          updateSubscriptionState(state.subscriptions.map((entry) => entry.id === updated.id ? updated : entry));
          syncGlobalActressFilterInput(state.subscriptions);
          setSummaryMessage(`已保存 ${updated.actressName || '该演员'} 的过滤演员数目：${threshold === 0 ? '关闭' : `${threshold} 位演员以上`}`);
          appendLog('info', `已保存过滤演员数目：${updated.actressName || '未命名订阅'}，阈值 ${threshold}`);
        }
      } catch (error) {
        appendLog('error', `保存过滤演员数目失败：${getErrorMessage(error)}`);
      } finally {
        if (button) {
          button.disabled = false;
        }
      }
    }

    async function applyActressFilterToAllSubscriptions() {
      if (!desktopApi || typeof desktopApi.patchAllAvSubscriptionActressFilter !== 'function') {
        throw new Error('当前版本未提供统一过滤演员数目设置。');
      }
      const current = Array.isArray(state.subscriptions) ? state.subscriptions : [];
      if (current.length === 0) {
        setSummaryMessage('当前没有订阅可应用过滤演员数目。');
        return;
      }
      const rawValue = elements.subscriptionGlobalActressFilter && elements.subscriptionGlobalActressFilter.value;
      if (String(rawValue == null ? '' : rawValue).trim() === '') {
        throw new Error('请填写统一过滤演员数目；填 0 可关闭过滤。');
      }
      const threshold = normalizeActressFilterThreshold(rawValue);
      const result = await desktopApi.patchAllAvSubscriptionActressFilter({
        actressCountFilterThreshold: threshold
      });
      const nextItems = result && Array.isArray(result.subscriptions) ? result.subscriptions : current;
      updateSubscriptionState(nextItems);
      syncGlobalActressFilterInput(nextItems);
      setSummaryMessage(`已将过滤演员数目应用到全部 ${nextItems.length} 条订阅：${threshold === 0 ? '关闭' : `${threshold} 位演员以上`}`);
      appendLog('info', `已统一设置过滤演员数目：${nextItems.length} 条订阅，阈值 ${threshold}`);
    }

    function resolveSubscriptionMediaURLs(item) {
      const values = [];
      if (item && item.avatarUrl) {
        values.push(item.avatarUrl);
      }
      if (Array.isArray(item && item.photoUrls)) {
        values.push(...item.photoUrls);
      }
      return Array.from(new Set(values.map(normalizeText).filter(Boolean)));
    }

    function mediaNeedsHydration(item) {
      if (resolveSubscriptionMediaURLs(item).length > 0) {
        return false;
      }
      const checkedAt = parseTimestamp(item && item.mediaUpdatedAt);
      return checkedAt <= 0 || Date.now() - checkedAt > 7 * 24 * 60 * 60 * 1000;
    }

    async function ensureSubscriptionMedia(item, force = false) {
      if (!item || !item.id || !desktopApi || typeof desktopApi.enrichAvSubscriptionMedia !== 'function') {
        return item;
      }
      if (!force && !mediaNeedsHydration(item)) {
        return item;
      }
      const updated = await desktopApi.enrichAvSubscriptionMedia({
        id: item.id,
        proxy: resolveSubscriptionProxyValue(),
        force
      });
      if (updated && updated.id) {
        state.subscriptions = sortSubscriptionItems(
          state.subscriptions.map((entry) => (entry.id === updated.id ? updated : entry))
        );
        renderSubscriptionList();
        renderSubscriptionDetail();
        return updated;
      }
      return item;
    }

    async function hydrateMissingSubscriptionMedia() {
      if (state.mediaHydrationRunning) {
        return;
      }
      state.mediaHydrationRunning = true;
      try {
        const pendingItems = currentSubscriptionList().filter(mediaNeedsHydration);
        for (const item of pendingItems) {
          try {
            await ensureSubscriptionMedia(item);
          } catch (_) {
            // Actor photos are optional enrichment and must never block the list.
          }
        }
      } finally {
        state.mediaHydrationRunning = false;
      }
    }

    async function openSubscriptionMediaGallery(item) {
      let current = item;
      if (resolveSubscriptionMediaURLs(current).length === 0) {
        try {
          current = await ensureSubscriptionMedia(current, true);
        } catch (_) {}
      }
      const mediaURLs = resolveSubscriptionMediaURLs(current);
      if (mediaURLs.length === 0) {
        await showSubscriptionAlert(`暂未获取到 ${current && current.actressName ? current.actressName : '该演员'} 的写真或宣传照。`);
        return;
      }

      const existing = getElementById('subscription-media-viewer');
      if (existing) {
        existing.remove();
      }
      const viewer = createElement('div', {
        id: 'subscription-media-viewer',
        className: 'subscription-media-viewer',
        attributes: { role: 'dialog', 'aria-modal': 'true' }
      });

      const title = createElement('strong', { textContent: current.actressName || '演员写真' });
      const closeButton = createElement('button', {
        type: 'button',
        className: 'subscription-media-close',
        textContent: '×',
        title: '关闭',
        ariaLabel: '关闭写真查看器'
      });
      const header = createElement('div', {
        className: 'subscription-media-header',
        children: [title, closeButton]
      });

      const mainImage = createElement('img', { alt: `${current.actressName || '演员'}写真` });
      const stage = createElement('div', {
        className: 'subscription-media-stage',
        children: [mainImage]
      });
      const thumbnails = createElement('div', { className: 'subscription-media-thumbnails' });
      let activeIndex = 0;
      const showImage = (index) => {
        activeIndex = Math.max(0, Math.min(mediaURLs.length - 1, index));
        mainImage.src = mediaURLs[activeIndex];
        selectAll(thumbnails, 'button').forEach((button, buttonIndex) => {
          button.classList.toggle('is-active', buttonIndex === activeIndex);
        });
      };
      mediaURLs.forEach((mediaURL, index) => {
        const image = createElement('img', {
          src: mediaURL,
          alt: `${current.actressName || '演员'}照片 ${index + 1}`
        });
        const button = createElement('button', {
          type: 'button',
          className: 'subscription-media-thumbnail',
          children: [image]
        });
        button.addEventListener('click', () => showImage(index));
        thumbnails.appendChild(button);
      });

      const panel = createElement('div', {
        className: 'subscription-media-panel',
        children: [header, stage, thumbnails]
      });
      viewer.appendChild(panel);

      const close = () => viewer.remove();
      closeButton.addEventListener('click', close);
      viewer.addEventListener('click', (event) => {
        if (event.target === viewer) close();
      });
      viewer.addEventListener('keydown', (event) => {
        if (event.key === 'Escape') close();
        if (event.key === 'ArrowLeft') showImage(activeIndex - 1);
        if (event.key === 'ArrowRight') showImage(activeIndex + 1);
      });
      document.body.appendChild(viewer);
      viewer.tabIndex = -1;
      viewer.focus();
      showImage(0);
    }

    function renderSubscriptionList() {
      if (!elements.subscriptionList) {
        return;
      }
      const list = visibleSubscriptionList();
      subscriptionListView.renderSubscriptionList(elements.subscriptionList, list, {
        emptyMessage: state.subscriptions.length > 0 ? '没有符合当前搜索或筛选条件的订阅。' : '当前还没有任何 AV 订阅。',
        showOrderControls: state.listSort === 'manual',
        onSelect: (item) => {
          state.selectedId = item && item.id ? item.id : '';
          renderSubscriptionList();
          renderSubscriptionDetail();
        },
        onOneClickUpdate: (item) => void runOneClickSubscriptionUpdate(item),
        onFillCrawl: prepareSubscriptionCrawlerFromItem,
        onOpenMedia: (item) => void openSubscriptionMediaGallery(item),
        onOpenUrl: (item) => {
          const url = normalizeText(item && item.crawlUrl);
          if (!url || !desktopApi || typeof desktopApi.openExternal !== 'function') {
            return;
          }
          void desktopApi.openExternal(url);
        },
        onMove: moveSubscription,
        onDrop: dropSubscription,
        onSaveActressFilter: saveActressFilter,
        crawlActive: Boolean(getActiveCrawlSession()),
        onBindDelete: (button, item) => {
          bindAsyncClick(button, async () => {
            const confirmed = confirmAction(`确认删除订阅“${item.actressName || '未命名订阅'}”吗？`);
            if (!confirmed) {
              return;
            }
            const result = await desktopApi.removeAvSubscription(item.id);
            updateSubscriptionState(result && result.subscriptions);
            setSummaryMessage(`已删除订阅：${item.actressName || '未命名订阅'}`);
            appendLog('info', `已删除订阅：${item.actressName || '未命名订阅'}`);
          });
        },
        onBindSync: (button, item) => {
          bindAsyncClick(button, async () => {
            const updated = await desktopApi.markAvSubscriptionSynced(item.id);
            const nextList = state.subscriptions.map((entry) => (entry.id === updated.id ? updated : entry));
            updateSubscriptionState(nextList);
            setSummaryMessage(`已将 ${updated.actressName} 标记为已同步。`);
            appendLog('info', `订阅已同步：${updated.actressName}`);
          });
        },
        onBindOpen: (button, item) => {
          bindAsyncClick(button, async () => {
            if (!item.crawlUrl) {
              return;
            }
            await desktopApi.openExternal(item.crawlUrl);
          });
        },
        onEdit: openSubscriptionEditDialog
      });
    }

    function renderSubscriptionDetail() {
      if (!elements.subscriptionDetailCard || !elements.subscriptionDetailEmpty) {
        return;
      }

      const current = findSubscriptionById(state.selectedId);
      if (!current) {
        elements.subscriptionDetailEmpty.classList.remove('hidden');
        elements.subscriptionDetailCard.classList.add('hidden');
        elements.subscriptionDetailCard.innerHTML = '';
        return;
      }

      elements.subscriptionDetailEmpty.classList.add('hidden');
      elements.subscriptionDetailCard.classList.remove('hidden');

      const baselineCodes = normalizeCodes(current.baselineCodes);
      const pendingCodes = normalizeCodes(current.pendingCodes);
      const baselineCount = resolveSubscriptionBaselineCount(current);
      const currentTotal = resolveSubscriptionCurrentTotal(current);
      const currentCount = resolveSubscriptionCurrentCount(current);
      const sourceTypeText =
        current.sourceType === 'metadata-auto'
          ? '刮削自动订阅'
          : current.sourceType === 'manual'
            ? '手动建档'
            : 'JAV 爬虫导入';
      const baselineMismatch = currentCount > baselineCount + pendingCodes.length;
      const mismatchWarning = baselineMismatch
        ? `<div class="detail-row detail-warning"><span>基数异常</span><div>用户基线 ${baselineCount} 部，实际观测 ${currentCount} 部。建议点击下方"修改订阅"修正基线数据。</div></div>`
        : '';
      const mediaURLs = resolveSubscriptionMediaURLs(current);
      const avatarPreview = mediaURLs.length > 0
        ? `<button type="button" class="subscription-detail-avatar" id="subscription-detail-media-btn" title="查看写真与宣传照"><img src="${escapeHtml(mediaURLs[0])}" alt="${escapeHtml(current.actressName)}头像" /></button>`
        : `<button type="button" class="subscription-detail-avatar is-empty" id="subscription-detail-media-btn" title="获取写真与宣传照">${escapeHtml(normalizeText(current.actressName).slice(0, 1) || '?')}</button>`;
      const prepared = state.preparedDraft && state.preparedDraft.id === current.id ? state.preparedDraft : null;
      const preparedRows = prepared
        ? `
          <div class="detail-row detail-prepared"><span>预载状态</span><strong>已填充，可开始更新</strong></div>
          <div class="detail-row"><span>预载影片</span><div>${escapeHtml(String(normalizeCount(prepared.targetCount, 0)))} 部 · ${renderCodePreview(prepared.pendingCodes, '等待检测番号')}</div></div>
          <div class="detail-row"><span>预载地址</span><div>${escapeHtml(prepared.crawlUrl || '未设置')}</div></div>
          <div class="detail-row"><span>保存位置</span><div>${escapeHtml(prepared.outputDir || '软件内部 AV订阅 路径')}</div></div>
        `
        : '<div class="detail-row"><span>预载状态</span><div>尚未填充爬取信息</div></div>';

      elements.subscriptionDetailCard.innerHTML = `
        <div class="subscription-detail-profile">
          ${avatarPreview}
          <div><span>当前演员</span><strong>${escapeHtml(current.actressName)}</strong></div>
        </div>
        <div class="detail-grid">
          <div class="detail-row"><span>女优名称</span><strong>${escapeHtml(current.actressName)}</strong></div>
          <div class="detail-row"><span>抓取地址</span><div>${escapeHtml(current.crawlUrl)}</div></div>
          <div class="detail-row"><span>来源类型</span><strong>${escapeHtml(sourceTypeText)}</strong></div>
          <div class="detail-row"><span>输出目录</span><div>${escapeHtml(resolveSubscriptionActorOutputDir(current) || '未设置')}</div></div>
          <div class="detail-row"><span>基数</span><strong>${escapeHtml(String(baselineCount))}</strong></div>
          <div class="detail-row"><span>当前总量</span><strong>${escapeHtml(String(currentTotal))}</strong></div>
          <div class="detail-row"><span>当前总数</span><strong>${escapeHtml(String(currentCount))}</strong></div>
          <div class="detail-row"><span>待更新番号</span><div>${renderCodePreview(pendingCodes, '暂无待更新番号')}</div></div>
          <div class="detail-row"><span>基线预览</span><div>${renderCodePreview(baselineCodes, '无基线番号')}</div></div>
          ${preparedRows}
          ${mismatchWarning}
        </div>
        <div class="detail-actions">
          <button type="button" class="secondary-button" id="subscription-detail-edit-btn">修改订阅</button>
        </div>
      `;

      const editBtn = select(elements.subscriptionDetailCard, '#subscription-detail-edit-btn');
      if (editBtn) {
        editBtn.addEventListener('click', () => openSubscriptionEditDialog(current));
      }
      const mediaBtn = select(elements.subscriptionDetailCard, '#subscription-detail-media-btn');
      if (mediaBtn) {
        mediaBtn.addEventListener('click', () => void openSubscriptionMediaGallery(current));
      }
    }

    function escapeHtml(value) {
      return String(value || '')
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');
    }

    function openSubscriptionEditDialog(item) {
      if (!item || !item.id) {
        return;
      }
      // 审计 H-10：将连续三次阻塞式 prompt() 改为非阻塞模态表单。
      openSubscriptionEditModal(item);
    }

    function openSubscriptionEditModal(item) {
      const existing = getElementById('subscription-edit-modal-overlay');
      if (existing) {
        existing.remove();
      }

      const overlay = createElement('div', {
        id: 'subscription-edit-modal-overlay',
        cssText:
          'position:fixed;inset:0;background:rgba(0,0,0,0.6);display:flex;align-items:center;justify-content:center;z-index:10000;'
      });

      function createField(labelText, inputValue, inputType = 'text') {
        const label = createElement('label', {
          textContent: labelText,
          cssText: 'display:block;margin-bottom:6px;font-size:14px;color:#94a3b8;'
        });
        const input = createElement('input', {
          type: inputType,
          value: String(inputValue || ''),
          cssText:
            'width:100%;padding:10px 12px;background:#0f172a;border:1px solid #334155;border-radius:8px;color:#f8fafc;font-size:14px;box-sizing:border-box;'
        });
        const row = createElement('div', {
          cssText: 'margin-bottom:14px;',
          children: [label, input]
        });
        return { row, input };
      }

      const title = createElement('h3', {
        textContent: '修改订阅',
        cssText: 'margin:0 0 16px 0;font-size:18px;font-weight:600;'
      });

      const nameField = createField('女优名称', item.actressName || '');
      const urlField = createField('抓取地址', item.crawlUrl || '');
      const pageField = createField('每页数量', item.itemsPerPage || 30, 'number');
      pageField.input.min = '1';

      const cancelButton = createElement('button', {
        type: 'button',
        textContent: '取消',
        cssText:
          'padding:8px 16px;background:#334155;color:#f8fafc;border:none;border-radius:8px;cursor:pointer;font-size:14px;'
      });
      const confirmButton = createElement('button', {
        type: 'button',
        textContent: '确认',
        cssText:
          'padding:8px 16px;background:#3b82f6;color:#fff;border:none;border-radius:8px;cursor:pointer;font-size:14px;'
      });
      const buttonRow = createElement('div', {
        cssText: 'display:flex;justify-content:flex-end;gap:12px;margin-top:20px;',
        children: [cancelButton, confirmButton]
      });

      const dialog = createElement('div', {
        cssText:
          'background:#1e293b;color:#f8fafc;padding:24px;border-radius:12px;min-width:320px;max-width:90vw;box-shadow:0 20px 50px rgba(0,0,0,0.5);',
        children: [title, nameField.row, urlField.row, pageField.row, buttonRow]
      });
      overlay.appendChild(dialog);

      function closeModal() {
        overlay.remove();
      }

      async function submitEdit() {
        const actressName = nameField.input.value;
        const crawlUrl = urlField.input.value;
        const itemsPerPage = Math.max(1, parseInt(pageField.input.value, 10) || 30);
        closeModal();
        try {
          const updated = await desktopApi.patchAvSubscription({
            id: item.id,
            actressName: actressName.trim(),
            crawlUrl: crawlUrl.trim(),
            itemsPerPage
          });
          if (updated) {
            const nextList = state.subscriptions.map((entry) =>
              entry.id === updated.id ? updated : entry
            );
            updateSubscriptionState(nextList);
            renderSubscriptionDetail();
            appendLog('info', `已修改订阅：${updated.actressName}`);
          }
        } catch (error) {
          appendLog('error', `修改订阅失败：${getErrorMessage(error)}`);
        }
      }

      cancelButton.addEventListener('click', closeModal);
      confirmButton.addEventListener('click', submitEdit);
      overlay.addEventListener('click', (event) => {
        if (event.target === overlay) {
          closeModal();
        }
      });
      overlay.addEventListener('keydown', (event) => {
        if (event.key === 'Escape') {
          closeModal();
        }
      });

      document.body.appendChild(overlay);
      nameField.input.focus();
    }

    function confirmAction(message) {
      if (typeof globalScope.confirm !== 'function') {
        return true;
      }
      return globalScope.confirm(String(message || '').trim());
    }

    async function refreshOneSubscription(id) {
      const items = currentSubscriptionList();
      const target = items.find((item) => item.id === id);
      if (!target) {
        throw new Error(`subscription not found: ${id}`);
      }

      await ensureSubscriptionProxyReady();
      appendLog('info', `开始检测更新：${target.actressName || '未命名订阅'}。`);
      updateSubscriptionState(state.subscriptions.map((item) => item.id === target.id ? { ...item, status: 'running', lastError: '' } : item));
      const payload = buildSubscriptionRuntimePayload({ id: target.id });
      appendLog(
        'info',
        `[diagnostic] 检测更新参数 proxy=${payload.proxy || 'none'} cloudflare=${payload.cloudflare} parallel=${payload.parallel} delay=${payload.delay} timeout=${payload.timeout}`
      );
      let result;
      try {
        result = await desktopApi.refreshAvSubscription(payload);
      } catch (error) {
        updateSubscriptionState(state.subscriptions.map((item) => item.id === target.id ? { ...item, status: 'error', lastError: getErrorMessage(error) } : item));
        throw error;
      }

      const nextItem = result && result.subscription ? result.subscription : target;
      const nextList = items.map((entry) => (entry.id === nextItem.id ? nextItem : entry));
      updateSubscriptionState(nextList);
      if (nextItem.pendingCount > 0) {
        setSummaryMessage(`${nextItem.actressName} 检测到 ${nextItem.pendingCount} 部待更新影片。`);
        appendLog('info', `${nextItem.actressName} 检测更新完成：新增 ${nextItem.pendingCount} 部，扫描 ${normalizeCount(result && result.scannedPages, 0)} 页。`);
      } else {
        setSummaryMessage(`${nextItem.actressName} 未发现更新。`);
        appendLog('info', `${nextItem.actressName} 未发现更新，扫描 ${normalizeCount(result && result.scannedPages, 0)} 页。`);
      }
      return nextItem;
    }

    async function runOneClickSubscriptionUpdate(item) {
      if (!item || !item.id) {
        return;
      }
      try {
        state.selectedId = item.id;
        renderSubscriptionList();
        renderSubscriptionDetail();
        setSubscriptionCrawlerStatus('正在检测更新...');
        setSummaryMessage(`正在一键更新 ${item.actressName || '该订阅'}...`);
        const refreshed = await refreshOneSubscription(item.id);
        prepareSubscriptionCrawlerFromItem(refreshed);
        if (normalizeCount(refreshed && refreshed.pendingCount, 0) <= 0) {
          setSubscriptionCrawlerStatus('已是最新');
          await showSubscriptionCompletionAlert(
            `演员：${refreshed.actressName || item.actressName || '订阅'}\n本次检测没有发现新增影片，无需执行刮削。`,
            { title: '更新刮削完成' }
          );
          return;
        }
        await startIndependentCrawl(refreshed);
      } catch (error) {
        const message = getErrorMessage(error);
        appendLog('error', `一键更新失败：${message}`);
        setSummaryMessage(`一键更新失败：${message}`);
        setSubscriptionCrawlerStatus('更新失败');
      }
    }

    async function refreshAllSubscriptions() {
      const items = currentSubscriptionList();
      if (items.length === 0) {
        setSummaryMessage('当前没有订阅可检测。');
        appendLog('info', '检测全部订阅已跳过：当前无订阅。');
        return;
      }

      await ensureSubscriptionProxyReady();
      appendLog('info', `开始批量检测 ${items.length} 条订阅。`);
      updateSubscriptionState(state.subscriptions.map((item) => ({ ...item, status: 'running', lastError: '' })));
      const payload = buildSubscriptionRuntimePayload();
      appendLog(
        'info',
        `[diagnostic] 批量检测参数 proxy=${payload.proxy || 'none'} cloudflare=${payload.cloudflare} parallel=${payload.parallel} delay=${payload.delay} timeout=${payload.timeout}`
      );
      let result;
      try {
        result = await desktopApi.refreshAvSubscriptions(payload);
      } catch (error) {
        updateSubscriptionState(state.subscriptions.map((item) => ({ ...item, status: 'error', lastError: getErrorMessage(error) })));
        throw error;
      }

      if (result && Array.isArray(result.subscriptions)) {
        updateSubscriptionState(result.subscriptions);
      }
      const checkedCount = normalizeCount(result && result.checkedCount, items.length);
      const updatedCount = normalizeCount(result && result.updatedCount, 0);
      const detectedCount = normalizeCount(result && result.detectedCount, 0);
      const failedCount = normalizeCount(result && result.failedCount, 0);
      const totalPending = normalizeCount(result && result.totalPending, 0);
      setRefreshSummary(`本次检查 ${checkedCount} 位，新发现 ${detectedCount} 位，仍待抓取 ${totalPending} 部${failedCount > 0 ? `，失败 ${failedCount} 位` : ''}。`);
      setSummaryMessage(detectedCount > 0
        ? `批量检测完成：本次新发现 ${detectedCount} 条订阅有更新。`
        : updatedCount > 0
          ? `批量检测完成：没有新发现，仍有 ${updatedCount} 条订阅待抓取。`
          : '批量检测完成：未发现新增。');
    }

    function prepareSubscriptionCrawlerFromItem(item) {
      if (!item || typeof item !== 'object') {
        return;
      }
      const pendingCount = normalizeCount(item.pendingCount, 0);
      const totalPages = Math.max(1, Math.ceil(Math.max(1, pendingCount) / 30));
      const pendingCodes = normalizeCodes(item.pendingCodes);
      applySubscriptionCrawlerDraft({
        crawlUrl: item.crawlUrl,
        targetCount: pendingCount > 0 ? pendingCount : 1,
        actressName: item.actressName
      });
      state.selectedId = normalizeText(item.id);
      state.preparedDraft = {
        id: normalizeText(item.id),
        actressName: normalizeText(item.actressName),
        crawlUrl: normalizeText(item.crawlUrl),
        targetCount: pendingCount > 0 ? pendingCount : 1,
        pendingCodes,
        outputDir: resolveSubscriptionActorOutputDir(item)
      };
      if (elements.subscriptionList) {
        elements.subscriptionList.dataset.currentPreparedId = normalizeText(item.id);
      }
      renderSubscriptionList();
      renderSubscriptionDetail();

      if (pendingCount > 0) {
        setSubscriptionCrawlerStatus(`待抓取 ${pendingCount} 部`);
        setSummaryMessage(`已准备 ${item.actressName || '该订阅'} 的抓取草稿：待更新 ${pendingCount} 部，实际按 ${totalPages} 页抓满后过滤。`);
        appendLog('info', `已准备抓取：${item.actressName || '未命名订阅'}，地址 ${item.crawlUrl || '未填写'}，待更新 ${pendingCount} 部。`);
        appendLog('info', `待更新番号：${pendingCodes.length > 0 ? pendingCodes.join('、') : '未解析到明确番号，将保留全部输出'}`);
        appendLog('info', `实际抓取范围：第 1-${totalPages} 页全部影片；抓取完成后只保留待更新番号对应的磁力。`);
        appendLog(
          'info',
          `[diagnostic] 填充爬取信息 pending=${pendingCount} pageCapacity=${totalPages * 30} pages=${totalPages} itemsPerPage=30 parallel=${resolveSubscriptionCrawlerParallel()} delay=${resolveSubscriptionCrawlerDelay()} timeout=${resolveSubscriptionCrawlerTimeout()} output=软件目录\\AV订阅`
        );
        return;
      }

      setSubscriptionCrawlerStatus('未检测到新增');
      setSummaryMessage(`${item.actressName || '该订阅'} 当前没有检测到待更新影片。`);
      appendLog('warn', `${item.actressName || '未命名订阅'} 当前待更新数量为 0。`);
    }

    function applySubscriptionCrawlerDraft(draft = {}) {
      if (!draft || typeof draft !== 'object') {
        return;
      }
      if (elements.subscriptionCrawlerUrl) {
        elements.subscriptionCrawlerUrl.value = normalizeText(draft.crawlUrl);
      }
      if (elements.subscriptionCrawlerCount) {
        const targetCount = draft.targetCount;
        elements.subscriptionCrawlerCount.value =
          targetCount == null || targetCount === '' ? '' : String(Math.max(0, Number(targetCount || 0)));
      }
      if (elements.subscriptionCrawlerActressName) {
        elements.subscriptionCrawlerActressName.value = normalizeText(draft.actressName);
      }
    }

    async function showSubscriptionAlert(message) {
      if (!desktopApi || typeof desktopApi.showAlert !== 'function') {
        if (typeof globalScope.alert === 'function') {
          globalScope.alert(message);
        }
        return;
      }
      await desktopApi.showAlert({
        type: 'warning',
        title: 'AV 订阅',
        message
      });
    }

    async function showSubscriptionCompletionAlert(message, options = {}) {
      const type = options && options.type === 'warning' ? 'warning' : 'success';
      const title = normalizeText(options && options.title) || '更新刮削完成';
      if (!desktopApi || typeof desktopApi.showAlert !== 'function') {
        if (typeof globalScope.alert === 'function') {
          globalScope.alert(`${title}\n${message}`);
        }
        return;
      }
      try {
        await desktopApi.showAlert({
          type,
          title,
          message,
          confirmText: '知道了'
        });
      } catch (_) {
        // 完成提示失败不能影响订阅结果回收和状态清理。
      }
    }

    async function prepareSubscriptionCrawlEnvironment() {
      const proxy = await ensureSubscriptionProxyReady();
      const cloudflareEnabled = resolveSubscriptionCloudflareEnabled();
      const outputDir = await resolveSubscriptionDefaultOutputDir();

      setSubscriptionCrawlerStatus('准备环境...');
      appendLog('info', `[diagnostic] AV订阅准备环境 cloudflare=${cloudflareEnabled} proxy=${proxy || 'none'} output=${outputDir || '未解析'}`);
      appendLog('info', `Cloudflare 兼容：${cloudflareEnabled ? '已启用（桥接 JAV 爬虫配置）' : '未启用'}`);

      try {
        if (desktopApi && typeof desktopApi.updateAntiBlock === 'function') {
          await desktopApi.updateAntiBlock({
            proxy,
            base: 'https://www.javbus.com/',
            cloudflare: cloudflareEnabled
          });
          state.antiBlockReady = true;
        }
      } catch (error) {
        state.antiBlockReady = false;
        appendLog('warn', `反屏蔽更新失败：${getErrorMessage(error)}`);
      }

      appendLog('info', `反屏蔽：${state.antiBlockReady ? '已启用' : '初始化失败'}`);
      setSubscriptionCrawlerStatus(cloudflareEnabled && state.antiBlockReady ? '准备就绪' : state.antiBlockReady ? '可抓取' : '准备未完成');

      return {
        proxy,
        cloudflareEnabled,
        outputDir
      };
    }

    async function startIndependentCrawl(item, customCount) {
      const environment = await prepareSubscriptionCrawlEnvironment();
      const outputDir = normalizeText(environment && environment.outputDir);
      if (!outputDir) {
        await showSubscriptionAlert('未能解析默认输出目录，请重启软件后重试。');
        return;
      }
      if (!item || !item.crawlUrl) {
        await showSubscriptionAlert('订阅缺少抓取地址。');
        return;
      }

      const pendingCount = normalizeCount(item.pendingCount, 0);
      const targetCount = customCount > 0 ? customCount : (pendingCount > 0 ? pendingCount : 1);
      const totalPages = Math.max(1, Math.ceil(Math.max(1, targetCount) / 30));
      const crawlLimit = totalPages * 30;
      const pendingCodes = normalizeCodes(item.pendingCodes);
      const proxy = environment.proxy;
      const cloudflareEnabled = Boolean(environment && environment.cloudflareEnabled);
      const parallel = resolveSubscriptionCrawlerParallel();
      const delay = resolveSubscriptionCrawlerDelay();
      const timeout = resolveSubscriptionCrawlerTimeout();

      appendLog('info', `开始订阅更新抓取：${item.actressName || '未命名'}，待更新 ${targetCount} 部。`);
      appendLog('info', `待更新番号：${pendingCodes.length > 0 ? pendingCodes.join('、') : '未解析到明确番号，将保留全部输出'}`);
      appendLog('info', `实际抓取范围：第 1-${totalPages} 页全部影片（页面容量约 ${crawlLimit} 部，不用数量上限判短缺）；抓取完成后回收过滤。`);
      appendLog('info', `[diagnostic] AV订阅抓取启动 cloudflare=${cloudflareEnabled} antiBlock=${state.antiBlockReady ? 'enabled' : 'failed'} proxy=${proxy || 'none'} output=${outputDir} pending=${targetCount} pageCapacity=${crawlLimit} pages=${totalPages} itemsPerPage=30 parallel=${parallel} delay=${delay} timeout=${timeout}`);
      appendLog('info', `Cloudflare 兼容：${cloudflareEnabled ? '已启用（桥接 JAV 爬虫配置）' : '未启用'}`);
      appendLog('info', `反屏蔽：${state.antiBlockReady ? '已启用' : '初始化失败'}`);
      setSubscriptionCrawlerStatus('正在抓取...');
      applySubscriptionStagePanel({
        status: 'starting',
        phaseTitle: '准备订阅抓取',
        phaseDescription: '正在初始化订阅抓取运行环境。',
        message: `即将抓取 ${targetCount} 部待更新影片。`,
        outputDir,
        stats: { pageIndex: 0, queued: 0, attempted: 0, completed: 0 }
      });

      try {
        // Publish the session before starting the shared crawl feed so the
        // first stage event is routed to the subscription workspace.
        setActiveCrawlSession({
          subscriptionId: normalizeText(item.id),
          actressName: normalizeText(item.actressName),
          targetCodes: normalizeCodes(item.pendingCodes),
          outputDir,
          baseOutputDir: outputDir,
          targetCount,
          crawlLimit
        });
        const startResult = await desktopApi.startSubscriptionCrawl(buildSubscriptionRuntimePayload({
          subscriptionId: item.id,
          outputDir,
          targetCount,
          targetCodes: Array.isArray(item.pendingCodes) ? item.pendingCodes : [],
          proxy
        }));
        if (state.activeCrawlSession) {
          state.activeCrawlSession.outputDir =
            normalizeText(startResult && startResult.currentTaskOutputDir) ||
            normalizeText(startResult && startResult.outputDir) ||
            outputDir;
          state.activeCrawlSession.baseOutputDir = normalizeText(startResult && startResult.baseOutputDir) || outputDir;
          state.activeCrawlSession.crawlLimit = normalizeCount(startResult && startResult.crawlLimit, crawlLimit) || crawlLimit;
          publishActiveSubscriptionCrawlSession(state.activeCrawlSession);
        }
      } catch (error) {
        clearActiveCrawlSession();
        appendLog('error', `启动抓取失败: ${getErrorMessage(error)}`);
        setSubscriptionCrawlerStatus('启动失败');
      }
    }

    function openBatchCrawlDialog() {
      const pendingItems = currentSubscriptionList().filter((item) => normalizeCount(item.pendingCount, 0) > 0);
      if (pendingItems.length === 0) {
        void showSubscriptionAlert('\u5f53\u524d\u6ca1\u6709\u5f85\u5904\u7406\u7684\u8ba2\u9605\u3002');
        return;
      }

      if (elements.subscriptionBatchConcurrency) {
        elements.subscriptionBatchConcurrency.value = '2';
      }
      if (elements.subscriptionBatchDialog && typeof elements.subscriptionBatchDialog.showModal === 'function') {
        elements.subscriptionBatchDialog.showModal();
        return;
      }
      void startBatchCrawl();
    }

    function closeBatchCrawlDialog() {
      if (elements.subscriptionBatchDialog && typeof elements.subscriptionBatchDialog.close === 'function') {
        elements.subscriptionBatchDialog.close();
      }
    }

    async function startBatchCrawl() {
      const pendingItems = currentSubscriptionList().filter((item) => normalizeCount(item.pendingCount, 0) > 0);
      if (pendingItems.length === 0) {
        await showSubscriptionAlert('\u5f53\u524d\u6ca1\u6709\u5f85\u5904\u7406\u7684\u8ba2\u9605\u3002');
        return;
      }

      const requestedConcurrency = toSafeInteger(
        elements.subscriptionBatchConcurrency && elements.subscriptionBatchConcurrency.value,
        2
      );
      const actorConcurrency = Math.max(1, Math.min(3, requestedConcurrency || 2));
      closeBatchCrawlDialog();

      const environment = await prepareSubscriptionCrawlEnvironment();
      const proxy = environment.proxy;
      const cloudflareEnabled = Boolean(environment && environment.cloudflareEnabled);
      const outputDir = normalizeText(environment && environment.outputDir);
      const parallel = resolveSubscriptionCrawlerParallel();
      const delay = resolveSubscriptionCrawlerDelay();
      const timeout = resolveSubscriptionCrawlerTimeout();
      appendLog('info', `\u5f00\u59cb\u4e00\u952e\u5168\u90e8\u66f4\u65b0\uff1a${pendingItems.length} \u4f4d\u6f14\u5458\uff0c\u540c\u65f6\u66f4\u65b0 ${actorConcurrency} \u4f4d\u3002`);
      appendLog('info', `[diagnostic] AV\u8ba2\u9605\u6279\u6b21\u66f4\u65b0 cloudflare=${cloudflareEnabled} antiBlock=${state.antiBlockReady ? 'enabled' : 'failed'} proxy=${proxy || 'none'} output=${outputDir} actorConcurrency=${actorConcurrency} parallel=${parallel} delay=${delay} timeout=${timeout}`);
      appendLog('info', `Cloudflare 兼容：${cloudflareEnabled ? '已启用（桥接 JAV 爬虫配置）' : '未启用'}`);
      appendLog('info', `反屏蔽：${state.antiBlockReady ? '已启用' : '初始化失败'}`);
      setSubscriptionCrawlerStatus('\u4e00\u952e\u5168\u90e8\u66f4\u65b0\u4e2d...');

      state.activeBatchCrawl = {
        expectedTotal: pendingItems.length,
        actorConcurrency,
        outputDir,
        startedAt: Date.now(),
        completionNotified: false
      };
      publishActiveSubscriptionCrawlSession(getBridgeCrawlSession());
      applySubscriptionStagePanel({
        status: 'starting',
        phaseTitle: '准备批量更新',
        phaseDescription: '正在初始化订阅批量抓取任务。',
        message: `即将处理 ${pendingItems.length} 位演员。`,
        outputDir,
        phaseIndex: 0,
        phaseTotal: pendingItems.length,
        stats: { pageIndex: 0, queued: 0, attempted: 0, completed: 0 }
      });

      try {
        await desktopApi.startSubscriptionBatchCrawl(buildSubscriptionRuntimePayload({
          actorConcurrency,
          proxy
        }));
      } catch (error) {
        state.activeBatchCrawl = null;
        publishActiveSubscriptionCrawlSession(null);
        appendLog('error', `\u4e00\u952e\u5168\u90e8\u66f4\u65b0\u542f\u52a8\u5931\u8d25: ${getErrorMessage(error)}`);
        setSubscriptionCrawlerStatus('\u542f\u52a8\u5931\u8d25');
      }
    }

    async function stopIndependentCrawl() {
      try {
        await desktopApi.stopSubscriptionCrawl();
        clearActiveCrawlSession();
        appendLog('info', '已请求停止抓取。');
        setSubscriptionCrawlerStatus('正在停止...');
      } catch (error) {
        appendLog('error', `停止失败: ${getErrorMessage(error)}`);
      }
    }

    function findPreparedSubscriptionItem() {
      const selected = findSubscriptionById(state.selectedId);
      if (selected) {
        return selected;
      }

      const crawlUrl = normalizeText(elements.subscriptionCrawlerUrl && elements.subscriptionCrawlerUrl.value);
      const actressName = normalizeText(elements.subscriptionCrawlerActressName && elements.subscriptionCrawlerActressName.value);
      return currentSubscriptionList().find((item) => {
        return normalizeText(item.crawlUrl) === crawlUrl || normalizeText(item.actressName) === actressName;
      }) || null;
    }

    async function startPreparedSubscriptionCrawl() {
      const preparedItem = findPreparedSubscriptionItem();
      if (!preparedItem) {
        throw new Error('请先从订阅列表选择一位女优，或先点击“填充爬取信息”。');
      }

      const manualCount = normalizeCount(elements.subscriptionCrawlerCount && elements.subscriptionCrawlerCount.value, 0);
      await startIndependentCrawl(preparedItem, manualCount > 0 ? manualCount : undefined);
    }

    async function openSubscriptionMagnetFile() {
      const current = findSubscriptionById(state.selectedId);
      const activeSession = getActiveCrawlSession();
      const outputDir = resolveSubscriptionActorOutputDir(current) ||
        normalizeText(activeSession && activeSession.outputDir) ||
        normalizeText(state.preparedDraft && state.preparedDraft.outputDir) ||
        normalizeText(current && current.preferredOutputDir);
      if (!outputDir) {
        throw new Error('当前订阅还没有可打开的磁力文件，请先完成一次更新。');
      }
      await desktopApi.openMagnetFile(outputDir);
    }

    async function openSubscriptionFolder() {
      const outputDir = normalizeText(state.defaultOutputDir) || await resolveSubscriptionDefaultOutputDir();
      if (!outputDir) {
        throw new Error('未能解析 AV订阅 目录，请重启软件后重试。');
      }
      state.defaultOutputDir = outputDir;
      await desktopApi.openOutputDir(outputDir);
    }

    async function finalizeActiveSubscriptionCrawl(statePayload) {
      const activeSession = getActiveCrawlSession();
      if (!activeSession || !activeSession.subscriptionId) {
        return;
      }
      // 主爬虫可能同时通过多个事件通道发出终态；同一 session 只允许回收一次。
      if (activeSession.finalizing) {
        return;
      }
      if (state.activeCrawlSession) {
        state.activeCrawlSession.finalizing = true;
      }

      const outputDir =
        normalizeText(statePayload && statePayload.currentTaskOutputDir) ||
        normalizeText(statePayload && statePayload.outputDir) ||
        normalizeText(activeSession.currentTaskOutputDir) ||
        normalizeText(activeSession.outputDir);
      if (!outputDir) {
        appendLog('warn', '订阅抓取已结束，但未解析到输出目录，跳过订阅结果回收。');
        clearActiveCrawlSession();
        return;
      }

      try {
        const finalized = await desktopApi.finalizeAvSubscriptionCrawl({
          subscriptionId: activeSession.subscriptionId,
          currentTaskOutputDir: outputDir,
          outputDir,
          targetCodes: Array.isArray(activeSession.targetCodes) ? activeSession.targetCodes : []
        });
        if (finalized && finalized.subscription) {
          const nextList = state.subscriptions.map((entry) =>
            entry.id === finalized.subscription.id ? finalized.subscription : entry
          );
          updateSubscriptionState(nextList);
          appendLog(
            'info',
            `${finalized.subscription.actressName || activeSession.actressName || '订阅'} 结果已回收，仅保留 ${normalizeCount(finalized.keptCount, 0)} 部待更新影片。`
          );
          if (Array.isArray(finalized.keptCodes) && finalized.keptCodes.length > 0) {
            appendLog('info', `已保留番号：${normalizeCodes(finalized.keptCodes).join('、')}`);
          }
          if (Array.isArray(finalized.missingCodes) && finalized.missingCodes.length > 0) {
            appendLog('warn', `仍未抓到待更新番号：${normalizeCodes(finalized.missingCodes).join('、')}`);
          }
          const completedOutputDir = normalizeText(finalized.outputDir) || outputDir;
          if (completedOutputDir && desktopApi && typeof desktopApi.openPath === 'function') {
            try {
              await desktopApi.openPath(completedOutputDir);
              appendLog('info', `已打开磁力文件所在目录：${completedOutputDir}`);
            } catch (openError) {
              appendLog('warn', `自动打开磁力目录失败：${getErrorMessage(openError)}`);
            }
          }

          // 订阅抓取结束后，同步刷新“最近爬取选择”以及视频整理/刮削工作区。
          void loadRecentCrawlOptionsFromHistory();
          refreshPeerWorkspacesAfterSubscriptionCrawl(completedOutputDir || outputDir);
          const missingCodes = normalizeCodes(finalized.missingCodes);
          const keptCount = normalizeCount(finalized.keptCount, 0);
          const actressName = finalized.subscription.actressName || activeSession.actressName || '订阅';
          const summaryLines = [
            `演员：${actressName}`,
            `本次更新刮削：保留 ${keptCount} 部影片`
          ];
          if (missingCodes.length > 0) {
            const previewCodes = missingCodes.slice(0, 8).join('、');
            const remainingText = missingCodes.length > 8 ? ' 等' : '';
            summaryLines.push(`未抓到 ${missingCodes.length} 个番号：${previewCodes}${remainingText}`);
          } else {
            summaryLines.push('待更新番号已全部回收');
          }
          void showSubscriptionCompletionAlert(
            summaryLines.join('\n'),
            { type: missingCodes.length > 0 ? 'warning' : 'success' }
          );
        } else {
          void showSubscriptionCompletionAlert('订阅抓取已结束，但没有可回收的结果，请查看订阅日志。', {
            type: 'warning',
            title: '更新刮削未完成'
          });
        }
      } catch (error) {
        appendLog('error', `订阅抓取收尾失败: ${getErrorMessage(error)}`);
        void showSubscriptionCompletionAlert(`订阅结果回收失败：${getErrorMessage(error)}`, {
          type: 'warning',
          title: '更新刮削未完成'
        });
      } finally {
        clearActiveCrawlSession();
      }
    }

    function refreshPeerWorkspacesAfterSubscriptionCrawl(outputDir) {
      if (!outputDir) {
        return;
      }
      appendLog('info', '订阅抓取已完成，正在同步刷新视频整理与刮削工作区...');
      if (globalScope.organizerController && typeof globalScope.organizerController.refreshLatestCrawlOutput === 'function') {
        globalScope.organizerController.refreshLatestCrawlOutput().catch(() => {});
      }
      if (globalScope.libraryMetadataController && typeof globalScope.libraryMetadataController.loadCrawlSources === 'function') {
        globalScope.libraryMetadataController.loadCrawlSources().catch(() => {});
      }
    }

    function bindSubcrawlEvents() {
      if (!desktopApi) {
        return;
      }
      if (typeof desktopApi.onSubcrawlLog === 'function') {
        desktopApi.onSubcrawlLog((rawData) => {
          try {
            const data = typeof rawData === 'string' ? JSON.parse(rawData) : rawData;
            const message = normalizeText(data && data.message);
            if (message) {
              appendLog(normalizeText(data.level) || 'info', message);
            }
          } catch (_) {}
        });
      }
      if (typeof desktopApi.onSubcrawlState === 'function') {
        desktopApi.onSubcrawlState((rawData) => {
          try {
            const data = typeof rawData === 'string' ? JSON.parse(rawData) : rawData;
            if (!data) {
              return;
            }
            const status = normalizeText(data.status).toLowerCase();
            if (status === 'completed') {
              const succeeded = normalizeCount(data.batchSucceeded, 0);
              const noUpdate = normalizeCount(data.batchNoUpdate, 0);
              const failed = normalizeCount(data.failed, 0);
              const batchRun = state.activeBatchCrawl;
              applySubscriptionStagePanel({
                status,
                phaseTitle: '批量更新完成',
                phaseDescription: '本次订阅批量抓取已完成。',
                message: `成功 ${succeeded} 位，无更新 ${noUpdate} 位，失败 ${failed} 位。`,
                outputDir: batchRun && batchRun.outputDir,
                phaseIndex: normalizeCount(data.batchTotal, batchRun && batchRun.expectedTotal),
                phaseTotal: normalizeCount(data.batchTotal, batchRun && batchRun.expectedTotal),
                stats: {
                  queued: normalizeCount(data.batchTotal, batchRun && batchRun.expectedTotal),
                  attempted: normalizeCount(data.batchCompleted, 0),
                  completed: normalizeCount(data.batchCompleted, 0)
                }
              });
              if (!batchRun || batchRun.completionNotified) {
                return;
              }
              batchRun.completionNotified = true;
              setSubscriptionCrawlerStatus('\u5168\u90e8\u66f4\u65b0\u5b8c\u6210');
              setSummaryMessage(`\u4e00\u952e\u66f4\u65b0\u5b8c\u6210\uff1a\u6210\u529f ${succeeded}\uff0c\u65e0\u66f4\u65b0 ${noUpdate}\uff0c\u5931\u8d25 ${failed}\u3002`);
              appendLog('info', `\u4e00\u952e\u66f4\u65b0\u5b8c\u6210\uff1a\u6210\u529f ${succeeded}\uff0c\u65e0\u66f4\u65b0 ${noUpdate}\uff0c\u5931\u8d25 ${failed}\u3002`);
              void loadSubscriptions();
              void showSubscriptionCompletionAlert(
                `本次一键更新：共处理 ${normalizeCount(data.batchTotal, batchRun.expectedTotal)} 位演员。\n成功 ${succeeded} 位，无更新 ${noUpdate} 位，失败 ${failed} 位。`,
                { type: failed > 0 ? 'warning' : 'success' }
              );
              state.activeBatchCrawl = null;
              publishActiveSubscriptionCrawlSession(null);
              return;
            }
            if (status === 'stopped') {
              const batchRun = state.activeBatchCrawl;
              applySubscriptionStagePanel({
                status,
                phaseTitle: '批量更新已停止',
                phaseDescription: '本次订阅批量抓取已停止。',
                message: `已完成 ${normalizeCount(data.batchCompleted, 0)} / ${normalizeCount(data.batchTotal, batchRun && batchRun.expectedTotal)} 位演员。`,
                outputDir: batchRun && batchRun.outputDir,
                phaseIndex: normalizeCount(data.batchCompleted, 0),
                phaseTotal: normalizeCount(data.batchTotal, batchRun && batchRun.expectedTotal),
                stats: {
                  queued: normalizeCount(data.batchTotal, batchRun && batchRun.expectedTotal),
                  attempted: normalizeCount(data.batchCompleted, 0),
                  completed: normalizeCount(data.batchCompleted, 0)
                }
              });
              setSubscriptionCrawlerStatus('\u5df2\u505c\u6b62\u5168\u90e8\u66f4\u65b0');
              void loadSubscriptions();
              if (batchRun && !batchRun.completionNotified) {
                batchRun.completionNotified = true;
                void showSubscriptionCompletionAlert(
                  `一键更新已停止：已完成 ${normalizeCount(data.batchCompleted, 0)} / ${normalizeCount(data.batchTotal, batchRun.expectedTotal)} 位演员。`,
                  { type: 'warning', title: '更新刮削已停止' }
                );
              }
              state.activeBatchCrawl = null;
              publishActiveSubscriptionCrawlSession(null);
              return;
            }
            const completed = normalizeCount(data.batchCompleted, 0);
            const total = normalizeCount(data.batchTotal, 0);
            const active = normalizeCount(data.active, 0);
            applySubscriptionStagePanel({
              status: 'running',
              phaseTitle: '正在批量更新',
              phaseDescription: '多个订阅正在并行抓取，请查看执行记录了解详情。',
              message: `已完成 ${completed} / ${total} 位演员，运行中 ${active} 位。`,
              outputDir: state.activeBatchCrawl && state.activeBatchCrawl.outputDir,
              phaseIndex: completed,
              phaseTotal: total,
              stats: { queued: total, attempted: completed, completed }
            });
            setSubscriptionCrawlerStatus(total > 0 ? `\u66f4\u65b0\u4e2d ${completed}/${total}\uff08\u8fd0\u884c ${active}\uff09` : '\u66f4\u65b0\u4e2d...');
          } catch (_) {}
        });
      }
      if (typeof desktopApi.onAvSubscriptionV2Log === 'function') {
        desktopApi.onAvSubscriptionV2Log((rawData) => {
          try {
            const entries = Array.isArray(rawData) ? rawData : [rawData];
            entries.forEach((entry) => {
              const level = normalizeText(entry && entry.level) || 'info';
              const message = normalizeText(entry && entry.message);
              if (!message) {
                return;
              }
              appendLog(level, message);
            });
          } catch (_) {}
        });
      }
      if (typeof desktopApi.onAvSubscriptionV2ListUpdated === 'function') {
        desktopApi.onAvSubscriptionV2ListUpdated(() => {
          void loadSubscriptions();
        });
      }
      if (typeof desktopApi.onState === 'function') {
        desktopApi.onState((rawData) => {
          const activeSession = getActiveCrawlSession();
          if (!activeSession) {
            return;
          }
          try {
            const data = typeof rawData === 'string' ? JSON.parse(rawData) : rawData;
            if (!data) {
              return;
            }
            const status = normalizeText(data.status).toLowerCase();
            if (status === 'completed') {
              applySubscriptionStagePanel({
                status,
                phaseTitle: '抓取完成',
                phaseDescription: '订阅抓取已完成，正在回收结果。',
                message: normalizeText(data.message) || '抓取完成，正在回收订阅结果。',
                outputDir: data.currentTaskOutputDir || data.outputDir || '',
                stats: data.stats || {}
              });
              setSubscriptionCrawlerStatus('抓取完成');
              void finalizeActiveSubscriptionCrawl(data);
              return;
            }
            if (status === 'incomplete') {
              applySubscriptionStagePanel({
                status,
                phaseTitle: '抓取未完全完成',
                phaseDescription: '部分结果已生成，正在回收已抓到的待更新影片。',
                message: normalizeText(data.message) || '抓取未完全完成，正在回收已抓到的结果。',
                outputDir: data.currentTaskOutputDir || data.outputDir || '',
                stats: data.stats || {}
              });
              setSubscriptionCrawlerStatus('抓取未完全完成，正在回收已抓到的待更新结果');
              appendLog('warn', '主爬虫返回未完成状态，AV 订阅仍会先回收已抓到的待更新番号，并在收尾日志中列出缺失番号。');
              void finalizeActiveSubscriptionCrawl(data);
              return;
            }
            if (status === 'stopped') {
              applySubscriptionStagePanel({
                status,
                phaseTitle: '抓取已停止',
                phaseDescription: '本次订阅抓取已停止。',
                message: normalizeText(data.message) || '抓取已停止。',
                outputDir: data.currentTaskOutputDir || data.outputDir || '',
                stats: data.stats || {}
              });
              setSubscriptionCrawlerStatus('已停止');
              clearActiveCrawlSession();
              return;
            }
            if (status === 'error') {
              applySubscriptionStagePanel({
                status,
                phaseTitle: '抓取出错',
                phaseDescription: '订阅抓取发生错误，请查看执行记录。',
                message: normalizeText(data.message) || '抓取出错。',
                outputDir: data.currentTaskOutputDir || data.outputDir || '',
                stats: data.stats || {}
              });
              setSubscriptionCrawlerStatus('抓取出错');
              clearActiveCrawlSession();
              return;
            }
            setSubscriptionCrawlerStatus(normalizeText(data.message) || '抓取中...');
          } catch (_) {}
        });
      }
      if (typeof desktopApi.onLog === 'function') {
        desktopApi.onLog((rawData) => {
          const activeSession = getActiveCrawlSession();
          if (!activeSession) {
            return;
          }
          try {
            const entries = Array.isArray(rawData) ? rawData : [rawData];
            entries.forEach((entry) => {
              const level = normalizeText(entry && entry.level) || 'info';
              const message = normalizeText(entry && entry.message);
              if (!message) {
                return;
              }
              appendLog(level, message);
            });
          } catch (_) {}
        });
      }
    }

    function validateManualForm() {
      const actressName = normalizeText(elements.subscriptionActressName && elements.subscriptionActressName.value);
      const targetUrl = normalizeText(elements.subscriptionTargetUrl && elements.subscriptionTargetUrl.value);
      const syncedCount = normalizeCount(elements.subscriptionSyncedCount && elements.subscriptionSyncedCount.value, 0);
      const itemsPerPage = Math.max(1, normalizeCount(elements.subscriptionItemsPerPage && elements.subscriptionItemsPerPage.value, 30) || 30);
      const totalPages = Math.max(1, normalizeCount(elements.subscriptionTotalPages && elements.subscriptionTotalPages.value, 1) || 1);

      if (!actressName) {
        throw new Error('请填写女优名称。');
      }
      if (!targetUrl) {
        throw new Error('请填写演员抓取地址。');
      }
      if (!syncedCount) {
        throw new Error('请填写当前总量。');
      }

      return {
        actressName,
        targetUrl,
        syncedCount,
        itemsPerPage,
        totalPages
      };
    }

    async function scanSubscriptionsFromOutput() {
      const artifactInput = getCurrentSubscriptionArtifactInput();
      if (!artifactInput) {
        throw new Error('请先选择订阅快照或抓取结果输入。');
      }
      appendLog('info', `开始导入订阅基线：${artifactInput}`);
      const result = await desktopApi.scanAvSubscriptionsFromOutput({
        artifactInput,
        proxy: resolveSubscriptionProxyValue()
      });
      updateSubscriptionState(result && result.subscriptions ? result.subscriptions : []);
      setSummaryMessage(`已从 ${artifactInput} 扫描订阅基线，正在自动检测更新...`);
      appendLog('info', `已从 ${artifactInput} 扫描主女优订阅，后台正在自动检测更新。`);

      const filteredCodes = Array.isArray(result && result.filteredCodes) ? result.filteredCodes : [];
      if (filteredCodes.length > 0) {
        const codeList = filteredCodes.map((entry) => entry.code).join('、');
        appendLog('warn', `已过滤 ${filteredCodes.length} 部：${codeList}`);
      }
    }

    async function addSubscription() {
      const { actressName, targetUrl, syncedCount, itemsPerPage, totalPages } = validateManualForm();
      const saved = await desktopApi.addAvSubscription({
        actressName,
        targetUrl,
        syncedCount,
        itemsPerPage,
        totalPages,
        preferredOutputDir: '',
        proxy: resolveSubscriptionProxyValue()
      });
      const nextList = currentSubscriptionList();
      const existingIndex = nextList.findIndex((item) => item.id === saved.id);
      if (existingIndex >= 0) {
        nextList.splice(existingIndex, 1, saved);
      } else {
        nextList.unshift(saved);
      }
      updateSubscriptionState(nextList);
      applySubscriptionCrawlerDraft({
        crawlUrl: saved.crawlUrl,
        outputDir: saved.preferredOutputDir || '',
        targetCount: saved.pendingCount > 0 ? saved.pendingCount : 1,
        actressName: saved.actressName
      });
      setSummaryMessage(`订阅已保存：${saved.actressName}，当前总量 ${saved.currentTotal || saved.baselineCount || 0} 部。`);
      appendLog('info', `已新增订阅：${saved.actressName}。`);
    }

    function clearForm() {
      if (elements.subscriptionActressName) elements.subscriptionActressName.value = '';
      if (elements.subscriptionTargetUrl) elements.subscriptionTargetUrl.value = '';
      if (elements.subscriptionSyncedCount) elements.subscriptionSyncedCount.value = '';
      if (elements.subscriptionItemsPerPage) elements.subscriptionItemsPerPage.value = '30';
      if (elements.subscriptionTotalPages) elements.subscriptionTotalPages.value = '1';
      if (elements.subscriptionOutput) elements.subscriptionOutput.value = '';
      if (elements.subscriptionRecentCrawlSelect) elements.subscriptionRecentCrawlSelect.value = '';
    }

    async function loadSubscriptions() {
      const result = await desktopApi.listAvSubscriptions();
      const subscriptions = result && result.subscriptions ? result.subscriptions : [];
      updateSubscriptionState(subscriptions);
      syncGlobalActressFilterInput(subscriptions);
      void hydrateMissingSubscriptionMedia();
    }

    function clearAllSubscriptions() {
      if (typeof desktopApi.clearAvSubscriptions !== 'function') {
        throw new Error('当前版本暂不支持清空订阅。');
      }
      const confirmed = confirmAction(['确认清空全部订阅吗？', '此操作不可恢复，是否继续清空全部订阅？']);
      if (!confirmed) {
        return Promise.resolve();
      }

      return desktopApi.clearAvSubscriptions().then((result) => {
        updateSubscriptionState(result && result.subscriptions ? result.subscriptions : []);
        setSummaryMessage(`已清空全部订阅，共清除 ${normalizeCount(result && result.clearedCount, 0)} 条。`);
        appendLog('warn', `已清空全部订阅，共清除 ${normalizeCount(result && result.clearedCount, 0)} 条。`);
      });
    }

    function renderLogsFromState() {
      if (!elements.subscriptionLogView) {
        return;
      }
      clearChildren(elements.subscriptionLogView);
      if (state.logs.length === 0) {
        const empty = createElement('p', {
          className: 'subscription-empty',
          textContent: '当前还没有执行日志。'
        });
        elements.subscriptionLogView.appendChild(empty);
        return;
      }
      const fragment = document.createDocumentFragment();
      state.logs.forEach((item) => {
        const line = createElement('div', {
          className: 'log-line',
          html: `<strong>${escapeHtml(item.level)}</strong> ${escapeHtml(item.message)}`
        });
        fragment.appendChild(line);
      });
      elements.subscriptionLogView.appendChild(fragment);
    }

    function bindAsyncClick(button, handler, onError) {
      if (typeof bindAsyncClickHelper === 'function') {
        bindAsyncClickHelper(button, handler, {
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

    function bindEvents() {
      if (eventsBound) {
        return;
      }
      eventsBound = true;
      if (elements.subscriptionListSearch) {
        elements.subscriptionListSearch.addEventListener('input', () => {
          updateSubscriptionListSearch(elements.subscriptionListSearch.value);
        });
      }
      if (elements.subscriptionListFilter) {
        elements.subscriptionListFilter.addEventListener('change', () => {
          state.listFilter = normalizeText(elements.subscriptionListFilter.value) || 'all';
          setSummaryCounts();
          renderSubscriptionList();
        });
      }
      if (elements.subscriptionListSort) {
        elements.subscriptionListSort.value = state.listSort;
        elements.subscriptionListSort.addEventListener('change', () => {
          state.listSort = normalizeText(elements.subscriptionListSort.value) || 'updated';
          setSummaryCounts();
          renderSubscriptionList();
          renderSubscriptionDetail();
        });
      }
      if (elements.subscriptionGlobalActressFilter) {
        elements.subscriptionGlobalActressFilter.addEventListener('input', () => {
          const rawValue = String(elements.subscriptionGlobalActressFilter.value || '').trim();
          if (!rawValue) {
            return;
          }
          elements.subscriptionGlobalActressFilter.value = String(normalizeActressFilterThreshold(rawValue));
        });
      }
      bindAsyncClick(elements.subscriptionApplyActressFilterButton, applyActressFilterToAllSubscriptions);
      bindAsyncClick(elements.subscriptionDetectAllButton, refreshAllSubscriptions);
      bindAsyncClick(elements.subscriptionImportRecentButton, async () => {
        const option = getSelectedRecentCrawlOption();
        if (!option) {
          throw new Error('请先选择最近抓取记录。');
        }
        applyRecentCrawlOption(option);
      });
      bindAsyncClick(elements.subscriptionUseSeedButton, async () => {
        const option = getSelectedRecentCrawlOption();
        if (!option) {
          throw new Error('请先选择最近抓取记录。');
        }
        applyRecentCrawlOption(option);
      });
      bindAsyncClick(elements.subscriptionScanButton, scanSubscriptionsFromOutput);
      bindAsyncClick(elements.subscriptionAddButton, addSubscription);
      bindAsyncClick(elements.subscriptionRefreshButton, async () => {
        const selectedId = normalizeText(state.selectedId);
        if (!selectedId) {
          throw new Error('请先选中一条订阅，再执行“检测更新”。');
        }
        await refreshOneSubscription(selectedId);
      });
      bindAsyncClick(elements.subscriptionClearAllButton, clearAllSubscriptions);
      bindAsyncClick(elements.subscriptionManualCreateButton, addSubscription);
      bindAsyncClick(elements.subscriptionManualImportButton, async () => {
        const file = elements.subscriptionManualJsonInput && elements.subscriptionManualJsonInput.files
          ? elements.subscriptionManualJsonInput.files[0]
          : null;
        if (!file) {
          throw new Error('请先选择 JSON 文件。');
        }
        const text = await file.text();
        const payload = JSON.parse(text);
        state.pendingImportedJson = payload;
        if (!payload || typeof payload !== 'object') {
          throw new Error('JSON 文件内容无效。');
        }
        const actressName = normalizeText(payload.actressName || payload.name);
        const crawlUrl = normalizeText(payload.crawlUrl || payload.url);
        const baselineCodes = normalizeCodes(
          Array.isArray(payload.baselineCodes)
            ? payload.baselineCodes
            : Array.isArray(payload.codes)
              ? payload.codes
              : []
        );
        if (!actressName || !crawlUrl || baselineCodes.length === 0) {
          throw new Error('JSON 导入失败：至少需要女优名称、抓取地址和唯一番号列表。');
        }
        const imported = await desktopApi.scanAvSubscriptionsFromOutput({
          artifactInput: normalizeText(payload.artifactInput || payload.outputDir || payload.crawlProfilePath || payload.filmDataPath)
        }).catch(() => null);
        if (imported && imported.subscriptions) {
          updateSubscriptionState(imported.subscriptions);
        }
        const importedFilteredCodes = Array.isArray(imported && imported.filteredCodes) ? imported.filteredCodes : [];
        if (importedFilteredCodes.length > 0) {
          const codeList = importedFilteredCodes.map((entry) => entry.code).join('、');
          appendLog('warn', `已过滤 ${importedFilteredCodes.length} 部：${codeList}`);
        }
        setSummaryMessage(`已处理 JSON 导入：${actressName}。`);
        appendLog('info', `JSON 导入完成：${actressName}，基线番号 ${baselineCodes.length} 个。`);
      });

      if (elements.subscriptionCrawlerProxy) {
        elements.subscriptionCrawlerProxy.addEventListener('input', scheduleSubscriptionProxyValidation);
        elements.subscriptionCrawlerProxy.addEventListener('blur', async () => {
          applyDefaultSubscriptionProxy();
          await validateSubscriptionProxyValue(resolveSubscriptionProxyValue());
        });
      }
      if (elements.subscriptionCrawlerCloudflare) {
        elements.subscriptionCrawlerCloudflare.addEventListener('change', () => {
          const enabled = resolveSubscriptionCloudflareEnabled();
          appendLog('info', `Cloudflare 兼容：${enabled ? '已启用' : '已关闭'}。`);
          setSubscriptionCrawlerStatus(enabled && state.antiBlockReady ? '准备就绪' : '待启动');
        });
      }
      if (elements.subscriptionUpdateAntiBlockButton) {
        bindAsyncClick(elements.subscriptionUpdateAntiBlockButton, updateSubscriptionAntiBlock);
      }

      if (elements.subscriptionUseLatestOutputButton) {
        bindAsyncClick(elements.subscriptionUseLatestOutputButton, async () => {
          const { artifactInput: latestOutput, message } = await artifactInputHelper.fillLatestArtifactInput(desktopApi);
          if (latestOutput && elements.subscriptionOutput) {
            elements.subscriptionOutput.value = latestOutput;
            appendLog('info', message || `已回填最近抓取结果输入：${latestOutput}`);
          }
        });
      }
      if (elements.subscriptionUseRecentCrawlButton) {
        bindAsyncClick(elements.subscriptionUseRecentCrawlButton, async () => {
          const option = getSelectedRecentCrawlOption();
          if (!option) {
            throw new Error('请先选择最近抓取记录。');
          }
          applyRecentCrawlOption(option);
        });
      }
      if (elements.subscriptionClearFormButton) {
        elements.subscriptionClearFormButton.addEventListener('click', () => {
          clearForm();
          appendLog('info', '已清空订阅输入表单。');
        });
      }

      if (elements.subscriptionStartCrawlButton) {
        bindAsyncClick(elements.subscriptionStartCrawlButton, startPreparedSubscriptionCrawl);
      }
      if (elements.subscriptionBatchCrawlButton) {
        elements.subscriptionBatchCrawlButton.addEventListener('click', openBatchCrawlDialog);
      }
      if (elements.subscriptionBatchConfirmButton) {
        bindAsyncClick(elements.subscriptionBatchConfirmButton, startBatchCrawl);
      }
      if (elements.subscriptionBatchCancelButton) {
        elements.subscriptionBatchCancelButton.addEventListener('click', closeBatchCrawlDialog);
      }
      if (elements.subscriptionStopCrawlButton) {
        bindAsyncClick(elements.subscriptionStopCrawlButton, stopIndependentCrawl);
      }
      if (elements.subscriptionOpenOutputButton) {
        bindAsyncClick(elements.subscriptionOpenOutputButton, openSubscriptionMagnetFile);
      }
      if (elements.subscriptionOpenFolderButton) {
        bindAsyncClick(elements.subscriptionOpenFolderButton, openSubscriptionFolder);
      }

      if (elements.subscriptionList) {
        elements.subscriptionList.addEventListener('click', (event) => {
          const target = event.target;
          if (!(target instanceof HTMLElement)) {
            return;
          }
          const card = target.closest('.subscription-card');
          if (card instanceof HTMLElement) {
            const itemId = normalizeText(card.dataset.id);
            if (itemId) {
              state.selectedId = itemId;
              renderSubscriptionList();
              renderSubscriptionDetail();
            }
          }
        });
      }
    }

    function bootstrap() {
      if (bootstrapCompleted) {
        if (proxyValidationState.autoValidationStopped) {
          proxyValidationState.autoValidationStopped = false;
          void loadGlobalSubscriptionProxy()
            .then((proxyValue) => validateSubscriptionProxyValue(proxyValue))
            .then(() => scheduleSubscriptionProxyAutoValidation());
        }
        return bootstrapPromise || Promise.resolve();
      }
      bootstrapCompleted = true;
      publishActiveSubscriptionCrawlSession(null);
      bindEvents();
      renderLogsFromState();
      setSummaryCounts();
      renderSubscriptionDetail();
      setSubscriptionCrawlerStatus('待启动');
      applySubscriptionCrawlerDefaults();
      void resolveSubscriptionDefaultOutputDir().then((outputDir) => {
        state.defaultOutputDir = normalizeText(outputDir);
        if (state.preparedDraft) {
          state.preparedDraft.outputDir = resolveSubscriptionActorOutputDir(state.preparedDraft);
        }
        renderSubscriptionDetail();
      });
      if (elements.subscriptionCrawlerCloudflare) {
        elements.subscriptionCrawlerCloudflare.checked = true;
      }
      setSubscriptionProxyStatus('checking');
      setSummaryMessage('等待检测订阅更新。');
      applySubscriptionStagePanel({
        status: 'idle',
        phaseTitle: '等待开始抓取',
        phaseDescription: '加载配置并初始化本次抓取运行环境。',
        message: '等待开始抓取。',
        stats: { pageIndex: 0, queued: 0, attempted: 0, completed: 0 }
      });
      appendLog('info', 'AV 订阅模块已就绪。');
      const proxyInitialization = loadGlobalSubscriptionProxy()
        .then((proxyValue) => validateSubscriptionProxyValue(proxyValue))
        .then(() => {
          scheduleSubscriptionProxyAutoValidation();
        });
      bindSubcrawlEvents();
      bootstrapPromise = loadRecentCrawlOptionsFromHistory()
        .then(() => loadSubscriptions())
        .then(() => proxyInitialization)
        .catch((error) => {
          appendLog('error', getErrorMessage(error));
          setSummaryMessage('订阅列表加载失败，请稍后重试。');
        });
      return bootstrapPromise;
    }

    function dispose() {
      // 审计 H-07：工作区切换或页面卸载时停止代理自动验证，避免定时器泄漏。
      stopSubscriptionProxyAutoValidation();
    }

    return {
      bootstrap,
      dispose,
      loadRecentCrawlOptionsFromHistory,
      loadSubscriptions,
      applyCrawlStagePanel: applySubscriptionStagePanel,
      getBridgeCrawlSession
    };
  }

  globalScope.desktopSubscriptionController = {
    createSubscriptionController
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
