import fs from 'fs';
import path from 'path';
import logger from './logger';
import type { AjaxBaseOriginHealthState } from './requestHandlerTypes';

export function normalizeRequestHandlerBaseOrigin(value: string | null | undefined): string | null {
  if (!value) {
    return null;
  }

  try {
    const parsed = new URL(value);
    return `${parsed.protocol}//${parsed.host}`;
  } catch {
    return null;
  }
}

function isOfficialJavbusOrigin(value: string | null | undefined): boolean {
  const normalized = normalizeRequestHandlerBaseOrigin(value);
  if (!normalized) {
    return false;
  }

  try {
    const parsed = new URL(normalized);
    return /(^|\.)javbus\.com$/i.test(parsed.hostname);
  } catch {
    return false;
  }
}

export function collectAjaxBaseOrigins(params: {
  preferredOrigin?: string | null;
  configuredBase?: string | null;
  antiBlockOrigins?: string[];
  knownMirrorOrigins?: string[];
}): string[] {
  const candidates: string[] = [];
  const addCandidate = (value: string | null | undefined) => {
    const normalized = normalizeRequestHandlerBaseOrigin(value);
    if (normalized && !candidates.includes(normalized)) {
      candidates.push(normalized);
    }
  };

  addCandidate(params.preferredOrigin);

  const antiBlockOrigins = params.antiBlockOrigins || [];
  const preferAntiBlockFirst =
    antiBlockOrigins.length > 0 && isOfficialJavbusOrigin(params.configuredBase);

  if (preferAntiBlockFirst) {
    for (const value of antiBlockOrigins) {
      addCandidate(value);
    }
    addCandidate(params.configuredBase);
  } else {
    addCandidate(params.configuredBase);
    for (const value of antiBlockOrigins) {
      addCandidate(value);
    }
  }

  for (const value of params.knownMirrorOrigins || []) {
    addCandidate(value);
  }

  return candidates;
}

export function createBaseOriginHealthState(origin: string): AjaxBaseOriginHealthState {
  return {
    origin,
    attempts: 0,
    successes: 0,
    failures: 0,
    consecutiveFailures: 0,
    lastLatencyMs: null,
    lastSuccessAt: 0,
    lastFailureAt: 0,
    cooldownUntil: 0,
    lastError: ''
  };
}

export function ensureBaseOriginHealthState(
  healthMap: Map<string, AjaxBaseOriginHealthState>,
  origin: string | null | undefined,
  normalizeOrigin: (value: string | null | undefined) => string | null = normalizeRequestHandlerBaseOrigin
): AjaxBaseOriginHealthState | null {
  const normalizedOrigin = normalizeOrigin(origin);
  if (!normalizedOrigin) {
    return null;
  }

  const existingState = healthMap.get(normalizedOrigin);
  if (existingState) {
    return existingState;
  }

  const nextState = createBaseOriginHealthState(normalizedOrigin);
  healthMap.set(normalizedOrigin, nextState);
  return nextState;
}

export function getBaseOriginScore(
  origin: string,
  state: AjaxBaseOriginHealthState | null,
  preferredOrigin: string | null | undefined,
  now = Date.now()
): number {
  if (!state) {
    return origin === preferredOrigin ? 112 : 100;
  }

  let score = 100;
  score += state.successes * 8;
  score -= state.failures * 4;
  score -= state.consecutiveFailures * 18;

  if (state.lastLatencyMs !== null) {
    score += Math.max(0, 24 - Math.round(Math.min(state.lastLatencyMs, 12000) / 500));
  }

  if (state.lastSuccessAt > 0) {
    const successAgeMinutes = Math.max(0, (now - state.lastSuccessAt) / 60000);
    score += Math.max(0, 12 - successAgeMinutes);
  }

  if (state.cooldownUntil > now) {
    score -= 240;
  }

  if (origin === preferredOrigin) {
    score += 14;
  }

  return score;
}

export function rankBaseOrigins(params: {
  origins: string[];
  healthMap: Map<string, AjaxBaseOriginHealthState>;
  preferredOrigin?: string | null;
  normalizeOrigin?: (value: string | null | undefined) => string | null;
  limit?: number;
  label?: string;
  now?: number;
}): string[] {
  const {
    origins,
    healthMap,
    preferredOrigin = null,
    normalizeOrigin = normalizeRequestHandlerBaseOrigin,
    limit = 0,
    label = '请求',
    now = Date.now()
  } = params;

  if (origins.length <= 1) {
    return limit > 0 ? origins.slice(0, limit) : origins;
  }

  const scoredOrigins = origins.map((origin, index) => {
    const state = ensureBaseOriginHealthState(healthMap, origin, normalizeOrigin);
    const cooldownRemainingMs = Math.max((state?.cooldownUntil || 0) - now, 0);
    return {
      origin,
      index,
      cooldownRemainingMs,
      score: getBaseOriginScore(origin, state, preferredOrigin, now)
    };
  });

  const activeOrigins = scoredOrigins
    .filter((item) => item.cooldownRemainingMs <= 0)
    .sort((left, right) => right.score - left.score || left.index - right.index);
  const coolingOrigins = scoredOrigins
    .filter((item) => item.cooldownRemainingMs > 0)
    .sort(
      (left, right) =>
        left.cooldownRemainingMs - right.cooldownRemainingMs ||
        right.score - left.score ||
        left.index - right.index
    );

  if (activeOrigins.length === 0 && coolingOrigins.length > 0) {
    logger.warn(
      `${label}域名当前全部处于冷却中，将优先尝试最早恢复的域名 ${coolingOrigins[0].origin}（剩余 ${Math.ceil(
        coolingOrigins[0].cooldownRemainingMs / 1000
      )} 秒）。`
    );
  }

  const rankedOrigins = [...activeOrigins, ...coolingOrigins].map((item) => item.origin);
  return limit > 0 ? rankedOrigins.slice(0, limit) : rankedOrigins;
}

export function recordBaseOriginSuccess(params: {
  healthMap: Map<string, AjaxBaseOriginHealthState>;
  origin: string | null | undefined;
  latencyMs: number;
  normalizeOrigin?: (value: string | null | undefined) => string | null;
}): void {
  const state = ensureBaseOriginHealthState(params.healthMap, params.origin, params.normalizeOrigin);
  if (!state) {
    return;
  }

  state.attempts += 1;
  state.successes += 1;
  state.consecutiveFailures = 0;
  state.lastLatencyMs = Number.isFinite(params.latencyMs) ? Math.max(0, Math.round(params.latencyMs)) : null;
  state.lastSuccessAt = Date.now();
  state.cooldownUntil = 0;
  state.lastError = '';
}

export function getBaseOriginCooldownThreshold(reason: string): number {
  const normalizedReason = String(reason || '').toLowerCase();

  if (
    normalizedReason.includes('timed out') ||
    normalizedReason.includes('timeout') ||
    normalizedReason.includes('econnreset') ||
    normalizedReason.includes('bad request') ||
    normalizedReason.includes('err_bad_request') ||
    normalizedReason.includes('403') ||
    normalizedReason.includes('forbidden')
  ) {
    return 2;
  }

  if (
    normalizedReason.includes('429') ||
    normalizedReason.includes('rate limit') ||
    normalizedReason.includes('challenge')
  ) {
    return 1;
  }

  return 3;
}

export function recordBaseOriginFailure(params: {
  healthMap: Map<string, AjaxBaseOriginHealthState>;
  origin: string | null | undefined;
  reason: string;
  cooldownMs: number;
  label?: string;
  normalizeOrigin?: (value: string | null | undefined) => string | null;
}): void {
  const state = ensureBaseOriginHealthState(params.healthMap, params.origin, params.normalizeOrigin);
  if (!state) {
    return;
  }

  const normalizedReason = String(params.reason || 'unknown');
  const now = Date.now();
  state.attempts += 1;
  state.failures += 1;
  state.consecutiveFailures += 1;
  state.lastFailureAt = now;
  state.lastError = normalizedReason;

  if (state.cooldownUntil > now) {
    return;
  }

  const threshold = getBaseOriginCooldownThreshold(normalizedReason);
  if (state.consecutiveFailures < threshold) {
    return;
  }

  state.cooldownUntil = now + params.cooldownMs;
  logger.warn(
    `${params.label || '请求'}域名 ${state.origin} 已连续失败 ${state.consecutiveFailures} 次，进入 ${Math.round(
      params.cooldownMs / 60000
    )} 分钟冷却。原因：${normalizedReason}`
  );
}

export function loadAjaxAntiBlockBaseOrigins(options: {
  homeDir?: string;
  antiBlockFilePath?: string;
} = {}): string[] {
  const homeDir =
    options.homeDir ||
    ((process.platform === 'win32' ? process.env.USERPROFILE : process.env.HOME) || process.cwd());
  const antiBlockFilePath = options.antiBlockFilePath || path.join(homeDir, '.jav-scrapy-antiblock-urls.json');

  try {
    if (!fs.existsSync(antiBlockFilePath)) {
      return [];
    }

    const data = JSON.parse(fs.readFileSync(antiBlockFilePath, 'utf8'));
    if (!Array.isArray(data)) {
      return [];
    }

    return data
      .map((item) => normalizeRequestHandlerBaseOrigin(String(item || '')))
      .filter((item): item is string => Boolean(item));
  } catch (error) {
    logger.warn(`读取 AJAX 备用网址缓存失败：${error instanceof Error ? error.message : String(error)}`);
    return [];
  }
}
