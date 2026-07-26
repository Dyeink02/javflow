import logger from './logger';
import type { ParsedMagnetCandidate } from './requestHandlerTypes';
import type { MagnetResult } from '../types/interfaces';

export function getMagnetExcludeKeywords(rawValue: string | undefined): string[] {
  const normalized = String(rawValue || '').trim();
  if (!normalized) {
    return [];
  }

  return Array.from(
    new Set(
      normalized
        .split(/[\r\n,，、]+/)
        .map((item) => item.trim().toLowerCase())
        .filter(Boolean)
    )
  );
}

export function getMagnetDisplayName(magnetLink: string): string {
  const rawValue = String(magnetLink || '').trim();
  if (!rawValue) {
    return '';
  }

  try {
    const magnetUrl = new URL(rawValue);
    const decodedName = magnetUrl.searchParams.get('dn') || '';
    return decodeURIComponent(decodedName.replace(/\+/g, ' ')).trim();
  } catch {
    const match = rawValue.match(/[?&]dn=([^&]+)/i);
    if (!match?.[1]) {
      return rawValue;
    }

    try {
      return decodeURIComponent(match[1].replace(/\+/g, ' ')).trim();
    } catch {
      return match[1].trim();
    }
  }
}

export function applyMagnetExcludeFilter(
  title: string,
  candidates: ParsedMagnetCandidate[],
  rawKeywords: string | undefined
): ParsedMagnetCandidate[] {
  const keywords = getMagnetExcludeKeywords(rawKeywords);
  if (keywords.length === 0 || candidates.length === 0) {
    return candidates;
  }

  const filteredOut: Array<{ candidate: ParsedMagnetCandidate; keyword: string }> = [];
  const retainedCandidates: ParsedMagnetCandidate[] = [];

  for (const candidate of candidates) {
    const displayName = candidate.displayName.toLowerCase();
    const magnetText = candidate.magnetLink.toLowerCase();
    const matchedKeyword = keywords.find(
      (keyword) => displayName.includes(keyword) || magnetText.includes(keyword)
    );

    if (matchedKeyword) {
      filteredOut.push({ candidate, keyword: matchedKeyword });
      continue;
    }

    retainedCandidates.push(candidate);
  }

  if (filteredOut.length > 0) {
    logger.info(
      `fetchMagnet: ${title} 命中过滤词并跳过 ${filteredOut.length} 条磁力，保留 ${retainedCandidates.length} 条。`
    );
    filteredOut.slice(0, 5).forEach(({ candidate, keyword }, index) => {
      const preview = candidate.displayName || candidate.magnetLink;
      logger.debug(`fetchMagnet: 已过滤磁力 ${index + 1}，关键词=${keyword}，标题=${preview}`);
    });
  }

  return retainedCandidates;
}

export function formatFileSize(sizeInMB: number): string {
  if (sizeInMB >= 1024) {
    return `${(sizeInMB / 1024).toFixed(2)}GB`;
  }

  return `${sizeInMB.toFixed(2)}MB`;
}

export function normalizeAjaxImageParam(value: string | null | undefined): string {
  const rawValue = String(value || '').trim();
  if (!rawValue) {
    return '';
  }

  const unescapedValue = rawValue.replace(/\\\//g, '/');

  try {
    const absoluteUrl = new URL(unescapedValue);
    return `${absoluteUrl.pathname}${absoluteUrl.search}`.replace(/^\/+/, '');
  } catch {
    return unescapedValue.replace(/^\/+/, '');
  }
}

export function extractMagnetLinks(responseBody: string): string[] {
  const matches = responseBody.match(/magnet:\?xt=urn:btih:[A-F0-9]+&dn=[^&"']+/gi) || [];
  return Array.from(new Set(matches.map((item) => item.trim()).filter(Boolean)));
}

export function extractSizeTokens(responseBody: string): string[] {
  return (responseBody.match(/\d+(\.\d+)?[GM]B/gi) || []).map((item) => item.toUpperCase());
}

// Normalize the AJAX response into sortable candidates so the request handler
// can focus on retry and fallback decisions.
export function buildParsedMagnetCandidates(
  magnetLinks: string[],
  sizeTokens: string[]
): ParsedMagnetCandidate[] {
  return magnetLinks.map((magnetLink, index) => {
    const sizeStr = sizeTokens[index] || '0MB';
    const normalizedSize = sizeStr.toUpperCase();
    const sizeValue = parseFloat(normalizedSize.replace(/GB|MB/, ''));
    const safeSizeValue = Number.isFinite(sizeValue) ? sizeValue : 0;
    const sizeInMB = normalizedSize.includes('GB') ? safeSizeValue * 1024 : safeSizeValue;

    return {
      magnetLink,
      size: sizeInMB,
      displayName: getMagnetDisplayName(magnetLink)
    };
  });
}

export function selectLargestMagnetCandidate(
  candidates: ParsedMagnetCandidate[]
): ParsedMagnetCandidate | null {
  if (!Array.isArray(candidates) || candidates.length === 0) {
    return null;
  }

  return candidates.reduce((prev, current) => (prev.size > current.size ? prev : current), candidates[0]);
}

export function buildMagnetResult(
  candidates: ParsedMagnetCandidate[],
  keepAll: boolean,
  formatSize: (sizeInMB: number) => string,
  backupTopN = 3
): MagnetResult | null {
  if (!Array.isArray(candidates) || candidates.length === 0) {
    return null;
  }

  const sortedBySize = [...candidates].sort((left, right) => right.size - left.size);
  const normalizedBackupTopN = Math.max(1, Math.min(10, Number(backupTopN) || 3));

  if (keepAll) {
    return {
      magnet: candidates.map((pair) => pair.magnetLink).join('\n'),
      magnetLinks: candidates.map((pair) => ({
        link: pair.magnetLink,
        size: formatSize(pair.size)
      })),
      backupMagnetLinks: sortedBySize.slice(0, normalizedBackupTopN).map((pair) => ({
        link: pair.magnetLink,
        size: formatSize(pair.size)
      }))
    };
  }

  const selected = selectLargestMagnetCandidate(candidates);
  if (!selected) {
    return null;
  }

  return {
    magnet: selected.magnetLink,
    magnetLinks: [
      {
        link: selected.magnetLink,
        size: formatSize(selected.size)
      }
    ],
    backupMagnetLinks: sortedBySize.slice(0, normalizedBackupTopN).map((pair) => ({
      link: pair.magnetLink,
      size: formatSize(pair.size)
    }))
  };
}
