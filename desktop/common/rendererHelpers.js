// Shared renderer helpers for the active desktop frontend.
// These helpers keep controller files focused on module orchestration instead
// of repeating the same DOM, integer, and async-click boilerplate.
//
// Ownership summary:
// 1) provide generic DOM/list/local-storage helpers
// 2) provide shared log-line append/batching helpers for renderer UIs
// 3) provide small async event binding utilities
//
// This file must stay business-agnostic. Crawl, organizer, and subscription
// semantics belong to their own controllers and model helpers.
//
// File map for maintainers:
// 1) generic error/integer/storage helpers
// 2) shared log-line append and batching helpers
// 3) async click/event binding utilities
(function registerRendererHelpers(globalScope) {
  function getErrorMessage(error, fallback = '未知错误') {
    if (error instanceof Error && String(error.message || '').trim()) {
      return error.message;
    }
    const text = String(error || '').trim();
    return text || fallback;
  }

  const asyncClickRegistry = new WeakMap();

  function toSafeInteger(value, fallback = 0, minimum = 0, maximum = Number.POSITIVE_INFINITY) {
    const parsed = Number.parseInt(String(value ?? '').trim(), 10);
    const normalized = Number.isFinite(parsed) ? parsed : fallback;
    return Math.max(minimum, Math.min(maximum, normalized));
  }

  function clearChildren(node) {
    if (node) {
      node.replaceChildren();
    }
  }

  function createOption(value, text) {
    const option = document.createElement('option');
    option.value = String(value);
    option.textContent = text;
    return option;
  }

  function createElement(tagName, options = {}) {
    const element = document.createElement(String(tagName || 'div'));
    const {
      id,
      className,
      textContent,
      html,
      type,
      title,
      alt,
      src,
      value,
      placeholder,
      role,
      tabIndex,
      ariaLabel,
      ariaModal,
      style,
      cssText,
      attributes,
      children,
      parent
    } = options;

    if (id !== undefined) element.id = id;
    if (className !== undefined) element.className = className;
    if (textContent !== undefined) element.textContent = textContent;
    if (html !== undefined) element.innerHTML = html;
    if (type !== undefined) element.type = type;
    if (title !== undefined) element.title = title;
    if (alt !== undefined) element.alt = alt;
    if (src !== undefined) element.src = src;
    if (value !== undefined) element.value = value;
    if (placeholder !== undefined) element.placeholder = placeholder;
    if (role !== undefined) element.setAttribute('role', role);
    if (tabIndex !== undefined) element.tabIndex = tabIndex;
    if (ariaLabel !== undefined) element.setAttribute('aria-label', ariaLabel);
    if (ariaModal !== undefined) element.setAttribute('aria-modal', String(ariaModal));
    if (cssText !== undefined) element.style.cssText = cssText;
    if (style && typeof style === 'object') {
      Object.entries(style).forEach(([key, value]) => {
        element.style[key] = value;
      });
    }
    if (attributes && typeof attributes === 'object') {
      Object.entries(attributes).forEach(([key, value]) => {
        element.setAttribute(key, value);
      });
    }
    if (Array.isArray(children)) {
      children.forEach((child) => element.appendChild(child));
    }
    if (parent) {
      parent.appendChild(element);
    }
    return element;
  }

  function getElementById(id) {
    return document.getElementById(id);
  }

  function querySelector(scope, selector) {
    if (typeof scope === 'string') {
      return document.querySelector(scope);
    }
    return scope ? scope.querySelector(selector) : null;
  }

  function querySelectorAll(scope, selector) {
    if (typeof scope === 'string') {
      return Array.from(document.querySelectorAll(scope));
    }
    return scope ? Array.from(scope.querySelectorAll(selector)) : [];
  }

  function safeLocalStorageGet(key, fallbackValue = '') {
    // Local-storage access is centralized here because browser/runtime
    // restrictions differ between test, Electron-legacy, and Wails contexts.
    try {
      const value = globalThis.localStorage.getItem(key);
      return value == null ? fallbackValue : value;
    } catch {
      return fallbackValue;
    }
  }

  function safeLocalStorageSet(key, value) {
    try {
      globalThis.localStorage.setItem(key, value);
    } catch {
      // Ignore localStorage write failures in restrictive runtimes.
    }
  }

  function appendTimestampedLogLine(logView, level, message, timestamp, options = {}) {
    // Immediate append is still useful for small, already-batched call sites.
    // Larger/high-frequency streams should prefer createBufferedLogAppender.
    if (!logView) {
      return null;
    }

    const {
      tagName = 'p',
      className = 'log-line',
      maxLines = 0
    } = options;
    const line = document.createElement(tagName);
    const date = timestamp ? new Date(timestamp) : new Date();
    const timeLabel = date.toLocaleTimeString('zh-CN', { hour12: false });

    line.className = `${className} ${level || 'info'}`.trim();
    line.textContent = `[${timeLabel}] ${String(message || '').trim()}`;
    logView.appendChild(line);

    if (Number.isFinite(maxLines) && maxLines > 0) {
      while (logView.childElementCount > maxLines) {
        logView.removeChild(logView.firstElementChild);
      }
    }

    logView.scrollTop = logView.scrollHeight;
    return line;
  }

  function createBufferedLogAppender(options = {}) {
    // Buffered log appending belongs here so feature controllers share one UI
    // batching rule instead of each managing their own micro-queue behavior.
    const {
      logView,
      tagName = 'p',
      className = 'log-line',
      maxLines = 0,
      flushDelayMs = 16
    } = options;
    let queuedLines = [];
    let flushHandle = null;
    let flushHandleKind = '';
    let flushScheduled = false;

    function clearScheduledFlush() {
      if (flushHandle == null) {
        return;
      }

      if (flushHandleKind === 'raf' && typeof globalScope.cancelAnimationFrame === 'function') {
        globalScope.cancelAnimationFrame(flushHandle);
      } else {
        globalScope.clearTimeout(flushHandle);
      }
      flushHandle = null;
      flushHandleKind = '';
      flushScheduled = false;
    }

    function flush() {
      flushHandle = null;
      flushHandleKind = '';
      flushScheduled = false;

      if (!logView || queuedLines.length === 0) {
        queuedLines = [];
        return;
      }

      const shouldStickToBottom =
        logView.scrollHeight - logView.scrollTop - logView.clientHeight <= Math.max(logView.clientHeight * 0.25, 48);
      const fragment = document.createDocumentFragment();
      const batch = queuedLines;
      queuedLines = [];

      batch.forEach((item) => {
        const line = document.createElement(tagName);
        const date = item.timestamp ? new Date(item.timestamp) : new Date();
        const timeLabel = date.toLocaleTimeString('zh-CN', { hour12: false });
        line.className = `${className} ${item.level || 'info'}`.trim();
        line.textContent = `[${timeLabel}] ${String(item.message || '').trim()}`;
        fragment.appendChild(line);
      });

      logView.appendChild(fragment);

      if (Number.isFinite(maxLines) && maxLines > 0) {
        while (logView.childElementCount > maxLines) {
          logView.removeChild(logView.firstElementChild);
        }
      }

      if (shouldStickToBottom) {
        logView.scrollTop = logView.scrollHeight;
      }
    }

    function scheduleFlush() {
      if (flushScheduled) {
        return;
      }

      flushScheduled = true;
      if (typeof globalScope.requestAnimationFrame === 'function') {
        flushHandleKind = 'raf';
        flushHandle = globalScope.requestAnimationFrame(flush);
        return;
      }

      flushHandleKind = 'timeout';
      flushHandle = globalScope.setTimeout(flush, flushDelayMs);
    }

    function append(level, message, timestamp) {
      queuedLines.push({
        level: String(level || 'info').toLowerCase(),
        message: String(message || '').trim(),
        timestamp
      });
      scheduleFlush();
    }

    function clear() {
      queuedLines = [];
      clearScheduledFlush();
      clearChildren(logView);
    }

    return {
      append,
      clear,
      flush
    };
  }

  function bindAsyncClick(button, handler, options = {}) {
    if (!button) {
      return;
    }

    // Prevent duplicate listeners when controllers re-bootstrap (e.g. workspace
    // switch disposes and re-runs bootstrap). Each button should bind once.
    if (asyncClickRegistry.has(button)) {
      return;
    }
    asyncClickRegistry.set(button, true);

    // Async click binding stays intentionally small: it standardizes the common
    // click->await->error flow, but feature-specific side effects stay outside.
    const { onError, fallbackErrorHandler } = options;
    button.addEventListener('click', async () => {
      try {
        await handler();
      } catch (error) {
        if (typeof onError === 'function') {
          onError(error);
          return;
        }
        if (typeof fallbackErrorHandler === 'function') {
          fallbackErrorHandler(error);
        }
      }
    });
  }

  globalScope.desktopRendererHelpers = {
    appendTimestampedLogLine,
    createBufferedLogAppender,
    bindAsyncClick,
    clearChildren,
    createElement,
    createOption,
    getElementById,
    getErrorMessage,
    querySelector,
    querySelectorAll,
    safeLocalStorageGet,
    safeLocalStorageSet,
    toSafeInteger
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
