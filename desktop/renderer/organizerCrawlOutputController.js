// Organizer crawl-output controller for the current desktop renderer.
// It resolves crawl artifacts into organizer preload state and keeps that
// artifact handoff separate from core organizer execution UI behavior.
//
// Ownership summary:
// 1) resolve artifact input into organizer preload snapshots
// 2) keep source-type/source-path/code-count metadata consistent in the UI
// 3) isolate crawl-output handoff from organizer run-state behavior
//
// File map for maintainers:
// 1) artifact input normalization and source-type helpers
// 2) organizer preload state synchronization
// 3) crawl-output import/browse/reset actions

(function initializeOrganizerCrawlOutputController(globalScope) {
  function createOrganizerCrawlOutputController(options) {
    const { elements, desktopApi, state, messages, appendLogLine, getErrorMessage } = options;
    const rendererHelpers = globalScope.desktopRendererHelpers || {};
    const clearChildren = rendererHelpers.clearChildren;
    const createOption = rendererHelpers.createOption;
    if (typeof clearChildren !== 'function' || typeof createOption !== 'function') {
      throw new Error('desktopRendererHelpers option helpers are required before organizerCrawlOutputController');
    }
    const artifactInputHelperFactory = globalScope.desktopArtifactInputHelper || null;
    if (!artifactInputHelperFactory) {
      throw new Error('desktopArtifactInputHelper is required before organizerCrawlOutputController');
    }
    const artifactInputHelper = artifactInputHelperFactory.createArtifactInputHelper({
      labels: {
        snapshot: '整理快照',
        fallback: '爬虫结果输入'
      },
      latestInputOptions: {
        artifactKey: 'preferredOrganizerCodesPath',
        artifactType: 'organizerCodes'
      },
      readCurrentValue: () => getCurrentOutputDir(),
      writeCurrentValue: (artifactInput) => applyOrganizerArtifactInputValue(artifactInput)
    });
    let eventsBound = false;

    function normalizeOutputDir(value) {
      return String(value || '').trim();
    }

    function sameExpectedSourcePath(left, right) {
      const normalizedLeft = normalizeOutputDir(left);
      const normalizedRight = normalizeOutputDir(right);
      if (!normalizedLeft || !normalizedRight) {
        return false;
      }

      return normalizedLeft.toLowerCase() === normalizedRight.toLowerCase();
    }

    function resolveLoadedExpectedSourceType(sourceType, options = {}) {
      // Source-type resolution centralizes legacy alias handling so organizer
      // preload state does not branch all over the renderer.
      const normalizedSourceType = normalizeOutputDir(sourceType);
      if (normalizedSourceType) {
        return normalizedSourceType;
      }

      const sourcePath = normalizeOutputDir(options.sourcePath);
      const filmDataPath = normalizeOutputDir(options.filmDataPath);
      const organizerCodesPath = normalizeOutputDir(options.organizerCodesPath);
      const hasCodes = Boolean(options.hasCodes);

      if (sameExpectedSourcePath(sourcePath, organizerCodesPath)) {
        return 'organizerCodes';
      }
      if (sameExpectedSourcePath(sourcePath, filmDataPath)) {
        return 'filmData';
      }
      if (organizerCodesPath && !filmDataPath) {
        return 'organizerCodes';
      }
      if (filmDataPath && !organizerCodesPath) {
        return 'filmData';
      }
      if (sourcePath || hasCodes) {
        return 'payload';
      }

      return '';
    }

    function resolveLoadedExpectedSourcePath(sourceType, options = {}) {
      const normalizedSourceType = normalizeOutputDir(sourceType);
      const sourcePath = normalizeOutputDir(options.sourcePath);
      const filmDataPath = normalizeOutputDir(options.filmDataPath);
      const organizerCodesPath = normalizeOutputDir(options.organizerCodesPath);

      if (normalizedSourceType === 'organizerCodes') {
        return organizerCodesPath || sourcePath || filmDataPath;
      }
      if (normalizedSourceType === 'filmData') {
        return filmDataPath || sourcePath || organizerCodesPath;
      }
      if (normalizedSourceType === 'payload') {
        return sourcePath || filmDataPath || organizerCodesPath;
      }

      return sourcePath || organizerCodesPath || filmDataPath;
    }

    function updateCodeMetaView(meta = {}) {
      // Meta view projects one normalized preload snapshot only. It should not
      // infer business status beyond source path/type and code count.
      if (elements.organizerCodeCount) {
        elements.organizerCodeCount.textContent = String(meta.codeCount || 0);
      }

      if (elements.organizerCodeSource) {
        if (meta.sourcePath) {
          const sourceTypeLabel =
            meta.sourceType === 'organizerCodes'
              ? '整理快照'
              : meta.sourceType === 'filmData'
                ? 'filmData'
                : '来源';
          elements.organizerCodeSource.textContent = `${sourceTypeLabel}: ${meta.sourcePath}`;
        } else {
          elements.organizerCodeSource.textContent = '来源：尚未加载番号名单';
        }
      }

      if (elements.organizerMagnetCount) {
        const actualMagnetCount = Number(meta.actualMagnetCount);
        elements.organizerMagnetCount.textContent =
          Number.isFinite(actualMagnetCount) && actualMagnetCount >= 0
            ? `${actualMagnetCount}（不含屏蔽番号）`
            : '尚未读取 magnet-links.txt';
      }
    }

    // preloadedExpected is the single source of truth for organizer-side crawl
    // snapshot state in the renderer. All display/meta helpers below derive
    // from that one snapshot so we do not reintroduce parallel state fields.
    function getLoadedExpectedSnapshot() {
      return state.preloadedExpected && typeof state.preloadedExpected === 'object' ? state.preloadedExpected : null;
    }

    function buildLoadedSourceMeta(loaded) {
      if (!loaded) {
        return {
          sourcePath: '',
          sourceType: ''
        };
      }

      const sourceType = resolveLoadedExpectedSourceType(loaded.sourceType, {
        sourcePath: loaded.sourcePath,
        filmDataPath: loaded.filmDataPath,
        organizerCodesPath: loaded.organizerCodesPath,
        hasCodes:
          (Array.isArray(loaded.codes) && loaded.codes.length > 0) ||
          (Array.isArray(loaded.codeEntries) && loaded.codeEntries.length > 0)
      });

      return {
        sourcePath: resolveLoadedExpectedSourcePath(sourceType, {
          sourcePath: loaded.sourcePath,
          filmDataPath: loaded.filmDataPath,
          organizerCodesPath: loaded.organizerCodesPath
        }),
        sourceType
      };
    }

    function getLoadedSourceMeta() {
      return buildLoadedSourceMeta(getLoadedExpectedSnapshot());
    }

    // Snapshot accessors below should stay read-only. If future maintenance
    // needs extra organizer preload fields, extend preloadedExpected once and
    // keep all derived UI/meta reads flowing through this section.
    function getLoadedCodes() {
      const loaded = getLoadedExpectedSnapshot();
      return Array.isArray(loaded && loaded.codes) ? loaded.codes : [];
    }

    function getLoadedCodeEntries() {
      const loaded = getLoadedExpectedSnapshot();
      return Array.isArray(loaded && loaded.codeEntries) ? loaded.codeEntries : [];
    }

    function clearLoadedExpectedState(options = {}) {
      const { updateMeta = true } = options;

      state.preloadedExpected = null;

      if (updateMeta) {
        updateCodeMetaView({
          codeCount: 0,
          sourcePath: '',
          sourceType: ''
        });
      }
    }

    function getCurrentOutputDir() {
      return normalizeOutputDir(elements.organizerCrawlOutput && elements.organizerCrawlOutput.value);
    }

    function appendOrganizerLog(level, message) {
      appendLogLine(elements.organizerLogView, level, message);
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
          appendOrganizerLog('error', getErrorMessage(error));
        }
      });
    }

    function getLoadedSnapshotOutputDir() {
      const loaded = getLoadedExpectedSnapshot();
      return normalizeOutputDir(loaded && loaded.outputDir);
    }

    // Expected-code snapshots are tied to one crawl-output directory. When the
    // textbox changes, clear the old snapshot so organizer does not silently
    // reuse stale codes for a different crawl run.
    function syncLoadedExpectedStateForOutput(nextOutputDir, options = {}) {
      // Output-dir changes invalidate the preloaded snapshot because codes are
      // tied to one crawl artifact root. Keep that invalidation rule here.
      const { updateMeta = true } = options;
      const normalizedOutputDir = normalizeOutputDir(nextOutputDir);
      const loadedOutputDir = getLoadedSnapshotOutputDir();
      if (!loadedOutputDir || loadedOutputDir === normalizedOutputDir) {
        return;
      }
      clearLoadedExpectedState({ updateMeta });
    }

    function cloneLoadedCodes(rawCodes) {
      if (!Array.isArray(rawCodes)) {
        return [];
      }

      return rawCodes.map((code) => normalizeOutputDir(code)).filter(Boolean);
    }

    function cloneLoadedCodeEntries(rawEntries) {
      if (!Array.isArray(rawEntries)) {
        return [];
      }

      return rawEntries
        .map((entry) => {
          const code = normalizeOutputDir(entry && entry.code);
          if (!code) {
            return null;
          }

          const magnets = Array.isArray(entry && entry.magnets)
            ? entry.magnets.map((magnet) => ({
                ...magnet
              }))
            : [];

          return {
            ...entry,
            code,
            magnets
          };
        })
        .filter(Boolean);
    }

    function normalizeFilteredCodes(rawEntries) {
      if (!Array.isArray(rawEntries)) {
        return [];
      }

      return rawEntries
        .map((entry) => {
          const code = normalizeOutputDir(entry && entry.code);
          if (!code) {
            return null;
          }

          return {
            code,
            reason: normalizeOutputDir(entry && entry.reason),
            remark: normalizeOutputDir(entry && entry.remark)
          };
        })
        .filter(Boolean);
    }

    function formatFilteredCodesMessage(filteredCodes) {
      if (!Array.isArray(filteredCodes) || filteredCodes.length === 0) {
        return '';
      }

      const codeList = filteredCodes.map((entry) => entry.code).join('、');
      return `已过滤 ${filteredCodes.length} 部：${codeList}`;
    }

    // Compatibility normalization is intentionally concentrated here. Older
    // payload aliases and newer artifact bundles must both land in one stable
    // renderer snapshot so organizer debugging never has to branch on source
    // shape first.
    // Some compatibility/Electron-era paths still return top-level
    // codes/codeEntries without the newer preloadedExpected bundle. Normalize
    // both shapes into the same single renderer snapshot so organizer state
    // does not disappear when those old return payloads are exercised.
    function normalizeLoadedExpectedResult(result, outputDir) {
      // Compatibility normalization is concentrated here so older payload
      // aliases and newer bundles both land in one stable renderer snapshot.
      const preloadedExpected =
        result && result.preloadedExpected && typeof result.preloadedExpected === 'object'
          ? {
              ...result.preloadedExpected
            }
          : null;
      const loadedCodes = cloneLoadedCodes(result && result.codes);
      const loadedCodeEntries = cloneLoadedCodeEntries(result && result.codeEntries);
      const fallbackOutputDir = normalizeOutputDir((result && result.outputDir) || outputDir);
      const fallbackFilmDataPath = normalizeOutputDir(result && result.filmDataPath);
      const fallbackOrganizerCodesPath = normalizeOutputDir(result && result.organizerCodesPath);
      const fallbackExplicitSourcePath = normalizeOutputDir(result && result.sourcePath);
      const fallbackSourceType = resolveLoadedExpectedSourceType(normalizeOutputDir(result && result.sourceType), {
        sourcePath: fallbackExplicitSourcePath,
        filmDataPath: fallbackFilmDataPath,
        organizerCodesPath: fallbackOrganizerCodesPath,
        hasCodes: loadedCodes.length > 0 || loadedCodeEntries.length > 0
      });
      const fallbackSourcePath = resolveLoadedExpectedSourcePath(fallbackSourceType, {
        sourcePath: fallbackExplicitSourcePath,
        filmDataPath: fallbackFilmDataPath,
        organizerCodesPath: fallbackOrganizerCodesPath
      });

      if (
        !preloadedExpected &&
        !fallbackOutputDir &&
        !fallbackSourcePath &&
        loadedCodes.length === 0 &&
        loadedCodeEntries.length === 0
      ) {
        return null;
      }

      const normalized = preloadedExpected || {
        sourceType: fallbackSourceType,
        sourcePath: fallbackSourcePath,
        outputDir: fallbackOutputDir,
        filmDataPath: fallbackFilmDataPath,
        organizerCodesPath: fallbackOrganizerCodesPath,
        actressName: normalizeOutputDir(result && result.actressName),
        totalRecords: Number(result && result.totalRecords) || 0,
        codeCount: Number(result && result.codeCount) || 0
      };

      normalized.sourceType = normalizeOutputDir(normalized.sourceType || fallbackSourceType);
      normalized.sourcePath = normalizeOutputDir(normalized.sourcePath || fallbackSourcePath);
      normalized.outputDir = normalizeOutputDir(normalized.outputDir || fallbackOutputDir);
      normalized.filmDataPath = normalizeOutputDir(normalized.filmDataPath || fallbackFilmDataPath);
      normalized.organizerCodesPath = normalizeOutputDir(normalized.organizerCodesPath || fallbackOrganizerCodesPath);
      normalized.actressName = normalizeOutputDir(normalized.actressName || (result && result.actressName));
      normalized.totalRecords = Number(normalized.totalRecords) || Number(result && result.totalRecords) || 0;
      const actualMagnetCount = Number(
        normalized.actualMagnetCount != null ? normalized.actualMagnetCount : result && result.actualMagnetCount
      );
      normalized.actualMagnetCount = Number.isFinite(actualMagnetCount) ? actualMagnetCount : -1;
      normalized.magnetPath = normalizeOutputDir(normalized.magnetPath || (result && result.magnetPath));
      normalized.codes = loadedCodes.length > 0 ? loadedCodes : cloneLoadedCodes(normalized.codes);
      normalized.codeEntries =
        loadedCodeEntries.length > 0 ? loadedCodeEntries : cloneLoadedCodeEntries(normalized.codeEntries);
      normalized.codeCount = Math.max(
        Number(normalized.codeCount) || Number(result && result.codeCount) || 0,
        normalized.codes.length
      );
      normalized.filteredCodes = normalizeFilteredCodes(result && result.filteredCodes);

      normalized.sourceType = resolveLoadedExpectedSourceType(normalized.sourceType, {
        sourcePath: normalized.sourcePath,
        filmDataPath: normalized.filmDataPath,
        organizerCodesPath: normalized.organizerCodesPath,
        hasCodes: normalized.codes.length > 0 || normalized.codeEntries.length > 0
      });
      normalized.sourcePath = resolveLoadedExpectedSourcePath(normalized.sourceType, {
        sourcePath: normalized.sourcePath,
        filmDataPath: normalized.filmDataPath,
        organizerCodesPath: normalized.organizerCodesPath
      });

      return normalized;
    }

    function buildLoadedSnapshotView(loadedSnapshot, fallbackOutputDir) {
      const sourceMeta = buildLoadedSourceMeta(loadedSnapshot);
      const loadedCodes = Array.isArray(loadedSnapshot && loadedSnapshot.codes) ? loadedSnapshot.codes : [];

      return {
        loadedSnapshot,
        loadedCodes,
        sourceMeta,
        resolvedOutputDir: sourceMeta.sourcePath || fallbackOutputDir
      };
    }

    function applyLoadedExpectedResult(result, outputDir) {
      const loadedSnapshot = normalizeLoadedExpectedResult(result, outputDir);
      state.preloadedExpected = loadedSnapshot;
      const snapshotView = buildLoadedSnapshotView(loadedSnapshot, outputDir);

      updateCodeMetaView({
        codeCount: snapshotView.loadedCodes.length,
        sourcePath: snapshotView.sourceMeta.sourcePath,
        sourceType: snapshotView.sourceMeta.sourceType,
        actualMagnetCount: loadedSnapshot && loadedSnapshot.actualMagnetCount
      });

      appendLogLine(
        elements.organizerLogView,
        'info',
        `番号名单加载完成：${snapshotView.loadedCodes.length} 条（${snapshotView.sourceMeta.sourceType || 'artifact'}: ${snapshotView.resolvedOutputDir}）`
      );

      const filteredMessage = formatFilteredCodesMessage(loadedSnapshot && loadedSnapshot.filteredCodes);
      if (filteredMessage) {
        appendLogLine(elements.organizerLogView, 'warn', filteredMessage);
      }
    }

    // This is the renderer's handoff to the organizer run payload. It returns
    // a snapshot only when it matches the currently selected crawl output dir.
    // That keeps strict-match organizer runs from silently mixing old and new
    // crawl outputs.
    function getLoadedSnapshotForOutput(outputDir) {
      const normalizedOutputDir = normalizeOutputDir(outputDir);
      syncLoadedExpectedStateForOutput(normalizedOutputDir);

      const loaded = getLoadedExpectedSnapshot();
      if (!loaded) {
        return null;
      }

      const loadedOutputDir = normalizeOutputDir(loaded.outputDir);
      if (!loadedOutputDir || loadedOutputDir !== normalizedOutputDir) {
        return null;
      }

      return {
        ...loaded,
        codes: [...getLoadedCodes()],
        codeEntries: [...getLoadedCodeEntries()]
      };
    }

    // Main organizer controller should consume this one boundary helper instead
    // of re-reading the textbox and snapshot separately. That keeps the
    // renderer-side "current crawl import input" contract in one place.
    function getCurrentOrganizerInputState() {
      const crawlOutputDir = getCurrentOutputDir();
      return {
        crawlOutputDir,
        preloadedExpected: getLoadedSnapshotForOutput(crawlOutputDir)
      };
    }

    function applyOrganizerArtifactInputValue(artifactInput) {
      const normalizedValue = normalizeOutputDir(artifactInput);
      if (elements.organizerCrawlOutput) {
        elements.organizerCrawlOutput.value = normalizedValue;
      }
      return normalizedValue;
    }

    // Organizer preload textbox is the only renderer-side crawl-artifact input
    // boundary for this module. Load-code flows should go through this helper so
    // autofill, textbox reuse and corresponding log wording stay aligned.
    async function resolveOrganizerArtifactInputState(options = {}) {
      return artifactInputHelper.resolveArtifactInputState(desktopApi, null, options);
    }

    // This only fills the textbox with the latest crawl-derived artifact input.
    // The organizer run still treats preloadedExpected as the single renderer
    // snapshot and treats crawlOutputDir as the lazy-load fallback boundary.
    async function applyLatestCrawlOutput() {
      const { artifactInput, message } = await artifactInputHelper.fillLatestArtifactInput(desktopApi);
      if (artifactInput) {
        syncLoadedExpectedStateForOutput(artifactInput);
        appendOrganizerLog('info', message || `已填入最近爬虫结果输入：${artifactInput}`);
      }
    }

    // Organizer code preload accepts an artifact path or a crawl output dir.
    // The current renderer path forwards artifactInput only; service-side
    // resolution decides whether to read organizer-codes.json or filmData.json
    // from that root, and bridge compatibility keeps older field aliases alive.
    async function loadExpectedCodes() {
      const inputState = await resolveOrganizerArtifactInputState({ mode: 'autofill' });
      const artifactInput = inputState.artifactInput;
      if (inputState.message) {
        syncLoadedExpectedStateForOutput(artifactInput);
        appendOrganizerLog('info', inputState.message);
      }
      if (!artifactInput) {
        throw new Error(messages.crawlOutputRequired);
      }

      const result = await desktopApi.loadCrawlFilmCodes({ artifactInput });
      applyLoadedExpectedResult(result, artifactInput);
      return result;
    }

    function setCrawlInputValue(value) {
      const normalizedValue = applyOrganizerArtifactInputValue(value);
      syncLoadedExpectedStateForOutput(normalizedValue);
      return normalizedValue;
    }

    async function browseCrawlFile() {
      if (!desktopApi || typeof desktopApi.chooseOrganizerCrawlFile !== 'function') {
        throw new Error('JSON 文件选择器尚未就绪');
      }
      const selected = await desktopApi.chooseOrganizerCrawlFile();
      if (!selected) {
        return null;
      }
      setCrawlInputValue(selected);
      return loadExpectedCodes();
    }

    async function browseCrawlFolder() {
      if (!desktopApi || typeof desktopApi.chooseOutput !== 'function') {
        throw new Error('文件夹选择器尚未就绪');
      }
      const selected = await desktopApi.chooseOutput();
      if (!selected) {
        return null;
      }
      setCrawlInputValue(selected);
      return loadExpectedCodes();
    }

    function renderCrawlHistory(items) {
      const select = elements.organizerCrawlOutputHistory;
      if (!select) {
        return;
      }
      clearChildren(select);
      select.appendChild(createOption('', '选择历史抓取快照'));
      (Array.isArray(items) ? items : []).forEach((item, index) => {
        const artifactInput = normalizeOutputDir(item && (item.organizerCodesPath || item.filmDataPath || item.outputDir));
        if (!artifactInput) {
          return;
        }
        const actressName = normalizeOutputDir(item.actressName) || '未命名任务';
        const updatedAt = normalizeOutputDir(item.updatedAt).replace('T', ' ').slice(0, 16);
        const completedCount = Number(item.completedCount) || 0;
        const option = createOption(
          artifactInput,
          `${actressName}${updatedAt ? ` | ${updatedAt}` : ''} | ${completedCount} 部`
        );
        option.dataset.historyIndex = String(index);
        select.appendChild(option);
      });
    }

    async function loadCrawlHistory() {
      if (!desktopApi || typeof desktopApi.listCrawlCacheSnapshots !== 'function') {
        return;
      }
      try {
        const result = await desktopApi.listCrawlCacheSnapshots();
        renderCrawlHistory(result && result.items);
      } catch (error) {
        appendOrganizerLog('warn', `读取历史抓取快照失败：${getErrorMessage(error)}`);
      }
    }

    // Read-only fallback for legacy libraries: identify unambiguous codes from
    // local video paths, persist the hidden snapshot, and feed that snapshot
    // through the same preload boundary as crawler artifacts.
    async function discoverLocalCodes() {
      const rootPath = normalizeOutputDir(elements.organizerRoot && elements.organizerRoot.value);
      if (!rootPath) {
        throw new Error('请先选择目标根目录');
      }
      const videoExtensions = normalizeOutputDir(
        elements.organizerVideoExtensions && elements.organizerVideoExtensions.value
      );
      const result = await desktopApi.discoverOrganizerCodes({
        rootPath,
        includeSubdirectories: Boolean(
          elements.organizerIncludeSubdirectories && elements.organizerIncludeSubdirectories.checked
        ),
        videoExtensions
      });
      const resolvedRoot = result.rootPath || rootPath;
      if (elements.organizerCrawlOutput) {
        elements.organizerCrawlOutput.value = resolvedRoot;
      }
      applyLoadedExpectedResult(
        {
          ...result,
          outputDir: resolvedRoot,
          sourceType: 'local-discovery',
          sourcePath: result.statePath,
          filmDataPath: result.statePath,
          codeCount: Array.isArray(result.codes) ? result.codes.length : 0
        },
        resolvedRoot
      );
      appendOrganizerLog(
        result.unidentifiedFiles > 0 ? 'warn' : 'info',
        `本地番号识别完成：识别 ${Array.isArray(result.codes) ? result.codes.length : 0} 个番号，视频 ${result.videoFiles || 0} 个，未识别 ${result.unidentifiedFiles || 0} 个。隐藏快照：${result.statePath || '未生成'}${result.reportPath ? `；未命中清单：${result.reportPath}` : ''}`
      );
      return result;
    }

    // Event binding stays intentionally narrow: this controller only owns
    // crawl-output preload input and expected-code snapshot state. Organizer run
    // execution buttons remain in organizerController.
    function bindEvents() {
      if (eventsBound) {
        return;
      }

      eventsBound = true;
      if (elements.organizerCrawlOutput) {
        const invalidateLoadedState = () => {
          syncLoadedExpectedStateForOutput(getCurrentOutputDir());
        };
        elements.organizerCrawlOutput.addEventListener('input', invalidateLoadedState);
        elements.organizerCrawlOutput.addEventListener('change', invalidateLoadedState);
      }

      bindAsyncClick(elements.organizerBrowseCrawlFileButton, browseCrawlFile);
      bindAsyncClick(elements.organizerBrowseCrawlFolderButton, browseCrawlFolder);
      if (elements.organizerCrawlOutputHistory) {
        elements.organizerCrawlOutputHistory.addEventListener('change', () => {
          const selected = normalizeOutputDir(elements.organizerCrawlOutputHistory.value);
          if (!selected) {
            return;
          }
          setCrawlInputValue(selected);
          void loadExpectedCodes().catch((error) => appendOrganizerLog('error', getErrorMessage(error)));
        });
      }
      void loadCrawlHistory();

      bindAsyncClick(
        elements.organizerLoadCodesButton,
        loadExpectedCodes,
        (error) => {
          const sourceMeta = getLoadedSourceMeta();
          appendOrganizerLog('error', getErrorMessage(error));
          updateCodeMetaView({
            codeCount: getLoadedCodes().length,
            sourcePath: sourceMeta.sourcePath,
            sourceType: sourceMeta.sourceType,
            actualMagnetCount: getLoadedExpectedSnapshot() && getLoadedExpectedSnapshot().actualMagnetCount
          });
        }
      );
      bindAsyncClick(elements.organizerDiscoverCodesButton, discoverLocalCodes);
    }

    function resetCodeMetaView() {
      clearLoadedExpectedState({ updateMeta: true });
    }

    return {
      updateCodeMetaView,
      getCurrentOrganizerInputState,
      applyLatestCrawlOutput,
      loadExpectedCodes,
      discoverLocalCodes,
      bindEvents,
      resetCodeMetaView,
      getLoadedSnapshotForOutput
    };
  }

  globalScope.desktopOrganizerCrawlOutputController = {
    createOrganizerCrawlOutputController
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
