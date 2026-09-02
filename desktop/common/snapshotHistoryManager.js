// Shared crawl-snapshot dropdown for organizer/library/subscription.
//
// Ownership summary:
// 1) replace the native snapshot <select> with a custom dropdown trigger
// 2) render one row per backend cache snapshot; each row carries its own
//    delete button inside the opened dropdown panel
// 3) notify every attached dropdown to reload after any removal
//
// Boundary rule:
// the hidden native select stays the value holder: row clicks sync
// select.value and fire its change event, so host controllers keep their
// existing change handling untouched.
//
// Layering rule:
// the open panel is reparented to document.body with position:fixed so it
// floats above every workspace panel (e.g. the scan-result card below the
// library source field) instead of being clipped or covered by siblings.
//
// File map for maintainers:
// 1) attach() idempotent wiring and shared handle registry
// 2) trigger/panel rendering with per-row "✕ 删除" buttons
// 3) fixed-position placement, removal flow, and cross-module reload
(function registerSnapshotHistoryManager(globalScope) {
  const attachedSelects = [];
  const handles = [];
  // Every delete invalidates list requests that were already in flight. This
  // matters when an initial workspace hydration finishes after the user has
  // removed a row and would otherwise paint an older response over the new
  // authoritative list.
  let mutationSerial = 0;

  function defaultFormatSnapshotItem(item) {
    const name = String((item && (item.displayName || item.actressName)) || '').trim() || '未命名任务';
    const updated = String((item && item.updatedAt) || '').replace('T', ' ').slice(0, 16);
    const completed = Number(item && item.completedCount) || 0;
    if (updated) {
      return `${name} | ${updated} | ${completed} 部`;
    }
    return `${name} | ${completed} 部`;
  }

  function logSafe(log, level, message) {
    if (typeof log === 'function') {
      try {
        log(level, message);
      } catch {
        // 日志失败不影响快照下拉功能。
      }
    }
  }

  async function reloadAllHandles(sourceHandle) {
    for (const handle of handles) {
      if (handle === sourceHandle) {
        continue;
      }
      try {
        await handle.reload();
      } catch {
        // 单个板块刷新失败不影响其他板块。
      }
    }
  }

  function syncTriggerLabel(handle) {
    const selected = handle.select.options[handle.select.selectedIndex];
    handle.trigger.textContent = selected
      ? String(selected.textContent || '').trim()
      : handle.placeholder;
  }

  function closePanel(handle) {
    handle.panel.hidden = true;
  }

  // The panel lives at document level while open: fixed coordinates escape
  // ancestor overflow clipping and sibling stacking contexts, so the list is
  // never squeezed by the panel rendered below the field.
  function placePanel(handle) {
    const rect = handle.trigger.getBoundingClientRect();
    const panel = handle.panel;
    panel.style.left = `${Math.max(8, Math.min(rect.left, window.innerWidth - 8 - panel.offsetWidth))}px`;
    const spaceBelow = window.innerHeight - rect.bottom;
    const opensUpward = spaceBelow < Math.min(panel.offsetHeight + 12, 240) && rect.top > spaceBelow;
    if (opensUpward) {
      panel.style.top = `${Math.max(8, rect.top - panel.offsetHeight - 6)}px`;
    } else {
      panel.style.top = `${rect.bottom + 6}px`;
    }
  }

  function buildPanelRows(handle, items) {
    const list = handle.panel.querySelector('.snapshot-picker-list');
    if (!list) {
      return;
    }
    list.replaceChildren();

    const entries = Array.isArray(items) ? items : [];
    if (entries.length === 0) {
      const empty = document.createElement('p');
      empty.className = 'snapshot-picker-empty';
      empty.textContent = '暂无历史快照';
      list.appendChild(empty);
      return;
    }

    entries.forEach((item) => {
      const cacheKey = String((item && item.cacheKey) || '').trim();
      const row = document.createElement('div');
      row.className = 'snapshot-picker-row';
      row.dataset.cacheKey = cacheKey;

      const label = document.createElement('span');
      label.className = 'snapshot-picker-label';
      label.textContent = handle.formatItem(item);
      label.title = label.textContent;
      row.appendChild(label);

      if (cacheKey) {
        const removeButton = document.createElement('button');
        removeButton.type = 'button';
        removeButton.className = 'snapshot-picker-delete';
        removeButton.textContent = '✕ 删除';
        removeButton.title = '删除该快照';
        row.appendChild(removeButton);
      }

      list.appendChild(row);
    });
  }

  function applySelection(handle, item) {
    if (typeof handle.onSelectItem === 'function') {
      handle.onSelectItem(item);
    } else if (typeof handle.itemValue === 'function') {
      handle.select.value = handle.itemValue(item);
      handle.select.dispatchEvent(new Event('change', { bubbles: true }));
    }
    syncTriggerLabel(handle);
  }

  function bindPanelEvents(handle) {
    const panel = handle.panel;

    panel.addEventListener('click', async (event) => {
      const target = event.target;
      if (!(target instanceof Element)) {
        return;
      }

      const removeButton = target.closest('.snapshot-picker-delete');
      if (removeButton) {
        event.preventDefault();
        event.stopPropagation();
        const row = removeButton.closest('.snapshot-picker-row');
        const cacheKey = String((row && row.dataset.cacheKey) || '').trim();
        if (!cacheKey || panel.dataset.busy === '1') {
          return;
        }
        const rowLabel = String((row.querySelector('.snapshot-picker-label') || {}).textContent || '').trim();
        if (typeof globalScope.confirm === 'function') {
          const confirmed = globalScope.confirm(`确定删除这条历史快照吗？删除后不可恢复。\n${rowLabel}`);
          if (!confirmed) {
            return;
          }
        }

        panel.dataset.busy = '1';
        removeButton.disabled = true;
        mutationSerial += 1;
        handle.requestSerial += 1;
        const desktopApi = handle.desktopApi && handle.desktopApi();
        try {
          if (!desktopApi || typeof desktopApi.removeCrawlCacheSnapshot !== 'function') {
            throw new Error('快照删除接口尚未就绪');
          }
          const result = await desktopApi.removeCrawlCacheSnapshot(cacheKey);
          handle.currentItems = Array.isArray(result && result.items) ? result.items : [];
          buildPanelRows(handle, handle.currentItems);
          placePanel(handle);
          syncTriggerLabel(handle);
          const removedCount = Number(result && result.removedCount) || 0;
          logSafe(
            handle.log,
            removedCount > 0 ? 'info' : 'warn',
            removedCount > 0
              ? '已删除历史快照，各板块的快照列表已同步刷新。'
              : '该历史快照已不存在，列表已按后端最新状态刷新。'
          );
          await handle.reload();
          await reloadAllHandles(handle);
          syncTriggerLabel(handle);
        } catch (error) {
          removeButton.disabled = false;
          logSafe(handle.log, 'error', `删除历史快照失败：${handle.errorMessage(error)}`);
        } finally {
          panel.dataset.busy = '';
        }
        return;
      }

      const row = target.closest('.snapshot-picker-row');
      if (row) {
        // 先关面板再应用选择：宿主把 select 包在 label 里时，阻止默认行为
        // 避免点击行触发 label 的控件激活；任何选择异常也不能让面板悬停。
        event.preventDefault();
        event.stopPropagation();
        closePanel(handle);
        const item = handle.currentItems.find(
          (entry) => String((entry && entry.cacheKey) || '') === String(row.dataset.cacheKey || '')
        );
        if (item) {
          try {
            applySelection(handle, item);
          } catch (selectionError) {
            logSafe(handle.log, 'warn', `选择历史快照失败：${handle.errorMessage(selectionError)}`);
          }
        }
      }
    });
  }

  async function openPanel(handle) {
    const panel = handle.panel;
    const requestSerial = ++handle.requestSerial;
    const mutationAtStart = mutationSerial;
    const desktopApi = handle.desktopApi && handle.desktopApi();
    if (panel.parentNode !== document.body) {
      document.body.appendChild(panel);
    }
    panel.hidden = false;
    panel.style.width = `${Math.max(handle.trigger.getBoundingClientRect().width, 320)}px`;

    const list = panel.querySelector('.snapshot-picker-list');
    if (list) {
      list.replaceChildren();
      const loading = document.createElement('p');
      loading.className = 'snapshot-picker-empty';
      loading.textContent = '正在读取历史快照…';
      list.appendChild(loading);
    }
    placePanel(handle);

    try {
      if (!desktopApi || typeof desktopApi.listCrawlCacheSnapshots !== 'function') {
        throw new Error('快照列表接口尚未就绪');
      }
      const result = await desktopApi.listCrawlCacheSnapshots();
      if (requestSerial !== handle.requestSerial || mutationAtStart !== mutationSerial) {
        return;
      }
      handle.currentItems = Array.isArray(result && result.items) ? result.items : [];
      buildPanelRows(handle, handle.currentItems);
      placePanel(handle);
    } catch (error) {
      handle.currentItems = [];
      if (list) {
        list.replaceChildren();
        const failed = document.createElement('p');
        failed.className = 'snapshot-picker-empty';
        failed.textContent = `读取历史快照失败：${handle.errorMessage(error)}`;
        list.appendChild(failed);
      }
      placePanel(handle);
    }
  }

  /**
   * attach() replaces one snapshot <select> with a deletable custom dropdown.
   * Options:
   * - select: the host <select>; it stays as the hidden value holder
   * - desktopApi: () => api object
   * - itemValue: (item) => select value for row clicks (default selection path)
   * - onSelectItem: optional (item) => void for index-based selects
   * - formatItem: optional (item) => row label
   * - reload: async () => void; refreshes the host select options
   * - log: optional (level, message) => void
   */
  function attach(options) {
    const select = options && options.select;
    if (!select || !(select instanceof Element)) {
      return null;
    }
    if (attachedSelects.includes(select)) {
      return null;
    }
    const desktopApi = options.desktopApi || (() => null);
    const formatItem =
      typeof options.formatItem === 'function' ? options.formatItem : defaultFormatSnapshotItem;
    const reload = typeof options.reload === 'function' ? options.reload : async () => {};
    const errorMessage =
      (options && options.errorMessage) ||
      ((error) => (error && error.message ? error.message : String(error || '未知错误')));

    attachedSelects.push(select);

    const placeholder =
      String((select.options[0] && select.options[0].value === '' && select.options[0].textContent) || '').trim() ||
      '请选择历史快照';

    const widget = document.createElement('div');
    widget.className = 'snapshot-picker';

    const trigger = document.createElement('button');
    trigger.type = 'button';
    trigger.className = 'snapshot-picker-trigger';
    trigger.title = '点击展开历史快照列表，可逐条选择或删除';

    const panel = document.createElement('div');
    panel.className = 'snapshot-picker-panel';
    panel.hidden = true;
    panel.innerHTML = '<div class="snapshot-picker-list"></div>';

    widget.appendChild(trigger);
    widget.appendChild(panel);
    select.parentNode.insertBefore(widget, select);
    select.hidden = true;

    const handle = {
      select,
      trigger,
      panel,
      placeholder,
      desktopApi,
      formatItem,
      reload,
      log: options.log,
      errorMessage,
      itemValue: typeof options.itemValue === 'function' ? options.itemValue : null,
      onSelectItem: typeof options.onSelectItem === 'function' ? options.onSelectItem : null,
      currentItems: [],
      requestSerial: 0,
      closePanel() {
        closePanel(handle);
      }
    };
    handles.push(handle);

    syncTriggerLabel(handle);

    trigger.addEventListener('click', async (event) => {
      // 宿主可能把 select 放在 label 内，阻止默认行为避免焦点被抢占。
      event.preventDefault();
      event.stopPropagation();
      if (panel.hidden) {
        await openPanel(handle);
      } else {
        closePanel(handle);
      }
    });

    select.addEventListener('change', () => syncTriggerLabel(handle));

    bindPanelEvents(handle);

    document.addEventListener('click', (event) => {
      if (panel.hidden) {
        return;
      }
      if (widget.contains(event.target) || panel.contains(event.target)) {
        return;
      }
      closePanel(handle);
    });

    document.addEventListener('keydown', (event) => {
      if (event.key === 'Escape' && !panel.hidden) {
        closePanel(handle);
      }
    });

    // 面板是 fixed 定位，外部滚动或缩放后坐标会脱节：监听捕获阶段（内层
    // 滚动容器不冒泡 scroll 事件），滚动发生在面板列表内部时放行正常
    // 滚动，只有外部滚动才收起面板。
    window.addEventListener(
      'scroll',
      (event) => {
        if (panel.hidden) {
          return;
        }
        const scrollTarget = event.target;
        if (scrollTarget instanceof Node && panel.contains(scrollTarget)) {
          return;
        }
        closePanel(handle);
      },
      true
    );
    window.addEventListener('resize', () => {
      if (!panel.hidden) {
        closePanel(handle);
      }
    });

    return handle;
  }

  globalScope.desktopSnapshotHistoryManager = { attach };
})(typeof globalThis !== 'undefined' ? globalThis : window);
