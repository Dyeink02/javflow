// Shared sidecar runtime helpers for compatibility facades.
// compatibility-owner: active crawl-compatible sidecar runtime helper; marker=compat-sidecar-shared-facade-runtime
// Centralize common settings-store/app-shim/runtime assembly so archived
// organizer/learning facades do not each reimplement the same bootstrap rules.
//
// Ownership summary:
// 1) build shared compatibility runtime pairs for archived sidecar facades
// 2) centralize app-shim and settings-store bootstrap for compatibility use
// 3) keep shared facade runtime assembly out of individual facades
//
// File map for maintainers:
// 1) archived app-shim bootstrap
// 2) shared settings-store assembly
// 3) minimal compatibility runtime exposure

const path = require('path');

const { createSettingsStore } = require('../../mainServices/settingsStore.js');
const { APP_INFO, FILE_NAMES } = require('../../common/appText.js');
const { createAppShim } = require('../runtimePaths.js');

function createSidecarSettingsStore({ fs }) {
  // Keep the app shim + settings-store bootstrap in one place so compatibility
  // facades share the same persisted-settings contract and do not each grow
  // their own runtime setup differences.
  //
  // Fast troubleshooting split:
  // 1) archived facade got the wrong userData/app-path/settings path -> inspect
  //    this helper and `runtimePaths.js`
  // 2) persisted settings shape/content issue -> inspect
  //    `desktop/mainServices/settingsStore.js`
  const app = createAppShim();
  const settingsStore = createSettingsStore({
    app,
    fs,
    path,
    appInfo: APP_INFO,
    magnetFilename: FILE_NAMES.magnetFilename || 'magnet-links.txt'
  });

  // Expose only the minimal compatibility pair used by archived facades.
  // If a caller wants more runtime state than `app` + `settingsStore`, that
  // state likely belongs in a dedicated runtime/facade helper instead.
  return {
    app,
    settingsStore
  };
}

module.exports = {
  createSidecarSettingsStore
};
