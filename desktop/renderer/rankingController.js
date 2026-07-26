// Ranking controller is the renderer-facing surface for actress ranking
// exploration. It only renders normalized ranking payloads from desktopApi and
// performs one-way handoff into the crawler form when the user wants to prefill
// a crawl target from a ranking entry.
//
// Ownership summary:
// 1) own ranking-page state, filters, and source selection
// 2) render normalized ranking payloads through ranking views
// 3) hand ranking selections one-way into the crawler form without owning crawl
//    runtime state
//
// File map for maintainers:
// 1) ranking text/default/filter helpers
// 2) ranking workspace bootstrap and state
// 3) ranking load/resolve/fill-crawler actions
(function initializeRankingController(globalScope) {
  const rendererHelpers = globalScope.desktopRendererHelpers || {};
  const rankingListView = globalScope.desktopRankingListView || null;
  const STORAGE_KEY = 'desktop-ranking-source-channel';
  const clearChildren = rendererHelpers.clearChildren;
  const createOption = rendererHelpers.createOption;
  const getErrorMessage = rendererHelpers.getErrorMessage;
  const safeLocalStorageGet = rendererHelpers.safeLocalStorageGet;
  const safeLocalStorageSet = rendererHelpers.safeLocalStorageSet;
  const bindAsyncClickHelper = rendererHelpers.bindAsyncClick || null;

  if (!rankingListView || !clearChildren || !createOption || !getErrorMessage || !safeLocalStorageGet || !safeLocalStorageSet) {
    throw new Error('rankingController requires desktopRankingListView and desktopRendererHelpers');
  }

  const DEFAULT_RANKING_TEXT = {
    empty: '\u5f53\u524d\u6ca1\u6709\u53ef\u5c55\u793a\u7684\u699c\u5355\u6570\u636e\u3002',
    loading: '\u6b63\u5728\u52a0\u8f7d\u699c\u5355\u6570\u636e...',
    loadFailedMeta: '\u53c2\u8003\u699c\u5355\u83b7\u53d6\u5931\u8d25\uff0c\u8bf7\u7a0d\u540e\u91cd\u8bd5\u3002',
    monthMode: '\u6708\u5ea6',
    annualMode: '\u5e74\u5ea6',
    modeLabel: '\u699c\u5355\u7c7b\u578b',
    channelLabel: '\u4fe1\u606f\u6e20\u9053',
    channelSmart: '\u667a\u80fd\u63a8\u8350',
    channelFanza: 'FANZA \u5b98\u65b9',
    channelDmm: 'DMM \u5b98\u65b9',
    channelAvfan: 'AVfan \u5728\u7ebf',
    channelLocal: '\u672c\u5730\u5386\u53f2',
    yearLabel: '\u5e74\u4efd',
    monthLabel: '\u6708\u4efd',
    monthHelp: '\u53ef\u6309\u5e74\u4efd\u4e0e\u6708\u4efd\u7b5b\u9009\u6708\u5ea6\u699c\u5355\uff0c\u4f18\u5148\u663e\u793a\u53ef\u7528\u7684\u6700\u65b0\u6570\u636e\u3002',
    annualHelp: '\u53ef\u6309\u5e74\u4efd\u67e5\u770b\u5e74\u5ea6\u699c\u5355\uff0c\u9002\u5408\u7528\u4e8e\u5feb\u901f\u53c2\u8003\u5973\u4f18\u70ed\u5ea6\u8d70\u52bf\u3002',
    currentMonthOnly: '\u6708\u5ea6\u6a21\u5f0f\u4f1a\u4f18\u5148\u5c55\u793a\u5f53\u524d\u6708\u4efd\uff0c\u5982\u5f53\u6708\u6682\u65f6\u4e0d\u53ef\u7528\uff0c\u4f1a\u81ea\u52a8\u56de\u9000\u5230\u6700\u8fd1\u53ef\u7528\u6570\u636e\u3002',
    channelHelpSmart: '\u667a\u80fd\u63a8\u8350\u4f1a\u4f18\u5148\u5c1d\u8bd5\u5b98\u65b9\u6708\u699c\uff0c\u82e5\u5f53\u524d\u7ebf\u8def\u6216\u6570\u636e\u4e0d\u53ef\u7528\uff0c\u5c06\u4f9d\u6b21\u56de\u9000\u81f3 AVfan \u4e0e\u672c\u5730\u5386\u53f2\u3002',
    channelHelpOfficial: '\u5b98\u65b9\u6e20\u9053\u4f18\u5148\u53c2\u8003 DMM / FANZA \u5f53\u524d\u6708\u699c\uff0c\u9700\u642d\u914d\u65e5\u672c\u5730\u533a\u4ee3\u7406 / VPN \u624d\u80fd\u66f4\u7a33\u5b9a\u8bbf\u95ee\u3002',
    channelHelpAvfan: 'AVfan \u9002\u5408\u67e5\u770b\u5df2\u516c\u5f00\u7684\u6708\u699c\u4e0e\u5e74\u699c\uff0c\u5386\u53f2\u6570\u636e\u8986\u76d6\u66f4\u5168\uff0c\u65e0\u6cd5\u8fde\u63a5\u5b98\u65b9\u65f6\u4f53\u9a8c\u66f4\u7a33\u5b9a\u3002',
    channelHelpLocal: '\u672c\u5730\u5386\u53f2\u4ec5\u8bfb\u53d6\u5df2\u7f13\u5b58\u5230\u672c\u673a\u7684\u699c\u5355\u8bb0\u5f55\uff0c\u4e0d\u4f1a\u53d1\u8d77\u5728\u7ebf\u8bf7\u6c42\u3002',
    officialProxyTip: '\u82e5\u5e0c\u671b\u7a33\u5b9a\u67e5\u770b\u5b98\u65b9\u699c\u5355\uff0c\u5efa\u8bae\u5f00\u542f\u65e5\u672c\u5730\u533a\u4ee3\u7406 / VPN\uff0c\u4ee5\u83b7\u5f97\u66f4\u597d\u7684\u52a0\u8f7d\u6210\u529f\u7387\u3002',
    officialAnnualTip: '\u5b98\u65b9\u6e20\u9053\u76ee\u524d\u4ee5\u6708\u699c\u4e3a\u4e3b\uff1b\u5f53\u4f60\u5207\u6362\u5230\u5e74\u5ea6\u699c\u5355\u65f6\uff0c\u7cfb\u7edf\u4f1a\u81ea\u52a8\u4f18\u5148\u6539\u7528 AVfan \u6216\u672c\u5730\u5386\u53f2\u3002',
    avfanTip: 'AVfan \u4e0e\u672c\u5730\u5386\u53f2\u6a21\u5f0f\u4e0d\u5f3a\u4f9d\u8d56\u65e5\u672c\u4ee3\u7406\uff0c\u66f4\u9002\u5408\u67e5\u770b\u5386\u53f2\u699c\u5355\u4e0e\u8fdb\u884c\u8865\u5145\u53c2\u8003\u3002',
    localTip: '\u5f53\u524d\u4ec5\u5c55\u793a\u672c\u5730\u5df2\u7f13\u5b58\u7684\u5386\u53f2\u699c\u5355\uff0c\u4e0d\u4f1a\u989d\u5916\u8fde\u63a5\u5916\u90e8\u7f51\u7ad9\u3002',
    sourcePrefix: '\u6765\u6e90\uff1a',
    selectedSourcePrefix: '\u5df2\u9009\u6e20\u9053\uff1a',
    resolvedSourcePrefix: '\u5f53\u524d\u4f7f\u7528\uff1a',
    fetchedAtPrefix: '\u6293\u53d6\u65f6\u95f4\uff1a',
    periodPrefix: '\u7edf\u8ba1\u5468\u671f\uff1a',
    totalPrefix: '\u699c\u5355\u6570\u91cf\uff1a',
    staleSuffix: '\u7f13\u5b58\u6570\u636e',
    openSource: '\u6253\u5f00\u6765\u6e90',
    unknownActress: '\u672a\u77e5\u5973\u4f18',
    autoFillButtonTitleSuffix: ' - \u70b9\u51fb\u540e\u81ea\u52a8\u586b\u5145\u771f\u5b9e\u5973\u4f18\u76ee\u5f55\u4e0e\u6709\u78c1\u529b\u6570\u91cf',
    rankSuffix: '\u540d',
    sourceItemPrefix: '\u76ee\u5f55\u94fe\u63a5\uff1a',
    noYearData: '\u6682\u65e0\u5e74\u5ea6\u6570\u636e',
    yearOptionSuffix: ' \u5e74',
    noMonthData: '\u6682\u65e0\u6708\u4efd\u6570\u636e',
    noticePrefix: '\u63d0\u793a\uff1a',
    warningPrefix: '\u8bf4\u660e\uff1a',
    openProfileLogPrefix: '\u5df2\u5728\u9ed8\u8ba4\u6d4f\u89c8\u5668\u6253\u5f00\u76ee\u5f55\u94fe\u63a5\uff1a'
  };

  function toSourceLabel(url) {
    if (!url) {
      return '';
    }

    try {
      const parsed = new URL(url);
      const decodedPath = decodeURIComponent(parsed.pathname);
      return `${parsed.hostname}${decodedPath}`;
    } catch {
      return String(url);
    }
  }

  function toFriendlyIssueText(message) {
    const rawText = String(message || '').trim();
    if (!rawText) {
      return '';
    }

    if (rawText.includes('ERR_PROXY_CONNECTION_FAILED')) {
      return '\u5f53\u524d\u4ee3\u7406\u7ebf\u8def\u8fde\u63a5\u5931\u8d25\uff0c\u8bf7\u66f4\u6362\u53ef\u7528\u7684\u65e5\u672c\u8282\u70b9\u540e\u91cd\u8bd5\u3002';
    }

    if (rawText.includes('not-available-in-your-region') || rawText.includes('region')) {
      return '\u5f53\u524d\u7ebf\u8def\u65e0\u6cd5\u7a33\u5b9a\u8bbf\u95ee\u5b98\u65b9\u699c\u5355\uff0c\u8bf7\u786e\u8ba4\u65e5\u672c\u5730\u533a\u4ee3\u7406 / VPN \u662f\u5426\u53ef\u7528\u3002';
    }

    if (rawText.includes('age') && rawText.includes('check')) {
      return '\u5b98\u65b9\u699c\u5355\u8bbf\u95ee\u672a\u901a\u8fc7\u5e74\u9f84\u9a8c\u8bc1\uff0c\u8bf7\u66f4\u6362\u53ef\u7528\u7684\u65e5\u672c\u8282\u70b9\u540e\u91cd\u8bd5\u3002';
    }

    return rawText;
  }

  function createRankingController(options) {
    const { elements, desktopApi, logController, uiText, formController } = options;
    const { UI_TEXT } = uiText;
    const rankingText = Object.assign({}, DEFAULT_RANKING_TEXT, UI_TEXT.ranking || {});
    // Ranking workflow map:
    // 1) fetch one normalized ranking payload from desktopApi
    // 2) render/filter that payload locally
    // 3) optionally prefill crawler form from one selected actress target
    //
    // This controller must stay read-mostly. It should not absorb crawler
    // runtime, subscription, or organizer ownership.

    // Ranking controller keeps only ranking-page state:
    // - currentSourceUrl for "open source" actions
    // - latestLoadToken for last-request-wins protection
    // It must not cache crawler runtime state; crawler interaction is a one-way
    // prefill handoff through fillActressTarget.
    let currentSourceUrl = '';
    let latestLoadToken = 0;
    let eventsBound = false;
    let bootstrapCompleted = false;
    let bootstrapPromise = null;
    let crawlCacheSnapshots = [];

    function appendRankingLog(level, message, timestamp = new Date().toISOString()) {
      logController.appendLog(level, message, timestamp);
    }

    function getSelectedCacheKey() {
      return elements.crawlCacheSelect ? String(elements.crawlCacheSelect.value || '').trim() : '';
    }

    function renderCacheSnapshotSummary() {
      if (!elements.crawlCacheSummary) {
        return;
      }

      if (!Array.isArray(crawlCacheSnapshots) || crawlCacheSnapshots.length === 0) {
        elements.crawlCacheSummary.textContent = '当前没有内部缓存快照。';
        return;
      }

      const selectedKey = getSelectedCacheKey();
      const selectedItem = crawlCacheSnapshots.find((item) => String(item && item.cacheKey ? item.cacheKey : '').trim() === selectedKey);
      const targetItem = selectedItem || crawlCacheSnapshots[0];
      const actressName = String(targetItem && targetItem.actressName ? targetItem.actressName : '').trim() || '未命名快照';
      const updatedAt = String(targetItem && targetItem.updatedAt ? targetItem.updatedAt : '').trim();
      const completedCount = Number(targetItem && targetItem.completedCount ? targetItem.completedCount : 0) || 0;
      const itemsPerPage = Number(targetItem && targetItem.itemsPerPage ? targetItem.itemsPerPage : 0) || 0;
      const totalPages = Number(targetItem && targetItem.totalPages ? targetItem.totalPages : 0) || 0;

      const parts = [`当前缓存：${actressName}`, `完成 ${completedCount} 部`];
      if (itemsPerPage > 0) {
        parts.push(`每页 ${itemsPerPage}`);
      }
      if (totalPages > 0) {
        parts.push(`总页数 ${totalPages}`);
      }
      if (updatedAt) {
        parts.push(`更新时间 ${updatedAt.replace('T', ' ').slice(0, 16)}`);
      }
      elements.crawlCacheSummary.textContent = parts.join(' | ');
    }

    function renderCacheSnapshotOptions() {
      if (!elements.crawlCacheSelect || !clearChildren) {
        return;
      }

      const previousValue = getSelectedCacheKey();
      clearChildren(elements.crawlCacheSelect);

      elements.crawlCacheSelect.appendChild(createOption('', '请选择缓存快照'));

      // 按更新时间降序排序，最新的缓存快照显示在最上面
      const sortedSnapshots = [...crawlCacheSnapshots].sort((a, b) => {
        const timeA = new Date(a && a.updatedAt ? a.updatedAt : '').getTime();
        const timeB = new Date(b && b.updatedAt ? b.updatedAt : '').getTime();
        return timeB - timeA;
      });

      sortedSnapshots.forEach((item, index) => {
        const cacheKey = String(item && item.cacheKey ? item.cacheKey : '').trim();
        const actressName = String(item && item.actressName ? item.actressName : '').trim() || `缓存快照 ${index + 1}`;
        const updatedAt = String(item && item.updatedAt ? item.updatedAt : '').trim();
        const completedCount = Number(item && item.completedCount ? item.completedCount : 0) || 0;
        const displayName = String(item && item.displayName ? item.displayName : '').trim();
        const text = displayName || (updatedAt
          ? `${actressName} | ${updatedAt.replace('T', ' ').slice(0, 16)} | ${completedCount} 部`
          : `${actressName} | ${completedCount} 部`);
        elements.crawlCacheSelect.appendChild(createOption(cacheKey, text));
      });

      const nextValue =
        previousValue && crawlCacheSnapshots.some((item) => String(item && item.cacheKey ? item.cacheKey : '').trim() === previousValue)
          ? previousValue
          : '';
      elements.crawlCacheSelect.value = nextValue;
      if (!nextValue && crawlCacheSnapshots.length > 0) {
        elements.crawlCacheSelect.selectedIndex = 1;
      }
      renderCacheSnapshotSummary();
    }

    async function loadCacheSnapshots(options = {}) {
      if (!desktopApi || typeof desktopApi.listCrawlCacheSnapshots !== 'function') {
        crawlCacheSnapshots = [];
        renderCacheSnapshotOptions();
        return;
      }

      const { silent = false } = options;
      try {
        const result = await desktopApi.listCrawlCacheSnapshots();
        crawlCacheSnapshots = Array.isArray(result && result.items) ? result.items : [];
        renderCacheSnapshotOptions();
        if (!silent) {
          appendRankingLog('info', `已刷新内部缓存快照：${crawlCacheSnapshots.length} 条。`);
        }
      } catch (error) {
        crawlCacheSnapshots = [];
        renderCacheSnapshotOptions();
        appendRankingLog('warn', `读取内部缓存快照失败：${getErrorMessage(error)}`);
      }
    }

    async function removeSelectedCacheSnapshot() {
      const cacheKey = getSelectedCacheKey();
      if (!cacheKey) {
        appendRankingLog('warn', '请先选择一个缓存快照。');
        return;
      }

      const result = await desktopApi.removeCrawlCacheSnapshot(cacheKey);
      crawlCacheSnapshots = Array.isArray(result && result.items) ? result.items : [];
      renderCacheSnapshotOptions();
      appendRankingLog('info', '已清除所选内部缓存快照。');
    }

    async function clearAllCacheSnapshots() {
      const confirmed = typeof globalScope.confirm === 'function'
        ? globalScope.confirm('确认清空全部内部缓存快照吗？该操作不会删除用户导出的抓取结果。')
        : true;
      if (!confirmed) {
        return;
      }

      const result = await desktopApi.clearCrawlCacheSnapshots();
      crawlCacheSnapshots = Array.isArray(result && result.items) ? result.items : [];
      renderCacheSnapshotOptions();
      appendRankingLog('info', '已清空全部内部缓存快照。');
    }

    // Event binding and bootstrap are kept idempotent here because ranking
    // workspace re-entry can happen through shell navigation and renderer
    // bootstrap re-entry.

    function bindAsyncClick(button, handler, onError) {
      if (typeof bindAsyncClickHelper === 'function') {
        bindAsyncClickHelper(button, handler, {
          onError,
          fallbackErrorHandler: (error) => appendRankingLog('warn', getErrorMessage(error))
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
          appendRankingLog('warn', getErrorMessage(error));
        }
      });
    }

    function bindAsyncChange(element, handler, onError) {
      if (!element) {
        return;
      }

      element.addEventListener('change', async () => {
        try {
          await handler();
        } catch (error) {
          if (typeof onError === 'function') {
            onError(error);
            return;
          }
          appendRankingLog('warn', getErrorMessage(error));
        }
      });
    }

    function isAnnualMode() {
      return elements.rankingMode.value === 'annual';
    }

    function getSelectedChannel() {
      return String(elements.rankingSourceChannel.value || 'smart').trim() || 'smart';
    }

    function getChannelHelpText(channel) {
      // Help-text routing is UI-only presentation logic. Ranking source
      // availability and fallback remain backend/service responsibilities.
      if (channel === 'fanza' || channel === 'dmm') {
        return rankingText.channelHelpOfficial;
      }

      if (channel === 'avfan') {
        return rankingText.channelHelpAvfan;
      }

      if (channel === 'local') {
        return rankingText.channelHelpLocal;
      }

      return rankingText.channelHelpSmart;
    }

    function getChannelTipText(channel) {
      if (isAnnualMode() && (channel === 'fanza' || channel === 'dmm' || channel === 'smart')) {
        return rankingText.officialAnnualTip;
      }

      if (channel === 'local') {
        return rankingText.localTip;
      }

      if (channel === 'avfan') {
        return rankingText.avfanTip;
      }

      return rankingText.officialProxyTip;
    }

    function setHelpText() {
      // Help/tip projection belongs to the ranking page shell so backend
      // ranking payloads stay transport/data focused.
      const modeHelp = isAnnualMode() ? rankingText.annualHelp : rankingText.currentMonthOnly || rankingText.monthHelp;
      const channel = getSelectedChannel();
      const channelHelp = getChannelHelpText(channel);
      elements.rankingHelp.textContent = [modeHelp, channelHelp].filter(Boolean).join(' ');
      if (elements.rankingChannelTip) {
        elements.rankingChannelTip.textContent = getChannelTipText(channel);
      }
      elements.rankingYearField.classList.remove('hidden');
      elements.rankingMonthField.classList.toggle('hidden', isAnnualMode());
    }

    function setMetaText(text) {
      elements.rankingMeta.textContent = text || rankingText.empty;
      elements.rankingMeta.title = text || rankingText.empty;
    }

    function setSource(sourceName, sourceUrl) {
      // Source banner is a read-only projection of the last successful result.
      // It must not become implicit input for later load decisions.
      currentSourceUrl = sourceUrl || '';

      if (!currentSourceUrl) {
        elements.rankingSource.classList.add('hidden');
        elements.rankingSourceText.textContent = '';
        elements.rankingSourceText.title = '';
        return;
      }

      const sourceText = `${rankingText.sourcePrefix}${sourceName} ${toSourceLabel(sourceUrl)}`;
      elements.rankingSource.classList.remove('hidden');
      elements.rankingSourceText.textContent = sourceText;
      elements.rankingSourceText.title = `${rankingText.sourcePrefix}${sourceName} ${sourceUrl}`;
    }

    function renderEmpty(text) {
      rankingListView.renderEmpty(elements.rankingView, text || rankingText.empty);
    }

    // Ranking only resolves/fills crawler inputs. It must not start crawl runs
    // or store hidden crawl-session state locally.
    async function fillActressTarget(item) {
      // Ranking-to-crawler is a one-way prefill handoff only. This controller
      // never starts crawl execution or stores crawl session state itself.
      const actressName = String(item && item.actressName ? item.actressName : '').trim();
      if (!actressName) {
        return;
      }

      try {
        const lookupContext = formController && typeof formController.getLookupContext === 'function'
          ? formController.getLookupContext()
          : { preferredBase: elements.base.value.trim(), magnetOnly: elements.nomag.checked };

        appendRankingLog('info', `${UI_TEXT.messages.rankingResolvingPrefix}${actressName}`);

        const result = await desktopApi.resolveActressCrawlTarget({
          actressName,
          preferredBase: lookupContext.preferredBase,
          magnetOnly: lookupContext.magnetOnly
        });

        if (formController && typeof formController.applyActressLookupResult === 'function') {
          formController.applyActressLookupResult(result);
        }

        const fillCount =
          Number.isFinite(result.fillCount) && result.fillCount >= 0 ? result.fillCount : result.preferredCount;
        const summary = [
          `${UI_TEXT.messages.rankingResolvedPrefix}${result.resolvedActressName || actressName}`,
          `${UI_TEXT.messages.rankingResolvedMagnetPrefix}${result.magnetCount || fillCount}${UI_TEXT.messages.rankingResolvedCountSuffix}`,
          `${UI_TEXT.messages.rankingResolvedAllPrefix}${result.allCount || fillCount}${UI_TEXT.messages.rankingResolvedCountSuffix}`,
          `${UI_TEXT.messages.rankingResolvedPagesPrefix}${result.totalPages}${UI_TEXT.messages.rankingResolvedPagesSuffix}`,
          UI_TEXT.messages.rankingResolvedDefaultHint
        ].join(' ');

        appendRankingLog('info', summary);
      } catch (error) {
        appendRankingLog('warn', `${UI_TEXT.messages.rankingResolveFailedPrefix}${getErrorMessage(error)}`);
      }
    }

    function renderItems(items) {
      rankingListView.renderRankingItems(
        elements.rankingView,
        Array.isArray(items) ? items : [],
        {
          formatSourceLabel: toSourceLabel,
          onBindFillTarget: (button, item) => {
            bindAsyncClick(button, async () => {
              await fillActressTarget(item);
            });
          },
          onBindOpenProfile: (button, item) => {
            bindAsyncClick(button, async () => {
              if (!item.profileUrl) {
                return;
              }

              await desktopApi.openExternal(item.profileUrl);
              appendRankingLog('info', `${rankingText.openProfileLogPrefix}${item.profileUrl}`);
            });
          }
        },
        rankingText
      );
    }

    function renderYearOptions(years = [], selectedYear = '') {
      rankingListView.renderYearOptions(elements.rankingYear, years, selectedYear, rankingText);
    }

    function renderMonthOptions(months = [], selectedMonth = '') {
      rankingListView.renderMonthOptions(elements.rankingMonth, months, selectedMonth, rankingText);
    }

    function syncPeriodSelectors(ranking) {
      renderYearOptions(ranking.availableYears, ranking.periodYear);
      if (!isAnnualMode()) {
        renderMonthOptions(ranking.availableMonths, ranking.periodMonth);
      }
    }

    function buildMetaText(ranking) {
      const channelLine = [
        `${rankingText.selectedSourcePrefix}${ranking.requestedSourceLabel || rankingText.channelSmart}`,
        `${rankingText.resolvedSourcePrefix}${ranking.resolvedSourceLabel || ranking.sourceName || rankingText.channelSmart}`
      ];

      const metaParts = [
        `${rankingText.periodPrefix}${ranking.periodLabel}`,
        `${rankingText.totalPrefix}${ranking.total}`,
        `${rankingText.fetchedAtPrefix}${new Date(ranking.fetchedAt).toLocaleString('zh-CN', { hour12: false })}`
      ];

      if (ranking.stale) {
        metaParts.push(rankingText.staleSuffix);
      }

      const lines = [channelLine.join(' | '), metaParts.join(' | ')];
      if (ranking.notice) {
        lines.push(`${rankingText.noticePrefix}${ranking.notice}`);
      }
      if (ranking.errorMessage && ranking.errorMessage !== ranking.notice) {
        lines.push(`${rankingText.warningPrefix}${toFriendlyIssueText(ranking.errorMessage)}`);
      }
      return lines.join('\n');
    }

    // Ranking requests are user-driven and race-prone when filters change
    // quickly. latestLoadToken makes "latest interaction wins" explicit so
    // slower old responses cannot repaint the page with stale list data.
    async function loadRankings(options = {}) {
      const { forceRefresh = false, silent = false } = options;
      const requestToken = latestLoadToken + 1;
      latestLoadToken = requestToken;

      const mode = elements.rankingMode.value || 'monthly';
      const year = elements.rankingYear.value;
      const month = !isAnnualMode() ? elements.rankingMonth.value : '';
      const source = getSelectedChannel();
      const proxy = elements.proxy && elements.proxy.value ? elements.proxy.value.trim() : '';

      if (!silent) {
        appendRankingLog('info', UI_TEXT.messages.rankingLoading);
      }

      elements.refreshRankingButton.disabled = true;
      setMetaText(rankingText.loading);

      try {
        const ranking = await desktopApi.getActressRankings({
          mode,
          year,
          month,
          source,
          proxy,
          forceRefresh
        });

        if (requestToken !== latestLoadToken) {
          return;
        }

        syncPeriodSelectors(ranking);
        setSource(ranking.sourceName, ranking.sourceUrl);
        renderItems(ranking.items);
        setMetaText(buildMetaText(ranking));

        if (!silent) {
          appendRankingLog(
            'info',
            `${UI_TEXT.messages.rankingLoadedPrefix}${ranking.periodLabel}${UI_TEXT.messages.rankingLoadedMiddle}${ranking.total}${UI_TEXT.messages.rankingLoadedSuffix}`
          );
        }

        if (ranking.notice) {
          appendRankingLog('info', ranking.notice);
        }
        if (ranking.errorMessage && ranking.errorMessage !== ranking.notice) {
          appendRankingLog('warn', ranking.errorMessage);
        }
      } catch (error) {
        if (requestToken !== latestLoadToken) {
          return;
        }

        currentSourceUrl = '';
        elements.rankingSource.classList.add('hidden');
        renderEmpty(getErrorMessage(error));
        setMetaText(rankingText.loadFailedMeta);
        appendRankingLog('warn', getErrorMessage(error));
      } finally {
        if (requestToken === latestLoadToken) {
          elements.refreshRankingButton.disabled = false;
        }
      }
    }

    function bindEvents() {
      if (eventsBound) {
        return;
      }

      eventsBound = true;
      bindAsyncChange(elements.rankingSourceChannel, async () => {
        safeLocalStorageSet(STORAGE_KEY, getSelectedChannel());
        setHelpText();
        await loadRankings({ silent: true });
      });

      bindAsyncChange(elements.rankingMode, async () => {
        setHelpText();
        await loadRankings({ silent: true });
      });

      bindAsyncChange(elements.rankingYear, async () => {
        await loadRankings({ silent: true });
      });

      bindAsyncChange(elements.rankingMonth, async () => {
        if (isAnnualMode()) {
          return;
        }

        await loadRankings({ silent: true });
      });

      bindAsyncClick(elements.refreshRankingButton, async () => {
        await loadRankings({ forceRefresh: true });
      });

      bindAsyncClick(elements.openRankingSourceButton, async () => {
        if (!currentSourceUrl) {
          return;
        }

        await desktopApi.openExternal(currentSourceUrl);
        appendRankingLog('info', `${UI_TEXT.messages.rankingSourceOpenedPrefix}${currentSourceUrl}`);
      });

      bindAsyncClick(elements.rankingSourceText, async () => {
        if (!currentSourceUrl) {
          return;
        }

        await desktopApi.openExternal(currentSourceUrl);
      });

      bindAsyncClick(elements.crawlCacheRefreshButton, async () => {
        await loadCacheSnapshots();
      });

      bindAsyncClick(elements.crawlCacheRemoveButton, async () => {
        await removeSelectedCacheSnapshot();
      });

      bindAsyncClick(elements.crawlCacheClearButton, async () => {
        await clearAllCacheSnapshots();
      });

      if (elements.crawlCacheSelect) {
        elements.crawlCacheSelect.addEventListener('change', () => {
          renderCacheSnapshotSummary();
        });
      }
    }

    function renderSourceChannelOptions() {
      const savedSource = safeLocalStorageGet(STORAGE_KEY, 'smart');
      rankingListView.renderSourceChannelOptions(elements.rankingSourceChannel, savedSource, rankingText);
    }

    function bootstrap() {
      if (bootstrapCompleted) {
        return Promise.resolve();
      }

      if (bootstrapPromise) {
        return bootstrapPromise;
      }

      // Ranking bootstrap only hydrates the initial filters + first list fetch.
      // After the first successful boot, user-driven filter changes become the
      // only authority so later bootstrap re-entry cannot silently reset them.
      bootstrapPromise = Promise.resolve()
        .then(async () => {
          renderSourceChannelOptions();
          rankingListView.renderModeOptions(elements.rankingMode, elements.rankingMode.value || 'monthly', rankingText);
          renderYearOptions([]);
          renderMonthOptions([]);
          setHelpText();
          renderCacheSnapshotOptions();
          bindEvents();
          await loadCacheSnapshots({ silent: true });
          await loadRankings({ silent: true });
          bootstrapCompleted = true;
        })
        .catch((error) => {
          bootstrapPromise = null;
          throw error;
        });

      return bootstrapPromise;
    }

    return {
      bootstrap,
      loadRankings
    };
  }

  globalScope.desktopRankingController = {
    createRankingController
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
