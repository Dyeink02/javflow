// Shared sidecar runtime assembly for the active crawl compatibility lane.
// compatibility-owner: active crawl-compatible runtime factory; marker=compat-sidecar-crawl-runtime-factory
// Keep runtime construction details here so `crawlService.js` can stay focused
// on start/restart/stop orchestration instead of mixing in lazy legacy module
// bootstrap, settings-store setup, and log/runtime wiring.
//
// Ownership summary:
// 1) assemble the sidecar-local crawl compatibility runtime once
// 2) keep Go-owned host and archived JS-owned runtime handles clearly separated
// 3) share log/state plumbing across all crawl compatibility calls
//
// Current product crawl ownership still belongs to the Go task controller first.
//
// File map for maintainers:
// 1) shared settings-store lazy bootstrap
// 2) archived JS runtime handle
// 3) active crawl compatibility runtime assembly
// 4) one-runtime-per-process factory wrapper

const path = require('path');

const { APP_INFO, FILE_NAMES, MAIN_TEXT } = require('../../common/appText.js');
const { createRuntimeState } = require('../../mainServices/runtimeState.js');
const { createRunnerService } = require('../../mainServices/runnerService.js');
const { getRuntimeContext, createAppShim } = require('../runtimePaths.js');
const { createTaskLogService } = require('./taskLogService.js');
const { createGoRunnerHost } = require('./goRunnerHost.js');
const { createSidecarSettingsStore } = require('./sharedFacadeRuntime.js');

function createLazySettingsStore({ fs }) {
  let runtime = null;

  function ensureRuntime() {
    if (!runtime) {
      // Reuse the shared sidecar settings-store bootstrap so legacy crawl
      // runtime pieces do not silently drift from organizer/learning facades.
      runtime = createSidecarSettingsStore({ fs });
    }
    return runtime;
  }

  return {
    loadSettings() {
      return ensureRuntime().settingsStore.loadSettings();
    },
    saveSettings(settings) {
      return ensureRuntime().settingsStore.saveSettings(settings);
    },
    getCurrentOutputDir() {
      return ensureRuntime().settingsStore.getCurrentOutputDir();
    }
  };
}

function createLegacyRuntimeHandle({
  fs,
  desktopRoot,
  runtimePackage,
  logBridge
}) {
  // The archived JS-owned runtime is wrapped behind a lazy handle so the sidecar
  // does not pay its startup cost unless a true legacy-lifecycle branch needs it.
  let legacyRuntime = null;

  function ensureLegacyRuntime() {
    if (legacyRuntime) {
      return legacyRuntime;
    }

    // This branch intentionally stays lazy because many sidecar sessions only
    // need the Go-owned compatibility host and should not instantiate the
    // heavier archived JS runner stack.
    const state = createRuntimeState();
    const settingsStore = createLazySettingsStore({ fs });
    const app = createAppShim();
    const runnerService = createRunnerService({
      state,
      app,
      dialog: {
        showMessageBox: async () => ({ response: 0 })
      },
      Notification: {
        isSupported: () => false
      },
      path,
      desktopRoot,
      runtimePackage,
      appTitle: APP_INFO.title,
      appVersion: APP_INFO.version || runtimePackage.version,
      appDemoLabel: runtimePackage.demoLabel || '',
      mainText: MAIN_TEXT,
      windowService: {
        getWindow: () => null,
        sendToRenderer: () => {}
      },
      settingsStore,
      logBridge
    });

    legacyRuntime = {
      state,
      settingsStore,
      runnerService
    };

    return legacyRuntime;
  }

  return {
    ensureLegacyRuntime,
    getLegacyRuntime() {
      return legacyRuntime;
    }
  };
}

function buildRuntime({ fs, eventBus }) {
  // Runtime ownership split:
  // 1) `taskLogService` / `logBridge` stay sidecar-local because renderer
  //    compatibility still consumes that event shape
  // 2) `goRunnerHost` is the preferred compatibility path when the Go task
  //    controller owns lifecycle
  // 3) `legacyRuntime.runnerService` exists only for the older JS-owned task
  //    lifecycle path and stays lazily loaded behind `ensureLegacyRuntime()`
  //
  // Troubleshooting split:
  // 1) duplicated/misaligned renderer logs in sidecar mode -> inspect
  //    `taskLogService` + `logBridge` wiring here first
  // 2) Go-task-controller compatibility launch issue -> inspect `goRunnerHost`
  // 3) legacy JS-owned lifecycle issue -> inspect `ensureLegacyRuntime()` and
  //    then `desktop/mainServices/runnerService.js`
  const runtimeContext = getRuntimeContext();
  const desktopRoot = path.join(runtimeContext.repoRoot, 'desktop');
  const taskLogService = createTaskLogService({ fs, eventBus });
  const logBridge = taskLogService.getLogBridge();
  const runtimePackage = require('../../../package.json');
  const goRunnerHost = createGoRunnerHost({
    desktopRoot,
    runtimePackage,
    appTitle: APP_INFO.title,
    mainText: MAIN_TEXT,
    logBridge
  });
  const legacyRuntimeHandle = createLegacyRuntimeHandle({
    fs,
    desktopRoot,
    runtimePackage,
    logBridge
  });

  return {
    taskLogService,
    logBridge,
    goRunnerHost,
    ensureLegacyRuntime: legacyRuntimeHandle.ensureLegacyRuntime,
    getLegacyRuntime: legacyRuntimeHandle.getLegacyRuntime
  };
}

function createCrawlRuntimeFactory({ fs, eventBus }) {
  let runtime = null;

  return {
    ensureRuntime() {
      // One runtime per sidecar process keeps log/state publication deterministic
      // across repeated start/restart/stop commands.
      if (!runtime) {
        // Build the compatibility runtime once per sidecar process so crawl
        // start/restart/stop stay on a single shared log/state boundary.
        runtime = buildRuntime({ fs, eventBus });
      }
      return runtime;
    }
  };
}

module.exports = {
  createCrawlRuntimeFactory
};
