// Legacy organizer compatibility phase for filesystem enumeration.
// deprecated: archived JS organizer phase only; marker=archived-organizer-phase-scan
// This is the first step of the archived JS organizer lane, not the source of
// truth for the current Wails organizer architecture.
//
// Scan phase is the organizer entrypoint for filesystem enumeration.
// Keep it responsible only for collecting/sorting candidate files so later
// phases can reason about a stable input order.
//
// Ownership summary:
// 1) enumerate and sort organizer candidate files in the archived JS lane
// 2) emit initial scan progress/log signals
// 3) keep scan-phase work separate from later judge/rename/report phases
//
// File map for maintainers:
// 1) filesystem enumeration handoff
// 2) deterministic candidate sort
// 3) scan-start progress/log emission
async function runScanPhase(context = {}) {
  const { collectFiles, rootPath, includeSubdirectories, emitLog, emitProgress, onLog, onProgress, summary } = context;

  const files = await collectFiles(rootPath, includeSubdirectories !== false);
  files.sort((left, right) => String(left.path || '').localeCompare(String(right.path || ''), 'en', { sensitivity: 'base' }));

  if (summary && typeof summary === 'object') {
    summary.scannedTotal = files.length;
  }

  emitLog(onLog, 'info', `扫描完成，待处理文件 ${files.length} 个。`);
  emitProgress(onProgress, {
    scope: 'organizer',
    phase: 'scan-start',
    total: files.length,
    processed: 0
  });

  return {
    files
  };
}

module.exports = {
  runScanPhase
};
