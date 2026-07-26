// Legacy organizer compatibility phase for post-move intro-ad review.
// deprecated: archived JS organizer phase only; marker=archived-organizer-phase-intro-ad
// This file belongs to the archived JS organizer lane and should not become
// the default place for new organizer product rules.
//
// Ownership summary:
// 1) post-process waiting-area files for optional intro-ad risk review
// 2) move risky files into the dedicated intro-ad bucket when needed
// 3) keep intro-ad-specific progress/logging local to this post-move phase
//
// File map for maintainers:
// 1) concurrency helpers for ad-risk review
// 2) intro-ad destination resolution helpers
// 3) intro-ad review/move execution pipeline

function toPositiveInt(value, fallback) {
  const parsed = Number.parseInt(String(value ?? '').trim(), 10);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    return fallback;
  }
  return parsed;
}

// Shared bounded-concurrency helper for post-move intro-ad review so the phase
// can scale without letting every record spawn an uncontrolled async task.
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

// Preserve relative structure when moving files from waiting-area into the
// intro-ad area, but fall back to basename if the source path is outside the
// managed waiting root.
function resolveIntroAdDestinationPath(paths, path, sourcePath) {
  const introAdRoot = String((paths && paths.introAdDir) || '').trim();
  const waitingRoot = String((paths && paths.waitingDir) || '').trim();
  const normalizedSource = String(sourcePath || '').trim();
  if (!introAdRoot || !normalizedSource) {
    return path.join(introAdRoot, path.basename(normalizedSource));
  }

  if (waitingRoot) {
    const relativePath = path.relative(waitingRoot, normalizedSource);
    const isSafeRelative =
      relativePath &&
      !relativePath.startsWith('..') &&
      !path.isAbsolute(relativePath) &&
      !relativePath.includes(`..${path.sep}`);

    if (isSafeRelative) {
      return path.join(introAdRoot, relativePath);
    }
  }

  return path.join(introAdRoot, path.basename(normalizedSource));
}

// Keep operator-facing ad-risk explanations compact and deterministic so logs
// and reports remain readable during bulk organizer runs.
function buildAdRiskReason(adRiskResult = {}) {
  const reasonParts = [];
  if (Number.isFinite(adRiskResult.score) && Number.isFinite(adRiskResult.threshold)) {
    reasonParts.push(`开头广告风险评分 ${adRiskResult.score}/${adRiskResult.threshold}`);
  }

  if (Array.isArray(adRiskResult.reasons) && adRiskResult.reasons.length > 0) {
    reasonParts.push(adRiskResult.reasons.join('；'));
  }

  return reasonParts.length > 0 ? reasonParts.join('；') : '命中开头广告风险';
}

// Intro-ad phase is a post-move review lane. It works only on files already
// accepted into waiting-area, then optionally reclassifies risky items into the
// dedicated intro-ad bucket without changing the earlier scan/judge results.
async function runIntroAdPhase(context = {}) {
  const {
    fs,
    path,
    dryRun,
    paths,
    renameRecords,
    adDetectionEnabled,
    adThreshold,
    evaluateAdRisk,
    normalizeFilmId,
    shouldReportProgress,
    moveWithUnique,
    emitLog,
    emitProgress,
    onLog,
    onProgress,
    summary
  } = context;

  const records = Array.isArray(renameRecords) ? renameRecords : [];
  const adRiskRecords = [];
  const introAdRecords = [];

  emitProgress(onProgress, {
    scope: 'organizer',
    phase: 'intro-ad-start',
    total: records.length,
    processed: 0,
    failedOperations: summary.failedOperations
  });

  if (!adDetectionEnabled) {
    emitLog(onLog, 'info', '已关闭开头广告检测，跳过后置复核阶段。');
    emitProgress(onProgress, {
      scope: 'organizer',
      phase: 'intro-ad-progress',
      total: records.length,
      processed: records.length,
      failedOperations: summary.failedOperations
    });
    return {
      adRiskRecords,
      introAdRecords
    };
  }

  if (typeof evaluateAdRisk !== 'function') {
    emitLog(onLog, 'warn', '开头广告检测已启用，但当前无可用评估服务，已跳过后置复核。');
    emitProgress(onProgress, {
      scope: 'organizer',
      phase: 'intro-ad-progress',
      total: records.length,
      processed: records.length,
      failedOperations: summary.failedOperations
    });
    return {
      adRiskRecords,
      introAdRecords
    };
  }

  const introAdConcurrency = dryRun ? 1 : toPositiveInt(context.introAdConcurrency, 4);
  emitLog(onLog, 'info', `开头广告后置复核并发数：${introAdConcurrency}`);

  let processed = 0;

  // Per-file handling stays local so concurrency, progress, and summary updates
  // remain coordinated in one place.
  async function processItem(record, index) {
    const waitingPath = String(record && record.waitingPath ? record.waitingPath : '').trim();
    const filmCode = normalizeFilmId(record && record.filmCode ? record.filmCode : '');
    const size = Number(record && record.size ? record.size : 0);

    if (!waitingPath) {
      summary.failedOperations += 1;
      emitLog(onLog, 'warn', '开头广告后置复核跳过：缺少待整理路径。');
    } else {
      try {
        if (!dryRun) {
          const stat = await fs.promises.stat(waitingPath).catch(() => null);
          if (!stat || !stat.isFile()) {
            throw new Error('待整理文件不存在或不可访问');
          }
        }

        const adRiskResult = await evaluateAdRisk({
          videoPath: waitingPath,
          filmCode,
          adThreshold
        });

        if (adRiskResult && adRiskResult.isAd) {
          const reasonText = buildAdRiskReason(adRiskResult);
          let destinationPath = waitingPath;
          let movedToIntroAd = false;
          summary.adRiskRejected += 1;
          if (!dryRun) {
            try {
              const targetPath = resolveIntroAdDestinationPath(paths, path, waitingPath);
              destinationPath = await moveWithUnique(waitingPath, targetPath);
              summary.movedToIntroAd += 1;
              summary.movedToWaiting = Math.max(0, Number(summary.movedToWaiting || 0) - 1);
              movedToIntroAd = true;
            } catch (moveError) {
              summary.failedOperations += 1;
              emitLog(
                onLog,
                'warn',
                `命中开头广告风险，但移入“含开头广告”失败：${waitingPath}，原因：${
                  moveError instanceof Error ? moveError.message : String(moveError)
                }`
              );
            }
          } else {
            summary.movedToIntroAd += 1;
            summary.movedToWaiting = Math.max(0, Number(summary.movedToWaiting || 0) - 1);
            movedToIntroAd = true;
          }

          introAdRecords.push({
            filmCode,
            path: destinationPath,
            size
          });
          adRiskRecords.push({
            filmCode,
            sourcePath: destinationPath,
            size,
            score: adRiskResult.score,
            threshold: adRiskResult.threshold,
            reasons: Array.isArray(adRiskResult.reasons) ? adRiskResult.reasons : [],
            evidence: adRiskResult.evidence || null
          });
          emitLog(
            onLog,
            movedToIntroAd ? 'warn' : 'info',
            movedToIntroAd
              ? `命中开头广告风险，已归入“含开头广告”：${waitingPath} -> ${destinationPath}（${reasonText}）`
              : `命中开头广告风险，保留在待整理待人工复核：${waitingPath}（${reasonText}）`
          );
        }
      } catch (error) {
        summary.adDetectionErrors += 1;
        emitLog(onLog, 'warn', `开头广告后置复核失败：${waitingPath}，原因：${error instanceof Error ? error.message : String(error)}`);
      }
    }

    processed += 1;
    if (shouldReportProgress(processed, records.length, 20)) {
      emitProgress(onProgress, {
        scope: 'organizer',
        phase: 'intro-ad-progress',
        total: records.length,
        processed,
        failedOperations: summary.failedOperations
      });
    }
    if (shouldReportProgress(index + 1, records.length, 80)) {
      emitLog(onLog, 'info', `开头广告后置复核进度 ${processed}/${records.length}`);
    }
  }

  if (introAdConcurrency <= 1) {
    for (let index = 0; index < records.length; index += 1) {
      // eslint-disable-next-line no-await-in-loop
      await processItem(records[index], index);
    }
  } else {
    await runWithConcurrency(records, introAdConcurrency, processItem);
  }

  return {
    adRiskRecords,
    introAdRecords
  };
}

module.exports = {
  runIntroAdPhase
};
