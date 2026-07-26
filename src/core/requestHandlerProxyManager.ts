import tunnel from 'tunnel';
import type { Config } from '../types/interfaces';
import type { ProxyRuntimeState, RequestConfig } from './requestHandlerTypes';

interface RequestHandlerLoggerLike {
  info(message: string): void;
  warn(message: string): void;
}

interface ProxyManagerOptions {
  config: Config;
  requestConfig: RequestConfig;
  logger: RequestHandlerLoggerLike;
  proxyFailureThreshold: number;
  proxyCooldownMs: number;
  onProxySwitch: (reason: string) => Promise<void>;
}

export class RequestHandlerProxyManager {
  private readonly config: Config;
  private readonly requestConfig: RequestConfig;
  private readonly logger: RequestHandlerLoggerLike;
  private readonly proxyFailureThreshold: number;
  private readonly proxyCooldownMs: number;
  private readonly onProxySwitch: (reason: string) => Promise<void>;
  private readonly invalidProxyValueWarnings = new Set<string>();
  private readonly proxyAgentFailureWarnings = new Set<string>();

  private proxyPool: ProxyRuntimeState[] = [];
  private activeProxyIndex = -1;

  constructor(options: ProxyManagerOptions) {
    this.config = options.config;
    this.requestConfig = options.requestConfig;
    this.logger = options.logger;
    this.proxyFailureThreshold = options.proxyFailureThreshold;
    this.proxyCooldownMs = options.proxyCooldownMs;
    this.onProxySwitch = options.onProxySwitch;
  }

  initializeProxyPool(proxyValue: string | undefined): void {
    const candidates = this.parseProxyCandidates(proxyValue);
    this.proxyPool = candidates.map((url) => ({
      url,
      attempts: 0,
      successes: 0,
      failures: 0,
      consecutiveFailures: 0,
      totalLatencyMs: 0,
      cooldownUntil: 0,
      lastError: ''
    }));

    if (this.proxyPool.length === 0) {
      this.applyActiveProxy(undefined);
      return;
    }

    this.activeProxyIndex = 0;
    this.applyActiveProxy(this.proxyPool[0].url);
    if (this.proxyPool.length > 1) {
      this.logger.info(`RequestHandler: 已启用代理池，共 ${this.proxyPool.length} 个代理候选。`);
    }
  }

  getProxyPool(): ProxyRuntimeState[] {
    return this.proxyPool;
  }

  getActiveProxyIndex(): number {
    return this.activeProxyIndex;
  }

  parseProxyCandidates(proxyValue: string | undefined): string[] {
    const rawValue = String(proxyValue || '').trim();
    if (!rawValue) {
      return [];
    }

    const seen = new Set<string>();
    const candidates: string[] = [];
    for (const part of rawValue.split(/[\r\n,;]+/)) {
      const normalized = this.normalizeProxyValue(part);
      if (!normalized || seen.has(normalized)) {
        continue;
      }

      seen.add(normalized);
      candidates.push(normalized);
    }

    return candidates;
  }

  normalizeProxyValue(value: string): string | null {
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
      return parsed.toString().replace(/\/$/, '');
    } catch {
      this.warnInvalidProxyValueOnce(rawValue);
      return null;
    }
  }

  applyActiveProxy(proxyUrl?: string): void {
    const normalizedProxy = proxyUrl ? this.normalizeProxyValue(proxyUrl) : null;
    this.requestConfig.proxy = normalizedProxy || undefined;
    this.config.proxy = normalizedProxy || undefined;
  }

  warnInvalidProxyValueOnce(rawValue: string): void {
    const messageKey = String(rawValue || '').trim();
    if (!messageKey || this.invalidProxyValueWarnings.has(messageKey)) {
      return;
    }

    this.invalidProxyValueWarnings.add(messageKey);
    this.logger.warn(`RequestHandler: 已忽略无效代理地址 ${messageKey}`);
  }

  warnProxyAgentFailureOnce(proxyValue: string, reason: string): void {
    const messageKey = `${String(proxyValue || '').trim()}|${String(reason || '').trim()}`;
    if (!messageKey || this.proxyAgentFailureWarnings.has(messageKey)) {
      return;
    }

    this.proxyAgentFailureWarnings.add(messageKey);
    this.logger.warn(`RequestHandler: 代理创建失败，已回退为直连。代理：${proxyValue}，原因：${reason}`);
  }

  getActiveProxyState(): ProxyRuntimeState | null {
    if (this.activeProxyIndex < 0 || this.activeProxyIndex >= this.proxyPool.length) {
      return null;
    }

    return this.proxyPool[this.activeProxyIndex];
  }

  getProxySummary(state: ProxyRuntimeState): string {
    const averageLatency = state.successes > 0 ? Math.round(state.totalLatencyMs / state.successes) : 0;
    return `${state.url} | 成功 ${state.successes} 次 | 失败 ${state.failures} 次 | 连续失败 ${state.consecutiveFailures} 次 | 平均延迟 ${averageLatency}ms`;
  }

  recordProxySuccess(latencyMs: number): void {
    const state = this.getActiveProxyState();
    if (!state) {
      return;
    }

    state.attempts += 1;
    state.successes += 1;
    state.consecutiveFailures = 0;
    state.cooldownUntil = 0;
    state.totalLatencyMs += Math.max(0, latencyMs);
  }

  async recordProxyFailure(error: unknown, context: string): Promise<void> {
    const state = this.getActiveProxyState();
    if (!state) {
      return;
    }

    const message = error instanceof Error ? error.message : String(error || 'unknown');
    if (!this.shouldRotateProxy(message)) {
      return;
    }

    state.attempts += 1;
    state.failures += 1;
    state.consecutiveFailures += 1;
    state.lastError = message;

    if (state.consecutiveFailures < this.proxyFailureThreshold || this.proxyPool.length <= 1) {
      return;
    }

    state.cooldownUntil = Date.now() + this.proxyCooldownMs;
    const switched = await this.switchProxy(`${context} 连续失败 ${state.consecutiveFailures} 次`);
    if (!switched) {
      this.logger.warn(`RequestHandler: 当前代理连续失败，但暂时没有可切换的备用代理。${this.getProxySummary(state)}`);
    }
  }

  shouldRotateProxy(message: string): boolean {
    const normalized = String(message || '').toLowerCase();
    return [
      'timed out',
      'timeout',
      'econnreset',
      'enotfound',
      'ehostunreach',
      'eai_again',
      'err_connection',
      'proxy',
      'socket hang up',
      '403',
      '429'
    ].some((token) => normalized.includes(token));
  }

  async switchProxy(reason: string): Promise<boolean> {
    if (this.proxyPool.length <= 1) {
      return false;
    }

    const now = Date.now();
    const nextIndex = this.selectNextProxyIndex(now);
    if (nextIndex === -1 || nextIndex === this.activeProxyIndex) {
      return false;
    }

    const previous = this.getActiveProxyState();
    const next = this.proxyPool[nextIndex];
    this.activeProxyIndex = nextIndex;
    this.applyActiveProxy(next.url);
    this.logger.warn(
      `RequestHandler: 检测到当前代理不稳定，已切换到备用代理。原因：${reason}。当前代理：${next.url}${
        previous ? `，上一个代理状态：${this.getProxySummary(previous)}` : ''
      }`
    );
    await this.onProxySwitch('代理已自动切换');
    return true;
  }

  selectNextProxyIndex(now = Date.now()): number {
    if (this.proxyPool.length === 0) {
      return -1;
    }

    for (let offset = 1; offset <= this.proxyPool.length; offset += 1) {
      const index = (this.activeProxyIndex + offset + this.proxyPool.length) % this.proxyPool.length;
      const candidate = this.proxyPool[index];
      if (candidate.cooldownUntil <= now) {
        return index;
      }
    }

    let earliestIndex = -1;
    let earliestCooldown = Number.POSITIVE_INFINITY;
    this.proxyPool.forEach((candidate, index) => {
      if (candidate.cooldownUntil < earliestCooldown) {
        earliestCooldown = candidate.cooldownUntil;
        earliestIndex = index;
      }
    });
    return earliestIndex;
  }

  createProxyAgent(proxyUrl: string): any | null {
    const normalizedProxy = this.normalizeProxyValue(proxyUrl);
    if (!normalizedProxy) {
      this.warnProxyAgentFailureOnce(proxyUrl, '代理地址格式无效');
      this.requestConfig.proxy = undefined;
      this.config.proxy = undefined;
      return null;
    }

    try {
      const url = new URL(normalizedProxy);
      const agentOptions = {
        proxy: {
          host: url.hostname,
          port: parseInt(url.port, 10)
        }
      };
      return url.protocol === 'http:' ? tunnel.httpsOverHttp(agentOptions) : tunnel.httpsOverHttps(agentOptions);
    } catch (error) {
      this.warnProxyAgentFailureOnce(proxyUrl, error instanceof Error ? error.message : String(error));
      this.requestConfig.proxy = undefined;
      this.config.proxy = undefined;
      return null;
    }
  }
}
