// Shared service-layer wording for active desktop ranking/lookup features.
// Keep transport/runtime-neutral service messages here instead of scattering
// them across renderer controllers or archived compatibility layers.
//
// Ownership summary:
// 1) define shared ranking/lookup service-layer wording
// 2) keep transport-neutral service messages out of controllers and facades
// 3) centralize active desktop service copy in one text source

// File map for maintainers:
// 1) ranking service wording
// 2) actress lookup wording
// 3) module/global registration for shared desktop text access

(function registerDesktopServiceText(globalScope) {
  const SERVICE_TEXT = {
    actressRanking: {
      sourceName: 'AVfan',
      browserMissing: '未找到可用的 Chrome / Edge 浏览器，无法拉取参考榜单。',
      latestRankingFallback: '最新榜单',
      parseEmpty: '未从榜单页面解析到有效内容。',
      annualYearMissing: '未找到可用的年度榜单年份。',
      cachedMonthlyFallback: '所选年月暂无稳定在线源，已回退到本地缓存。',
      monthlyHistoryUnavailable: (requestedKey) =>
        `当前公开源暂未提供 ${requestedKey} 的稳定历史月榜，请先使用已缓存月份或最新月榜。`
    },
    actressLookup: {
      missingName: '缺少女优名称，无法填充抓取信息。',
      ambiguousCandidates: (candidates) => `找到多个匹配目录：${candidates.join('、')}`,
      noCandidate: '未找到可用的女优目录。',
      resolveFailed: (lookupErrors) => `未能定位女优目录。${lookupErrors.join('；')}`
    }
  };

  const payload = { SERVICE_TEXT };
  const registry = (globalScope.__desktopTextModules = globalScope.__desktopTextModules || {});
  Object.assign(registry, payload);

  if (typeof module !== 'undefined' && module.exports) {
    module.exports = payload;
  }
})(typeof globalThis !== 'undefined' ? globalThis : window);
