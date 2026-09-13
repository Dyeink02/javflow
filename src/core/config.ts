import fs from 'fs';
import path from 'path';
import chalk from 'chalk';
import type { Command } from 'commander';
import { Config, RuntimeOptions } from '../types/interfaces';
import { getSystemProxy, parseProxyServer } from '../utils/systemProxy';
import { ErrorHandler } from '../utils/errorHandler';
import logger from './logger';
import { DEFAULT_CONFIG, BASE_URL, DEFAULT_HEADERS } from './constants';

/**
 * 配置装配器：
 * 把默认值、系统代理、本地防屏蔽地址缓存、CLI/桌面端输入整合成一次运行配置。
 */
class ConfigManager {
  private config: Config;
  private configPath: string;

  constructor() {
    this.config = {
      retryCount: DEFAULT_CONFIG.retryCount,
      retryDelay: DEFAULT_CONFIG.retryDelay,
      BASE_URL,
      baseUrl: BASE_URL,
      searchUrl: DEFAULT_CONFIG.searchUrl,
      parallel: DEFAULT_CONFIG.parallel,
      headers: {
        ...DEFAULT_HEADERS
      },
      output: process.cwd(),
      timeout: DEFAULT_CONFIG.timeout,
      search: null,
      base: null,
      nomag: false,
      allmag: false,
      nopic: false,
      limit: 0,
      totalPages: 0,
      itemsPerPage: 30,
      delay: 2,
      strictSSL: DEFAULT_CONFIG.strictSSL,
      proxy: undefined,
      useCloudflareBypass: false,
      secondValidation: true,
      taskTemplate: 'balanced',
      magnetExcludeKeywords: '',
      magnetContentValidation: false,
      supplementMagnetTopN: 3,
      actressCountFilterThreshold: 0,
      filmCodeFilterThreshold: '',
      minReleaseDate: '',
      demoMode: 'base',
      demoLabel: '',
      productDisplayName: 'JavFlow',
      puppeteerPool: {
        maxSize: Math.max(1, Math.min(2, Math.floor(DEFAULT_CONFIG.parallel / 2))),
        maxIdleTime: 3 * 60 * 1000,
        healthCheckInterval: 30 * 1000,
        requestTimeout: 60 * 1000,
        retryAttempts: 3
      }
    };

    const homeDir =
      (process.platform === 'win32' ? process.env.USERPROFILE : process.env.HOME) || process.cwd();
    this.configPath = path.join(homeDir, '.jav-scrapy.config.json');
  }

  public async updateFromProgram(program: Command): Promise<void> {
    const options = program.opts();

    await this.updateFromOptions({
      parallel: options.parallel,
      timeout: options.timeout,
      output: options.output,
      search: options.search,
      base: options.base,
      proxy: options.proxy,
      nomag: options.nomag,
      allmag: options.allmag,
      nopic: options.nopic,
      limit: options.limit,
      totalPages: options.totalPages,
      itemsPerPage: options.itemsPerPage,
      delay: options.delay,
      cookies: options.cookies,
      cloudflare: options.cloudflare,
      secondValidation: options.secondValidation,
      taskTemplate: options.taskTemplate,
      magnetExcludeKeywords: options.magnetExcludeKeywords || options.magnetExclude,
      magnetContentValidation: options.magnetContentValidation,
      supplementMagnetTopN: options.supplementMagnetTopN,
      actressCountFilterThreshold: options.actressCountFilterThreshold,
      filmCodeFilterThreshold: options.filmCodeFilterThreshold,
      minReleaseDate: options.minReleaseDate,
      strictSSL:
        Object.prototype.hasOwnProperty.call(options, 'strictSSL') && options.strictSSL === false
          ? false
          : null
    });
  }

  public async updateFromOptions(options: Partial<RuntimeOptions>): Promise<void> {
    const systemProxy = await getSystemProxy();
    const proxyEnabled = systemProxy.enabled && !!systemProxy.server;
    logger.info(`System proxy detected: enabled=${proxyEnabled}${proxyEnabled ? ', configured' : ''}`);

    if (systemProxy.enabled && systemProxy.server) {
      this.config.proxy = this.normalizeProxyInput(parseProxyServer(systemProxy.server));
    }

    // 若本地已有备用网址缓存，优先随机选一个作为基础域名，提高可用性。
    const antiBlockUrls = this.loadAntiBlockUrls();
    if (antiBlockUrls.length > 0) {
      const selectedUrl = antiBlockUrls[Math.floor(Math.random() * antiBlockUrls.length)];
      this.applyBaseUrl(selectedUrl);
      logger.info(`Using anti-block base URL: ${chalk.underline.blue(selectedUrl)}`);
    }

    if (this.hasValue(options.proxy)) {
      const normalizedProxy = this.normalizeProxyInput(options.proxy);
      if (normalizedProxy) {
        this.config.proxy = normalizedProxy;
      } else {
        logger.warn(
          this.config.proxy
            ? '手动代理输入无效，已自动忽略并继续沿用系统代理。'
            : '手动代理输入无效，已自动忽略并改为直连运行。'
        );
      }
    }

    if (this.hasValue(options.cookies)) {
      this.applyCookies(String(options.cookies));
    }

    this.config.parallel = this.parseNumberOption(options.parallel, DEFAULT_CONFIG.parallel);
    this.config.timeout = this.parseNumberOption(options.timeout, DEFAULT_CONFIG.timeout);
    this.config.limit = this.parseNumberOption(options.limit, 0);
    this.config.totalPages = this.parseNumberOption(options.totalPages, 0);
    this.config.itemsPerPage = this.parseNumberOption(options.itemsPerPage, 30);
    this.config.delay = this.parseNumberOption(options.delay, 2);
    this.config.actressCountFilterThreshold = Math.max(
      0,
      this.parseNumberOption(options.actressCountFilterThreshold, 0)
    );
    logger.info(
      `ConfigManager actress threshold=${this.config.actressCountFilterThreshold} input=${String(options.actressCountFilterThreshold ?? '')}`
    );

    this.config.filmCodeFilterThreshold = this.normalizeFilmCodeFilterThreshold(options.filmCodeFilterThreshold);
    this.config.minReleaseDate = String(options.minReleaseDate ?? this.config.minReleaseDate ?? '').trim();
    logger.info(
      `ConfigManager film code filter=${this.config.filmCodeFilterThreshold} input=${String(options.filmCodeFilterThreshold ?? '')}`
    );

    if (this.hasValue(options.output)) {
      this.config.output = String(options.output);
    }

    if (this.hasValue(options.search)) {
      this.config.search = String(options.search);
    }

    if (this.hasValue(options.base)) {
      this.applyBaseUrl(String(options.base));
    }

    if (typeof options.nomag === 'boolean') {
      this.config.nomag = options.nomag;
    }

    if (typeof options.allmag === 'boolean') {
      this.config.allmag = options.allmag;
    }

    if (typeof options.nopic === 'boolean') {
      this.config.nopic = options.nopic;
    }

    if (typeof options.cloudflare === 'boolean') {
      this.config.useCloudflareBypass = options.cloudflare;
    }

    // The desktop JAV workflow always reconciles its final result. Retain the
    // option in the public shape for old callers, but do not allow a stale
    // configuration payload to turn this integrity check off.
    this.config.secondValidation = true;

    if (this.hasValue(options.taskTemplate)) {
      this.config.taskTemplate = String(options.taskTemplate);
    }

    if (this.hasValue(options.magnetExcludeKeywords)) {
      this.config.magnetExcludeKeywords = String(options.magnetExcludeKeywords);
    }

    if (typeof options.magnetContentValidation === 'boolean') {
      this.config.magnetContentValidation = options.magnetContentValidation;
    }

    if (this.hasValue(options.supplementMagnetTopN)) {
      const parsedTopN = this.parseNumberOption(options.supplementMagnetTopN, 3);
      this.config.supplementMagnetTopN = Math.max(1, Math.min(10, parsedTopN));
    }

    if (this.hasValue(options.demoMode)) {
      this.config.demoMode = String(options.demoMode);
    }

    if (this.hasValue(options.demoLabel)) {
      this.config.demoLabel = String(options.demoLabel);
    }

    if (this.hasValue(options.productDisplayName)) {
      this.config.productDisplayName = String(options.productDisplayName);
    }

    if (typeof options.strictSSL === 'boolean') {
      this.config.strictSSL = options.strictSSL;
      if (!options.strictSSL) {
        logger.warn('Strict SSL verification disabled for this run.');
      }
    }
  }

  public updateConfig(newConfig: Partial<Config>): void {
    this.config = { ...this.config, ...newConfig };

    try {
      fs.writeFileSync(this.configPath, JSON.stringify(this.config, null, 2));
    } catch (error) {
      ErrorHandler.handleFileError(error, 'update config file');
    }
  }

  public getConfig(): Config {
    return this.config;
  }

  private applyBaseUrl(value: string): void {
    const normalized = value.trim().replace(/\/$/, '');
    this.config.base = normalized;
    this.config.baseUrl = normalized;
    this.config.BASE_URL = normalized;

    try {
      const parsed = new URL(normalized);
      this.config.headers.Referer = `${parsed.protocol}//${parsed.hostname}/`;
    } catch (error) {
      logger.warn(`Failed to parse base URL "${value}", keeping existing referer.`);
    }
  }

  private applyCookies(cookieString: string): void {
    const cookiePairs = cookieString
      .split(';')
      .map((item) => item.trim())
      .filter(Boolean);

    const parsedCookies: Record<string, string> = {};
    for (const pair of cookiePairs) {
      const [key, ...rest] = pair.split('=');
      if (!key || rest.length === 0) {
        continue;
      }

      parsedCookies[key.trim()] = rest.join('=').trim();
    }

    if (Object.keys(parsedCookies).length > 0) {
      this.config.headers.Cookie = cookieString;
      logger.info(`Manual cookies loaded: ${Object.keys(parsedCookies).join(', ')}`);
    }
  }

  private loadAntiBlockUrls(): string[] {
    const homeDir =
      (process.platform === 'win32' ? process.env.USERPROFILE : process.env.HOME) || process.cwd();
    const antiBlockFile = path.join(homeDir, '.jav-scrapy-antiblock-urls.json');

    try {
      if (!fs.existsSync(antiBlockFile)) {
        return [];
      }

      const data = JSON.parse(fs.readFileSync(antiBlockFile, 'utf-8'));
      if (Array.isArray(data)) {
        return data.filter((item) => typeof item === 'string' && item.trim().length > 0);
      }
    } catch (error) {
      logger.error(
        `Failed to load local anti-block URLs: ${
          error instanceof Error ? error.message : String(error)
        }`
      );
    }

    return [];
  }

  private parseNumberOption(
    value: number | string | null | undefined,
    fallback: number
  ): number {
    if (!this.hasValue(value)) {
      return fallback;
    }

    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : fallback;
  }

  private normalizeFilmCodeFilterThreshold(
    value: number | string | null | undefined
  ): string {
    if (!this.hasValue(value)) {
      return '';
    }

    // 兼容英文逗号、中文逗号、顿号、中英分号与空白分隔，与整理板块的番号过滤口径一致。
    return String(value)
      .split(/[,，、;；\s]+/)
      .map((part) => part.trim())
      .filter((part) => part.length > 0)
      .join(',');
  }

  private normalizeProxyInput(value: unknown): string | undefined {
    if (!this.hasValue(value)) {
      return undefined;
    }

    const validCandidates: string[] = [];
    const invalidCandidates: string[] = [];
    const seen = new Set<string>();

    for (const part of String(value).split(/[\r\n,;]+/)) {
      const normalized = this.normalizeProxyCandidate(part);
      const rawCandidate = String(part || '').trim();

      if (!rawCandidate) {
        continue;
      }

      if (!normalized) {
        invalidCandidates.push(rawCandidate);
        continue;
      }

      if (seen.has(normalized)) {
        continue;
      }

      seen.add(normalized);
      validCandidates.push(normalized);
    }

    if (invalidCandidates.length > 0) {
      logger.warn(`已忽略无效代理配置：${invalidCandidates.join('、')}`);
    }

    return validCandidates.length > 0 ? validCandidates.join('\n') : undefined;
  }

  private normalizeProxyCandidate(value: string): string | null {
    const rawValue = String(value || '').trim();
    if (!rawValue) {
      return null;
    }

    const proxyValue = /^[a-z][a-z0-9+.-]*:\/\//i.test(rawValue)
      ? rawValue
      : /^[^/\s]+:\d+$/.test(rawValue)
        ? `http://${rawValue}`
        : rawValue;

    try {
      const parsed = new URL(proxyValue);
      if (!parsed.hostname) {
        return null;
      }

      return parsed.toString().replace(/\/$/, '');
    } catch {
      return null;
    }
  }

  private hasValue(value: unknown): boolean {
    return value !== undefined && value !== null && String(value).trim() !== '';
  }
}

export default ConfigManager;
export { Config };
