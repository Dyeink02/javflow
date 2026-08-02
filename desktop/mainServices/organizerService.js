// Legacy organizer implementation reused by the Node sidecar facade.
// deprecated: archived JS organizer compatibility only; marker=archived-mainservices-organizer-service
// This file is still active for compatibility flows even though the Wails app
// owns the main desktop shell. Keep organizer logic changes local and avoid
// coupling this module to new Go-side orchestration details.
//
// Current maintenance rule:
// - do not add new organizer product logic here
// - do not treat this file as the current desktop main path
// - organizer feature work should go to the Go organizer/bridge path first
// - if the active Wails app needs organizer behavior changes, update the Go
//   path first and only mirror here if archived compatibility truly requires it
//
// This file remains only as a historical compatibility asset until old
// Electron/sidecar lanes are fully retired.
//
// Ownership summary:
// 1) run the archived JS organizer scan/judge/rename/report/cleanup workflow
// 2) keep legacy organizer report/file-shape compatibility stable
// 3) expose one compatibility service boundary for sidecar/Electron-era callers
//
// File map for maintainers:
// 1) normalization/constants and identifier parsing
// 2) crawl artifact import and expected-code shaping
// 3) workflow assembly and phase orchestration handoff
// 4) output/report helpers and compatibility report naming

const { pipeline } = require('stream/promises');
const progressSchema = require('../common/progressSchema.js');
const { runScanPhase } = require('./organizerModules/scanPhase.js');
const { runJudgePhase } = require('./organizerModules/judgePhase.js');
const { runRenamePhase } = require('./organizerModules/renamePhase.js');
const { runIntroAdPhase } = require('./organizerModules/introAdPhase.js');
const { runReportPhase } = require('./organizerModules/reportPhase.js');
const { runCleanupPhase } = require('./organizerModules/cleanupPhase.js');

function createOrganizerService({ fs, path }) {
  // Troubleshooting rule:
  // - phase behavior issues should usually start in `organizerModules/*Phase.js`
  // - artifact preload issues should start in this file's import helpers
  // - current Wails organizer issues should be classified first: Go path vs
  //   archived JS compatibility path

  const DEFAULT_VIDEO_EXTENSIONS = Object.freeze(['.mp4', '.mkv', '.avi', '.mov', '.flv', '.wmv', '.ts', '.m4v', '.iso']);
  const DEFAULT_VIDEO_EXTENSION_SET = new Set(DEFAULT_VIDEO_EXTENSIONS);
  const nowIso = () => new Date().toISOString();
  const MANAGED_TOP_DIRS = new Set([
    '\u5f85\u6574\u7406',
    '\u5f85\u5220\u9664',
    '\u542b\u5f00\u5934\u5e7f\u544a',
    'logs',
    '.video-organizer-state'
  ]);
  const MANAGED_TOP_DIRS_LOWER = new Set(Array.from(MANAGED_TOP_DIRS).map((item) => String(item || '').trim().toLowerCase()));
  const NORMALIZED_REPORT_FILES = {
    renameMap: '\u66f4\u6539\u524d\u540e\u5bf9\u7167.txt',
    unmatched: '\u672a\u8bc6\u522b\u756a\u53f7\u89c6\u9891.txt',
    adRiskCodes: '\u542b\u5f00\u5934\u5e7f\u544a\u756a\u53f7.txt',
    adRiskDetail: '\u542b\u5f00\u5934\u5e7f\u544a\u660e\u7ec6.txt',
    adRiskMagnets: '\u542b\u5f00\u5934\u5e7f\u544a\u8865\u6293\u78c1\u529b.txt',
    missingMagnets: '\u9057\u6f0f\u756a\u53f7\u78c1\u529b\u8865\u6293.txt'
  };
  const LEGACY_REPORT_FILE_NAMES = Object.freeze([
    '\u5220\u9664\u6e05\u5355.txt',
    '\u5e7f\u544a\u9ad8\u98ce\u9669\u756a\u53f7.txt',
    '\u5e7f\u544a\u98ce\u9669\u5206\u7ea7\u660e\u7ec6.txt',
    '\u5e7f\u544a\u9ad8\u98ce\u9669\u78c1\u529b\u8865\u6293.txt'
  ]);
  const CRAWL_FILM_DATA_FILE = 'filmData.json';
  const CRAWL_ORGANIZER_CODES_FILE = 'organizer-codes.json';
  const PREFIX_BLACKLIST = new Set([
    'H264',
    'H265',
    'X264',
    'X265',
    'HEVC',
    'AAC',
    'DTS',
    'WEB',
    'WEBRIP',
    'WEBDL',
    'BLURAY',
    'UHD',
    'FHD',
    'HD',
    'SD',
    'MP4',
    'MKV',
    'TS',
    'AVI',
    'MOV',
    'M4V'
  ]);

  function emitLog(onLog, level, message) {
    if (typeof onLog !== 'function') {
      return;
    }

    onLog({
      level,
      message: String(message || ''),
      timestamp: nowIso()
    });
  }

  function emitProgress(onProgress, payload = {}) {
    if (typeof onProgress !== 'function') {
      return;
    }

    onProgress(progressSchema.createProgress(payload.scope, payload.phase, payload));
  }

  function shouldReportProgress(processed, total, step = 25) {
    if (total <= 0) {
      return true;
    }
    if (processed <= 1 || processed >= total) {
      return true;
    }
    return processed % Math.max(1, step) === 0;
  }

  function isManagedDirectoryName(value) {
    const raw = String(value || '').trim();
    if (!raw) {
      return false;
    }
    return MANAGED_TOP_DIRS.has(raw) || MANAGED_TOP_DIRS_LOWER.has(raw.toLowerCase());
  }

  function toSafeInteger(value, fallback, minimum = 0) {
    const parsed = Number.parseInt(String(value ?? '').trim(), 10);
    if (!Number.isFinite(parsed)) {
      return Math.max(minimum, fallback);
    }
    return Math.max(minimum, parsed);
  }

  function normalizeAdFileAction(rawValue) {
    return String(rawValue || '').trim() === 'delete-directly' ? 'delete-directly' : 'move-to-delete';
  }

  function getAdFileActionLabel(action) {
    return action === 'delete-directly' ? 'Delete ad files directly' : 'Move to waiting-delete';
  }

  function normalizeRootPath(rootPath) {
    const trimmed = String(rootPath || '').trim();
    if (!trimmed) {
      return '';
    }
    return path.resolve(trimmed);
  }

  function normalizeSuffixInput(rawInput) {
    const normalized = String(rawInput || '').trim().toUpperCase();
    return ['-A', '-1', '_DUP1'].includes(normalized) ? normalized : '-A';
  }

  function normalizeFilmId(rawValue) {
    const compactValue = String(rawValue || '')
      .toUpperCase()
      .trim()
      .replace(/[_\s]+/g, '-')
      .replace(/-+/g, '-');
    // Keep archived compatibility matching aligned with the active Go rule:
    // TL-1, TL-01, and TL-00001 all refer to canonical TL-001.
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

  function extractFilmId(value) {
    const normalizedValue = String(value || '').toUpperCase();
    const directMatch = normalizedValue.match(/([A-Z]{2,12}-?\d{1,8}[A-Z]*)/);
    if (directMatch && directMatch[1]) {
      return normalizeFilmId(directMatch[1]);
    }

    try {
      const parsedUrl = new URL(String(value || ''));
      const pathname = parsedUrl.pathname.split('/').filter(Boolean).pop() || '';
      const pathMatch = pathname.toUpperCase().match(/([A-Z]{2,12}-?\d{1,8}[A-Z]*)/);
      return pathMatch && pathMatch[1] ? normalizeFilmId(pathMatch[1]) : '';
    } catch {
      return '';
    }
  }

  function buildExpectedCodeSets(rawCodes) {
    const codes = Array.isArray(rawCodes) ? rawCodes : [];
    const codeSet = new Set();
    const tokenSet = new Set();

    codes.forEach((code) => {
      const normalizedCode = normalizeFilmId(code);
      if (!normalizedCode) {
        return;
      }

      codeSet.add(normalizedCode);
      const token = normalizeCodeToken(normalizedCode);
      if (token) {
        tokenSet.add(token);
      }
    });

    return {
      codeSet,
      tokenSet
    };
  }

  function normalizeMagnetEntry(rawEntry) {
    if (!rawEntry) {
      return null;
    }

    if (typeof rawEntry === 'string') {
      const link = rawEntry.trim();
      if (!link) {
        return null;
      }
      return {
        link,
        size: '',
        displayName: ''
      };
    }

    if (typeof rawEntry !== 'object') {
      return null;
    }

    const link = String(rawEntry.link || rawEntry.magnet || '').trim();
    if (!link) {
      return null;
    }

    return {
      link,
      size: String(rawEntry.size || '').trim(),
      displayName: String(rawEntry.displayName || '').trim()
    };
  }

  function normalizeMagnetEntries(rawValue) {
    const list = Array.isArray(rawValue)
      ? rawValue
      : typeof rawValue === 'string'
        ? rawValue
            .split(/\r?\n+/)
            .map((item) => item.trim())
            .filter(Boolean)
        : [];

    const output = [];
    const seen = new Set();

    list.forEach((rawEntry) => {
      const entry = normalizeMagnetEntry(rawEntry);
      if (!entry) {
        return;
      }

      const key = entry.link.toLowerCase();
      if (seen.has(key)) {
        return;
      }

      seen.add(key);
      output.push(entry);
    });

    return output;
  }

  function mergeMagnetEntries(...groups) {
    const merged = [];
    const seen = new Set();

    groups.forEach((group) => {
      normalizeMagnetEntries(group).forEach((entry) => {
        const key = entry.link.toLowerCase();
        if (seen.has(key)) {
          return;
        }

        seen.add(key);
        merged.push(entry);
      });
    });

    return merged;
  }

  function buildExpectedCodeEntryMap(rawEntries) {
    const entries = Array.isArray(rawEntries) ? rawEntries : [];
    const map = new Map();

    entries.forEach((entry) => {
      if (!entry || typeof entry !== 'object') {
        return;
      }

      const code = normalizeFilmId(entry.code || '');
      if (!code) {
        return;
      }

      const existing = map.get(code) || [];
      const merged = mergeMagnetEntries(existing, entry.magnets);
      map.set(code, merged);
    });

    return map;
  }

  function buildNormalizedExpectedCodeEntries(codes, codeEntryMap) {
    return sortCodeAlphabetically(codes).map((code) => ({
      code,
      magnets: mergeMagnetEntries(codeEntryMap.get(code) || [])
    }));
  }

  function readNonNegativeCount(value, fallback = 0) {
    return toSafeInteger(value, fallback, 0);
  }

  function sameExpectedSourcePath(left, right) {
    const normalizedLeft = String(left || '').trim();
    const normalizedRight = String(right || '').trim();
    if (!normalizedLeft || !normalizedRight) {
      return false;
    }

    try {
      return path.resolve(normalizedLeft).toLowerCase() === path.resolve(normalizedRight).toLowerCase();
    } catch {
      return normalizedLeft.toLowerCase() === normalizedRight.toLowerCase();
    }
  }

  // Organizer surfaces crawl artifact provenance in UI/logs. Keep the source
  // type/path resolution centralized so compatibility callers do not drift into
  // contradictory metadata such as "filmData" paired with organizer-codes.json.
  function resolveExpectedSourceType(sourceType, options = {}) {
    const normalizedSourceType = String(sourceType || '').trim();
    if (normalizedSourceType) {
      return normalizedSourceType;
    }

    const sourcePath = String(options.sourcePath || '').trim();
    const filmDataPath = String(options.filmDataPath || '').trim();
    const organizerCodesPath = String(options.organizerCodesPath || '').trim();
    const preferredType = String(options.preferredType || '').trim();
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
    if (preferredType && preferredType !== 'payload') {
      return preferredType;
    }
    if (sourcePath || hasCodes || preferredType === 'payload') {
      return 'payload';
    }

    return '';
  }

  function resolveExpectedSourcePath(sourceType, options = {}) {
    const normalizedSourceType = String(sourceType || '').trim();
    const sourcePath = String(options.sourcePath || '').trim();
    const filmDataPath = String(options.filmDataPath || '').trim();
    const organizerCodesPath = String(options.organizerCodesPath || '').trim();

    if (normalizedSourceType === 'organizerCodes') {
      return organizerCodesPath || sourcePath || filmDataPath || '';
    }
    if (normalizedSourceType === 'filmData') {
      return filmDataPath || sourcePath || organizerCodesPath || '';
    }
    if (normalizedSourceType === 'payload') {
      return sourcePath || filmDataPath || organizerCodesPath || '';
    }

    return sourcePath || organizerCodesPath || filmDataPath || '';
  }

  // Compatibility organizer callers may still pass one of three shapes:
  // 1) a single preloadedExpected snapshot
  // 2) legacy expectedCodes / expectedCodeEntries fields
  // 3) a mixture while old entrypoints are still being trimmed
  //
  // Normalize them here so the service owns one merge rule for crawl-derived
  // organizer expectations instead of letting IPC/sidecar layers drift apart.
  function buildExpectedSnapshotPayload(snapshot = {}, fallback = {}) {
    const snapshotValue = snapshot && typeof snapshot === 'object' ? snapshot : {};
    const fallbackValue = fallback && typeof fallback === 'object' ? fallback : {};
    const codeSetBundle = buildExpectedCodeSets([
      ...(Array.isArray(snapshotValue.codes) ? snapshotValue.codes : []),
      ...(Array.isArray(fallbackValue.codes) ? fallbackValue.codes : [])
    ]);
    const codeEntryMap = buildExpectedCodeEntryMap([
      ...(Array.isArray(snapshotValue.codeEntries) ? snapshotValue.codeEntries : []),
      ...(Array.isArray(fallbackValue.codeEntries) ? fallbackValue.codeEntries : [])
    ]);

    codeEntryMap.forEach((_, code) => {
      codeSetBundle.codeSet.add(code);
      const token = normalizeCodeToken(code);
      if (token) {
        codeSetBundle.tokenSet.add(token);
      }
    });

    const codes = sortCodeAlphabetically(codeSetBundle.codeSet);
    const codeEntries = buildNormalizedExpectedCodeEntries(codes, codeEntryMap);
    const totalRecords = readNonNegativeCount(
      snapshotValue.totalRecords,
      readNonNegativeCount(fallbackValue.totalRecords, 0)
    );
    const filmDataPath = String(snapshotValue.filmDataPath || fallbackValue.filmDataPath || '').trim();
    const organizerCodesPath = String(snapshotValue.organizerCodesPath || fallbackValue.organizerCodesPath || '').trim();
    const sourcePathInput = String(snapshotValue.sourcePath || fallbackValue.sourcePath || '').trim();
    const sourceType = resolveExpectedSourceType(String(snapshotValue.sourceType || '').trim(), {
      sourcePath: sourcePathInput,
      filmDataPath,
      organizerCodesPath,
      preferredType: String(fallbackValue.sourceType || '').trim(),
      hasCodes: codes.length > 0 || codeEntries.length > 0
    });
    const sourcePath = resolveExpectedSourcePath(sourceType, {
      sourcePath: sourcePathInput,
      filmDataPath,
      organizerCodesPath
    });

    return {
      sourceType,
      sourcePath,
      outputDir: String(snapshotValue.outputDir || fallbackValue.outputDir || '').trim(),
      filmDataPath,
      organizerCodesPath,
      actressName: String(snapshotValue.actressName || fallbackValue.actressName || '').trim(),
      totalRecords,
      codeCount: Math.max(
        readNonNegativeCount(snapshotValue.codeCount, readNonNegativeCount(fallbackValue.codeCount, 0)),
        codes.length
      ),
      codes,
      codeEntries
    };
  }

  function resolveExpectedInput(options = {}) {
    const preloadedExpected = buildExpectedSnapshotPayload(options.preloadedExpected, {
      sourceType: 'payload',
      outputDir: String(options.crawlOutputDir || '').trim(),
      codes: options.expectedCodes,
      codeEntries: options.expectedCodeEntries
    });

    return {
      preloadedExpected,
      expectedCodes: Array.isArray(preloadedExpected.codes) ? preloadedExpected.codes : [],
      expectedCodeEntries: Array.isArray(preloadedExpected.codeEntries) ? preloadedExpected.codeEntries : []
    };
  }

  // Current Wails renderer normally sends either:
  // 1) one preloadedExpected snapshot, or
  // 2) crawlOutputDir only, letting the backend lazily read artifacts.
  //
  // Keep this compatibility hydration local to organizerService so historical
  // Electron/sidecar callers do not need to each re-implement the same
  // "no snapshot yet, read from crawl output" fallback.
  async function resolveExpectedInputForRun(options = {}) {
    const resolved = resolveExpectedInput(options);
    const hasExpectedCodes =
      (Array.isArray(resolved.expectedCodes) && resolved.expectedCodes.length > 0) ||
      (Array.isArray(resolved.expectedCodeEntries) && resolved.expectedCodeEntries.length > 0);

    if (hasExpectedCodes) {
      return resolved;
    }

    const crawlOutputDir = String(options.crawlOutputDir || '').trim();
    if (!crawlOutputDir) {
      return resolved;
    }

    try {
      const loaded = await loadCrawlFilmCodes({ outputDir: crawlOutputDir });
      return resolveExpectedInput({
        ...options,
        preloadedExpected:
          loaded && loaded.preloadedExpected && typeof loaded.preloadedExpected === 'object'
            ? loaded.preloadedExpected
            : null
      });
    } catch {
      return resolved;
    }
  }

  function extractRecordCode(record) {
    if (!record || typeof record !== 'object') {
      return '';
    }

    const candidates = [record.filmCode, record.sourceLink, record.code, record.title, record.fileName];
    for (const candidate of candidates) {
      const parsed = extractFilmId(candidate || '');
      if (parsed) {
        return parsed;
      }
    }

    return '';
  }

  function extractRecordMagnetEntries(record) {
    if (!record || typeof record !== 'object') {
      return [];
    }

    return mergeMagnetEntries(record.backupMagnetLinks, record.magnetLinks, record.magnet, record.magnets);
  }

  function normalizeVideoExtensionToken(value) {
    const normalized = String(value || '')
      .trim()
      .toLowerCase()
      .replace(/^\*+/, '')
      .replace(/^\.+/, '');

    if (!normalized || !/^[a-z0-9]+$/.test(normalized)) {
      return '';
    }

    return `.${normalized}`;
  }

  function normalizeVideoExtensions(rawValue) {
    const rawItems = Array.isArray(rawValue)
      ? rawValue
      : String(rawValue || '')
          .split(/[,，、;；\s]+/g)
          .filter(Boolean);
    const normalized = rawItems.map((item) => normalizeVideoExtensionToken(item)).filter(Boolean);
    const unique = normalized.length > 0 ? Array.from(new Set(normalized)) : DEFAULT_VIDEO_EXTENSIONS;
    return new Set(unique);
  }

  function formatVideoExtensions(extensionSet) {
    const source = extensionSet instanceof Set ? Array.from(extensionSet) : DEFAULT_VIDEO_EXTENSIONS;
    return source.map((item) => String(item || '').replace(/^\./, '')).filter(Boolean).join(', ');
  }

  function isVideoFile(filePath, extensionSet = DEFAULT_VIDEO_EXTENSION_SET) {
    const allowedExtensions = extensionSet instanceof Set && extensionSet.size > 0 ? extensionSet : DEFAULT_VIDEO_EXTENSION_SET;
    return allowedExtensions.has(path.extname(filePath).toLowerCase());
  }

  function stripDomainNoise(value) {
    return String(value || '')
      .replace(/https?:\/\/\S+/gi, ' ')
      .replace(/[a-z0-9-]+\.(com|net|org|cn|cc|tv|xyz|me|vip|top)/gi, ' ')
      .trim();
  }

  function extractAdvancedFilmCode(value) {
    const compact = String(value || '')
      .toUpperCase()
      .replace(/\s+/g, '');
    const patterns = [
      /^([A-Z]{2,6})[-_]?(\d{1,6})$/,
      /^(N\d{3,6})$/,
      /^(T-?\d{3,6})$/,
      /^(CARIB\d{2,6})$/,
      /^(HEYZO\d{2,6})$/,
      /^(1PONDO\d{2,6})$/
    ];

    for (const pattern of patterns) {
      const match = compact.match(pattern);
      if (!match) {
        continue;
      }

      if (match.length === 3) {
        return normalizeFilmId(`${match[1]}-${match[2]}`);
      }

      return normalizeFilmId(String(match[1] || '').replace(/_/g, '-'));
    }

    return '';
  }

  function extractFilmCodeFromFile(filePath, expectedTokenSet) {
    const expectedTokens = expectedTokenSet || new Set();
    let baseName = path.basename(filePath, path.extname(filePath));
    const atIndex = baseName.lastIndexOf('@');
    if (atIndex >= 0 && atIndex + 1 < baseName.length) {
      baseName = baseName.slice(atIndex + 1);
    }

    baseName = stripDomainNoise(baseName);
    const normalized = baseName
      .toUpperCase()
      .replace(/[^A-Z0-9]+/g, ' ')
      .trim();

    if (!normalized) {
      return '';
    }

    const compact = normalized.replace(/\s+/g, '');
    if (expectedTokens.size > 0) {
      for (const token of expectedTokens) {
        if (compact.includes(token)) {
          return normalizeFilmId(token);
        }
      }
    }

    const advanced = extractAdvancedFilmCode(normalized);
    if (advanced) {
      return advanced;
    }

    const fc2Match = normalized.match(/\bFC2[-_ ]*PPV[-_ ]*([0-9]{5,8})\b/i);
    if (fc2Match) {
      return normalizeFilmId(`FC2-PPV-${fc2Match[1]}`);
    }

    const standardMatch = normalized.match(/([A-Z]{2,12})[-_ ]*([0-9]{1,8})/i);
    if (standardMatch) {
      const prefix = String(standardMatch[1] || '').toUpperCase();
      const number = String(standardMatch[2] || '');
      if (!PREFIX_BLACKLIST.has(prefix)) {
        return normalizeFilmId(`${prefix}-${number}`);
      }
    }

    const compactMatch = normalized.match(/\b([A-Z]{2,12})([0-9]{1,8})\b/i);
    if (compactMatch) {
      const prefix = String(compactMatch[1] || '').toUpperCase();
      const number = String(compactMatch[2] || '');
      if (!PREFIX_BLACKLIST.has(prefix)) {
        return normalizeFilmId(`${prefix}-${number}`);
      }
    }

    return '';
  }

  // Last-resort matching for a noisy filename/path. The regular extraction
  // remains authoritative; this only runs when its result misses the loaded
  // expected-code set, so the fallback cannot invent an unrelated code.
  function matchExpectedCodeFallback(value, expectedCodeSet) {
    const expectedCodes = expectedCodeSet instanceof Set ? Array.from(expectedCodeSet) : [];
    if (expectedCodes.length === 0) {
      return '';
    }

    const cleanedValue = stripDomainNoise(String(value || '').toUpperCase());
    const matchedCodes = new Set();

    expectedCodes.forEach((expectedCode) => {
      const normalizedCode = normalizeFilmId(expectedCode);
      const match = normalizedCode.match(/^([A-Z]{2,12})-?(\d{1,8})([A-Z]*)$/);
      if (!match) {
        return;
      }

      const prefix = match[1];
      const number = String(Number(match[2]));
      const suffix = match[3] || '';
      const pattern = new RegExp(
        `(?:^|[^A-Z0-9])${prefix}[-_ ]*0*${number}${suffix}(?:$|[^A-Z0-9])`,
        'i'
      );
      if (pattern.test(cleanedValue)) {
        matchedCodes.add(normalizedCode);
      }
    });

    return matchedCodes.size === 1 ? Array.from(matchedCodes)[0] : '';
  }

  function alphaIndexToText(n) {
    let index = Math.max(1, n);
    let output = '';
    while (index > 0) {
      index -= 1;
      output = String.fromCharCode(65 + (index % 26)) + output;
      index = Math.floor(index / 26);
    }
    return output;
  }

  function isAlphaNumericAscii(charCode) {
    return (
      (charCode >= 48 && charCode <= 57) ||
      (charCode >= 65 && charCode <= 90) ||
      (charCode >= 97 && charCode <= 122)
    );
  }

  function parseConflictSuffixStrategy(rawInput) {
    const raw = normalizeSuffixInput(rawInput);

    if (/\s/.test(raw)) {
      throw new Error('冲突后缀只能选择 A、B、1、2 或 _DUP1、_DUP2。');
    }

    const alphaMatch = raw.match(/^(.*?)([A-Za-z])$/);
    if (alphaMatch) {
      const prefix = alphaMatch[1] || '';
      const lastPrefixChar = prefix ? prefix.charCodeAt(prefix.length - 1) : 0;
      const canUseAlpha = !prefix || !isAlphaNumericAscii(lastPrefixChar);
      if (canUseAlpha) {
        const startChar = String(alphaMatch[2] || 'A').toUpperCase().charCodeAt(0) - 64;
        return {
          mode: 'alpha',
          prefix,
          startChar: Math.min(Math.max(startChar, 1), 26),
          startNum: 1,
          raw
        };
      }
    }

    const numericMatch = raw.match(/^(.*?)(\d+)$/);
    if (numericMatch) {
      return {
        mode: 'num',
        prefix: numericMatch[1] || '',
        startChar: 1,
        startNum: Math.max(1, Number.parseInt(numericMatch[2], 10) || 1),
        raw
      };
    }

    return {
      mode: 'num',
      prefix: raw,
      startChar: 1,
      startNum: 1,
      raw
    };
  }

  function formatSuffix(strategy, sequence) {
    if (strategy.mode === 'alpha') {
      return `${strategy.prefix}${alphaIndexToText(strategy.startChar + sequence)}`;
    }
    return `${strategy.prefix}${strategy.startNum + sequence}`;
  }

  // Path resolution stays centralized here so report names and managed
  // directories cannot drift across scan / move / report phases.
  function resolvePaths(rootPath) {
    const normalizedRootPath = normalizeRootPath(rootPath);

    return {
      rootPath: normalizedRootPath,
      waitingDir: path.join(normalizedRootPath, '\u5f85\u6574\u7406'),
      toDeleteDir: path.join(normalizedRootPath, '\u5f85\u5220\u9664'),
      introAdDir: path.join(normalizedRootPath, '\u542b\u5f00\u5934\u5e7f\u544a'),
      logsDir: path.join(normalizedRootPath, 'logs'),
      stateDir: path.join(normalizedRootPath, '.video-organizer-state'),
      renameMapPath: path.join(normalizedRootPath, NORMALIZED_REPORT_FILES.renameMap),
      unmatchedPath: path.join(normalizedRootPath, NORMALIZED_REPORT_FILES.unmatched),
      adRiskCodesPath: path.join(normalizedRootPath, NORMALIZED_REPORT_FILES.adRiskCodes),
      adRiskDetailPath: path.join(normalizedRootPath, NORMALIZED_REPORT_FILES.adRiskDetail),
      adRiskMagnetsPath: path.join(normalizedRootPath, NORMALIZED_REPORT_FILES.adRiskMagnets),
      missingMagnetsPath: path.join(normalizedRootPath, NORMALIZED_REPORT_FILES.missingMagnets)
    };
  }

  function resolveTargetPath(rootPath, kind = 'root') {
    const paths = resolvePaths(rootPath);
    switch (String(kind || 'root')) {
      case 'waiting':
        return paths.waitingDir;
      case 'delete':
        return paths.toDeleteDir;
      case 'intro-ad':
        return paths.introAdDir;
      case 'logs':
        return paths.logsDir;
      case 'reports':
        return paths.rootPath;
      case 'root':
      default:
        return paths.rootPath;
    }
  }

  // Organizer compatibility flows treat the crawl output directory as the
  // canonical artifact root. filmData.json and organizer-codes.json are both
  // derived from that one root so callers do not each rebuild path guesses.
  function resolveCrawlOutputPaths(outputDir) {
    const normalizedOutputDir = normalizeRootPath(outputDir);
    return {
      outputDir: normalizedOutputDir,
      filmDataPath: path.join(normalizedOutputDir, CRAWL_FILM_DATA_FILE),
      organizerCodesPath: path.join(normalizedOutputDir, CRAWL_ORGANIZER_CODES_FILE)
    };
  }

  // Legacy-report cleanup exists only to remove files emitted by older
  // Electron/compatibility builds before the normalized report names below
  // became stable.
  async function cleanupLegacyReportFiles(rootPath, onLog) {
    if (!rootPath || !path.isAbsolute(rootPath)) {
      return 0;
    }

    let removedCount = 0;
    for (const fileName of LEGACY_REPORT_FILE_NAMES) {
      const legacyPath = path.join(rootPath, fileName);
      const stat = await fs.promises.stat(legacyPath).catch(() => null);
      if (!stat || !stat.isFile()) {
        continue;
      }

      await fs.promises.rm(legacyPath, { force: true }).catch(() => {});
      removedCount += 1;
      emitLog(onLog, 'info', `Removed legacy report: ${legacyPath}`);
    }

    return removedCount;
  }

  async function pathExists(targetPath) {
    try {
      await fs.promises.access(targetPath);
      return true;
    } catch {
      return false;
    }
  }

  async function ensureDirectory(targetPath) {
    await fs.promises.mkdir(targetPath, { recursive: true });
  }

  async function renameOrCopy(srcPath, targetPath) {
    try {
      await fs.promises.rename(srcPath, targetPath);
      return;
    } catch {
      await ensureDirectory(path.dirname(targetPath));
      await pipeline(fs.createReadStream(srcPath), fs.createWriteStream(targetPath));
      await fs.promises.unlink(srcPath);
    }
  }

  async function moveWithUnique(srcPath, desiredTargetPath) {
    await ensureDirectory(path.dirname(desiredTargetPath));

    let targetPath = desiredTargetPath;
    if (await pathExists(targetPath)) {
      const extension = path.extname(desiredTargetPath);
      const baseName = path.basename(desiredTargetPath, extension);
      const parentDir = path.dirname(desiredTargetPath);

      for (let index = 1; index <= 9999; index += 1) {
        const candidate = path.join(parentDir, `${baseName}_DUP${index}${extension}`);
        if (!(await pathExists(candidate))) {
          targetPath = candidate;
          break;
        }
      }
    }

    await renameOrCopy(srcPath, targetPath);
    return targetPath;
  }

  async function collectFiles(rootPath, includeSubdirectories, videoExtensionSet = DEFAULT_VIDEO_EXTENSION_SET) {
    const files = [];

    // 顶层扫描会跳过 organizer 自己管理的目录，避免重复运行时把历史输出再次当成原始素材。
    async function walk(currentPath, topDirName = '') {
      let entries = [];
      try {
        entries = await fs.promises.readdir(currentPath, { withFileTypes: true });
      } catch {
        return;
      }

      for (const entry of entries) {
        const entryPath = path.join(currentPath, entry.name);
        if (entry.isFile()) {
          const relativePath = path.relative(rootPath, entryPath);
          files.push({
            path: entryPath,
            relativePath,
            topDirName,
            isRootLevel: Boolean(relativePath) && !relativePath.includes(path.sep),
            isVideo: isVideoFile(entryPath, videoExtensionSet)
          });
          continue;
        }

        if (!entry.isDirectory()) {
          continue;
        }

        const nextTopDirName = topDirName || entry.name;
        if (isManagedDirectoryName(nextTopDirName) || isManagedDirectoryName(entry.name)) {
          continue;
        }

        if (!includeSubdirectories) {
          continue;
        }

        await walk(entryPath, nextTopDirName);
      }
    }

    await walk(rootPath, '');
    return files;
  }

  async function cleanupEmptyDirectories(rootPath, options = {}) {
    const preservedTopDirs = options.preservedTopDirs instanceof Set ? options.preservedTopDirs : MANAGED_TOP_DIRS;
    const removedDirs = [];
    const retryableCodes = new Set(['EBUSY', 'EPERM', 'ENOTEMPTY']);

    async function removeEmptyDirectory(targetPath) {
      for (let attempt = 0; attempt < 3; attempt += 1) {
        try {
          await fs.promises.rmdir(targetPath);
          return true;
        } catch (error) {
          const code = error && error.code ? String(error.code) : '';
          if (!retryableCodes.has(code) || attempt >= 2) {
              if (code && code !== 'ENOTEMPTY') {
                emitLog(options.onLog, 'warn', `空目录清理失败：${targetPath}（${code}）`);
              }
            return false;
          }
          // eslint-disable-next-line no-await-in-loop
          await new Promise((resolve) => setTimeout(resolve, 30 * (attempt + 1)));
        }
      }
      return false;
    }

    async function walk(currentPath, isRoot = false) {
      let entries = [];
      try {
        entries = await fs.promises.readdir(currentPath, { withFileTypes: true });
      } catch {
        return;
      }

      for (const entry of entries) {
        if (!entry.isDirectory()) {
          continue;
        }
        await walk(path.join(currentPath, entry.name), false);
      }

      if (isRoot) {
        return;
      }

      const relativePath = path.relative(rootPath, currentPath);
      if (!relativePath || relativePath.startsWith('..')) {
        return;
      }

      const topDirName = relativePath.split(path.sep).filter(Boolean)[0] || '';
      if (preservedTopDirs.has(topDirName)) {
        return;
      }

      const restEntries = await fs.promises.readdir(currentPath).catch(() => null);
      if (!Array.isArray(restEntries) || restEntries.length > 0) {
        return;
      }

      const removed = await removeEmptyDirectory(currentPath);
      if (removed) {
        removedDirs.push(currentPath);
        emitLog(options.onLog, 'info', `已删除空目录：${currentPath}`);
      }
    }

    await walk(rootPath, true);
    return removedDirs;
  }

  async function removeDirectoryWithRetry(targetPath, options = {}) {
    const retryableCodes = new Set(['EBUSY', 'EPERM', 'ENOTEMPTY']);
    const maxAttempts = Math.max(1, Number.parseInt(String(options.maxAttempts ?? ''), 10) || 4);

    for (let attempt = 0; attempt < maxAttempts; attempt += 1) {
      try {
        await fs.promises.rm(targetPath, { recursive: true, force: true });
      } catch (error) {
        const code = error && error.code ? String(error.code) : '';
        if (!retryableCodes.has(code) && attempt >= maxAttempts - 1) {
          return false;
        }
      }

      // eslint-disable-next-line no-await-in-loop
      const exists = await pathExists(targetPath);
      if (!exists) {
        return true;
      }

      // eslint-disable-next-line no-await-in-loop
      await new Promise((resolve) => setTimeout(resolve, 40 * (attempt + 1)));
    }

    return !(await pathExists(targetPath));
  }

  async function compactRootDirectories(rootPath, paths, adFileAction, options = {}) {
    if (!rootPath || !paths || !path.isAbsolute(rootPath)) {
      return {
        removedDirs: 0,
        movedDirs: 0
      };
    }

    const dryRun = Boolean(options.dryRun);
    const protectedSourcePaths = Array.isArray(options.protectedSourcePaths)
      ? options.protectedSourcePaths
          .map((item) => {
            try {
              return path.resolve(String(item || '').trim());
            } catch {
              return '';
            }
          })
          .filter(Boolean)
      : [];
    const keepTopDirs = new Set([
      path.basename(paths.waitingDir),
      path.basename(paths.introAdDir),
      path.basename(paths.logsDir),
      path.basename(paths.stateDir)
    ]);
    if (adFileAction === 'move-to-delete') {
      keepTopDirs.add(path.basename(paths.toDeleteDir));
    }

    let movedDirs = 0;
    let removedDirs = 0;
    const entries = await fs.promises.readdir(rootPath, { withFileTypes: true }).catch(() => []);

    for (const entry of entries) {
      if (!entry.isDirectory()) {
        continue;
      }

      if (keepTopDirs.has(entry.name)) {
        continue;
      }

      const sourceDir = path.join(rootPath, entry.name);
      const hasProtectedSource = protectedSourcePaths.some(
        (protectedPath) => protectedPath === sourceDir || protectedPath.startsWith(`${sourceDir}${path.sep}`)
      );
      if (hasProtectedSource) {
        emitLog(options.onLog, 'warn', `根目录残留目录已保留：${sourceDir}（存在移动失败的视频，请人工复核）`);
        continue;
      }

      if (dryRun) {
        emitLog(options.onLog, 'info', `[预览] 根目录残留目录待处理：${sourceDir}`);
        continue;
      }

      const removed = await removeDirectoryWithRetry(sourceDir, { maxAttempts: 5 });
      if (removed) {
        removedDirs += 1;
        emitLog(options.onLog, 'info', `根目录残留目录已删除：${sourceDir}`);
      } else {
        emitLog(options.onLog, 'warn', `根目录残留目录删除失败，请手动处理：${sourceDir}`);
      }
    }

    if (!dryRun) {
      const finalSweepEntries = await fs.promises.readdir(rootPath, { withFileTypes: true }).catch(() => []);
      for (const entry of finalSweepEntries) {
        if (!entry.isDirectory() || keepTopDirs.has(entry.name)) {
          continue;
        }

        const sourceDir = path.join(rootPath, entry.name);
        const hasProtectedSource = protectedSourcePaths.some(
          (protectedPath) => protectedPath === sourceDir || protectedPath.startsWith(`${sourceDir}${path.sep}`)
        );
        if (hasProtectedSource) {
          emitLog(options.onLog, 'warn', `根目录二次清理已跳过：${sourceDir}（存在移动失败的视频）`);
          continue;
        }

        const removed = await removeDirectoryWithRetry(sourceDir, { maxAttempts: 6 });
        if (removed) {
          removedDirs += 1;
          emitLog(options.onLog, 'warn', `根目录二次清理已删除残留目录：${sourceDir}`);
        } else {
          emitLog(options.onLog, 'warn', `根目录二次清理仍失败，请关闭占用后重试：${sourceDir}`);
        }
      }
    }

    return {
      removedDirs,
      movedDirs
    };
  }

  function planTargetNames(candidates, strategy) {
    const groupedIndex = new Map();
    const outputNames = new Array(candidates.length);

    candidates.forEach((item, index) => {
      const shouldRenameByFilmCode = Boolean(item && item.renameByFilmCode && item.filmCode);
      if (!shouldRenameByFilmCode) {
        const originalName = path.basename(String((item && item.src) || '').trim());
        outputNames[index] = originalName || `UNNAMED_${index + 1}`;
        return;
      }

      if (!groupedIndex.has(item.filmCode)) {
        groupedIndex.set(item.filmCode, []);
      }
      groupedIndex.get(item.filmCode).push(index);
    });

    const sortedCodes = Array.from(groupedIndex.keys()).sort((left, right) =>
      left.localeCompare(right, 'en', { sensitivity: 'base' })
    );

    sortedCodes.forEach((filmCode) => {
      const indexes = groupedIndex.get(filmCode) || [];
      indexes.sort((left, right) =>
        String(candidates[left].src || '').localeCompare(String(candidates[right].src || ''), 'en', {
          sensitivity: 'base'
        })
      );

      const useSuffix = indexes.length > 1;
      indexes.forEach((candidateIndex, sequence) => {
        const extension = path.extname(candidates[candidateIndex].src).toLowerCase();
        const suffix = useSuffix ? formatSuffix(strategy, sequence) : '';
        outputNames[candidateIndex] = `${filmCode}${suffix}${extension}`;
      });
    });

    return outputNames;
  }

  function formatBytesToGB(bytes) {
    return (bytes / 1024 / 1024 / 1024).toFixed(2);
  }

  function sortCodeAlphabetically(codes) {
    return Array.from(codes || []).sort((left, right) => String(left || '').localeCompare(String(right || ''), 'en'));
  }

  function buildAdRiskCodeDetails(records = []) {
    const codeMap = new Map();

    (Array.isArray(records) ? records : []).forEach((record) => {
      const filmCode = normalizeFilmId(record && record.filmCode ? record.filmCode : '');
      if (!filmCode) {
        return;
      }

      const score = Number.isFinite(Number(record && record.score)) ? Number(record.score) : 0;
      const reasons = Array.isArray(record && record.reasons)
        ? record.reasons
            .map((item) => String(item || '').trim())
            .filter(Boolean)
        : [];
      const sourcePath = String(record && record.sourcePath ? record.sourcePath : '');
      const size = Number.isFinite(Number(record && record.size)) ? Number(record.size) : 0;
      const evidence = record && record.evidence && typeof record.evidence === 'object' ? record.evidence : null;
      const existing = codeMap.get(filmCode);

      if (!existing || score > existing.maxScore) {
        codeMap.set(filmCode, {
          filmCode,
          maxScore: score,
          sourcePath,
          size,
          reasons: reasons.slice(0, 6),
          evidence
        });
        return;
      }

      if (existing.reasons.length === 0 && reasons.length > 0) {
        existing.reasons = reasons.slice(0, 6);
      }
    });

    return Array.from(codeMap.values()).sort((left, right) => {
      if (right.maxScore !== left.maxScore) {
        return right.maxScore - left.maxScore;
      }
      return String(left.filmCode || '').localeCompare(String(right.filmCode || ''), 'en');
    });
  }

  function buildSupplementMagnetEntries(codes, expectedCodeEntryMap) {
    const codeEntryMap = expectedCodeEntryMap instanceof Map ? expectedCodeEntryMap : new Map();

    return sortCodeAlphabetically(codes).map((code) => ({
      code,
      magnets: mergeMagnetEntries(codeEntryMap.get(code) || [])
    }));
  }

  // Report writing is isolated here so encoding/newline decisions stay in one
  // place for all organizer compatibility reports.
  async function writeTextFile(filePath, lines) {
    await fs.promises.writeFile(filePath, `${lines.join('\r\n')}\r\n`, 'utf8');
  }

  // These summary helpers feed only the report layer. If counts look wrong but
  // file movement is correct, start here instead of re-reading earlier phases.
  function summarizeUnmatchedRecords(records = [], maxDisplay = 600) {
    const list = Array.isArray(records) ? records.filter((item) => item && typeof item === 'object') : [];
    const reasonCounter = new Map();
    let videoCount = 0;
    let nonVideoCount = 0;

    list.forEach((item) => {
      const reason = String(item.reason || 'unclassified').trim() || 'unclassified';
      reasonCounter.set(reason, Number(reasonCounter.get(reason) || 0) + 1);
      if (Boolean(item.isVideo) || isVideoFile(item.path || '')) {
        videoCount += 1;
      } else {
        nonVideoCount += 1;
      }
    });

    const displayRecords = list
      .slice(0)
      .sort((left, right) => {
        const leftVideo = Boolean(left.isVideo) || isVideoFile(left.path || '') ? 1 : 0;
        const rightVideo = Boolean(right.isVideo) || isVideoFile(right.path || '') ? 1 : 0;
        if (leftVideo !== rightVideo) {
          return rightVideo - leftVideo;
        }
        const leftSize = Number(left.size || 0);
        const rightSize = Number(right.size || 0);
        if (leftSize !== rightSize) {
          return rightSize - leftSize;
        }
        return String(left.path || '').localeCompare(String(right.path || ''), 'en', { sensitivity: 'base' });
      })
      .slice(0, Math.max(1, maxDisplay));

    const reasonStats = Array.from(reasonCounter.entries())
      .map(([reason, count]) => ({ reason, count }))
      .sort((left, right) => right.count - left.count);

    return {
      total: list.length,
      videoCount,
      nonVideoCount,
      reasonStats,
      displayRecords,
      omittedCount: Math.max(0, list.length - displayRecords.length)
    };
  }

  function formatRenameRecordLine(record = {}, index = 0) {
    const filmCodeLabel = String(record.filmCode || '').trim() || 'unknown-code';
    const renameApplied = Boolean(record.renameApplied);
    const note = String(record.note || '').trim();
    const actionLabel = renameApplied ? 'renamed by film code' : `kept original name (${note || 'no matched film code'})`; 
    return `${index + 1}. ${record.originalName} => ${record.newName} | [${filmCodeLabel}] | ${actionLabel} | ${record.originalPath}`;
  }

  // Report generation is the last compatibility-facing presentation step after
  // scan/judge/rename/cleanup have already produced the raw summary data.
  // 让展示文案集中留在这里，前面的 scan/rename/move 阶段就只负责业务决策。
  async function writeReports(
    paths,
    summary,
    renameRecords,
    unmatchedRecords,
    adRiskRecords = [],
    adRiskMagnetEntries = [],
    missingMagnetEntries = []
  ) {
    const nowText = new Date().toLocaleString('zh-CN', { hour12: false });

    await writeTextFile(paths.renameMapPath, [
      '视频整理助手 - 更改前后对照',
      `生成时间：${nowText}`,
      `扫描总数：${summary.scannedTotal}`,
      `视频总数：${summary.videoTotal}`,
      `命中番号：${summary.qualifiedVideo}`,
      `移入待整理：${summary.movedToWaiting}`,
      '',
      '明细（原名 => 新名 | 番号 | 原路径）：',
      '----------------------------------------',
      ...renameRecords.map(
        (record, index) => formatRenameRecordLine(record, index)
      )
    ]);

    const unmatchedSummary = summarizeUnmatchedRecords(unmatchedRecords, 600);
    await writeTextFile(paths.unmatchedPath, [
      '视频整理助手 - 待删除或未命中明细',
      `生成时间：${nowText}`,
      `视频总数：${summary.videoTotal}`,
      `命中番号：${summary.qualifiedVideo}`,
      `待删除处理总数：${(summary.movedToDelete || 0) + (summary.deletedDirectly || 0)}`,
      `移入待删除：${summary.movedToDelete || 0}`,
      `直接删除：${summary.deletedDirectly || 0}`,
      `归入含开头广告：${summary.movedToIntroAd || 0}`,
      '',
      '明细（原因 | 大小GB | 路径）：',
      '----------------------------------------',
      ...unmatchedSummary.displayRecords.map(
        (record, index) => `${index + 1}. [${record.reason}] ${formatBytesToGB(record.size)}GB | ${record.path}`
      ),
      ...(unmatchedSummary.omittedCount > 0
        ? [`... ${unmatchedSummary.omittedCount} more records omitted to keep the report readable.`]
        : [])
    ]);

    const adRiskCodeDetails = buildAdRiskCodeDetails(adRiskRecords);
    const adRiskCodes = adRiskCodeDetails.map((item) => item.filmCode);
    const highRiskCodes = adRiskCodeDetails.filter((item) => item.maxScore >= 80);
    const reviewRiskCodes = adRiskCodeDetails.filter((item) => item.maxScore >= 70 && item.maxScore < 80);
    const observedRiskCodes = adRiskCodeDetails.filter((item) => item.maxScore < 70);

    await writeTextFile(paths.adRiskCodesPath, [
      '视频整理助手 - 含开头广告番号',
      `生成时间：${nowText}`,
      `含开头广告番号总数：${adRiskCodes.length}`,
      `高置信（>=80）：${highRiskCodes.length}`,
      `建议复核（70-79）：${reviewRiskCodes.length}`,
      `观察项（<70）：${observedRiskCodes.length}`,
      '',
      ...(adRiskCodes.length > 0 ? adRiskCodes.map((code, index) => `${index + 1}. ${code}`) : ['未发现含开头广告番号。'])
    ]);

    await writeTextFile(paths.adRiskDetailPath, [
      '视频整理助手 - 含开头广告明细',
      `生成时间：${nowText}`,
      `含开头广告番号总数：${adRiskCodeDetails.length}`,
      '分级规则：高置信 >=80；建议复核 70-79；观察项 <70',
      '',
      '明细（番号 | 评分 | 大小GB | 原因 | 证据 | 路径）：',
      '----------------------------------------',
      ...adRiskCodeDetails.map((item, index) => {
        const reasons = item.reasons.length > 0 ? item.reasons.join('; ') : '-';
        const sourcePath = item.sourcePath || '-';
        const evidence = item.evidence
          ? `帧哈希数=${Array.isArray(item.evidence.frameHashes) ? item.evidence.frameHashes.length : 0}；模板命中=${
              item.evidence.bestTemplateMatch ? item.evidence.bestTemplateMatch.templateId || '-' : '-'
            }；广告样本命中=${item.evidence.bestAdSampleMatch ? item.evidence.bestAdSampleMatch.sampleId || '-' : '-'}`
          : '-';
        return `${index + 1}. ${item.filmCode} | ${item.maxScore} | ${formatBytesToGB(item.size)}GB | ${reasons} | ${evidence} | ${sourcePath}`;
      }),
      ...(adRiskCodeDetails.length === 0 ? ['未发现含开头广告明细。'] : [])
    ]);

    const adRiskMagnetLines = [
      '视频整理助手 - 含开头广告补抓磁力',
      `生成时间：${nowText}`,
      `补抓条目总数：${adRiskMagnetEntries.length}`,
      ''
    ];

    if (!Array.isArray(adRiskMagnetEntries) || adRiskMagnetEntries.length === 0) {
      adRiskMagnetLines.push('未生成含开头广告补抓磁力。');
    } else {
      adRiskMagnetEntries.forEach((entry, index) => {
        const code = normalizeFilmId(entry.code || '');
        const magnets = mergeMagnetEntries(entry.magnets || []);
        adRiskMagnetLines.push(`${index + 1}. [${code || '未知番号'}]`);
        if (magnets.length === 0) {
          adRiskMagnetLines.push('   （无可用磁力）');
        } else {
          magnets.forEach((magnet, magnetIndex) => {
            const sizeLabel = magnet.size ? ` [${magnet.size}]` : '';
            adRiskMagnetLines.push(`   ${magnetIndex + 1})${sizeLabel} ${magnet.link}`);
          });
        }
        adRiskMagnetLines.push('');
      });
    }

    await writeTextFile(paths.adRiskMagnetsPath, adRiskMagnetLines);

    const missingMagnetLines = [
      '视频整理助手 - 遗漏番号磁力补抓',
      `生成时间：${nowText}`,
      `爬虫番号总数：${summary.expectedCodeTotal || 0}`,
      `本地识别番号总数：${summary.detectedCodeCount || 0}`,
      `遗漏番号总数：${summary.missingCodeCount || 0}`,
      `补抓磁力总数：${summary.missingMagnetCount || 0}`,
      ''
    ];

    if (!Array.isArray(missingMagnetEntries) || missingMagnetEntries.length === 0) {
      missingMagnetLines.push('未生成遗漏番号补抓磁力。');
    } else {
      missingMagnetEntries.forEach((entry, index) => {
        const code = normalizeFilmId(entry.code || '');
        const magnets = mergeMagnetEntries(entry.magnets || []);
        missingMagnetLines.push(`${index + 1}. [${code || '未知番号'}]`);
        if (magnets.length === 0) {
          missingMagnetLines.push('   （无可用磁力）');
        } else {
          magnets.forEach((magnet, magnetIndex) => {
            const sizeLabel = magnet.size ? ` [${magnet.size}]` : '';
            missingMagnetLines.push(`   ${magnetIndex + 1})${sizeLabel} ${magnet.link}`);
          });
        }
        missingMagnetLines.push('');
      });
    }

    await writeTextFile(paths.missingMagnetsPath, missingMagnetLines);

    return {
      renameMap: paths.renameMapPath,
      unmatched: paths.unmatchedPath,
      adRiskCodes: paths.adRiskCodesPath,
      adRiskDetail: paths.adRiskDetailPath,
      adRiskMagnets: paths.adRiskMagnetsPath,
      missingMagnets: paths.missingMagnetsPath
    };
  }
  function normalizeFilmDataRecords(parsed) {
    if (Array.isArray(parsed)) {
      return parsed;
    }

    if (!parsed || typeof parsed !== 'object') {
      return [];
    }

    if (Array.isArray(parsed.records)) {
      return parsed.records;
    }

    if (Array.isArray(parsed.filmData)) {
      return parsed.filmData;
    }

    const values = Object.values(parsed);
    if (values.length > 0 && values.every((item) => item && typeof item === 'object' && !Array.isArray(item))) {
      return values;
    }

    return [parsed];
  }

  // This is the artifact-import reader for organizer compatibility flows.
  // It only loads persisted crawl output records; it does not know anything
  // about organizer execution state, AI paths, or UI-specific preload state.
  async function readFilmDataRecords(outputDir) {
    const { outputDir: normalizedOutputDir, filmDataPath } = resolveCrawlOutputPaths(outputDir);

    if (!normalizedOutputDir) {
      throw new Error('请先选择爬虫输出目录。');
    }

    const stat = await fs.promises.stat(filmDataPath).catch(() => null);
    if (!stat || !stat.isFile()) {
      throw new Error(`未找到 filmData.json：${filmDataPath}`);
    }

    let parsed = null;
    try {
      parsed = JSON.parse(await fs.promises.readFile(filmDataPath, 'utf8'));
    } catch (error) {
      throw new Error(`解析 filmData.json 失败：${error instanceof Error ? error.message : String(error)}`);
    }

    const records = normalizeFilmDataRecords(parsed);
    return {
      outputDir: normalizedOutputDir,
      filmDataPath,
      records
    };
  }
  // loadCrawlFilmCodes is the organizer-side snapshot builder.
  // Given one crawl artifact root, it derives the unique-code list plus magnet
  // hints and then packages them into the preloadedExpected shape reused by
  // renderer, Node compatibility paths, and bridge-side organizer runs.
  async function loadCrawlFilmCodes(options = {}) {
    const { outputDir, records, filmDataPath } = await readFilmDataRecords(options.outputDir);
    const { organizerCodesPath } = resolveCrawlOutputPaths(outputDir);

    const codeEntryMap = new Map();
    records.forEach((record) => {
      if (!record || typeof record !== 'object') {
        return;
      }

      const code = extractRecordCode(record);
      if (!code) {
        return;
      }

      const existing = codeEntryMap.get(code) || [];
      const magnets = extractRecordMagnetEntries(record);
      codeEntryMap.set(code, mergeMagnetEntries(existing, magnets));
    });

    const codes = sortCodeAlphabetically(codeEntryMap.keys());
    const codeEntries = buildNormalizedExpectedCodeEntries(codes, codeEntryMap);
    const preloadedExpected = buildExpectedSnapshotPayload(
      {
        sourceType: 'filmData',
        outputDir,
        filmDataPath,
        organizerCodesPath,
        totalRecords: records.length,
        codes,
        codeEntries
      },
      {}
    );

    return {
      outputDir,
      filmDataPath,
      organizerCodesPath,
      sourceType: 'filmData',
      sourcePath: preloadedExpected.sourcePath,
      actressName: '',
      totalRecords: records.length,
      codeCount: codes.length,
      codes,
      codeEntries,
      preloadedExpected
    };
  }

  // runOrganizer remains the legacy Node compatibility executor.
  // The main product direction is still:
  // 1) artifact import / expected-code snapshot
  // 2) local file scan and rename
  // 3) optional AI/intro-ad compatibility branches
  //
  // When debugging organizer behavior, first confirm whether the bug is in:
  // - expectedInput hydration
  // - scan/judge/rename/report phases
  // - optional ad-detection compatibility logic
  // instead of treating this whole file as one undifferentiated path.
  async function runOrganizer(options = {}) {
    // Top-level archived organizer workflow coordinator.
    // This method owns:
    // 1) run-level option normalization
    // 2) expected-code/artifact preload resolution
    // 3) shared phase-context construction
    // 4) phase ordering and final result packaging
    //
    // It should not absorb phase-local business rules that already belong in
    // the dedicated phase modules.
    const normalizedRootPath = normalizeRootPath(options.rootPath);
    if (!normalizedRootPath) {
      throw new Error('请先选择需要整理的根目录。');
    }

    const rootStat = await fs.promises.stat(normalizedRootPath).catch(() => null);
    if (!rootStat || !rootStat.isDirectory()) {
      throw new Error(`根目录不存在：${normalizedRootPath}`);
    }

    const dryRun = Boolean(options.dryRun);
    const includeSubdirectories = options.includeSubdirectories !== false;
    const minSizeMB = toSafeInteger(options.minSizeMB, 100, 1);
    const minSizeBytes = minSizeMB * 1024 * 1024;
    const suffixInput = normalizeSuffixInput(options.suffix);
    const suffixStrategy = parseConflictSuffixStrategy(suffixInput);
    const adFileAction = normalizeAdFileAction(options.adFileAction);
    const strictExpectedCodes = options.strictExpectedCodes !== false;
    // Missing-code detection remains available for review, while magnet
    // generation is opt-in. Keep the boolean normalization at the service
    // boundary so archived callers cannot accidentally enable follow-up
    // downloads with truthy strings such as "false".
    const retryMissingMagnets = options.retryMissingMagnets === true;
    const expectedInput = await resolveExpectedInputForRun(options);
    const expectedCodeSets = buildExpectedCodeSets(expectedInput.expectedCodes);
    const expectedCodeEntryMap = buildExpectedCodeEntryMap(expectedInput.expectedCodeEntries);
    const adDetectionEnabled = options.adDetectionEnabled !== false;
    const adModelType = String(options.adModelType || '').trim() || 'mobile-net-v3-lite';
    const adThreshold = toSafeInteger(options.adThreshold, 60, 1);
    const videoExtensionSet = normalizeVideoExtensions(options.videoExtensions);
    const videoExtensionsText = formatVideoExtensions(videoExtensionSet);
    const evaluateAdRisk = typeof options.evaluateAdRisk === 'function' ? options.evaluateAdRisk : null;
    const onProgress = typeof options.onProgress === 'function' ? options.onProgress : null;
    const paths = resolvePaths(normalizedRootPath);

    if (!dryRun) {
      const ensureTasks = [ensureDirectory(paths.waitingDir), ensureDirectory(paths.introAdDir)];
      if (adFileAction === 'move-to-delete') {
        ensureTasks.push(ensureDirectory(paths.toDeleteDir));
      }
      await Promise.all(ensureTasks);
      await cleanupLegacyReportFiles(normalizedRootPath, options.onLog);
    }

    emitProgress(onProgress, {
      scope: 'organizer',
      phase: 'starting',
      dryRun,
      rootPath: normalizedRootPath,
      minSizeMB,
      adFileAction,
      adModelType,
      retryMissingMagnets
    });

    emitLog(
      options.onLog,
      'info',
      `开始执行整理（根目录=${normalizedRootPath}，最小体积=${minSizeMB}MB，模式=${dryRun ? '预览' : '执行'}，广告处理=${getAdFileActionLabel(adFileAction)}）`
    );

    emitLog(options.onLog, 'info', `视频后缀判定：${videoExtensionsText}`);

    if (expectedCodeSets.codeSet.size > 0) {
      emitLog(
        options.onLog,
        'info',
        `已加载爬虫番号名单：${expectedCodeSets.codeSet.size} 条，严格匹配=${strictExpectedCodes ? '是' : '否'}`
      );
    } else {
      emitLog(options.onLog, 'warn', '未加载爬虫番号名单，将回退为仅按文件名提取番号。');
    }

    if (adDetectionEnabled) {
      if (evaluateAdRisk) {
        emitLog(options.onLog, 'info', `已启用开头广告风险检测，阈值=${adThreshold}`);
      } else {
        emitLog(options.onLog, 'warn', '已启用开头广告风险检测，但当前没有可用的评估服务。');
      }
    }

    const { files } = await runScanPhase({
      collectFiles: (scanRootPath, scanIncludeSubdirectories) =>
        collectFiles(scanRootPath, scanIncludeSubdirectories, videoExtensionSet),
      rootPath: normalizedRootPath,
      includeSubdirectories,
      emitLog,
      emitProgress,
      onLog: options.onLog,
      onProgress,
      summary: null
    });

    emitLog(options.onLog, 'info', `扫描完成，待处理文件 ${files.length} 个。`);

    const summary = {
      scannedTotal: files.length,
      videoTotal: 0,
      nonAdVideo: 0,
      qualifiedVideo: 0,
      matchedToCrawlCode: 0,
      movedToWaiting: 0,
      movedToDelete: 0,
      movedToIntroAd: 0,
      deletedDirectly: 0,
      adFileCount: 0,
      skippedNoCode: 0,
      skippedSmall: 0,
      unmatchedVideo: 0,
      failedOperations: 0,
      adRiskRejected: 0,
      adDetectionErrors: 0,
      supplementMagnetCount: 0,
      expectedCodeTotal: expectedCodeSets.codeSet.size,
      detectedCodeCount: 0,
      missingCodeCount: 0,
      missingMagnetCount: 0,
      removedEmptyDirs: 0
    };

    // Phase 2: 扫描并分类文件，判断哪些是候选视频、哪些命中 crawl 预期番号。
    const phaseJudgeResult = await runJudgePhase({
      fs,
      files,
      minSizeBytes,
      strictExpectedCodes,
      adFileAction,
      expectedCodeSets,
      extractFilmCodeFromFile,
      matchExpectedCodeFallback,
      rootPath: normalizedRootPath,
      path,
      normalizeFilmId,
      shouldReportProgress,
      emitLog,
      emitProgress,
      onLog: options.onLog,
      onProgress,
      summary
    });

    // Phase 3: 执行 rename / move / delete，并在最后输出复盘报告与补抓产物。
    const phaseTargetNames = planTargetNames(phaseJudgeResult.candidates, suffixStrategy);

    const phaseRenameResult = await runRenamePhase({
      fs,
      path,
      dryRun,
      adFileAction,
      paths,
      candidates: phaseJudgeResult.candidates,
      pendingDelete: phaseJudgeResult.pendingDelete,
      targetNames: phaseTargetNames,
      shouldReportProgress,
      moveWithUnique,
      emitLog,
      emitProgress,
      onLog: options.onLog,
      onProgress,
      summary
    });

    const phaseIntroAdResult = await runIntroAdPhase({
      fs,
      path,
      dryRun,
      paths,
      renameRecords: phaseRenameResult.renameRecords,
      adDetectionEnabled,
      adThreshold,
      evaluateAdRisk,
      normalizeFilmId,
      shouldReportProgress,
      moveWithUnique,
      emitLog,
      emitProgress,
      onLog: options.onLog,
      onProgress,
      summary
    });

    const phaseReportResult = await runReportPhase({
      dryRun,
      paths,
      summary,
      expectedCodeSets,
      expectedCodeEntryMap,
      detectedFilmCodes: phaseJudgeResult.detectedFilmCodes,
      retryMissingMagnets,
      adRiskRecords: phaseIntroAdResult.adRiskRecords,
      renameRecords: phaseRenameResult.renameRecords,
      unmatchedRecords: phaseJudgeResult.unmatchedRecords,
      buildSupplementMagnetEntries,
      mergeMagnetEntries,
      normalizeFilmId,
      sortCodeAlphabetically,
      emitLog,
      onLog: options.onLog,
      writeReports
    });

    const compactResult = await compactRootDirectories(normalizedRootPath, paths, adFileAction, {
      dryRun,
      onLog: options.onLog,
      protectedSourcePaths: phaseRenameResult.waitingMoveFailedSources
    });
    emitLog(
      options.onLog,
      'info',
      `根目录收口完成：删除残留目录 ${Number(compactResult.removedDirs || 0)} 个。`
    );

    const cleanupPreservedTopDirs = new Set([
      path.basename(paths.waitingDir),
      path.basename(paths.introAdDir),
      path.basename(paths.logsDir),
      path.basename(paths.stateDir)
    ]);
    if (adFileAction === 'move-to-delete') {
      cleanupPreservedTopDirs.add(path.basename(paths.toDeleteDir));
    }

    const phaseCleanupResult = await runCleanupPhase({
      dryRun,
      rootPath: normalizedRootPath,
      cleanupEmptyDirectories,
      emitLog,
      onLog: options.onLog,
      preservedTopDirs: cleanupPreservedTopDirs
    });
    summary.removedEmptyDirs = Number(phaseCleanupResult.removedEmptyDirs || 0);

    emitLog(
      options.onLog,
      'info',
      `整理完成：待整理=${summary.movedToWaiting}，待删除=${summary.movedToDelete}，含开头广告=${summary.movedToIntroAd}，直接删除=${summary.deletedDirectly}，开头广告命中=${summary.adRiskRejected}，失败=${summary.failedOperations}`
    );

    emitProgress(onProgress, {
      scope: 'organizer',
      phase: 'completed',
      waitingTotal: summary.movedToWaiting,
      waitingProcessed: summary.movedToWaiting,
      deleteTotal: summary.movedToDelete,
      deleteProcessed: summary.movedToDelete,
      introAdTotal: summary.movedToIntroAd,
      adFileAction,
      deletedDirectly: summary.deletedDirectly,
      failedOperations: summary.failedOperations
    });

    return {
      rootPath: normalizedRootPath,
      dryRun,
      config: {
        includeSubdirectories,
        minSizeMB,
        suffixInput: suffixStrategy.raw,
        adFileAction,
        strictExpectedCodes,
        adDetectionEnabled,
        adModelType,
        adThreshold,
        videoExtensions: videoExtensionsText
      },
      expectedCodeCount: expectedCodeSets.codeSet.size,
      summary,
      paths: {
        waitingDir: paths.waitingDir,
        toDeleteDir: paths.toDeleteDir,
        introAdDir: paths.introAdDir,
        logsDir: paths.logsDir,
        reportsDir: paths.rootPath
      },
      reportMap: phaseReportResult.reportMap || {},
      reportFiles: phaseReportResult.reportFiles || [],
      preview: {
        renameRecords: phaseRenameResult.renameRecords.slice(0, 200),
        unmatchedRecords: phaseJudgeResult.unmatchedRecords.slice(0, 200),
        adRiskRecords: phaseIntroAdResult.adRiskRecords.slice(0, 200)
      },
      adRisk: {
        riskCodeCount: phaseReportResult.adRiskCodes.length,
        supplementMagnetCount: summary.supplementMagnetCount,
        riskCodes: phaseReportResult.adRiskCodes.slice(0, 500)
      },
      missingDownload: {
        missingCodeCount: summary.missingCodeCount,
        missingMagnetCount: summary.missingMagnetCount,
        missingCodes: phaseReportResult.missingCodes.slice(0, 500)
      }
    };

  }

  return {
    runOrganizer,
    resolveTargetPath,
    loadCrawlFilmCodes,
    resolveCrawlOutputPaths,
    resolveExpectedInput,
    resolveExpectedInputForRun
  };
}

module.exports = {
  createOrganizerService
};
