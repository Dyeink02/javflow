import logger from './logger';
import type { ParsedMagnetCandidate } from './requestHandlerTypes';

export interface MagnetMetadataFileLike {
  name?: string;
  path?: string;
  length?: number;
}

export interface MagnetCandidateInspectionResult {
  candidate: ParsedMagnetCandidate;
  status: 'accepted' | 'rejected' | 'unverified';
  reason: string;
  summary: string;
}

export interface FilterMagnetCandidatesOptions {
  title: string;
  candidates: ParsedMagnetCandidate[];
  enabled?: boolean;
  keepAll?: boolean;
  maxInspectCount?: number;
  validationTimeoutSeconds?: number;
  maxValidationTimeMs?: number;
  abortSignal?: AbortSignal;
  inspectCandidate?: (candidate: ParsedMagnetCandidate) => Promise<MagnetCandidateInspectionResult>;
}

export interface MagnetCandidateValidationStats {
  totalCandidates: number;
  validationApplied: boolean;
  validationSkippedReason: 'disabled' | 'no-suspicious-candidate' | 'aborted' | 'budget-exhausted' | null;
  suspiciousCandidateCount: number;
  inspectedCount: number;
  acceptedCount: number;
  rejectedCount: number;
  unverifiedCount: number;
  timeoutCount: number;
  skippedCount: number;
}

export interface FilterMagnetCandidatesResult {
  candidates: ParsedMagnetCandidate[];
  stats: MagnetCandidateValidationStats;
}

interface ClassifiedFile {
  name: string;
  extension: string;
  length: number;
  isVideo: boolean;
  isImage: boolean;
  isSubtitle: boolean;
  isDangerous: boolean;
  isSoftSuspicious: boolean;
  isUnknown: boolean;
}

interface MagnetMetadataClassification {
  accepted: boolean;
  reason: string;
  summary: string;
}

const DEFAULT_VALIDATION_TIMEOUT_SECONDS = 5;
const DEFAULT_MAX_INSPECT_COUNT = 2;
const DEFAULT_MAX_VALIDATION_TIME_MS = 9000;
const SOFT_SUSPICIOUS_TOTAL_LIMIT_BYTES = 1024 * 1024;
const UNKNOWN_TOTAL_LIMIT_BYTES = 8 * 1024 * 1024;

const DEFAULT_TRACKERS = [
  'udp://tracker.opentrackr.org:1337/announce',
  'udp://open.stealth.si:80/announce',
  'udp://tracker.torrent.eu.org:451/announce'
];

const VIDEO_EXTENSIONS = new Set([
  'mp4',
  'mkv',
  'avi',
  'wmv',
  'mov',
  'm4v',
  'ts',
  'm2ts',
  'mpeg',
  'mpg',
  'flv',
  'webm',
  'iso'
]);

const IMAGE_EXTENSIONS = new Set(['jpg', 'jpeg', 'png', 'webp', 'gif', 'bmp', 'avif']);
const SUBTITLE_EXTENSIONS = new Set(['srt', 'ass', 'ssa', 'sub', 'idx']);

const DANGEROUS_EXTENSIONS = new Set([
  'apk',
  'exe',
  'msi',
  'bat',
  'cmd',
  'scr',
  'com',
  'jar',
  'url',
  'mht',
  'mhtml',
  'lnk',
  'html',
  'htm',
  'js',
  'vbs',
  'ps1',
  'php',
  'asp',
  'aspx',
  'jsp',
  'cab',
  'zip',
  'rar',
  '7z'
]);

const SOFT_SUSPICIOUS_EXTENSIONS = new Set(['txt', 'nfo', 'md', 'log', 'json', 'xml']);
const DANGEROUS_KEYWORDS = ['广告', '最新地址', '防屏蔽', '备用网址', '官网', 'install', 'setup', 'readme'];
const SUSPICIOUS_CANDIDATE_KEYWORDS = [
  'sis001',
  '第一会所',
  '第一會所',
  'sehuatang',
  '色花堂',
  '最新地址',
  '备用网址',
  '防屏蔽',
  '安装',
  'install',
  'setup',
  'readme',
  '官网',
  '官網',
  '网址',
  '網址',
  '广告',
  '廣告',
  'sample',
  'photo',
  '图包',
  '圖包'
];

function normalizeFileName(file: MagnetMetadataFileLike): string {
  const rawPath = String(file.path || file.name || '').trim().replace(/\\/g, '/');
  if (!rawPath) {
    return '';
  }

  const segments = rawPath.split('/').filter(Boolean);
  return segments.length > 0 ? segments[segments.length - 1] : rawPath;
}

function getFileExtension(fileName: string): string {
  const normalized = String(fileName || '').trim();
  const lastDotIndex = normalized.lastIndexOf('.');
  if (lastDotIndex <= 0 || lastDotIndex === normalized.length - 1) {
    return '';
  }

  return normalized.slice(lastDotIndex + 1).toLowerCase();
}

function toSafeLength(value: number | undefined): number {
  return Number.isFinite(value) && Number(value) > 0 ? Number(value) : 0;
}

function formatBytes(bytes: number): string {
  const sizeInMb = bytes / 1024 / 1024;
  if (sizeInMb >= 1024) {
    return `${(sizeInMb / 1024).toFixed(2)}GB`;
  }

  return `${sizeInMb.toFixed(2)}MB`;
}

function normalizeErrorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }

  return String(error || '未知错误');
}

function createValidationStats(totalCandidates: number): MagnetCandidateValidationStats {
  return {
    totalCandidates,
    validationApplied: false,
    validationSkippedReason: null,
    suspiciousCandidateCount: 0,
    inspectedCount: 0,
    acceptedCount: 0,
    rejectedCount: 0,
    unverifiedCount: 0,
    timeoutCount: 0,
    skippedCount: 0
  };
}

function normalizeComparisonToken(value: string): string {
  return String(value || '')
    .toUpperCase()
    .replace(/[^A-Z0-9]/g, '');
}

function extractFilmCode(value: string): string | null {
  const match = String(value || '').toUpperCase().match(/[A-Z]{2,8}[-_ ]?\d{2,5}/);
  return match ? normalizeComparisonToken(match[0]) : null;
}

function isTimeoutReason(reason: string): boolean {
  return String(reason || '').toLowerCase().includes('timeout');
}

function createAbortPromise(signal?: AbortSignal): Promise<never> | null {
  if (!signal) {
    return null;
  }

  if (signal.aborted) {
    return Promise.reject(new Error('Validation cancelled'));
  }

  return new Promise((_, reject) => {
    signal.addEventListener('abort', () => reject(new Error('Validation cancelled')), { once: true });
  });
}

function buildClassifiedFiles(files: MagnetMetadataFileLike[]): ClassifiedFile[] {
  return files
    .map((file) => {
      const name = normalizeFileName(file);
      const extension = getFileExtension(name);
      const lowerName = name.toLowerCase();
      const isVideo = VIDEO_EXTENSIONS.has(extension);
      const isImage = IMAGE_EXTENSIONS.has(extension);
      const isSubtitle = SUBTITLE_EXTENSIONS.has(extension);
      const isDangerous =
        DANGEROUS_EXTENSIONS.has(extension) ||
        DANGEROUS_KEYWORDS.some((keyword) => lowerName.includes(keyword));
      const isSoftSuspicious = SOFT_SUSPICIOUS_EXTENSIONS.has(extension);
      const isUnknown = !isVideo && !isImage && !isSubtitle && !isDangerous && !isSoftSuspicious;

      return {
        name,
        extension,
        length: toSafeLength(file.length),
        isVideo,
        isImage,
        isSubtitle,
        isDangerous,
        isSoftSuspicious,
        isUnknown
      };
    })
    .filter((file) => file.name);
}

export function classifyMagnetMetadataFiles(files: MagnetMetadataFileLike[]): MagnetMetadataClassification {
  const classifiedFiles = buildClassifiedFiles(files).sort((left, right) => right.length - left.length);
  if (classifiedFiles.length === 0) {
    return {
      accepted: false,
      reason: '未读取到磁力内部文件列表',
      summary: '未读取到磁力内部文件列表'
    };
  }

  const videoFiles = classifiedFiles.filter((file) => file.isVideo);
  const dangerousFiles = classifiedFiles.filter((file) => file.isDangerous);
  const softSuspiciousFiles = classifiedFiles.filter((file) => file.isSoftSuspicious);
  const unknownFiles = classifiedFiles.filter((file) => file.isUnknown);
  const imageFiles = classifiedFiles.filter((file) => file.isImage);
  const subtitleFiles = classifiedFiles.filter((file) => file.isSubtitle);
  const largestFile = classifiedFiles[0];
  const largestVideoFile = videoFiles.sort((left, right) => right.length - left.length)[0];
  const softSuspiciousTotal = softSuspiciousFiles.reduce((sum, file) => sum + file.length, 0);
  const unknownTotal = unknownFiles.reduce((sum, file) => sum + file.length, 0);

  if (!largestVideoFile) {
    return {
      accepted: false,
      reason: '未检测到视频文件',
      summary: `文件 ${classifiedFiles.length} 个，但未发现视频文件`
    };
  }

  if (largestFile && !largestFile.isVideo) {
    return {
      accepted: false,
      reason: `最大文件不是视频：${largestFile.name}`,
      summary: `最大文件 ${largestFile.name}（${formatBytes(largestFile.length)}）不是视频`
    };
  }

  if (dangerousFiles.length > 0) {
    const previewNames = dangerousFiles.slice(0, 2).map((file) => file.name).join('、');
    return {
      accepted: false,
      reason: `检测到广告/安装类文件：${previewNames}`,
      summary: `危险文件 ${dangerousFiles.length} 个，示例：${previewNames}`
    };
  }

  if (softSuspiciousFiles.length >= 2 || softSuspiciousTotal > SOFT_SUSPICIOUS_TOTAL_LIMIT_BYTES) {
    return {
      accepted: false,
      reason: '检测到较多文本/说明类杂文件',
      summary: `可疑文本文件 ${softSuspiciousFiles.length} 个，总大小 ${formatBytes(softSuspiciousTotal)}`
    };
  }

  if (unknownFiles.length >= 2 || unknownTotal > UNKNOWN_TOTAL_LIMIT_BYTES) {
    const previewNames = unknownFiles.slice(0, 2).map((file) => file.name).join('、');
    return {
      accepted: false,
      reason: `检测到未知格式杂文件：${previewNames}`,
      summary: `未知文件 ${unknownFiles.length} 个，总大小 ${formatBytes(unknownTotal)}`
    };
  }

  return {
    accepted: true,
    reason: '校验通过',
    summary:
      `主视频 ${largestVideoFile.name}（${formatBytes(largestVideoFile.length)}）` +
      `，视频 ${videoFiles.length} 个，图片 ${imageFiles.length} 个，字幕 ${subtitleFiles.length} 个`
  };
}

async function inspectMagnetCandidate(
  candidate: ParsedMagnetCandidate,
  validationTimeoutSeconds = DEFAULT_VALIDATION_TIMEOUT_SECONDS
): Promise<MagnetCandidateInspectionResult> {
  try {
    const Magnet2torrent = require('magnet2torrent-js') as any;
    const client = new Magnet2torrent({
      timeout: validationTimeoutSeconds,
      trackers: DEFAULT_TRACKERS
    });
    const torrent = await client.getTorrent(candidate.magnetLink);
    const files = Array.isArray(torrent?.files) ? torrent.files : [];
    const classification = classifyMagnetMetadataFiles(files);

    return {
      candidate,
      status: classification.accepted ? 'accepted' : 'rejected',
      reason: classification.reason,
      summary: classification.summary
    };
  } catch (error) {
    return {
      candidate,
      status: 'unverified',
      reason: normalizeErrorMessage(error),
      summary: '磁力元数据读取失败，已记为未验证候选'
    };
  }
}

function getCandidateLabel(candidate: ParsedMagnetCandidate): string {
  return candidate.displayName || candidate.magnetLink;
}

export function isSuspiciousMagnetCandidate(candidate: ParsedMagnetCandidate, title: string): boolean {
  const candidateLabel = getCandidateLabel(candidate);
  const lowerLabel = candidateLabel.toLowerCase();
  const normalizedLabel = normalizeComparisonToken(candidateLabel);
  const titleFilmCode = extractFilmCode(title);

  if (SUSPICIOUS_CANDIDATE_KEYWORDS.some((keyword) => lowerLabel.includes(keyword))) {
    return true;
  }

  if (titleFilmCode && !normalizedLabel.includes(titleFilmCode)) {
    return true;
  }

  return false;
}

async function inspectCandidateWithAbort(
  inspect: (candidate: ParsedMagnetCandidate) => Promise<MagnetCandidateInspectionResult>,
  candidate: ParsedMagnetCandidate,
  abortSignal?: AbortSignal
): Promise<MagnetCandidateInspectionResult> {
  const abortPromise = createAbortPromise(abortSignal);
  if (!abortPromise) {
    return inspect(candidate);
  }

  return Promise.race([inspect(candidate), abortPromise]);
}

async function selectSingleMagnetCandidateByContent(
  title: string,
  orderedCandidates: ParsedMagnetCandidate[],
  stats: MagnetCandidateValidationStats,
  inspect: (candidate: ParsedMagnetCandidate) => Promise<MagnetCandidateInspectionResult>,
  maxInspectCount: number,
  maxValidationTimeMs: number,
  abortSignal?: AbortSignal
): Promise<FilterMagnetCandidatesResult> {
  // Default mode only validates the current best candidate, then steps down one
  // candidate at a time when it is explicitly rejected as an ad/bundle magnet.
  const suspiciousCandidates = orderedCandidates.filter((candidate) => isSuspiciousMagnetCandidate(candidate, title));
  stats.suspiciousCandidateCount = suspiciousCandidates.length;

  stats.validationApplied = true;
  logger.info(`fetchMagnet: 已启用磁力内容校验（广告过滤），将按大小顺序逐条判断并在命中广告后顺延下一条：${title}`);

  const validationStartedAt = Date.now();

  for (let index = 0; index < orderedCandidates.length; index += 1) {
    const candidate = orderedCandidates[index];
    const remainingCount = Math.max(orderedCandidates.length - index - 1, 0);

    if (abortSignal?.aborted) {
      stats.validationSkippedReason = 'aborted';
      stats.skippedCount = remainingCount + 1;
      return {
        candidates: [candidate],
        stats
      };
    }

    if (stats.inspectedCount >= maxInspectCount || Date.now() - validationStartedAt >= maxValidationTimeMs) {
      stats.validationSkippedReason = 'budget-exhausted';
      stats.skippedCount = remainingCount + 1;
      logger.warn(`fetchMagnet: 磁力内容校验已达到单片预算上限，保留当前顺延候选并停止继续检查：${title}`);
      return {
        candidates: [candidate],
        stats
      };
    }

    const candidateLabel = getCandidateLabel(candidate);
    let inspection: MagnetCandidateInspectionResult;
    try {
      inspection = await inspectCandidateWithAbort(inspect, candidate, abortSignal);
    } catch (error) {
      const reason = normalizeErrorMessage(error);
      if (abortSignal?.aborted || reason === 'Validation cancelled') {
        stats.validationSkippedReason = 'aborted';
        stats.skippedCount = remainingCount + 1;
        return {
          candidates: [candidate],
          stats
        };
      }

      inspection = {
        candidate,
        status: 'unverified',
        reason,
        summary: '磁力元数据读取失败，已保留当前候选'
      };
    }

    stats.inspectedCount += 1;

    if (inspection.status === 'accepted') {
      stats.acceptedCount += 1;
      stats.skippedCount = remainingCount;
      logger.debug(`fetchMagnet: 磁力内容校验通过：${candidateLabel}；${inspection.summary}`);
      return {
        candidates: [candidate],
        stats
      };
    }

    if (inspection.status === 'unverified') {
      stats.unverifiedCount += 1;
      if (isTimeoutReason(inspection.reason)) {
        stats.timeoutCount += 1;
      }
      stats.skippedCount = remainingCount;
      logger.warn(`fetchMagnet: 磁力内容校验未完成，保留当前候选 ${candidateLabel}；原因：${inspection.reason}`);
      return {
        candidates: [candidate],
        stats
      };
    }

    stats.rejectedCount += 1;
    logger.warn(`fetchMagnet: 磁力内容校验未通过，已跳过候选 ${candidateLabel}；原因：${inspection.reason}`);
  }

  logger.warn(`fetchMagnet: ${title} 的候选磁力均被判定为广告包或杂文件包，已放弃当前磁力结果。`);
  return {
    candidates: [],
    stats
  };
}

export async function filterMagnetCandidatesByContentDetailed(
  options: FilterMagnetCandidatesOptions
): Promise<FilterMagnetCandidatesResult> {
  const {
    title,
    candidates,
    enabled = false,
    keepAll = false,
    maxInspectCount = DEFAULT_MAX_INSPECT_COUNT,
    validationTimeoutSeconds = DEFAULT_VALIDATION_TIMEOUT_SECONDS,
    maxValidationTimeMs = DEFAULT_MAX_VALIDATION_TIME_MS,
    abortSignal
  } = options;
  const stats = createValidationStats(candidates.length);
  const orderedCandidates = [...candidates].sort((left, right) => right.size - left.size);

  if (!enabled || orderedCandidates.length === 0) {
    stats.validationSkippedReason = enabled ? null : 'disabled';
    stats.skippedCount = orderedCandidates.length;
    return {
      candidates: orderedCandidates,
      stats
    };
  }

  if (abortSignal?.aborted) {
    stats.validationSkippedReason = 'aborted';
    stats.skippedCount = orderedCandidates.length;
    return {
      candidates: orderedCandidates,
      stats
    };
  }

  const inspect = options.inspectCandidate
    ? options.inspectCandidate
    : (candidate: ParsedMagnetCandidate) => inspectMagnetCandidate(candidate, validationTimeoutSeconds);

  if (!keepAll) {
    return selectSingleMagnetCandidateByContent(
      title,
      orderedCandidates,
      stats,
      inspect,
      Math.max(1, maxInspectCount),
      maxValidationTimeMs,
      abortSignal
    );
  }

  const inspectWindow = orderedCandidates.slice(0, Math.max(1, maxInspectCount));
  const suspiciousCandidates = inspectWindow.filter((candidate) => isSuspiciousMagnetCandidate(candidate, title));
  stats.suspiciousCandidateCount = suspiciousCandidates.length;

  if (suspiciousCandidates.length === 0) {
    stats.validationSkippedReason = 'no-suspicious-candidate';
    stats.skippedCount = orderedCandidates.length;
    return {
      candidates: orderedCandidates,
      stats
    };
  }

  stats.validationApplied = true;
  logger.info(
    `fetchMagnet: 已启用磁力内容校验（广告过滤），本次仅检查前 ${inspectWindow.length} 条中的 ${suspiciousCandidates.length} 条可疑候选：${title}`
  );

  const inspectedCandidateSet = new Set<ParsedMagnetCandidate>();
  const acceptedCandidates: ParsedMagnetCandidate[] = [];
  const unverifiedCandidates: ParsedMagnetCandidate[] = [];
  const validationStartedAt = Date.now();

  for (const candidate of suspiciousCandidates) {
    if (abortSignal?.aborted) {
      stats.validationSkippedReason = 'aborted';
      break;
    }

    if (Date.now() - validationStartedAt >= maxValidationTimeMs) {
      stats.validationSkippedReason = 'budget-exhausted';
      logger.warn(`fetchMagnet: 磁力内容校验已达到单片预算上限，停止继续检查：${title}`);
      break;
    }

    const candidateLabel = getCandidateLabel(candidate);
    inspectedCandidateSet.add(candidate);
    let inspection: MagnetCandidateInspectionResult;
    try {
      inspection = await inspectCandidateWithAbort(inspect, candidate, abortSignal);
    } catch (error) {
      const reason = normalizeErrorMessage(error);
      if (abortSignal?.aborted || reason === 'Validation cancelled') {
        stats.validationSkippedReason = 'aborted';
        break;
      }

      inspection = {
        candidate,
        status: 'unverified',
        reason,
        summary: '磁力元数据读取失败，已记为未验证候选'
      };
    }

    stats.inspectedCount += 1;

    if (inspection.status === 'accepted') {
      acceptedCandidates.push(candidate);
      stats.acceptedCount += 1;
      logger.debug(`fetchMagnet: 磁力内容校验通过：${candidateLabel}；${inspection.summary}`);

      if (!keepAll) {
        return {
          candidates: [candidate],
          stats
        };
      }
      continue;
    }

    if (inspection.status === 'unverified') {
      unverifiedCandidates.push(candidate);
      stats.unverifiedCount += 1;
      if (isTimeoutReason(inspection.reason)) {
        stats.timeoutCount += 1;
      }
      logger.warn(`fetchMagnet: 磁力内容校验未完成：${candidateLabel}；原因：${inspection.reason}`);
      continue;
    }

    stats.rejectedCount += 1;
    logger.warn(`fetchMagnet: 磁力内容校验未通过，已跳过候选 ${candidateLabel}；原因：${inspection.reason}`);
  }

  const skippedCandidates = orderedCandidates.filter((candidate) => !inspectedCandidateSet.has(candidate));
  stats.skippedCount = skippedCandidates.length;

  if (keepAll) {
    const retainedCandidates = acceptedCandidates
      .concat(unverifiedCandidates.filter((candidate) => !acceptedCandidates.includes(candidate)))
      .concat(skippedCandidates.filter((candidate) => !acceptedCandidates.includes(candidate)));

    if (retainedCandidates.length === 0) {
      logger.warn(`fetchMagnet: ${title} 的全部候选磁力均未通过内容校验，已全部跳过。`);
      return {
        candidates: [],
        stats
      };
    }

    if (unverifiedCandidates.length > 0) {
      logger.warn(
        `fetchMagnet: ${title} 有 ${unverifiedCandidates.length} 条候选因元数据读取失败未能确认，已暂时保留未验证结果。`
      );
    }

    return {
      candidates: retainedCandidates,
      stats
    };
  }

  if (unverifiedCandidates.length > 0) {
    const fallbackCandidate = unverifiedCandidates[0];
    logger.warn(
      `fetchMagnet: ${title} 没有找到明确通过校验的候选磁力，已回退到首条未验证候选：${getCandidateLabel(fallbackCandidate)}`
    );
    return {
      candidates: [fallbackCandidate],
      stats
    };
  }

  if (skippedCandidates.length > 0) {
    const fallbackCandidate = skippedCandidates[0];
    logger.info(`fetchMagnet: ${title} 未发现可直接通过校验的候选，回退到未检查的下一条候选：${getCandidateLabel(fallbackCandidate)}`);
    return {
      candidates: [fallbackCandidate],
      stats
    };
  }

  logger.warn(`fetchMagnet: ${title} 的候选磁力均被判定为广告包或杂文件包，已放弃当前磁力结果。`);
  return {
    candidates: [],
    stats
  };
}

export async function filterMagnetCandidatesByContent(
  options: FilterMagnetCandidatesOptions
): Promise<ParsedMagnetCandidate[]> {
  const result = await filterMagnetCandidatesByContentDetailed(options);
  return result.candidates;
}
