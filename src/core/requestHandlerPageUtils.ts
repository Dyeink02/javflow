import type { RequestConfig } from './requestHandlerTypes';

export function getManualCookieHeader(configCookie: string | undefined, defaultCookieHeader: string): string | null {
  if (!configCookie || configCookie === 'existmag=mag' || configCookie === defaultCookieHeader) {
    return null;
  }

  return configCookie;
}

interface BuildPageRequestHeadersOptions {
  requestHeaders: RequestConfig['headers'];
  configCookie: string | undefined;
  cookieOverride?: string | null;
  cloudflareCookies?: string | null;
  defaultCookieHeader: string;
}

export function buildPageRequestHeaders(options: BuildPageRequestHeadersOptions): RequestConfig['headers'] {
  const headers = { ...options.requestHeaders };
  const manualCookies = getManualCookieHeader(options.configCookie, options.defaultCookieHeader);

  if (manualCookies) {
    headers.Cookie = manualCookies;
  } else if (options.cookieOverride) {
    headers.Cookie = options.cookieOverride;
  } else if (options.cloudflareCookies) {
    headers.Cookie = options.cloudflareCookies;
  } else {
    headers.Cookie = options.defaultCookieHeader;
  }

  return headers;
}

export function isUsablePageBody(body: string | null | undefined): boolean {
  return typeof body === 'string' && body.trim().length > 0;
}

export function isAgeVerificationResponse(body: string | null | undefined): boolean {
  const normalizedBody = String(body || '').toLowerCase();
  return (
    normalizedBody.includes('age verification javbus') ||
    normalizedBody.includes('/doc/driver-verify') ||
    normalizedBody.includes('driver-verify?referer=') ||
    normalizedBody.includes('must be 18') ||
    normalizedBody.includes('adult only') ||
    normalizedBody.includes('age verification')
  );
}

export function isCloudflareChallengeResponse(statusCode: number | null | undefined, body: string | null | undefined): boolean {
  const normalizedBody = String(body || '').toLowerCase();
  return (
    Boolean(statusCode && [403, 429, 503].includes(statusCode)) ||
    normalizedBody.includes('cf-browser-verification') ||
    normalizedBody.includes('challenge-platform') ||
    normalizedBody.includes('/cdn-cgi/challenge-platform/') ||
    normalizedBody.includes('__cf_chl_') ||
    normalizedBody.includes('checking your browser before accessing') ||
    normalizedBody.includes('just a moment...') ||
    normalizedBody.includes('attention required! | cloudflare') ||
    normalizedBody.includes('please stand by, while we are checking your browser')
  );
}

export function isUsablePageResponse(response: { statusCode: number; body: string } | null): boolean {
  return Boolean(
    response &&
      isUsablePageBody(response.body) &&
      !isCloudflareChallengeResponse(response.statusCode, response.body) &&
      !isAgeVerificationResponse(response.body)
  );
}

export function describePageFallbackReason(response: { statusCode: number; body: string } | null): string {
  if (!response) {
    return '常规请求未能拿到页面内容';
  }

  if (!isUsablePageBody(response.body)) {
    return '常规请求返回空页面';
  }

  if (isCloudflareChallengeResponse(response.statusCode, response.body)) {
    return '常规请求命中验证页或拦截页';
  }

  if (isAgeVerificationResponse(response.body)) {
    return '常规请求命中年龄验证页';
  }

  return '常规请求内容疑似异常';
}

export function isRecoverableAjaxError(error: unknown): boolean {
  const message = error instanceof Error ? error.message : String(error);
  const normalized = message.toLowerCase();

  return [
    'err_connection_timed_out',
    'etimedout',
    'econnreset',
    'chromewebdata',
    'chrome-error',
    'err_tunnel_connection_failed',
    'err_proxy_connection_failed',
    'err_name_not_resolved',
    'navigation timeout',
    'challenge timeout',
    'cloudflare',
    'target closed',
    'execution context was destroyed',
    'session closed'
  ].some((keyword) => normalized.includes(keyword));
}

export function isRecoverableBypassError(error: unknown): boolean {
  const message = error instanceof Error ? error.message.toLowerCase() : String(error).toLowerCase();
  return (
    isRecoverableAjaxError(error) ||
    message.includes('net::err_connection') ||
    message.includes('net::err_aborted') ||
    message.includes('cannot read properties of null') ||
    message.includes('page 为 null') ||
    message.includes('获取 puppeteer 实例失败') ||
    message.includes('从共享池获取的页面实例为 null')
  );
}

export function isFastFallbackAjaxError(error: unknown): boolean {
  const message = error instanceof Error ? error.message : String(error);
  const normalized = message.toLowerCase();

  return [
    'err_bad_request',
    'bad request',
    'forbidden',
    'too many requests',
    'err_connection_timed_out',
    'etimedout',
    'timeout',
    'cloudflare',
    'empty response'
  ].some((keyword) => normalized.includes(keyword));
}

export function isValidCookieValue(value: string): boolean {
  if (!value || typeof value !== 'string') {
    return false;
  }

  if (value.length > 4096) {
    return false;
  }

  for (let index = 0; index < value.length; index += 1) {
    const charCode = value.charCodeAt(index);
    if (charCode < 32 || charCode === 127) {
      return false;
    }
  }

  return true;
}

export function isValidCookieString(cookieString: string): boolean {
  if (!cookieString || typeof cookieString !== 'string') {
    return false;
  }

  if (cookieString.length > 8192) {
    return false;
  }

  const cookies = cookieString.split(';');
  for (const cookie of cookies) {
    const trimmedCookie = cookie.trim();
    if (trimmedCookie.length === 0) {
      continue;
    }

    const equalIndex = trimmedCookie.indexOf('=');
    if (equalIndex <= 0 || equalIndex >= trimmedCookie.length - 1) {
      return false;
    }

    const name = trimmedCookie.substring(0, equalIndex).trim();
    if (!/^[a-zA-Z0-9_-]+$/.test(name)) {
      return false;
    }

    const value = trimmedCookie.substring(equalIndex + 1).trim();
    if (!isValidCookieValue(value)) {
      return false;
    }
  }

  return true;
}

export function setCookieHeader(headers: Record<string, string>, cookieString: string): boolean {
  if (!isValidCookieString(cookieString)) {
    return false;
  }

  headers.Cookie = cookieString;
  return true;
}
