// Shared sidecar facade for actress ranking / target-resolution compatibility.
// deprecated: archived sidecar facade only; marker=archived-sidecar-ranking-facade
// Keep ranking cache-path fallback and target lookup routing here so the
// sidecar command router does not grow knowledge of ranking storage details.
//
// Ownership summary:
// 1) adapt ranking queries and target-resolution calls for the sidecar lane
// 2) localize ranking cache/history fallback paths for compatibility usage
// 3) keep ranking compatibility details out of the sidecar command router
//
// Current maintenance rule:
// - the active Wails app should prefer the Go lookup bridge
// - this archived sidecar lane is disabled by default unless the archived
//   sidecar compatibility gate is enabled explicitly
//
// File map for maintainers:
// 1) compatibility cache/history path defaults
// 2) ranking query passthrough
// 3) target lookup passthrough

const path = require('path');

const { getActressRankings } = require('../../common/actressRankingService.js');
const {
  resolveActressCrawlTarget,
  inspectActressTarget
} = require('../../common/javBusActressLookupService.js');
const { getRuntimeContext } = require('../runtimePaths.js');

function createRankingFacade() {
  // Fast troubleshooting split:
  // 1) archived sidecar ranking cache/history default path issue -> inspect
  //    this facade first
  // 2) ranking parsing / actress lookup / target-resolution issue -> inspect
  //    the shared common services or the Go lookup bridge
  function resolveRankingPayload(payload = {}) {
    // Ranking compatibility keeps its cache-path defaults local to this facade
    // so command routing and other sidecar services do not need to know where
    // ranking history/cache files live.
    const runtimeContext = getRuntimeContext();
    return {
      ...payload,
      cacheFilePath:
        payload.cacheFilePath || path.join(runtimeContext.userData, 'actress-ranking-cache.json'),
      historyDirectories: Array.isArray(payload.historyDirectories)
        ? payload.historyDirectories
        : [path.join(runtimeContext.userData, 'ranking-history')]
    };
  }

  return {
    getRankings(payload = {}) {
      return getActressRankings(resolveRankingPayload(payload));
    },
    resolveTarget(payload = {}) {
      // Target resolution stays pass-through on purpose. Any future ranking
      // policy belongs in the shared lookup service or Go bridge, not here.
      return resolveActressCrawlTarget(payload);
    },
    inspectTarget(payload = {}) {
      return inspectActressTarget(payload);
    }
  };
}

module.exports = {
  createRankingFacade
};
