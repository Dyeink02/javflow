// Subscription list view owns DOM rendering for subscription cards and empty
// state. Business actions stay in the controller and are passed in as callbacks.
//
// Ownership summary:
// 1) render normalized subscription cards and empty states
// 2) expose controller-provided button bindings
// 3) keep card copy/layout shaping local to the subscription workspace view
//
// Crawl handoff, refresh policy, and persistence remain controller/service work.
//
// File map for maintainers:
// 1) date/status/meta formatting helpers
// 2) empty-state render helper
// 3) subscription card/list DOM builders
// 4) rank/order decoration helpers
(function initializeSubscriptionListView(globalScope) {
  const rendererHelpers = globalScope.desktopRendererHelpers || {};
  const clearChildren = rendererHelpers.clearChildren;

  if (!clearChildren) {
    throw new Error('desktopRendererHelpers must be loaded before subscriptionListView');
  }

  function formatDateTime(value) {
    const rawValue = String(value || '').trim();
    if (!rawValue) {
      return '';
    }

    const parsed = new Date(rawValue);
    if (Number.isNaN(parsed.getTime())) {
      return rawValue;
    }

    return parsed.toLocaleString('zh-CN', { hour12: false });
  }

  function resolveBaselineCount(item) {
    // For manual subscriptions, show user's declared total
    if (item && item.sourceType === 'manual') {
      const declared = Number(item.manualDeclaredTotal);
      if (Number.isFinite(declared) && declared >= 0) {
        return declared;
      }
    }

    const directValue = Number(item && item.baselineCount);
    if (Number.isFinite(directValue) && directValue >= 0) {
      return directValue;
    }

    const syncedValue = Number(item && item.syncedCount);
    if (Number.isFinite(syncedValue) && syncedValue >= 0) {
      return syncedValue;
    }

    if (Array.isArray(item && item.baselineCodes)) {
      return item.baselineCodes.length;
    }

    return 0;
  }

  function resolveCurrentCount(item) {
    const directValue = Number(item && item.currentObservedCount);
    if (Number.isFinite(directValue) && directValue >= 0) {
      return directValue;
    }

    const currentValue = Number(item && item.currentCount);
    if (Number.isFinite(currentValue) && currentValue >= 0) {
      return currentValue;
    }

    return resolveBaselineCount(item) + Number(item && item.pendingCount || 0);
  }

  function resolveCurrentTotal(item) {
    const directValue = Number(item && item.currentTotal);
    if (Number.isFinite(directValue) && directValue > 0) {
      return directValue;
    }

    return resolveBaselineCount(item);
  }

  function buildStatusChip(item) {
    const chip = document.createElement('span');
    const hasPending = Number(item && item.pendingCount || 0) > 0;
    const hasError = String(item && item.lastError || '').trim() !== '';
    chip.className = `subscription-status-chip ${hasError ? 'error' : hasPending ? 'updated' : 'idle'}`;
    chip.textContent = hasError ? '检查失败' : hasPending ? `发现更新 +${item.pendingCount}` : '正常';
    return chip;
  }

  function buildMetaText(item) {
    const segments = [
      `基线 ${resolveBaselineCount(item)} 部`,
      `当前总量 ${resolveCurrentTotal(item)} 部`,
      `当前 ${resolveCurrentCount(item)} 部`,
      `待抓 ${Number(item && item.pendingCount || 0)} 部`,
      `每页 ${Number(item && item.itemsPerPage || 30)} 条`
    ];

    if (item && item.lastCheckedAt) {
      segments.push(`最近检查 ${formatDateTime(item.lastCheckedAt)}`);
    }

    return segments.join(' · ');
  }

  function buildTimeInfo(label, value, emptyText) {
    const wrapper = document.createElement('div');
    wrapper.className = 'subscription-time-info';

    const labelNode = document.createElement('span');
    labelNode.textContent = label;
    wrapper.appendChild(labelNode);

    const valueNode = document.createElement('strong');
    valueNode.textContent = value ? formatDateTime(value) : emptyText;
    if (!value) {
      valueNode.classList.add('is-empty');
    }
    wrapper.appendChild(valueNode);
    return wrapper;
  }

  function createMetric(label, value) {
    const metric = document.createElement('div');
    metric.className = 'subscription-metric';

    const labelNode = document.createElement('span');
    labelNode.textContent = label;

    const valueNode = document.createElement('strong');
    valueNode.textContent = String(value);

    metric.appendChild(labelNode);
    metric.appendChild(valueNode);
    return metric;
  }

  function buildActressFilter(item, callbacks) {
    const wrapper = document.createElement('div');
    wrapper.className = 'subscription-actress-filter';

    const copy = document.createElement('div');
    copy.className = 'subscription-actress-filter-copy';
    const label = document.createElement('span');
    label.textContent = '合集过滤';
    const hint = document.createElement('small');
    hint.textContent = '达到该演员数时不输出磁力，0 为关闭';
    copy.appendChild(label);
    copy.appendChild(hint);

    const controls = document.createElement('div');
    controls.className = 'subscription-actress-filter-controls';
    const input = document.createElement('input');
    input.type = 'number';
    input.min = '0';
    input.step = '1';
    input.inputMode = 'numeric';
    input.value = String(Math.max(0, Number(item && item.actressCountFilterThreshold) || 0));
    input.setAttribute('aria-label', '合集过滤演员数量阈值');
    const saveButton = document.createElement('button');
    saveButton.type = 'button';
    saveButton.className = 'ghost-button subscription-filter-save-button';
    saveButton.textContent = '保存';
    saveButton.addEventListener('click', (event) => {
      event.preventDefault();
      event.stopPropagation();
      if (typeof callbacks.onSaveActressFilter === 'function') {
        callbacks.onSaveActressFilter(item, input.value, saveButton);
      }
    });
    controls.appendChild(input);
    controls.appendChild(saveButton);
    wrapper.appendChild(copy);
    wrapper.appendChild(controls);
    wrapper.addEventListener('click', (event) => event.stopPropagation());
    return wrapper;
  }

  function resolveRankLabel(index, item) {
    const explicitRank = Number(item && item.rankOrder);
    if (Number.isFinite(explicitRank) && explicitRank > 0) {
      return String(explicitRank);
    }
    return String(Number(index) + 1);
  }

  function resolveMediaURLs(item) {
    const values = [];
    if (item && item.avatarUrl) {
      values.push(item.avatarUrl);
    }
    if (Array.isArray(item && item.photoUrls)) {
      values.push(...item.photoUrls);
    }
    return Array.from(new Set(values.map((value) => String(value || '').trim()).filter(Boolean)));
  }

  function buildAvatarButton(item, callbacks) {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'subscription-avatar-button';
    button.title = '查看写真与宣传照';
    button.setAttribute('aria-label', `查看${item && item.actressName ? item.actressName : '演员'}的写真与宣传照`);

    const fallback = document.createElement('span');
    fallback.className = 'subscription-avatar-fallback';
    fallback.textContent = String(item && item.actressName || '?').trim().slice(0, 1) || '?';
    button.appendChild(fallback);

    const mediaURLs = resolveMediaURLs(item);
    if (mediaURLs.length > 0) {
      const image = document.createElement('img');
      image.src = mediaURLs[0];
      image.alt = `${item && item.actressName ? item.actressName : '演员'}头像`;
      image.loading = 'lazy';
      image.addEventListener('load', () => button.classList.add('has-image'));
      image.addEventListener('error', () => image.remove());
      button.appendChild(image);
    } else {
      button.classList.add('is-loading');
    }

    if (typeof callbacks.onOpenMedia === 'function') {
      button.addEventListener('click', () => callbacks.onOpenMedia(item));
    }
    return button;
  }

  function buildOrderControls(item, index, callbacks) {
    const controls = document.createElement('div');
    controls.className = 'subscription-order-controls';

    const dragHandle = document.createElement('span');
    dragHandle.className = 'subscription-drag-handle';
    dragHandle.textContent = `#${resolveRankLabel(index, item)} ⋮⋮`;
    dragHandle.title = '拖拽调整顺序';
    controls.appendChild(dragHandle);

    const upButton = document.createElement('button');
    upButton.type = 'button';
    upButton.className = 'subscription-order-button';
    upButton.textContent = '↑';
    upButton.title = '上移';
    upButton.setAttribute('aria-label', '上移订阅');
    upButton.disabled = index <= 0;
    upButton.addEventListener('click', () => callbacks.onMove && callbacks.onMove(item, -1));
    controls.appendChild(upButton);

    const downButton = document.createElement('button');
    downButton.type = 'button';
    downButton.className = 'subscription-order-button';
    downButton.textContent = '↓';
    downButton.title = '下移';
    downButton.setAttribute('aria-label', '下移订阅');
    downButton.disabled = index >= Number(callbacks.itemCount || 0) - 1;
    downButton.addEventListener('click', () => callbacks.onMove && callbacks.onMove(item, 1));
    controls.appendChild(downButton);
    return controls;
  }

  function renderEmptyList(container, message = '当前还没有任何 AV 订阅。') {
    if (!container) {
      return;
    }

    clearChildren(container);
    const empty = document.createElement('p');
    empty.className = 'subscription-empty';
    empty.textContent = message;
    container.appendChild(empty);
  }

  // View builds one passive card from normalized subscription data. All
  // crawler handoff, delete confirmation, refresh, and sync policy stay in the
  // controller; the view only exposes button binding hooks.
  function createSubscriptionCard(item, index, callbacks = {}) {
    const card = document.createElement('article');
    const hasPending = Number(item && item.pendingCount || 0) > 0;
    const hasError = String(item && item.lastError || '').trim() !== '';
    card.className = `subscription-card${hasPending ? ' has-update' : ''}${hasError ? ' has-error' : ''}`;
    card.dataset.id = String(item && item.id ? item.id : '');
    if (isSelected(callbacks, item)) {
      card.classList.add('is-active');
    }
    card.draggable = callbacks.showOrderControls !== false;
    card.addEventListener('dragstart', (event) => {
      card.classList.add('is-dragging');
      if (event.dataTransfer) {
        event.dataTransfer.effectAllowed = 'move';
        event.dataTransfer.setData('text/plain', String(item && item.id || ''));
      }
      if (typeof callbacks.onDragStart === 'function') {
        callbacks.onDragStart(item);
      }
    });
    card.addEventListener('dragend', () => {
      card.classList.remove('is-dragging');
      card.classList.remove('is-drag-over');
      if (typeof callbacks.onDragEnd === 'function') {
        callbacks.onDragEnd(item);
      }
    });
    card.addEventListener('dragover', (event) => {
      event.preventDefault();
      card.classList.add('is-drag-over');
      if (event.dataTransfer) {
        event.dataTransfer.dropEffect = 'move';
      }
    });
    card.addEventListener('dragleave', () => card.classList.remove('is-drag-over'));
    card.addEventListener('drop', (event) => {
      event.preventDefault();
      card.classList.remove('is-drag-over');
      const draggedID = event.dataTransfer ? event.dataTransfer.getData('text/plain') : '';
      if (typeof callbacks.onDrop === 'function') {
        callbacks.onDrop(draggedID, item);
      }
    });

    card.addEventListener('click', (event) => {
      const target = event.target;
      if (target instanceof HTMLElement && target.closest('button')) {
        return;
      }
      if (typeof callbacks.onSelect === 'function') {
        callbacks.onSelect(item);
      }
    });

    const head = document.createElement('div');
    head.className = 'subscription-card-head';

    head.appendChild(buildAvatarButton(item, callbacks));

    const titleWrap = document.createElement('div');
    const title = document.createElement('strong');
    title.textContent = item && item.actressName ? item.actressName : '未命名订阅';
    titleWrap.appendChild(title);

    const meta = document.createElement('p');
    meta.className = 'subscription-meta';
    meta.textContent = buildMetaText(item);
    titleWrap.appendChild(meta);

    head.appendChild(titleWrap);
    head.appendChild(buildStatusChip(item));
    if (callbacks.showOrderControls !== false) {
      head.appendChild(buildOrderControls(item, index, callbacks));
    }
    card.appendChild(head);

    const metricGrid = document.createElement('div');
    metricGrid.className = 'subscription-metric-grid';
    metricGrid.appendChild(createMetric('基线番号', resolveBaselineCount(item)));
    metricGrid.appendChild(createMetric('当前总量', resolveCurrentTotal(item)));
    metricGrid.appendChild(createMetric('当前总数', resolveCurrentCount(item)));
    metricGrid.appendChild(createMetric('待抓新增', Number(item && item.pendingCount || 0)));
    metricGrid.appendChild(createMetric('每页抓取', Number(item && item.itemsPerPage || 30)));
    card.appendChild(metricGrid);

    const timeGrid = document.createElement('div');
    timeGrid.className = 'subscription-time-grid';
    timeGrid.appendChild(buildTimeInfo('发现更新', item && item.lastUpdateDetectedAt, '尚未发现'));
    timeGrid.appendChild(buildTimeInfo('上次检查', item && item.lastCheckedAt, '尚未检查'));
    timeGrid.appendChild(buildTimeInfo('上次抓取', item && item.lastCrawlAt, '尚未抓取'));
    card.appendChild(timeGrid);
    card.appendChild(buildActressFilter(item, callbacks));

    const urlBlock = document.createElement('div');
    urlBlock.className = 'subscription-url-block';
    const urlLabel = document.createElement('span');
    urlLabel.textContent = '订阅地址';
    const urlText = document.createElement('a');
    urlText.className = 'subscription-url-link';
    const crawlUrl = String(item && item.crawlUrl || '').trim();
    urlText.href = crawlUrl || '#';
    urlText.textContent = crawlUrl || '尚未保存地址';
    urlText.title = crawlUrl || '';
    if (!crawlUrl) {
      urlText.classList.add('is-empty');
    }
    urlText.addEventListener('click', (event) => {
      event.preventDefault();
      event.stopPropagation();
      if (crawlUrl && typeof callbacks.onOpenUrl === 'function') {
        callbacks.onOpenUrl(item);
      }
    });
    urlBlock.appendChild(urlLabel);
    urlBlock.appendChild(urlText);
    card.appendChild(urlBlock);

    if (item && item.lastError) {
      const errorText = document.createElement('p');
      errorText.className = 'subscription-error-text';
      errorText.textContent = `最近异常：${item.lastError}`;
      card.appendChild(errorText);
    }

    const actions = document.createElement('div');
    actions.className = 'subscription-actions';

    const prepareCrawlButton = document.createElement('button');
    prepareCrawlButton.type = 'button';
    prepareCrawlButton.className = 'secondary-button subscription-update-crawl-button';
    prepareCrawlButton.textContent = '更新';
    prepareCrawlButton.disabled = Boolean(callbacks.crawlActive);
    if (typeof callbacks.onOneClickUpdate === 'function') {
      prepareCrawlButton.addEventListener('click', () => callbacks.onOneClickUpdate(item));
    }
    actions.appendChild(prepareCrawlButton);

    const deleteButton = document.createElement('button');
    deleteButton.type = 'button';
    deleteButton.className = 'danger-button subscription-danger-button';
    deleteButton.textContent = '删除订阅';
    if (typeof callbacks.onBindDelete === 'function') {
      callbacks.onBindDelete(deleteButton, item);
    }
    actions.appendChild(deleteButton);

    const editButton = document.createElement('button');
    editButton.type = 'button';
    editButton.className = 'ghost-button';
    editButton.textContent = '修改';
    if (typeof callbacks.onEdit === 'function') {
      editButton.addEventListener('click', () => callbacks.onEdit(item));
    }
    actions.appendChild(editButton);

    card.appendChild(actions);
    return card;
  }

  function isSelected(callbacks, item) {
    return Boolean(callbacks && callbacks.selectedId && item && String(callbacks.selectedId) === String(item.id));
  }

  function renderSubscriptionList(container, items, callbacks = {}) {
    if (!container) {
      return;
    }

    const list = Array.isArray(items) ? items : [];
    if (list.length === 0) {
      renderEmptyList(container, callbacks.emptyMessage || '当前还没有任何 AV 订阅。');
      return;
    }

    clearChildren(container);
    const fragment = document.createDocumentFragment();
    list.forEach((item, index) => {
      fragment.appendChild(createSubscriptionCard(item, index, { ...callbacks, itemCount: list.length }));
    });
    container.appendChild(fragment);
  }

  globalScope.desktopSubscriptionListView = {
    createSubscriptionCard,
    renderEmptyList,
    renderSubscriptionList
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
