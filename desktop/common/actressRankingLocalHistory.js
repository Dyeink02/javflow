// Local ranking-history loader for the active desktop ranking workflow.
// This file only reads cached ranking artifacts and should not grow online
// fetch or renderer-specific behavior.
//
// Ownership summary:
// 1) read local ranking-history JSON artifacts
// 2) normalize local-history payloads into the shared cache contract
// 3) stay filesystem-only and out of online ranking fetch logic

// File map for maintainers:
// 1) filesystem scan/read helpers
// 2) ranking-entry normalization from single-file or `rankings[]` history inputs
// 3) merge path from local files into the normalized shared cache buckets

const fs = require('fs');
const path = require('path');

const {
  buildCachePayload,
  getMonthKey,
  normalizeYearList
} = require('./actressRankingShared.js');

function listJsonFiles(directoryPath) {
  // History scanning stays file-system only. Online fetch/fallback behavior
  // must remain outside this local-history loader.
  const normalizedPath = String(directoryPath || '').trim();
  if (!normalizedPath || !fs.existsSync(normalizedPath)) {
    return [];
  }

  const queue = [normalizedPath];
  const files = [];

  while (queue.length > 0) {
    const currentPath = queue.shift();
    const entries = fs.readdirSync(currentPath, { withFileTypes: true });

    entries.forEach((entry) => {
      const entryPath = path.join(currentPath, entry.name);
      if (entry.isDirectory()) {
        queue.push(entryPath);
        return;
      }

      if (entry.isFile() && entry.name.toLowerCase().endsWith('.json')) {
        files.push(entryPath);
      }
    });
  }

  return files;
}

function safeReadJson(filePath) {
  try {
    return JSON.parse(fs.readFileSync(filePath, 'utf8'));
  } catch {
    return null;
  }
}

function normalizeRankingItems(items) {
  return (Array.isArray(items) ? items : [])
    .map((item, index) => {
      const actressName = String(item && item.actressName ? item.actressName : '').trim();
      if (!actressName) {
        return null;
      }

      const parsedRank = Number.parseInt(String(item.rank || ''), 10);
      return {
        rank: Number.isFinite(parsedRank) && parsedRank > 0 ? parsedRank : index + 1,
        actressName,
        profileUrl: String(item.profileUrl || '').trim(),
        imageUrl: String(item.imageUrl || '').trim()
      };
    })
    .filter(Boolean);
}

function collectHistoryEntriesFromFile(filePath) {
  // History-file normalization remains local so malformed archives do not leak
  // arbitrary shapes into the aggregate cache contract.
  const payload = safeReadJson(filePath);
  if (!payload || typeof payload !== 'object') {
    return [];
  }

  // Accept both:
  // 1) a history envelope with `rankings: [...]`
  // 2) one ranking record per JSON file
  // This keeps local-history import tolerant to older manual exports and
  // smaller hand-authored ranking-history files.
  const rankings = Array.isArray(payload.rankings) ? payload.rankings : [payload];
  return rankings
    .map((ranking) => {
      const mode = String(ranking.mode || '').trim();
      const periodYear = Number.parseInt(String(ranking.periodYear || ''), 10);
      const periodMonth = Number.parseInt(String(ranking.periodMonth || ''), 10);
      const items = normalizeRankingItems(ranking.items);
      if (!mode || !Number.isFinite(periodYear) || items.length === 0) {
        return null;
      }

      return {
        mode,
        periodYear,
        periodMonth: Number.isFinite(periodMonth) ? periodMonth : null,
        periodLabel: String(ranking.periodLabel || '').trim(),
        sourceName: String(ranking.sourceName || '').trim(),
        sourceChannel: String(ranking.sourceChannel || '').trim(),
        title: String(ranking.title || '').trim(),
        items
      };
    })
    .filter(Boolean);
}

function mergeHistoryDirectoriesIntoCache(historyDirectories = [], currentCache = {}) {
  // Merging local history into cache is the only responsibility of this file.
  // Ranking-source selection and renderer behavior stay in higher layers.
  const directories = Array.isArray(historyDirectories) ? historyDirectories : [];
  const monthlyEntries = new Map();
  const annualEntries = new Map();

  directories.forEach((directoryPath) => {
    listJsonFiles(directoryPath).forEach((filePath) => {
      collectHistoryEntriesFromFile(filePath).forEach((entry) => {
        if (entry.mode === 'annual') {
          annualEntries.set(String(entry.periodYear), entry);
          return;
        }

        const monthKey = getMonthKey(entry.periodYear, entry.periodMonth);
        if (monthKey) {
          monthlyEntries.set(monthKey, entry);
        }
      });
    });
  });

  const mergedCache = buildCachePayload(currentCache);
  mergedCache.local = mergedCache.local || {};
  mergedCache.local.monthly = mergedCache.local.monthly || {};
  mergedCache.local.annual = mergedCache.local.annual || {};

  monthlyEntries.forEach((entry, key) => {
    mergedCache.local.monthly[key] = {
      title: entry.title,
      periodYear: entry.periodYear,
      periodMonth: entry.periodMonth,
      periodLabel: entry.periodLabel || `${entry.periodYear}年${String(entry.periodMonth).padStart(2, '0')}月`,
      sourceName: entry.sourceName || '本地历史',
      sourceChannel: entry.sourceChannel || 'local',
      items: entry.items
    };
  });

  annualEntries.forEach((entry, key) => {
    mergedCache.local.annual[key] = {
      title: entry.title,
      periodYear: entry.periodYear,
      periodMonth: null,
      periodLabel: entry.periodLabel || `${entry.periodYear}年`,
      sourceName: entry.sourceName || '本地历史',
      sourceChannel: entry.sourceChannel || 'local',
      items: entry.items
    };
  });

  mergedCache.local.availableMonths = Object.keys(mergedCache.local.monthly).sort().reverse();
  mergedCache.local.availableYears = normalizeYearList([
    ...Object.keys(mergedCache.local.annual).map((year) => Number.parseInt(year, 10)),
    ...mergedCache.local.availableMonths
      .map((monthKey) => Number.parseInt(String(monthKey).slice(0, 4), 10))
      .filter(Number.isFinite)
  ]);

  return mergedCache;
}

module.exports = {
  mergeHistoryDirectoriesIntoCache
};
