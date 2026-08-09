// Shared actress-ranking service used by the active desktop ranking workflow.
// It aggregates source fetchers, cache rules, and fallback decisions so the
// renderer only needs to consume normalized ranking payloads.
//
// Maintenance boundary:
// - this file owns ranking-source orchestration and normalized payload shape
// - source-specific scraping stays in `actressRanking*Source.js`
// - cache/history persistence helpers stay in the dedicated shared modules
// - do not mix crawler task lifecycle or subscription policy into this service

// Ownership summary:
// 1) orchestrate source selection and fallback across official/avfan/local
// 2) normalize cache reads/writes and decorate stable ranking payloads
// 3) keep renderer/subscription policy out of ranking source orchestration

// File map for maintainers:
// 1) cache readers/writers:
//    normalized bucket access, monthly/annual availability, cache persistence
// 2) source execution lanes:
//    `getAvfanResult`, `getOfficialResult`, `getLocalResult`
// 3) output shaping:
//    `decorateMonthlyResult`, `decorateAnnualResult`, fallback notices
// 4) public orchestration:
//    `getActressRankings`

const {
  MONTHLY_CACHE_MAX_AGE_MS,
  YEARLY_CACHE_MAX_AGE_MS,
  SOURCE_CHANNELS,
  buildCachePayload,
  createRankingError,
  getCacheSkeleton,
  getChannelLabel,
  getMonthKey,
  isFresh,
  normalizeCache,
  normalizeMonthList,
  normalizeProxy,
  normalizeRankingChannel,
  normalizeYearList,
  readJsonFile,
  writeJsonFile
} = require('./actressRankingShared.js');
const {
  fetchLatestAvfanMonthlyRanking,
  fetchAvfanAnnualRanking
} = require('./actressRankingAvfanSource.js');
const {
  fetchOfficialMonthlyActressRanking
} = require('./actressRankingOfficialSource.js');
const {
  mergeHistoryDirectoriesIntoCache
} = require('./actressRankingLocalHistory.js');

const MESSAGES = {
  localCacheMissing: '\u672c\u5730\u5386\u53f2\u6682\u65e0\u53ef\u7528\u699c\u5355\u7f13\u5b58\uff0c\u8bf7\u5148\u6210\u529f\u6293\u53d6\u4e00\u6b21\u5728\u7ebf\u699c\u5355\u3002',
  localMonthlyMissing: (key) => `\u672c\u5730\u5386\u53f2\u6682\u65e0 ${key} \u7684\u6708\u699c\u7f13\u5b58\u3002`,
  localAnnualMissing: (year) => `\u672c\u5730\u5386\u53f2\u6682\u65e0 ${year}\u5e74 \u7684\u5e74\u699c\u7f13\u5b58\u3002`,
  officialMonthlyOnly: '\u5f53\u524d DMM/FANZA \u5b98\u65b9\u6e20\u9053\u4ec5\u63d0\u4f9b\u5f53\u524d\u6708\u5ea6\u5973\u4f18\u699c\u5355\u3002',
  fallbackTo: (targetName) => `\u5df2\u81ea\u52a8\u5207\u6362\u81f3 ${targetName}\u3002`,
  officialFallbackTo: (targetName) => `\u5b98\u65b9\u6e20\u9053\u6682\u65f6\u4e0d\u53ef\u7528\uff0c\u5df2\u81ea\u52a8\u5207\u6362\u81f3 ${targetName}\u3002`,
  officialAnnualFallbackTo: (targetName) => `\u5b98\u65b9\u6e20\u9053\u6682\u4ec5\u652f\u6301\u6708\u699c\uff0c\u5df2\u81ea\u52a8\u5207\u6362\u81f3 ${targetName}\u3002`,
  requestedMonthFallbackToCache: '\u6240\u9009\u6708\u4efd\u6682\u65f6\u65e0\u7a33\u5b9a\u5728\u7ebf\u6e90\uff0c\u5df2\u56de\u9000\u5230\u672c\u5730\u7f13\u5b58\u3002',
  smartOfficialNotice: '\u667a\u80fd\u6a21\u5f0f\u5c06\u4f18\u5148\u4f7f\u7528\u5b98\u65b9\u5f53\u524d\u6708\u699c\uff0c\u4e0d\u53ef\u7528\u65f6\u81ea\u52a8\u56de\u9000\u3002',
  avfanDirectRetryNotice:
    '\u68c0\u6d4b\u5230\u5f53\u524d\u4ee3\u7406\u65e0\u6cd5\u8bbf\u95ee AVfan\uff0c\u5df2\u81ea\u52a8\u5207\u6362\u4e3a\u76f4\u8fde\u6a21\u5f0f\u7ee7\u7eed\u83b7\u53d6\u699c\u5355\u3002'
};

function getErrorMessage(error) {
  return error instanceof Error ? error.message : String(error);
}

function isProxyConnectionError(error) {
  const message = getErrorMessage(error).toLowerCase();
  return (
    message.includes('err_proxy_connection_failed') ||
    message.includes('proxy connection failed') ||
    message.includes('proxy') ||
    message.includes('tunnel connection failed') ||
    message.includes('socks') ||
    message.includes('econnrefused')
  );
}

function mergeNotice(...parts) {
  return parts
    .map((part) => String(part || '').trim())
    .filter(Boolean)
    .filter((part, index, list) => list.indexOf(part) === index)
    .join(' ');
}

// Cache readers below intentionally operate on normalized cross-source buckets.
// Callers should not need to know whether data came from local history, AVfan,
// or official monthly ranking pages.
function getSourceBucket(cache, bucketId) {
  return cache[bucketId] || getCacheSkeleton()[bucketId];
}

function listMonthlyPeriods(cache, bucketIds) {
  // Period enumeration stays centralized here so monthly availability and
  // fallback logic all work from the same normalized source-bucket view.
  const periods = [];

  bucketIds.forEach((bucketId) => {
    const bucket = getSourceBucket(cache, bucketId);
    Object.entries(bucket.monthly || {}).forEach(([key, entry]) => {
      const match = key.match(/^(\d{4})-(\d{2})$/);
      if (!match || !entry) {
        return;
      }

      periods.push({
        bucketId,
        key,
        year: Number.parseInt(match[1], 10),
        month: Number.parseInt(match[2], 10),
        cachedAt: entry.updatedAt || '',
        entry: {
          cachedAt: entry.updatedAt || '',
          data: entry
        }
      });
    });
  });

  return periods;
}

function listAnnualEntries(cache, bucketIds) {
  // Same rule for annual entries: one normalized enumeration path keeps cache
  // readers deterministic across local/avfan/official buckets.
  const entries = [];

  bucketIds.forEach((bucketId) => {
    const bucket = getSourceBucket(cache, bucketId);
    Object.entries(bucket.annual || {}).forEach(([yearKey, entry]) => {
      const year = Number.parseInt(String(yearKey || '').replace(/[^\d]/g, ''), 10);
      if (!Number.isFinite(year) || !entry) {
        return;
      }

      entries.push({
        bucketId,
        year,
        cachedAt: entry.updatedAt || '',
        entry: {
          cachedAt: entry.updatedAt || '',
          data: entry
        }
      });
    });
  });

  return entries;
}

function getMonthlyAvailability(cache, bucketIds, selectedYear) {
  const periods = listMonthlyPeriods(cache, bucketIds);
  const availableYears = normalizeYearList(periods.map((item) => item.year));
  const numericYear = Number.parseInt(String(selectedYear || ''), 10);
  const effectiveYear = Number.isFinite(numericYear) ? numericYear : availableYears[0];
  const availableMonths = normalizeMonthList(
    periods.filter((item) => item.year === effectiveYear).map((item) => item.month)
  );

  return { availableYears, availableMonths };
}

function getAnnualAvailability(cache, bucketIds) {
  const cachedYears = listAnnualEntries(cache, bucketIds).map((item) => item.year);
  const declaredYears = bucketIds.flatMap((bucketId) => getSourceBucket(cache, bucketId).availableYears || []);
  return normalizeYearList([...cachedYears, ...declaredYears]);
}

function resolveCachedMonthlyEntry(cache, bucketIds, year, month, options = {}) {
  const requestedKey = getMonthKey(year, month);
  const exactMatch = requestedKey
    ? listMonthlyPeriods(cache, bucketIds).find((item) => item.key === requestedKey)
    : null;
  if (exactMatch) {
    return exactMatch;
  }

  if (options.exactOnly && requestedKey) {
    return null;
  }

  const latest = listMonthlyPeriods(cache, bucketIds)
    .sort((left, right) => new Date(right.cachedAt).getTime() - new Date(left.cachedAt).getTime())[0];
  return latest || null;
}

// Smart/local views may combine real periods from every persisted source. A
// source-specific view remains scoped to that source so its period selector
// never claims data that source did not return.
function getAvailabilityBucketIds(requestedChannel, resolvedChannel, bucketIds) {
  if (requestedChannel !== 'smart' && resolvedChannel !== 'local') {
    return bucketIds;
  }
  return Array.from(new Set([...(bucketIds || []), 'official', 'avfan', 'local']));
}

function resolveCachedAnnualEntry(cache, bucketIds, year, options = {}) {
  const requestedYear = Number.parseInt(String(year || ''), 10);
  const annualEntries = listAnnualEntries(cache, bucketIds);

  if (Number.isFinite(requestedYear)) {
    const exact = annualEntries.find((item) => item.year === requestedYear);
    if (exact) {
      return exact;
    }

    if (options.exactOnly) {
      return null;
    }
  }

  return annualEntries.sort((left, right) => right.year - left.year)[0] || null;
}

function persistMonthly(cache, bucketId, data) {
  // Persistence shaping stays in the aggregate service because source fetchers
  // should return normalized payloads, not decide cache bucket write rules.
  const bucket = getSourceBucket(cache, bucketId);
  const key = getMonthKey(data.periodYear, data.periodMonth);
  if (!key) {
    return;
  }

  bucket.monthly[key] = data;
  bucket.availableMonths = normalizeMonthList([...(bucket.availableMonths || []), key]);
  bucket.availableYears = normalizeYearList([
    ...(bucket.availableYears || []),
    Number.parseInt(String(data.periodYear || ''), 10)
  ]);
}

function persistAnnual(cache, bucketId, data) {
  const bucket = getSourceBucket(cache, bucketId);
  const year = Number.parseInt(String(data.periodYear || ''), 10);
  if (!Number.isFinite(year)) {
    return;
  }

  bucket.annual[String(year)] = data;
  bucket.availableYears = normalizeYearList([...(bucket.availableYears || []), ...(data.availableYears || []), year]);
}

// Decoration is the last step before returning data to renderer/UI callers.
// Keep output-shape policy here so source fetchers remain focused on extraction.
function decorateMonthlyResult(params) {
  const {
    cache,
    bucketIds,
    data,
    requestedChannel,
    resolvedChannel,
    fromCache,
    stale,
    notice,
    errorMessage,
    fallbackUsed
  } = params;
  const availability = getMonthlyAvailability(
    cache,
    getAvailabilityBucketIds(requestedChannel, resolvedChannel, bucketIds),
    data.periodYear
  );

  return {
    ...data,
    sourceName:
      resolvedChannel === 'local'
        ? `${getChannelLabel('local')} · ${data.sourceName || getChannelLabel('local')}`
        : data.sourceName,
    originSourceName: data.sourceName,
    mode: 'monthly',
    requestedSource: requestedChannel,
    requestedSourceLabel: getChannelLabel(requestedChannel),
    resolvedSource: resolvedChannel,
    resolvedSourceLabel: getChannelLabel(resolvedChannel),
    availableYears: availability.availableYears,
    availableMonths: availability.availableMonths,
    fromCache: Boolean(fromCache),
    stale: Boolean(stale),
    notice: notice || undefined,
    errorMessage: errorMessage || undefined,
    fallbackUsed: Boolean(fallbackUsed)
  };
}

function decorateAnnualResult(params) {
  const {
    cache,
    bucketIds,
    data,
    requestedChannel,
    resolvedChannel,
    fromCache,
    stale,
    notice,
    errorMessage,
    fallbackUsed
  } = params;

  return {
    ...data,
    sourceName:
      resolvedChannel === 'local'
        ? `${getChannelLabel('local')} · ${data.sourceName || getChannelLabel('local')}`
        : data.sourceName,
    originSourceName: data.sourceName,
    mode: 'annual',
    requestedSource: requestedChannel,
    requestedSourceLabel: getChannelLabel(requestedChannel),
    resolvedSource: resolvedChannel,
    resolvedSourceLabel: getChannelLabel(resolvedChannel),
    availableYears: getAnnualAvailability(cache, bucketIds),
    availableMonths: [],
    fromCache: Boolean(fromCache),
    stale: Boolean(stale),
    notice: notice || undefined,
    errorMessage: errorMessage || undefined,
    fallbackUsed: Boolean(fallbackUsed)
  };
}

async function fetchAvfanWithDirectFallback(fetcher, options = {}) {
  try {
    const data = await fetcher(options);
    return {
      data,
      notice: ''
    };
  } catch (error) {
    const normalizedProxy = normalizeProxy(options.proxy);
    if (!normalizedProxy || !isProxyConnectionError(error)) {
      throw error;
    }

    const data = await fetcher({
      ...options,
      proxy: ''
    });

    return {
      data,
      notice: MESSAGES.avfanDirectRetryNotice
    };
  }
}

async function getAvfanResult(context) {
  const { mode, year, month, forceRefresh, proxy, cache, cacheFilePath, requestedChannel } = context;
  const bucketId = 'avfan';
  const bucket = getSourceBucket(cache, bucketId);
  const requestedMonthKey = getMonthKey(year, month);
  const requestedAnnualYear = Number.parseInt(String(year || ''), 10);

  if (mode === 'monthly') {
    const cached = resolveCachedMonthlyEntry(cache, [bucketId], year, month, {
      exactOnly: Boolean(requestedMonthKey)
    });
    if (!forceRefresh && cached && isFresh(cached.entry, MONTHLY_CACHE_MAX_AGE_MS)) {
      return decorateMonthlyResult({
        cache,
        bucketIds: [bucketId],
        data: cached.entry.data,
        requestedChannel,
        resolvedChannel: 'avfan',
        fromCache: true,
        stale: false
      });
    }

    try {
      const fetchResult = await fetchAvfanWithDirectFallback(fetchLatestAvfanMonthlyRanking, { proxy });
      const data = fetchResult.data;
      persistMonthly(cache, bucketId, data);
      writeJsonFile(cacheFilePath, cache);

      const requestedKey = getMonthKey(year, month);
      const latestKey = getMonthKey(data.periodYear, data.periodMonth);
      if (requestedKey && requestedKey !== latestKey) {
        const requestedCached = bucket.monthly?.[requestedKey];
        if (requestedCached) {
          return decorateMonthlyResult({
            cache,
            bucketIds: [bucketId],
            data: requestedCached,
            requestedChannel,
            resolvedChannel: 'avfan',
            fromCache: true,
            stale: true,
            notice: mergeNotice(MESSAGES.requestedMonthFallbackToCache, fetchResult.notice),
            errorMessage: MESSAGES.requestedMonthFallbackToCache
          });
        }

        throw createRankingError('avfan_month_history_missing', `AVfan 暂未提供 ${requestedKey} 的稳定历史月榜。`);
      }

      return decorateMonthlyResult({
        cache,
        bucketIds: [bucketId],
        data,
        requestedChannel,
        resolvedChannel: 'avfan',
        fromCache: false,
        stale: false,
        notice: fetchResult.notice
      });
    } catch (error) {
      if (cached?.entry?.data) {
        return decorateMonthlyResult({
          cache,
          bucketIds: [bucketId],
          data: cached.entry.data,
          requestedChannel,
          resolvedChannel: 'avfan',
          fromCache: true,
          stale: true,
          notice: isProxyConnectionError(error) ? MESSAGES.avfanDirectRetryNotice : undefined,
          errorMessage: getErrorMessage(error)
        });
      }

      throw error;
    }
  }

  const annualCached = resolveCachedAnnualEntry(cache, [bucketId], year, {
    exactOnly: Number.isFinite(requestedAnnualYear)
  });
  if (!forceRefresh && annualCached && isFresh(annualCached.entry, YEARLY_CACHE_MAX_AGE_MS)) {
    return decorateAnnualResult({
      cache,
      bucketIds: [bucketId],
      data: annualCached.entry.data,
      requestedChannel,
      resolvedChannel: 'avfan',
      fromCache: true,
      stale: false
    });
  }

  try {
    const fetchResult = await fetchAvfanWithDirectFallback(fetchAvfanAnnualRanking, { year, proxy });
    const data = fetchResult.data;
    persistAnnual(cache, bucketId, data);
    writeJsonFile(cacheFilePath, cache);
    return decorateAnnualResult({
      cache,
      bucketIds: [bucketId],
      data,
      requestedChannel,
      resolvedChannel: 'avfan',
      fromCache: false,
      stale: false,
      notice: fetchResult.notice
    });
  } catch (error) {
    if (annualCached?.entry?.data) {
      return decorateAnnualResult({
        cache,
        bucketIds: [bucketId],
        data: annualCached.entry.data,
        requestedChannel,
        resolvedChannel: 'avfan',
        fromCache: true,
        stale: true,
        notice: isProxyConnectionError(error) ? MESSAGES.avfanDirectRetryNotice : undefined,
        errorMessage: getErrorMessage(error)
      });
    }

    throw error;
  }
}

async function getOfficialResult(context) {
  const { mode, year, month, forceRefresh, proxy, cache, cacheFilePath, requestedChannel } = context;
  const bucketId = 'official';
  const effectiveRequestedChannel = requestedChannel === 'smart' ? 'fanza' : requestedChannel;
  const cached = resolveCachedMonthlyEntry(cache, [bucketId], year, month, {
    exactOnly: Boolean(getMonthKey(year, month))
  });

  if (mode !== 'monthly') {
    throw createRankingError('official_annual_unsupported', MESSAGES.officialMonthlyOnly);
  }

  if (!forceRefresh && cached && isFresh(cached.entry, MONTHLY_CACHE_MAX_AGE_MS)) {
    return decorateMonthlyResult({
      cache,
      bucketIds: [bucketId],
      data: cached.entry.data,
      requestedChannel,
      resolvedChannel: effectiveRequestedChannel,
      fromCache: true,
      stale: false
    });
  }

  try {
    const data = await fetchOfficialMonthlyActressRanking({
      proxy,
      requestedChannel: effectiveRequestedChannel
    });
    persistMonthly(cache, bucketId, data);
    writeJsonFile(cacheFilePath, cache);
    return decorateMonthlyResult({
      cache,
      bucketIds: [bucketId],
      data,
      requestedChannel,
      resolvedChannel: effectiveRequestedChannel,
      fromCache: false,
      stale: false
    });
  } catch (error) {
    if (cached?.entry?.data) {
      return decorateMonthlyResult({
        cache,
        bucketIds: [bucketId],
        data: cached.entry.data,
        requestedChannel,
        resolvedChannel: effectiveRequestedChannel,
        fromCache: true,
        stale: true,
        errorMessage: error instanceof Error ? error.message : String(error)
      });
    }

    throw error;
  }
}

function getLocalResult(context) {
  const { mode, year, month, cache, requestedChannel } = context;
  const monthlyBuckets = ['local', 'official', 'avfan'];
  const annualBuckets = ['local', 'avfan'];

  if (mode === 'monthly') {
    const requestedKey = getMonthKey(year, month);
    const cached =
      resolveCachedMonthlyEntry(cache, monthlyBuckets, year, month, {
        exactOnly: Boolean(requestedKey)
      }) || (requestedKey ? null : resolveCachedMonthlyEntry(cache, monthlyBuckets, year, month));

    if (!cached?.entry?.data) {
      if (requestedKey) {
        throw createRankingError('local_month_missing', MESSAGES.localMonthlyMissing(requestedKey));
      }
      throw createRankingError('local_cache_missing', MESSAGES.localCacheMissing);
    }

    return decorateMonthlyResult({
      cache,
      bucketIds: monthlyBuckets,
      data: cached.entry.data,
      requestedChannel,
      resolvedChannel: 'local',
      fromCache: true,
      stale: true,
      notice:
        cached.bucketId === 'local'
          ? '本地历史当前优先展示你手动写入或导入的榜单。'
          : cached.bucketId === 'official'
            ? '本地历史当前优先展示最近一次官方月榜缓存。'
            : '本地历史当前优先展示最近一次 AVfan 缓存。'
    });
  }

  const requestedYear = Number.parseInt(String(year || ''), 10);
  const cached =
    resolveCachedAnnualEntry(cache, annualBuckets, year, {
      exactOnly: Number.isFinite(requestedYear)
    }) || (Number.isFinite(requestedYear) ? null : resolveCachedAnnualEntry(cache, annualBuckets, year));

  if (!cached?.entry?.data) {
    if (Number.isFinite(requestedYear)) {
      throw createRankingError('local_annual_missing', MESSAGES.localAnnualMissing(requestedYear));
    }
    throw createRankingError('local_cache_missing', MESSAGES.localCacheMissing);
  }

  return decorateAnnualResult({
    cache,
    bucketIds: annualBuckets,
    data: cached.entry.data,
    requestedChannel,
    resolvedChannel: 'local',
    fromCache: true,
    stale: true,
    notice:
      cached.bucketId === 'local'
        ? '本次优先展示你手动写入或导入的本地历史榜单。'
        : '\u672c\u6b21\u4ec5\u4f7f\u7528\u672c\u5730\u5386\u53f2\u699c\u5355\u7f13\u5b58\u3002'
  });
}

function buildSourcePlan(requestedChannel, mode) {
  if (requestedChannel === 'local') {
    return ['local'];
  }

  if (requestedChannel === 'avfan') {
    return ['avfan', 'local'];
  }

  if (requestedChannel === 'fanza' || requestedChannel === 'dmm') {
    if (mode === 'annual') {
      return ['avfan', 'local'];
    }

    return ['official', 'avfan', 'local'];
  }

  return mode === 'monthly' ? ['official', 'avfan', 'local'] : ['avfan', 'local'];
}

function enrichFallbackNotice(requestedChannel, attemptedSource, result) {
  if (requestedChannel === 'smart' && attemptedSource === 'official') {
    return mergeNotice(MESSAGES.officialFallbackTo(result.resolvedSourceLabel || result.sourceName), result.notice);
  }

  if ((requestedChannel === 'fanza' || requestedChannel === 'dmm') && attemptedSource === 'official') {
    return mergeNotice(MESSAGES.officialFallbackTo(result.resolvedSourceLabel || result.sourceName), result.notice);
  }

  if ((requestedChannel === 'fanza' || requestedChannel === 'dmm') && result.mode === 'annual') {
    return mergeNotice(MESSAGES.officialAnnualFallbackTo(result.resolvedSourceLabel || result.sourceName), result.notice);
  }

  return mergeNotice(MESSAGES.fallbackTo(result.resolvedSourceLabel || result.sourceName), result.notice);
}

async function trySource(sourceId, context) {
  if (sourceId === 'official') {
    return getOfficialResult(context);
  }

  if (sourceId === 'avfan') {
    return getAvfanResult(context);
  }

  return getLocalResult(context);
}

async function getActressRankings(options = {}) {
  // This is the single orchestration entry for ranking source selection,
  // fallback, cache refresh, and result decoration. Keep renderer or
  // subscription-specific policy out of this service.
  const requestedChannel = normalizeRankingChannel(options.source);
  const mode = options.mode === 'annual' ? 'annual' : 'monthly';
  const year = options.year;
  const month = options.month;
  const forceRefresh = Boolean(options.forceRefresh);
  const proxy = normalizeProxy(options.proxy);
  const cacheFilePath = options.cacheFilePath;
  const cache = normalizeCache(readJsonFile(cacheFilePath));
  mergeHistoryDirectoriesIntoCache(options.historyDirectories || [], cache);

  const context = {
    requestedChannel,
    mode,
    year,
    month,
    forceRefresh,
    proxy,
    cache,
    cacheFilePath
  };

  const sourcePlan = buildSourcePlan(requestedChannel, mode);
  const failures = [];

  for (const sourceId of sourcePlan) {
    try {
      const result = await trySource(sourceId, context);
      if (failures.length > 0) {
        result.notice = enrichFallbackNotice(requestedChannel, failures[0].sourceId, result);
        result.fallbackUsed = true;
      } else if ((requestedChannel === 'fanza' || requestedChannel === 'dmm') && mode === 'annual') {
        result.notice = mergeNotice(
          MESSAGES.officialAnnualFallbackTo(result.resolvedSourceLabel || result.sourceName),
          result.notice
        );
        result.fallbackUsed = true;
      } else if (requestedChannel === 'smart' && sourceId === 'official' && !result.notice) {
        result.notice = MESSAGES.smartOfficialNotice;
      }
      return result;
    } catch (error) {
      failures.push({
        sourceId,
        message: error instanceof Error ? error.message : String(error)
      });
    }
  }

  const detail = failures.map((item) => `${SOURCE_CHANNELS[item.sourceId]?.label || item.sourceId}: ${item.message}`);
  throw createRankingError('ranking_all_sources_failed', detail.join(' | '));
}

module.exports = {
  getActressRankings,
  __private__: {
    buildSourcePlan,
    normalizeCache,
    resolveCachedMonthlyEntry,
    resolveCachedAnnualEntry,
    decorateMonthlyResult,
    decorateAnnualResult
  }
};

