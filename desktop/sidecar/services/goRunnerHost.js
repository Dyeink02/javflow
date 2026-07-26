// Wrapper that runs the legacy JS scraper runtime inside the Node sidecar
// without requiring the old Electron shell. This is compatibility plumbing,
// not the primary Go-native crawl path.
// compatibility-owner: active crawl-compatible JS runner host; marker=compat-sidecar-go-runner-host
//
// Ownership summary:
// 1) host the archived JS ScraperRunner inside the sidecar process
// 2) mirror compatibility log/state events back through the sidecar log bridge
// 3) stay renderer-facing only when Go already owns task lifecycle
//
// This file must not become a second task-controller policy layer.
//
// File map for maintainers:
// 1) host-local state + runner option shaping
// 2) start/restart/stop compatibility lifecycle
// 3) shutdown + idle bookkeeping

const path = require('path');

function createGoRunnerHost({
  desktopRoot,
  runtimePackage,
  appTitle,
  mainText,
  logBridge
}) {
  // Sidecar-local compatibility bookkeeping only.
  // Prefer the Go-native Wails controller whenever the current runtime does not
  // need this JS wrapper.
  //
  // In `goTaskController` mode the Go layer already owns:
  // 1) session/latest task-log files
  // 2) crawl log-context publication
  // 3) latest-log.txt persistence
  //
  // This compatibility host must therefore stay renderer-only. If it also
  // writes task logs, the Go controller and the Node compatibility runner both
  // append the same crawl events into the same output directory, which shows up
  // as paired duplicate lines in `latest-log.txt`.
  //
  // Host rule:
  // this wrapper may mirror logs/state for compatibility, but it should not
  // become an alternative source of truth for crawl task ownership.
  //
  // Fast troubleshooting split:
  // 1) bug reproduces only when `goTaskController=true` and sidecar is active:
  //    inspect this host before touching `runnerService.js`
  // 2) duplicate or missing compatibility UI logs in Go-task-controller mode:
  //    inspect renderer-only mirroring here and then `taskLogService.js`
  // 3) bug reproduces even without sidecar/compat mode: this file is the wrong
  //    place; inspect the Go bridge/controller path instead
  const state = {
    activeRunner: null,
    activeRunPromise: null,
    currentTaskOutputDir: null,
    lastTaskOutputDir: null
  };

  function queueHostRendererLog(level, message, timestamp = new Date().toISOString()) {
    logBridge.queueRendererLog(level, message, timestamp);
  }

  function getRunnerModule() {
    return require(path.join(desktopRoot, '..', 'dist', 'core', 'scraperRunner.js'));
  }

  function getOutputRuntimeUtilsModule() {
    return require(path.join(desktopRoot, '..', 'dist', 'core', 'outputRuntimeUtils.js'));
  }

  function resolveRuntimeSettings(settings) {
    // Output-dir normalization remains shared with the archived runtime so
    // compatibility runs and older output-folder expectations stay aligned.
    const outputResolution = settings.outputResolved
      ? {
          outputDir: settings.output,
          createdRunDir: false
        }
      : getOutputRuntimeUtilsModule().resolveRunOutputDirectory({
          outputDir: settings.output,
          resumeExisting: Boolean(settings.resumeExisting)
        });

    return {
      outputResolution,
      runtimeSettings: {
        ...settings,
        output: outputResolution.outputDir,
        outputResolved: true
      }
    };
  }

  function getBusyMessage() {
    return String(mainText?.runnerBusy || 'A crawl task is already running.');
  }

  function buildRunnerOptions(settings) {
    queueHostRendererLog(
      'info',
      `go runner options: actressCountFilterThreshold=${Number(settings.actressCountFilterThreshold ?? 0)} cloudflare=${Boolean(settings.cloudflare)} goTaskController=${Boolean(settings.goTaskController)} output=${String(settings.output || '')}`
    );
    return {
      ...settings,
      demoMode: settings.demoMode || runtimePackage.demoMode || 'aed',
      demoLabel: settings.demoLabel || runtimePackage.demoLabel || 'AED',
      productDisplayName: runtimePackage.productDisplayName || appTitle,
      useProgressBars: false,
      handleSignals: false
    };
  }

  function isRunning() {
    return Boolean(state.activeRunner);
  }

  async function waitForIdle() {
    // Idle waiting is local compatibility bookkeeping only. It prevents restart
    // races inside the sidecar host; it is not the canonical crawl lifecycle.
    if (!state.activeRunPromise) {
      return;
    }
    await state.activeRunPromise.catch(() => {});
  }

  async function start(settings = {}) {
    if (state.activeRunner) {
      throw new Error(getBusyMessage());
    }

    const { runtimeSettings } = resolveRuntimeSettings(settings);

    // Reserve the slot before loading the legacy runner so concurrent start
    // calls cannot both pass the idle check and create two runners.
    state.activeRunner = { _placeholder: true };
    try {
      const { default: ScraperRunner } = getRunnerModule();

      await logBridge.flushDesktopPipelines();
      state.currentTaskOutputDir = runtimeSettings.output;
      state.lastTaskOutputDir = runtimeSettings.output;
      state.activeRunner = new ScraperRunner(buildRunnerOptions(runtimeSettings));
    } catch (err) {
      state.activeRunner = null;
      throw err;
    }

    state.activeRunner.on('log', (entry) => {
      logBridge.queueRendererLogEntry(entry);
    });

    state.activeRunner.on('state', (taskState) => {
      logBridge.queueRendererState(taskState);
    });

    state.activeRunPromise = Promise.resolve()
      .then(() => state.activeRunner.run())
      .catch((error) => {
        const message = error instanceof Error ? error.message : String(error);
        queueHostRendererLog('error', message);
        logBridge.queueRendererState({
          status: 'error',
          message
        });
      })
      .finally(async () => {
        await logBridge.flushDesktopPipelines();
        state.activeRunner = null;
        state.activeRunPromise = null;
        state.currentTaskOutputDir = null;
      });

    return { ok: true };
  }

  async function restart(settings = {}) {
    const nextSettings = {
      ...settings,
      resumeExisting: true
    };

    if (state.activeRunner) {
      try {
        await stop();
      } catch (err) {
        // If stop fails, clear local compatibility state so a later restart
        // does not get blocked by stale in-memory bookkeeping.
        state.activeRunner = null;
        state.activeRunPromise = null;
        throw err;
      }
      await waitForIdle();
      await start(nextSettings);
      return { ok: true, restarting: true };
    }

    await start(nextSettings);
    return { ok: true, restarting: false };
  }

  async function stop() {
    if (!state.activeRunner) {
      return { ok: true };
    }

    await state.activeRunner.stop();
    return { ok: true };
  }

  async function shutdown() {
    await stop().catch(() => {});
    await waitForIdle();
    await logBridge.flushDesktopPipelines();
    return { ok: true };
  }

  return {
    start,
    restart,
    stop,
    shutdown,
    isRunning
  };
}

module.exports = {
  createGoRunnerHost
};
