import { FilmData, MagnetLink } from '../types/interfaces';

function normalizeFilmId(rawValue: string): string {
  const compactValue = String(rawValue || '')
    .toUpperCase()
    .trim()
    .replace(/[_\s]+/g, '-')
    .replace(/-+/g, '-');
  const match = compactValue.match(/^([A-Z]{2,8})-?(\d{2,6})([A-Z]*)$/);

  if (!match) {
    return compactValue;
  }

  const [, prefix, digits, suffix] = match;
  return `${prefix}-${digits}${suffix}`.replace(/-+/g, '-');
}

export function extractFilmId(value: string | null | undefined): string | null {
  const normalizedValue = String(value || '').toUpperCase();
  const directMatch = normalizedValue.match(/([A-Z]{2,8}-?\d{2,6}[A-Z]*)/);
  if (directMatch?.[1]) {
    return normalizeFilmId(directMatch[1]);
  }

  try {
    const parsedUrl = new URL(String(value || ''));
    const pathname = parsedUrl.pathname.split('/').filter(Boolean).pop() || '';
    const pathMatch = pathname.toUpperCase().match(/([A-Z]{2,8}-?\d{2,6}[A-Z]*)/);
    return pathMatch?.[1] ? normalizeFilmId(pathMatch[1]) : null;
  } catch {
    return null;
  }
}

export function normalizeSourceLink(sourceLink?: string | null): string {
  const rawValue = String(sourceLink || '').trim();
  if (!rawValue) {
    return '';
  }

  try {
    const parsedUrl = new URL(rawValue);
    const normalizedPath = parsedUrl.pathname.replace(/\/+$/, '') || '/';
    return `${parsedUrl.origin}${normalizedPath}`.toLowerCase();
  } catch {
    return rawValue.replace(/[?#].*$/, '').replace(/\/+$/, '').toLowerCase();
  }
}

export function normalizeTitle(title: string | null | undefined): string {
  return String(title || '')
    .toLowerCase()
    .replace(/[^\w\s\u4e00-\u9fff]/g, '')
    .replace(/\s+/g, ' ')
    .trim();
}

export function serializeMagnetLinks(magnetLinks: MagnetLink[] | undefined | null): string {
  if (!Array.isArray(magnetLinks) || magnetLinks.length === 0) {
    return '';
  }

  return magnetLinks
    .map((item) => item?.link?.trim())
    .filter((item): item is string => Boolean(item))
    .sort()
    .join('|');
}

export function createFilmIdentityKey(record: Partial<FilmData>): string {
  const filmId = extractFilmId(record.sourceLink || record.title || serializeMagnetLinks(record.magnetLinks));
  if (filmId) {
    return filmId;
  }

  const normalizedSourceLink = normalizeSourceLink(record.sourceLink);
  if (normalizedSourceLink) {
    return normalizedSourceLink;
  }

  const normalizedTitle = normalizeTitle(record.title);
  if (normalizedTitle) {
    return normalizedTitle;
  }

  return serializeMagnetLinks(record.magnetLinks);
}
