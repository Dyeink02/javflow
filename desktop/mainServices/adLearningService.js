// Legacy ad-learning implementation reused by the Node sidecar facade.
// deprecated: archived JS adlearning compatibility only; marker=archived-mainservices-adlearning-service
// This module is not part of the default Go crawl path, but it still powers
// compatibility-side organizer/ad-learning workflows.
//
// Current maintenance rule:
// - do not add new default organizer behavior here
// - do not treat this file as the preferred ad-learning path in the Wails app
// - current organizer/ad-learning product changes should land in the Go path
//   first, using this file only when archived compatibility absolutely needs it
// - if the active Wails app changes ad-learning behavior, update the Go path
//   first and mirror here only for deliberate archived compatibility support
//
// Ownership summary:
// 1) own the archived JS-side ad-learning model, sample import, and scoring flow
// 2) manage local ad-learning model/cache artifacts used by compatibility lanes
// 3) stay behind the sidecar facade instead of becoming a default organizer path
//
// File map for maintainers:
// 1) model/cache metadata and persistence helpers
// 2) FFmpeg/frame extraction and media fingerprint helpers
// 3) learning entrypoints for sample import and code-driven learning
// 4) scoring/evaluation helpers for ad-risk classification

const { execFile } = require('child_process');
const crypto = require('crypto');

function createAdLearningService({ app, fs, path }) {
  // Troubleshooting rule:
  // - ffmpeg/frame extraction issues belong in the media helper section
  // - learning-source scan issues belong around `learnSamplesByCodes*`
  // - score drift issues belong in the sample/template comparison helpers

  const MODEL_VERSION = 1;
  const DEFAULT_AD_MODEL_TYPE = 'mobile-net-v3-lite';
  const nowIso = () => new Date().toISOString();
  const AD_MODEL_PRESETS = Object.freeze({
    'mobile-net-v3-lite': {
      id: 'mobile-net-v3-lite',
      label: 'MobileNetV3 Lite',
      frameSeconds: [3, 8, 15],
      keywordScorePerHit: 15,
      keywordScoreMax: 40,
      domainPatternScore: 30,
      templateScore: { high: 55, medium: 35, low: 15 },
      adSampleScore: { high: 45, medium: 30, low: 12 },
      normalSamplePenalty: { high: 45, medium: 25 }
    },
    'squeezenet-fast': {
      id: 'squeezenet-fast',
      label: 'SqueezeNet Fast',
      frameSeconds: [2, 6, 11],
      keywordScorePerHit: 13,
      keywordScoreMax: 36,
      domainPatternScore: 24,
      templateScore: { high: 48, medium: 30, low: 12 },
      adSampleScore: { high: 38, medium: 24, low: 10 },
      normalSamplePenalty: { high: 38, medium: 20 }
    },
    'yolov8n-balanced': {
      id: 'yolov8n-balanced',
      label: 'YOLOv8n Balanced',
      frameSeconds: [2, 5, 8, 12, 15],
      keywordScorePerHit: 17,
      keywordScoreMax: 45,
      domainPatternScore: 32,
      templateScore: { high: 62, medium: 40, low: 20 },
      adSampleScore: { high: 52, medium: 35, low: 15 },
      normalSamplePenalty: { high: 48, medium: 28 }
    }
  });
  const DEFAULT_MODEL = {
    version: MODEL_VERSION,
    updatedAt: '',
    keywords: [],
    thresholds: {
      adScore: 60,
      highSimilarityDistance: 10,
      mediumSimilarityDistance: 16,
      lowSimilarityDistance: 22
    },
    adSamples: [],
    normalSamples: [],
    introTemplates: [],
    meta: {
      activeModel: DEFAULT_AD_MODEL_TYPE
    },
    metrics: {
      lastLearning: null,
      totalLearningRuns: 0
    }
  };
  const IMAGE_EXTENSIONS = new Set(['.jpg', '.jpeg', '.png', '.bmp', '.webp']);
  const VIDEO_EXTENSIONS = new Set(['.mp4', '.mkv', '.avi', '.mov', '.wmv', '.flv', '.ts', '.m4v']);
  const MANAGED_DIR_NAMES = new Set(['待整理', '待删除', 'logs', '.video-organizer-state']);
  const MANAGED_DIR_NAMES_LOWER = new Set(Array.from(MANAGED_DIR_NAMES).map((item) => String(item || '').trim().toLowerCase()));
  const DEFAULT_IGNORED_DIR_NAMES = new Set(['2048', '宣传文件', '宣傳文件']);
  const DEFAULT_VIDEO_FRAME_SECONDS = [3, 8, 15];
  const URL_PATTERN = /[a-z0-9-]+\.(com|net|org|cn|cc|tv|xyz|me|vip|top)/i;
  const BUNDLED_FFMPEG_RELATIVE_PATH = path.join('tools', 'ffmpeg', 'ffmpeg.exe');
  const HASH_CACHE_VERSION = 1;
  const HASH_CACHE_MAX_ITEMS = 12000;
  let ffmpegAvailable = null;
  let ffmpegCommand = '';
  let hashCache = null;

  function getModelPath() {
    return path.join(app.getPath('userData'), 'ad-learning-model.json');
  }

  function getHashCachePath() {
    return path.join(app.getPath('userData'), 'ad-learning-hash-cache.json');
  }

  function ensureParentDir(filePath) {
    fs.mkdirSync(path.dirname(filePath), { recursive: true });
  }

  function uniqueText(values = []) {
    return Array.from(
      new Set(
        values
          .map((item) => String(item || '').trim().toLowerCase())
          .filter(Boolean)
      )
    );
  }

  function normalizeThreshold(value, fallback) {
    const parsed = Number.parseInt(String(value ?? '').trim(), 10);
    if (!Number.isFinite(parsed)) {
      return fallback;
    }
    return Math.max(1, Math.min(100, parsed));
  }

  function clamp(value, min, max) {
    return Math.max(min, Math.min(max, value));
  }

  function normalizeAdModelType(rawValue) {
    const value = String(rawValue || '').trim().toLowerCase();
    if (Object.prototype.hasOwnProperty.call(AD_MODEL_PRESETS, value)) {
      return value;
    }
    return DEFAULT_AD_MODEL_TYPE;
  }

  function getAdModelPreset(rawValue) {
    const modelType = normalizeAdModelType(rawValue);
    return AD_MODEL_PRESETS[modelType] || AD_MODEL_PRESETS[DEFAULT_AD_MODEL_TYPE];
  }

  function normalizeFrameSeconds(rawValue) {
    const input = Array.isArray(rawValue) ? rawValue : [];
    const output = [];
    const seen = new Set();

    input.forEach((item) => {
      const parsed = Number.parseInt(String(item ?? '').trim(), 10);
      if (!Number.isFinite(parsed)) {
        return;
      }

      const second = Math.max(0, Math.min(30, parsed));
      if (seen.has(second)) {
        return;
      }

      seen.add(second);
      output.push(second);
    });

    return output.length > 0 ? output : DEFAULT_VIDEO_FRAME_SECONDS.slice(0);
  }

  function getModelFrameSeconds(rawValue) {
    return normalizeFrameSeconds(getAdModelPreset(rawValue).frameSeconds);
  }

  function getModelLabel(rawValue) {
    return getAdModelPreset(rawValue).label;
  }

  function normalizeDirName(value) {
    return String(value || '').trim().toLowerCase();
  }

  function buildIgnoredDirNameSet(rawNames, options = {}) {
    const values = new Set();
    const includeManagedDirs = Boolean(options.includeManagedDirs);
    const includeDefaultIgnored = options.includeDefaultIgnored !== false;

    if (!includeManagedDirs) {
      MANAGED_DIR_NAMES_LOWER.forEach((item) => values.add(item));
    }

    if (includeDefaultIgnored) {
      Array.from(DEFAULT_IGNORED_DIR_NAMES)
        .map((item) => normalizeDirName(item))
        .filter(Boolean)
        .forEach((item) => values.add(item));
    }

    if (Array.isArray(rawNames)) {
      rawNames
        .map((item) => normalizeDirName(item))
        .filter(Boolean)
        .forEach((item) => values.add(item));
    }

    return values;
  }

  function shouldSkipDirectory(entryName, ignoredDirNames) {
    if (!(ignoredDirNames instanceof Set)) {
      return false;
    }
    const normalized = normalizeDirName(entryName);
    if (!normalized) {
      return false;
    }
    return ignoredDirNames.has(normalized);
  }

  function loadHashCache() {
    if (hashCache) {
      return hashCache;
    }

    const cachePath = getHashCachePath();
    if (!fs.existsSync(cachePath)) {
      hashCache = {
        version: HASH_CACHE_VERSION,
        updatedAt: '',
        items: {}
      };
      return hashCache;
    }

    try {
      const parsed = JSON.parse(fs.readFileSync(cachePath, 'utf8'));
      hashCache = {
        version: HASH_CACHE_VERSION,
        updatedAt: String(parsed.updatedAt || ''),
        items: parsed && parsed.items && typeof parsed.items === 'object' ? parsed.items : {}
      };
    } catch {
      hashCache = {
        version: HASH_CACHE_VERSION,
        updatedAt: '',
        items: {}
      };
    }

    return hashCache;
  }

  function trimHashCache(cache) {
    if (!cache || !cache.items || typeof cache.items !== 'object') {
      return;
    }

    const entries = Object.entries(cache.items);
    if (entries.length <= HASH_CACHE_MAX_ITEMS) {
      return;
    }

    entries.sort((left, right) => {
      const leftTimestamp = Number(left[1] && left[1].updatedAtMs ? left[1].updatedAtMs : 0);
      const rightTimestamp = Number(right[1] && right[1].updatedAtMs ? right[1].updatedAtMs : 0);
      return leftTimestamp - rightTimestamp;
    });

    const removeCount = entries.length - HASH_CACHE_MAX_ITEMS;
    for (let index = 0; index < removeCount; index += 1) {
      delete cache.items[entries[index][0]];
    }
  }

  function saveHashCache(cache) {
    const nextCache = cache || loadHashCache();
    nextCache.updatedAt = new Date().toISOString();
    trimHashCache(nextCache);

    const cachePath = getHashCachePath();
    ensureParentDir(cachePath);
    fs.writeFileSync(cachePath, JSON.stringify(nextCache, null, 2), 'utf8');
    return nextCache;
  }

  function buildVideoHashCacheKey(videoPath, stat, frameSeconds = DEFAULT_VIDEO_FRAME_SECONDS) {
    const normalizedPath = path.resolve(String(videoPath || '')).toLowerCase();
    const size = Number(stat && Number.isFinite(stat.size) ? stat.size : 0);
    const mtimeMs = Number(stat && Number.isFinite(stat.mtimeMs) ? stat.mtimeMs : 0);
    const frameKey = normalizeFrameSeconds(frameSeconds).join(',');
    return `${normalizedPath}|${size}|${mtimeMs}|${frameKey}`;
  }

  function getCachedVideoHashes(cacheKey) {
    if (!cacheKey) {
      return [];
    }
    const cache = loadHashCache();
    const record = cache.items && cache.items[cacheKey];
    if (!record || !Array.isArray(record.hashes)) {
      return [];
    }

    return record.hashes
      .map((hash) => String(hash || '').trim())
      .filter(Boolean);
  }

  function setCachedVideoHashes(cacheKey, hashes) {
    if (!cacheKey || !Array.isArray(hashes) || hashes.length === 0) {
      return;
    }

    const cache = loadHashCache();
    cache.items[cacheKey] = {
      updatedAtMs: Date.now(),
      hashes: hashes
        .map((hash) => String(hash || '').trim())
        .filter(Boolean)
        .slice(0, 20)
    };
    saveHashCache(cache);
  }

  function normalizeFilmId(rawValue) {
    const compactValue = String(rawValue || '')
      .toUpperCase()
      .trim()
      .replace(/[_\s]+/g, '-')
      .replace(/-+/g, '-');
    // Samples must use the same canonical identity as organizer/crawler
    // records, otherwise TL-1 and TL-001 create separate learning entries.
    const match = compactValue.match(/^([A-Z]{2,12})-?(\d{1,8})([A-Z]*)$/);

    if (!match) {
      return compactValue;
    }

    const [, prefix, digits, suffix] = match;
    const parsedNumber = Number.parseInt(digits, 10);
    const normalizedDigits = Number.isFinite(parsedNumber)
      ? parsedNumber < 100
        ? String(parsedNumber).padStart(3, '0')
        : String(parsedNumber)
      : digits;
    return `${prefix}-${normalizedDigits}${suffix}`.replace(/-+/g, '-');
  }

  function normalizeCodeToken(code) {
    return normalizeFilmId(code).replace(/[^A-Z0-9]/g, '');
  }

  function normalizeCodeList(rawCodes) {
    const rawList = Array.isArray(rawCodes)
      ? rawCodes
      : String(rawCodes || '')
          .split(/[\r\n,，、;；\s]+/)
          .map((item) => item.trim())
          .filter(Boolean);

    return Array.from(
      new Set(
        rawList
          .map((item) => normalizeFilmId(item))
          .filter(Boolean)
      )
    );
  }

  function hashBitsToHex(bits) {
    if (!bits) {
      return '';
    }

    const paddedBits = bits.padEnd(Math.ceil(bits.length / 4) * 4, '0');
    let output = '';
    for (let index = 0; index < paddedBits.length; index += 4) {
      output += Number.parseInt(paddedBits.slice(index, index + 4), 2).toString(16);
    }
    return output;
  }

  function hammingDistance(leftBits, rightBits) {
    const left = String(leftBits || '');
    const right = String(rightBits || '');
    const maxLength = Math.max(left.length, right.length);
    if (maxLength === 0) {
      return Number.POSITIVE_INFINITY;
    }

    let diff = 0;
    for (let index = 0; index < maxLength; index += 1) {
      if ((left[index] || '0') !== (right[index] || '0')) {
        diff += 1;
      }
    }
    return diff;
  }

  function toPerceptualBits(rawBuffer) {
    const bytes = rawBuffer.length >= 64 ? rawBuffer.subarray(0, 64) : null;
    if (!bytes) {
      return '';
    }

    let total = 0;
    for (let index = 0; index < 64; index += 1) {
      total += bytes[index];
    }
    const average = total / 64;

    let bits = '';
    for (let index = 0; index < 64; index += 1) {
      bits += bytes[index] >= average ? '1' : '0';
    }
    return bits;
  }

  function emitProgress(onProgress, payload = {}) {
    if (typeof onProgress !== 'function') {
      return;
    }

    onProgress({
      ...payload,
      timestamp: nowIso()
    });
  }

  function shouldReportProgress(processed, total, step = 20) {
    if (total <= 0) {
      return true;
    }
    if (processed <= 1 || processed >= total) {
      return true;
    }
    return processed % Math.max(1, step) === 0;
  }

  function execFileBuffer(command, args, timeoutMs = 12000) {
    return new Promise((resolve, reject) => {
      execFile(
        command,
        args,
        {
          windowsHide: true,
          encoding: 'buffer',
          maxBuffer: 4 * 1024 * 1024,
          timeout: timeoutMs
        },
        (error, stdout, stderr) => {
          if (error) {
            reject(
              new Error(
                `ffmpeg执行失败: ${error.message}; ${Buffer.isBuffer(stderr) ? stderr.toString('utf8') : String(stderr || '')}`
              )
            );
            return;
          }

          resolve(Buffer.isBuffer(stdout) ? stdout : Buffer.from(stdout || ''));
        }
      );
    });
  }

  // FFmpeg path probing still carries historical candidates from the
  // Electron-era package layout plus current Wails/runtime layouts. Keep this
  // list centralized here so later cleanup can delete old release assumptions
  // deliberately instead of re-adding scattered path guesses elsewhere.
  function getBundledFfmpegCandidates() {
    const candidates = [];
    const userDataPath = typeof app.getPath === 'function' ? String(app.getPath('userData') || '') : '';
    if (userDataPath) {
      candidates.push(path.join(userDataPath, 'tools', 'ffmpeg', 'ffmpeg.exe'));
    }

    const resourcesPath = process && process.resourcesPath ? String(process.resourcesPath || '') : '';
    if (resourcesPath) {
      candidates.push(path.join(resourcesPath, BUNDLED_FFMPEG_RELATIVE_PATH));
      candidates.push(path.join(resourcesPath, 'tools', 'ffmpeg', 'ffmpeg.exe'));
    }

    const appPath = typeof app.getAppPath === 'function' ? String(app.getAppPath() || '') : '';
    if (appPath) {
      candidates.push(path.join(appPath, 'tools', 'ffmpeg', 'ffmpeg.exe'));
      candidates.push(path.join(appPath, 'desktop', 'resources', 'ffmpeg', 'win-x64', 'ffmpeg.exe'));
      candidates.push(path.join(appPath, 'resources', 'ffmpeg', 'win-x64', 'ffmpeg.exe'));
    }

    candidates.push(path.join(__dirname, '..', 'resources', 'ffmpeg', 'win-x64', 'ffmpeg.exe'));
    return Array.from(
      new Set(
        candidates
          .map((candidate) => path.resolve(String(candidate || '').trim()))
          .filter(Boolean)
      )
    );
  }

  async function probeFfmpegCommand(command) {
    try {
      await execFileBuffer(command, ['-version'], 5000);
      return true;
    } catch {
      return false;
    }
  }

  // This service is still part of the organizer compatibility layer. The
  // resolver prefers bundled FFmpeg first so sample import/learning can keep
  // working across old package layouts, then falls back to PATH for developer
  // or manually prepared environments.
  // 解析结果会缓存到进程级变量，避免每次导样本或算风险都重新探测 FFmpeg。
  async function resolveFfmpegCommand() {
    if (typeof ffmpegAvailable === 'boolean') {
      return ffmpegAvailable ? ffmpegCommand : '';
    }

    const bundledCandidates = getBundledFfmpegCandidates().filter((candidate) => fs.existsSync(candidate));
    for (const candidate of bundledCandidates) {
      if (await probeFfmpegCommand(candidate)) {
        ffmpegAvailable = true;
        ffmpegCommand = candidate;
        return ffmpegCommand;
      }
    }

    if (await probeFfmpegCommand('ffmpeg')) {
      ffmpegAvailable = true;
      ffmpegCommand = 'ffmpeg';
      return ffmpegCommand;
    }

    ffmpegAvailable = false;
    ffmpegCommand = '';
    return '';
  }

  async function detectFfmpegAvailable() {
    const resolved = await resolveFfmpegCommand();
    return Boolean(resolved);
  }

  async function computeImageHash(filePath) {
    const command = await resolveFfmpegCommand();
    if (!command) {
      throw new Error('未检测到 ffmpeg 可执行文件');
    }

    const raw = await execFileBuffer(command, [
      '-v',
      'error',
      '-i',
      filePath,
      '-frames:v',
      '1',
      '-vf',
      'scale=8:8,format=gray',
      '-f',
      'rawvideo',
      '-'
    ]);
    return toPerceptualBits(raw);
  }

  async function computeVideoFrameHash(filePath, second) {
    const command = await resolveFfmpegCommand();
    if (!command) {
      throw new Error('未检测到 ffmpeg 可执行文件');
    }

    const raw = await execFileBuffer(command, [
      '-v',
      'error',
      '-ss',
      String(second),
      '-i',
      filePath,
      '-frames:v',
      '1',
      '-vf',
      'scale=8:8,format=gray',
      '-f',
      'rawvideo',
      '-'
    ]);
    return toPerceptualBits(raw);
  }

  async function collectVideoFiles(rootPath, includeSubdirectories = true, options = {}) {
    const normalizedRoot = path.resolve(String(rootPath || '').trim());
    const files = [];
    const ignoredDirNames = buildIgnoredDirNameSet(options.ignoredDirNames, {
      includeManagedDirs: options.includeManagedDirs === true
    });

    async function walk(currentPath) {
      let entries = [];
      try {
        entries = await fs.promises.readdir(currentPath, { withFileTypes: true });
      } catch {
        return;
      }

      for (const entry of entries) {
        const entryPath = path.join(currentPath, entry.name);
        if (entry.isFile()) {
          if (VIDEO_EXTENSIONS.has(path.extname(entryPath).toLowerCase())) {
            files.push(entryPath);
          }
          continue;
        }

        if (!entry.isDirectory()) {
          continue;
        }

        if (shouldSkipDirectory(entry.name, ignoredDirNames)) {
          continue;
        }

        if (!includeSubdirectories) {
          continue;
        }

        await walk(entryPath);
      }
    }

    await walk(normalizedRoot);
    return files;
  }

  async function collectVideoFilesWithManagedFallback(sourceRoot, includeSubdirectories = true, options = {}) {
    // 当原始根目录已经只剩 organizer 管理目录时，学习/评估仍可回退到这些目录继续工作。
    // 这个 fallback 规则只保留在 adLearningService 内，避免 UI/IPC 层各自复制目录规则。
    const normalizedRoot = path.resolve(String(sourceRoot || '').trim());
    const ignoredDirNames = Array.isArray(options.ignoredDirNames) ? options.ignoredDirNames : [];
    const baseFiles = await collectVideoFiles(normalizedRoot, includeSubdirectories, {
      ignoredDirNames
    });

    if (baseFiles.length > 0) {
      return {
        videoFiles: baseFiles,
        scannedRoots: [normalizedRoot],
        usedManagedFallback: false
      };
    }

    const fallbackRoots = [
      path.join(normalizedRoot, '\u5f85\u6574\u7406'),
      path.join(normalizedRoot, '\u542b\u5f00\u5934\u5e7f\u544a'),
      path.join(normalizedRoot, '\u5f85\u5220\u9664')
    ];
    const existingFallbackRoots = [];
    for (const candidateRoot of fallbackRoots) {
      const stat = await fs.promises.stat(candidateRoot).catch(() => null);
      if (stat && stat.isDirectory()) {
        existingFallbackRoots.push(candidateRoot);
      }
    }

    if (existingFallbackRoots.length === 0) {
      return {
        videoFiles: baseFiles,
        scannedRoots: [normalizedRoot],
        usedManagedFallback: false
      };
    }

    const fallbackFiles = [];
    const seenFiles = new Set();
    for (const fallbackRoot of existingFallbackRoots) {
      // 姿态兜底：按番号学习时，如果根目录只剩待整理/含开头广告，也自动进入这些目录扫描
      const currentFiles = await collectVideoFiles(fallbackRoot, includeSubdirectories, {
        ignoredDirNames,
        includeManagedDirs: true
      });
      currentFiles.forEach((filePath) => {
        const dedupeKey = path.resolve(filePath).toLowerCase();
        if (seenFiles.has(dedupeKey)) {
          return;
        }
        seenFiles.add(dedupeKey);
        fallbackFiles.push(filePath);
      });
    }

    if (fallbackFiles.length === 0) {
      return {
        videoFiles: baseFiles,
        scannedRoots: [normalizedRoot],
        usedManagedFallback: false
      };
    }

    return {
      videoFiles: fallbackFiles,
      scannedRoots: existingFallbackRoots,
      usedManagedFallback: true
    };
  }

  function detectFilmCodeFromPath(filePath, tokenMap) {
    const fileName = path.basename(filePath, path.extname(filePath));
    const compact = String(fileName || '')
      .toUpperCase()
      .replace(/[^A-Z0-9]+/g, '');

    if (!compact) {
      return '';
    }

    for (const [token, code] of tokenMap.entries()) {
      if (token && compact.includes(token)) {
        return code;
      }
    }

    return '';
  }

  function loadModel() {
    const modelPath = getModelPath();
    if (!fs.existsSync(modelPath)) {
      return {
        ...DEFAULT_MODEL
      };
    }

    try {
      const parsed = JSON.parse(fs.readFileSync(modelPath, 'utf8'));
      const safeModel = {
        ...DEFAULT_MODEL,
        ...parsed,
        keywords: uniqueText(parsed.keywords || []),
        thresholds: {
          ...DEFAULT_MODEL.thresholds,
          ...(parsed.thresholds || {})
        },
        adSamples: Array.isArray(parsed.adSamples) ? parsed.adSamples : [],
        normalSamples: Array.isArray(parsed.normalSamples) ? parsed.normalSamples : [],
        introTemplates: Array.isArray(parsed.introTemplates) ? parsed.introTemplates : [],
        meta:
          parsed.meta && typeof parsed.meta === 'object'
            ? {
                ...DEFAULT_MODEL.meta,
                ...parsed.meta,
                activeModel: normalizeAdModelType(parsed.meta.activeModel)
              }
            : {
                ...DEFAULT_MODEL.meta
              },
        metrics:
          parsed.metrics && typeof parsed.metrics === 'object'
            ? {
                ...DEFAULT_MODEL.metrics,
                ...parsed.metrics
              }
            : {
                ...DEFAULT_MODEL.metrics
              }
      };
      return safeModel;
    } catch {
      return {
        ...DEFAULT_MODEL
      };
    }
  }

  function saveModel(model) {
    const modelPath = getModelPath();
    ensureParentDir(modelPath);
    const nextModel = {
      ...DEFAULT_MODEL,
      ...model,
      keywords: uniqueText(model.keywords || []),
      adSamples: Array.isArray(model.adSamples) ? model.adSamples : [],
      normalSamples: Array.isArray(model.normalSamples) ? model.normalSamples : [],
      introTemplates: Array.isArray(model.introTemplates) ? model.introTemplates : [],
      meta:
        model.meta && typeof model.meta === 'object'
          ? {
              ...DEFAULT_MODEL.meta,
              ...model.meta,
              activeModel: normalizeAdModelType(model.meta.activeModel)
            }
          : {
              ...DEFAULT_MODEL.meta
            },
      metrics:
        model.metrics && typeof model.metrics === 'object'
          ? {
              ...DEFAULT_MODEL.metrics,
              ...model.metrics
            }
          : {
              ...DEFAULT_MODEL.metrics
            },
      updatedAt: new Date().toISOString()
    };
    fs.writeFileSync(modelPath, JSON.stringify(nextModel, null, 2), 'utf8');
    return nextModel;
  }

  function summarizeModel(model) {
    const nextModel = ensureLearningModelShape(model || loadModel());
    const activeModel = normalizeAdModelType(nextModel && nextModel.meta ? nextModel.meta.activeModel : '');
    return {
      modelPath: getModelPath(),
      version: nextModel.version,
      updatedAt: nextModel.updatedAt || '',
      keywordCount: (nextModel.keywords || []).length,
      adSampleCount: (nextModel.adSamples || []).length,
      normalSampleCount: (nextModel.normalSamples || []).length,
      introTemplateCount: (nextModel.introTemplates || []).length,
      activeModel,
      activeModelLabel: getModelLabel(activeModel),
      thresholds: nextModel.thresholds,
      metrics:
        nextModel.metrics && typeof nextModel.metrics === 'object'
          ? nextModel.metrics
          : {
              ...DEFAULT_MODEL.metrics
            }
    };
  }

  async function updateModel(options = {}) {
    // 这里只负责“配置层”更新：
    // - 关键词
    // - 阈值
    // - 当前激活模型
    // 样本语料与风险评估都放在后续函数，避免配置问题和语料问题混在一起。
    const model = loadModel();
    const mergedKeywords = uniqueText([...(model.keywords || []), ...(options.keywords || [])]);
    model.keywords = mergedKeywords;
    model.meta = {
      ...(model.meta && typeof model.meta === 'object' ? model.meta : {}),
      activeModel: normalizeAdModelType(options.modelType || (model.meta && model.meta.activeModel))
    };
    model.thresholds = {
      ...model.thresholds,
      adScore: normalizeThreshold(options.adScore, model.thresholds.adScore || 60),
      highSimilarityDistance: normalizeThreshold(
        options.highSimilarityDistance,
        model.thresholds.highSimilarityDistance || 10
      ),
      mediumSimilarityDistance: normalizeThreshold(
        options.mediumSimilarityDistance,
        model.thresholds.mediumSimilarityDistance || 16
      ),
      lowSimilarityDistance: normalizeThreshold(
        options.lowSimilarityDistance,
        model.thresholds.lowSimilarityDistance || 22
      )
    };

    return summarizeModel(saveModel(model));
  }

  function buildSampleRecord({ id, label, sourcePath, sourceType, filmCode = '', frameSecond = null, hashBits }) {
    return {
      id,
      label,
      sourcePath,
      sourceType,
      filmCode: filmCode || '',
      frameSecond: Number.isFinite(Number(frameSecond)) ? Number(frameSecond) : null,
      hashBits,
      hashHex: hashBitsToHex(hashBits),
      addedAt: new Date().toISOString()
    };
  }

  function appendIntroTemplate(model, payload = {}) {
    if (!model || typeof model !== 'object') {
      return;
    }
    if (!Array.isArray(model.introTemplates)) {
      model.introTemplates = [];
    }

    const hashBits = String(payload.hashBits || '').trim();
    if (!hashBits) {
      return;
    }

    const exists = model.introTemplates.some((item) => String(item && item.hashBits ? item.hashBits : '') === hashBits);
    if (exists) {
      return;
    }

    const templateId = crypto
      .createHash('sha1')
      .update(`intro-${hashBits}-${payload.sourcePath || ''}-${Date.now()}-${model.introTemplates.length}`)
      .digest('hex')
      .slice(0, 12);

    model.introTemplates.push({
      id: templateId,
      hashBits,
      hashHex: hashBitsToHex(hashBits),
      sourcePath: String(payload.sourcePath || ''),
      frameSecond: Number.isFinite(Number(payload.frameSecond)) ? Number(payload.frameSecond) : null,
      filmCode: String(payload.filmCode || ''),
      addedAt: new Date().toISOString()
    });
  }

  function findBestSampleMatch(videoHashes, samples = []) {
    if (!Array.isArray(videoHashes) || videoHashes.length === 0 || !Array.isArray(samples) || samples.length === 0) {
      return null;
    }

    let best = null;
    samples.forEach((sample) => {
      const sampleHash = String(sample && sample.hashBits ? sample.hashBits : '').trim();
      if (!sampleHash) {
        return;
      }

      videoHashes.forEach((videoHash, index) => {
        const distance = hammingDistance(videoHash, sampleHash);
        if (!Number.isFinite(distance)) {
          return;
        }
        if (!best || distance < best.distance) {
          best = {
            distance,
            videoHashIndex: index,
            sampleId: String(sample.id || ''),
            sourcePath: String(sample.sourcePath || ''),
            filmCode: String(sample.filmCode || ''),
            frameSecond: Number.isFinite(Number(sample.frameSecond)) ? Number(sample.frameSecond) : null,
            hashBits: sampleHash,
            label: String(sample.label || '')
          };
        }
      });
    });

    return best;
  }

  // Compatibility-side sample import entry for organizer ad-learning. This is
  // not part of the Go crawl main path, but it remains a live execution path
  // for current video-organizer workflows until ad-learning is split further.
  async function importSamples(options = {}) {
    const label = options.label === 'normal' ? 'normal' : 'ad';
    const samplePaths = Array.isArray(options.samplePaths) ? options.samplePaths : [];
    const model = loadModel();
    const ffmpegReady = await detectFfmpegAvailable();
    if (!ffmpegReady) {
      throw new Error('未检测到 ffmpeg（内置与系统 PATH 均不可用），暂时无法导入样本。请更新到最新软件包或手动安装 ffmpeg 并加入 PATH。');
    }

    const targetList = label === 'ad' ? model.adSamples : model.normalSamples;
    const imported = [];
    const skipped = [];

    for (const rawPath of samplePaths) {
      const samplePath = String(rawPath || '').trim();
      if (!samplePath) {
        continue;
      }

      const ext = path.extname(samplePath).toLowerCase();
      const isImage = IMAGE_EXTENSIONS.has(ext);
      const isVideo = VIDEO_EXTENSIONS.has(ext);
      if (!isImage && !isVideo) {
        skipped.push({
          path: samplePath,
          reason: '仅支持图片或视频样本。'
        });
        continue;
      }

      try {
        const bits = isImage
          ? await computeImageHash(samplePath)
          : await computeVideoFrameHash(samplePath, 3);
        if (!bits) {
          throw new Error('样本哈希为空');
        }

        const id = crypto.createHash('sha1').update(`${samplePath}-${bits}-${Date.now()}`).digest('hex').slice(0, 12);
        targetList.push({
          id,
          label,
          sourcePath: samplePath,
          sourceType: isImage ? 'image' : 'video',
          hashBits: bits,
          hashHex: hashBitsToHex(bits),
          addedAt: new Date().toISOString()
        });
        imported.push(samplePath);
      } catch (error) {
        skipped.push({
          path: samplePath,
          reason: error instanceof Error ? error.message : String(error)
        });
      }
    }

    if (imported.length > 0) {
      saveModel(model);
    }

    return {
      summary: summarizeModel(model),
      imported,
      skipped
    };
  }

  // Compatibility-side organizer learning entry. It consumes local files and
  // organizer code selections; it should stay isolated from crawl runtime
  // state so later refactors can replace it without touching the crawler.
  async function learnSamplesByCodes(options = {}) {
    // Primary archived learning coordinator:
    // 1) normalize source root / code list / ignored directories
    // 2) walk local files and match requested codes
    // 3) extract representative samples
    // 4) persist learned model updates and metrics
    //
    // Keep transport/UI concerns out of this method; facade-level event mirroring
    // belongs in the archived compatibility facade layer.
    const label = options.label === 'normal' ? 'normal' : 'ad';
    const codes = normalizeCodeList(options.codes);
    const includeSubdirectories = options.includeSubdirectories !== false;
    const ignoredDirNames = Array.isArray(options.ignoredDirNames) ? options.ignoredDirNames : [];
    const rawRootPath = String(options.rootPath || '').trim();
    const sourceRoot = rawRootPath ? path.resolve(rawRootPath) : '';
    const onProgress = typeof options.onProgress === 'function' ? options.onProgress : null;

    if (!sourceRoot) {
      throw new Error('按番号学习时，来源目录不能为空。');
    }
    if (codes.length === 0) {
      throw new Error('请至少输入一个番号。');
    }

    emitProgress(onProgress, {
      scope: 'learning',
      phase: 'starting',
      label,
      sourceRoot,
      requestedCodeCount: codes.length
    });

    const rootStat = await fs.promises.stat(sourceRoot).catch(() => null);
    if (!rootStat || !rootStat.isDirectory()) {
      throw new Error(`学习来源目录不存在：${sourceRoot}`);
    }

    const ffmpegReady = await detectFfmpegAvailable();
    if (!ffmpegReady) {
      throw new Error('未检测到 ffmpeg（内置与系统 PATH 均不可用），无法按番号自动抓帧学习。请更新到最新软件包或手动安装 ffmpeg 并加入 PATH。');
    }

    const model = loadModel();
    const targetList = label === 'ad' ? model.adSamples : model.normalSamples;
    const existingHashes = new Set(
      targetList
        .map((item) => `${label}:${String(item.hashBits || '')}`)
        .filter((item) => item !== `${label}:`)
    );

    const tokenMap = new Map();
    codes.forEach((code) => {
      const token = normalizeCodeToken(code);
      if (!token) {
        return;
      }
      tokenMap.set(token, code);
    });

    const scanResult = await collectVideoFilesWithManagedFallback(sourceRoot, includeSubdirectories, {
      ignoredDirNames
    });
    const videoFiles = Array.isArray(scanResult.videoFiles) ? scanResult.videoFiles : [];
    const scannedRoots = Array.isArray(scanResult.scannedRoots) && scanResult.scannedRoots.length > 0
      ? scanResult.scannedRoots
      : [sourceRoot];
    const usedManagedFallback = Boolean(scanResult.usedManagedFallback);

    if (usedManagedFallback) {
      emitProgress(onProgress, {
        scope: 'learning',
        phase: 'managed-fallback',
        label,
        sourceRoot,
        scannedRoots,
        requestedCodeCount: codes.length
      });
    }
    const imported = [];
    const skipped = [];
    const matchedCodes = new Set();
    let matchedVideoCount = 0;

    emitProgress(onProgress, {
      scope: 'learning',
      phase: 'scan-ready',
      label,
      sourceRoot,
      scannedRoots,
      usedManagedFallback,
      requestedCodeCount: codes.length,
      totalVideos: videoFiles.length,
      processedVideos: 0,
      matchedVideoCount,
      importedSampleCount: imported.length
    });

    for (let videoIndex = 0; videoIndex < videoFiles.length; videoIndex += 1) {
      const videoPath = videoFiles[videoIndex];
      const processedVideos = videoIndex + 1;

      if (shouldReportProgress(processedVideos, videoFiles.length, 30)) {
        emitProgress(onProgress, {
          scope: 'learning',
          phase: 'matching',
          label,
          sourceRoot,
          scannedRoots,
          usedManagedFallback,
          requestedCodeCount: codes.length,
          totalVideos: videoFiles.length,
          processedVideos,
          matchedVideoCount,
          importedSampleCount: imported.length
        });
      }

      const matchedCode = detectFilmCodeFromPath(videoPath, tokenMap);
      if (!matchedCode) {
        continue;
      }

      matchedVideoCount += 1;
      matchedCodes.add(matchedCode);

      for (const second of DEFAULT_VIDEO_FRAME_SECONDS) {
        try {
          const bits = await computeVideoFrameHash(videoPath, second);
          if (!bits) {
            throw new Error('抓帧哈希为空');
          }

          const dedupeKey = `${label}:${bits}`;
          if (existingHashes.has(dedupeKey)) {
            skipped.push({
              path: videoPath,
              reason: `第 ${second}s 帧与已有${label === 'ad' ? '广告' : '正常'}样本重复`
            });
            continue;
          }

          const id = crypto
            .createHash('sha1')
            .update(`${videoPath}-${matchedCode}-${second}-${bits}-${Date.now()}-${targetList.length}`)
            .digest('hex')
            .slice(0, 12);

          targetList.push({
            id,
            label,
            sourcePath: videoPath,
            sourceType: 'video-frame',
            filmCode: matchedCode,
            frameSecond: second,
            hashBits: bits,
            hashHex: hashBitsToHex(bits),
            addedAt: new Date().toISOString()
          });
          existingHashes.add(dedupeKey);
          imported.push(`${videoPath} @${second}s`);
        } catch (error) {
          skipped.push({
            path: videoPath,
            reason: `第 ${second}s 抓帧失败：${error instanceof Error ? error.message : String(error)}`
          });
        }
      }

      emitProgress(onProgress, {
        scope: 'learning',
        phase: 'learning',
        label,
        sourceRoot,
        scannedRoots,
        usedManagedFallback,
        requestedCodeCount: codes.length,
        totalVideos: videoFiles.length,
        processedVideos,
        matchedVideoCount,
        importedSampleCount: imported.length,
        currentCode: matchedCode
      });
    }

    if (imported.length > 0) {
      saveModel(model);
    }

    const missingCodes = codes.filter((code) => !matchedCodes.has(code));
    emitProgress(onProgress, {
      scope: 'learning',
      phase: 'completed',
      label,
      sourceRoot,
      requestedCodeCount: codes.length,
      totalVideos: videoFiles.length,
      processedVideos: videoFiles.length,
      matchedVideoCount,
      importedSampleCount: imported.length,
      missingCodeCount: missingCodes.length
    });

    return {
      summary: summarizeModel(model),
      label,
      sourceRoot,
      requestedCodeCount: codes.length,
      matchedVideoCount,
      importedSampleCount: imported.length,
      matchedCodes: Array.from(matchedCodes).sort((a, b) => String(a).localeCompare(String(b), 'en')),
      missingCodes: missingCodes.sort((a, b) => String(a).localeCompare(String(b), 'en')),
      imported,
      skipped
    };
  }

  // Hash extraction is the lowest-level reusable primitive in this file. When
  // risk scores look implausible, verify frame-hash generation before checking
  // higher-level scoring heuristics.
  async function buildVideoHashes(videoPath, frameSeconds = DEFAULT_VIDEO_FRAME_SECONDS) {
    const ffmpegReady = await detectFfmpegAvailable();
    if (!ffmpegReady) {
      return {
        ffmpegAvailable: false,
        hashes: [],
        fromCache: false
      };
    }

    const stat = await fs.promises.stat(videoPath).catch(() => null);
    if (!stat || !stat.isFile()) {
      return {
        ffmpegAvailable: true,
        hashes: [],
        fromCache: false
      };
    }

    const normalizedFrameSeconds = normalizeFrameSeconds(frameSeconds);
    const cacheKey = buildVideoHashCacheKey(videoPath, stat, normalizedFrameSeconds);
    const cachedHashes = getCachedVideoHashes(cacheKey);
    if (cachedHashes.length > 0) {
      return {
        ffmpegAvailable: true,
        hashes: cachedHashes,
        fromCache: true
      };
    }

    const hashes = [];
    for (const second of normalizedFrameSeconds) {
      try {
        const bits = await computeVideoFrameHash(videoPath, second);
        if (bits) {
          hashes.push(bits);
        }
      } catch {
        // Ignore individual frame failures to keep the detector resilient.
      }
    }

    if (hashes.length > 0) {
      setCachedVideoHashes(cacheKey, hashes);
    }

    return {
      ffmpegAvailable: true,
      hashes,
      fromCache: false
    };
  }

  function findBestDistance(videoHashes, sampleHashes) {
    if (!Array.isArray(videoHashes) || videoHashes.length === 0 || !Array.isArray(sampleHashes) || sampleHashes.length === 0) {
      return Number.POSITIVE_INFINITY;
    }

    let best = Number.POSITIVE_INFINITY;
    for (const videoHash of videoHashes) {
      for (const sampleHash of sampleHashes) {
        const distance = hammingDistance(videoHash, sampleHash);
        if (distance < best) {
          best = distance;
        }
      }
    }
    return best;
  }

  // This is the older compatibility risk-evaluation path kept for historical
  // callers. The V2/V3 implementations below are the evolved layers that later
  // organizer compatibility flows migrated toward.
  async function evaluateVideoRisk(options = {}) {
    const videoPath = String(options.videoPath || '').trim();
    if (!videoPath) {
      throw new Error('视频路径不能为空。');
    }

    const model = loadModel();
    const threshold = normalizeThreshold(options.adThreshold, model.thresholds.adScore || 60);
    const filename = path.basename(videoPath).toLowerCase();
    const reasons = [];
    let score = 0;

    const keywordHits = (model.keywords || []).filter((keyword) => filename.includes(keyword));
    if (keywordHits.length > 0) {
      const keywordScore = Math.min(40, keywordHits.length * 15);
      score += keywordScore;
      reasons.push(`命中广告关键词: ${keywordHits.join(', ')}`);
    }

    if (URL_PATTERN.test(filename)) {
      score += 30;
      reasons.push('文件名疑似包含广告站点域名特征');
    }

    const { ffmpegAvailable, hashes, fromCache } = await buildVideoHashes(videoPath);
    const adHashes = (model.adSamples || []).map((item) => item.hashBits).filter(Boolean);
    const normalHashes = (model.normalSamples || []).map((item) => item.hashBits).filter(Boolean);

    const bestAdDistance = findBestDistance(hashes, adHashes);
    const bestNormalDistance = findBestDistance(hashes, normalHashes);

    if (Number.isFinite(bestAdDistance)) {
      if (bestAdDistance <= model.thresholds.highSimilarityDistance) {
        score += 65;
        reasons.push(`与广告样本高相似（距离 ${bestAdDistance}）`);
      } else if (bestAdDistance <= model.thresholds.mediumSimilarityDistance) {
        score += 45;
        reasons.push(`与广告样本中度相似（距离 ${bestAdDistance}）`);
      } else if (bestAdDistance <= model.thresholds.lowSimilarityDistance) {
        score += 20;
        reasons.push(`与广告样本低度相似（距离 ${bestAdDistance}）`);
      }
    }

    if (Number.isFinite(bestNormalDistance)) {
      if (bestNormalDistance <= model.thresholds.highSimilarityDistance) {
        score -= 50;
        reasons.push(`与正常样本高相似（距离 ${bestNormalDistance}）`);
      } else if (bestNormalDistance <= model.thresholds.mediumSimilarityDistance) {
        score -= 25;
        reasons.push(`与正常样本中度相似（距离 ${bestNormalDistance}）`);
      }
    }

    score = clamp(score, 0, 100);
    const isAd = score >= threshold;

    return {
      videoPath,
      ffmpegAvailable,
      hashesFromCache: Boolean(fromCache),
      score,
      threshold,
      isAd,
      reasons,
      bestAdDistance: Number.isFinite(bestAdDistance) ? bestAdDistance : null,
      bestNormalDistance: Number.isFinite(bestNormalDistance) ? bestNormalDistance : null,
      sampleCounts: {
        ad: adHashes.length,
        normal: normalHashes.length
      }
    };
  }

  // Compatibility note:
  // The V2/V3 functions below are evolutionary layers kept so archived desktop
  // organizer/ad-learning paths can continue to run without forcing a risky
  // rewrite. They are not a signal that new product work should branch further
  // inside this JS service. Prefer Go-side product changes first.
  function buildIntroTemplatesFromAdSamples(adSamples = []) {
    const templates = [];
    const seenHashes = new Set();
    const list = Array.isArray(adSamples) ? adSamples : [];

    for (let index = 0; index < list.length; index += 1) {
      const sample = list[index] || {};
      const hashBits = String(sample.hashBits || '').trim();
      if (!hashBits || seenHashes.has(hashBits)) {
        continue;
      }

      seenHashes.add(hashBits);
      const templateId = String(sample.id || '').trim() || `legacy-template-${index + 1}`;
      templates.push({
        id: templateId,
        hashBits,
        hashHex: hashBitsToHex(hashBits),
        sourcePath: String(sample.sourcePath || '').trim(),
        frameSecond: Number.isFinite(Number(sample.frameSecond)) ? Number(sample.frameSecond) : null,
        filmCode: normalizeFilmId(sample.filmCode || ''),
        addedAt: String(sample.addedAt || '').trim() || new Date().toISOString()
      });
    }

    return templates;
  }

  function ensureLearningModelShape(model) {
    const nextModel = model && typeof model === 'object' ? model : {};
    const adSamples = Array.isArray(nextModel.adSamples) ? nextModel.adSamples : [];
    const introTemplatesRaw = Array.isArray(nextModel.introTemplates) ? nextModel.introTemplates : [];
    const introTemplates =
      introTemplatesRaw.length > 0 ? introTemplatesRaw : buildIntroTemplatesFromAdSamples(adSamples);

    return {
      ...DEFAULT_MODEL,
      ...nextModel,
      keywords: uniqueText(nextModel.keywords || []),
      thresholds: {
        ...DEFAULT_MODEL.thresholds,
        ...(nextModel.thresholds || {})
      },
      adSamples,
      normalSamples: Array.isArray(nextModel.normalSamples) ? nextModel.normalSamples : [],
      introTemplates,
      meta:
        nextModel.meta && typeof nextModel.meta === 'object'
          ? {
              ...DEFAULT_MODEL.meta,
              ...nextModel.meta,
              activeModel: normalizeAdModelType(nextModel.meta.activeModel)
            }
          : {
              ...DEFAULT_MODEL.meta
            },
      metrics:
        nextModel.metrics && typeof nextModel.metrics === 'object'
          ? {
              ...DEFAULT_MODEL.metrics,
              ...nextModel.metrics
            }
          : {
              ...DEFAULT_MODEL.metrics
            }
    };
  }

  async function importSamplesV2(options = {}) {
    // V2 样本导入是当前兼容层主入口：
    // 1) 读取模型
    // 2) 确认 FFmpeg
    // 3) 计算/去重样本哈希
    // 4) 落盘新的样本库
    const label = options.label === 'normal' ? 'normal' : 'ad';
    const samplePaths = Array.isArray(options.samplePaths) ? options.samplePaths : [];
    const model = ensureLearningModelShape(loadModel());
    const activeModelType = normalizeAdModelType(options.modelType || (model.meta && model.meta.activeModel));
    const frameSeconds = getModelFrameSeconds(activeModelType);
    model.meta = {
      ...(model.meta && typeof model.meta === 'object' ? model.meta : {}),
      activeModel: activeModelType
    };
    const ffmpegReady = await detectFfmpegAvailable();
    if (!ffmpegReady) {
      throw new Error('未检测到 ffmpeg（内置与系统 PATH 均不可用），暂时无法导入样本。');
    }

    const targetList = label === 'ad' ? model.adSamples : model.normalSamples;
    const existingHashes = new Set(
      targetList
        .map((item) => `${label}:${String(item && item.hashBits ? item.hashBits : '')}`)
        .filter((item) => item !== `${label}:`)
    );

    const imported = [];
    const skipped = [];
    let sampleIncrement = 0;

    for (const rawPath of samplePaths) {
      const samplePath = String(rawPath || '').trim();
      if (!samplePath) {
        continue;
      }

      const ext = path.extname(samplePath).toLowerCase();
      const isImage = IMAGE_EXTENSIONS.has(ext);
      const isVideo = VIDEO_EXTENSIONS.has(ext);
      if (!isImage && !isVideo) {
        skipped.push({
          path: samplePath,
          reason: '仅支持图片或视频样本。'
        });
        continue;
      }

      try {
        const frameEntries = [];
        if (isImage) {
          const bits = await computeImageHash(samplePath);
          if (!bits) {
            throw new Error('样本哈希为空');
          }
          frameEntries.push({
            bits,
            frameSecond: null,
            sourceType: 'image'
          });
        } else {
          for (const second of frameSeconds) {
            try {
              const bits = await computeVideoFrameHash(samplePath, second);
              if (bits) {
                frameEntries.push({
                  bits,
                  frameSecond: second,
                  sourceType: 'video-frame'
                });
              }
            } catch {
              // Keep going when a single frame extraction fails.
            }
          }
          if (frameEntries.length === 0) {
            throw new Error('无法从视频抓取有效样本帧');
          }
        }

        frameEntries.forEach((entry) => {
          const dedupeKey = `${label}:${entry.bits}`;
          if (existingHashes.has(dedupeKey)) {
            skipped.push({
              path: samplePath,
              reason:
                entry.frameSecond === null
                  ? `样本重复（${label}）`
                  : `${entry.frameSecond}s 抓帧样本重复（${label}）`
            });
            return;
          }

          const id = crypto
            .createHash('sha1')
            .update(`${samplePath}-${entry.bits}-${entry.frameSecond}-${Date.now()}-${targetList.length}`)
            .digest('hex')
            .slice(0, 12);

          targetList.push(
            buildSampleRecord({
              id,
              label,
              sourcePath: samplePath,
              sourceType: entry.sourceType,
              frameSecond: entry.frameSecond,
              hashBits: entry.bits
            })
          );
          existingHashes.add(dedupeKey);
          sampleIncrement += 1;
          imported.push(entry.frameSecond === null ? samplePath : `${samplePath} @${entry.frameSecond}s`);

          if (label === 'ad') {
            appendIntroTemplate(model, {
              hashBits: entry.bits,
              sourcePath: samplePath,
              frameSecond: entry.frameSecond
            });
          }
        });
      } catch (error) {
        skipped.push({
          path: samplePath,
          reason: error instanceof Error ? error.message : String(error)
        });
      }
    }

    if (sampleIncrement > 0) {
      saveModel(model);
    }

    return {
      summary: summarizeModel(model),
      imported,
      skipped,
      sampleIncrement
    };
  }

  async function learnSamplesByCodesV2(options = {}) {
    // V2 keeps the same compatibility boundary as `learnSamplesByCodes`, but
    // uses the newer local-sample import path. Keep parity/fallback concerns
    // here rather than splitting state ownership between multiple callers.
    const label = options.label === 'normal' ? 'normal' : 'ad';
    const codes = normalizeCodeList(options.codes);
    const includeSubdirectories = options.includeSubdirectories !== false;
    const ignoredDirNames = Array.isArray(options.ignoredDirNames) ? options.ignoredDirNames : [];
    const rawRootPath = String(options.rootPath || '').trim();
    const sourceRoot = rawRootPath ? path.resolve(rawRootPath) : '';
    const onProgress = typeof options.onProgress === 'function' ? options.onProgress : null;

    if (!sourceRoot) {
      throw new Error('按番号学习时，来源目录不能为空。');
    }
    if (codes.length === 0) {
      throw new Error('请至少输入一个番号。');
    }

    emitProgress(onProgress, {
      scope: 'learning',
      phase: 'starting',
      label,
      sourceRoot,
      requestedCodeCount: codes.length
    });

    const rootStat = await fs.promises.stat(sourceRoot).catch(() => null);
    if (!rootStat || !rootStat.isDirectory()) {
      throw new Error(`学习来源目录不存在：${sourceRoot}`);
    }

    const ffmpegReady = await detectFfmpegAvailable();
    if (!ffmpegReady) {
      throw new Error('未检测到 ffmpeg（内置与系统 PATH 均不可用），无法按番号自动抓帧学习。');
    }

    const model = ensureLearningModelShape(loadModel());
    const activeModelType = normalizeAdModelType(options.modelType || (model.meta && model.meta.activeModel));
    const frameSeconds = getModelFrameSeconds(activeModelType);
    model.meta = {
      ...(model.meta && typeof model.meta === 'object' ? model.meta : {}),
      activeModel: activeModelType
    };
    const targetList = label === 'ad' ? model.adSamples : model.normalSamples;
    const oppositeList = label === 'ad' ? model.normalSamples : model.adSamples;

    const existingHashes = new Set(
      targetList
        .map((item) => `${label}:${String(item && item.hashBits ? item.hashBits : '')}`)
        .filter((item) => item !== `${label}:`)
    );

    const tokenMap = new Map();
    codes.forEach((code) => {
      const token = normalizeCodeToken(code);
      if (!token) {
        return;
      }
      tokenMap.set(token, code);
    });

    const scanResult = await collectVideoFilesWithManagedFallback(sourceRoot, includeSubdirectories, {
      ignoredDirNames
    });
    const videoFiles = Array.isArray(scanResult.videoFiles) ? scanResult.videoFiles : [];
    const scannedRoots =
      Array.isArray(scanResult.scannedRoots) && scanResult.scannedRoots.length > 0
        ? scanResult.scannedRoots
        : [sourceRoot];
    const usedManagedFallback = Boolean(scanResult.usedManagedFallback);

    if (usedManagedFallback) {
      emitProgress(onProgress, {
        scope: 'learning',
        phase: 'managed-fallback',
        label,
        sourceRoot,
        scannedRoots,
        requestedCodeCount: codes.length
      });
    }

    const imported = [];
    const skipped = [];
    const matchedCodes = new Set();
    let matchedVideoCount = 0;
    let potentialFalsePositiveCount = 0;

    emitProgress(onProgress, {
      scope: 'learning',
      phase: 'scan-ready',
      label,
      sourceRoot,
      scannedRoots,
      usedManagedFallback,
      requestedCodeCount: codes.length,
      totalVideos: videoFiles.length,
      processedVideos: 0,
      matchedVideoCount,
      importedSampleCount: imported.length
    });

    for (let videoIndex = 0; videoIndex < videoFiles.length; videoIndex += 1) {
      const videoPath = videoFiles[videoIndex];
      const processedVideos = videoIndex + 1;

      if (shouldReportProgress(processedVideos, videoFiles.length, 30)) {
        emitProgress(onProgress, {
          scope: 'learning',
          phase: 'matching',
          label,
          sourceRoot,
          scannedRoots,
          usedManagedFallback,
          requestedCodeCount: codes.length,
          totalVideos: videoFiles.length,
          processedVideos,
          matchedVideoCount,
          importedSampleCount: imported.length
        });
      }

      const matchedCode = detectFilmCodeFromPath(videoPath, tokenMap);
      if (!matchedCode) {
        continue;
      }

      matchedVideoCount += 1;
      matchedCodes.add(matchedCode);

      const videoFrameHashes = [];
      for (const second of frameSeconds) {
        try {
          const bits = await computeVideoFrameHash(videoPath, second);
          if (!bits) {
            throw new Error('抓帧哈希为空');
          }

          videoFrameHashes.push(bits);
          const dedupeKey = `${label}:${bits}`;
          if (existingHashes.has(dedupeKey)) {
            skipped.push({
              path: videoPath,
              reason: `${second}s 抓帧与已有${label === 'ad' ? '广告' : '正常'}样本重复`
            });
            continue;
          }

          const id = crypto
            .createHash('sha1')
            .update(`${videoPath}-${matchedCode}-${second}-${bits}-${Date.now()}-${targetList.length}`)
            .digest('hex')
            .slice(0, 12);

          targetList.push(
            buildSampleRecord({
              id,
              label,
              sourcePath: videoPath,
              sourceType: 'video-frame',
              filmCode: matchedCode,
              frameSecond: second,
              hashBits: bits
            })
          );
          existingHashes.add(dedupeKey);
          imported.push(`${videoPath} @${second}s`);

          if (label === 'ad') {
            appendIntroTemplate(model, {
              hashBits: bits,
              sourcePath: videoPath,
              frameSecond: second,
              filmCode: matchedCode
            });
          }
        } catch (error) {
          skipped.push({
            path: videoPath,
            reason: `${second}s 抓帧失败：${error instanceof Error ? error.message : String(error)}`
          });
        }
      }

      const oppositeMatch = findBestSampleMatch(videoFrameHashes, oppositeList);
      if (oppositeMatch && oppositeMatch.distance <= model.thresholds.highSimilarityDistance) {
        potentialFalsePositiveCount += 1;
      }

      emitProgress(onProgress, {
        scope: 'learning',
        phase: 'learning',
        label,
        sourceRoot,
        scannedRoots,
        usedManagedFallback,
        requestedCodeCount: codes.length,
        totalVideos: videoFiles.length,
        processedVideos,
        matchedVideoCount,
        importedSampleCount: imported.length,
        currentCode: matchedCode
      });
    }

    const missingCodes = codes.filter((code) => !matchedCodes.has(code));
    const hitRate = codes.length > 0 ? (matchedVideoCount / codes.length) * 100 : 0;
    const falsePositiveRate = matchedVideoCount > 0 ? (potentialFalsePositiveCount / matchedVideoCount) * 100 : 0;
    const sampleIncrement = imported.length;

    model.metrics = {
      ...(model.metrics && typeof model.metrics === 'object' ? model.metrics : {}),
      totalLearningRuns: Number((model.metrics && model.metrics.totalLearningRuns) || 0) + 1,
      lastLearning: {
        at: new Date().toISOString(),
        label,
        rootPath: sourceRoot,
        scannedRoots,
        usedManagedFallback,
        requestedCodeCount: codes.length,
        matchedVideoCount,
        missingCodeCount: missingCodes.length,
        sampleIncrement,
        hitRate,
        falsePositiveRate,
        potentialFalsePositiveCount
      }
    };

    saveModel(model);

    emitProgress(onProgress, {
      scope: 'learning',
      phase: 'completed',
      label,
      sourceRoot,
      scannedRoots,
      usedManagedFallback,
      requestedCodeCount: codes.length,
      totalVideos: videoFiles.length,
      processedVideos: videoFiles.length,
      matchedVideoCount,
      importedSampleCount: imported.length,
      missingCodeCount: missingCodes.length,
      hitRate,
      falsePositiveRate,
      sampleIncrement
    });

    return {
      summary: summarizeModel(model),
      label,
      sourceRoot,
      scannedRoots,
      usedManagedFallback,
      requestedCodeCount: codes.length,
      matchedVideoCount,
      importedSampleCount: imported.length,
      matchedCodes: Array.from(matchedCodes).sort((a, b) => String(a).localeCompare(String(b), 'en')),
      missingCodes: missingCodes.sort((a, b) => String(a).localeCompare(String(b), 'en')),
      imported,
      skipped,
      hitRate,
      falsePositiveRate,
      sampleIncrement,
      observability: {
        hitRate,
        falsePositiveRate,
        sampleIncrement,
        potentialFalsePositiveCount
      }
    };
  }

  async function evaluateVideoRiskV2Legacy(options = {}) {
    // Legacy 评分保留给旧兼容路径，核心还是文件名/模板/简单样本得分。
    // 新判断异常时，优先确认调用方是不是已经应当走 V3。
    const videoPath = String(options.videoPath || '').trim();
    if (!videoPath) {
      throw new Error('videoPath 不能为空。');
    }

    const model = ensureLearningModelShape(loadModel());
    const threshold = normalizeThreshold(options.adThreshold, model.thresholds.adScore || 60);
    const filename = path.basename(videoPath).toLowerCase();
    const reasons = [];
    let score = 0;

    const keywordHits = (model.keywords || []).filter((keyword) => filename.includes(keyword));
    if (keywordHits.length > 0) {
      const keywordScore = Math.min(40, keywordHits.length * 15);
      score += keywordScore;
      reasons.push(`命中广告关键词: ${keywordHits.join(', ')}`);
    }

    if (URL_PATTERN.test(filename)) {
      score += 30;
      reasons.push('文件名疑似包含广告站点域名特征');
    }

    const { ffmpegAvailable, hashes, fromCache } = await buildVideoHashes(videoPath);
    const adMatch = findBestSampleMatch(hashes, model.adSamples || []);
    const normalMatch = findBestSampleMatch(hashes, model.normalSamples || []);
    const templateMatch = findBestSampleMatch(
      hashes,
      (model.introTemplates || []).map((item) => ({
        ...item,
        label: 'intro-template'
      }))
    );

    if (templateMatch) {
      if (templateMatch.distance <= model.thresholds.highSimilarityDistance) {
        score += 55;
        reasons.push(`命中片头模板（高相似，距离 ${templateMatch.distance}）`);
      } else if (templateMatch.distance <= model.thresholds.mediumSimilarityDistance) {
        score += 35;
        reasons.push(`命中片头模板（中相似，距离 ${templateMatch.distance}）`);
      } else if (templateMatch.distance <= model.thresholds.lowSimilarityDistance) {
        score += 15;
        reasons.push(`命中片头模板（低相似，距离 ${templateMatch.distance}）`);
      }
    }

    if (adMatch) {
      if (adMatch.distance <= model.thresholds.highSimilarityDistance) {
        score += 45;
        reasons.push(`与广告样本高相似（距离 ${adMatch.distance}）`);
      } else if (adMatch.distance <= model.thresholds.mediumSimilarityDistance) {
        score += 30;
        reasons.push(`与广告样本中度相似（距离 ${adMatch.distance}）`);
      } else if (adMatch.distance <= model.thresholds.lowSimilarityDistance) {
        score += 12;
        reasons.push(`与广告样本低度相似（距离 ${adMatch.distance}）`);
      }
    }

    if (normalMatch) {
      if (normalMatch.distance <= model.thresholds.highSimilarityDistance) {
        score -= 45;
        reasons.push(`与正常样本高相似（距离 ${normalMatch.distance}）`);
      } else if (normalMatch.distance <= model.thresholds.mediumSimilarityDistance) {
        score -= 25;
        reasons.push(`与正常样本中度相似（距离 ${normalMatch.distance}）`);
      }
    }

    score = clamp(score, 0, 100);
    const isAd = score >= threshold;

    return {
      videoPath,
      ffmpegAvailable,
      hashesFromCache: Boolean(fromCache),
      score,
      threshold,
      isAd,
      reasons,
      bestAdDistance: adMatch ? adMatch.distance : null,
      bestNormalDistance: normalMatch ? normalMatch.distance : null,
      sampleCounts: {
        ad: (model.adSamples || []).length,
        normal: (model.normalSamples || []).length,
        introTemplates: (model.introTemplates || []).length
      },
      evidence: {
        frameHashes: (hashes || []).map((hashBits, index) => ({
          index,
          frameSecond: DEFAULT_VIDEO_FRAME_SECONDS[index] || null,
          hashHex: hashBitsToHex(hashBits)
        })),
        keywordHits,
        bestTemplateMatch: templateMatch
          ? {
              templateId: templateMatch.sampleId || '',
              distance: templateMatch.distance,
              sourcePath: templateMatch.sourcePath || '',
              frameSecond: templateMatch.frameSecond
            }
          : null,
        bestAdSampleMatch: adMatch
          ? {
              sampleId: adMatch.sampleId || '',
              distance: adMatch.distance,
              sourcePath: adMatch.sourcePath || '',
              filmCode: adMatch.filmCode || '',
              frameSecond: adMatch.frameSecond
            }
          : null,
        bestNormalSampleMatch: normalMatch
          ? {
              sampleId: normalMatch.sampleId || '',
              distance: normalMatch.distance,
              sourcePath: normalMatch.sourcePath || '',
              filmCode: normalMatch.filmCode || '',
              frameSecond: normalMatch.frameSecond
            }
          : null,
        cacheInfo: {
          fromCache: Boolean(fromCache)
        }
      }
    };
  }

  async function evaluateVideoRiskV2(options = {}) {
    const videoPath = String(options.videoPath || '').trim();
    if (!videoPath) {
      throw new Error('videoPath 不能为空。');
    }

    const model = ensureLearningModelShape(loadModel());
    const modelType = normalizeAdModelType(options.modelType || (model.meta && model.meta.activeModel));
    const modelPreset = getAdModelPreset(modelType);
    const frameSeconds = getModelFrameSeconds(modelType);
    const threshold = normalizeThreshold(options.adThreshold, model.thresholds.adScore || 60);
    const filename = path.basename(videoPath).toLowerCase();
    const reasons = [];
    let score = 0;

    const keywordHits = (model.keywords || []).filter((keyword) => filename.includes(keyword));
    if (keywordHits.length > 0) {
      const keywordScore = Math.min(modelPreset.keywordScoreMax, keywordHits.length * modelPreset.keywordScorePerHit);
      score += keywordScore;
      reasons.push(`命中广告关键词：${keywordHits.join(', ')}`);
    }

    if (URL_PATTERN.test(filename)) {
      score += modelPreset.domainPatternScore;
      reasons.push('文件名疑似包含广告站点域名特征');
    }

    const { ffmpegAvailable, hashes, fromCache } = await buildVideoHashes(videoPath, frameSeconds);
    const adMatch = findBestSampleMatch(hashes, model.adSamples || []);
    const normalMatch = findBestSampleMatch(hashes, model.normalSamples || []);
    const templateMatch = findBestSampleMatch(
      hashes,
      (model.introTemplates || []).map((item) => ({
        ...item,
        label: 'intro-template'
      }))
    );

    if (templateMatch) {
      if (templateMatch.distance <= model.thresholds.highSimilarityDistance) {
        score += modelPreset.templateScore.high;
        reasons.push(`命中片头模板（高相似，距离 ${templateMatch.distance}）`);
      } else if (templateMatch.distance <= model.thresholds.mediumSimilarityDistance) {
        score += modelPreset.templateScore.medium;
        reasons.push(`命中片头模板（中相似，距离 ${templateMatch.distance}）`);
      } else if (templateMatch.distance <= model.thresholds.lowSimilarityDistance) {
        score += modelPreset.templateScore.low;
        reasons.push(`命中片头模板（低相似，距离 ${templateMatch.distance}）`);
      }
    }

    if (adMatch) {
      if (adMatch.distance <= model.thresholds.highSimilarityDistance) {
        score += modelPreset.adSampleScore.high;
        reasons.push(`与广告样本高相似（距离 ${adMatch.distance}）`);
      } else if (adMatch.distance <= model.thresholds.mediumSimilarityDistance) {
        score += modelPreset.adSampleScore.medium;
        reasons.push(`与广告样本中相似（距离 ${adMatch.distance}）`);
      } else if (adMatch.distance <= model.thresholds.lowSimilarityDistance) {
        score += modelPreset.adSampleScore.low;
        reasons.push(`与广告样本低相似（距离 ${adMatch.distance}）`);
      }
    }

    if (normalMatch) {
      if (normalMatch.distance <= model.thresholds.highSimilarityDistance) {
        score -= modelPreset.normalSamplePenalty.high;
        reasons.push(`与正常样本高相似（距离 ${normalMatch.distance}）`);
      } else if (normalMatch.distance <= model.thresholds.mediumSimilarityDistance) {
        score -= modelPreset.normalSamplePenalty.medium;
        reasons.push(`与正常样本中相似（距离 ${normalMatch.distance}）`);
      }
    }

    score = clamp(score, 0, 100);
    const isAd = score >= threshold;
    reasons.unshift(`模型策略：${modelPreset.label}`);

    return {
      videoPath,
      modelType,
      modelLabel: modelPreset.label,
      ffmpegAvailable,
      hashesFromCache: Boolean(fromCache),
      score,
      threshold,
      isAd,
      reasons,
      bestAdDistance: adMatch ? adMatch.distance : null,
      bestNormalDistance: normalMatch ? normalMatch.distance : null,
      sampleCounts: {
        ad: (model.adSamples || []).length,
        normal: (model.normalSamples || []).length,
        introTemplates: (model.introTemplates || []).length
      },
      evidence: {
        frameHashes: (hashes || []).map((hashBits, index) => ({
          index,
          frameSecond: frameSeconds[index] || null,
          hashHex: hashBitsToHex(hashBits)
        })),
        keywordHits,
        model: {
          modelType,
          modelLabel: modelPreset.label,
          frameSeconds
        },
        bestTemplateMatch: templateMatch
          ? {
              templateId: templateMatch.sampleId || '',
              distance: templateMatch.distance,
              sourcePath: templateMatch.sourcePath || '',
              frameSecond: templateMatch.frameSecond
            }
          : null,
        bestAdSampleMatch: adMatch
          ? {
              sampleId: adMatch.sampleId || '',
              distance: adMatch.distance,
              sourcePath: adMatch.sourcePath || '',
              filmCode: adMatch.filmCode || '',
              frameSecond: adMatch.frameSecond
            }
          : null,
        bestNormalSampleMatch: normalMatch
          ? {
              sampleId: normalMatch.sampleId || '',
              distance: normalMatch.distance,
              sourcePath: normalMatch.sourcePath || '',
              filmCode: normalMatch.filmCode || '',
              frameSecond: normalMatch.frameSecond
            }
          : null,
        cacheInfo: {
          fromCache: Boolean(fromCache)
        }
      }
    };
  }

  async function evaluateVideoRiskV3(options = {}) {
    // V3 是当前兼容层默认评估入口：
    // 1) 文件名/域名启发式
    // 2) 开头广告模板相似度
    // 3) 广告/正常样本距离对比
    // 4) FFmpeg 帧哈希缓存
    //
    // 当广告判断不符合预期时，先查这里返回的 reasons / bestMatch 载荷。
    const videoPath = String(options.videoPath || '').trim();
    if (!videoPath) {
      throw new Error('videoPath 不能为空。');
    }

    function pickMatchPayload(match, includeFilmCode = false) {
      if (!match) {
        return null;
      }
      const payload = {
        sampleId: match.sampleId || '',
        distance: match.distance,
        sourcePath: match.sourcePath || '',
        frameSecond: match.frameSecond
      };
      if (includeFilmCode) {
        payload.filmCode = match.filmCode || '';
      }
      return payload;
    }

    const model = ensureLearningModelShape(loadModel());
    const modelType = normalizeAdModelType(options.modelType || (model.meta && model.meta.activeModel));
    const modelPreset = getAdModelPreset(modelType);
    const frameSeconds = getModelFrameSeconds(modelType);
    const threshold = normalizeThreshold(options.adThreshold, model.thresholds.adScore || 60);
    const filename = path.basename(videoPath).toLowerCase();
    const reasons = [];
    let score = 0;

    const keywordHits = (model.keywords || []).filter((keyword) => filename.includes(keyword));
    const domainPatternHit = URL_PATTERN.test(filename);

    if (keywordHits.length > 0) {
      const keywordScore = Math.min(modelPreset.keywordScoreMax, keywordHits.length * modelPreset.keywordScorePerHit);
      score += keywordScore;
      reasons.push(`命中广告关键词：${keywordHits.join(', ')}`);
    }

    if (domainPatternHit) {
      score += modelPreset.domainPatternScore;
      reasons.push('文件名疑似包含广告站点域名特征');
    }

    const { ffmpegAvailable, hashes, fromCache } = await buildVideoHashes(videoPath, frameSeconds);
    const adMatch = findBestSampleMatch(hashes, model.adSamples || []);
    const normalMatch = findBestSampleMatch(hashes, model.normalSamples || []);
    const templateMatch = findBestSampleMatch(
      hashes,
      (model.introTemplates || []).map((item) => ({
        ...item,
        label: 'intro-template'
      }))
    );

    const coarseByTemplate = templateMatch && templateMatch.distance <= model.thresholds.lowSimilarityDistance ? templateMatch : null;
    const coarseByAdSample = adMatch && adMatch.distance <= model.thresholds.mediumSimilarityDistance ? adMatch : null;
    const coarseByKeyword = keywordHits.length > 0 || domainPatternHit;
    const coarsePassed = Boolean(coarseByTemplate || coarseByAdSample || coarseByKeyword);

    if (!coarsePassed) {
      reasons.unshift(`模型策略：${modelPreset.label}`);
      reasons.push('FFmpeg粗筛未命中广告特征，跳过AI精筛。');
      return {
        videoPath,
        modelType,
        modelLabel: modelPreset.label,
        ffmpegAvailable,
        hashesFromCache: Boolean(fromCache),
        score: 0,
        threshold,
        isAd: false,
        reasons,
        bestAdDistance: adMatch ? adMatch.distance : null,
        bestNormalDistance: normalMatch ? normalMatch.distance : null,
        sampleCounts: {
          ad: (model.adSamples || []).length,
          normal: (model.normalSamples || []).length,
          introTemplates: (model.introTemplates || []).length
        },
        evidence: {
          frameHashes: (hashes || []).map((hashBits, index) => ({
            index,
            frameSecond: frameSeconds[index] || null,
            hashHex: hashBitsToHex(hashBits)
          })),
          keywordHits,
          model: {
            modelType,
            modelLabel: modelPreset.label,
            frameSeconds
          },
          coarseStage: {
            passed: false,
            byTemplate: null,
            byAdSample: null,
            byKeyword: coarseByKeyword
          },
          bestTemplateMatch: templateMatch
            ? {
                templateId: templateMatch.sampleId || '',
                distance: templateMatch.distance,
                sourcePath: templateMatch.sourcePath || '',
                frameSecond: templateMatch.frameSecond
              }
            : null,
          bestAdSampleMatch: pickMatchPayload(adMatch, true),
          bestNormalSampleMatch: pickMatchPayload(normalMatch, true),
          cacheInfo: {
            fromCache: Boolean(fromCache)
          }
        }
      };
    }

    if (coarseByTemplate) {
      if (coarseByTemplate.distance <= model.thresholds.highSimilarityDistance) {
        score += 20;
        reasons.push(`FFmpeg粗筛命中片头模板（高相似，距离 ${coarseByTemplate.distance}）`);
      } else {
        score += 12;
        reasons.push(`FFmpeg粗筛命中片头模板（中低相似，距离 ${coarseByTemplate.distance}）`);
      }
    }

    if (coarseByAdSample) {
      if (coarseByAdSample.distance <= model.thresholds.highSimilarityDistance) {
        score += 20;
        reasons.push(`FFmpeg粗筛命中广告样本（高相似，距离 ${coarseByAdSample.distance}）`);
      } else {
        score += 10;
        reasons.push(`FFmpeg粗筛命中广告样本（中相似，距离 ${coarseByAdSample.distance}）`);
      }
    }

    if (templateMatch) {
      if (templateMatch.distance <= model.thresholds.highSimilarityDistance) {
        score += modelPreset.templateScore.high;
        reasons.push(`命中片头模板（高相似，距离 ${templateMatch.distance}）`);
      } else if (templateMatch.distance <= model.thresholds.mediumSimilarityDistance) {
        score += modelPreset.templateScore.medium;
        reasons.push(`命中片头模板（中相似，距离 ${templateMatch.distance}）`);
      } else if (templateMatch.distance <= model.thresholds.lowSimilarityDistance) {
        score += modelPreset.templateScore.low;
        reasons.push(`命中片头模板（低相似，距离 ${templateMatch.distance}）`);
      }
    }

    if (adMatch) {
      if (adMatch.distance <= model.thresholds.highSimilarityDistance) {
        score += modelPreset.adSampleScore.high;
        reasons.push(`与广告样本高相似（距离 ${adMatch.distance}）`);
      } else if (adMatch.distance <= model.thresholds.mediumSimilarityDistance) {
        score += modelPreset.adSampleScore.medium;
        reasons.push(`与广告样本中相似（距离 ${adMatch.distance}）`);
      } else if (adMatch.distance <= model.thresholds.lowSimilarityDistance) {
        score += modelPreset.adSampleScore.low;
        reasons.push(`与广告样本低相似（距离 ${adMatch.distance}）`);
      }
    }

    if (normalMatch) {
      if (normalMatch.distance <= model.thresholds.highSimilarityDistance) {
        score -= modelPreset.normalSamplePenalty.high;
        reasons.push(`与正常样本高相似（距离 ${normalMatch.distance}）`);
      } else if (normalMatch.distance <= model.thresholds.mediumSimilarityDistance) {
        score -= modelPreset.normalSamplePenalty.medium;
        reasons.push(`与正常样本中相似（距离 ${normalMatch.distance}）`);
      }
    }

    score = clamp(score, 0, 100);
    const isAd = score >= threshold;
    reasons.unshift(`模型策略：${modelPreset.label}`);

    return {
      videoPath,
      modelType,
      modelLabel: modelPreset.label,
      ffmpegAvailable,
      hashesFromCache: Boolean(fromCache),
      score,
      threshold,
      isAd,
      reasons,
      bestAdDistance: adMatch ? adMatch.distance : null,
      bestNormalDistance: normalMatch ? normalMatch.distance : null,
      sampleCounts: {
        ad: (model.adSamples || []).length,
        normal: (model.normalSamples || []).length,
        introTemplates: (model.introTemplates || []).length
      },
      evidence: {
        frameHashes: (hashes || []).map((hashBits, index) => ({
          index,
          frameSecond: frameSeconds[index] || null,
          hashHex: hashBitsToHex(hashBits)
        })),
        keywordHits,
        model: {
          modelType,
          modelLabel: modelPreset.label,
          frameSeconds
        },
        coarseStage: {
          passed: true,
          byTemplate: coarseByTemplate
            ? {
                distance: coarseByTemplate.distance,
                sourcePath: coarseByTemplate.sourcePath || '',
                frameSecond: coarseByTemplate.frameSecond
              }
            : null,
          byAdSample: coarseByAdSample
            ? {
                distance: coarseByAdSample.distance,
                sourcePath: coarseByAdSample.sourcePath || '',
                filmCode: coarseByAdSample.filmCode || '',
                frameSecond: coarseByAdSample.frameSecond
              }
            : null,
          byKeyword: coarseByKeyword
        },
        bestTemplateMatch: templateMatch
          ? {
              templateId: templateMatch.sampleId || '',
              distance: templateMatch.distance,
              sourcePath: templateMatch.sourcePath || '',
              frameSecond: templateMatch.frameSecond
            }
          : null,
        bestAdSampleMatch: pickMatchPayload(adMatch, true),
        bestNormalSampleMatch: pickMatchPayload(normalMatch, true),
        cacheInfo: {
          fromCache: Boolean(fromCache)
        }
      }
    };
  }

  return {
    getModelPath,
    loadModel: () => loadModel(),
    getSummary: () => summarizeModel(loadModel()),
    updateModel,
    importSamples: importSamplesV2,
    learnSamplesByCodes: learnSamplesByCodesV2,
    evaluateVideoRisk: evaluateVideoRiskV3
  };
}

module.exports = {
  createAdLearningService
};
