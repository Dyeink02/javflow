// Form controller owns crawler setup inputs and client-side validation.
// It normalizes raw field values and delegates persisted/runtime state rendering
// to other controllers, so setup bugs can be isolated from crawl-state bugs.
//
// Ownership summary:
// 1) own editable crawler setup fields and local validation
// 2) manage input-side helpers such as drafts, templates, and proxy checks
// 3) hand normalized settings to downstream runtime/bridge layers without
//    owning crawl execution or result projection
//
// File map for maintainers:
// 1) raw input normalization helpers
// 2) crawler form bootstrap/draft/template/proxy helpers
// 3) validated crawl-settings payload builders and submit actions
(function initializeFormController(globalScope) {
  const rendererHelpers = globalScope.desktopRendererHelpers || {};
  const getErrorMessage = rendererHelpers.getErrorMessage;
  const bindAsyncClickHelper = rendererHelpers.bindAsyncClick || null;
  const safeLocalStorageGet = rendererHelpers.safeLocalStorageGet;
  const safeLocalStorageSet = rendererHelpers.safeLocalStorageSet;

  if (!getErrorMessage || !safeLocalStorageGet || !safeLocalStorageSet) {
    throw new Error('desktopRendererHelpers must be loaded before formController');
  }

  function normalizeIntegerText(value) {
    // Input normalization accepts common full-width and mojibake variants so
    // the setup form remains tolerant without pushing those quirks into the
    // backend settings contract.
    return String(value ?? '')
      .replace(/[０-９]/g, (char) => String.fromCharCode(char.charCodeAt(0) - 0xfee0))
      .replace(/＋/g, '+')
      .replace(/[－–—]/g, '-')
      .replace(/[，,]/g, '')
      .replace(/\u3000/g, ' ')
      .trim();
  }

  function toSafeInteger(value, fallback) {
    const parsed = Number.parseInt(normalizeIntegerText(value), 10);
    return Number.isFinite(parsed) ? parsed : fallback;
  }

  function normalizeMinInteger(value, minimum, fallback) {
    return Math.max(minimum, toSafeInteger(value, fallback));
  }

  function normalizeNonNegativeInteger(value, fallback) {
    return Math.max(0, toSafeInteger(value, fallback));
  }

  // createFormController is the crawler workspace's input-side module
  // boundary. It should own editable settings state only.
  function createFormController(options) {
    const { elements, desktopApi, logController, stateController, uiText } = options;
    const platformBridge = globalScope.desktopPlatformBridge || null;
    const { UI_TEXT, TASK_TEMPLATES } = uiText;
    const PROXY_AUTO_CHECK_INTERVAL_MS = 30000;
    const STORAGE_KEYS = {
      crawlerDraft: 'jav.crawler.formDraft.v1'
    };
    const proxyValidationState = {
      timerId: null,
      autoTimerId: null,
      requestToken: 0,
      lastValue: '',
      lastStatus: 'empty',
      autoValidationStopped: false,
      autoValidationCount: 0
    };
    let eventsBound = false;
    let bootstrapCompleted = false;
    let hydratingFormState = false;
    // A cross-workspace handoff can arrive after this controller was disposed
    // while another tab was open. Keep it here until bootstrap has restored
    // saved settings, otherwise that restore overwrites the requested target.
    let pendingActressLookupPrefill = null;

    // Form-controller responsibilities:
    // 1) normalize raw user input into stable crawl settings
    // 2) own local input-side behaviors such as proxy validation and templates
    // 3) hand validated settings to the bridge without coupling to crawl-state
    //
    // Keep runtime/result-panel interpretation out of this file. That boundary
    // is important when debugging whether an issue came from setup, bridge
    // dispatch, or downstream crawler execution.
    //
    // Workflow map:
    // 1) load saved settings + local draft
    // 2) normalize/validate current input values
    // 3) run input-side preflight checks such as proxy validation
    // 4) hand one normalized crawl-settings payload to desktopApi
    // 5) never interpret crawl runtime/results beyond basic form locking

    function normalizeActressThresholdField() {
      if (!elements.actressCountFilterThreshold) {
        return;
      }

      const normalizedValue = normalizeIntegerText(elements.actressCountFilterThreshold.value);
      if (elements.actressCountFilterThreshold.value !== normalizedValue) {
        elements.actressCountFilterThreshold.value = normalizedValue;
      }
    }

    function normalizeFilmCodeFilterField() {
      if (!elements.filmCodeFilterThreshold) {
        return;
      }

      const rawValue = String(elements.filmCodeFilterThreshold.value || '');
      const normalizedValue = rawValue
        .split(',')
        .map((part) => part.trim())
        .filter((part) => part.length > 0)
        .join(', ');
      if (elements.filmCodeFilterThreshold.value !== normalizedValue) {
        elements.filmCodeFilterThreshold.value = normalizedValue;
      }
    }

    function parseMinReleaseDate(raw) {
      const trimmed = String(raw || '').trim();
      if (!trimmed) {
        return '';
      }

      const normalized = trimmed
        .replace(/[０-９]/g, (char) => String.fromCharCode(char.charCodeAt(0) - 0xfee0))
        .replace(/[／]/g, '/')
        .replace(/[．。]/g, '.')
        .replace(/[年月]/g, '-')
        .replace(/日/g, '')
        .replace(/\s+/g, '-');

      const matches = normalized.match(/^(\d{4})[-/.](\d{1,2})[-/.](\d{1,2})$/);
      if (!matches) {
        return '';
      }

      const y = Number(matches[1]);
      const m = Number(matches[2]);
      const d = Number(matches[3]);
      if (y < 1000 || y > 9999 || m < 1 || m > 12 || d < 1 || d > 31) {
        return '';
      }

      const date = new Date(y, m - 1, d);
      if (date.getFullYear() !== y || date.getMonth() !== m - 1 || date.getDate() !== d) {
        return '';
      }

      const pad = (value) => String(value).padStart(2, '0');
      return `${y}-${pad(m)}-${pad(d)}`;
    }

    function normalizeMinReleaseDateField() {
      if (!elements.minReleaseDate) {
        return;
      }

      const parsed = parseMinReleaseDate(elements.minReleaseDate.value);
      if (parsed && parsed !== elements.minReleaseDate.value) {
        elements.minReleaseDate.value = parsed;
      }
    }

    function buildCrawlerDraftSnapshot() {
      // Draft snapshots are renderer-local protection for in-progress edits.
      // They are not the persisted product settings source of truth.
      const settings = getSettings();
      return {
        version: '2026-05-07-crawler-draft',
        updatedAt: new Date().toISOString(),
        ...settings
      };
    }

    function loadCrawlerDraftSnapshot() {
      const rawValue = safeLocalStorageGet(STORAGE_KEYS.crawlerDraft, '');
      if (!rawValue) {
        return null;
      }

      try {
        const parsed = JSON.parse(rawValue);
        return parsed && typeof parsed === 'object' ? parsed : null;
      } catch {
        return null;
      }
    }

    function persistCrawlerDraft() {
      // Draft persistence only exists to protect in-progress edits from
      // renderer reload/bootstrap re-entry. Do not write drafts while the form
      // is still being hydrated from saved settings.
      if (!bootstrapCompleted || hydratingFormState) {
        return;
      }

      safeLocalStorageSet(STORAGE_KEYS.crawlerDraft, JSON.stringify(buildCrawlerDraftSnapshot()));
    }

    function clearCrawlerDraft() {
      safeLocalStorageSet(STORAGE_KEYS.crawlerDraft, '');
    }

    function applyCrawlerDraftSnapshot(draft = null) {
      if (!draft || typeof draft !== 'object') {
        return false;
      }

      // Draft hydration should stay field-by-field and tolerant so one stale
      // draft key never blocks the rest of the form from recovering.
      if (Object.prototype.hasOwnProperty.call(draft, 'taskTemplate') && TASK_TEMPLATES[draft.taskTemplate]) {
        elements.taskTemplate.value = draft.taskTemplate;
      }
      if (Object.prototype.hasOwnProperty.call(draft, 'base')) {
        elements.base.value = String(draft.base || '').trim();
      }
      if (Object.prototype.hasOwnProperty.call(draft, 'output')) {
        elements.output.value = String(draft.output || '').trim();
      }
      if (Object.prototype.hasOwnProperty.call(draft, 'limit')) {
        elements.limit.value = String(normalizeNonNegativeInteger(draft.limit, 0));
      }
      if (Object.prototype.hasOwnProperty.call(draft, 'totalPages')) {
        elements.totalPages.value = String(normalizeNonNegativeInteger(draft.totalPages, 0));
      }
      if (Object.prototype.hasOwnProperty.call(draft, 'itemsPerPage')) {
        elements.itemsPerPage.value = String(normalizeMinInteger(draft.itemsPerPage, 1, UI_TEXT.limits.defaultItemsPerPage));
      }
      if (Object.prototype.hasOwnProperty.call(draft, 'parallel')) {
        elements.parallel.value = String(normalizeMinInteger(draft.parallel, 1, TASK_TEMPLATES.balanced.parallel));
      }
      if (Object.prototype.hasOwnProperty.call(draft, 'delay')) {
        elements.delay.value = String(normalizeNonNegativeInteger(draft.delay, TASK_TEMPLATES.balanced.delay));
      }
      if (Object.prototype.hasOwnProperty.call(draft, 'timeout')) {
        elements.timeout.value = String(
          normalizeMinInteger(draft.timeout, UI_TEXT.limits.minTimeout, TASK_TEMPLATES.balanced.timeout)
        );
      }
      if (Object.prototype.hasOwnProperty.call(draft, 'proxy')) {
        elements.proxy.value = String(draft.proxy || '').trim();
      }
      if (Object.prototype.hasOwnProperty.call(draft, 'magnetExcludeKeywords')) {
        elements.magnetExcludeKeywords.value = String(draft.magnetExcludeKeywords || '').trim();
      }
      if (Object.prototype.hasOwnProperty.call(draft, 'actressCountFilterThreshold')) {
        elements.actressCountFilterThreshold.value = String(
          normalizeNonNegativeInteger(draft.actressCountFilterThreshold, 0)
        );
      }
      if (Object.prototype.hasOwnProperty.call(draft, 'filmCodeFilterThreshold')) {
        elements.filmCodeFilterThreshold.value = String(draft.filmCodeFilterThreshold || '').trim();
      }
      if (Object.prototype.hasOwnProperty.call(draft, 'minReleaseDate')) {
        elements.minReleaseDate.value = String(draft.minReleaseDate || '').trim();
      }
      if (Object.prototype.hasOwnProperty.call(draft, 'cloudflare')) {
        elements.cloudflare.checked = Boolean(draft.cloudflare);
      }
      // 结果二次校验是 JAV 抓取的固定收尾步骤，不再允许草稿关闭。
      if (elements.secondValidation) elements.secondValidation.checked = true;
      if (Object.prototype.hasOwnProperty.call(draft, 'nomag')) {
        elements.nomag.checked = Boolean(draft.nomag);
      }
      if (Object.prototype.hasOwnProperty.call(draft, 'allmag')) {
        elements.allmag.checked = Boolean(draft.allmag);
      }
      if (Object.prototype.hasOwnProperty.call(draft, 'magnetContentValidation')) {
        elements.magnetContentValidation.checked = Boolean(draft.magnetContentValidation);
      }
      if (Object.prototype.hasOwnProperty.call(draft, 'nopic')) {
        elements.nopic.checked = Boolean(draft.nopic);
      }
      if (Object.prototype.hasOwnProperty.call(draft, 'metadataOnly') && elements.metadataOnly) {
        elements.metadataOnly.checked = Boolean(draft.metadataOnly);
      }

      return true;
    }

    function clearProxyValidationTimer() {
      if (!proxyValidationState.timerId) {
        return;
      }

      clearTimeout(proxyValidationState.timerId);
      proxyValidationState.timerId = null;
    }

    function clearProxyAutoValidationTimer() {
      if (!proxyValidationState.autoTimerId) {
        return;
      }

      clearTimeout(proxyValidationState.autoTimerId);
      proxyValidationState.autoTimerId = null;
    }

    function setProxyStatus(status, detailText = '') {
      const normalizedStatus =
        status === 'checking' || status === 'valid' || status === 'invalid' ? status : 'empty';
      const statusText = UI_TEXT.proxyStatus[normalizedStatus] || UI_TEXT.proxyStatus.empty;
      const fallbackDetailKey = `${normalizedStatus}Detail`;
      const nextDetail =
        typeof detailText === 'string' && detailText.trim()
          ? detailText.trim()
          : UI_TEXT.proxyStatus[fallbackDetailKey] || UI_TEXT.fields.proxyHelp;

      proxyValidationState.lastStatus = normalizedStatus;

      if (elements.proxyStatus) {
        elements.proxyStatus.className = `proxy-status-chip ${normalizedStatus}`;
        elements.proxyStatus.textContent = statusText;
      }

      if (elements.proxyStatusDetail) {
        elements.proxyStatusDetail.textContent = nextDetail;
      }
    }

    async function validateProxyValue(proxyValue, options = {}) {
      // Proxy validation belongs to setup-time transport diagnosis only. The
      // result should update form UX, not trigger broader page-state reloads.
      const trimmedValue = String(proxyValue || '').trim();
      clearProxyValidationTimer();
      proxyValidationState.requestToken += 1;
      const requestToken = proxyValidationState.requestToken;
      proxyValidationState.lastValue = trimmedValue;

      if (!trimmedValue) {
        setProxyStatus('empty');
        return {
          status: 'empty',
          detail: UI_TEXT.proxyStatus.emptyDetail
        };
      }

      setProxyStatus('checking');

      try {
        const result = await desktopApi.validateProxy(trimmedValue, {
          targetUrl: elements.base.value.trim() || UI_TEXT.placeholders.base
        });

        if (requestToken !== proxyValidationState.requestToken) {
          return result;
        }

        if (result && result.status === 'valid') {
          setProxyStatus('valid', result.detail);
          return result;
        }

        setProxyStatus('invalid', result && result.detail);
        return result || { status: 'invalid', detail: UI_TEXT.proxyStatus.invalidDetail };
      } catch (error) {
        const message = getErrorMessage(error);
        if (requestToken === proxyValidationState.requestToken) {
          setProxyStatus('invalid', message);
        }
        return {
          status: 'invalid',
          detail: message
        };
      }
    }

    function scheduleProxyValidation(delayMs = 650) {
      const trimmedValue = elements.proxy.value.trim();
      clearProxyValidationTimer();

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
      // 审计 H-07：代理自动验证形成无限递归定时器链。
      // 这里限制最大自动验证次数，并在控制器 dispose 时彻底停止。
      if (proxyValidationState.autoValidationStopped || proxyValidationState.autoValidationCount >= 100) {
        return;
      }

      clearProxyAutoValidationTimer();
      proxyValidationState.autoTimerId = setTimeout(async () => {
        proxyValidationState.autoTimerId = null;
        if (proxyValidationState.autoValidationStopped) {
          return;
        }
        proxyValidationState.autoValidationCount += 1;
        const trimmedValue = elements.proxy.value.trim();

        if (!trimmedValue) {
          setProxyStatus('empty');
          scheduleProxyAutoValidation();
          return;
        }

        await validateProxyValue(trimmedValue);
        scheduleProxyAutoValidation();
      }, Math.max(1000, delayMs));
    }

    function stopProxyAutoValidation() {
      proxyValidationState.autoValidationStopped = true;
      clearProxyAutoValidationTimer();
      clearProxyValidationTimer();
    }

    async function ensureProxyReady(proxyValue) {
      const trimmedValue = String(proxyValue || '').trim();
      if (!trimmedValue) {
        setProxyStatus('empty');
        return;
      }

      const result = await validateProxyValue(trimmedValue);
      if (!result || result.status !== 'valid') {
        throw new Error(UI_TEXT.validation.proxyInvalid);
      }
    }

    function normalizeMagnetExcludeKeywords(rawValue) {
      return String(rawValue || '')
        .split(',')
        .map((item) => item.trim())
        .filter(Boolean)
        .join(', ');
    }

    function validateMagnetExcludeKeywords(rawValue) {
      const trimmedValue = String(rawValue || '').trim();
      if (!trimmedValue) {
        return {
          valid: true,
          normalized: ''
        };
      }

      if (/[，、；;\r\n]/.test(trimmedValue)) {
        return {
          valid: false,
          normalized: trimmedValue
        };
      }

      const parts = trimmedValue.split(',');
      if (
        trimmedValue.startsWith(',') ||
        trimmedValue.endsWith(',') ||
        parts.some((item) => !item.trim())
      ) {
        return {
          valid: false,
          normalized: trimmedValue
        };
      }

      return {
        valid: true,
        normalized: normalizeMagnetExcludeKeywords(trimmedValue)
      };
    }

    function focusMagnetExcludeKeywordsField() {
      window.setTimeout(() => {
        elements.magnetExcludeKeywords.focus();
        elements.magnetExcludeKeywords.select();
      }, 0);
    }

    async function ensureMagnetExcludeKeywordsReady(rawValue) {
      const validation = validateMagnetExcludeKeywords(rawValue);
      if (validation.valid) {
        elements.magnetExcludeKeywords.value = validation.normalized;
        return validation.normalized;
      }

      if (desktopApi && typeof desktopApi.showAlert === 'function') {
        await desktopApi.showAlert({
          type: 'warning',
          title: UI_TEXT.fields.magnetExcludeKeywords,
          message: UI_TEXT.validation.magnetExcludeKeywordsInvalid
        });
      } else {
        window.alert(UI_TEXT.validation.magnetExcludeKeywordsInvalid);
      }

      focusMagnetExcludeKeywordsField();
      throw new Error(UI_TEXT.validation.magnetExcludeKeywordsInvalid);
    }

    function applyBackgroundImage(backgroundImageUrl = '') {
      if (backgroundImageUrl) {
        document.documentElement.style.setProperty('--page-backdrop-image', `url("${backgroundImageUrl}")`);
      } else {
        document.documentElement.style.removeProperty('--page-backdrop-image');
      }

      if (elements.resetBackgroundButton) {
        elements.resetBackgroundButton.disabled = !backgroundImageUrl;
      }
    }

    function getItemsPerPage() {
      return normalizeMinInteger(elements.itemsPerPage.value, 1, UI_TEXT.limits.defaultItemsPerPage);
    }

    function getSuggestedPages(limit, itemsPerPage) {
      if (!limit || limit <= 0 || !itemsPerPage || itemsPerPage <= 0) {
        return null;
      }

      const pages = Math.ceil(limit / itemsPerPage);
      const remainder = limit % itemsPerPage;

      return {
        pages,
        lastPageCount: remainder === 0 ? itemsPerPage : remainder
      };
    }

    function applyTemplate(templateKey, templateOptions = {}) {
      const template = TASK_TEMPLATES[templateKey] || TASK_TEMPLATES.balanced;
      const { keepLimit = true, keepBase = true, keepOutput = true } = templateOptions;

      elements.taskTemplate.value = templateKey;
      elements.parallel.value = String(template.parallel);
      elements.delay.value = String(template.delay);
      elements.timeout.value = String(template.timeout);
      elements.itemsPerPage.value = String(template.itemsPerPage);
      elements.cloudflare.checked = template.cloudflare;
      if (elements.secondValidation) elements.secondValidation.checked = true;

      if (!keepLimit) {
        elements.limit.value = '0';
      }

      if (!keepBase) {
        elements.base.value = '';
      }

      if (!keepOutput) {
        elements.output.value = '';
      }

      refreshSuggestedPages();
      persistCrawlerDraft();
    }

    // 审计 H-08：formController.getSettings() 访问 DOM 元素无空值保护。
    // 下面两个辅助函数在元素不存在时返回安全默认值，避免 HTML 缺失即崩溃。
    function getElementValue(element, defaultValue) {
      if (defaultValue === undefined) {
        defaultValue = '';
      }
      return element && typeof element.value === 'string' ? element.value : defaultValue;
    }

    function getElementChecked(element, defaultValue) {
      if (defaultValue === undefined) {
        defaultValue = false;
      }
      return element && typeof element.checked === 'boolean' ? element.checked : defaultValue;
    }

    function getSettings() {
      // 注意：该返回值会被直接送入 Wails/sidecar/Go 三条运行链路。
      // 这里一旦漏字段或把数字归一化成 0，后面的演员过滤/统计都会整体失真。
      //
      // 排障顺序建议：
      // 1) 先确认这里收集的 payload 是否正确
      // 2) 再确认 desktopApi.startCrawl / restartCrawl 是否收到同样的数据
      // 3) 最后才看运行时状态面板或日志为何显示异常
      return {
        frontendPayloadVersion: '2026-05-04-review-bootstrap-sync',
        base: getElementValue(elements.base).trim(),
        output: getElementValue(elements.output).trim(),
        limit: normalizeNonNegativeInteger(getElementValue(elements.limit), 0),
        totalPages: normalizeNonNegativeInteger(getElementValue(elements.totalPages), 0),
        itemsPerPage: getItemsPerPage(),
        parallel: normalizeMinInteger(getElementValue(elements.parallel), 1, TASK_TEMPLATES.balanced.parallel),
        delay: normalizeNonNegativeInteger(getElementValue(elements.delay), TASK_TEMPLATES.balanced.delay),
        timeout: normalizeMinInteger(
          getElementValue(elements.timeout),
          UI_TEXT.limits.minTimeout,
          TASK_TEMPLATES.balanced.timeout
        ),
        proxy: getElementValue(elements.proxy).trim(),
        magnetExcludeKeywords: getElementValue(elements.magnetExcludeKeywords).trim(),
        minReleaseDate: parseMinReleaseDate(getElementValue(elements.minReleaseDate)),
        actressCountFilterThreshold: normalizeNonNegativeInteger(getElementValue(elements.actressCountFilterThreshold), 0),
        filmCodeFilterThreshold: getElementValue(elements.filmCodeFilterThreshold).trim(),
        taskTemplate: getElementValue(elements.taskTemplate),
        cloudflare: getElementChecked(elements.cloudflare, true),
        secondValidation: true,
        nomag: getElementChecked(elements.nomag),
        allmag: getElementChecked(elements.allmag),
        magnetContentValidation: getElementChecked(elements.magnetContentValidation),
        metadataOnly: getElementChecked(elements.metadataOnly),
        nopic: getElementChecked(elements.nopic)
      };
    }

    function validateSettings(settings) {
      if (!settings.base) {
        throw new Error(UI_TEXT.validation.baseRequired);
      }

      if (!settings.output) {
        throw new Error(UI_TEXT.validation.outputRequired);
      }

      if (Number.isNaN(settings.itemsPerPage) || settings.itemsPerPage < 1) {
        throw new Error(UI_TEXT.validation.itemsPerPageInvalid);
      }

      if (Number.isNaN(settings.parallel) || settings.parallel < 1) {
        throw new Error(UI_TEXT.validation.parallelInvalid);
      }

      if (Number.isNaN(settings.totalPages) || settings.totalPages < 0) {
        throw new Error(UI_TEXT.validation.totalPagesInvalid);
      }

      if (Number.isNaN(settings.delay) || settings.delay < 0) {
        throw new Error(UI_TEXT.validation.delayInvalid);
      }

      if (Number.isNaN(settings.timeout) || settings.timeout < UI_TEXT.limits.minTimeout) {
        throw new Error(
          `${UI_TEXT.validation.timeoutInvalidPrefix}${UI_TEXT.limits.minTimeout}${UI_TEXT.validation.timeoutInvalidSuffix}`
        );
      }
    }

    function isPreflightErrorMessage(message) {
      const knownMessages = new Set([
        UI_TEXT.validation.baseRequired,
        UI_TEXT.validation.outputRequired,
        UI_TEXT.validation.itemsPerPageInvalid,
        UI_TEXT.validation.parallelInvalid,
        UI_TEXT.validation.totalPagesInvalid,
        UI_TEXT.validation.delayInvalid,
        UI_TEXT.validation.proxyInvalid,
        UI_TEXT.validation.magnetExcludeKeywordsInvalid
      ]);

      if (knownMessages.has(message)) {
        return true;
      }

      return (
        typeof message === 'string' &&
        message.startsWith(UI_TEXT.validation.timeoutInvalidPrefix) &&
        message.endsWith(UI_TEXT.validation.timeoutInvalidSuffix)
      );
    }

    function refreshSuggestedPages() {
      const limit = Number(elements.limit.value || 0);
      const totalPages = Number(elements.totalPages.value || 0);
      const itemsPerPage = getItemsPerPage();
      const suggestion = getSuggestedPages(limit, itemsPerPage);

      if (!suggestion) {
        elements.totalPagesAdvice.textContent = UI_TEXT.advice.defaultPrimary;
        elements.totalPagesMeta.textContent = `${UI_TEXT.advice.defaultSecondaryPrefix}${itemsPerPage}${UI_TEXT.advice.defaultSecondarySuffix}`;
        elements.useSuggestedPagesButton.disabled = true;
        return;
      }

      elements.totalPagesAdvice.textContent = `${UI_TEXT.advice.suggestedPagesPrefix}${suggestion.pages}${UI_TEXT.advice.suggestedPagesSuffix}`;
      elements.totalPagesMeta.textContent =
        `${UI_TEXT.advice.lastPageEstimatePrefix}${itemsPerPage}${UI_TEXT.advice.lastPageEstimateMiddle}${suggestion.lastPageCount}${UI_TEXT.advice.lastPageEstimateSuffix}` +
        (totalPages > 0
          ? ` ${UI_TEXT.advice.manualPagesPrefix}${totalPages}${UI_TEXT.advice.manualPagesSuffix}`
          : '');
      elements.useSuggestedPagesButton.disabled = false;
    }

    async function prepareCrawlSettings() {
      const settings = getSettings();
      validateSettings(settings);
      settings.magnetExcludeKeywords = await ensureMagnetExcludeKeywordsReady(settings.magnetExcludeKeywords);
      await ensureProxyReady(settings.proxy);
      return settings;
    }

    async function refreshAntiBlockBeforeCrawl(settings) {
      try {
        appendFormLog('info', UI_TEXT.messages.antiBlockUpdating);
        const result = await desktopApi.updateAntiBlock(settings);
        const count = Array.isArray(result && result.antiBlockUrls) ? result.antiBlockUrls.length : 0;
        appendFormLog(
          'info',
          `${UI_TEXT.messages.antiBlockUpdatedPrefix}${count}${UI_TEXT.messages.antiBlockUpdatedSuffix}${(result && result.filePath) || '已保存到默认位置'}`
        );
      } catch (error) {
        // Refresh failure should keep the saved fallback addresses available so
        // the requested crawl can still start when the network is unstable.
        appendFormLog('warn', `${UI_TEXT.messages.antiBlockUpdateFailedPrefix}${getErrorMessage(error)}`);
      }
    }

    async function prepareCrawlWithAntiBlock() {
      const settings = await prepareCrawlSettings();
      // Starting an update crawl should always take the recovery path first:
      // keep Cloudflare enabled for the current run and refresh anti-block
      // URLs before the actual crawl dispatch. This keeps the update flow
      // consistent even when the user forgot to toggle either control.
      settings.cloudflare = true;
      if (elements.cloudflare && !elements.cloudflare.checked) {
        elements.cloudflare.checked = true;
        persistCrawlerDraft();
      }

      await refreshAntiBlockBeforeCrawl(settings);
      return settings;
    }

    // Bridge crawl start is the only supported way for other workspaces to
    // reuse crawler runtime dispatch without copying setup validation logic.
    // Callers may override only a narrow subset of crawl settings.
    function buildBridgeCrawlSettings(overrides = {}) {
      const settings = getSettings();

      if (Object.prototype.hasOwnProperty.call(overrides, 'base')) {
        settings.base = String(overrides.base || '').trim();
      }
      if (Object.prototype.hasOwnProperty.call(overrides, 'output')) {
        settings.output = String(overrides.output || '').trim();
      }
      if (Object.prototype.hasOwnProperty.call(overrides, 'limit')) {
        settings.limit = normalizeNonNegativeInteger(overrides.limit, 0);
      }
      if (Object.prototype.hasOwnProperty.call(overrides, 'totalPages')) {
        settings.totalPages = normalizeNonNegativeInteger(overrides.totalPages, settings.totalPages);
      }

      return settings;
    }

    async function prepareBridgeCrawlSettings(overrides = {}) {
      const settings = buildBridgeCrawlSettings(overrides);
      validateSettings(settings);
      settings.magnetExcludeKeywords = await ensureMagnetExcludeKeywordsReady(settings.magnetExcludeKeywords);
      await ensureProxyReady(settings.proxy);
      await refreshAntiBlockBeforeCrawl(settings);
      return settings;
    }

    async function dispatchStartCrawl(settingsPromise, logMessage) {
      const settings = await settingsPromise;
      const nextLogMessage = String(logMessage || UI_TEXT.messages.startRunning).trim() || UI_TEXT.messages.startRunning;
      stateController.setStatus('starting', nextLogMessage);
      appendFormLog('info', nextLogMessage);
      await desktopApi.startCrawl(settings);
      clearCrawlerDraft();
      return settings;
    }

    async function startBridgeCrawl(overrides = {}, options = {}) {
      const sourceLabel = String(options && options.sourceLabel ? options.sourceLabel : '模块桥接').trim();
      const logMessage = sourceLabel ? `${sourceLabel}：已提交抓取任务` : UI_TEXT.messages.startRunning;
      return dispatchStartCrawl(prepareBridgeCrawlSettings(overrides), logMessage);
    }

    function bindBaseUrlChips() {
      elements.baseUrlHints.addEventListener('click', (event) => {
        const chip = event.target.closest('.base-url-chip');
        if (!chip) {
          return;
        }

        const url = chip.dataset.url || '';
        if (!url) {
          return;
        }

        if (elements.base.value !== url) {
          elements.base.value = url;
          elements.base.dispatchEvent(new Event('input', { bubbles: true }));
          elements.base.dispatchEvent(new Event('change', { bubbles: true }));
        }
      });
    }

    function getLookupContext() {
      return {
        preferredBase: elements.base.value.trim(),
        magnetOnly: elements.nomag.checked,
        // Actor directory and ranking calls must use the proxy currently
        // visible in the form, even before the user saves a crawler draft.
        proxy: elements.proxy.value.trim()
      };
    }

    function applyActressLookupResultNow(result = {}, options = {}) {
      const fillCount =
        Number.isFinite(result.fillCount) && result.fillCount >= 0 ? result.fillCount : result.preferredCount;

      if (result.resolvedBase) {
        elements.base.value = String(result.resolvedBase).trim();
      }

      if (Number.isFinite(result.itemsPerPage) && result.itemsPerPage > 0) {
        elements.itemsPerPage.value = String(result.itemsPerPage);
      }

      if (Number.isFinite(fillCount) && fillCount >= 0) {
        elements.limit.value = String(fillCount);
      }

      if (Number.isFinite(result.totalPages) && result.totalPages >= 0) {
        elements.totalPages.value = String(result.totalPages);
      }

      // Actor-atlas handoff owns its deterministic output choice. Other
      // ranking/crawler callers intentionally omit this option and therefore
      // keep the user's current output directory unchanged.
      if (Object.prototype.hasOwnProperty.call(options, 'output') && elements.output) {
        elements.output.value = String(options.output || '').trim();
      }

      refreshSuggestedPages();
      persistCrawlerDraft();
    }

    function applyActressLookupResult(result = {}, options = {}) {
      if (!bootstrapCompleted || hydratingFormState) {
        pendingActressLookupPrefill = { result, options };
        return;
      }
      applyActressLookupResultNow(result, options);
    }

    function queueActressLookupResult(result = {}, options = {}) {
      // Actor Atlas queues before shell navigation. This makes the handoff
      // deterministic even though workspace switching triggers an async form
      // bootstrap that restores the user's older draft first.
      pendingActressLookupPrefill = { result, options };
      if (bootstrapCompleted && !hydratingFormState) {
        const prefill = pendingActressLookupPrefill;
        pendingActressLookupPrefill = null;
        applyActressLookupResultNow(prefill.result, prefill.options);
      }
    }

    function appendFormLog(level, message, timestamp = new Date().toISOString()) {
      logController.appendLog(level, message, timestamp);
    }

    function bindAsyncClick(button, handler, onError) {
      if (typeof bindAsyncClickHelper === 'function') {
        bindAsyncClickHelper(button, handler, {
          onError,
          fallbackErrorHandler: (error) => appendFormLog('error', getErrorMessage(error))
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
          appendFormLog('error', getErrorMessage(error));
        }
      });
    }

    function bindEvents() {
      // 所有爬虫设置页交互都从这里进入：
      // 1) 字段归一化与建议值
      // 2) 启停/重启按钮
      // 3) 输出路径、背景图、结果入口等便捷操作
      //
      // 当问题表现为“点了按钮没反应”或“输入一改 UI 就乱”，优先查这里。
      if (eventsBound) {
        return;
      }

      eventsBound = true;
      elements.limit.addEventListener('input', refreshSuggestedPages);
      elements.limit.addEventListener('input', persistCrawlerDraft);
      elements.totalPages.addEventListener('input', refreshSuggestedPages);
      elements.totalPages.addEventListener('input', persistCrawlerDraft);
      elements.itemsPerPage.addEventListener('input', refreshSuggestedPages);
      elements.itemsPerPage.addEventListener('input', persistCrawlerDraft);
      elements.actressCountFilterThreshold.addEventListener('input', normalizeActressThresholdField);
      elements.actressCountFilterThreshold.addEventListener('blur', normalizeActressThresholdField);
      elements.actressCountFilterThreshold.addEventListener('input', persistCrawlerDraft);
      elements.actressCountFilterThreshold.addEventListener('blur', persistCrawlerDraft);
      elements.filmCodeFilterThreshold.addEventListener('blur', normalizeFilmCodeFilterField);
      elements.filmCodeFilterThreshold.addEventListener('input', persistCrawlerDraft);
      elements.filmCodeFilterThreshold.addEventListener('blur', persistCrawlerDraft);
      if (elements.minReleaseDate) {
        elements.minReleaseDate.addEventListener('blur', normalizeMinReleaseDateField);
        elements.minReleaseDate.addEventListener('input', persistCrawlerDraft);
        elements.minReleaseDate.addEventListener('blur', persistCrawlerDraft);
      }
      elements.base.addEventListener('input', () => {
        refreshSuggestedPages();
        persistCrawlerDraft();
        if (elements.proxy.value.trim()) {
          scheduleProxyValidation(300);
        }
        scheduleProxyAutoValidation();
      });
      elements.proxy.addEventListener('input', () => {
        persistCrawlerDraft();
        scheduleProxyValidation();
        scheduleProxyAutoValidation();
      });
      elements.proxy.addEventListener('blur', () => {
        if (!elements.proxy.value.trim()) {
          setProxyStatus('empty');
          scheduleProxyAutoValidation();
          return;
        }

        void validateProxyValue(elements.proxy.value.trim());
        scheduleProxyAutoValidation();
      });

      elements.taskTemplate.addEventListener('change', () => {
        applyTemplate(elements.taskTemplate.value);
        appendFormLog(
          'info',
          `${UI_TEXT.messages.templateAppliedPrefix}${TASK_TEMPLATES[elements.taskTemplate.value]?.label || TASK_TEMPLATES.balanced.label}`
        );
      });

      elements.useSuggestedPagesButton.addEventListener('click', () => {
        const suggestion = getSuggestedPages(Number(elements.limit.value || 0), getItemsPerPage());
        if (!suggestion) {
          return;
        }

        elements.totalPages.value = String(suggestion.pages);
        refreshSuggestedPages();
        persistCrawlerDraft();
        appendFormLog(
          'info',
          `${UI_TEXT.messages.suggestedPagesAppliedPrefix}${suggestion.pages}${UI_TEXT.messages.suggestedPagesAppliedSuffix}`
        );
      });

      [
        elements.parallel,
        elements.delay,
        elements.timeout,
        elements.magnetExcludeKeywords,
        elements.output
      ].forEach((element) => {
        if (!element) {
          return;
        }
        element.addEventListener('input', persistCrawlerDraft);
      });

      [
        elements.cloudflare,
        elements.nomag,
        elements.allmag,
        elements.magnetContentValidation,
        elements.metadataOnly,
        elements.nopic
      ].forEach((element) => {
        if (!element) {
          return;
        }
        element.addEventListener('change', persistCrawlerDraft);
      });

      bindAsyncClick(
        elements.startButton,
        async () => {
          await dispatchStartCrawl(prepareCrawlWithAntiBlock(), UI_TEXT.messages.startRunning);
        },
        (error) => {
          const message = getErrorMessage(error);
          appendFormLog('error', message);
          stateController.setStatus(isPreflightErrorMessage(message) ? 'idle' : 'error', message);
        }
      );

      bindAsyncClick(
        elements.restartButton,
        async () => {
          const settings = await prepareCrawlWithAntiBlock();
          stateController.setStatus('starting', UI_TEXT.messages.restartRunning);
          appendFormLog('warn', UI_TEXT.messages.restartRunning);
          const result = await desktopApi.restartCrawl(settings);
          clearCrawlerDraft();

          if (result && result.restarting) {
            appendFormLog('info', UI_TEXT.messages.restartQueued);
          } else {
            appendFormLog('info', UI_TEXT.messages.restartStarted);
          }
        },
        (error) => {
          const message = getErrorMessage(error);
          appendFormLog('error', message);
          stateController.setStatus(isPreflightErrorMessage(message) ? 'idle' : 'error', message);
        }
      );

      bindAsyncClick(
        elements.stopButton,
        async () => {
          if (elements.stopButton.disabled) {
            return;
          }

          elements.stopButton.disabled = true;
          stateController.setStatus('stopping', UI_TEXT.messages.stopRequested);
          appendFormLog('warn', UI_TEXT.messages.stopRequested);
          await desktopApi.stopCrawl();
        },
        (error) => {
          appendFormLog('error', getErrorMessage(error));
        }
      );

      bindAsyncClick(elements.browseOutputButton, async () => {
        const selected = await desktopApi.chooseOutput();
        if (selected) {
          elements.output.value = selected;
          persistCrawlerDraft();
          appendFormLog('info', `${UI_TEXT.messages.outputSelectedPrefix}${selected}`);
        }
      });

      bindAsyncClick(elements.chooseBackgroundButton, async () => {
          const nextSettings = await desktopApi.chooseBackgroundImage();
          if (!nextSettings) {
            return;
          }

          applyBackgroundImage(nextSettings.backgroundImageUrl);
          if (nextSettings.backgroundImage) {
            appendFormLog('info', `${UI_TEXT.messages.backgroundSelectedPrefix}${nextSettings.backgroundImage}`);
          }
      });

      bindAsyncClick(elements.resetBackgroundButton, async () => {
        const nextSettings = await desktopApi.clearBackgroundImage();
        applyBackgroundImage(nextSettings && nextSettings.backgroundImageUrl);
        appendFormLog('info', UI_TEXT.messages.backgroundReset);
      });

      bindAsyncClick(elements.openLogFolderButton, async () => {
        const opened = await desktopApi.openLogFolder();
        if (opened) {
          appendFormLog('info', `${UI_TEXT.messages.logFolderOpenedPrefix}${opened}`);
        }
      });

      bindAsyncClick(elements.openLogOutputButton, async () => {
        const opened = await desktopApi.openOutputDir(elements.output.value.trim());
        if (opened) {
          appendFormLog('info', `${UI_TEXT.messages.outputOpenedPrefix}${opened}`);
        }
      });

      bindAsyncClick(
        elements.openLogMagnetButton,
        async () => {
          const opened = await desktopApi.openMagnetFile(elements.output.value.trim());
          if (opened) {
            appendFormLog('info', `${UI_TEXT.messages.magnetOpenedPrefix}${opened}`);
          }
        },
        (error) => {
          appendFormLog('warn', getErrorMessage(error));
        }
      );


      elements.clearLogButton.addEventListener('click', () => {
        logController.clearLogView();
        appendFormLog('info', UI_TEXT.log.cleared);
      });
    }

    async function bootstrap() {
      if (bootstrapCompleted) {
        return;
      }

      // bootstrap 只负责把“已保存设置 + 当前运行上下文 + 当前日志上下文”
      // 折叠为页面初始状态，不负责解释运行期事件。
      const initialSettings = await desktopApi.getSettings();
      const draftSettings = loadCrawlerDraftSnapshot();
      const initialRunContext =
        platformBridge && typeof platformBridge.invokeOptionalQuery === 'function'
          ? await platformBridge.invokeOptionalQuery(
              typeof desktopApi.getCrawlRunContext === 'function' ? () => desktopApi.getCrawlRunContext() : null
            )
          : typeof desktopApi.getCrawlRunContext === 'function'
            ? await desktopApi.getCrawlRunContext().catch(() => null)
            : null;
      const initialLogContext = initialRunContext || (await desktopApi.getLogContext());
      const templateKey = initialSettings.taskTemplate || 'balanced';
      hydratingFormState = true;

      if (TASK_TEMPLATES[templateKey]) {
        applyTemplate(templateKey, { keepLimit: true, keepBase: true, keepOutput: true });
      } else {
        elements.taskTemplate.value = 'balanced';
        elements.itemsPerPage.value = String(initialSettings.itemsPerPage || UI_TEXT.limits.defaultItemsPerPage);
        elements.parallel.value = String(initialSettings.parallel || TASK_TEMPLATES.balanced.parallel);
        elements.delay.value = String(initialSettings.delay || TASK_TEMPLATES.balanced.delay);
        elements.timeout.value = String(initialSettings.timeout || TASK_TEMPLATES.balanced.timeout);
        elements.cloudflare.checked = Boolean(initialSettings.cloudflare ?? true);
        if (elements.secondValidation) elements.secondValidation.checked = true;
      }

      elements.base.value = initialSettings.base || '';
      elements.output.value =
        initialSettings.output ||
        String(initialRunContext && initialRunContext.preferredOutputDir ? initialRunContext.preferredOutputDir : '');
      elements.limit.value = String(initialSettings.limit || 0);
      elements.totalPages.value = String(initialSettings.totalPages || 0);
      elements.proxy.value = initialSettings.proxy || '';
      elements.magnetExcludeKeywords.value = initialSettings.magnetExcludeKeywords || '';
      elements.minReleaseDate.value = String(initialSettings.minReleaseDate || '').trim();
      elements.actressCountFilterThreshold.value = String(initialSettings.actressCountFilterThreshold || 0);
      elements.filmCodeFilterThreshold.value = String(initialSettings.filmCodeFilterThreshold || '').trim();
      elements.nomag.checked = Boolean(initialSettings.nomag);
      elements.allmag.checked = Boolean(initialSettings.allmag);
      elements.magnetContentValidation.checked = Boolean(initialSettings.magnetContentValidation);
      if (elements.metadataOnly) {
        elements.metadataOnly.checked = Boolean(initialSettings.metadataOnly);
      }
      elements.nopic.checked = Boolean(initialSettings.nopic);
      applyCrawlerDraftSnapshot(draftSettings);
      hydratingFormState = false;

      bindBaseUrlChips();
      bindEvents();
      applyBackgroundImage(initialSettings.backgroundImageUrl);
      logController.updateLogContext(initialLogContext);
      refreshSuggestedPages();
      bootstrapCompleted = true;
      if (pendingActressLookupPrefill) {
        const prefill = pendingActressLookupPrefill;
        pendingActressLookupPrefill = null;
        applyActressLookupResultNow(prefill.result, prefill.options);
        appendFormLog('info', '已应用演员图鉴预填：地址、数量、页数和输出目录已更新。');
      }
      if (elements.proxy.value.trim()) {
        await validateProxyValue(elements.proxy.value.trim());
      } else {
        setProxyStatus('empty');
      }
      scheduleProxyAutoValidation();
      stateController.setStatus('idle', UI_TEXT.state.defaultMessage);
      appendFormLog('info', UI_TEXT.state.ready);
    }

    function dispose() {
      // 审计 H-07：工作区切换或页面卸载时停止代理自动验证，避免定时器泄漏。
      stopProxyAutoValidation();
      eventsBound = false;
      bootstrapCompleted = false;
    }

    return {
      bootstrap,
      dispose,
      getLookupContext,
      applyActressLookupResult,
      queueActressLookupResult,
      startBridgeCrawl
    };
  }

  globalScope.desktopFormController = {
    createFormController
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
