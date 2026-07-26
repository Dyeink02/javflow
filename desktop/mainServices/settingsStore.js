// Shared desktop settings persistence used by legacy desktop modules and the
// Node sidecar compatibility path.
// compatibility-owner: active crawl-compatible desktop settings helper; marker=compat-mainservices-settings-store
//
// Maintenance boundary:
// - current Wails product settings should prefer Go-owned contracts
// - this store remains because sidecar compatibility code still reads the same
//   persisted desktop shape
// - path or filename changes here can silently break sidecar-only flows, so
//   compatibility edits should stay explicit and heavily commented
//
// Ownership summary:
// 1) persist the archived desktop-compatible settings shape
// 2) expose compatibility artifact paths used by sidecar-only helpers
// 3) seed lightweight local test/ranking-history scaffolds
//
// It should not become the policy owner for current Wails runtime behavior.

// File map for maintainers:
// 1) compatibility settings/artifact path getters
// 2) ranking-history / desktop-test scaffold writers
// 3) default settings and load/save persistence rules

function createSettingsStore({ app, fs, path, appInfo, magnetFilename }) {
  // Path getters below are the remaining compatibility storage contract for
  // Node sidecar / archived desktop helpers. The current Wails app should keep
  // product policy on the Go side, but these filenames must remain stable until
  // those callers are fully retired.
  function getSettingsPath() {
    return path.join(app.getPath('userData'), 'desktop-settings.json');
  }

  function getRankingCachePath() {
    return path.join(app.getPath('userData'), 'actress-ranking-cache.json');
  }

  function getDesktopTestOutputDir() {
    return path.join(app.getPath('temp'), 'jav-desktop-ui-test-output');
  }

  function getRankingHistoryDir() {
    return path.join(app.getPath('userData'), 'ranking-history');
  }

  function getRankingHistoryDirectories() {
    return [getRankingHistoryDir()];
  }

  function ensureRankingHistoryArtifacts() {
    // Ranking history artifacts are a compatibility scaffold for local import
    // workflows. Keep filenames stable here; they are not the source of truth
    // for current Wails ranking UX or product policy.
    const historyDir = getRankingHistoryDir();
    const guidePath = path.join(historyDir, 'ranking-history-guide.txt');
    const monthlyTemplatePath = path.join(historyDir, 'monthly-template.example.txt');
    const annualTemplatePath = path.join(historyDir, 'annual-template.example.txt');

    fs.mkdirSync(historyDir, { recursive: true });

    if (!fs.existsSync(guidePath)) {
      fs.writeFileSync(
        guidePath,
        [
          '本地历史榜单目录说明',
          '',
          '1. 你可以把历史榜单直接写成 JSON 文件后放进当前目录，软件会自动读取。',
          '2. 月榜建议文件名：2026-01-monthly.json',
          '3. 年榜建议文件名：2025-annual.json',
          '4. 支持字段：mode, sourceName, sourceUrl, title, periodLabel, periodYear, periodMonth, total, items',
          '5. items 内每条至少包含：rank, actressName',
          '',
          '如果你暂时没有真实数据，请不要直接伪造榜单内容。'
        ].join('\r\n'),
        'utf8'
      );
    }

    if (!fs.existsSync(monthlyTemplatePath)) {
      fs.writeFileSync(
        monthlyTemplatePath,
        [
          '{',
          '  "mode": "monthly",',
          '  "sourceName": "本地历史导入",',
          '  "sourceUrl": "",',
          '  "title": "2026年01月 本地历史月榜",',
          '  "periodLabel": "2026年01月",',
          '  "periodYear": 2026,',
          '  "periodMonth": 1,',
          '  "total": 2,',
          '  "items": [',
          '    { "rank": 1, "actressName": "示例女优A", "profileUrl": "", "imageUrl": "" },',
          '    { "rank": 2, "actressName": "示例女优B", "profileUrl": "", "imageUrl": "" }',
          '  ]',
          '}'
        ].join('\r\n'),
        'utf8'
      );
    }

    if (!fs.existsSync(annualTemplatePath)) {
      fs.writeFileSync(
        annualTemplatePath,
        [
          '{',
          '  "mode": "annual",',
          '  "sourceName": "本地历史导入",',
          '  "sourceUrl": "",',
          '  "title": "2025年 本地历史年榜",',
          '  "periodLabel": "2025年",',
          '  "periodYear": 2025,',
          '  "periodMonth": null,',
          '  "total": 2,',
          '  "items": [',
          '    { "rank": 1, "actressName": "示例女优A", "profileUrl": "", "imageUrl": "" },',
          '    { "rank": 2, "actressName": "示例女优B", "profileUrl": "", "imageUrl": "" }',
          '  ]',
          '}'
        ].join('\r\n'),
        'utf8'
      );
    }

    return {
      historyDir,
      guidePath,
      monthlyTemplatePath,
      annualTemplatePath
    };
  }

  function ensureDesktopTestArtifacts(outputDir = getDesktopTestOutputDir()) {
    // Desktop test artifacts are a local smoke-test convenience path only.
    // Product features should not start depending on these placeholder files.
    fs.mkdirSync(outputDir, { recursive: true });

    const magnetFilePath = path.join(outputDir, magnetFilename);
    if (!fs.existsSync(magnetFilePath)) {
      fs.writeFileSync(magnetFilePath, 'magnet:?xt=urn:btih:desktop-test\n', 'utf8');
    }

    return {
      outputDir,
      magnetFilePath
    };
  }

  function getDefaultSettings() {
    // Default settings remain the shared compatibility shape consumed by old
    // desktop helpers and sidecar-only flows. Additions here should stay aligned
    // with Go-side settings contracts instead of inventing JS-only state.
    return {
      base: appInfo.defaultBaseUrl || 'https://www.javbus.com',
      output: path.join(app.getPath('documents'), appInfo.outputFolderName || 'JavFlow输出'),
      limit: 10,
      totalPages: 0,
      itemsPerPage: 30,
      parallel: 2,
      delay: 2,
      timeout: 30000,
      proxy: '',
      magnetExcludeKeywords: '',
      magnetContentValidation: false,
      cloudflare: false,
      nomag: false,
      allmag: false,
      nopic: false,
      secondValidation: true,
      taskTemplate: 'balanced',
      backgroundImage: '',
      organizerRoot: '',
      organizerMinSizeMB: 100,
      organizerSuffix: '-A',
      organizerVideoExtensions: 'mp4, mkv, avi, mov, flv, wmv, ts, m4v, iso',
      organizerAdFileAction: 'move-to-delete',
      organizerDryRun: false,
      organizerIncludeSubdirectories: true,
      organizerCrawlOutput: '',
      organizerStrictCodeMatch: true,
      organizerAdDetectionEnabled: false,
      organizerAdThreshold: 60,
      organizerAdKeywords: '',
      organizerAdModelType: 'mobile-net-v3-lite'
    };
  }

  function loadSettings() {
    // Load remains tolerant because historical desktop builds may have persisted
    // older shapes. Keep normalization narrow and let Go-side settings decide
    // current product semantics.
    const defaults = getDefaultSettings();
    const filePath = getSettingsPath();

    if (!fs.existsSync(filePath)) {
      return defaults;
    }

    try {
      const loadedSettings = {
        ...defaults,
        ...JSON.parse(fs.readFileSync(filePath, 'utf8'))
      };
      delete loadedSettings.exportCoverImages;
      return loadedSettings;
    } catch {
      return defaults;
    }
  }

  function saveSettings(settings) {
    // Runtime-only fields are intentionally stripped here so compatibility
    // persistence stays focused on durable user preferences.
    const persistedSettings = {
      ...loadSettings(),
      ...settings
    };
    delete persistedSettings.resumeExisting;
    delete persistedSettings.exportCoverImages;

    fs.mkdirSync(path.dirname(getSettingsPath()), { recursive: true });
    fs.writeFileSync(getSettingsPath(), JSON.stringify(persistedSettings, null, 2), 'utf8');
  }

  function getCurrentOutputDir() {
    return loadSettings().output || app.getPath('documents');
  }

  function getMagnetFilePath(outputDir) {
    // Magnet output naming remains part of the compatibility storage contract
    // because old desktop helpers still derive file paths from settings only.
    return path.join(outputDir || getCurrentOutputDir(), magnetFilename);
  }

  return {
    getSettingsPath,
    getRankingCachePath,
    getRankingHistoryDir,
    getRankingHistoryDirectories,
    ensureRankingHistoryArtifacts,
    getDesktopTestOutputDir,
    ensureDesktopTestArtifacts,
    getDefaultSettings,
    loadSettings,
    saveSettings,
    getCurrentOutputDir,
    getMagnetFilePath
  };
}

module.exports = {
  createSettingsStore
};
