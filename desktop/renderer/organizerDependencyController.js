// Organizer dependency controller for the current desktop renderer.
// Keep dependency probing/install prompts here so organizer execution flow does
// not get mixed with installer or prerequisite-specific UI state.
//
// Ownership summary:
// 1) own organizer dependency probing/install UI state
// 2) keep optional AI/compat prerequisite status in one controller
// 3) avoid mixing installer/probe behavior into organizer execution flow
//
// File map for maintainers:
// 1) dependency status lookup helpers
// 2) dependency-box visibility/button synchronization
// 3) probe/install action handlers

(function initializeOrganizerDependencyController(globalScope) {
  const rendererHelpers = globalScope.desktopRendererHelpers || {};
  const dependencyView = globalScope.desktopOrganizerDependencyView || null;
  const safeLocalStorageSet = rendererHelpers.safeLocalStorageSet;

  if (!safeLocalStorageSet || !dependencyView) {
    throw new Error('organizerDependencyController requires desktopRendererHelpers and desktopOrganizerDependencyView');
  }

  function createOrganizerDependencyController(options) {
    const {
      elements,
      desktopApi,
      state,
      appendLogLine,
      getErrorMessage,
      isAdDetectionEnabled,
      getLogView,
      requestRenderAdDetectionUiState
    } = options;

    function getDependencyItem(status, name) {
      // Dependency-item reads stay tiny and local so controller code does not
      // duplicate object-shape guards around every status access.
      if (!status || typeof status !== 'object') {
        return null;
      }
      return status[name] && typeof status[name] === 'object' ? status[name] : null;
    }

    function getDependencyBox() {
      return dependencyView.getDependencyBox(elements.organizerWorkspace);
    }

    function getDependencyAdvancedNodes() {
      return dependencyView.getDependencyAdvancedNodes(elements.organizerWorkspace, elements);
    }

    function decorateDependencyBox() {
      dependencyView.decorateDependencyBox(elements.organizerWorkspace, elements);
    }

    // Organizer default mode no longer treats FFmpeg/ONNX/AList as first-class
    // requirements. Keep this controller as the single compatibility boundary so
    // future maintenance does not have to chase dependency state across modules.
    function syncDependencyButtonState() {
      const ffmpegStatus = getDependencyItem(state.dependencyStatus, 'ffmpeg');
      const onnxStatus = getDependencyItem(state.dependencyStatus, 'onnx');
      const installing = String(state.dependencyInstalling || '').trim().toLowerCase();
      const busy = state.running || Boolean(installing);
      const aiEnabled = isAdDetectionEnabled();
      const advancedDisabled = !aiEnabled || busy;

      if (elements.organizerRefreshDependencyButton) {
        elements.organizerRefreshDependencyButton.disabled = advancedDisabled;
      }
      if (elements.organizerInstallFfmpegButton) {
        elements.organizerInstallFfmpegButton.disabled =
          advancedDisabled || Boolean(ffmpegStatus && ffmpegStatus.available);
        elements.organizerInstallFfmpegButton.textContent =
          installing === 'ffmpeg' ? '正在安装 FFmpeg...' : '一键安装 FFmpeg';
      }
      if (elements.organizerInstallOnnxButton) {
        elements.organizerInstallOnnxButton.disabled = advancedDisabled || Boolean(onnxStatus && onnxStatus.available);
        elements.organizerInstallOnnxButton.textContent =
          installing === 'onnx' ? '正在安装 ONNX Runtime...' : '一键安装 ONNX Runtime';
      }
      if (elements.organizerDeleteDependencyButton) {
        elements.organizerDeleteDependencyButton.disabled = advancedDisabled;
      }
      if (elements.organizerFfmpegDownloadUrl) {
        elements.organizerFfmpegDownloadUrl.disabled = !aiEnabled || state.running;
      }
      if (elements.organizerOnnxDownloadUrl) {
        elements.organizerOnnxDownloadUrl.disabled = !aiEnabled || state.running;
      }
      if (elements.organizerAlistUrl) {
        elements.organizerAlistUrl.disabled = !aiEnabled || state.running;
      }
    }

    function renderDependencyStatus(status = null) {
      const box = getDependencyBox();
      const ffmpegStatus = getDependencyItem(status, 'ffmpeg');
      const onnxStatus = getDependencyItem(status, 'onnx');
      const installing = String(state.dependencyInstalling || '').trim().toLowerCase();
      const summaryElement = elements.organizerDependencySummary;
      const detailElement = elements.organizerDependencyDetail;
      const aiEnabled = isAdDetectionEnabled();

      decorateDependencyBox();

      if (box) {
        const hasMissing = Boolean(aiEnabled && ffmpegStatus && !ffmpegStatus.available);
        box.classList.toggle('is-missing', hasMissing);
        box.classList.toggle('is-installing', Boolean(installing));
        box.classList.toggle('is-compat-collapsed', !aiEnabled);
      }

      if (summaryElement) {
        if (installing && state.dependencyProgressMessage) {
          summaryElement.textContent = state.dependencyProgressMessage;
        } else if (!aiEnabled) {
          summaryElement.textContent =
            '当前主整理模式未启用 AI 广告检测，FFmpeg / ONNX / AList 依赖入口已降级为可选兼容能力。';
        } else if (!status) {
          summaryElement.textContent = '尚未获取依赖检测结果，可使用已存在的 FFmpeg / ONNX Runtime。';
        } else {
          const ffmpegText = ffmpegStatus && ffmpegStatus.available ? 'FFmpeg：已就绪' : 'FFmpeg：未安装';
          const onnxText = onnxStatus && onnxStatus.available ? 'ONNX Runtime：已就绪' : 'ONNX Runtime：未安装（可选）';
          summaryElement.textContent = `${ffmpegText} | ${onnxText}`;
        }
      }

      if (detailElement) {
        if (installing && state.dependencyProgressMessage) {
          detailElement.textContent = '下载安装进度会写入当前窗口日志，无需额外打开外部安装器。';
        } else if (!aiEnabled) {
          detailElement.textContent =
            '默认整理链路只依赖番号匹配、文件大小和命名规则。仅在你手动启用 AI 广告检测时，才需要检查 FFmpeg、可选 ONNX 以及 AList 流式读取配置。';
        } else if (!status) {
          detailElement.textContent = '启用 AI 广告检测前至少需要 FFmpeg；若未检测到，可在此面板安装。';
        } else {
          const details = [];
          if (ffmpegStatus) {
            details.push(
              ffmpegStatus.available
                ? `FFmpeg 路径：${ffmpegStatus.installedPath || '-'}`
                : 'FFmpeg 未就绪：启用 AI 广告检测前必须先安装。'
            );
          }
          if (onnxStatus) {
            details.push(
              onnxStatus.available
                ? `ONNX Runtime 路径：${onnxStatus.installedPath || '-'}`
                : 'ONNX Runtime 当前为可选增强依赖，可按需预装。'
            );
          }
          detailElement.textContent = details.join(' | ');
        }
      }

      syncDependencyButtonState();
      if (typeof requestRenderAdDetectionUiState === 'function') {
        requestRenderAdDetectionUiState();
      }
    }

    async function refreshDependencyStatus(logMessage = false) {
      // Dependency refresh is a UI read-model action. It should not infer or
      // mutate organizer run policy beyond refreshing displayed prerequisites.
      if (typeof desktopApi.getDependencyStatus !== 'function') {
        state.dependencyStatus = null;
        renderDependencyStatus(null);
        return null;
      }

      const status = await desktopApi.getDependencyStatus();
      state.dependencyStatus = status || null;
      if (!state.dependencyInstalling) {
        state.dependencyProgressMessage = '';
      }
      renderDependencyStatus(state.dependencyStatus);
      if (logMessage) {
        appendLogLine(getLogView(), 'info', 'AI 依赖状态已刷新。');
      }
      return state.dependencyStatus;
    }

    async function installDependency(name) {
      // Installation orchestration belongs here because it is operator-facing
      // compatibility UX, not part of the core organizer execution flow.
      const normalizedName = String(name || '').trim().toLowerCase();
      if (!normalizedName || typeof desktopApi.installDependency !== 'function') {
        return;
      }

      const customUrl =
        normalizedName === 'ffmpeg'
          ? String((elements.organizerFfmpegDownloadUrl && elements.organizerFfmpegDownloadUrl.value) || '').trim()
          : String((elements.organizerOnnxDownloadUrl && elements.organizerOnnxDownloadUrl.value) || '').trim();

      if (customUrl) {
        safeLocalStorageSet(`jav.organizer.${normalizedName}.download.url`, customUrl);
      }

      state.dependencyInstalling = normalizedName;
      const displayName = normalizedName === 'onnx' ? 'ONNX Runtime' : 'FFmpeg';
      state.dependencyProgressMessage = customUrl
        ? `正在从自定义地址安装 ${displayName} ...`
        : `正在准备安装 ${displayName} ...`;
      renderDependencyStatus(state.dependencyStatus);
      appendLogLine(getLogView(), 'info', `开始安装 ${displayName}${customUrl ? '（自定义下载地址）' : ''}`);

      try {
        const nextStatus = await desktopApi.installDependency(normalizedName, customUrl || undefined);
        state.dependencyStatus = nextStatus || state.dependencyStatus;
        appendLogLine(getLogView(), 'info', `${displayName} 安装完成。`);
      } catch (error) {
        appendLogLine(getLogView(), 'error', `${displayName} 安装失败：${getErrorMessage(error)}`);
        throw error;
      } finally {
        state.dependencyInstalling = '';
        state.dependencyProgressMessage = '';
        await refreshDependencyStatus(false).catch(() => {});
      }
    }

    async function deleteDependencyState() {
      if (typeof desktopApi.uninstallDependency !== 'function') {
        state.dependencyStatus = null;
        state.dependencyInstalling = '';
        state.dependencyProgressMessage = '';
        renderDependencyStatus(null);
        appendLogLine(getLogView(), 'info', '已清除 AI 依赖检测状态（当前环境未接入卸载能力）。');
        return;
      }

      if (globalThis.confirm) {
        if (!globalThis.confirm('确定要删除已安装的 AI 依赖吗？将会删除 FFmpeg 和 ONNX Runtime 文件。')) {
          return;
        }
      }

      appendLogLine(getLogView(), 'info', '开始卸载 FFmpeg 和 ONNX Runtime ...');
      renderDependencyStatus(state.dependencyStatus);

      try {
        await desktopApi.uninstallDependency('ffmpeg');
        appendLogLine(getLogView(), 'info', 'FFmpeg 已卸载。');
      } catch (error) {
        appendLogLine(getLogView(), 'warn', `FFmpeg 卸载失败：${getErrorMessage(error)}`);
      }

      try {
        await desktopApi.uninstallDependency('onnx');
        appendLogLine(getLogView(), 'info', 'ONNX Runtime 已卸载。');
      } catch (error) {
        appendLogLine(getLogView(), 'warn', `ONNX Runtime 卸载失败：${getErrorMessage(error)}`);
      }

      await refreshDependencyStatus(false).catch(() => {});
      appendLogLine(getLogView(), 'info', 'AI 依赖卸载完成。');
    }

    async function ensureAiDependenciesReady() {
      // Readiness gating only protects the optional AI lane. The default
      // organizer path should remain independent from these checks.
      if (!isAdDetectionEnabled()) {
        return null;
      }

      const status = (await refreshDependencyStatus(false).catch(() => state.dependencyStatus)) || {};
      const ffmpegStatus = getDependencyItem(status, 'ffmpeg');
      const onnxStatus = getDependencyItem(status, 'onnx');

      if (!ffmpegStatus || !ffmpegStatus.available) {
        throw new Error('已启用 AI 广告检测，但未检测到 FFmpeg。请先在“AI 依赖环境”中点击“一键安装 FFmpeg”。');
      }

      if (onnxStatus && !onnxStatus.available) {
        appendLogLine(
          getLogView(),
          'warn',
          '当前未检测到 ONNX Runtime。现阶段仍可继续使用规则与样本相似度链路，后续若启用增强模型再补装即可。'
        );
      }

      return status;
    }

    function applyInstallProgress(payload) {
      if (!payload || typeof payload !== 'object') {
        return;
      }

      const name = String(payload.name || '').trim().toLowerCase();
      if (name) {
        state.dependencyInstalling = name;
      }
      state.dependencyProgressMessage = String(payload.message || '').trim();
      renderDependencyStatus(state.dependencyStatus);

      if (payload.message) {
        appendLogLine(getLogView(), payload.stage === 'error' ? 'error' : 'info', String(payload.message));
      }
    }

    return {
      syncDependencyButtonState,
      renderDependencyStatus,
      refreshDependencyStatus,
      installDependency,
      deleteDependencyState,
      ensureAiDependenciesReady,
      applyInstallProgress
    };
  }

  globalScope.desktopOrganizerDependencyController = {
    createOrganizerDependencyController
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
