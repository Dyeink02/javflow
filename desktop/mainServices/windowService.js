// Legacy Electron shell service retained for archive/demo desktop builds.
// deprecated: retired Electron shell only; marker=retired-electron-window-service
// The active production desktop app now runs through Wails + Go under
// `wails-shell/`. Keep this file for historical compatibility only and do not
// treat it as part of the current Wails runtime path when debugging.
//
// Retirement-candidate note:
// - current runtime code does not import this file
// - it remains only as archived shell reference material
// - if future cleanup removes Electron shell archives, this is one of the
//   first files that can leave with very low runtime risk
// - no new product feature should depend on this file again
//
// Archived-shell rule:
// do not copy current Wails UI behavior back into this file unless the goal is
// specifically to preserve an Electron archive build.
//
// Ownership summary:
// 1) preserve archived Electron window-shell behavior only
// 2) centralize external navigation guards for the retired Electron shell
// 3) keep this file clearly outside the current Wails production runtime
//
// File map for maintainers:
// 1) external navigation guard helpers
// 2) archived BrowserWindow bootstrap
// 3) minimal renderer-message bridge

function createWindowService({ BrowserWindow, path, state, desktopRoot, windowIconPath, appTitle, appVersion, appDemoLabel, mainText }) {
  // Archived Electron windows should open remote links in the user's browser
  // instead of navigating the local renderer away from its packaged entrypoint.
  function shouldOpenExternally(targetUrl) {
    return /^https?:\/\//i.test(String(targetUrl || '').trim());
  }

  // Keep external-link behavior centralized so old shell builds do not each
  // attach slightly different navigation guards.
  function attachExternalNavigationGuards(windowInstance) {
    if (!windowInstance || !windowInstance.webContents) {
      return;
    }

    windowInstance.webContents.setWindowOpenHandler(({ url }) => {
      if (shouldOpenExternally(url)) {
        require('electron').shell.openExternal(url).catch(() => undefined);
        return { action: 'deny' };
      }

      return { action: 'allow' };
    });

    windowInstance.webContents.on('will-navigate', (event, url) => {
      if (!shouldOpenExternally(url)) {
        return;
      }

      event.preventDefault();
      require('electron').shell.openExternal(url).catch(() => undefined);
    });
  }

  // This boot path exists only for the historical Electron shell. Current
  // production Wails rendering does not load this HTML entry at all.
  async function createWindow() {
    const rendererEntry = path.join(desktopRoot, 'renderer', '.generated', 'index.html');

    state.mainWindow = new BrowserWindow({
      width: 1500,
      height: 980,
      minWidth: 1260,
      minHeight: 840,
      title: `${appTitle}${appDemoLabel ? `${mainText.demoTitleSeparator}${appDemoLabel}` : ''} v${appVersion}`,
      backgroundColor: '#070a16',
      icon: windowIconPath,
      autoHideMenuBar: true,
      webPreferences: {
        preload: path.join(desktopRoot, 'preload.js'),
        contextIsolation: true,
        nodeIntegration: false
      }
    });

    state.mainWindow.on('closed', () => {
      state.mainWindow = null;
    });

    attachExternalNavigationGuards(state.mainWindow);

    await state.mainWindow.loadFile(rendererEntry);
    return state.mainWindow;
  }

  // Minimal renderer bridge retained for legacy shell messaging only.
  function getWindow() {
    return state.mainWindow;
  }

  function sendToRenderer(channel, payload) {
    if (state.mainWindow && !state.mainWindow.isDestroyed()) {
      state.mainWindow.webContents.send(channel, payload);
    }
  }

  return {
    createWindow,
    getWindow,
    sendToRenderer
  };
}

module.exports = {
  createWindowService
};
