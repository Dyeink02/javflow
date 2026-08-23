// Review panel renderer owns the item-list panels shown during and after crawl:
// active, unfinished, duplicate, page-gap, filtered, and failed details.
//
// Ownership summary:
// 1) render active/unfinished/duplicate/page-gap/failed/filtered panels
// 2) dedupe repaint work with per-panel render signatures
// 3) keep review-panel DOM policy out of state transport/controllers
//
// Boundary rule:
// - panel-specific DOM rendering and signature dedupe live here
// - normalization helpers come from stateHelpers
// - runtime event routing and fallback-state selection stay in controllers/model
//
// File map for maintainers:
// 1) panel-local wording/default bundles
// 2) per-list normalization/signature helpers
// 3) review panel DOM projection
(function initializeReviewPanelRenderer(globalScope) {
  function createReviewPanelRenderer(options) {
    const stateHelpers = globalScope.desktopStateHelpers || null;
    if (!stateHelpers) {
      throw new Error('desktopStateHelpers is required before reviewPanelRenderer');
    }

    const {
      buildChipNodes,
      createChip,
      createEmptyChip,
      normalizeDateText,
      normalizeFailedDetails,
      normalizeItems,
      replaceChildren,
      sanitizeReadyFailedDetails,
      sanitizeReadyItems,
      toSafeArray
    } = stateHelpers;
    const {
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
      emptyTexts = {},
      maxPanelItems = 12,
      failureCategoryLabels = {},
      stateTexts = {}
    } = options || {};

    // Review panel keeps all list-card wording in one renderer-local bundle so
    // stateController can stay focused on transport/state batching instead of
    // panel-specific fallback copy.
    const safeStateTexts = {
      failedSummaryPrefix: stateTexts.failedSummaryPrefix || '失败详情共 ',
      failedSummaryMiddle: stateTexts.failedSummaryMiddle || ' 条，当前显示 ',
      failedSummarySuffix: stateTexts.failedSummarySuffix || ' 条。',
      unknownItem: stateTexts.unknownItem || '未知项目',
      defaultFailureReason: stateTexts.defaultFailureReason || '未记录失败原因。',
      failureCategoryPrefix: stateTexts.failureCategoryPrefix || '分类：',
      failureRetryPrefix: stateTexts.failureRetryPrefix || '重试：',
      failureManualReview: stateTexts.failureManualReview || '需人工复核',
      failureAdvicePrefix: stateTexts.failureAdvicePrefix || '建议：',
      failureTimePrefix: stateTexts.failureTimePrefix || '最后失败时间：'
    };
    // Each panel owns an independent render signature. This lets crawl UI state,
    // review panel events, and fallback state snapshots update different
    // sections without forcing the whole review area to repaint every time.
    const lastRenderSignature = {
      active: '',
      unfinished: '',
      duplicate: '',
      pageGap: '',
      failed: '',
      filtered: '',
      completed: ''
    };

    // 当抓取结束时，各复盘面板应保留最后一次非空数据，而不是被清空为 0。
    let preserveOnEmpty = false;
    let hasRenderedNonEmptyFiltered = false;
    let hasRenderedNonEmptyCompleted = false;
    let hasRenderedNonEmptyFailed = false;
    let hasRenderedNonEmptyDuplicate = false;

    function translateFailureCategory(category) {
      return failureCategoryLabels[String(category || '').toLowerCase()] || failureCategoryLabels.unknown || '未知异常';
    }

    function renderActiveItems(items = [], totalCount = items.length) {
      const visibleItems = toSafeArray(items);
      const safeTotal = Number.isFinite(totalCount) ? totalCount : visibleItems.length;
      const signature = `${safeTotal}|${visibleItems.join('|')}`;

      if (signature === lastRenderSignature.active) {
        return;
      }

      lastRenderSignature.active = signature;
      if (!currentBox || !currentItemsView || !currentTotalView) {
        return;
      }

      currentTotalView.textContent = String(safeTotal);

      if (visibleItems.length === 0) {
        // 待机/刚打开软件时卡片保持可见并显示空态文案，而不是整卡隐藏。
        currentBox.classList.remove('hidden');
        replaceChildren(currentItemsView, [createEmptyChip(emptyTexts.active || '当前没有执行的番号。')]);
        return;
      }

      currentBox.classList.remove('hidden');
      replaceChildren(currentItemsView, buildChipNodes(visibleItems, 'state-chip chip-done', emptyTexts.active || '当前没有正在执行的项目。'));
    }

    function updateActiveItems(items = [], totalCount = items.length) {
      const normalized = normalizeItems(items, maxPanelItems);
      renderActiveItems(normalized, Number.isFinite(totalCount) ? totalCount : normalized.length);
    }

    function renderUnfinishedItems(items = [], totalCount = items.length) {
      const safeTotal = Number.isFinite(totalCount) ? totalCount : items.length;
      const visibleItems = toSafeArray(items);
      const signature = `${safeTotal}|${visibleItems.join('|')}`;

      if (signature === lastRenderSignature.unfinished) {
        return;
      }

      lastRenderSignature.unfinished = signature;
      if (!unfinishedItemsView || !unfinishedTotalView) {
        return;
      }

      unfinishedTotalView.textContent = String(safeTotal);
      replaceChildren(
        unfinishedItemsView,
        buildChipNodes(visibleItems, 'state-chip chip-pending', emptyTexts.unfinished || '暂无待抓取番号。')
      );
    }

    function updateUnfinishedItems(items = [], totalCount = items.length) {
      const normalized = normalizeItems(items, maxPanelItems);
      renderUnfinishedItems(normalized, Number.isFinite(totalCount) ? totalCount : normalized.length);
    }

    function renderDuplicateItems(items = [], totalCount = items.length) {
      const safeTotal = Number.isFinite(totalCount) ? totalCount : items.length;
      const visibleItems = toSafeArray(items);
      const signature = `${safeTotal}|${visibleItems.join('|')}`;

      if (signature === lastRenderSignature.duplicate) {
        return;
      }

      lastRenderSignature.duplicate = signature;
      if (!duplicateBox || !duplicateItemsView || !duplicateTotalView) {
        return;
      }

      if (visibleItems.length > 0) {
        hasRenderedNonEmptyDuplicate = true;
      } else if (preserveOnEmpty && hasRenderedNonEmptyDuplicate) {
        // 抓取结束时保留最后一次非空重复数据，避免子面板被收起清空。
        return;
      }

      if (visibleItems.length === 0) {
        duplicateBox.classList.add('hidden');
        duplicateTotalView.textContent = '0';
        replaceChildren(duplicateItemsView, []);
        return;
      }

      duplicateBox.classList.remove('hidden');
      duplicateTotalView.textContent = String(safeTotal);
      replaceChildren(
        duplicateItemsView,
        buildChipNodes(visibleItems, 'duplicate-chip', emptyTexts.duplicate || '当前没有重复番号。')
      );
    }

    function updateDuplicateItems(items = [], totalCount = items.length) {
      const normalized = normalizeItems(items, maxPanelItems);
      renderDuplicateItems(normalized, Number.isFinite(totalCount) ? totalCount : normalized.length);
    }

    function renderPageGapItems(items = []) {
      const visibleItems = toSafeArray(items);
      const signature = visibleItems.join('|');

      if (signature === lastRenderSignature.pageGap) {
        return;
      }

      lastRenderSignature.pageGap = signature;
      if (!pageGapBox || !pageGapItemsView) {
        return;
      }

      if (visibleItems.length === 0) {
        pageGapBox.classList.add('hidden');
        replaceChildren(pageGapItemsView, []);
        return;
      }

      pageGapBox.classList.remove('hidden');
      replaceChildren(pageGapItemsView, buildChipNodes(visibleItems, 'page-gap-chip', emptyTexts.pageGap || '当前没有分页缺口。'));
    }

    function updatePageGapItems(items = []) {
      renderPageGapItems(normalizeItems(items, maxPanelItems));
    }

    function renderFilteredItems(items = [], totalCount = items.length) {
      const safeTotal = Number.isFinite(totalCount) ? totalCount : items.length;
      const visibleItems = toSafeArray(items);
      const signature = `${safeTotal}|${visibleItems.join('|')}`;

      if (signature === lastRenderSignature.filtered) {
        return;
      }

      if (safeTotal > 0 || visibleItems.length > 0) {
        hasRenderedNonEmptyFiltered = true;
      } else if (preserveOnEmpty && hasRenderedNonEmptyFiltered) {
        // 抓取结束时保留最后一次非空过滤数据，避免面板被清空。
        return;
      }

      lastRenderSignature.filtered = signature;
      if (!filteredItemsView || !filteredTotalView) {
        return;
      }

      filteredTotalView.textContent = String(safeTotal);
      replaceChildren(
        filteredItemsView,
        buildChipNodes(visibleItems, 'state-chip chip-filtered', emptyTexts.filtered || '当前没有过滤影片番号。')
      );
    }

    function updateFilteredItems(items = [], totalCount = items.length) {
      const normalized = normalizeItems(items, maxPanelItems);
      renderFilteredItems(normalized, Number.isFinite(totalCount) ? totalCount : normalized.length);
    }

    function renderCompletedItems(items = [], totalCount = items.length) {
      const safeTotal = Number.isFinite(totalCount) ? totalCount : items.length;
      const visibleItems = toSafeArray(items);
      const signature = `${safeTotal}|${visibleItems.join('|')}`;

      if (signature === lastRenderSignature.completed) {
        return;
      }

      if (safeTotal > 0 || visibleItems.length > 0) {
        hasRenderedNonEmptyCompleted = true;
      } else if (preserveOnEmpty && hasRenderedNonEmptyCompleted) {
        // 抓取结束时保留最后一次非空已完成数据，避免面板被清空。
        return;
      }

      lastRenderSignature.completed = signature;
      if (!completedItemsView || !completedTotalView) {
        return;
      }

      completedTotalView.textContent = String(safeTotal);
      replaceChildren(
        completedItemsView,
        buildChipNodes(visibleItems, 'state-chip chip-done', emptyTexts.completed || '当前没有已完成番号。')
      );
    }

    function updateCompletedItems(items = [], totalCount = items.length) {
      const normalized = normalizeItems(items, maxPanelItems);
      renderCompletedItems(normalized, Number.isFinite(totalCount) ? totalCount : normalized.length);
    }

    // 控制“抓取结束后是否保留最后一次非空数据”。新的抓取开始时应关闭保留模式并清空标记。
    function setPreserveMode(value) {
      const next = Boolean(value);
      if (next === preserveOnEmpty) {
        return;
      }
      preserveOnEmpty = next;
      if (!preserveOnEmpty) {
        lastRenderSignature.filtered = '';
        lastRenderSignature.completed = '';
        lastRenderSignature.failed = '';
        lastRenderSignature.duplicate = '';
        hasRenderedNonEmptyFiltered = false;
        hasRenderedNonEmptyCompleted = false;
        hasRenderedNonEmptyFailed = false;
        hasRenderedNonEmptyDuplicate = false;
      }
    }

    function renderFailedDetails(items = [], totalCount = items.length) {
      const visibleItems = toSafeArray(items);
      const safeTotal = Number.isFinite(totalCount) ? totalCount : visibleItems.length;
      const failedSignature = [
        safeTotal,
        ...visibleItems.map((item) =>
          [
            item.item || item.sourceLink || '',
            item.reason || '',
            item.category || '',
            item.retryCount || 0,
            item.recoverable === false ? 'manual' : 'auto',
            item.retryAdvice || '',
            item.lastFailedAt || ''
          ].join('|')
        )
      ].join('##');

      if (failedSignature === lastRenderSignature.failed) {
        return;
      }

      if (visibleItems.length > 0 || safeTotal > 0) {
        hasRenderedNonEmptyFailed = true;
      } else if (preserveOnEmpty && hasRenderedNonEmptyFailed) {
        // 抓取结束时保留最后一次非空失败数据：终态事件可能不携带失败明细，
        // 不能让运行期正常显示的“失败与重复”在结束后被清空。
        return;
      }

      lastRenderSignature.failed = failedSignature;
      if (!failedItemsView || !failedTotalView) {
        return;
      }

      failedTotalView.textContent = String(safeTotal);

      if (visibleItems.length === 0) {
        replaceChildren(failedItemsView, [createEmptyChip(emptyTexts.failed || '暂无失败记录。')]);
        return;
      }

      const nodes = [];

      if (safeTotal > visibleItems.length) {
        const summary = document.createElement('article');
        summary.className = 'failed-summary';
        summary.textContent =
          `${safeStateTexts.failedSummaryPrefix}${safeTotal}${safeStateTexts.failedSummaryMiddle}${visibleItems.length}${safeStateTexts.failedSummarySuffix}`;
        nodes.push(summary);
      }

      visibleItems.forEach((item) => {
        const card = document.createElement('article');
        card.className = 'failed-card';

        const title = document.createElement('strong');
        title.textContent = item.item || item.sourceLink || safeStateTexts.unknownItem;
        title.title = item.sourceLink || item.item || '';

        const reason = document.createElement('span');
        reason.textContent = item.reason || safeStateTexts.defaultFailureReason;

        const meta = document.createElement('div');
        meta.className = 'failed-meta';

        const badges = document.createElement('div');
        badges.className = 'failed-badges';
        badges.appendChild(
          createChip(`${safeStateTexts.failureCategoryPrefix}${translateFailureCategory(item.category)}`, 'failed-badge')
        );
        badges.appendChild(
          createChip(`${safeStateTexts.failureRetryPrefix}${Math.max(1, Number(item.retryCount) || 1)}`, 'failed-badge')
        );
        if (item.recoverable === false) {
          badges.appendChild(createChip(safeStateTexts.failureManualReview, 'failed-badge is-warning'));
        }
        meta.appendChild(badges);

        if (item.retryAdvice) {
          const advice = document.createElement('span');
          advice.textContent = `${safeStateTexts.failureAdvicePrefix}${item.retryAdvice}`;
          meta.appendChild(advice);
        }

        if (item.lastFailedAt) {
          const failedAt = document.createElement('span');
          failedAt.className = 'failed-next-retry';
          failedAt.textContent = `${safeStateTexts.failureTimePrefix}${normalizeDateText(item.lastFailedAt)}`;
          meta.appendChild(failedAt);
        }

        card.appendChild(title);
        card.appendChild(reason);
        card.appendChild(meta);
        nodes.push(card);
      });

      replaceChildren(failedItemsView, nodes);
    }

    function updateFailedDetails(items = [], totalCount = items.length) {
      const visibleItems = normalizeFailedDetails(items, maxPanelItems);
      renderFailedDetails(visibleItems, Number.isFinite(totalCount) ? totalCount : visibleItems.length);
    }

    function applyPanel(panel = {}) {
      renderDuplicateItems(
        sanitizeReadyItems(panel.duplicateItems, maxPanelItems),
        Number.isFinite(panel.duplicateItemsTotal)
          ? panel.duplicateItemsTotal
          : toSafeArray(panel.duplicateItems).length
      );
      renderUnfinishedItems(
        sanitizeReadyItems(panel.unfinishedItems, maxPanelItems),
        Number.isFinite(panel.unfinishedItemsTotal)
          ? panel.unfinishedItemsTotal
          : toSafeArray(panel.unfinishedItems).length
      );
      renderPageGapItems(sanitizeReadyItems(panel.pageGapItems, maxPanelItems));
      renderFilteredItems(
        sanitizeReadyItems(panel.filteredItems || panel.filteredItemIds, maxPanelItems),
        Number.isFinite(panel.filteredItemsTotal)
          ? panel.filteredItemsTotal
          : Number.isFinite(panel.filteredByActressCount)
            ? panel.filteredByActressCount
            : toSafeArray(panel.filteredItems || panel.filteredItemIds).length
      );
      renderCompletedItems(
        sanitizeReadyItems(panel.completedItems || panel.completedItemIds, maxPanelItems),
        Number.isFinite(panel.completedItemsTotal)
          ? panel.completedItemsTotal
          : Number.isFinite(panel.completedCount)
            ? panel.completedCount
          : Number.isFinite(panel.completedItems)
            ? panel.completedItems
            : toSafeArray(panel.completedItems || panel.completedItemIds).length
      );
      renderFailedDetails(
        sanitizeReadyFailedDetails(panel.failedDetails, maxPanelItems),
        Number.isFinite(panel.failedDetailsTotal)
          ? panel.failedDetailsTotal
          : toSafeArray(panel.failedDetails).length
      );
    }

    return {
      applyPanel,
      renderActiveItems,
      renderCompletedItems,
      renderFilteredItems,
      setPreserveMode,
      updateActiveItems,
      updateCompletedItems,
      updateDuplicateItems,
      updateFailedDetails,
      updateFilteredItems,
      updatePageGapItems,
      updateUnfinishedItems
    };
  }

  globalScope.desktopReviewPanelRenderer = {
    createReviewPanelRenderer
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
