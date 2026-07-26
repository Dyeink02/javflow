// Shared ranking-source utilities for the active desktop ranking workflow.
// Browser launch, cache shape, channel metadata, and normalization helpers live
// here so the source fetchers and aggregate service share one contract.
//
// Ownership summary:
// 1) own shared ranking cache/channel/browser helpers
// 2) provide normalization/error helpers used by ranking source fetchers
// 3) keep ranking transport/runtime details out of renderer controllers

// File map for maintainers:
// 1) static contracts:
//    channel metadata, cache constants, browser path candidates
// 2) normalization helpers:
//    proxy/channel/year/month/cache normalization
// 3) shared persistence/runtime helpers:
//    read/write cache files, browser launch, ready-state navigation
// 4) source payload helpers:
//    `createRankingError`, `createRankingPage`, URL normalization

const fs = require('fs');
const path = require('path');
const puppeteer = require('puppeteer-core');

const AVFAN_MONTHLY_URL = 'https://av-fan.tokyo/ranking/fanza-dvd-actress-monthly.php';
const AVFAN_YEARLY_URL = 'https://av-fan.tokyo/ranking/fanza-rental-dvd-actress-top100.php';
const OFFICIAL_MONTHLY_URL = 'https://www.dmm.co.jp/mono/dvd/-/ranking/=/mode=actress/term=monthly/';

const MONTHLY_CACHE_MAX_AGE_MS = 12 * 60 * 60 * 1000;
const YEARLY_CACHE_MAX_AGE_MS = 7 * 24 * 60 * 60 * 1000;
const DEFAULT_TIMEOUT_MS = 45000;
const CACHE_VERSION = 2;

const BROWSER_PATHS = [
  'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
  'C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe',
  'C:\\Program Files\\Microsoft\\Edge\\Application\\msedge.exe',
  'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe',
  'C:\\Users\\%USERNAME%\\AppData\\Local\\Google\\Chrome\\Application\\chrome.exe',
  'C:\\Users\\%USERNAME%\\AppData\\Local\\Microsoft\\Edge\\Application\\msedge.exe'
];

const SOURCE_CHANNELS = Object.freeze({
  smart: {
    id: 'smart',
    label: '智能推荐',
    cacheBucket: 'smart'
  },
  fanza: {
    id: 'fanza',
    label: 'FANZA 官方',
    cacheBucket: 'official'
  },
  dmm: {
    id: 'dmm',
    label: 'DMM 官方',
    cacheBucket: 'official'
  },
  avfan: {
    id: 'avfan',
    label: 'AVfan 在线',
    cacheBucket: 'avfan'
  },
  local: {
    id: 'local',
    label: '本地历史',
    cacheBucket: 'local'
  }
});

function expandWindowsPath(filePath) {
  return String(filePath || '').replace('%USERNAME%', process.env.USERNAME || '');
}

function getBrowserExecutablePath() {
  // Ranking browser discovery remains local to the ranking workflow. If future
  // product code needs broader browser-selection policy, that should move to a
  // dedicated shared runtime contract rather than grow here ad hoc.
  for (const candidate of BROWSER_PATHS) {
    const resolvedPath = expandWindowsPath(candidate);
    if (fs.existsSync(resolvedPath)) {
      return resolvedPath;
    }
  }

  throw new Error('未找到可用的 Chrome / Edge 浏览器。');
}

function ensureParentDir(filePath) {
  fs.mkdirSync(path.dirname(filePath), { recursive: true });
}

function readJsonFile(filePath) {
  try {
    if (!fs.existsSync(filePath)) {
      return null;
    }

    return JSON.parse(fs.readFileSync(filePath, 'utf8'));
  } catch {
    return null;
  }
}

function writeJsonFile(filePath, payload) {
  ensureParentDir(filePath);
  fs.writeFileSync(filePath, JSON.stringify(payload, null, 2), 'utf8');
}

function normalizeProxy(proxyValue) {
  const rawValue = String(proxyValue || '').trim();
  if (!rawValue) {
    return '';
  }

  if (/^[a-z][a-z0-9+.-]*:\/\//i.test(rawValue)) {
    return rawValue;
  }

  if (/^[^/\s]+:\d+$/.test(rawValue)) {
    return `http://${rawValue}`;
  }

  return rawValue;
}

function normalizeRankingChannel(channelValue) {
  const channel = String(channelValue || '').trim().toLowerCase();
  if (channel in SOURCE_CHANNELS) {
    return channel;
  }
  return 'smart';
}

function getChannelLabel(channelValue) {
  const channel = normalizeRankingChannel(channelValue);
  return SOURCE_CHANNELS[channel].label;
}

function normalizeYearList(values) {
  const years = Array.isArray(values) ? values : [];
  return Array.from(
    new Set(
      years
        .map((value) => Number.parseInt(String(value || '').trim(), 10))
        .filter((value) => Number.isFinite(value) && value >= 2000 && value <= 2100)
    )
  ).sort((left, right) => right - left);
}

function normalizeMonthList(values) {
  const months = Array.isArray(values) ? values : [];
  return Array.from(
    new Set(
      months
        .map((value) => String(value || '').trim())
        .filter((value) => /^\d{4}-\d{2}$/.test(value))
    )
  ).sort().reverse();
}

function getMonthKey(year, month) {
  const parsedYear = Number.parseInt(String(year || '').trim(), 10);
  const parsedMonth = Number.parseInt(String(month || '').trim(), 10);
  if (!Number.isFinite(parsedYear) || !Number.isFinite(parsedMonth) || parsedMonth < 1 || parsedMonth > 12) {
    return '';
  }
  return `${parsedYear}-${String(parsedMonth).padStart(2, '0')}`;
}

function getCurrentJapanYearMonth() {
  const formatter = new Intl.DateTimeFormat('en-CA', {
    timeZone: 'Asia/Tokyo',
    year: 'numeric',
    month: '2-digit'
  });
  const parts = formatter.formatToParts(new Date());
  const year = Number.parseInt(parts.find((item) => item.type === 'year').value, 10);
  const month = Number.parseInt(parts.find((item) => item.type === 'month').value, 10);
  return { year, month };
}

function createRankingError(code, message, details = null) {
  const error = new Error(message);
  error.code = code;
  error.details = details;
  return error;
}

function absoluteUrl(maybeUrl, baseUrl) {
  const value = String(maybeUrl || '').trim();
  if (!value) {
    return '';
  }

  try {
    return new URL(value, baseUrl).toString();
  } catch {
    return value;
  }
}

function buildCachePayload(existingCache = {}) {
  // Cache payload shaping stays centralized here so source fetchers and the
  // aggregate ranking service never drift in bucket structure/versioning.
  return {
    version: CACHE_VERSION,
    updatedAt: new Date().toISOString(),
    smart: existingCache.smart || {},
    official: existingCache.official || {},
    avfan: existingCache.avfan || {},
    local: existingCache.local || {}
  };
}

function getCacheSkeleton() {
  return buildCachePayload({
    smart: { monthly: {}, annual: {}, availableMonths: [], availableYears: [] },
    official: { monthly: {}, annual: {}, availableMonths: [], availableYears: [] },
    avfan: { monthly: {}, annual: {}, availableMonths: [], availableYears: [] },
    local: { monthly: {}, annual: {}, availableMonths: [], availableYears: [] }
  });
}

function normalizeCache(cache) {
  // Normalization belongs at this shared layer, not in individual fetchers, so
  // monthly/annual/history call sites all consume the same cache contract.
  const normalized = buildCachePayload(cache || {});
  for (const bucket of ['smart', 'official', 'avfan', 'local']) {
    normalized[bucket] = normalized[bucket] || {};
    normalized[bucket].monthly = normalized[bucket].monthly || {};
    normalized[bucket].annual = normalized[bucket].annual || {};
    normalized[bucket].availableMonths = normalizeMonthList(normalized[bucket].availableMonths || Object.keys(normalized[bucket].monthly));
    normalized[bucket].availableYears = normalizeYearList(
      normalized[bucket].availableYears || [
        ...Object.keys(normalized[bucket].annual).map((value) => Number.parseInt(value, 10)),
        ...normalized[bucket].availableMonths.map((value) => Number.parseInt(String(value).slice(0, 4), 10))
      ]
    );
  }
  return normalized;
}

function isFresh(updatedAt, maxAgeMs) {
  const parsed = Date.parse(String(updatedAt || '').trim());
  if (!Number.isFinite(parsed)) {
    return false;
  }
  return Date.now() - parsed <= maxAgeMs;
}

function createRankingPage(payload = {}) {
  return {
    mode: payload.mode || 'monthly',
    sourceChannel: payload.sourceChannel || 'smart',
    sourceName: payload.sourceName || getChannelLabel(payload.sourceChannel),
    sourceUrl: payload.sourceUrl || '',
    title: payload.title || '',
    periodYear: payload.periodYear || null,
    periodMonth: payload.periodMonth || null,
    periodLabel: payload.periodLabel || '',
    items: Array.isArray(payload.items) ? payload.items : [],
    updatedAt: new Date().toISOString()
  };
}

async function launchRankingBrowser(options = {}) {
  // Browser launch is intentionally isolated here because ranking fetchers need
  // one shared anti-drift launch profile, not source-specific browser policy.
  const browserPath = options.browserPath || getBrowserExecutablePath();
  return puppeteer.launch({
    executablePath: browserPath,
    headless: options.headless !== false ? 'new' : false,
    args: [
      '--disable-blink-features=AutomationControlled',
      '--no-default-browser-check',
      '--disable-dev-shm-usage',
      '--disable-features=Translate,BackForwardCache',
      '--lang=ja-JP'
    ],
    defaultViewport: {
      width: 1440,
      height: 960
    }
  });
}

async function gotoWithReadyState(page, targetUrl, options = {}) {
  await page.goto(targetUrl, options);
  await page.waitForFunction(() => document.readyState === 'complete', {
    timeout: DEFAULT_TIMEOUT_MS
  }).catch(() => {});
}

module.exports = {
  AVFAN_MONTHLY_URL,
  AVFAN_YEARLY_URL,
  OFFICIAL_MONTHLY_URL,
  MONTHLY_CACHE_MAX_AGE_MS,
  YEARLY_CACHE_MAX_AGE_MS,
  DEFAULT_TIMEOUT_MS,
  CACHE_VERSION,
  SOURCE_CHANNELS,
  absoluteUrl,
  buildCachePayload,
  createRankingError,
  createRankingPage,
  expandWindowsPath,
  getBrowserExecutablePath,
  getCacheSkeleton,
  getChannelLabel,
  getCurrentJapanYearMonth,
  getMonthKey,
  gotoWithReadyState,
  isFresh,
  launchRankingBrowser,
  normalizeCache,
  normalizeMonthList,
  normalizeProxy,
  normalizeRankingChannel,
  normalizeYearList,
  readJsonFile,
  writeJsonFile
};
