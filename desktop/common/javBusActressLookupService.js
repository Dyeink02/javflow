// Lightweight target lookup service used by crawler prefill, ranking handoff,
// and future subscription refresh flows.
//
// Responsibility boundary:
// - resolve actress targets and counts from public pages
// - inspect direct target URLs
// - normalize proxy/base inputs for lookup requests
//
// Out of scope:
// - crawl execution
// - task orchestration
// - Cloudflare/age-check compatibility chains
//
// Ownership summary:
// 1) own lightweight target lookup transport + HTML parsing
// 2) expose actress target/count discovery for prefill-style callers
// 3) stay independent from full crawl runtime/state machinery
//
// File map for maintainers:
// 1) input normalization + proxy helpers
// 2) transport helpers (`fetch` / axios fallback)
// 3) HTML parsing + candidate selection
// 4) public lookup entrypoints (`resolveActressCrawlTarget` / `inspectActressTarget`)
const axios = require('axios');
const cheerio = require('cheerio');
const tunnel = require('tunnel');

const { SERVICE_TEXT } = require('./text/serviceText.js');

const DEFAULT_TIMEOUT_MS = 15000;
const DEFAULT_ITEMS_PER_PAGE = 30;
const DEFAULT_BASE_ORIGINS = [
  'https://www.javbus.com',
  'https://www.busjav.cyou',
  'https://www.fanbus.bond',
  'https://www.cdnbus.bond'
];
const REQUEST_HEADERS = {
  'user-agent':
    'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36',
  'accept-language': 'zh-CN,zh;q=0.9,ja;q=0.8,en;q=0.7'
};

// Input normalization helpers keep caller-specific formatting noise local to
// this file so lookup call sites stay small.
function normalizeName(value) {
  return String(value || '')
    .normalize('NFKC')
    .replace(/\s+/g, '')
    .replace(/[·・•]/g, '')
    .replace(/[()（）]/g, '')
    .toLowerCase();
}

function toOrigin(input) {
  const fallback = 'https://www.javbus.com';
  try {
    return new URL(String(input || fallback)).origin;
  } catch {
    return fallback;
  }
}

function uniqStrings(items = []) {
  return Array.from(
    new Set(
      items
        .map((value) => String(value || '').trim())
        .filter(Boolean)
    )
  );
}

function uniqOrigins(origins = []) {
  return uniqStrings(
    origins.map((value) => {
      try {
        return toOrigin(value);
      } catch {
        return '';
      }
    })
  );
}

function normalizeProxyValue(value) {
  // Proxy normalization is shared for lookup-only traffic. Keep it here so
  // crawler runtime proxy rules do not have to leak into lightweight lookup
  // callers such as prefill, ranking handoff, or future subscription refresh.
  const rawValue = String(value || '').trim();
  if (!rawValue) {
    return '';
  }

  const proxyValue = /^[a-z][a-z0-9+.-]*:\/\//i.test(rawValue)
    ? rawValue
    : /^[^/\s]+:\d+$/.test(rawValue)
      ? `http://${rawValue}`
      : rawValue;

  try {
    const parsed = new URL(proxyValue);
    if (!parsed.hostname) {
      return '';
    }
    return parsed.toString().replace(/\/$/, '');
  } catch {
    return '';
  }
}

// Transport helpers deliberately sit above parsing helpers so lookup callers do
// not care whether `fetch` or axios succeeded. This keeps request-fallback
// policy centralized and makes later replacement easier.
function createProxyAgent(proxyUrl) {
  const parsedProxy = new URL(proxyUrl);
  const port =
    Number.parseInt(parsedProxy.port, 10) || (parsedProxy.protocol === 'https:' ? 443 : 80);
  const proxyOptions = {
    proxy: {
      host: parsedProxy.hostname,
      port
    }
  };

  if (parsedProxy.username || parsedProxy.password) {
    proxyOptions.proxy.proxyAuth = `${decodeURIComponent(parsedProxy.username)}:${decodeURIComponent(parsedProxy.password)}`;
  }

  if (parsedProxy.protocol === 'http:') {
    return tunnel.httpsOverHttp(proxyOptions);
  }
  if (parsedProxy.protocol === 'https:') {
    return tunnel.httpsOverHttps(proxyOptions);
  }

  throw new Error('当前仅支持 HTTP / HTTPS 代理');
}

async function fetchHtmlViaFetch(url) {
  if (typeof fetch !== 'function') {
    throw new Error('fetch 不可用');
  }

  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), DEFAULT_TIMEOUT_MS);

  try {
    const response = await fetch(url, {
      method: 'GET',
      headers: REQUEST_HEADERS,
      redirect: 'follow',
      signal: controller.signal
    });

    if (!response.ok) {
      throw new Error(`HTTP ${response.status}`);
    }

    return await response.text();
  } finally {
    clearTimeout(timer);
  }
}

async function fetchHtmlViaAxios(url, proxyUrl = '') {
  const normalizedProxy = normalizeProxyValue(proxyUrl);
  const requestConfig = {
    headers: REQUEST_HEADERS,
    timeout: DEFAULT_TIMEOUT_MS,
    maxRedirects: 5,
    responseType: 'text',
    transformResponse: [(value) => value],
    validateStatus: (status) => status >= 200 && status < 400
  };

  if (normalizedProxy) {
    const proxyAgent = createProxyAgent(normalizedProxy);
    requestConfig.httpAgent = proxyAgent;
    requestConfig.httpsAgent = proxyAgent;
    requestConfig.proxy = false;
  }

  const response = await axios.get(url, requestConfig);
  return typeof response.data === 'string' ? response.data : String(response.data || '');
}

// Lookup transport prefers the current runtime fetch path, then falls back to
// axios so caller code does not need to care which request engine succeeded.
async function fetchHtml(url, options = {}) {
  const normalizedUrl = String(url || '').trim();
  if (!normalizedUrl) {
    throw new Error('缺少目标地址');
  }

  const normalizedProxy = normalizeProxyValue(options.proxy);
  const attempts = normalizedProxy
    ? [
        () => fetchHtmlViaAxios(normalizedUrl, normalizedProxy),
        () => fetchHtmlViaAxios(normalizedUrl, '')
      ]
    : [
        () => fetchHtmlViaFetch(normalizedUrl),
        () => fetchHtmlViaAxios(normalizedUrl, '')
      ];

  const errors = [];
  for (const attempt of attempts) {
    try {
      return await attempt();
    } catch (error) {
      errors.push(error instanceof Error ? error.message : String(error));
    }
  }

  throw new Error(errors.filter(Boolean).join(' | ') || '请求失败');
}

// Parsing helpers isolate upstream HTML drift to one service boundary.
function parseSearchStarCandidates(html, baseOrigin) {
  const $ = cheerio.load(html);

  return $('a.avatar-box[href*="/star/"]')
    .map((_, element) => {
      const anchor = $(element);
      const href = anchor.attr('href') || '';
      const titleName = anchor.find('img').first().attr('title') || '';
      const rawText = anchor.find('.mleft').first().text() || anchor.text() || '';
      const actressName = String(titleName || rawText)
        .replace(/\s+/g, ' ')
        .replace(/(有码|无码|破解)\s*$/u, '')
        .trim();

      if (!href || !actressName) {
        return null;
      }

      return {
        actressName,
        href: new URL(href, baseOrigin).toString()
      };
    })
    .get()
    .filter(Boolean);
}

function selectBestCandidate(candidates, actressName) {
  // Candidate resolution policy is intentionally deterministic because crawl
  // prefill/subscription handoff should not silently bounce between matches.
  const rawTarget = String(actressName || '').trim();
  const normalizedTarget = normalizeName(actressName);
  const normalizedCandidates = candidates.map((candidate) => ({
    ...candidate,
    normalizedName: normalizeName(candidate.actressName)
  }));

  const rawExactMatches = normalizedCandidates.filter((candidate) => candidate.actressName.trim() === rawTarget);
  if (rawExactMatches.length === 1) {
    return { candidate: rawExactMatches[0], matchMode: 'exact' };
  }
  if (rawExactMatches.length > 1) {
    return { candidate: rawExactMatches[0], matchMode: 'exact-ambiguous' };
  }

  const exactMatches = normalizedCandidates.filter((candidate) => candidate.normalizedName === normalizedTarget);
  if (exactMatches.length === 1) {
    return { candidate: exactMatches[0], matchMode: 'exact' };
  }
  if (exactMatches.length > 1) {
    return { candidate: exactMatches[0], matchMode: 'exact-ambiguous' };
  }

  const containsMatches = normalizedCandidates.filter(
    (candidate) =>
      candidate.normalizedName.includes(normalizedTarget) || normalizedTarget.includes(candidate.normalizedName)
  );
  if (containsMatches.length === 1) {
    return { candidate: containsMatches[0], matchMode: 'contains' };
  }

  if (normalizedCandidates.length === 1) {
    return { candidate: normalizedCandidates[0], matchMode: 'single' };
  }

  return {
    candidate: null,
    matchMode: normalizedCandidates.length > 1 ? 'ambiguous' : 'missing'
  };
}

function parseStarPage(html) {
  // Parsing star pages stays in this service boundary so upstream markup drift
  // for count extraction does not spread into prefill/ranking/subscription code.
  const $ = cheerio.load(html);
  const bodyText = $('body').text().replace(/\s+/g, ' ').trim();
  const countMatch = bodyText.match(/已有磁力\s*(\d+)\s*部[\s\S]*?全部影片\s*(\d+)\s*部/u);
  const itemsPerPage = $('a.movie-box').length || DEFAULT_ITEMS_PER_PAGE;
  const actressName =
    $('.star-box .star-name').first().text().trim() ||
    $('title')
      .first()
      .text()
      .split('-')
      .map((item) => item.trim())
      .filter(Boolean)[0] ||
    '';

  return {
    actressName,
    itemsPerPage,
    magnetCount: countMatch ? Number.parseInt(countMatch[1], 10) : 0,
    allCount: countMatch ? Number.parseInt(countMatch[2], 10) : 0,
    hasMovieGrid: $('a.movie-box').length > 0
  };
}

function getFillCount(starPage) {
  const magnetCount = Number.isFinite(starPage.magnetCount) ? starPage.magnetCount : 0;
  const allCount = Number.isFinite(starPage.allCount) ? starPage.allCount : 0;
  return magnetCount > 0 ? magnetCount : allCount;
}

function buildBaseOrigins(options = {}) {
  const targetUrl = String(options.targetUrl || '').trim();
  const preferredBase = String(options.preferredBase || '').trim();
  const fallbackBases = Array.isArray(options.fallbackBases) ? options.fallbackBases : [];
  return uniqOrigins([
    targetUrl ? toOrigin(targetUrl) : '',
    preferredBase,
    ...fallbackBases,
    ...DEFAULT_BASE_ORIGINS
  ]);
}

function buildTargetCandidates(targetUrl, options = {}) {
  const trimmedUrl = String(targetUrl || '').trim();
  if (!trimmedUrl) {
    return [];
  }

  const candidates = [trimmedUrl];

  try {
    const parsed = new URL(trimmedUrl);
    const pathSegments = parsed.pathname.split('/').filter(Boolean);
    const origins = buildBaseOrigins({ ...options, targetUrl: trimmedUrl });

    if (pathSegments.length >= 2 && String(pathSegments[pathSegments.length - 2]).toLowerCase() === 'star') {
      const slug = pathSegments[pathSegments.length - 1];
      origins.forEach((origin) => {
        candidates.push(`${origin}/star/${slug}`);
      });
    } else if (pathSegments.length > 0) {
      const normalizedPath = pathSegments.join('/');
      origins.forEach((origin) => {
        candidates.push(`${origin}/${normalizedPath}`);
      });
    }
  } catch {
    return uniqStrings(candidates);
  }

  return uniqStrings(candidates);
}

function isUsableStarPage(starPage) {
  return Boolean(starPage.actressName || starPage.hasMovieGrid || starPage.allCount > 0 || starPage.magnetCount > 0);
}

async function fetchStarPage(targetUrl, options = {}) {
  const candidates = buildTargetCandidates(targetUrl, options);
  const errors = [];

  for (const candidateUrl of candidates) {
    try {
      const html = await fetchHtml(candidateUrl, options);
      const starPage = parseStarPage(html);
      if (!isUsableStarPage(starPage)) {
        throw new Error('页面内容无法识别为女优目录');
      }

      return {
        html,
        starPage,
        resolvedUrl: candidateUrl
      };
    } catch (error) {
      errors.push(`${candidateUrl}：${error instanceof Error ? error.message : String(error)}`);
    }
  }

  throw new Error(errors.join('；'));
}

// Public entrypoints either resolve from an actress name or inspect a direct
// target URL. They return normalized payloads that the renderer/bridge can use
// without knowing page structure details.
async function resolveActressCrawlTarget(options = {}) {
  const actressName = String(options.actressName || '').trim();
  if (!actressName) {
    throw new Error(SERVICE_TEXT.actressLookup.missingName);
  }

  const origins = buildBaseOrigins(options);
  const lookupErrors = [];

  for (const origin of origins) {
    const searchStarUrl = `${origin}/searchstar/${encodeURIComponent(actressName)}`;

    try {
      const searchStarHtml = await fetchHtml(searchStarUrl, options);
      const candidates = parseSearchStarCandidates(searchStarHtml, origin);
      const selection = selectBestCandidate(candidates, actressName);

      if (!selection.candidate) {
        if (selection.matchMode === 'ambiguous') {
          throw new Error(
            SERVICE_TEXT.actressLookup.ambiguousCandidates(
              candidates.slice(0, 4).map((candidate) => candidate.actressName)
            )
          );
        }

        throw new Error(SERVICE_TEXT.actressLookup.noCandidate);
      }

      const fetchedStar = await fetchStarPage(selection.candidate.href, {
        ...options,
        preferredBase: origin
      });
      const starPage = fetchedStar.starPage;
      const fillCount = getFillCount(starPage);
      const totalPages =
        fillCount > 0 && starPage.itemsPerPage > 0 ? Math.ceil(fillCount / starPage.itemsPerPage) : 0;

      return {
        actressName,
        resolvedActressName: starPage.actressName || selection.candidate.actressName,
        resolvedBase: fetchedStar.resolvedUrl,
        lookupBaseOrigin: origin,
        matchMode: selection.matchMode,
        candidateCount: candidates.length,
        candidatePreview: candidates.slice(0, 5).map((candidate) => ({
          actressName: candidate.actressName,
          href: candidate.href
        })),
        magnetCount: starPage.magnetCount,
        allCount: starPage.allCount,
        fillCount,
        preferredCount: fillCount,
        itemsPerPage: starPage.itemsPerPage || DEFAULT_ITEMS_PER_PAGE,
        totalPages
      };
    } catch (error) {
      lookupErrors.push(`${origin}：${error instanceof Error ? error.message : String(error)}`);
    }
  }

  throw new Error(SERVICE_TEXT.actressLookup.resolveFailed(lookupErrors));
}

async function inspectActressTarget(options = {}) {
  const targetUrl = String(options.targetUrl || '').trim();
  const actressName = String(options.actressName || '').trim();

  if (!targetUrl) {
    return resolveActressCrawlTarget(options);
  }

  try {
    const fetchedStar = await fetchStarPage(targetUrl, options);
    const starPage = fetchedStar.starPage;
    const fillCount = getFillCount(starPage);
    const itemsPerPage = starPage.itemsPerPage || DEFAULT_ITEMS_PER_PAGE;

    return {
      actressName: actressName || starPage.actressName || '',
      resolvedActressName: starPage.actressName || actressName || '',
      resolvedBase: fetchedStar.resolvedUrl,
      lookupBaseOrigin: toOrigin(fetchedStar.resolvedUrl),
      matchMode: fetchedStar.resolvedUrl.includes('/star/') ? 'direct-url' : 'direct',
      candidateCount: 1,
      candidatePreview: [],
      magnetCount: starPage.magnetCount,
      allCount: starPage.allCount,
      fillCount,
      preferredCount: fillCount,
      itemsPerPage,
      totalPages: fillCount > 0 ? Math.ceil(fillCount / itemsPerPage) : 1
    };
  } catch (directError) {
    if (!actressName) {
      throw directError;
    }

    try {
      return await resolveActressCrawlTarget(options);
    } catch (resolveError) {
      const directMessage = directError instanceof Error ? directError.message : String(directError);
      const resolveMessage = resolveError instanceof Error ? resolveError.message : String(resolveError);
      throw new Error(`${directMessage}；${resolveMessage}`);
    }
  }
}

module.exports = {
  resolveActressCrawlTarget,
  inspectActressTarget
};
