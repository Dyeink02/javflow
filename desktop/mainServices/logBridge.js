// Shared task/session log adapter for legacy desktop modules and the Node
// sidecar compatibility path.
// compatibility-owner: active crawl-compatible log writer bridge; marker=compat-mainservices-log-bridge
//
// This file sits on a sensitive compatibility boundary: file naming, flush
// timing, UTF-8 BOM handling, and renderer batching here directly affect field
// troubleshooting. Keep behavior aligned with the Go-native logging pipeline
// unless a sidecar-specific reason is documented next to the change.
//
// Ownership summary:
// 1) create/update compatibility task/session log files
// 2) batch renderer log/state events to reduce UI churn
// 3) preserve UTF-8 BOM/file-writing rules expected by historical desktop
//    tooling and current sidecar troubleshooting workflows
//
// File map for maintainers:
// 1) UTF-8/file helpers and log-context paths
// 2) renderer batching queues and flush helpers
// 3) session/task log formatting and noisy-log filtering
// 4) task-log artifact lifecycle helpers

function createLogBridge({
  fs,
  path,
  sendToRenderer,
  mainText,
  statusLabels,
  logFilterPatterns,
  fileNames,
  appTitle,
  appVersion,
  appDemoLabel
}) {
  // If a bug is purely about Go-native task logs written from the main Wails
  // path, verify the Go logger first before changing this bridge.

  let currentTaskLogDir = null;
  let currentTaskLogPath = null;
  let currentLatestLogPath = null;
  let lastTaskStateSignature = '';
  let lastTaskStateAt = 0;
  let pendingTaskLogLines = [];
  let pendingTaskLogFlushTimer = null;
  let taskLogFlushPromise = Promise.resolve();
  let pendingRendererLogEntries = [];
  let pendingRendererLogFlushTimer = null;
  let pendingRendererState = null;
  let pendingRendererStateFlushTimer = null;

  const SESSION_LOG_LEVELS = new Set(['info', 'warn', 'error']);
  const TASK_LOG_BATCH_SIZE = 24;
  const TASK_LOG_FLUSH_INTERVAL_MS = 180;
  const RENDERER_LOG_BATCH_SIZE = 40;
  const RENDERER_LOG_FLUSH_INTERVAL_MS = 180;
  const RENDERER_STATE_FLUSH_INTERVAL_MS = 320;
  const noisyLogPatterns = Array.isArray(logFilterPatterns?.noisy) ? logFilterPatterns.noisy : [];
  const keyLogPatterns = Array.isArray(logFilterPatterns?.key) ? logFilterPatterns.key : [];
  const localizedStatusLabels = statusLabels || {};
  const logLevelLabels = mainText?.logLevelLabels || {};
  const logPrefixLabels = mainText?.logPrefixLabels || {};
  const UTF8_BOM = '\uFEFF';

  function withUTF8BOM(content) {
    const text = String(content ?? '');
    return text.startsWith(UTF8_BOM) ? text : `${UTF8_BOM}${text}`;
  }

  function writeUTF8TextFile(filePath, content) {
    fs.mkdirSync(path.dirname(filePath), { recursive: true });
    fs.writeFileSync(filePath, withUTF8BOM(content), 'utf8');
  }

  async function appendUTF8TextFile(filePath, content) {
    const text = String(content ?? '');

    try {
      const stats = await fs.promises.stat(filePath);
      if (stats.size === 0) {
        await fs.promises.appendFile(filePath, withUTF8BOM(text), 'utf8');
        return;
      }
    } catch (error) {
      if (error?.code !== 'ENOENT') {
        throw error;
      }
      await fs.promises.mkdir(path.dirname(filePath), { recursive: true });
      await fs.promises.writeFile(filePath, withUTF8BOM(text), 'utf8');
      return;
    }

    await fs.promises.appendFile(filePath, text, 'utf8');
  }

  function padNumber(value) {
    return String(value).padStart(2, '0');
  }

  function formatLogStamp(date = new Date()) {
    return `${date.getFullYear()}${padNumber(date.getMonth() + 1)}${padNumber(date.getDate())}-${padNumber(
      date.getHours()
    )}${padNumber(date.getMinutes())}${padNumber(date.getSeconds())}`;
  }

  function formatLogLineStamp(value) {
    if (!value) {
      const now = new Date();
      return `${now.getFullYear()}-${padNumber(now.getMonth() + 1)}-${padNumber(now.getDate())} ${padNumber(
        now.getHours()
      )}:${padNumber(now.getMinutes())}:${padNumber(now.getSeconds())}`;
    }

    const date = new Date(value);
    if (Number.isNaN(date.getTime())) {
      return String(value);
    }

    return `${date.getFullYear()}-${padNumber(date.getMonth() + 1)}-${padNumber(date.getDate())} ${padNumber(
      date.getHours()
    )}:${padNumber(date.getMinutes())}:${padNumber(date.getSeconds())}`;
  }

  function getLogContext() {
    return {
      logDir: currentTaskLogDir,
      sessionLogPath: currentTaskLogPath,
      latestLogPath: currentLatestLogPath
    };
  }

  function flushRendererLogBatch() {
    if (pendingRendererLogFlushTimer) {
      clearTimeout(pendingRendererLogFlushTimer);
      pendingRendererLogFlushTimer = null;
    }

    if (pendingRendererLogEntries.length === 0) {
      return;
    }

    const batch = pendingRendererLogEntries;
    pendingRendererLogEntries = [];
    sendToRenderer('runner:log', batch);
  }

  function scheduleRendererLogFlush() {
    if (pendingRendererLogEntries.length >= RENDERER_LOG_BATCH_SIZE) {
      flushRendererLogBatch();
      return;
    }

    if (!pendingRendererLogFlushTimer) {
      pendingRendererLogFlushTimer = setTimeout(flushRendererLogBatch, RENDERER_LOG_FLUSH_INTERVAL_MS);
    }
  }

  function getLocalizedLogLevel(level) {
    return logLevelLabels[String(level || '').toLowerCase()] || String(level || '信息').toUpperCase();
  }

  function localizeLogPrefix(message) {
    let normalizedMessage = String(message || '');

    Object.entries(logPrefixLabels).forEach(([sourcePrefix, localizedPrefix]) => {
      if (normalizedMessage.startsWith(sourcePrefix)) {
        normalizedMessage = `${localizedPrefix}${normalizedMessage.slice(sourcePrefix.length).trimStart()}`;
      }
    });

    return normalizedMessage;
  }

  function formatSessionLogMessage(message) {
    const normalizedMessage = localizeLogPrefix(String(message || '').trim());
    if (!normalizedMessage) {
      return '';
    }

    if (String(mainText.keyLogPrefix || '') && normalizedMessage.startsWith(String(mainText.keyLogPrefix))) {
      return normalizedMessage;
    }

    if (keyLogPatterns.some((pattern) => normalizedMessage.includes(pattern))) {
      return `${mainText.keyLogPrefix}${normalizedMessage}`;
    }

    return normalizedMessage;
  }

  function shouldWriteSessionLogEntry(entry) {
    const level = String(entry?.level || 'info').toLowerCase();
    if (!SESSION_LOG_LEVELS.has(level)) {
      return false;
    }

    const message = String(entry?.message || '');
    return !noisyLogPatterns.some((pattern) => message.includes(pattern));
  }

  function queueRendererLogEntry(entry) {
    // Renderer batching exists to keep the compatibility UI responsive while
    // still preserving operator-visible key lines. Avoid bypassing this unless
    // a UI timing issue is proven.
    //
    // This is the renderer-log choke point for the compatibility lane:
    // filtering, localization, and batch scheduling all converge here.
    if (!shouldWriteSessionLogEntry(entry)) {
      return;
    }

    pendingRendererLogEntries.push({
      ...entry,
      message: formatSessionLogMessage(entry.message)
    });
    scheduleRendererLogFlush();
  }

  function queueRendererLog(level, message, timestamp = new Date().toISOString()) {
    queueRendererLogEntry({
      level,
      message,
      timestamp
    });
  }

  function flushRendererState() {
    if (pendingRendererStateFlushTimer) {
      clearTimeout(pendingRendererStateFlushTimer);
      pendingRendererStateFlushTimer = null;
    }

    if (!pendingRendererState) {
      return;
    }

    const state = pendingRendererState;
    pendingRendererState = null;
    sendToRenderer('runner:state', state);
  }

  function queueRendererState(state) {
    // Renderer state batching is a compatibility projection only. The canonical
    // task lifecycle state lives in the active runner/Go controller.
    if (!state) {
      return;
    }

    const isFinalState = ['completed', 'error', 'stopped', 'incomplete'].includes(
      String(state.status || '').toLowerCase()
    );
    pendingRendererState = state;

    if (isFinalState) {
      flushRendererState();
      return;
    }

    if (!pendingRendererStateFlushTimer) {
      pendingRendererStateFlushTimer = setTimeout(flushRendererState, RENDERER_STATE_FLUSH_INTERVAL_MS);
    }
  }

  async function flushTaskLogBuffer() {
    // Task-log flushing is the most sensitive file I/O point in this module:
    // both session log and latest-log must stay UTF-8 aligned, and changes here
    // can directly create duplicate/missing lines during troubleshooting.
    if (pendingTaskLogFlushTimer) {
      clearTimeout(pendingTaskLogFlushTimer);
      pendingTaskLogFlushTimer = null;
    }

    if (!currentTaskLogPath || !currentLatestLogPath || pendingTaskLogLines.length === 0) {
      return taskLogFlushPromise;
    }

    const taskLogPath = currentTaskLogPath;
    const latestLogPath = currentLatestLogPath;
    const lines = pendingTaskLogLines;
    pendingTaskLogLines = [];
    const payload = `${lines.join('\r\n')}\r\n`;

    taskLogFlushPromise = taskLogFlushPromise
      .then(() =>
        Promise.all([
          appendUTF8TextFile(taskLogPath, payload),
          appendUTF8TextFile(latestLogPath, payload)
        ])
      )
      .catch((error) => {
        console.warn('flushTaskLogBuffer failed:', error);
      });

    return taskLogFlushPromise;
  }

  function scheduleTaskLogFlush() {
    if (pendingTaskLogLines.length >= TASK_LOG_BATCH_SIZE) {
      void flushTaskLogBuffer();
      return;
    }

    if (!pendingTaskLogFlushTimer) {
      pendingTaskLogFlushTimer = setTimeout(() => {
        void flushTaskLogBuffer();
      }, TASK_LOG_FLUSH_INTERVAL_MS);
    }
  }

  async function flushDesktopPipelines() {
    flushRendererState();
    flushRendererLogBatch();
    await flushTaskLogBuffer();
  }

  function appendTaskLogLine(line) {
    if (!currentTaskLogPath || !currentLatestLogPath) {
      return;
    }

    pendingTaskLogLines.push(String(line || ''));
    scheduleTaskLogFlush();
  }

  function writeTaskLog(level, message, timestamp) {
    appendTaskLogLine(
      `[${formatLogLineStamp(timestamp)}] ${getLocalizedLogLevel(level)}: ${String(message || '')}`
    );
  }

  function appendTaskLogEntry(entry) {
    if (!shouldWriteSessionLogEntry(entry)) {
      return;
    }

    writeTaskLog(entry.level || 'info', formatSessionLogMessage(entry.message), entry.timestamp);
  }

  function shouldWriteTaskStateEntry(state) {
    const status = String(state?.status || 'unknown').toLowerCase();
    const message = String(state?.message || '').trim();
    const now = Date.now();
    const signature = `${status}|${message}`;
    const isFinalState = ['completed', 'error', 'stopped', 'incomplete'].includes(status);

    if (isFinalState) {
      lastTaskStateSignature = signature;
      lastTaskStateAt = now;
      return true;
    }

    if (signature === lastTaskStateSignature && now - lastTaskStateAt < 2000) {
      return false;
    }

    lastTaskStateSignature = signature;
    lastTaskStateAt = now;
    return true;
  }

  function appendTaskStateEntry(state) {
    if (!shouldWriteTaskStateEntry(state)) {
      return;
    }

    const status = String(state.status || 'unknown').toLowerCase();
    const message = formatSessionLogMessage(state.message || '');
    const localizedStatus = localizedStatusLabels[status] || status;
    appendTaskLogLine(`[${formatLogLineStamp()}] ${mainText.stateLogPrefix || '状态'}(${localizedStatus}): ${message}`);
  }

  function initializeTaskLogFiles(outputDir, settings) {
    // Filename/header shape here is part of the historical desktop log artifact
    // contract. Keep the shape stable unless every compatibility reader is
    // updated together.
    pendingTaskLogLines = [];
    pendingRendererLogEntries = [];
    pendingRendererState = null;
    currentTaskLogDir = path.join(outputDir, 'logs');
    currentTaskLogPath = path.join(currentTaskLogDir, `${fileNames.taskLogPrefix}-${formatLogStamp()}.txt`);
    currentLatestLogPath = path.join(currentTaskLogDir, fileNames.latestLogFilename);

    const headerLines = [
      `${appTitle}${appDemoLabel ? ` ${appDemoLabel}` : ''} ${mainText.taskLogTitleSuffix}`,
      `${mainText.versionLabel}: ${appVersion}`,
      `${mainText.startTimeLabel}: ${formatLogLineStamp()}`,
      `${mainText.outputLabel}: ${outputDir}`,
      `${mainText.baseLabel}: ${settings.base || ''}`,
      `${mainText.runtimeSchemeLabel}: ${settings.demoLabel || settings.demoMode || appDemoLabel || 'AED'}`,
      '------------------------------------------------------------'
    ];
    const header = `${headerLines.join('\r\n')}\r\n`;

    writeUTF8TextFile(currentTaskLogPath, header);
    writeUTF8TextFile(currentLatestLogPath, header);
    lastTaskStateSignature = '';
    lastTaskStateAt = 0;
    sendToRenderer('runner:log-context', getLogContext());
  }

  return {
    getLogContext,
    flushDesktopPipelines,
    queueRendererLog,
    queueRendererLogEntry,
    queueRendererState,
    appendTaskLogEntry,
    appendTaskStateEntry,
    appendTaskLogLine,
    writeTaskLog,
    initializeTaskLogFiles
  };
}

module.exports = {
  createLogBridge
};
