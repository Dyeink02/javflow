// Shared helpers for the crawler state renderers.
// Keep normalization and tiny DOM builders centralized so state renderers only
// own panel-specific presentation decisions.
//
// Ownership summary:
// 1) normalize small item/path/date values for state-oriented views
// 2) build tiny shared DOM fragments like chips
// 3) keep presentation-only sanitation out of the controllers
//
// These helpers must not grow business semantics for crawl quality/review logic.
//
// File map for maintainers:
// 1) chip/DOM micro-builders
// 2) item/date/path normalization helpers
// 3) failed-detail/review list sanitation helpers
(function initializeStateHelpers(globalScope) {
  function createChip(text, className) {
    const chip = document.createElement('span');
    chip.className = className;
    chip.textContent = text;
    return chip;
  }

  function createEmptyChip(text) {
    return createChip(text, 'task-empty-chip');
  }

  function toSafeArray(value) {
    return Array.isArray(value) ? value : [];
  }

  function normalizeItems(items = [], limit = 12) {
    // Item normalization is presentation hygiene only: trim, dedupe, cap.
    // Business-side filtering/ordering decisions must happen before data
    // reaches these helpers.
    const seen = new Set();
    const normalized = [];

    for (const rawItem of toSafeArray(items)) {
      const item = String(rawItem || '').trim();
      if (!item || seen.has(item)) {
        continue;
      }
      seen.add(item);
      normalized.push(item);
      if (normalized.length >= limit) {
        break;
      }
    }

    return normalized;
  }

  function sanitizeReadyItems(items = [], limit = 12) {
    // Ready-state sanitizers intentionally avoid dedupe/policy changes unless
    // the caller explicitly wants normalized chips instead of raw ready items.
    const normalized = [];
    for (const rawItem of toSafeArray(items)) {
      const item = String(rawItem || '').trim();
      if (!item) {
        continue;
      }
      normalized.push(item);
      if (normalized.length >= limit) {
        break;
      }
    }
    return normalized;
  }

  function normalizeDateText(value) {
    if (!value) {
      return '';
    }
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? String(value) : date.toLocaleString('zh-CN', { hour12: false });
  }

  function normalizeFailedDetails(items = [], limit = 12) {
    // Failed-detail normalization keeps panel rendering deterministic without
    // reclassifying failure semantics in the renderer layer.
    const unique = [];
    const seen = new Set();

    for (const item of toSafeArray(items)) {
      if (!item) {
        continue;
      }

      const itemId = item.item || item.sourceLink || 'unknown';
      const reason = item.reason || '';
      const signature = `${itemId}::${reason}`;
      if (seen.has(signature)) {
        continue;
      }

      seen.add(signature);
      unique.push(item);
      if (unique.length >= limit) {
        break;
      }
    }

    return unique;
  }

  function sanitizeReadyFailedDetails(items = [], limit = 12) {
    const normalized = [];
    for (const item of toSafeArray(items)) {
      if (!item || typeof item !== 'object') {
        continue;
      }
      normalized.push(item);
      if (normalized.length >= limit) {
        break;
      }
    }
    return normalized;
  }

  function replaceChildren(container, nodes) {
    if (!container) {
      return;
    }
    container.replaceChildren(...nodes);
  }

  function buildChipNodes(values, chipClass, emptyText) {
    // Chip-node creation is shared presentation glue only; callers decide which
    // values count as duplicate/filtered/unfinished before they reach here.
    if (!values || values.length === 0) {
      return [createEmptyChip(emptyText)];
    }
    return values.map((item) => createChip(item, chipClass));
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

  function applyMiniStatus(element, status, fallbackText, statusLabels = {}) {
    if (!element) {
      return;
    }

    const normalizedStatus = String(status || 'idle').trim().toLowerCase() || 'idle';
    element.className = `mini-status ${resolveMiniStatusClass(normalizedStatus)}`;
    element.textContent = fallbackText || statusLabels[normalizedStatus] || normalizedStatus;
  }

  globalScope.desktopStateHelpers = {
    applyMiniStatus,
    buildChipNodes,
    createChip,
    createEmptyChip,
    normalizeDateText,
    normalizeFailedDetails,
    normalizeItems,
    normalizePathText,
    replaceChildren,
    resolveMiniStatusClass,
    sanitizeReadyFailedDetails,
    sanitizeReadyItems,
    toSafeArray
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
