// Organizer learning controller for the current desktop renderer.
// This file owns ad-learning actions and feedback surfaces, separate from the
// main organizer run flow and dependency-management concerns.
//
// Ownership summary:
// 1) own optional ad-learning UI actions and summary refresh
// 2) keep learning-mode visibility/state local to the organizer workspace
// 3) avoid mixing learning controls into organizer execution or dependency flows
//
// File map for maintainers:
// 1) learning log/button state helpers
// 2) learning summary/model visibility synchronization
// 3) import/learn/evaluate action handlers

(function initializeOrganizerLearningController(globalScope) {
  const learningView = globalScope.desktopOrganizerLearningView || null;

  if (!learningView) {
    throw new Error('desktopOrganizerLearningView is required before organizerLearningController');
  }

  function createOrganizerLearningController(options) {
    const {
      elements,
      desktopApi,
      state,
      messages,
      appendLogLine,
      getErrorMessage,
      normalizeKeywordText,
      normalizeAdModelType,
      toSafeInteger,
      setStatus,
      setSummaryMessage,
      requestSyncActionButtons,
      requestSyncDependencyStatus
    } = options;
    let eventsBound = false;

    function appendOrganizerLearningLog(level, message, timestamp) {
      // Learning logs stay scoped to organizer log projection so optional
      // ad-learning UX does not invent a second log surface.
      appendLogLine(elements.organizerLogView, level, message, timestamp);
    }

    function bindAsyncClick(button, handler, onError) {
      if (!button) {
        return;
      }

      button.addEventListener('click', async () => {
        try {
          await handler();
        } catch (error) {
          if (typeof onError === 'function') {
            onError(error);
            return;
          }
          appendOrganizerLearningLog('error', getErrorMessage(error));
        }
      });
    }

    function syncActionButtons() {
      if (typeof requestSyncActionButtons === 'function') {
        requestSyncActionButtons();
      }
    }

    // The ad-learning box is an optional compatibility lane. The helpers below
    // only decide whether that lane is visible/interactive; they should stay
    // separate from the actual sample-import and learning-task execution code.
    // Organizer's main path is now file-rule based. The ad-learning block is an
    // optional compatibility lane that should stay visually secondary unless the
    // user explicitly enables AI ad detection.
    function getLearningBox() {
      return learningView.getLearningBox();
    }

    function ensureModeHintElement() {
      return learningView.ensureModeHintElement();
    }

    function decorateLearningBox() {
      learningView.decorateLearningBox(elements);
    }

    function notifyAdDetectionModeChanged() {
      syncActionButtons();
      if (typeof requestSyncDependencyStatus === 'function') {
        requestSyncDependencyStatus();
      }
      if (isAdDetectionEnabled()) {
        refreshAdLearningSummary(false).catch(() => {});
      }
    }

    function isAdDetectionEnabled() {
      if (elements.organizerAdDetectionEnable || elements.organizerAdDetectionDisable) {
        if (elements.organizerAdDetectionDisable && elements.organizerAdDetectionDisable.checked) {
          return false;
        }
        if (elements.organizerAdDetectionEnable && elements.organizerAdDetectionEnable.checked) {
          return true;
        }
      }
      return Boolean(elements.organizerAdDetectionEnabled && elements.organizerAdDetectionEnabled.checked);
    }

    function applyAdDetectionUiState() {
      const enabled = isAdDetectionEnabled();
      const learningBox = getLearningBox();
      const modeHint = ensureModeHintElement();
      decorateLearningBox();

      if (learningBox) {
        learningBox.classList.toggle('is-compat-collapsed', !enabled);
      }
      if (modeHint) {
        modeHint.textContent = enabled
          ? '已启用 AI 广告检测兼容链路：将显示样本学习、依赖检查与可选 AList 配置。'
          : '当前为主整理模式：仅按番号、文件大小和命名规则整理。样本学习、FFmpeg / ONNX / AList 仅在启用 AI 广告检测后使用。';
      }

      const advancedDisabled = !enabled || state.running;
      if (elements.organizerAdModelType) {
        elements.organizerAdModelType.disabled = advancedDisabled;
      }
      [
        elements.organizerAdThreshold,
        elements.organizerAdKeywords,
        elements.organizerLearningCodes,
        elements.organizerImportAdSamplesButton,
        elements.organizerHelpImportAdButton,
        elements.organizerImportNormalSamplesButton,
        elements.organizerHelpImportNormalButton,
        elements.organizerLearnAdByCodesButton,
        elements.organizerHelpLearnAdButton,
        elements.organizerLearnNormalByCodesButton,
        elements.organizerHelpLearnNormalButton,
        elements.organizerRefreshLearningSummaryButton
      ].forEach((element) => {
        if (element) {
          element.disabled = advancedDisabled;
        }
      });
    }

    function getLearningConfig() {
      // Config collection is setup/UI normalization only. Backend learning
      // services remain responsible for validating operational behavior.
      return {
        adDetectionEnabled: isAdDetectionEnabled(),
        adModelType: normalizeAdModelType(elements.organizerAdModelType && elements.organizerAdModelType.value),
        adThreshold: toSafeInteger(elements.organizerAdThreshold && elements.organizerAdThreshold.value, 60, 1, 100),
        adKeywords: normalizeKeywordText(elements.organizerAdKeywords && elements.organizerAdKeywords.value).join(', ')
      };
    }

    // Summary rendering is the operator-facing read model for the learning
    // cache. If summary wording or counts are wrong, debug here before touching
    // sample import or training task calls.
    function renderLearningSummary(summary = null) {
      if (!elements.organizerLearningSummary) {
        return;
      }

      if (!summary) {
        elements.organizerLearningSummary.textContent = '尚未加载学习模型。';
        return;
      }

      const threshold = summary.thresholds && Number.isFinite(summary.thresholds.adScore) ? summary.thresholds.adScore : 60;
      const updatedAt = summary.updatedAt ? new Date(summary.updatedAt).toLocaleString('zh-CN', { hour12: false }) : '-';
      const introTemplateCount = Number(summary.introTemplateCount || 0);
      const activeModelLabel = String(summary.activeModelLabel || summary.activeModel || 'MobileNetV3 Lite');
      const metrics = summary.metrics && typeof summary.metrics === 'object' ? summary.metrics : {};
      const lastLearning = metrics.lastLearning && typeof metrics.lastLearning === 'object' ? metrics.lastLearning : null;
      const observability = lastLearning
        ? `命中率 ${Number(lastLearning.hitRate || 0).toFixed(1)}% / 误判率 ${Number(lastLearning.falsePositiveRate || 0).toFixed(
            1
          )}% / 样本增量=${Number(lastLearning.sampleIncrement || 0)}`
        : '暂无';

      elements.organizerLearningSummary.textContent = [
        `关键词 ${summary.keywordCount || 0}`,
        `广告样本 ${summary.adSampleCount || 0}`,
        `正常样本 ${summary.normalSampleCount || 0}`,
        `片头模板 ${introTemplateCount}`,
        `识别策略 ${activeModelLabel}`,
        `阈值 ${threshold}`,
        `学习指标 ${observability}`,
        `更新时间 ${updatedAt}`
      ].join(' | ');
    }

    async function refreshAdLearningSummary(logMessage = false) {
      // Summary refresh is the read-model sync point for the optional learning
      // lane. Keep task execution and summary projection separate.
      if (typeof desktopApi.getAdLearningSummary !== 'function') {
        renderLearningSummary(null);
        return null;
      }

      const summary = await desktopApi.getAdLearningSummary();
      state.adSummary = summary || null;

      if (elements.organizerAdThreshold && summary && summary.thresholds && Number.isFinite(summary.thresholds.adScore)) {
        elements.organizerAdThreshold.value = String(summary.thresholds.adScore);
      }
      if (elements.organizerAdModelType && summary) {
        elements.organizerAdModelType.value = normalizeAdModelType(summary.activeModel);
      }

      renderLearningSummary(summary);
      applyAdDetectionUiState();
      if (logMessage) {
        appendLogLine(elements.organizerLogView, 'info', '广告学习摘要已刷新。');
      }
      return summary;
    }

    async function syncAdLearningModel(options = {}) {
      // Model-sync is the one place this controller writes persistent learning
      // knobs; do not let import/learn task helpers each patch settings alone.
      if (typeof desktopApi.updateAdLearningModel !== 'function') {
        return state.adSummary;
      }

      const learningConfig = getLearningConfig();
      const result = await desktopApi.updateAdLearningModel({
        keywords: learningConfig.adKeywords,
        adScore: learningConfig.adThreshold,
        modelType: learningConfig.adModelType
      });
      state.adSummary = result || null;
      renderLearningSummary(result);
      applyAdDetectionUiState();

      if (options.logSuccess) {
        appendLogLine(
          elements.organizerLogView,
          'info',
          `广告学习策略已同步：阈值 ${learningConfig.adThreshold}，关键词 ${learningConfig.adKeywords || '(空)'}`
        );
      }

      return result;
    }

    // The next helpers are the two execution lanes of this module:
    // 1) import explicit sample files
    // 2) derive samples by video codes from organizer root
    // They should keep UI messaging and state transitions symmetric so failure
    // analysis does not depend on which lane the user picked.
    async function showLearningGuide(kind) {
      if (typeof desktopApi.showAlert !== 'function') {
        return;
      }

      const title = '样本学习使用说明';
      if (kind === 'import-ad') {
        await desktopApi.showAlert({
          type: 'info',
          title,
          message: '导入广告样本',
          detail:
            '请选择“确认含开头广告”的截图或视频样本。建议优先选择视频开头 3-15 秒画面，并持续补充样本。',
          buttonLabel: '我知道了'
        });
        return;
      }

      if (kind === 'import-normal') {
        await desktopApi.showAlert({
          type: 'info',
          title,
          message: '导入正常样本',
          detail:
            '请选择“确认无开头广告”的正常视频样本。广告 / 正常样本数量尽量接近，可以降低误判。',
          buttonLabel: '我知道了'
        });
        return;
      }

      await desktopApi.showAlert({
        type: 'info',
        title,
        message: '按番号自动学习',
        detail: '先输入番号（逗号或换行分隔），再点击学习。软件会在根目录匹配番号并自动抓取开头帧。',
        buttonLabel: '开始学习'
      });
    }

    function getLearningCodes() {
      const rawCodes =
        elements.organizerLearningCodes && elements.organizerLearningCodes.value
          ? String(elements.organizerLearningCodes.value)
          : '';
      return Array.from(
        new Set(
          rawCodes
            .split(/[\r\n,，、\s]+/)
            .map((item) => item.trim().toUpperCase())
            .filter(Boolean)
        )
      );
    }

    async function learnSamplesByCodes(label) {
      if (typeof desktopApi.learnAdSamplesByCodes !== 'function') {
        throw new Error('当前版本未启用“按番号自动学习”能力。');
      }

      await showLearningGuide('learn-by-codes');

      const codes = getLearningCodes();
      if (codes.length === 0) {
        throw new Error('请先输入要学习的番号（支持逗号或换行分隔）。');
      }

      const sourceRoot = String(elements.organizerRoot && elements.organizerRoot.value ? elements.organizerRoot.value : '').trim();
      if (!sourceRoot) {
        throw new Error(messages.rootRequired);
      }

      const result = await desktopApi.learnAdSamplesByCodes({
        label,
        codes,
        rootPath: sourceRoot,
        includeSubdirectories: true,
        modelType: getLearningConfig().adModelType
      });

      state.adSummary = result && result.summary ? result.summary : state.adSummary;
      renderLearningSummary(state.adSummary);

      const matchedVideoCount = Number(result && result.matchedVideoCount) || 0;
      const importedSampleCount = Number(result && result.importedSampleCount) || 0;
      const missingCodes = Array.isArray(result && result.missingCodes) ? result.missingCodes : [];

      appendLogLine(
        elements.organizerLogView,
        'info',
        `${label === 'normal' ? '正常' : '广告'}按番号学习完成：命中视频 ${matchedVideoCount}，新增样本 ${importedSampleCount}，未命中番号 ${missingCodes.length}`
      );

      if (missingCodes.length > 0) {
        appendLogLine(elements.organizerLogView, 'warn', `未匹配到的番号：${missingCodes.join(', ')}`);
      }
    }

    async function runLearningTask(label) {
      if (state.running) {
        appendLogLine(elements.organizerLogView, 'warn', '当前已有任务在运行，请等待完成后再发起新的学习任务。');
        return;
      }

      const readableLabel = label === 'normal' ? '正常样本' : '广告样本';
      state.running = true;
      state.activeTask = 'learning';
      syncActionButtons();
      setStatus('running', `按番号学习进行中（${readableLabel}）...`);
      setSummaryMessage(`按番号学习已启动（${readableLabel}）。`);
      appendLogLine(elements.organizerLogView, 'info', `开始按番号学习：${readableLabel}`);

      try {
        await learnSamplesByCodes(label);
        setStatus('completed', `学习完成：${readableLabel}`);
      } catch (error) {
        const message = getErrorMessage(error);
        appendLogLine(elements.organizerLogView, 'error', message);
        setStatus('error', message);
        setSummaryMessage(message);
      } finally {
        state.running = false;
        state.activeTask = '';
        syncActionButtons();
      }
    }

    async function importLearningSamples(label) {
      if (typeof desktopApi.chooseLearningSamples !== 'function' || typeof desktopApi.importAdLearningSamples !== 'function') {
        throw new Error('当前版本未启用广告学习导入能力。');
      }

      await showLearningGuide(label === 'normal' ? 'import-normal' : 'import-ad');

      const samplePaths = await desktopApi.chooseLearningSamples();
      if (!Array.isArray(samplePaths) || samplePaths.length === 0) {
        appendLogLine(elements.organizerLogView, 'info', '未选择样本文件。');
        return;
      }

      const result = await desktopApi.importAdLearningSamples({
        label,
        samplePaths,
        modelType: getLearningConfig().adModelType
      });

      state.adSummary = result && result.summary ? result.summary : state.adSummary;
      renderLearningSummary(state.adSummary);

      const importedCount = Array.isArray(result && result.imported) ? result.imported.length : 0;
      const skippedCount = Array.isArray(result && result.skipped) ? result.skipped.length : 0;
      appendLogLine(
        elements.organizerLogView,
        'info',
        `${label === 'normal' ? '正常' : '广告'}样本导入完成：成功 ${importedCount}，跳过 ${skippedCount}`
      );
    }

    // Event binding is intentionally the last section of this file. If a bug is
    // "button does nothing" start here; if a bug is "summary/state wrong after
    // click" start from the task helpers above.
    function bindEvents() {
      if (eventsBound) {
        return;
      }

      eventsBound = true;
      bindAsyncClick(elements.organizerImportAdSamplesButton, async () => {
        await importLearningSamples('ad');
      });

      bindAsyncClick(elements.organizerHelpImportAdButton, async () => {
        await showLearningGuide('import-ad');
      });

      bindAsyncClick(elements.organizerImportNormalSamplesButton, async () => {
        await importLearningSamples('normal');
      });

      bindAsyncClick(elements.organizerHelpImportNormalButton, async () => {
        await showLearningGuide('import-normal');
      });

      bindAsyncClick(elements.organizerLearnAdByCodesButton, async () => {
        await runLearningTask('ad');
      });

      bindAsyncClick(elements.organizerHelpLearnAdButton, async () => {
        await showLearningGuide('learn-by-codes');
      });

      bindAsyncClick(elements.organizerLearnNormalByCodesButton, async () => {
        await runLearningTask('normal');
      });

      bindAsyncClick(elements.organizerHelpLearnNormalButton, async () => {
        await showLearningGuide('learn-by-codes');
      });

      bindAsyncClick(elements.organizerRefreshLearningSummaryButton, async () => {
        await syncAdLearningModel({ logSuccess: true });
        await refreshAdLearningSummary(true);
      });

      if (elements.organizerAdDetectionEnabled) {
        elements.organizerAdDetectionEnabled.addEventListener('change', () => {
          if (elements.organizerAdDetectionEnable) {
            elements.organizerAdDetectionEnable.checked = Boolean(elements.organizerAdDetectionEnabled.checked);
          }
          if (elements.organizerAdDetectionDisable) {
            elements.organizerAdDetectionDisable.checked = !elements.organizerAdDetectionEnabled.checked;
          }
          applyAdDetectionUiState();
          notifyAdDetectionModeChanged();
        });
      }

      if (elements.organizerAdDetectionEnable) {
        elements.organizerAdDetectionEnable.addEventListener('change', () => {
          if (!elements.organizerAdDetectionEnable.checked) {
            return;
          }
          if (elements.organizerAdDetectionEnabled) {
            elements.organizerAdDetectionEnabled.checked = true;
          }
          if (elements.organizerAdDetectionDisable) {
            elements.organizerAdDetectionDisable.checked = false;
          }
          applyAdDetectionUiState();
          notifyAdDetectionModeChanged();
        });
      }

      if (elements.organizerAdDetectionDisable) {
        elements.organizerAdDetectionDisable.addEventListener('change', () => {
          if (!elements.organizerAdDetectionDisable.checked) {
            return;
          }
          if (elements.organizerAdDetectionEnabled) {
            elements.organizerAdDetectionEnabled.checked = false;
          }
          if (elements.organizerAdDetectionEnable) {
            elements.organizerAdDetectionEnable.checked = false;
          }
          applyAdDetectionUiState();
          notifyAdDetectionModeChanged();
        });
      }

      if (elements.organizerAdModelType) {
        elements.organizerAdModelType.addEventListener('change', () => {
          elements.organizerAdModelType.value = normalizeAdModelType(elements.organizerAdModelType.value);
        });
      }
    }

    function applySettings(settings = {}) {
      // AI 广告检测默认关闭：只有设置显式为 true 时才启用；缺失/undefined 按 false 处理。
      const enabled = settings.organizerAdDetectionEnabled === true;
      if (elements.organizerAdDetectionEnabled) {
        elements.organizerAdDetectionEnabled.checked = enabled;
      }
      if (elements.organizerAdDetectionEnable) {
        elements.organizerAdDetectionEnable.checked = enabled;
      }
      if (elements.organizerAdDetectionDisable) {
        elements.organizerAdDetectionDisable.checked = !enabled;
      }
      if (elements.organizerAdModelType) {
        elements.organizerAdModelType.value = normalizeAdModelType(settings.organizerAdModelType);
      }
      if (elements.organizerAdThreshold) {
        elements.organizerAdThreshold.value = String(toSafeInteger(settings.organizerAdThreshold, 60, 1, 100));
      }
      if (elements.organizerAdKeywords) {
        elements.organizerAdKeywords.value = String(settings.organizerAdKeywords || '');
      }
      if (elements.organizerLearningCodes) {
        elements.organizerLearningCodes.value = '';
      }
      applyAdDetectionUiState();
    }

    return {
      isAdDetectionEnabled,
      applyAdDetectionUiState,
      getLearningConfig,
      renderLearningSummary,
      refreshAdLearningSummary,
      syncAdLearningModel,
      showLearningGuide,
      getLearningCodes,
      learnSamplesByCodes,
      runLearningTask,
      importLearningSamples,
      bindEvents,
      applySettings
    };
  }

  globalScope.desktopOrganizerLearningController = {
    createOrganizerLearningController
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
