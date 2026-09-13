/**
 * Output file handling:
 * - maintain incremental writes and dedupe for filmData.json
 * - maintain pure magnet output in magnet-links.txt
 * - keep internal backups outside the user-facing output directory
 */
import fs from 'fs';
import path from 'path';
import crypto from 'crypto';
import { FilmData, MagnetLink } from '../types/interfaces';
import {
  extractFilmId,
  normalizeSourceLink,
  normalizeTitle,
  serializeMagnetLinks
} from './filmIdentity';
import logger from './logger';

const UTF8_BOM = Buffer.from([0xef, 0xbb, 0xbf]);
const UNFINISHED_REPORT_FILENAME = '\u672A\u5B8C\u6210\u756A\u53F7.txt';

function writeUTF8TextFile(filePath: string, content: string): void {
  fs.writeFileSync(filePath, Buffer.concat([UTF8_BOM, Buffer.from(content, 'utf8')]));
}

class FileHandler {
  private outputDir: string;
  private jsonFilename: string;
  private magnetFilename: string;
  private unfinishedFilename: string;
  private backupDir: string;
  private recordsCache: FilmData[] = [];
  private recordIndexByFilmId = new Map<string, number>();
  private recordIndexBySourceLink = new Map<string, number>();
  private cacheLoaded = false;
  private flushTimer: NodeJS.Timeout | null = null;
  private flushInFlight: Promise<void> | null = null;
  private dirty = false;
  private bufferedChanges = 0;
  private readonly flushThreshold = 12;
  private readonly flushDelayMs = 1200;
  private actressCountFilterThreshold = 0;
  private filmCodeFilterThreshold = '';
  private minReleaseDate = '';
  private magnetOutputCount = 0;

  constructor(
    outputDir: string,
    options: {
      actressCountFilterThreshold?: number;
      filmCodeFilterThreshold?: string;
      minReleaseDate?: string;
    } = {}
  ) {
    if (typeof outputDir !== 'string' || outputDir.trim() === '') {
      throw new Error(`Invalid output directory provided: "${outputDir}".`);
    }

    this.outputDir = outputDir;
    this.actressCountFilterThreshold = Math.max(
      0,
      Number.isFinite(Number(options.actressCountFilterThreshold))
        ? Number(options.actressCountFilterThreshold)
        : 0
    );
    this.filmCodeFilterThreshold = this.normalizeFilmCodeFilterThreshold(options.filmCodeFilterThreshold);
    this.minReleaseDate = String(options.minReleaseDate || '').trim();
    this.jsonFilename = 'filmData.json';
    this.magnetFilename = 'magnet-links.txt';
    this.unfinishedFilename = UNFINISHED_REPORT_FILENAME;
    this.backupDir = this.resolveBackupDir(outputDir);
    void this.ensureOutputDirExists();
  }

  /** Actual magnet-links.txt line count from the latest flush. */
  getMagnetOutputCount() {
    return this.magnetOutputCount;
  }

  private async ensureOutputDirExists(): Promise<void> {
    await fs.promises.mkdir(this.outputDir, { recursive: true });
    await fs.promises.mkdir(this.backupDir, { recursive: true });
  }

  public syncUnfinishedItemsReport(lines: string[]): void {
    const filePath = path.join(this.outputDir, this.unfinishedFilename);
    const normalizedLines = Array.from(
      new Set(
        (Array.isArray(lines) ? lines : [])
          .map((item) => String(item || '').trim())
          .filter(Boolean)
      )
    );

    if (normalizedLines.length === 0) {
      if (fs.existsSync(filePath)) {
        fs.rmSync(filePath, { force: true });
      }
      return;
    }

    writeUTF8TextFile(filePath, `${normalizedLines.join('\r\n')}\r\n`);
    logger.info(`FileHandler: unfinished items report saved: ${filePath}`);
  }

  public cleanupLegacyOutputArtifacts(): void {
    const legacyTargets = [
      path.join(this.outputDir, 'task-state.json'),
      path.join(this.outputDir, 'validation-report.json'),
      path.join(this.outputDir, 'backups')
    ];

    for (const target of legacyTargets) {
      if (!fs.existsSync(target)) {
        continue;
      }

      fs.rmSync(target, { recursive: true, force: true });
    }
  }

  public async writeFilmDataToFile(data: FilmData): Promise<void> {
    if (typeof data !== 'object' || data === null) {
      throw new Error(`Invalid data provided: "${data}".`);
    }

    logger.debug(`FileHandler: start writing film data, title: ${data.title}`);

    try {
      this.loadCacheIfNeeded();
      const normalizedIncoming = this.normalizeFilmData(data);
      const duplicateIndex = this.findDuplicateIndex(this.recordsCache, normalizedIncoming);

      if (duplicateIndex === -1) {
        this.recordsCache.push(normalizedIncoming);
        this.indexRecord(normalizedIncoming, this.recordsCache.length - 1);
        logger.info(`FileHandler: appended new film data: ${data.title}`);
      } else {
        const mergedRecord = this.mergeFilmData(this.recordsCache[duplicateIndex], normalizedIncoming);
        if (this.isSameFilmData(this.recordsCache[duplicateIndex], mergedRecord)) {
          logger.debug(`FileHandler: duplicate film data unchanged, skipped flush: ${data.title}`);
          return;
        }

        this.recordsCache[duplicateIndex] = mergedRecord;
        this.rebuildIndexes(this.recordsCache);
        logger.info(`FileHandler: merged duplicate film data: ${data.title}`);
      }

      this.dirty = true;
      this.bufferedChanges += 1;

      if (this.bufferedChanges >= this.flushThreshold) {
        await this.flush(true);
      } else {
        this.scheduleDeferredFlush();
      }
    } catch (error) {
      logger.error(
        `FileHandler: failed to write film data: ${error instanceof Error ? error.message : String(error)}`
      );
      logger.error(`FileHandler: error details: ${error instanceof Error ? error.stack : String(error)}`);
      throw error;
    }
  }

  public getPersistedFilmData(data: Partial<FilmData>): FilmData | null {
    if (typeof data !== 'object' || data === null) {
      return null;
    }

    this.loadCacheIfNeeded();
    const normalizedIncoming = this.normalizeFilmData(data);
    const duplicateIndex = this.findDuplicateIndex(this.recordsCache, normalizedIncoming);
    if (duplicateIndex === -1) {
      return null;
    }

    const record = this.recordsCache[duplicateIndex];
    return {
      ...record,
      category: [...(record.category || [])],
      actress: [...(record.actress || [])],
      magnetLinks: this.mergeMagnetLinks(record.magnetLinks || [], []),
      backupMagnetLinks: this.mergeMagnetLinks(record.backupMagnetLinks || [], [])
    };
  }

  public async flush(force = false): Promise<void> {
    if (!this.dirty && !force) {
      return;
    }

    if (this.flushTimer) {
      clearTimeout(this.flushTimer);
      this.flushTimer = null;
    }

    if (this.flushInFlight) {
      await this.flushInFlight;
      if (!this.dirty || !force) {
        return;
      }
    }

    this.flushInFlight = this.flushToDisk();

    try {
      await this.flushInFlight;
    } finally {
      this.flushInFlight = null;
    }
  }

  public async close(): Promise<void> {
    await this.flush(true);
  }

  public getFilmDataSnapshot(): FilmData[] {
    this.loadCacheIfNeeded();
    return this.recordsCache.map((item) => ({
      ...item,
      category: [...(item.category || [])],
      actress: [...(item.actress || [])],
      magnetLinks: this.mergeMagnetLinks(item.magnetLinks || [], []),
      backupMagnetLinks: this.mergeMagnetLinks(item.backupMagnetLinks || [], [])
    }));
  }

  private loadExistingData(filePath: string): FilmData[] {
    if (!fs.existsSync(filePath)) {
      return [];
    }

    try {
      const fileContent = fs.readFileSync(filePath, 'utf8');
      const parsed = JSON.parse(fileContent);
      if (Array.isArray(parsed)) {
        return parsed.map((item) => this.normalizeFilmData(item));
      }

      return parsed ? [this.normalizeFilmData(parsed)] : [];
    } catch {
      logger.warn(`FileHandler: invalid JSON format in ${filePath}, resetting to empty array`);
      return [];
    }
  }

  private loadCacheIfNeeded(): void {
    if (this.cacheLoaded) {
      return;
    }

    const jsonPath = path.join(this.outputDir, this.jsonFilename);
    this.recordsCache = this.compactRecords(this.loadExistingData(jsonPath));
    this.rebuildIndexes(this.recordsCache);
    this.cacheLoaded = true;
  }

  private scheduleDeferredFlush(): void {
    if (this.flushTimer) {
      return;
    }

    this.flushTimer = setTimeout(() => {
      this.flushTimer = null;
      void this.flush();
    }, this.flushDelayMs);
  }

  private async flushToDisk(): Promise<void> {
    if (!this.dirty) {
      return;
    }

    await this.ensureOutputDirExists();

    const jsonPath = path.join(this.outputDir, this.jsonFilename);
    const compactedData = this.compactRecords(this.recordsCache);
    this.recordsCache = compactedData;
    this.rebuildIndexes(this.recordsCache);
    this.writeJsonOutput(jsonPath, compactedData);
    this.writeMagnetTextOutput(compactedData);
    this.dirty = false;
    this.bufferedChanges = 0;
    logger.info(`FileHandler: flushed ${compactedData.length} film records to disk`);
  }

  private writeJsonOutput(filePath: string, data: FilmData[]): void {
    const hadExistingFile = fs.existsSync(filePath);
    this.createLatestBackup(filePath);
    fs.writeFileSync(filePath, JSON.stringify(data, null, 2), 'utf8');
    if (!hadExistingFile) {
      this.createLatestBackup(filePath);
    }
    logger.info(`FileHandler: film data saved to ${filePath}`);
  }

  private writeMagnetTextOutput(data: FilmData[]): void {
    const magnetPath = path.join(this.outputDir, this.magnetFilename);
    const hadExistingFile = fs.existsSync(magnetPath);
    const uniqueMagnets = Array.from(
      new Set(
        data
          .filter((film) => !film.filteredByActressCount && !film.filteredByFilmCode && !film.filteredByReleaseDate)
          .flatMap((film) =>
            (film.magnetLinks || [])
              .map((magnet) => magnet.link?.trim())
              .filter((link): link is string => Boolean(link))
          )
      )
    );

    this.createLatestBackup(magnetPath);
    fs.writeFileSync(magnetPath, uniqueMagnets.join('\n'), 'utf8');
    this.magnetOutputCount = uniqueMagnets.length;
    if (!hadExistingFile) {
      this.createLatestBackup(magnetPath);
    }
    logger.info(`FileHandler: pure magnet text saved to ${magnetPath}`);
  }

  private createLatestBackup(filePath: string): void {
    if (!fs.existsSync(filePath)) {
      return;
    }

    fs.mkdirSync(this.backupDir, { recursive: true });
    const backupPath = path.join(this.backupDir, `${path.basename(filePath)}.bak`);
    fs.copyFileSync(filePath, backupPath);
  }

  private rebuildIndexes(records: FilmData[]): void {
    this.recordIndexByFilmId.clear();
    this.recordIndexBySourceLink.clear();
    records.forEach((record, index) => this.indexRecord(record, index));
  }

  private indexRecord(record: FilmData, index: number): void {
    const filmId = extractFilmId(record.sourceLink || record.title || serializeMagnetLinks(record.magnetLinks));
    if (filmId) {
      this.recordIndexByFilmId.set(filmId, index);
    }

    const sourceLink = normalizeSourceLink(record.sourceLink);
    if (sourceLink) {
      this.recordIndexBySourceLink.set(sourceLink, index);
    }
  }

  private resolveBackupDir(outputDir: string): string {
    const homeDir =
      (process.platform === 'win32' ? process.env.USERPROFILE : process.env.HOME) || process.cwd();
    const runtimeRoot = process.env.JAV_SCRAPY_FILE_BACKUP_DIR || path.join(homeDir, '.jav-scrapy', 'file-backups');
    const normalizedOutput = path.resolve(outputDir || process.cwd()).toLowerCase();
    const stateId = crypto.createHash('sha1').update(normalizedOutput).digest('hex').slice(0, 16);
    return path.join(runtimeRoot, stateId);
  }

  private findDuplicateIndex(existingData: FilmData[], incomingData: FilmData): number {
    const incomingSourceLink = normalizeSourceLink(incomingData.sourceLink);
    const incomingFilmId = extractFilmId(
      incomingData.sourceLink || incomingData.title || serializeMagnetLinks(incomingData.magnetLinks)
    );

    if (incomingFilmId) {
      const filmIdIndex =
        existingData === this.recordsCache
          ? this.recordIndexByFilmId.get(incomingFilmId) ?? -1
          : existingData.findIndex(
              (item) =>
                extractFilmId(item.sourceLink || item.title || serializeMagnetLinks(item.magnetLinks)) ===
                incomingFilmId
            );
      if (filmIdIndex !== -1) {
        return filmIdIndex;
      }
    }

    if (incomingSourceLink) {
      const sourceLinkIndex =
        existingData === this.recordsCache
          ? this.recordIndexBySourceLink.get(incomingSourceLink) ?? -1
          : existingData.findIndex((item) => normalizeSourceLink(item.sourceLink) === incomingSourceLink);
      if (sourceLinkIndex !== -1) {
        return sourceLinkIndex;
      }
    }

    const incomingMagnets = new Set(serializeMagnetLinks(incomingData.magnetLinks).split('|').filter(Boolean));
    if (incomingMagnets.size > 0) {
      const magnetIndex = existingData.findIndex((item) => {
        const existingMagnets = serializeMagnetLinks(item.magnetLinks).split('|').filter(Boolean);
        return existingMagnets.some((link) => incomingMagnets.has(link));
      });

      if (magnetIndex !== -1) {
        return magnetIndex;
      }
    }

    const normalizedIncomingTitle = normalizeTitle(incomingData.title);
    if (normalizedIncomingTitle.length > 0) {
      const titleIndex = existingData.findIndex((item) => {
        const normalizedExistingTitle = normalizeTitle(item.title);
        return normalizedIncomingTitle === normalizedExistingTitle;
      });

      if (titleIndex !== -1) {
        return titleIndex;
      }
    }

    return -1;
  }

  private mergeFilmData(existingItem: FilmData, incomingData: FilmData): FilmData {
    const normalizedExisting = this.normalizeFilmData(existingItem);
    const normalizedIncoming = this.normalizeFilmData(incomingData);
    const actress = this.mergeUniqueText(normalizedExisting.actress, normalizedIncoming.actress);
    const actressCount = Math.max(
      normalizedExisting.actressCount || 0,
      normalizedIncoming.actressCount || 0,
      actress.length
    );
    const merged = this.applyActressCountFilter({
      title:
        normalizedIncoming.title.length >= normalizedExisting.title.length
          ? normalizedIncoming.title
          : normalizedExisting.title,
      sourceLink: normalizedExisting.sourceLink || normalizedIncoming.sourceLink,
      coverImage: this.pickPreferredCoverImage(
        normalizedExisting.coverImage,
        normalizedIncoming.coverImage
      ),
      category: this.mergeUniqueText(normalizedExisting.category, normalizedIncoming.category),
      actress,
      releaseDate: normalizedExisting.releaseDate || normalizedIncoming.releaseDate,
      maker: normalizedExisting.maker || normalizedIncoming.maker,
      label: normalizedExisting.label || normalizedIncoming.label,
      series: normalizedExisting.series || normalizedIncoming.series,
      actressCount,
      magnetLinks: this.mergeMagnetLinks(normalizedExisting.magnetLinks, normalizedIncoming.magnetLinks),
      backupMagnetLinks: this.mergeMagnetLinks(
        normalizedExisting.backupMagnetLinks || normalizedExisting.magnetLinks,
        normalizedIncoming.backupMagnetLinks || normalizedIncoming.magnetLinks
      )
    });

    return merged;
  }

  private compactRecords(records: FilmData[]): FilmData[] {
    const compacted: FilmData[] = [];

    for (const record of records.map((item) => this.normalizeFilmData(item))) {
      const duplicateIndex = this.findDuplicateIndex(compacted, record);
      if (duplicateIndex === -1) {
        compacted.push(record);
      } else {
        compacted[duplicateIndex] = this.mergeFilmData(compacted[duplicateIndex], record);
      }
    }

    return compacted;
  }

  private normalizeFilmData(data: Partial<FilmData>): FilmData {
    const actress = this.mergeUniqueText(data.actress || [], []);
    return this.applyActressCountFilter({
      title: (data.title || '').trim(),
      sourceLink: data.sourceLink?.trim(),
      coverImage: data.coverImage?.trim() || undefined,
      category: this.mergeUniqueText(data.category || [], []),
      actress,
      releaseDate: data.releaseDate?.trim() || undefined,
      maker: data.maker?.trim() || undefined,
      label: data.label?.trim() || undefined,
      series: data.series?.trim() || undefined,
      actressCount: this.normalizeActressCount(data.actressCount, actress.length),
      filteredByActressCount: Boolean(data.filteredByActressCount),
      filterReason: String(data.filterReason || '').trim() || undefined,
      magnetLinks: this.mergeMagnetLinks(data.magnetLinks || [], []),
      backupMagnetLinks: this.mergeMagnetLinks(data.backupMagnetLinks || data.magnetLinks || [], [])
    });
  }

  private normalizeActressCount(value: unknown, fallback: number): number {
    const parsed = Number(value);
    if (Number.isFinite(parsed) && parsed >= 0) {
      return parsed;
    }

    return Math.max(0, fallback);
  }

  private applyActressCountFilter(data: Partial<FilmData>): FilmData {
    const actress = this.mergeUniqueText(data.actress || [], []);
    const actressCount = this.normalizeActressCount(data.actressCount, actress.length);
    const filteredByActressCount =
      this.actressCountFilterThreshold > 0 && actressCount > this.actressCountFilterThreshold;
    const filteredByFilmCode = this.isFilmCodeFiltered(data.title);
    const releaseDate = data.releaseDate?.trim() || undefined;
    const filteredByReleaseDate = this.isReleaseDateFiltered(releaseDate);

    const reasons: string[] = [];
    if (filteredByActressCount) {
      reasons.push('actress-count');
    }
    if (filteredByFilmCode) {
      reasons.push('film-code');
    }
    if (filteredByReleaseDate) {
      reasons.push('release-date');
    }

    return {
      title: String(data.title || '').trim(),
      sourceLink: data.sourceLink?.trim(),
      coverImage: data.coverImage?.trim() || undefined,
      category: this.mergeUniqueText(data.category || [], []),
      actress,
      releaseDate,
      actressCount,
      filteredByActressCount,
      filteredByFilmCode,
      filteredByReleaseDate,
      filterReason: reasons.length > 0 ? reasons.join(',') : undefined,
      magnetLinks: this.mergeMagnetLinks(data.magnetLinks || [], []),
      backupMagnetLinks: this.mergeMagnetLinks(data.backupMagnetLinks || data.magnetLinks || [], [])
    };
  }

  // 发行日期过滤：记录日期早于配置的最低日期时排除（只影响磁力输出，
  // 记录本身保留在 filmData.json 供审计）。两侧都归一成纯数字后按字典序比较。
  private isReleaseDateFiltered(releaseDate: unknown): boolean {
    if (!this.minReleaseDate) {
      return false;
    }
    const recordDigits = String(releaseDate || '').replace(/\D+/g, '');
    const minDigits = this.minReleaseDate.replace(/\D+/g, '');
    if (!recordDigits || !minDigits) {
      return false;
    }
    return recordDigits.padEnd(8, '0') < minDigits.padEnd(8, '0');
  }

  private normalizeFilmCodeFilterThreshold(value: unknown): string {
    // 兼容英文逗号、中文逗号、顿号、中英分号与空白分隔，与整理板块的番号过滤口径一致。
    return String(value || '')
      .split(/[,，、;；\s]+/)
      .map((part) => part.trim())
      .filter((part) => part.length > 0)
      .join(',');
  }

  private isFilmCodeFiltered(title: unknown): boolean {
    if (!this.filmCodeFilterThreshold) {
      return false;
    }

    const normalizedTitle = String(title || '').toLowerCase();
    if (!normalizedTitle) {
      return false;
    }

    const substrings = this.filmCodeFilterThreshold.split(',').map((part) => part.trim().toLowerCase());
    return substrings.some((substring) => substring.length > 0 && normalizedTitle.includes(substring));
  }

  private pickPreferredCoverImage(
    existingCoverImage?: string,
    incomingCoverImage?: string
  ): string | undefined {
    const normalizedExisting = String(existingCoverImage || '').trim();
    const normalizedIncoming = String(incomingCoverImage || '').trim();

    if (!normalizedExisting) {
      return normalizedIncoming || undefined;
    }

    if (!normalizedIncoming) {
      return normalizedExisting;
    }

    const existingIsAbsolute = /^https?:\/\//i.test(normalizedExisting);
    const incomingIsAbsolute = /^https?:\/\//i.test(normalizedIncoming);

    if (incomingIsAbsolute && !existingIsAbsolute) {
      return normalizedIncoming;
    }

    if (existingIsAbsolute && !incomingIsAbsolute) {
      return normalizedExisting;
    }

    return normalizedIncoming.length >= normalizedExisting.length
      ? normalizedIncoming
      : normalizedExisting;
  }

  private mergeUniqueText(primary: string[], secondary: string[]): string[] {
    return Array.from(
      new Set(
        [...primary, ...secondary]
          .map((item) => String(item || '').trim())
          .filter(Boolean)
      )
    );
  }

  private mergeMagnetLinks(primary: MagnetLink[] = [], secondary: MagnetLink[] = []): MagnetLink[] {
    const map = new Map<string, MagnetLink>();

    for (const item of [...primary, ...secondary]) {
      const link = item?.link?.trim();
      if (!link) {
        continue;
      }

      const existing = map.get(link);
      const size = String(item.size || '').trim() || existing?.size || '';
      const displayName = String(item.displayName || '').trim() || existing?.displayName || '';
      map.set(link, {
        link,
        size,
        ...(displayName ? { displayName } : {})
      });
    }

    return Array.from(map.values());
  }

  private isSameFilmData(left: FilmData, right: FilmData): boolean {
    return JSON.stringify(this.normalizeFilmData(left)) === JSON.stringify(this.normalizeFilmData(right));
  }
}

export default FileHandler;
