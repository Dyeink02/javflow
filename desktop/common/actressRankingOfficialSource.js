// Official DMM/FANZA ranking fetcher for the active desktop ranking workflow.
// Keep official-source parsing isolated here so fallback/caching policy stays
// centralized in the shared ranking service layer.

// Official ranking source fetcher for the desktop ranking workflow.
// This file owns official-channel navigation/parsing only and should not absorb
// aggregate ranking selection or renderer policy.
//
// Ownership summary:
// 1) fetch official ranking pages
// 2) parse official monthly/annual payloads
// 3) keep official-source-specific fallback rules local
//
// File map for maintainers:
// 1) source metadata + age-check helpers
// 2) monthly parser
// 3) browser-driven fetch sequence
const cheerio = require('cheerio');

const {
  DEFAULT_TIMEOUT_MS,
  OFFICIAL_MONTHLY_URL,
  absoluteUrl,
  createRankingError,
  createRankingPage,
  getCurrentJapanYearMonth,
  gotoWithReadyState,
  launchRankingBrowser
} = require('./actressRankingShared.js');

function getOfficialSourceName(requestedChannel) {
  if (requestedChannel === 'dmm') {
    return 'DMM 官方';
  }

  if (requestedChannel === 'fanza') {
    return 'FANZA 官方';
  }

  return 'DMM/FANZA 官方';
}

function buildAgePassUrl(targetUrl) {
  return `https://www.dmm.co.jp/age_check/=/declared=yes/?rurl=${encodeURIComponent(targetUrl)}`;
}

function isAgeCheckPage(pageUrl, html, title) {
  // Official-source age-gate detection stays local to this source so ranking
  // orchestration only receives a clean success/failure contract.
  return (
    String(pageUrl || '').includes('/age_check/') ||
    String(title || '').includes('年齢認証') ||
    String(html || '').includes('/age_check/')
  );
}

async function clickAgeConfirmationIfVisible(page) {
  const selector = 'a[href*="/age_check/=/declared=yes/"]';
  const button = await page.$(selector);
  if (!button) {
    return false;
  }

  await Promise.allSettled([
    page.waitForNavigation({ waitUntil: 'domcontentloaded', timeout: 15000 }),
    button.click()
  ]);
  await new Promise((resolve) => setTimeout(resolve, 1500));
  return true;
}

function parseOfficialMonthlyRankingHtml(html, options = {}) {
  // Official parsing is intentionally limited to current monthly ranking shape.
  // Cross-source fallback logic belongs in the aggregate ranking service.
  const { requestedChannel = 'fanza' } = options;
  const $ = cheerio.load(html);
  const title = $('title').first().text().trim() || '官方女优月榜';
  const current = getCurrentJapanYearMonth();

  const items = $('.area-rank table .bd-b')
    .map((index, element) => {
      const cells = $(element).find('td');
      const rankText = cells.eq(0).text().trim();
      const actressLink = cells.eq(1).find('a[href]').first();
      const actressName = actressLink.text().trim();
      if (!actressName) {
        return null;
      }

      const parsedRank = Number.parseInt(rankText, 10);
      return {
        rank: Number.isFinite(parsedRank) && parsedRank > 0 ? parsedRank : index + 1,
        actressName,
        profileUrl: absoluteUrl(actressLink.attr('href') || '', OFFICIAL_MONTHLY_URL),
        imageUrl: absoluteUrl(cells.eq(1).find('img').first().attr('src') || '', OFFICIAL_MONTHLY_URL)
      };
    })
    .get()
    .filter(Boolean);

  if (items.length === 0) {
    throw createRankingError('parse-empty', '未从官方月榜页面解析到有效内容。');
  }

  return createRankingPage({
    mode: 'monthly',
    sourceChannel: requestedChannel,
    sourceName: getOfficialSourceName(requestedChannel),
    sourceUrl: OFFICIAL_MONTHLY_URL,
    title,
    periodYear: current.year,
    periodMonth: current.month,
    periodLabel: `${current.year}年${String(current.month).padStart(2, '0')}月`,
    items
  });
}

async function fetchOfficialMonthlyActressRanking(options = {}) {
  // Age-pass navigation and official-page fetch sequencing stay here because
  // they are source-specific transport steps, not shared ranking policy.
  const requestedChannel = String(options.requestedChannel || 'fanza').trim().toLowerCase() || 'fanza';
  const browser = await launchRankingBrowser(options);
  try {
    const page = await browser.newPage();
    page.setDefaultNavigationTimeout(DEFAULT_TIMEOUT_MS);
    page.setDefaultTimeout(DEFAULT_TIMEOUT_MS);

    await gotoWithReadyState(page, buildAgePassUrl(OFFICIAL_MONTHLY_URL), {
      waitUntil: 'domcontentloaded',
      timeout: DEFAULT_TIMEOUT_MS
    });
    await new Promise((resolve) => setTimeout(resolve, 1200));

    await gotoWithReadyState(page, OFFICIAL_MONTHLY_URL, {
      waitUntil: 'domcontentloaded',
      timeout: DEFAULT_TIMEOUT_MS
    });
    await new Promise((resolve) => setTimeout(resolve, 1800));

    const html = await page.content();
    const currentUrl = page.url();
    const title = await page.title().catch(() => '');

    if (isAgeCheckPage(currentUrl, html, title)) {
      const clicked = await clickAgeConfirmationIfVisible(page);
      if (!clicked) {
        throw createRankingError('age-check', '官方榜单页面仍停留在年龄确认页。');
      }
    }

    const finalHtml = await page.content();
    const finalUrl = page.url();
    const finalTitle = await page.title().catch(() => '');

    if (isAgeCheckPage(finalUrl, finalHtml, finalTitle)) {
      throw createRankingError('age-check', '官方榜单页面仍停留在年龄确认页。');
    }

    return parseOfficialMonthlyRankingHtml(finalHtml, { requestedChannel });
  } finally {
    await browser.close().catch(() => {});
  }
}

module.exports = {
  fetchOfficialMonthlyActressRanking,
  parseOfficialMonthlyRankingHtml
};
