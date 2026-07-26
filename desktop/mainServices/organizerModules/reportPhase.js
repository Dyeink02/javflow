// Legacy organizer compatibility phase for operator-facing report output.
// deprecated: archived JS organizer phase only; marker=archived-organizer-phase-report
// The current product may eventually replace this with a Go-native report
// pipeline, but archived JS organizer runs still rely on these artifacts.
//
// Report phase translates organizer execution results into operator-facing
// artifacts such as rename maps, unmatched summaries, ad-risk lists, and
// supplemental magnet reports for missing or filtered titles.
//
// Ownership summary:
// 1) assemble operator-facing organizer report artifacts in the archived JS lane
// 2) separate ad-risk/missing/unmatched reporting outputs
// 3) keep report shaping distinct from scan/judge/rename execution phases
//
// File map for maintainers:
// 1) ad-risk and missing-code supplemental report shaping
// 2) rename/unmatched report aggregation
// 3) report write/log emission
async function runReportPhase(context = {}) {
  const {
    dryRun,
    paths,
    summary,
    expectedCodeSets,
    expectedCodeEntryMap,
    detectedFilmCodes,
    adRiskRecords,
    renameRecords,
    unmatchedRecords,
    buildSupplementMagnetEntries,
    mergeMagnetEntries,
    normalizeFilmId,
    sortCodeAlphabetically,
    emitLog,
    onLog,
    writeReports
  } = context;

  // Titles filtered into the intro-ad/ad-risk lane still need supplemental
  // magnets and reports, but they should stay separate from truly missing codes.
  const adRiskCodes = sortCodeAlphabetically(
    new Set(
      (Array.isArray(adRiskRecords) ? adRiskRecords : [])
        .map((record) => normalizeFilmId(record && record.filmCode ? record.filmCode : ''))
        .filter(Boolean)
    )
  );
  const adRiskMagnetEntries = buildSupplementMagnetEntries(adRiskCodes, expectedCodeEntryMap);
  summary.supplementMagnetCount = adRiskMagnetEntries.reduce(
    (total, entry) => total + mergeMagnetEntries((entry && entry.magnets) || []).length,
    0
  );

  // Missing codes are titles expected from crawl artifacts but never found in
  // the organizer scan result. This becomes the operator's补抓 list.
  const missingCodes = sortCodeAlphabetically(
    new Set(Array.from((expectedCodeSets && expectedCodeSets.codeSet) || []).filter((code) => !detectedFilmCodes.has(code)))
  );
  const missingMagnetEntries = buildSupplementMagnetEntries(missingCodes, expectedCodeEntryMap);
  summary.missingCodeCount = missingCodes.length;
  summary.missingMagnetCount = missingMagnetEntries.reduce(
    (total, entry) => total + mergeMagnetEntries((entry && entry.magnets) || []).length,
    0
  );

  if (summary.missingCodeCount > 0) {
    emitLog(onLog, 'warn', `发现遗漏番号 ${summary.missingCodeCount} 条，已生成补抓磁力报告（总磁力 ${summary.missingMagnetCount} 条）。`);
  } else {
    emitLog(onLog, 'info', '未发现遗漏番号。');
  }

  let reportMap = {};
  if (!dryRun) {
    reportMap =
      (await writeReports(
        paths,
        summary,
        renameRecords,
        unmatchedRecords,
        adRiskRecords,
        adRiskMagnetEntries,
        missingMagnetEntries
      )) || {};
  }

  return {
    adRiskCodes,
    adRiskMagnetEntries,
    missingCodes,
    missingMagnetEntries,
    reportMap,
    reportFiles: dryRun
      ? []
      : [
          paths.renameMapPath,
          paths.unmatchedPath,
          paths.adRiskCodesPath,
          paths.adRiskDetailPath,
          paths.adRiskMagnetsPath,
          paths.missingMagnetsPath
        ]
  };
}

module.exports = {
  runReportPhase
};
