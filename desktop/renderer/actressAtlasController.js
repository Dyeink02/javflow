// Actor Atlas view controller.
//
// This controller owns only the Actor Atlas view state. It talks to explicit
// bridge routes for rankings, profile lookup, media caching, and work paging;
// it does not reach into crawler, organizer, subscription, or scraper state.
//
// Ownership summary:
// 1) maintain the Actor Atlas ranking, detail and work-page view state
// 2) route explicit user actions through the desktop bridge
// 3) keep actor browsing independent from every execution workflow
//
// File map for maintainers:
// 1) common UI, proxy and ranking helpers
// 2) profile/media rendering and lazy JAVBus work paging
// 3) event binding and public controller lifecycle
(function initializeActressAtlasController(globalScope) {
  const CACHE_PREFIX = 'javflow.actress-atlas.v3';
  const PROXY_KEY = `${CACHE_PREFIX}.proxy`;
  // Keep the work grid readable while showing two additional works per page.
  // Cover downloads remain separately limited to three concurrent requests.
  const UI_PAGE_SIZE = 8;
  const SOURCE_PAGE_SIZE = 30;

  function createActressAtlasController(options) {
    const { elements, desktopApi, formController, switchWorkspace } = options || {};
    const mediaViewer = globalScope.desktopActressAtlasMediaViewer;
    const viewFactory = globalScope.desktopActressAtlasView;
    if (!mediaViewer || !viewFactory) {
      throw new Error('演员图鉴视图模块未加载。');
    }
    const atlasView = viewFactory.createActressAtlasView({ elements, openMedia: mediaViewer.open });
    let ranking = { items: [], availableYears: [], availableMonths: [], mode: 'monthly' };
    let selected = null;
    let works = [];
    let workTotal = 0;
    let workPage = 0;
    let loadedSourcePages = new Set();
    let rankingRequest = 0;
    let detailRequest = 0;
    let proxyValue = '';
    const proxyValidationState = { value: '', status: 'empty' };
    let bootstrapped = false;
    let eventsBound = false;
    let logs = [];

    const nodeText = (node, value) => {
      if (node) node.textContent = value == null ? '' : String(value);
    };
    const inputValue = (node) => String(node && node.value || '').trim();
    const setEnabled = (node, enabled) => {
      if (node) node.disabled = !enabled;
    };
    const normalizedName = (value) => String(value || '')
      .toLowerCase()
      .replace(/[\s()（）\[\]【】·・]/g, '');
    const canonicalMediaKey = (value) => {
      try {
        const parsed = new URL(String(value || '').trim(), globalScope.location && globalScope.location.href);
        parsed.search = '';
        parsed.hash = '';
        return parsed.href.toLowerCase();
      } catch (_) {
        return String(value || '').trim().toLowerCase();
      }
    };
    const readStorage = (key, fallback) => {
      try {
        const raw = globalScope.localStorage && globalScope.localStorage.getItem(key);
        return raw ? JSON.parse(raw) : fallback;
      } catch (_) {
        return fallback;
      }
    };
    const writeStorage = (key, value) => {
      try {
        if (globalScope.localStorage) globalScope.localStorage.setItem(key, JSON.stringify(value));
      } catch (_) {
        // Storage is a convenience only. The persisted app setting remains the
        // source of truth for the next desktop launch.
      }
    };
    const lookupOptions = (extra) => ({ proxy: proxyValue, ...(extra || {}) });

    function appendLog(message, level = 'info') {
      logs = [...logs, {
        time: new Date().toLocaleTimeString('zh-CN', { hour12: false }),
        message: String(message),
        level
      }].slice(-80);

      if (!elements.atlasActivityLog) return;
      elements.atlasActivityLog.replaceChildren();
      logs.forEach((entry) => {
        const row = document.createElement('li');
        row.className = `atlas-activity-log-entry${entry.level === 'error' ? ' is-error' : entry.level === 'success' ? ' is-success' : entry.level === 'warn' ? ' is-warn' : ''}`;
        const time = document.createElement('time');
        time.className = 'atlas-activity-log-time';
        time.textContent = entry.time;
        const text = document.createElement('span');
        text.textContent = entry.message;
        row.append(time, text);
        elements.atlasActivityLog.appendChild(row);
      });
      elements.atlasActivityLog.scrollTop = elements.atlasActivityLog.scrollHeight;
    }

    function setProxyStatus(status, detail = '') {
      const normalized = ['checking', 'valid', 'global', 'invalid'].includes(status) ? status : 'empty';
      const label = {
        empty: '未检测',
        checking: '检测中...',
        valid: '代理正常',
        global: '已连接',
        invalid: '代理失败'
      }[normalized];
      const defaultDetail = {
        empty: '请先在 JAV 爬虫中填写代理地址，或在此填写后点击应用。',
        checking: '正在检测演员资料与榜单的代理连通性。',
        valid: '检测通过，榜单和演员资料会使用当前代理。',
        global: `已连接：${proxyValue}。此处与 JAV 爬虫共用同一保存的代理设置。`,
        invalid: '当前代理不可用，请检查地址或代理软件状态。'
      }[normalized];
      if (elements.atlasProxyStatus) {
        elements.atlasProxyStatus.className = `proxy-status-chip ${normalized}`;
        elements.atlasProxyStatus.textContent = label;
      }
      nodeText(elements.atlasProxyStatusDetail, detail || defaultDetail);
    }

    function setNodeStatus(status, result = {}) {
      const normalized = ['checking', 'japan', 'foreign', 'invalid'].includes(status) ? status : 'empty';
      const chipState = { empty: 'empty', checking: 'checking', japan: 'valid', foreign: 'foreign', invalid: 'invalid' }[normalized];
      const country = String(result.country || result.countryCode || '').trim();
      const city = String(result.city || '').trim();
      const label = normalized === 'japan'
        ? (city ? `日本 · ${city}` : '日本节点')
        : normalized === 'foreign'
          ? `非日本 · ${country || '未知地区'}`
          : { empty: '节点未检测', checking: '检测中...', invalid: '检测失败' }[normalized];
      if (elements.atlasNodeStatus) {
        elements.atlasNodeStatus.className = `proxy-status-chip ${chipState}`;
        elements.atlasNodeStatus.textContent = label;
        elements.atlasNodeStatus.title = String(result.detail || '检测当前出口 IP 和地区，点击重新检测');
      }
    }

    let nodeCheckRunning = false;
    async function checkNodeRegion(trigger = '启动') {
      if (!desktopApi || typeof desktopApi.checkProxyRegion !== 'function' || nodeCheckRunning) return;
      nodeCheckRunning = true;
      setNodeStatus('checking');
      appendLog(`节点检测（${trigger}）：正在确认出口 IP 和地区...`);
      try {
        const result = await desktopApi.checkProxyRegion({ proxyValue });
        const status = String(result && result.status || 'invalid');
        if (status === 'japan') {
          setNodeStatus('japan', result);
          appendLog(`节点检测完成：${result.city ? `日本 · ${result.city}` : '日本节点'}${result.ip ? `（${result.ip}）` : ''}。`, 'success');
        } else if (status === 'foreign') {
          setNodeStatus('foreign', result);
          appendLog(`节点检测完成：${result.country || result.countryCode || '非日本地区'}${result.city ? ` · ${result.city}` : ''}${result.ip ? `（${result.ip}）` : ''}。`, 'warn');
        } else {
          setNodeStatus('invalid', result || {});
          appendLog(`节点检测失败：${result && (result.detail || result.message) || '出口 IP 检测服务不可用。'}`, 'error');
        }
      } catch (error) {
        setNodeStatus('invalid', {});
        appendLog(`节点检测失败：${error && error.message ? error.message : error}`, 'error');
      } finally {
        nodeCheckRunning = false;
      }
    }

    async function loadProxy() {
      let saved = String(readStorage(PROXY_KEY, '') || '').trim();
      try {
        const settings = await desktopApi.getSettings();
        // An explicitly saved empty value clears stale local Atlas state and
        // leaves the user in direct/system-proxy mode.
        if (settings && Object.prototype.hasOwnProperty.call(settings, 'proxy')) {
          saved = String(settings.proxy || '').trim();
        }
      } catch (_) {
        // Retain the last local value until the settings bridge becomes ready.
      }
      proxyValue = saved;
      if (elements.atlasProxy) elements.atlasProxy.value = saved;
      if (!saved) {
        setProxyStatus('empty');
        proxyValidationState.value = '';
        proxyValidationState.status = 'empty';
        void checkNodeRegion('启动');
        return { status: 'empty' };
      }
      // Settings are restored before the first ranking request so a new launch
      // reflects the real connection state instead of pretending the saved URL
      // is usable.
      const validation = await validateProxy(saved, true);
      void checkNodeRegion('启动');
      return validation;
    }

    async function saveProxy() {
      proxyValue = inputValue(elements.atlasProxy);
      writeStorage(PROXY_KEY, proxyValue);
      try {
        const saveGlobalProxy = desktopApi && (desktopApi.saveGlobalProxy || desktopApi.saveActressAtlasProxy);
        if (typeof saveGlobalProxy === 'function') {
          await saveGlobalProxy({ proxy: proxyValue });
        }
      } catch (_) {
        // Ranking lookup still carries the explicit value in this request.
      }
      await validateProxy(proxyValue, true);
      void checkNodeRegion('应用代理');
      await loadRankings(true);
    }

    async function validateProxy(value, usingGlobalProxy = false) {
      const proxy = String(value || '').trim();
      proxyValidationState.value = proxy;
      if (!proxy) {
        proxyValidationState.status = 'empty';
        setProxyStatus('empty');
        return { status: 'empty' };
      }
      if (!desktopApi || typeof desktopApi.validateProxy !== 'function') {
        proxyValidationState.status = 'invalid';
        setProxyStatus('invalid', '当前版本未提供代理检测能力。');
        return { status: 'invalid' };
      }
      setProxyStatus('checking');
      try {
        const result = await desktopApi.validateProxy(proxy, { targetUrl: 'https://www.javbus.com/' });
        if (result && result.status === 'valid') {
          proxyValidationState.status = 'valid';
          setProxyStatus(usingGlobalProxy ? 'global' : 'valid', usingGlobalProxy ? '' : result.detail || '检测通过，榜单和演员资料会使用当前代理。');
          return result;
        }
        proxyValidationState.status = 'invalid';
        setProxyStatus('invalid', result && result.detail);
        return result || { status: 'invalid' };
      } catch (error) {
        const message = error && error.message ? error.message : String(error || '代理检测失败。');
        proxyValidationState.status = 'invalid';
        setProxyStatus('invalid', message);
        return { status: 'invalid', detail: message };
      }
    }

    // An empty proxy intentionally means direct/system-proxy mode. Only a
    // configured-but-invalid proxy blocks further Atlas network requests.
    // The region verdict is advisory: both japan and foreign nodes continue
    // through the same ranking, search, profile, and cover-loading paths.
    async function ensureAtlasProxyReady(actionLabel) {
      const proxy = String(proxyValue || '').trim();
      if (!proxy) {
        return true;
      }
      if (proxyValidationState.value === proxy && proxyValidationState.status === 'valid') {
        return true;
      }
      const result = await validateProxy(proxy, true);
      if (result && result.status === 'valid') {
        return true;
      }
      appendLog(`${String(actionLabel || '联网操作').trim()}已取消：当前填写的代理未通过检测。`, 'error');
      return false;
    }

    function rankingMode() {
      return elements.atlasModeAnnualButton && elements.atlasModeAnnualButton.classList.contains('active')
        ? 'annual'
        : 'monthly';
    }

    function rankingOptions(forceRefresh) {
      return lookupOptions({
        source: inputValue(elements.atlasSource) || 'smart',
        mode: rankingMode(),
        year: Number(inputValue(elements.atlasYear)) || 0,
        month: Number(inputValue(elements.atlasMonth)) || 0,
        forceRefresh: Boolean(forceRefresh)
      });
    }

    function populatePeriodSelect(node, values, selectedValue, formatter) {
      if (!node) return;
      const before = inputValue(node);
      const options = Array.from(new Set((values || []).map(Number).filter(Number.isFinite)))
        .sort((left, right) => right - left);
      node.replaceChildren();
      options.forEach((value) => {
        const option = document.createElement('option');
        option.value = String(value);
        option.textContent = formatter(value);
        node.appendChild(option);
      });
      const desired = String(selectedValue || before || options[0] || '');
      if (desired) node.value = desired;
    }

    function updatePeriodControls(data) {
      const annual = rankingMode() === 'annual';
      const periodRow = elements.atlasMonthField && elements.atlasMonthField.parentElement;
      if (periodRow) periodRow.classList.toggle('annual-mode', annual);
      populatePeriodSelect(elements.atlasYear, data.availableYears, data.periodYear, (year) => `${year} 年`);
      if (annual) {
        // Annual mode has no month dimension. Remove old monthly options as
        // well as the visual field so a stale month cannot leak into a request.
        if (elements.atlasMonth) {
          elements.atlasMonth.replaceChildren();
          elements.atlasMonth.value = '';
          elements.atlasMonth.disabled = true;
        }
        if (elements.atlasMonthField) elements.atlasMonthField.classList.add('hidden');
        return;
      }
      if (elements.atlasMonth) elements.atlasMonth.disabled = false;
      populatePeriodSelect(elements.atlasMonth, data.availableMonths, data.periodMonth, (month) => `${month} 月`);
      if (elements.atlasMonthField) elements.atlasMonthField.classList.remove('hidden');
    }

    // A monthly result has a different period set from annual rankings. Clear
    // those old selects before switching mode so the first annual request can
    // resolve the newest year that actually has rows instead of inheriting an
    // invalid monthly year such as the still-unpublished current annual list.
    function resetPeriodControlsForMode(mode) {
      const setLoadingOption = (node, label) => {
        if (!node) return;
        const option = document.createElement('option');
        option.value = '';
        option.textContent = label;
        node.replaceChildren(option);
        node.value = '';
      };
      setLoadingOption(elements.atlasYear, '正在读取…');
      const periodRow = elements.atlasMonthField && elements.atlasMonthField.parentElement;
      if (mode === 'annual') {
        if (periodRow) periodRow.classList.add('annual-mode');
        if (elements.atlasMonth) {
          elements.atlasMonth.replaceChildren();
          elements.atlasMonth.value = '';
          elements.atlasMonth.disabled = true;
        }
        if (elements.atlasMonthField) elements.atlasMonthField.classList.add('hidden');
        return;
      }
      if (periodRow) periodRow.classList.remove('annual-mode');
      if (elements.atlasMonth) elements.atlasMonth.disabled = false;
      setLoadingOption(elements.atlasMonth, '正在读取…');
      if (elements.atlasMonthField) elements.atlasMonthField.classList.remove('hidden');
    }

    function rankingItemIdentity(item) {
      return `${Number(item && item.rank) || 0}|${normalizedName(item && item.actressName)}`;
    }

    // Ranking portraits are optional UI enrichment. Cache them in the
    // background after the 100-row ranking itself is visible, then replace
    // remote URLs with same-origin media routes so WebView2 is not dependent on
    // hotlink behavior from the image provider.
    async function cacheRankingAvatars(result, request) {
      if (!desktopApi || typeof desktopApi.cacheActressRankingAvatars !== 'function' || !result || !Array.isArray(result.items)) return;
      const candidates = result.items.filter((item) => {
        const imageUrl = String(item && item.imageUrl || '');
        // 内联 data: URL（基线头像）已完成交付，不再进入缓存回写流程。
        if (/^data:/i.test(imageUrl)) return false;
        const source = String(item && (item.sourceImageUrl || item.imageUrl) || '').trim();
        return /^https?:\/\//i.test(source) && !/^\/subscription-media\//i.test(imageUrl);
      });
      if (!candidates.length) return;
      appendLog(`开始缓存榜单头像：${candidates.length} 张（并发 5）。`);
      try {
        const response = await desktopApi.cacheActressRankingAvatars({
          items: candidates,
          proxy: proxyValue,
          source: result.resolvedSource || result.sourceName || '',
          mode: result.mode || rankingMode(),
          periodYear: result.periodYear || 0,
          periodMonth: result.periodMonth || 0
        });
        if (request !== rankingRequest || !response || !Array.isArray(response.items)) return;
        const updates = new Map(response.items.map((item) => [rankingItemIdentity(item), item]));
        ranking = {
          ...ranking,
          items: ranking.items.map((item) => {
            const update = updates.get(rankingItemIdentity(item));
            if (!update) return item;
            const hasLocalImage = /^\/subscription-media\//i.test(String(update.imageUrl || ''));
            return { ...item, ...update, avatarCacheFailed: !hasLocalImage };
          })
        };
        atlasView.renderRanking(ranking, (item) => void selectActor(item));
        const cached = Number(response.cached) || 0;
        const failed = Number(response.failed) || 0;
        appendLog(`榜单头像缓存完成：成功 ${cached} 张，${failed ? `失败 ${failed} 张，失败项已显示占位。` : '全部可用'}。`, failed ? 'warn' : 'success');
      } catch (error) {
        if (request !== rankingRequest) return;
        appendLog(`榜单头像缓存失败：${error && error.message ? error.message : error}。已保留失败占位。`, 'error');
      }
    }

    // A failed refresh never clears the existing ranking. This keeps the
    // bundled or last successful cache useful when a source is rate-limited.
    let historyBackfillRunning = false;
    async function backfillHistory() {
      if (historyBackfillRunning) return;
      if (!desktopApi || typeof desktopApi.backfillRankingHistory !== 'function') {
        appendLog('回填功能尚未就绪，请更新软件后重试。', 'error');
        return;
      }
      historyBackfillRunning = true;
      if (elements.atlasHistoryBackfill) {
        elements.atlasHistoryBackfill.disabled = true;
        elements.atlasHistoryBackfill.textContent = '正在回填近5年年榜...';
      }
      appendLog('开始回填近 5 年官方年榜（每年 100 位）与全部演员头像，预计 3-8 分钟，期间请保持代理开启...', 'info');
      try {
        const result = await desktopApi.backfillRankingHistory({
          proxy: String(proxyValue || '').trim(),
          yearsBack: 5
        });
        const years = result && Array.isArray(result.years) ? result.years : [];
        const yearText = years.map((item) => `${item.year}年${item.total}位`).join('、');
        appendLog(`年榜回填完成：${yearText || '全部年份已有缓存'}；头像缓存成功 ${result.avatarsCached || 0} 张、失败 ${result.avatarsFailed || 0} 张。`, 'success');
        await loadRankings(false);
      } catch (error) {
        appendLog(`年榜回填失败：${error instanceof Error ? error.message : String(error || '未知错误')}`, 'error');
      } finally {
        historyBackfillRunning = false;
        if (elements.atlasHistoryBackfill) {
          elements.atlasHistoryBackfill.disabled = false;
          elements.atlasHistoryBackfill.textContent = '缓存近5年年榜';
        }
      }
    }

    let historySaveRunning = false;
    async function saveCurrentMonthlyRanking() {
      if (historySaveRunning) return;
      if (rankingMode() !== 'monthly' || !ranking || !Array.isArray(ranking.items) || !ranking.items.length) {
        appendLog('当前没有可保存的完整月榜。', 'warn');
        return;
      }
      if (!desktopApi || typeof desktopApi.saveActressRankingHistory !== 'function') {
        appendLog('本地月榜保存功能尚未就绪，请更新软件后重试。', 'error');
        return;
      }
      historySaveRunning = true;
      if (elements.atlasHistorySave) {
        elements.atlasHistorySave.disabled = true;
        elements.atlasHistorySave.textContent = '正在保存月榜...';
      }
      try {
        const saved = await desktopApi.saveActressRankingHistory({ ranking });
        const period = saved && saved.periodLabel || ranking.periodLabel || `${ranking.periodYear}年${ranking.periodMonth}月`;
        if (saved && saved.alreadySaved) {
          appendLog(`${period} 已有本地记录，保留原记录而未覆盖。`, 'info');
        } else {
          appendLog(`${period} 已保存为本地月榜，以后官方不再提供时仍可查看。`, 'success');
        }
      } catch (error) {
        appendLog(`保存当前月榜失败：${error && error.message ? error.message : error}`, 'error');
      } finally {
        historySaveRunning = false;
        if (elements.atlasHistorySave) {
          elements.atlasHistorySave.disabled = rankingMode() !== 'monthly' || !ranking.items.length;
          elements.atlasHistorySave.textContent = '保存当前月榜';
        }
      }
    }

    function isCurrentCalendarMonth(year, month) {
      const now = new Date();
      return Number(year) === now.getFullYear() && Number(month) === now.getMonth() + 1;
    }

    async function loadRankings(forceRefresh = false) {
      if (!desktopApi || typeof desktopApi.getActressRankings !== 'function') return;
      const request = ++rankingRequest;
      // 历史月份榜单数据不会变化：一律走本地缓存秒开，绝不联网；
      // 只有当前日历月（数据仍在更新）才允许联网刷新。
      const selectedYear = Number(inputValue(elements.atlasYear)) || 0;
      const selectedMonth = Number(inputValue(elements.atlasMonth)) || 0;
      if (!isCurrentCalendarMonth(selectedYear, selectedMonth)) {
        forceRefresh = false;
      }
      if (forceRefresh) {
        nodeText(elements.atlasRankingNotice, '正在加载最新榜单，预加载内容仍保留。');
      } else {
        nodeText(elements.atlasRankingNotice, '');
      }
      if (!ranking.items.length && atlasView.renderRankingLoading) {
        atlasView.renderRankingLoading('正在加载榜单，请稍候...');
      }
      try {
        const next = await desktopApi.getActressRankings(rankingOptions(forceRefresh));
        if (request !== rankingRequest) return;
        if (!next || !Array.isArray(next.items) || !next.items.length) {
          nodeText(elements.atlasRankingNotice, '最新榜单没有返回内容，继续显示已有内容。');
          appendLog('榜单请求没有返回有效条目。', 'error');
          return;
        }
        ranking = next;
        updatePeriodControls(next);
        setEnabled(elements.atlasHistorySave, rankingMode() === 'monthly');
        atlasView.renderRanking(ranking, (item) => void selectActor(item));
        appendLog(`榜单已更新：${next.items.length} 位演员。`, 'success');
        void cacheRankingAvatars(next, request);
      } catch (error) {
        if (request !== rankingRequest) return;
        const message = error && error.message ? error.message : error;
        nodeText(elements.atlasRankingNotice, `榜单加载失败，继续显示已有内容：${message}`);
        appendLog(`榜单加载失败：${message}`, 'error');
        if (/日本地区|年龄验证|地区限制/.test(String(message))) {
          void checkNodeRegion('检测到地区限制');
        }
      }
    }

    function workIdentity(work) {
      return `${String(work && work.url || '').trim()}|${String(work && work.code || '').trim()}`;
    }

    function isLocalAtlasMedia(url) {
      return /^(?:data:|\/subscription-media\/)/i.test(String(url || '').trim());
    }

    function preferredCachedAvatar(...candidates) {
      const values = candidates.map((value) => String(value || '').trim()).filter(Boolean);
      return values.find(isLocalAtlasMedia) || values[0] || '';
    }

    function workPageCount() {
      return Math.max(1, Math.ceil(Math.max(workTotal, works.length) / UI_PAGE_SIZE));
    }

    function renderWorks() {
      const rendered = atlasView.renderWorks({
        works,
        workTotal,
        workPage,
        pageSize: UI_PAGE_SIZE,
        onOpenWork: (url) => desktopApi.openExternal(url),
        onRetryCover: (work) => void retryWorkCovers([work], '单张封面')
      });
      workPage = rendered.page;
    }

    // The UI displays eight works per page. Each batch stays within the Go
    // bridge's three-download limit; current-page caching always completes
    // before the N+1/N+2/N+3 background prefetch begins.
    async function cacheWorkCoverPages(token, firstPage, pageCount = 3, purpose = '预加载') {
      if (!desktopApi.cacheActressWorkCovers || token !== detailRequest) return;
      if (!(await ensureAtlasProxyReady(`${purpose}作品封面`))) return;
      const start = Math.max(0, Number(firstPage) || 0) * UI_PAGE_SIZE;
      const visible = works.slice(start, start + Math.max(1, pageCount) * UI_PAGE_SIZE);
      if (!visible.length) return;
      const lastPage = Math.floor((start + visible.length - 1) / UI_PAGE_SIZE);
      appendLog(`开始${purpose}作品封面：第 ${firstPage + 1}-${lastPage + 1} 页（并发 3）。`);
      try {
        const response = await desktopApi.cacheActressWorkCovers({
          actressName: selected && selected.actressName,
          works: visible,
          ...lookupOptions()
        });
        if (token !== detailRequest || !response || !Array.isArray(response.works)) return;
        const updates = new Map(response.works.map((work) => [workIdentity(work), work]));
        let cachedCount = 0;
        works = works.map((work) => {
          const updated = updates.get(workIdentity(work));
          if (!updated || !updated.coverUrl) return work;
          const cached = isLocalAtlasMedia(updated.coverUrl);
          if (cached) cachedCount += 1;
          return { ...work, ...updated, coverLoadFailed: cached ? false : work.coverLoadFailed };
        });
        renderWorks();
        appendLog(`${purpose}作品封面完成：第 ${firstPage + 1}-${lastPage + 1} 页，已缓存 ${cachedCount} 张。`, 'success');
      } catch (error) {
        appendLog(`${purpose}作品封面失败：${error && error.message ? error.message : error}`, 'error');
      }
    }

    async function cacheCurrentThenPrefetch(token, page) {
      await cacheWorkCoverPages(token, page, 1, '当前页');
      if (token !== detailRequest) return;
      await cacheWorkCoverPages(token, page + 1, 3, '预加载');
    }

    async function loadWorkPage(nextPage) {
      const pages = workPageCount();
      workPage = Math.max(0, Math.min(Number(nextPage) || 0, pages - 1));
      renderWorks();
      const sourcePage = Math.floor(workPage * UI_PAGE_SIZE / SOURCE_PAGE_SIZE) + 1;
      const token = detailRequest;
      if (!loadedSourcePages.has(sourcePage)) {
        if (!(await ensureAtlasProxyReady('加载作品'))) return;
        loadedSourcePages.add(sourcePage);
        try {
          const response = await desktopApi.loadActressWorksPage({
            actressName: selected && selected.actressName,
            targetUrl: selected && selected.lookupProfile && selected.lookupProfile.resolvedBase || selected && selected.profileUrl || '',
            page: sourcePage,
            ...lookupOptions()
          });
          if (token !== detailRequest) return;
          const known = new Set(works.map(workIdentity));
          (response.works || []).forEach((work) => {
            if (!known.has(workIdentity(work))) {
              known.add(workIdentity(work));
              works.push(work);
            }
          });
          workTotal = Math.max(workTotal, Number(response.allCount) || 0);
          atlasView.renderStats(selected, selected && selected.lookupProfile || {}, works.length);
          renderWorks();
          appendLog(`已加载作品源页 ${sourcePage}。`, 'success');
        } catch (error) {
          loadedSourcePages.delete(sourcePage);
          appendLog(`作品第 ${sourcePage} 源页加载失败：${error && error.message ? error.message : error}`, 'error');
        }
      }
      void cacheCurrentThenPrefetch(token, workPage);
    }

    async function retryWorkCovers(candidates, label) {
      if (!desktopApi.cacheActressWorkCovers || !selected) return;
      if (!(await ensureAtlasProxyReady(`重新加载${label || '作品封面'}`))) return;
      const token = detailRequest;
      const retryWorks = (candidates || [])
        .filter((work) => work && workIdentity(work) && work.coverLoadFailed === true && work.coverRetrying !== true)
        .slice(0, UI_PAGE_SIZE);
      if (!retryWorks.length) {
        appendLog('当前页没有加载失败的封面，无需重新刷新。');
        return;
      }
      const retryKeys = new Set(retryWorks.map(workIdentity));
      works = works.map((work) => retryKeys.has(workIdentity(work)) ? { ...work, coverRetrying: true } : work);
      renderWorks();
      appendLog(`开始重新刷新${label}：${retryWorks.length} 张失败封面（并发 3）。`);
      try {
        const response = await desktopApi.cacheActressWorkCovers({
          actressName: selected.actressName,
          works: retryWorks,
          ...lookupOptions()
        });
        if (token !== detailRequest || !response || !Array.isArray(response.works)) return;
        const updates = new Map(response.works.map((work) => [workIdentity(work), work]));
        let refreshed = 0;
        works = works.map((work) => {
          const updated = updates.get(workIdentity(work));
          if (!updated || !updated.coverUrl) {
            return retryKeys.has(workIdentity(work)) ? { ...work, coverRetrying: false } : work;
          }
          const refreshedLocally = isLocalAtlasMedia(updated.coverUrl);
          if (refreshedLocally) refreshed += 1;
          return {
            ...work,
            ...updated,
            coverLoadFailed: refreshedLocally ? false : true,
            coverRetrying: false
          };
        });
        renderWorks();
        appendLog(`${label}重新刷新完成：成功 ${refreshed} 张，仍失败 ${retryWorks.length - refreshed} 张。`, refreshed === retryWorks.length ? 'success' : 'warn');
      } catch (error) {
        works = works.map((work) => retryKeys.has(workIdentity(work)) ? { ...work, coverRetrying: false } : work);
        appendLog(`${label}重新刷新失败：${error && error.message ? error.message : error}`, 'error');
        renderWorks();
      }
    }

    function renderSelectedState(item, profile) {
      const rendered = atlasView.renderSelectedState({
        item,
        profile: profile || {},
        works,
        worksLength: works.length,
        workTotal,
        workPage,
        pageSize: UI_PAGE_SIZE,
        onOpenWork: (url) => desktopApi.openExternal(url),
        onRetryCover: (work) => void retryWorkCovers([work], '单张封面')
      });
      workPage = rendered.page;
    }

    function mergeResolvedMedia(item, profile, media) {
      if (!media || !Array.isArray(media.images) || !media.images.length) return profile;
      const current = profile || {};
      const images = media.images.filter(Boolean);
      const sourceImages = (Array.isArray(media.sourceImages) && media.sourceImages.length ? media.sourceImages : images).filter(Boolean);
      const sourceKeys = new Set(sourceImages.map(canonicalMediaKey).filter(Boolean));
      // The actor-directory portrait is already verified and locally cached by
      // the profile lookup. Metadata media supplements public photos only; it
      // must not replace an already-visible avatar with another request.
      const currentAvatar = current.avatarUrl || item.imageUrl || '';
      const filteredPromotions = (current.promotionImageUrls || []).filter((url) => {
        const key = canonicalMediaKey(url);
        return key && !sourceKeys.has(key) && key !== canonicalMediaKey(currentAvatar);
      });
      return {
        ...current,
        avatarUrl: currentAvatar || images[0] || '',
        promotionImageUrls: [...images.slice(1), ...filteredPromotions]
      };
    }

    async function selectActor(item) {
      selected = item || {};
      const token = ++detailRequest;
      const initialProfile = selected.lookupProfile || {};
      // A ranking avatar already delivered from data: or /subscription-media/
      // is ready to render. Keep that exact local asset throughout this detail
      // request instead of swapping it for a second cached avatar once profile
      // and cover requests complete.
      const sessionAvatar = preferredCachedAvatar(selected.imageUrl, initialProfile.avatarUrl);
      works = Array.isArray(initialProfile.works) ? initialProfile.works.slice() : [];
      workTotal = Number(initialProfile.allCount) || Number(selected.worksCount) || works.length;
      workPage = 0;
      loadedSourcePages = works.length ? new Set([1]) : new Set();
      renderSelectedState(selected, initialProfile);
      appendLog(`已选择演员：${selected.actressName || '未知演员'}。`);

      if (!(await ensureAtlasProxyReady('读取演员资料'))) {
        return;
      }

      // Lookup and optional media cache are independent. A profile response is
      // valid by itself; image-provider failure must never erase measurements
      // or the JAVBus work list.
      const [detailResult, mediaResult] = await Promise.allSettled([
        desktopApi.inspectActressTarget({
          actressName: selected.actressName,
          // Ranking rows already carry the source directory URL. Use it so a
          // colliding local alias (for example 森沢かな) cannot block a click
          // that already identifies the intended performer.
          targetUrl: initialProfile.resolvedBase || selected.profileUrl || '',
          ...lookupOptions({ includeProfile: true, cacheProfileMedia: true })
        }),
        desktopApi.resolveActressMedia({ actorName: selected.actressName, ...lookupOptions() })
      ]);
      if (token !== detailRequest || normalizedName(selected.actressName) !== normalizedName(item && item.actressName)) return;

      let profile = initialProfile;
      if (detailResult.status === 'fulfilled' && detailResult.value) {
        profile = detailResult.value;
        works = Array.isArray(profile.works) ? profile.works.slice() : works;
        workTotal = Number(profile.allCount) || workTotal;
        loadedSourcePages = works.length ? new Set([1]) : new Set();
        appendLog(`演员基础资料和作品已更新：已加载 ${works.length} / ${workTotal || works.length} 部。`, 'success');
      } else {
        appendLog(`演员资料读取失败：${detailResult.reason && detailResult.reason.message || detailResult.reason}`, 'error');
      }
      if (isLocalAtlasMedia(sessionAvatar)) {
        profile = { ...profile, avatarUrl: sessionAvatar };
      }
      if (mediaResult.status === 'fulfilled') {
        profile = mergeResolvedMedia(selected, profile, mediaResult.value);
        if (mediaResult.value && Array.isArray(mediaResult.value.images) && mediaResult.value.images.length) {
          appendLog(`头像与公开照片已更新：${mediaResult.value.images.length} 张。`, 'success');
        }
      }

      selected = { ...selected, lookupProfile: profile };
      renderSelectedState(selected, profile);
      void cacheCurrentThenPrefetch(token, 0);
    }

    async function searchActor() {
      const query = inputValue(elements.atlasSearch);
      if (!query) return;
      setEnabled(elements.atlasSearchButton, false);
      nodeText(elements.atlasSearchSummary, `正在查询 ${query} 的完整演员目录…`);
      try {
        if (!(await ensureAtlasProxyReady('演员搜索'))) {
          nodeText(elements.atlasSearchSummary, '当前代理检测失败，请修正后再搜索。');
          return;
        }
        let lookupName = query;
        let aliasRecord = null;
        if (desktopApi.resolveActressAlias) {
          try {
            const alias = await desktopApi.resolveActressAlias({ actorName: query, ...lookupOptions() });
            if (alias && alias.name) {
              aliasRecord = alias;
              lookupName = alias.name;
              appendLog(`已通过媒体库资料源解析演员别名：${query} → ${lookupName}。`, 'success');
            }
          } catch (aliasError) {
            appendLog(`别名资料源未命中，继续使用 JAV 目录搜索：${aliasError && aliasError.message ? aliasError.message : aliasError}`);
          }
        }
        const profile = await desktopApi.resolveActressCrawlTarget({
          actressName: lookupName,
          ...lookupOptions({ includeProfile: true })
        });
        if (aliasRecord && desktopApi.rememberActressAlias && profile && profile.resolvedActressName) {
          try {
            await desktopApi.rememberActressAlias({
              canonical: profile.resolvedActressName,
              aliases: aliasRecord.aliases || [],
              source: aliasRecord.provider || 'metadata-provider'
            });
          } catch (cacheError) {
            appendLog(`演员别名缓存写入失败：${cacheError && cacheError.message ? cacheError.message : cacheError}`, 'error');
          }
        }
        await selectActor({
          rank: '',
          actressName: profile.resolvedActressName || query,
          profileUrl: profile.resolvedBase,
          worksCount: profile.allCount,
          lookupProfile: profile
        });
        nodeText(elements.atlasSearchSummary, `已定位 ${profile.resolvedActressName || query}，与榜单无关。`);
      } catch (error) {
        nodeText(elements.atlasSearchSummary, `未定位 ${query}：${error && error.message ? error.message : error}`);
      } finally {
        setEnabled(elements.atlasSearchButton, true);
      }
    }

    function bindEvents() {
      if (eventsBound) return;
      eventsBound = true;
      const rankingLogSubscription = desktopApi && typeof desktopApi.onAtlasRankingLog === 'function'
        ? desktopApi.onAtlasRankingLog
        : desktopApi && desktopApi.onActressRankingLog;
      if (typeof rankingLogSubscription === 'function') {
        rankingLogSubscription((entry) => {
          if (!entry || typeof entry !== 'object') return;
          const level = ['error', 'warn', 'success'].includes(String(entry.level || '')) ? String(entry.level) : 'info';
          const message = String(entry.message || '').trim();
          if (message) appendLog(`[榜单] ${message}`, level);
        });
      }
      elements.atlasProxySave && elements.atlasProxySave.addEventListener('click', () => void saveProxy());
      elements.atlasNodeStatus && elements.atlasNodeStatus.addEventListener('click', () => void checkNodeRegion('手动检测'));
      elements.atlasSearchButton && elements.atlasSearchButton.addEventListener('click', () => void searchActor());
      elements.atlasSearch && elements.atlasSearch.addEventListener('keydown', (event) => {
        if (event.key === 'Enter') void searchActor();
      });
      elements.atlasRefresh && elements.atlasRefresh.addEventListener('click', () => void loadRankings(true));
      elements.atlasHistoryBackfill && elements.atlasHistoryBackfill.addEventListener('click', () => void backfillHistory());
      elements.atlasHistorySave && elements.atlasHistorySave.addEventListener('click', () => void saveCurrentMonthlyRanking());
      elements.atlasSource && elements.atlasSource.addEventListener('change', () => void loadRankings(false));
      elements.atlasYear && elements.atlasYear.addEventListener('change', () => void loadRankings(false));
      elements.atlasMonth && elements.atlasMonth.addEventListener('change', () => void loadRankings(false));
      elements.atlasModeMonthlyButton && elements.atlasModeMonthlyButton.addEventListener('click', () => {
        elements.atlasModeMonthlyButton.classList.add('active');
        elements.atlasModeAnnualButton.classList.remove('active');
        resetPeriodControlsForMode('monthly');
        void loadRankings(true);
      });
      elements.atlasModeAnnualButton && elements.atlasModeAnnualButton.addEventListener('click', () => {
        elements.atlasModeAnnualButton.classList.add('active');
        elements.atlasModeMonthlyButton.classList.remove('active');
        resetPeriodControlsForMode('annual');
        void loadRankings(true);
      });
      elements.atlasWorkPrevious && elements.atlasWorkPrevious.addEventListener('click', () => void loadWorkPage(workPage - 1));
      elements.atlasWorkNext && elements.atlasWorkNext.addEventListener('click', () => void loadWorkPage(workPage + 1));
      elements.atlasWorkRetry && elements.atlasWorkRetry.addEventListener('click', () => {
        const start = workPage * UI_PAGE_SIZE;
        void retryWorkCovers(works.slice(start, start + UI_PAGE_SIZE).filter((work) => work.coverLoadFailed), '本页失败封面');
      });
      elements.atlasActivityLogClear && elements.atlasActivityLogClear.addEventListener('click', () => {
        logs = [];
        appendLog('日志已清空。');
      });
      elements.atlasOpenProfile && elements.atlasOpenProfile.addEventListener('click', () => {
        const profileURL = selected && selected.lookupProfile && selected.lookupProfile.resolvedBase || selected && selected.profileUrl;
        if (profileURL) desktopApi.openExternal(profileURL);
      });
      elements.atlasFillCrawler && elements.atlasFillCrawler.addEventListener('click', async () => {
        if (!selected) return;
        let profile = selected.lookupProfile || null;
        if (!profile || !profile.resolvedActressName) {
          if (!(await ensureAtlasProxyReady('读取演员资料'))) return;
          profile = await desktopApi.inspectActressTarget({
            actressName: selected.actressName,
            ...lookupOptions({ includeProfile: true })
          });
        }
        const actressName = String(profile.resolvedActressName || selected.actressName || '').trim();
        if (!actressName) {
          appendLog('无法识别演员名称，未填充 JAV 爬虫。', 'error');
          return;
        }
        let output = '';
        let outputSource = '';
        try {
          const resolvedOutput = desktopApi && desktopApi.resolveActressCrawlOutput
            ? await desktopApi.resolveActressCrawlOutput({ actressName })
            : null;
          output = String(resolvedOutput && resolvedOutput.output || '').trim();
          outputSource = String(resolvedOutput && resolvedOutput.source || '').trim();
          if (!output) throw new Error('未返回安全的演员输出目录。');
        } catch (error) {
          appendLog(`JAV 爬虫输出目录初始化失败，已取消跳转：${error && error.message ? error.message : error}`, 'error');
          return;
        }
        if (formController && formController.queueActressLookupResult) {
          formController.queueActressLookupResult(profile, { output });
          appendLog(
            `已填充 JAV 爬虫：${actressName}，${Number(profile.fillCount || profile.preferredCount || 0)} 部，` +
            `输出 ${output}（${outputSource === 'last-user-output' ? '上次用户导出位置' : '应用隐藏路径'}）。`,
            'success'
          );
        } else if (formController && formController.applyActressLookupResult) {
          // Compatibility for an older controller loaded from a stale bundle.
          formController.applyActressLookupResult(profile, { output });
        }
        if (switchWorkspace) switchWorkspace('crawler');
      });
      elements.atlasSubscribe && elements.atlasSubscribe.addEventListener('click', async () => {
        if (!selected) return;
        const profile = selected.lookupProfile || {};
        await desktopApi.addAvSubscription({
          actressName: profile.resolvedActressName || selected.actressName,
          targetUrl: profile.resolvedBase || selected.profileUrl,
          syncedCount: profile.allCount || 0,
          itemsPerPage: SOURCE_PAGE_SIZE,
          totalPages: Math.ceil((profile.allCount || 0) / SOURCE_PAGE_SIZE)
        });
        nodeText(elements.atlasActorMeta, '订阅已建立。');
      });
    }

    async function bootstrap() {
      if (bootstrapped) return;
      bindEvents();
      // 运行日志面板在进入图鉴板块时即显示，并写入初始化日志，
      // 不等用户点击任何信息。
      if (elements.atlasActivityLogPanel) {
        elements.atlasActivityLogPanel.classList.remove('hidden');
      }
      appendLog('演员图鉴就绪，正在加载本地榜单缓存...');
      await loadProxy();
      bootstrapped = true;
      await loadRankings(false);
    }

    async function ensureProxyNotice() {
      // Empty means direct/system-proxy mode; do not present it as an error.
      return true;
    }

    return {
      bootstrap,
      refresh: async () => {
        await loadProxy();
        return loadRankings(false);
      },
      ensureProxyNotice
    };
  }

  globalScope.desktopActressAtlasController = { createActressAtlasController };
})(typeof globalThis !== 'undefined' ? globalThis : window);
