// Crawl result history view owns DOM rendering for persisted crawl result
// entries. The controller passes storage-backed data and callbacks so result
// persistence and file-opening actions stay outside the view layer.
//
// Ownership summary:
// 1) render recent-result cards from normalized history items
// 2) expose passive open/delete bindings supplied by the controller
// 3) keep path-row presentation local to the view
//
// Storage, dedupe, and artifact discovery stay outside this file.
//
// File map for maintainers:
// 1) date/path/status formatting helpers
// 2) result-history row/card DOM builders
// 3) list render and empty-state helpers
(function initializeCrawlResultHistoryView(globalScope) {
  const rendererHelpers = globalScope.desktopRendererHelpers || {};
  const clearChildren = rendererHelpers.clearChildren;

  if (!clearChildren) {
    throw new Error('desktopRendererHelpers must be loaded before crawlResultHistoryView');
  }

  function normalizeDateText(value) {
    if (!value) {
      return '';
    }
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? String(value) : date.toLocaleString('zh-CN', { hour12: false });
  }

  function normalizePathText(value, fallback = '尚未生成') {
    const text = String(value || '').trim();
    return text || fallback;
  }

  function resolveMiniStatusClass(status) {
    const normalizedStatus = String(status || 'idle').trim().toLowerCase() || 'idle';
    if (normalizedStatus === 'ok') {
      return 'completed';
    }
    if (normalizedStatus === 'warning') {
      return 'incomplete';
    }
    if (normalizedStatus === 'empty') {
      return 'idle';
    }
    if (normalizedStatus.startsWith('stopped')) {
      return 'stopped';
    }
    return normalizedStatus;
  }

  function createPathActionRow(label, pathValue, exists, callbacks = {}) {
    // Path rows remain deliberately generic so artifact-specific existence rules
    // continue to live in the history controller/model layers.
    const row = document.createElement('div');
    row.className = 'result-history-row';

    const copy = document.createElement('div');
    copy.className = 'result-history-copy';
    const labelNode = document.createElement('span');
    labelNode.textContent = label;
    const pathNode = document.createElement('p');
    pathNode.className = 'ops-path';
    pathNode.textContent = normalizePathText(pathValue);
    pathNode.title = String(pathValue || '').trim();
    copy.appendChild(labelNode);
    copy.appendChild(pathNode);

    const action = document.createElement('button');
    action.type = 'button';
    action.className = 'inline-button';
    action.textContent = '打开';
    action.disabled = !pathValue || !exists;
    if (typeof callbacks.onBindOpenPath === 'function') {
      callbacks.onBindOpenPath(action, pathValue, exists);
    }

    row.appendChild(copy);
    row.appendChild(action);
    return row;
  }

  function renderEmpty(container, message = '等待抓取完成后生成最近结果入口。') {
    if (!container) {
      return;
    }

    clearChildren(container);
    const empty = document.createElement('p');
    empty.className = 'result-history-empty';
    empty.textContent = message;
    container.appendChild(empty);
  }

  // View remains intentionally passive: it renders normalized entries and
  // exposes click hooks, but storage identity, dedupe, and file existence
  // decisions stay in the controller/model layers.
  function renderHistory(container, items, callbacks = {}) {
    if (!container) {
      return;
    }

    const list = Array.isArray(items) ? items : [];
    if (list.length === 0) {
      renderEmpty(container, callbacks.emptyMessage || '等待抓取完成后生成最近结果入口。');
      return;
    }

    clearChildren(container);
    const fragment = document.createDocumentFragment();

    list.forEach((item) => {
      const card = document.createElement('article');
      card.className = 'result-history-card';

      const head = document.createElement('div');
      head.className = 'result-history-head';
      const titleWrap = document.createElement('div');
      const title = document.createElement('strong');
      title.textContent = item.title || '最近结果';
      const meta = document.createElement('p');
      meta.className = 'result-history-meta';
      meta.textContent = normalizeDateText(item.updatedAt);
      titleWrap.appendChild(title);
      titleWrap.appendChild(meta);
      head.appendChild(titleWrap);

      const headRight = document.createElement('div');
      headRight.className = 'result-history-head-right';

      const chip = document.createElement('span');
      chip.className = `mini-status ${resolveMiniStatusClass(item.qualityStatus)}`;
      chip.textContent = item.qualityStatusText || '尚未生成复盘';
      headRight.appendChild(chip);

      const deleteButton = document.createElement('button');
      deleteButton.type = 'button';
      deleteButton.className = 'icon-delete-button';
      deleteButton.title = '删除此记录（不删除本地文件）';
      deleteButton.textContent = '×';
      if (typeof callbacks.onDelete === 'function') {
        deleteButton.addEventListener('click', (event) => {
          event.stopPropagation();
          callbacks.onDelete(item);
        });
      }
      headRight.appendChild(deleteButton);

      head.appendChild(headRight);
      card.appendChild(head);

      if (item.summary) {
        const summary = document.createElement('p');
        summary.className = 'ops-summary';
        summary.textContent = item.summary;
        card.appendChild(summary);
      }

      const rowList = document.createElement('div');
      rowList.className = 'result-history-grid';
      rowList.appendChild(createPathActionRow('输出目录', item.outputDir, item.outputDirExists, callbacks));
      rowList.appendChild(createPathActionRow('filmData.json', item.filmDataPath, item.filmDataExists, callbacks));
      rowList.appendChild(createPathActionRow('磁力文档', item.magnetPath, item.magnetExists, callbacks));
      rowList.appendChild(createPathActionRow('日志目录', item.logDir, item.logDirExists, callbacks));
      rowList.appendChild(
        createPathActionRow('latest-log.txt', item.latestLogPath, item.latestLogExists, callbacks)
      );
      rowList.appendChild(createPathActionRow('复盘报告', item.reportPath, item.reportExists, callbacks));
      card.appendChild(rowList);

      fragment.appendChild(card);
    });

    container.appendChild(fragment);
  }

  globalScope.desktopCrawlResultHistoryView = {
    renderEmpty,
    renderHistory
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
