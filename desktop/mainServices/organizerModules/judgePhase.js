// Legacy organizer compatibility phase for raw file classification.
// deprecated: archived JS organizer phase only; marker=archived-organizer-phase-judge
// Keep current-product organizer rule changes in the Go path first; this file
// is retained so the archived JS organizer flow still behaves predictably.
//
// Judge phase turns raw scanned files into three buckets:
// 1) valid video candidates that will enter rename/move processing
// 2) pending-delete records for ads/small files/non-target content
// 3) unmatched records used later for reports and operator review
//
// Ownership summary:
// 1) classify scanned files into candidate/delete/unmatched buckets
// 2) apply size and expected-code gating for the archived JS organizer lane
// 3) emit progress/logs for classification only, leaving later actions to
//    rename/move/report phases
//
// File map for maintainers:
// 1) expected-code and bucket setup
// 2) per-file classification loop
// 3) judge-phase summary/progress emission
async function runJudgePhase(context = {}) {
  const {
    fs,
    files,
    minSizeBytes,
    adFileAction,
    expectedCodeSets,
    extractFilmCodeFromFile,
    normalizeFilmId,
    shouldReportProgress,
    emitLog,
    emitProgress,
    onLog,
    onProgress,
    summary
  } = context;

  const candidates = [];
  const pendingDelete = [];
  const unmatchedRecords = [];
  const detectedFilmCodes = new Set();
  const expectedCodeSet =
    expectedCodeSets && expectedCodeSets.codeSet instanceof Set ? expectedCodeSets.codeSet : new Set();
  const hasExpectedCodes = expectedCodeSet.size > 0;
  const adActionLogPrefix = adFileAction === 'delete-directly' ? '待直接删除' : '已归入待删除';

  // This loop is the classification core for the JS organizer compatibility
  // path. Keep scan-time progress, size gating, and film-code matching together
  // here so later phases receive already-normalized buckets.
  for (let fileIndex = 0; fileIndex < files.length; fileIndex += 1) {
    const fileEntry = files[fileIndex] || {};
    const srcPath = String(fileEntry.path || '');
    if (!srcPath) {
      summary.failedOperations += 1;
      continue;
    }

    const scannedCount = fileIndex + 1;
    if (shouldReportProgress(scannedCount, files.length, 30)) {
      emitProgress(onProgress, {
        scope: 'organizer',
        phase: 'scan-progress',
        total: files.length,
        processed: scannedCount,
        videoTotal: summary.videoTotal,
        qualifiedVideo: summary.qualifiedVideo
      });
      emitLog(onLog, 'info', `扫描进度 ${scannedCount}/${files.length}（已识别视频 ${summary.videoTotal}，有效视频 ${summary.qualifiedVideo}）`);
    }

    const item = {
      src: srcPath,
      size: 0,
      isVideo: Boolean(fileEntry.isVideo),
      isRootLevel: Boolean(fileEntry.isRootLevel),
      filmCode: '',
      renameByFilmCode: false,
      keepOriginalReason: '',
      expectedCodeMatched: false
    };

    if (item.isVideo) {
      const stat = await fs.promises.stat(srcPath).catch(() => null);
      if (!stat || !stat.isFile()) {
        summary.failedOperations += 1;
        emitLog(onLog, 'warn', `文件状态异常，已跳过：${srcPath}`);
        continue;
      }
      item.size = stat.size;
      summary.videoTotal += 1;
    }

    const isLargeVideo = item.isVideo && item.size >= minSizeBytes;
    if (!isLargeVideo) {
      let reason = '非视频文件';
      if (item.isVideo) {
        reason = '低于最小容量阈值，判定为广告文件';
        summary.skippedSmall += 1;
      }

      if (item.isRootLevel) {
        unmatchedRecords.push({
          path: item.src,
          size: item.size,
          isVideo: item.isVideo,
          reason: `${reason}；根目录文件已保留，避免误删`
        });
        emitLog(onLog, 'warn', `根目录文件已保留，不进入待删除：${item.src}（${reason}）`);
        continue;
      }

      pendingDelete.push(item);
      unmatchedRecords.push({
        path: item.src,
        size: item.size,
        isVideo: item.isVideo,
        reason
      });
      if (item.isVideo) {
        emitLog(onLog, 'info', `${adActionLogPrefix}：${item.src}（${reason}）`);
      }
      continue;
    }

    summary.nonAdVideo += 1;
    const extractedFilmCode = extractFilmCodeFromFile(item.src, expectedCodeSets.tokenSet);
    const normalizedFilmCode = extractedFilmCode ? normalizeFilmId(extractedFilmCode) : '';
    const expectedMatched = Boolean(normalizedFilmCode) && (!hasExpectedCodes || expectedCodeSet.has(normalizedFilmCode));

    if (normalizedFilmCode) {
      item.filmCode = normalizedFilmCode;
      item.expectedCodeMatched = expectedMatched;
      detectedFilmCodes.add(normalizedFilmCode);
      if (hasExpectedCodes && expectedMatched) {
        summary.matchedToCrawlCode += 1;
      }
    }

    if (normalizedFilmCode && expectedMatched) {
      item.renameByFilmCode = true;
      emitLog(onLog, 'info', `识别为有效视频：${item.src} -> ${normalizedFilmCode}`);
    } else {
      item.renameByFilmCode = false;
      if (!normalizedFilmCode) {
        summary.skippedNoCode += 1;
        item.keepOriginalReason = '未识别番号，保留原名';
      } else {
        item.keepOriginalReason = `未命中爬虫番号名单（${normalizedFilmCode}），保留原名`;
      }

      emitLog(onLog, 'info', `识别为有效视频但保留原名：${item.src}（${item.keepOriginalReason}）`);
    }

    summary.qualifiedVideo += 1;
    candidates.push(item);
  }

  summary.unmatchedVideo = unmatchedRecords.length;
  summary.adFileCount = pendingDelete.length;
  summary.detectedCodeCount = detectedFilmCodes.size;

  emitProgress(onProgress, {
    scope: 'organizer',
    phase: 'scan-completed',
    total: files.length,
    processed: files.length,
    waitingTotal: candidates.length,
    deleteTotal: pendingDelete.length,
    introAdTotal: 0,
    videoTotal: summary.videoTotal,
    qualifiedVideo: summary.qualifiedVideo
  });

  return {
    candidates,
    pendingDelete,
    unmatchedRecords,
    detectedFilmCodes
  };
}

module.exports = {
  runJudgePhase
};
