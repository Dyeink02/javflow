// AVfan ranking fetcher for the active desktop ranking workflow.
// This file is responsible only for scraping/parsing AVfan ranking pages into
// normalized source records for the shared ranking service.

// AVfan ranking source fetcher for the desktop ranking workflow.
// This file owns AVfan-specific navigation/parsing only and should stay separate
// from aggregate source selection or renderer behavior.
//
// Ownership summary:
// 1) fetch AVfan ranking pages
// 2) parse AVfan monthly/annual payloads
// 3) keep AVfan source-specific fallback/year rules local
//
// File map for maintainers:
// 1) AVfan source text/date sanitizers
// 2) AVfan ranking HTML parsing helpers
// 3) AVfan fetch/browser orchestration entrypoints
const cheerio = require('cheerio');

const {
  AVFAN_MONTHLY_URL,
  AVFAN_YEARLY_URL,
  absoluteUrl,
  createRankingError,
  createRankingPage,
  getCurrentJapanYearMonth,
  gotoWithReadyState,
  launchRankingBrowser,
  normalizeYearList
} = require('./actressRankingShared.js');

function stripControlChars(value) {
  return String(value || '')
    .replace(/[\u0000-\u001f\u007f]/g, '')
    .trim();
}

function decodeActressNameFromProfileUrl(profileUrl) {
  try {
    const url = new URL(profileUrl || '', AVFAN_MONTHLY_URL);
    const slug = (url.pathname.split('/').pop() || '').replace(/\.html?$/i, '');
    return stripControlChars(decodeURIComponent(slug));
  } catch {
    return '';
  }
}

function buildRankingTitle(mode, periodYear, periodMonth) {
  if (mode === 'annual') {
    return `${periodYear} AVfan FANZA DVD Actress Annual Ranking`;
  }

  return `${periodYear}.${String(periodMonth).padStart(2, '0')} AVfan FANZA DVD Actress Monthly Ranking`;
}

function buildPeriodLabel(mode, periodYear, periodMonth) {
  if (mode === 'annual') {
    return `${periodYear}年`;
  }

  return `${periodYear}年${String(periodMonth).padStart(2, '0')}月`;
}

function sanitizeRankingPayload(payload) {
  return {
    ...payload,
    title: buildRankingTitle(payload.mode, payload.periodYear, payload.periodMonth),
    periodLabel: buildPeriodLabel(payload.mode, payload.periodYear, payload.periodMonth),
    items: (payload.items || []).map((item) => {
      const decodedName = decodeActressNameFromProfileUrl(item.profileUrl);
      return {
        ...item,
        actressName: decodedName || stripControlChars(item.actressName)
      };
    })
  };
}

function parsePeriodParts(mode, title, fallbackYear) {
  const monthlyMatch = String(title || '').match(/(\d{4})\.(\d{2})/);
  if (monthlyMatch) {
    const year = Number.parseInt(monthlyMatch[1], 10);
    const month = Number.parseInt(monthlyMatch[2], 10);
    return {
      periodYear: year,
      periodMonth: month,
      periodLabel: `${year}年${String(month).padStart(2, '0')}月`
    };
  }

  const yearlyMatch = String(title || '').match(/(\d{4})/);
  if (mode === 'annual' && yearlyMatch) {
    return {
      periodYear: Number.parseInt(yearlyMatch[1], 10),
      periodMonth: null,
      periodLabel: `${yearlyMatch[1]}年`
    };
  }

  return {
    periodYear: fallbackYear,
    periodMonth: mode === 'annual' ? null : getCurrentJapanYearMonth().month,
    periodLabel: mode === 'annual' ? `${fallbackYear}年` : `${fallbackYear}年${String(getCurrentJapanYearMonth().month).padStart(2, '0')}月`
  };
}

function parseAvfanRankingHtml(html, options = {}) {
  // AVfan parsing stays source-local so upstream selector/title changes do not
  // leak into the aggregate ranking service or renderer callers.
  const {
    mode = 'monthly',
    periodYear = getCurrentJapanYearMonth().year
  } = options;

  const $ = cheerio.load(html);
  const pageTitle = $('title').first().text().trim() || '';
  const period = parsePeriodParts(mode, pageTitle, periodYear);

  const itemSelector = mode === 'annual' ? '.c-rankBox li' : '.rankBox li';
  const items = $(itemSelector)
    .map((index, element) => {
      const rankText = $(element).find('.rankNum, .num, .rank').first().text().trim();
      const actressLink = $(element).find('a[href]').first();
      const actressName = stripControlChars(actressLink.text());
      if (!actressName) {
        return null;
      }

      const imageUrl = absoluteUrl($(element).find('img').first().attr('src') || '', AVFAN_MONTHLY_URL);
      const profileUrl = absoluteUrl(actressLink.attr('href') || '', AVFAN_MONTHLY_URL);
      const parsedRank = Number.parseInt(rankText, 10);

      return {
        rank: Number.isFinite(parsedRank) && parsedRank > 0 ? parsedRank : index + 1,
        actressName,
        profileUrl,
        imageUrl
      };
    })
    .get()
    .filter(Boolean);

  if (items.length === 0) {
    throw createRankingError('parse-empty', '未从 AVfan 榜单页面解析到有效内容。');
  }

  return sanitizeRankingPayload(
    createRankingPage({
      mode,
      sourceChannel: 'avfan',
      sourceName: 'AVfan 在线',
      sourceUrl: mode === 'annual' ? AVFAN_YEARLY_URL : AVFAN_MONTHLY_URL,
      title: pageTitle || buildRankingTitle(mode, period.periodYear, period.periodMonth),
      periodYear: period.periodYear,
      periodMonth: period.periodMonth,
      periodLabel: period.periodLabel,
      items
    })
  );
}

async function fetchRankingPage(page, targetUrl) {
  // Navigation/wait rules are shared for AVfan requests within this source
  // file so page timing tweaks stay next to the source-specific parser.
  await gotoWithReadyState(page, targetUrl, {
    waitUntil: 'domcontentloaded',
    timeout: 45000
  });
  await new Promise((resolve) => setTimeout(resolve, 1500));
  return page.content();
}

async function fetchLatestAvfanMonthlyRanking(options = {}) {
  const browser = await launchRankingBrowser(options);
  try {
    const page = await browser.newPage();
    const html = await fetchRankingPage(page, AVFAN_MONTHLY_URL);
    return parseAvfanRankingHtml(html, { mode: 'monthly' });
  } finally {
    await browser.close().catch(() => {});
  }
}

async function fetchAvfanAnnualRanking(options = {}) {
  // Annual fallback/year-rewrite policy remains scoped to AVfan because it is
  // about what this source can return, not about aggregate ranking selection.
  const requestedYear = Number.parseInt(String(options.year || ''), 10);
  const availableYears = normalizeYearList(options.availableYears || []);
  const browser = await launchRankingBrowser(options);

  try {
    const page = await browser.newPage();
    const html = await fetchRankingPage(page, AVFAN_YEARLY_URL);
    const ranking = parseAvfanRankingHtml(html, {
      mode: 'annual',
      periodYear: Number.isFinite(requestedYear) ? requestedYear : getCurrentJapanYearMonth().year
    });

    if (Number.isFinite(requestedYear) && ranking.periodYear !== requestedYear) {
      if (availableYears.length > 0 && !availableYears.includes(requestedYear)) {
        throw createRankingError('year-unavailable', `AVfan 年榜暂未提供 ${requestedYear} 年数据。`);
      }
      ranking.periodYear = requestedYear;
      ranking.periodLabel = `${requestedYear}年`;
      ranking.title = buildRankingTitle('annual', requestedYear, null);
    }

    return ranking;
  } finally {
    await browser.close().catch(() => {});
  }
}

module.exports = {
  fetchLatestAvfanMonthlyRanking,
  fetchAvfanAnnualRanking,
  parseAvfanRankingHtml
};
