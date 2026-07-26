// Legacy organizer compatibility phase for rename/move execution.
// deprecated: archived JS organizer phase only; marker=archived-organizer-phase-rename
// This phase still matters for archived JS organizer runs, but current product
// organizer ownership should continue moving toward the Go service path.
//
// Ownership summary:
// 1) plan waiting/delete destinations for classified records
// 2) execute bounded-concurrency move/delete work for the archived JS lane
// 3) preserve move-safety checks and rename records for later report/review use
//
// File map for maintainers:
// 1) path-safety and managed-root helpers
// 2) bounded-concurrency worker helpers
// 3) rename/move/delete execution pipeline

function toPositiveInt(value, fallback) {
  const parsed = Number.parseInt(String(value ?? '').trim(), 10);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    return fallback;
  }
  return parsed;
}

// Normalize before path comparisons so move/delete safety checks do not depend
// on whether callers passed relative or absolute paths.
function normalizeAbsolutePath(path, sourcePath) {
  return path.resolve(String(sourcePath || '').trim());
}

function isPathInside(path, parentPath, targetPath) {
  const parent = normalizeAbsolutePath(path, parentPath);
  const target = normalizeAbsolutePath(path, targetPath);
  const relativePath = path.relative(parent, target);
  if (!relativePath) {
    return true;
  }
  return !relativePath.startsWith('..') && !path.isAbsolute(relativePath);
}

// Managed-root children are organizer-owned directories that should not be fed
// back into later delete passes as if they were original source content.
function isManagedRootChild(paths, path, directoryPath) {
  const rootPath = normalizeAbsolutePath(path, paths.rootPath);
  const targetPath = normalizeAbsolutePath(path, directoryPath);
  if (!isPathInside(path, rootPath, targetPath)) {
    return true;
  }

  const relativePath = path.relative(rootPath, targetPath);
  const topDirName = relativePath.split(path.sep).filter(Boolean)[0] || '';
  if (!topDirName) {
    return true;
  }

  const managedTopDirs = new Set([
    path.basename(paths.waitingDir),
    path.basename(paths.introAdDir),
    path.basename(paths.toDeleteDir),
    'logs',
    '.video-organizer-state'
  ]);

  return managedTopDirs.has(topDirName);
}

// Shared bounded-concurrency helper for delete/move subwork inside the rename
// phase so file-system pressure stays predictable.
async function runWithConcurrency(items, concurrency, worker) {
  const list = Array.isArray(items) ? items : [];
  if (list.length === 0) {
    return;
  }

  const workerCount = Math.max(1, Math.min(concurrency, list.length));
  let cursor = 0;

  async function consume() {
    while (true) {
      const currentIndex = cursor;
      cursor += 1;
      if (currentIndex >= list.length) {
        return;
      }
      // eslint-disable-next-line no-await-in-loop
      await worker(list[currentIndex], currentIndex);
    }
  }

  await Promise.all(Array.from({ length: workerCount }, () => consume()));
}

// Preserve relative source structure when moving ad/trash files into the
// waiting-delete area, but degrade safely to basename if the source path is not
// inside the managed root.
function resolveDeleteDestinationPath(paths, path, sourcePath) {
  const rootPath = String((paths && paths.rootPath) || '').trim();
  const normalizedSource = String(sourcePath || '').trim();
  if (!rootPath || !normalizedSource) {
    return path.join(paths.toDeleteDir, path.basename(normalizedSource));
  }

  const relativePath = path.relative(rootPath, normalizedSource);
  const isSafeRelative =
    relativePath &&
    !relativePath.startsWith('..') &&
    !path.isAbsolute(relativePath) &&
    !relativePath.includes(`..${path.sep}`);

  if (!isSafeRelative) {
    return path.join(paths.toDeleteDir, path.basename(normalizedSource));
  }

  return path.join(paths.toDeleteDir, relativePath);
}

// Rename phase owns all file movement after classification:
// 1) move valid candidates into waiting-area with final names
// 2) process pending-delete items using the configured ad-file action
// 3) keep delete safety rules local so judge phase stays classification-only
// Legacy organizer compatibility phase for rename/move into waiting/delete
// buckets after classification is complete.
//
// Ownership summary:
// 1) plan output names for accepted candidates
// 2) move accepted files into waiting-area and rejected files into delete-area
// 3) record rename/move artifacts for later review/report phases
//
// Earlier scan/judge decisions should already be made before entries reach here.
async function runRenamePhase(context = {}) {
  const {
    fs,
    path,
    dryRun,
    adFileAction,
    paths,
    candidates,
    pendingDelete,
    targetNames,
    shouldReportProgress,
    moveWithUnique,
    emitLog,
    emitProgress,
    onLog,
    onProgress,
    summary
  } = context;

  const renameRecords = [];
  const waitingMoveFailedSources = new Set();
  const totalDeleteCount = Array.isArray(pendingDelete) ? pendingDelete.length : 0;

  emitProgress(onProgress, {
    scope: 'organizer',
    phase: 'waiting-start',
    total: candidates.length,
    processed: 0,
    deleteTotal: totalDeleteCount,
    introAdTotal: 0
  });

  // Candidate handling is intentionally sequential so name conflicts and log
  // order remain easier to reason about during incident review.
  for (let index = 0; index < candidates.length; index += 1) {
    const candidate = candidates[index] || {};
    const plannedName = String(targetNames[index] || '').trim();
    const fallbackName = path.basename(String(candidate.src || '').trim());
    const fileName = plannedName || fallbackName;
    const destinationPath = path.join(paths.waitingDir, fileName);
    const originalName = path.basename(String(candidate.src || ''));
    const renameApplied = Boolean(candidate.renameByFilmCode && candidate.filmCode);
    const keepOriginalReason = renameApplied ? '' : String(candidate.keepOriginalReason || '未命中番号，保留原名');

    if (dryRun) {
      summary.movedToWaiting += 1;
      renameRecords.push({
        originalName,
        originalPath: candidate.src,
        waitingPath: destinationPath,
        newName: path.basename(destinationPath),
        filmCode: candidate.filmCode,
        renameApplied,
        note: keepOriginalReason,
        expectedCodeMatched: Boolean(candidate.expectedCodeMatched),
        size: Number(candidate.size || 0)
      });
      emitProgress(onProgress, {
        scope: 'organizer',
        phase: 'waiting-progress',
        total: candidates.length,
        processed: index + 1,
        deleteTotal: totalDeleteCount,
        introAdTotal: 0,
        failedOperations: summary.failedOperations
      });
      emitLog(onLog, 'info', `[预览] 待整理：${candidate.src} -> ${destinationPath}`);
      continue;
    }

    try {
      const movedPath = await moveWithUnique(candidate.src, destinationPath);
      summary.movedToWaiting += 1;
      renameRecords.push({
        originalName,
        originalPath: candidate.src,
        waitingPath: movedPath,
        newName: path.basename(movedPath),
        filmCode: candidate.filmCode,
        renameApplied,
        note: keepOriginalReason,
        expectedCodeMatched: Boolean(candidate.expectedCodeMatched),
        size: Number(candidate.size || 0)
      });
      emitLog(
        onLog,
        'info',
        renameApplied
          ? `已移入待整理并按番号改名：${candidate.src} -> ${movedPath}`
          : `已移入待整理并保留原名：${candidate.src} -> ${movedPath}（${keepOriginalReason}）`
      );
    } catch (error) {
      summary.failedOperations += 1;
      waitingMoveFailedSources.add(normalizeAbsolutePath(path, candidate.src));
      emitLog(onLog, 'warn', `移动到待整理失败：${candidate.src}，原因：${error instanceof Error ? error.message : String(error)}`);
    }

    emitProgress(onProgress, {
      scope: 'organizer',
      phase: 'waiting-progress',
      total: candidates.length,
      processed: index + 1,
      deleteTotal: totalDeleteCount,
      introAdTotal: 0,
      failedOperations: summary.failedOperations
    });
  }

  emitProgress(onProgress, {
    scope: 'organizer',
    phase: 'delete-start',
    total: totalDeleteCount,
    processed: 0,
    adFileAction,
    introAdTotal: 0
  });

  const deleteConcurrency = dryRun
    ? 1
    : toPositiveInt(context.deleteConcurrency, adFileAction === 'delete-directly' ? 20 : 16);
  emitLog(onLog, 'info', `待删除阶段并发数：${deleteConcurrency}`);

  let deleteProcessed = 0;
  const pendingDeleteMap = new Map();
  (Array.isArray(pendingDelete) ? pendingDelete : []).forEach((item) => {
    const sourcePath = String(item && item.src ? item.src : '').trim();
    if (!sourcePath) {
      return;
    }
    pendingDeleteMap.set(normalizeAbsolutePath(path, sourcePath), item);
  });

  // 直接删除模式优先按目录快速清理，减少逐文件 unlink 开销。
  // 如果大视频移动失败，则保留原目录，避免为了清理小广告误删有效视频。
  if (!dryRun && adFileAction === 'delete-directly' && pendingDeleteMap.size > 0) {
    const directoryCandidates = new Set();
    for (const item of pendingDeleteMap.values()) {
      const sourcePath = normalizeAbsolutePath(path, item.src);
      const sourceDir = path.dirname(sourcePath);
      if (!sourceDir || sourceDir === normalizeAbsolutePath(path, paths.rootPath)) {
        continue;
      }
      if (isManagedRootChild(paths, path, sourceDir)) {
        continue;
      }
      directoryCandidates.add(sourceDir);
    }

    const sortedDirs = Array.from(directoryCandidates).sort((left, right) => left.length - right.length);
    for (const sourceDir of sortedDirs) {
      let hasProtectedSource = false;
      for (const failedSource of waitingMoveFailedSources) {
        if (failedSource === sourceDir || failedSource.startsWith(`${sourceDir}${path.sep}`)) {
          hasProtectedSource = true;
          break;
        }
      }
      if (hasProtectedSource) {
        continue;
      }

      await fs.promises.rm(sourceDir, { recursive: true, force: true }).catch(() => {});
      const dirExists = await fs.promises.stat(sourceDir).then(() => true).catch(() => false);
      if (dirExists) {
        continue;
      }

      let removedInDir = 0;
      for (const [sourcePath] of Array.from(pendingDeleteMap.entries())) {
        if (sourcePath === sourceDir || sourcePath.startsWith(`${sourceDir}${path.sep}`)) {
          pendingDeleteMap.delete(sourcePath);
          removedInDir += 1;
        }
      }

      if (removedInDir > 0) {
        deleteProcessed += removedInDir;
        summary.deletedDirectly += removedInDir;
        emitLog(onLog, 'info', `已按目录快速删除：${sourceDir}（文件 ${removedInDir} 个）`);
        if (shouldReportProgress(deleteProcessed, totalDeleteCount, 40)) {
          emitProgress(onProgress, {
            scope: 'organizer',
            phase: 'delete-progress',
            total: totalDeleteCount,
            processed: deleteProcessed,
            adFileAction,
            introAdTotal: 0,
            failedOperations: summary.failedOperations
          });
        }
      }
    }
  }

  const remainingDeleteItems =
    adFileAction === 'delete-directly' && !dryRun ? Array.from(pendingDeleteMap.values()) : Array.from(pendingDeleteMap.values());

  async function processDeleteItem(item, deleteIndex) {
    const shouldLogDeleteDetail = Boolean(item.isVideo) || shouldReportProgress(deleteProcessed + 1, totalDeleteCount, 80);

    if (dryRun) {
      if (adFileAction === 'delete-directly') {
        summary.deletedDirectly += 1;
      } else {
        summary.movedToDelete += 1;
      }
      if (shouldLogDeleteDetail) {
        emitLog(
          onLog,
          'info',
          adFileAction === 'delete-directly' ? `[预览] 待直接删除：${item.src}` : `[预览] 待移入待删除：${item.src}`
        );
      }
    } else {
      try {
        if (adFileAction === 'delete-directly') {
          await fs.promises.unlink(item.src);
          summary.deletedDirectly += 1;
          if (shouldLogDeleteDetail) {
            emitLog(onLog, 'info', `已直接删除：${item.src}`);
          }
        } else {
          const destinationPath = resolveDeleteDestinationPath(paths, path, item.src);
          const movedPath = await moveWithUnique(item.src, destinationPath);
          summary.movedToDelete += 1;
          if (shouldLogDeleteDetail) {
            emitLog(onLog, 'info', `已移入待删除：${item.src} -> ${movedPath}`);
          }
        }
      } catch (error) {
        summary.failedOperations += 1;
        emitLog(
          onLog,
          'warn',
          `${adFileAction === 'delete-directly' ? '直接删除失败' : '移入待删除失败'}：${item.src}，原因：${
            error instanceof Error ? error.message : String(error)
          }`
        );
      }
    }

    deleteProcessed += 1;
    if (shouldReportProgress(deleteProcessed, totalDeleteCount, 40)) {
      emitProgress(onProgress, {
        scope: 'organizer',
        phase: 'delete-progress',
        total: totalDeleteCount,
        processed: deleteProcessed,
        adFileAction,
        introAdTotal: 0,
        failedOperations: summary.failedOperations
      });
    }
  }

  if (deleteConcurrency <= 1) {
    for (let deleteIndex = 0; deleteIndex < remainingDeleteItems.length; deleteIndex += 1) {
      // eslint-disable-next-line no-await-in-loop
      await processDeleteItem(remainingDeleteItems[deleteIndex], deleteIndex);
    }
  } else {
    await runWithConcurrency(remainingDeleteItems, deleteConcurrency, processDeleteItem);
  }

  return {
    renameRecords,
    waitingMoveFailedSources: Array.from(waitingMoveFailedSources)
  };
}

module.exports = {
  runRenamePhase
};
