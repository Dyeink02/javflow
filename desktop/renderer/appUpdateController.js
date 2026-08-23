// Renderer controller for the portable JavFlow update affordance.
//
// Ownership summary:
// 1) bind the crawler-topbar check/download/apply button
// 2) project update-service results into compact visible status text
// 3) keep release/network policy in the Go appupdate service
//
// Boundary rule:
// this controller only coordinates UI state. It must not fetch GitHub directly,
// inspect proxy settings, or implement executable replacement logic.
//
// File map for maintainers:
// 1) update status/button projection
// 2) check/download/apply transitions
// 3) bootstrap/dispose lifecycle helpers
(function initializeAppUpdateController(globalScope) {
  const DEFAULT_TEXT = {
    checkButton: '检查更新',
    checking: '检测更新中...',
    availablePrefix: '待更新 v',
    downloadButton: '下载更新',
    downloading: '正在下载更新...',
    downloadedPrefix: '已下载 v',
    applyButton: '立即更新',
    applying: '正在准备重启...',
    latest: '',
    checkFailedPrefix: '更新检查失败：',
    downloadFailedPrefix: '更新下载失败：',
    applyFailedPrefix: '更新启动失败：',
    dialogTitle: '更新 JavFlow',
    dialogMessagePrefix: '新版本 v',
    dialogMessageSuffix: ' 已下载，应用更新后软件会自动重启。现在重启吗？',
    restartButton: '立即重启',
    laterButton: '稍后'
  };

  function createAppUpdateController(options) {
    const {
      elements,
      desktopApi,
      uiText,
      autoCheckDelayMs = 1200
    } = options || {};
    const text = Object.assign(
      {},
      DEFAULT_TEXT,
      uiText && uiText.UI_TEXT && uiText.UI_TEXT.appUpdate ? uiText.UI_TEXT.appUpdate : {}
    );
    let state = 'idle';
    let latestInfo = null;
    let bootstrapCompleted = false;
    let autoCheckTimer = null;
    let actionPromise = null;

    function setStatus(message, statusKind = '') {
      const node = elements && elements.crawlerUpdateStatus;
      if (!node) {
        return;
      }
      node.textContent = String(message || '');
      node.hidden = !String(message || '').trim();
      node.classList.remove('is-checking', 'is-available', 'is-ready', 'is-error');
      if (statusKind) {
        node.classList.add(`is-${statusKind}`);
      }
    }

    function setButton(label, disabled = false) {
      const button = elements && elements.crawlerCheckUpdateButton;
      if (!button) {
        return;
      }
      button.textContent = String(label || text.checkButton);
      button.disabled = Boolean(disabled);
    }

    function errorMessage(error) {
      return error instanceof Error ? error.message : String(error || '未知错误');
    }

    function runOnce(task) {
      if (actionPromise) {
        return actionPromise;
      }
      actionPromise = Promise.resolve()
        .then(task)
        .finally(() => {
          actionPromise = null;
        });
      return actionPromise;
    }

    async function check() {
      return runOnce(async () => {
        state = 'checking';
        setStatus(text.checking, 'checking');
        setButton(text.checkButton, true);
        try {
          const result = await desktopApi.checkAppUpdate();
          latestInfo = result && typeof result === 'object' ? result : null;
          if (!latestInfo || !latestInfo.updateAvailable) {
            state = 'latest';
            setStatus(text.latest);
            setButton(text.checkButton, false);
            return latestInfo;
          }
          state = 'available';
          const version = String(latestInfo.latestVersion || '').trim();
          setStatus(`${text.availablePrefix}${version}`, 'available');
          setButton(text.downloadButton, false);
          return latestInfo;
        } catch (error) {
          state = 'error';
          setStatus(`${text.checkFailedPrefix}${errorMessage(error)}`, 'error');
          setButton(text.checkButton, false);
          return null;
        }
      });
    }

    async function download() {
      if (!latestInfo || !latestInfo.updateAvailable) {
        return check();
      }
      return runOnce(async () => {
        state = 'downloading';
        setStatus(text.downloading, 'checking');
        setButton(text.downloadButton, true);
        try {
          latestInfo = await desktopApi.downloadAppUpdate({
            version: latestInfo.latestVersion
          });
          state = 'downloaded';
          const version = String(latestInfo && latestInfo.latestVersion ? latestInfo.latestVersion : '').trim();
          setStatus(`${text.downloadedPrefix}${version}`, 'ready');
          setButton(text.applyButton, false);
          return latestInfo;
        } catch (error) {
          state = 'error';
          setStatus(`${text.downloadFailedPrefix}${errorMessage(error)}`, 'error');
          setButton(text.downloadButton, false);
          return null;
        }
      });
    }

    async function confirmRestart(version) {
      if (!desktopApi || typeof desktopApi.showAlert !== 'function') {
        return typeof globalScope.confirm === 'function' ? globalScope.confirm('现在重启应用更新吗？') : false;
      }
      const result = await desktopApi.showAlert({
        type: 'question',
        title: text.dialogTitle,
        message: `${text.dialogMessagePrefix}${version}${text.dialogMessageSuffix}`,
        buttons: [text.restartButton, text.laterButton]
      });
      return Boolean(result && result.selection === text.restartButton);
    }

    async function apply() {
      return runOnce(async () => {
        if (state !== 'downloaded') {
          return null;
        }
        const version = String(latestInfo && latestInfo.latestVersion ? latestInfo.latestVersion : '').trim();
        let shouldRestart = false;
        try {
          shouldRestart = await confirmRestart(version);
        } catch (error) {
          state = 'error';
          setStatus(`${text.applyFailedPrefix}${errorMessage(error)}`, 'error');
          setButton(text.applyButton, false);
          return null;
        }
        if (!shouldRestart) {
          setStatus(`${text.downloadedPrefix}${version}`, 'ready');
          setButton(text.applyButton, false);
          return latestInfo;
        }
        state = 'applying';
        setStatus(text.applying, 'checking');
        setButton(text.applyButton, true);
        try {
          const result = await desktopApi.applyAppUpdate();
          setButton(text.applying, true);
          return result;
        } catch (error) {
          state = 'error';
          setStatus(`${text.applyFailedPrefix}${errorMessage(error)}`, 'error');
          setButton(text.applyButton, false);
          return null;
        }
      });
    }

    function handleButtonClick() {
      if (state === 'available') {
        void download();
        return;
      }
      if (state === 'downloaded') {
        void apply();
        return;
      }
      void check();
    }

    function bootstrap() {
      if (bootstrapCompleted) {
        return;
      }
      bootstrapCompleted = true;
      const button = elements && elements.crawlerCheckUpdateButton;
      if (button) {
        button.addEventListener('click', handleButtonClick);
      }
      if (autoCheckDelayMs !== null && Number(autoCheckDelayMs) >= 0) {
        autoCheckTimer = setTimeout(() => {
          autoCheckTimer = null;
          void check();
        }, Number(autoCheckDelayMs));
      }
    }

    function dispose() {
      if (autoCheckTimer !== null) {
        clearTimeout(autoCheckTimer);
        autoCheckTimer = null;
      }
    }

    return {
      bootstrap,
      check,
      download,
      apply,
      dispose,
      getState: () => state
    };
  }

  globalScope.desktopAppUpdateController = {
    createAppUpdateController
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
