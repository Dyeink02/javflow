import type { Config } from '../types/interfaces';

export function buildIndexPageUrl(config: Config, pageNumber: number): string {
  const baseUrl = (config.base || config.BASE_URL).replace(/\/$/, '');
  const pagePart = pageNumber === 1 ? '' : `/${pageNumber}`;

  if (config.search) {
    return `${baseUrl}${config.searchUrl ? `/${config.searchUrl}` : ''}/${encodeURIComponent(config.search)}${pagePart}`;
  }

  const parsedUrl = new URL(baseUrl);
  const normalizedPath = parsedUrl.pathname.replace(/\/$/, '');
  const pathSegments = normalizedPath.split('/').filter(Boolean);

  if (
    normalizedPath.includes('/genre/') ||
    normalizedPath.includes('/search/') ||
    normalizedPath.includes('/star/') ||
    normalizedPath.includes('/studio/') ||
    normalizedPath.includes('/label/') ||
    normalizedPath.includes('/director/') ||
    normalizedPath.includes('/series/')
  ) {
    return `${baseUrl}${pagePart}`;
  }

  if (pathSegments.length === 0) {
    return `${baseUrl}${pageNumber === 1 ? '' : `/page${pagePart}`}`;
  }

  return `${baseUrl}${pagePart}`;
}

export function normalizeDetailLink(link: string): string {
  const raw = String(link || '').trim();
  if (!raw) {
    return '';
  }

  try {
    const parsed = new URL(raw);
    const normalizedPath = parsed.pathname.replace(/\/+$/, '') || '/';
    return `${parsed.origin}${normalizedPath}${parsed.search}`.toLowerCase();
  } catch {
    return raw.replace(/\/+$/, '').toLowerCase();
  }
}

export function getDetailItemId(
  value: string,
  extractFilmId: (candidate: string) => string | null
): string {
  return extractFilmId(value) || normalizeDetailLink(value) || value;
}

export function mergePageLinks(
  existingLinks: string[],
  incomingLinks: string[],
  getIdentity: (link: string) => string
): string[] {
  const mergedLinks: string[] = [];
  const seen = new Set<string>();

  for (const link of [...existingLinks, ...incomingLinks]) {
    const identity = getIdentity(link);
    if (seen.has(identity)) {
      continue;
    }

    seen.add(identity);
    mergedLinks.push(link);
  }

  return mergedLinks;
}

export interface PageLinkDuplicateAnalysis {
  uniqueCount: number;
  duplicateCount: number;
  duplicateIds: string[];
}

export function analyzePageLinkDuplicates(
  links: string[],
  getIdentity: (link: string) => string
): PageLinkDuplicateAnalysis {
  const counts = new Map<string, number>();

  for (const link of links) {
    const identity = getIdentity(link);
    if (!identity) {
      continue;
    }

    counts.set(identity, (counts.get(identity) || 0) + 1);
  }

  const duplicateIds = Array.from(counts.entries())
    .filter(([, count]) => count > 1)
    .map(([identity]) => identity)
    .sort((left, right) => left.localeCompare(right, 'zh-CN'));

  return {
    uniqueCount: counts.size,
    duplicateCount: duplicateIds.reduce((total, identity) => total + ((counts.get(identity) || 1) - 1), 0),
    duplicateIds
  };
}

export function getExpectedItemCountForPage(params: {
  currentPage: number;
  targetTotalPages: number;
  filmLimit: number;
  expectedItemsPerPage: number | null;
}): number | null {
  const { currentPage, targetTotalPages, filmLimit, expectedItemsPerPage } = params;
  if (expectedItemsPerPage === null) {
    return null;
  }

  if (targetTotalPages > 0 && currentPage < targetTotalPages) {
    return expectedItemsPerPage;
  }

  if (targetTotalPages > 0 && currentPage === targetTotalPages && filmLimit > 0) {
    const remainder = filmLimit % expectedItemsPerPage;
    return remainder === 0 ? expectedItemsPerPage : remainder;
  }

  return expectedItemsPerPage;
}

export function getInferredTotalPages(params: {
  filmLimit: number;
  expectedItemsPerPage: number | null;
}): number {
  const { filmLimit, expectedItemsPerPage } = params;
  if (expectedItemsPerPage === null || expectedItemsPerPage <= 0) {
    return 0;
  }

  if (filmLimit > 0) {
    return Math.ceil(filmLimit / expectedItemsPerPage);
  }

  return 0;
}

export interface IndexTargetPageState {
  inferredTotalPages: number;
  targetTotalPages: number;
  isLastTargetPage: boolean;
}

export function resolveIndexTargetPageState(params: {
  currentPage: number;
  configuredTotalPages: number;
  filmLimit: number;
  expectedItemsPerPage: number | null;
}): IndexTargetPageState {
  const { currentPage, configuredTotalPages, filmLimit, expectedItemsPerPage } = params;
  const inferredTotalPages = getInferredTotalPages({
    filmLimit,
    expectedItemsPerPage
  });
  const targetTotalPages = configuredTotalPages > 0 ? configuredTotalPages : inferredTotalPages;

  return {
    inferredTotalPages,
    targetTotalPages,
    isLastTargetPage: targetTotalPages > 0 && currentPage >= targetTotalPages
  };
}

export interface IndexQueueLimitDecision {
  queueCount: number;
  remainingSlots: number;
  shouldStopBeforeQueue: boolean;
  shouldStopAfterQueue: boolean;
}

export function resolveIndexQueueLimitDecision(params: {
  filmLimit: number;
  filmsQueued: number;
  newLinksCount: number;
}): IndexQueueLimitDecision {
  const { filmLimit, filmsQueued, newLinksCount } = params;
  if (filmLimit <= 0) {
    return {
      queueCount: newLinksCount,
      remainingSlots: Number.MAX_SAFE_INTEGER,
      shouldStopBeforeQueue: false,
      shouldStopAfterQueue: false
    };
  }

  const remainingSlots = Math.max(filmLimit - filmsQueued, 0);
  if (remainingSlots <= 0) {
    return {
      queueCount: 0,
      remainingSlots: 0,
      shouldStopBeforeQueue: true,
      shouldStopAfterQueue: true
    };
  }

  const queueCount = Math.min(newLinksCount, remainingSlots);
  return {
    queueCount,
    remainingSlots,
    shouldStopBeforeQueue: false,
    shouldStopAfterQueue: filmsQueued + queueCount >= filmLimit
  };
}

export interface IndexProcessingDecision {
  action:
    | 'continue_after_gap'
    | 'stop_empty_page'
    | 'continue_resume_completed_page'
    | 'stop_no_new_links'
    | 'stop_limit_reached'
    | 'stop_target_page_reached'
    | 'continue';
  shouldAdvancePage: boolean;
  shouldStopIndexing: boolean;
}

export function resolveIndexProcessingDecision(params: {
  currentPage: number;
  targetTotalPages: number;
  expectedCount: number | null;
  linksCount: number;
  newLinksCount: number;
  resumeExisting: boolean;
  filmLimit: number;
  filmsQueued: number;
}): IndexProcessingDecision {
  const {
    currentPage,
    targetTotalPages,
    expectedCount,
    linksCount,
    newLinksCount,
    resumeExisting,
    filmLimit,
    filmsQueued
  } = params;

  if (linksCount === 0) {
    const canContinueAfterGap = expectedCount !== null && targetTotalPages > 0 && currentPage < targetTotalPages;
    if (canContinueAfterGap) {
      return {
        action: 'continue_after_gap',
        shouldAdvancePage: true,
        shouldStopIndexing: false
      };
    }

    return {
      action: 'stop_empty_page',
      shouldAdvancePage: false,
      shouldStopIndexing: true
    };
  }

  if (newLinksCount === 0) {
    if (resumeExisting) {
      return {
        action: 'continue_resume_completed_page',
        shouldAdvancePage: true,
        shouldStopIndexing: false
      };
    }

    return {
      action: 'stop_no_new_links',
      shouldAdvancePage: false,
      shouldStopIndexing: true
    };
  }

  if (filmLimit > 0 && filmsQueued >= filmLimit) {
    return {
      action: 'stop_limit_reached',
      shouldAdvancePage: true,
      shouldStopIndexing: true
    };
  }

  if (targetTotalPages > 0 && currentPage >= targetTotalPages) {
    return {
      action: 'stop_target_page_reached',
      shouldAdvancePage: true,
      shouldStopIndexing: true
    };
  }

  return {
    action: 'continue',
    shouldAdvancePage: true,
    shouldStopIndexing: false
  };
}

export function shouldWarnSparseIndexPage(params: {
  shouldStopIndexing: boolean;
  expectedItemsPerPage: number | null;
  isLastTargetPage: boolean;
  linksCount: number;
}): boolean {
  const { shouldStopIndexing, expectedItemsPerPage, isLastTargetPage, linksCount } = params;
  return !shouldStopIndexing && expectedItemsPerPage !== null && !isLastTargetPage && linksCount < expectedItemsPerPage;
}

export function getTrackedPageLinks(
  links: string[],
  targetLimit: number,
  expectedCount: number
): string[] {
  if (targetLimit <= 0) {
    return links;
  }

  const remainingSlots = Math.max(targetLimit - expectedCount, 0);
  if (remainingSlots <= 0) {
    return [];
  }

  return links.slice(0, remainingSlots);
}
