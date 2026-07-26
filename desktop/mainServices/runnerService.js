// Legacy crawl runtime shared by the Node sidecar compatibility path.
// compatibility-owner: active crawl-compatible JS runner service; marker=compat-mainservices-runner-service
// The default Wails crawl controller now lives in Go under
// wails-shell/internal/{crawltask,crawlrunner}. Keep behavior changes here
// narrowly scoped to sidecar compatibility and legacy JS runner integration.
//
// Ownership summary:
// 1) host the archived JS ScraperRunner lifecycle for compatibility sessions
// 2) bridge compatibility logs/state through the shared logBridge surface
// 3) keep Go-owned and JS-owned task lifecycle branches explicit in one file
//
// Important maintenance boundary:
// this file is still active for the Cloudflare / age-check compatibility lane.
// Treat cleanup here as high-risk unless the change is comment-only or a
// strictly local readability improvement.
//
// File map for maintainers:
// 1) runtime module resolution + reminder helpers
// 2) start/restart ownership split for Go-task-controller vs legacy JS lifecycle
// 3) stop/before-quit/update-url compatibility helpers

function createRunnerService({
  state,
  app,
  dialog,
  Notification,
  path,
  desktopRoot,
  runtimePackage,
  appTitle,
  appVersion,
  appDemoLabel,
  mainText,
  windowService,
  settingsStore,
  logBridge
}) {
  // Archived Electron compatibility service. In the current product the normal
  // crawl path is the Go-native Wails controller, so investigate this module
  // only after confirming the app is running through the legacy JS runtime.
  function getRunnerModule() {
    return require(path.join(desktopRoot, '..', 'dist', 'core', 'scraperRunner.js'));
  }

  // Output-path resolution still lives in the historical JS runtime package for
  // this compatibility lane. Keep the import local so current Wails incidents
  // are less likely to start from the wrong runtime layer.
  function getOutputRuntimeUtilsModule() {
    return require(path.join(desktopRoot, '..', 'dist', 'core', 'outputRuntimeUtils.js'));
  }

  // Compatibility reminder surface for legacy desktop runs. The Go-native crawl
  // path owns its own state/report flow and should not be debugged from here.
  function showReminder(taskState) {
    const title = appTitle;
    const body = taskState.message || mainText.reminderFallback;

    if (Notification.isSupported()) {
      new Notification({ title, body }).show();
      return;
    }

    const mainWindow = windowService.getWindow();
    if (mainWindow && !mainWindow.isDestroyed()) {
      void dialog.showMessageBox(mainWindow, {
        type: taskState.status === 'completed' ? 'info' : taskState.status === 'error' ? 'error' : 'warning',
        title,
        message: body
      });
    }
  }

  function shouldUseLocalDesktopControl(useGoTaskController) {
    // Go task control moves task lifecycle ownership away from this JS layer.
    // In that mode the service is only responsible for compatibility launch.
    return !useGoTaskController;
  }

  function queueCompatRendererLog(level, message, timestamp = new Date().toISOString()) {
    // UI log mirroring for the compatibility lane intentionally funnels through
    // logBridge so renderer batching rules stay consistent with other JS events.
    logBridge.queueRendererLog(level, message, timestamp);
  }

  // startRunner is the main compatibility fork:
  // 1) resolve output path using the historical JS helper
  // 2) either own logging/state locally, or defer lifecycle ownership to Go
  // 3) boot the archived ScraperRunner and bridge its events back outward
  async function startRunner(settings) {
    if (state.activeRunner) {
      throw new Error(mainText.runnerBusy);
    }

    // The single most important branch in this file:
    // - `true`: Go already owns task lifecycle and this service becomes a
    //   compatibility launch/log bridge only
    // - `false`: this JS service still owns output path setup, logs, restarts,
    //   and final reminder behavior
    const useGoTaskController = Boolean(settings.goTaskController);
    const useLocalDesktopControl = shouldUseLocalDesktopControl(useGoTaskController);
    const outputResolution = settings.outputResolved
      ? {
          outputDir: settings.output,
          createdRunDir: false
        }
      : getOutputRuntimeUtilsModule().resolveRunOutputDirectory({
          outputDir: settings.output,
          resumeExisting: Boolean(settings.resumeExisting)
        });
    const runtimeSettings = {
      ...settings,
      output: outputResolution.outputDir,
      outputResolved: true
    };
    state.goTaskControllerActive = useGoTaskController;

    await logBridge.flushDesktopPipelines();
    if (useLocalDesktopControl) {
      settingsStore.saveSettings(settings);
    }
    state.currentTaskOutputDir = runtimeSettings.output;
    state.lastTaskOutputDir = runtimeSettings.output;
    if (useLocalDesktopControl) {
      logBridge.initializeTaskLogFiles(runtimeSettings.output, runtimeSettings);
      logBridge.writeTaskLog('info', `${mainText.taskLogCreatedPrefix}${logBridge.getLogContext().sessionLogPath}`);
    }
    if (outputResolution.createdRunDir && useLocalDesktopControl) {
      const outputMessage = `检测到输出目录已有历史结果，本次任务已自动切换到独立输出目录：${runtimeSettings.output}`;
      logBridge.writeTaskLog('info', outputMessage);
      queueCompatRendererLog('info', outputMessage);
    }

    const { default: ScraperRunner } = getRunnerModule();

    state.activeRunner = new ScraperRunner({
      ...runtimeSettings,
      demoMode: runtimeSettings.demoMode || runtimePackage.demoMode || 'aed',
      demoLabel: runtimeSettings.demoLabel || runtimePackage.demoLabel || 'AED',
      productDisplayName: runtimePackage.productDisplayName || appTitle,
      useProgressBars: false,
      handleSignals: false
    });

    state.activeRunner.on('log', (entry) => {
      // In Go-task-controller mode the Go side writes canonical task logs, so
      // this branch must avoid duplicating file writes and only mirror to UI.
      if (!useGoTaskController) {
        logBridge.appendTaskLogEntry(entry);
      }
      logBridge.queueRendererLogEntry(entry);
    });

    state.activeRunner.on('state', (taskState) => {
      // Same rule as log events: JS owns persisted task-state only in the fully
      // legacy branch; otherwise it mirrors state outward for UI compatibility.
      if (!useGoTaskController) {
        logBridge.appendTaskStateEntry(taskState);
      }
      logBridge.queueRendererState(taskState);

      const isFinalState = ['completed', 'error', 'stopped', 'incomplete'].includes(taskState.status);
      if (isFinalState && !state.pendingRestartSettings) {
        showReminder(taskState);
      }
    });

    // Finalization is where this compatibility service either yields completely
    // back to the Go task controller, or continues the older JS restart flow.
    state.activeRunner
      .run()
      .catch((error) => {
        const message = error instanceof Error ? error.message : String(error);
        if (!useGoTaskController) {
          logBridge.writeTaskLog('error', message);
        } else {
          queueCompatRendererLog('error', message);
        }
        logBridge.queueRendererState({
          status: 'error',
          message
        });
      })
      .finally(async () => {
        await logBridge.flushDesktopPipelines();
        // 移除 runner 事件监听，避免旧实例被闭包引用导致内存泄漏。
        if (state.activeRunner && typeof state.activeRunner.removeAllListeners === 'function') {
          state.activeRunner.removeAllListeners();
        }
        state.activeRunner = null;
        state.currentTaskOutputDir = null;

        if (useGoTaskController) {
          // Go-owned lifecycle stops here cleanly. Do not re-enter JS restart or
          // quit-handling logic after the compatibility runner has finished.
          state.pendingRestartSettings = null;
          if (state.quittingAfterStop) {
            state.quittingAfterStop = false;
          }
          return;
        }

        if (state.pendingRestartSettings) {
          // Legacy-only restart queue. Keeping it in one place makes future
          // retirement easier because the restart surface is narrowly scoped.
          const nextSettings = state.pendingRestartSettings;
          state.pendingRestartSettings = null;

          queueCompatRendererLog('info', mainText.continueRecovery);

          try {
            await startRunner(nextSettings);
          } catch (error) {
            const message = `${mainText.restartFailedPrefix}${error instanceof Error ? error.message : String(error)}`;
            if (!useGoTaskController) {
              logBridge.writeTaskLog('error', message);
            } else {
              queueCompatRendererLog('error', message);
            }
            logBridge.queueRendererState({
              status: 'error',
              message
            });
          }
          return;
        }

        if (state.quittingAfterStop) {
          state.quittingAfterStop = false;
          app.quit();
        }
      });

    return { ok: true };
  }

  // The legacy restart path is intentionally shallow: set resumeExisting, stop
  // the active compatibility runner, and let the normal start path re-enter.
  async function restartRunner(settings) {
    const nextSettings = {
      ...settings,
      resumeExisting: true
    };

    if (state.activeRunner) {
      state.pendingRestartSettings = nextSettings;
      await state.activeRunner.stop();
      return { ok: true, restarting: true };
    }

    await startRunner(nextSettings);
    return { ok: true, restarting: false };
  }

  // Stop remains a thin compatibility wrapper so current crawl-stop semantics
  // are still centralized in the underlying runner implementation.
  async function stopRunner(options = {}) {
    if (!options.preserveRestart) {
      state.pendingRestartSettings = null;
    }

    if (!state.activeRunner) {
      return { ok: true };
    }

    await state.activeRunner.stop();
    return { ok: true };
  }

  // Before-quit handling matters only for the archived desktop runtime because
  // it may still own the active JS runner and pending log flushes.
  async function handleBeforeQuit(event) {
    state.pendingRestartSettings = null;

    if (!state.activeRunner) {
      await logBridge.flushDesktopPipelines();
      return;
    }

    event.preventDefault();
    state.quittingAfterStop = true;
    await state.activeRunner.stop();
    await logBridge.flushDesktopPipelines();
  }

  // Anti-block URL refresh stays here only because the archived JS runtime
  // still exposes that helper. Current crawler investigations should first
  // confirm whether the active path is this compatibility layer at all.
  async function updateAntiBlockUrls(settings) {
    const { default: ScraperRunner } = getRunnerModule();
    return ScraperRunner.updateAntiBlockUrls({
      base: settings.base,
      proxy: settings.proxy
    });
  }

  function isRunning() {
    // Exposes only compatibility-runner presence. Current Go-native lifecycle
    // checks should continue using the Go task/runtime query path instead.
    return Boolean(state.activeRunner);
  }

  return {
    startRunner,
    restartRunner,
    stopRunner,
    handleBeforeQuit,
    updateAntiBlockUrls,
    isRunning
  };
}

module.exports = {
  createRunnerService
};
