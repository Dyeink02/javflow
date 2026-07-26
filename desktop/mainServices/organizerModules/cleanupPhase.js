// Legacy organizer compatibility phase: cleanup runs last and stays narrow.
// deprecated: archived JS organizer phase only; marker=archived-organizer-phase-cleanup
// Current Wails organizer work should prefer the Go organizer service path;
// this helper remains only so the archived JS organizer flow can finish with a
// deterministic empty-directory sweep.
//
// Cleanup phase is intentionally last and intentionally narrow: only remove
// empty directories that remain after organizer moves/deletes are finished.
//
// Ownership summary:
// 1) perform the archived JS organizer's final empty-directory cleanup
// 2) keep cleanup narrow and deterministic after move/delete work
// 3) separate cleanup behavior from earlier organizer phases
//
// File map for maintainers:
// 1) dry-run guard
// 2) empty-directory cleanup handoff
// 3) cleanup summary/log emission
async function runCleanupPhase(context = {}) {
  const { dryRun, rootPath, cleanupEmptyDirectories, emitLog, onLog, preservedTopDirs } = context;

  if (dryRun) {
    return {
      removedEmptyDirs: 0
    };
  }

  const removedEmptyDirs = await cleanupEmptyDirectories(rootPath, {
    onLog,
    preservedTopDirs: preservedTopDirs instanceof Set ? preservedTopDirs : new Set()
  });
  const count = Array.isArray(removedEmptyDirs) ? removedEmptyDirs.length : 0;

  emitLog(onLog, 'info', `空目录清理完成：${count} 个。`);

  return {
    removedEmptyDirs: count
  };
}

module.exports = {
  runCleanupPhase
};
