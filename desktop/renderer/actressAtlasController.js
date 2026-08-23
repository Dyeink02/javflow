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
        row.className = `atlas-activity-log-entry${entry.level === 'error' ? ' is-error' : entry.level === 'success' ? ' is-success' : ''}`;
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
        return { status: 'empty' };
      }
      // Settings are restored before the first ranking request so a new launch
      // reflects the real connection state instead of pretending the saved URL
      // is usable.
      return validateProxy(saved, true);
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

    // A failed refresh never clears the existing ranking. This keeps the
    // bundled or last successful cache useful when a source is rate-limited.
    async function loadRankings(forceRefresh = false) {
      if (!desktopApi || typeof desktopApi.getActressRankings !== 'function') return;
      if (!(await ensureAtlasProxyReady('加载榜单'))) {
        nodeText(elements.atlasRankingNotice, '当前代理检测失败，已保留已有榜单。');
        return;
      }
      const request = ++rankingRequest;
      nodeText(elements.atlasRankingNotice, '正在加载最新榜单，预加载内容仍保留。');
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
        atlasView.renderRanking(ranking, (item) => void selectActor(item));
        appendLog(`榜单已更新：${next.items.length} 位演员。`, 'success');
      } catch (error) {
        if (request !== rankingRequest) return;
        const message = error && error.message ? error.message : error;
        nodeText(elements.atlasRankingNotice, `榜单加载失败，继续显示已有内容：${message}`);
        appendLog(`榜单加载失败：${message}`, 'error');
      }
    }

    function workIdentity(work) {
      return `${String(work && work.url || '').trim()}|${String(work && work.code || '').trim()}`;
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
          if (String(updated.coverUrl).startsWith('/subscription-media/')) cachedCount += 1;
          return { ...work, ...updated };
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
      const retryWorks = (candidates || []).filter((work) => work && workIdentity(work)).slice(0, UI_PAGE_SIZE);
      if (!retryWorks.length) {
        appendLog('当前页没有需要重新加载的封面。');
        return;
      }
      appendLog(`开始重新加载${label}：${retryWorks.length} 张（并发 3）。`);
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
          if (!updated || !updated.coverUrl) return work;
          refreshed += 1;
          return { ...work, ...updated, coverLoadFailed: false, coverRetrying: false };
        });
        renderWorks();
        appendLog(`${label}重新加载完成：已提交 ${refreshed} 张封面。`, 'success');
      } catch (error) {
        appendLog(`${label}重新加载失败：${error && error.message ? error.message : error}`, 'error');
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
      const currentAvatar = current.avatarUrl || item.imageUrl || '';
      const filteredPromotions = (current.promotionImageUrls || []).filter((url) => {
        const key = canonicalMediaKey(url);
        return key && !sourceKeys.has(key) && key !== canonicalMediaKey(currentAvatar);
      });
      return {
        ...current,
        // Keep a verified profile avatar when available. Metadata providers
        // are a fallback, and their source list is used to remove duplicates
        // from the public-photo section even after local caching rewrites URLs.
        avatarUrl: images[0] || currentAvatar || '',
        promotionImageUrls: [...images.slice(1), ...filteredPromotions]
      };
    }

    async function selectActor(item) {
      selected = item || {};
      const token = ++detailRequest;
      const initialProfile = selected.lookupProfile || {};
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
          targetUrl: initialProfile.resolvedBase || '',
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
        appendLog('演员基础资料和作品已更新。', 'success');
      } else {
        appendLog(`演员资料读取失败：${detailResult.reason && detailResult.reason.message || detailResult.reason}`, 'error');
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
      elements.atlasProxySave && elements.atlasProxySave.addEventListener('click', () => void saveProxy());
      elements.atlasSearchButton && elements.atlasSearchButton.addEventListener('click', () => void searchActor());
      elements.atlasSearch && elements.atlasSearch.addEventListener('keydown', (event) => {
        if (event.key === 'Enter') void searchActor();
      });
      elements.atlasRefresh && elements.atlasRefresh.addEventListener('click', () => void loadRankings(true));
      elements.atlasSource && elements.atlasSource.addEventListener('change', () => void loadRankings(true));
      elements.atlasYear && elements.atlasYear.addEventListener('change', () => void loadRankings(true));
      elements.atlasMonth && elements.atlasMonth.addEventListener('change', () => void loadRankings(true));
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
