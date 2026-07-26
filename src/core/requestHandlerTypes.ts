export interface RequestConfig {
  timeout?: number;
  proxy?: string;
  cookie?: string;
  headers: {
    authority?: string;
    method?: string;
    path?: string;
    scheme?: string;
    accept?: string;
    'accept-encoding'?: string;
    'accept-language'?: string;
    'cache-control'?: string;
    'sec-ch-ua'?: string;
    'sec-ch-ua-mobile'?: string;
    'sec-ch-ua-platform'?: string;
    'sec-fetch-dest'?: string;
    'sec-fetch-mode'?: string;
    'sec-fetch-site'?: string;
    'sec-fetch-user'?: string;
    'upgrade-insecure-requests'?: string;
    'user-agent': string;
    referer: string;
    Cookie: string;
    Connection: string;
    'X-Requested-With'?: string;
  };
}

export interface MagnetFetchOptions {
  mode?: 'auto' | 'http-only' | 'cloudflare-only';
  fastFallback?: boolean;
}

export interface FastHttpMagnetCircuitState {
  attempts: number;
  successes: number;
  failures: number;
  consecutiveFailures: number;
  disabled: boolean;
  disabledAt: number | null;
  disableReason: string;
}

export interface CloudflareAjaxWorkerSlot {
  client: { close(): Promise<void>; executeAjax(url: string): Promise<string | null>; prewarm(): Promise<string | null> };
  inFlight: number;
  slotId: number;
}

export interface ProxyRuntimeState {
  url: string;
  attempts: number;
  successes: number;
  failures: number;
  consecutiveFailures: number;
  totalLatencyMs: number;
  cooldownUntil: number;
  lastError: string;
}

export interface AjaxBaseOriginHealthState {
  origin: string;
  attempts: number;
  successes: number;
  failures: number;
  consecutiveFailures: number;
  lastLatencyMs: number | null;
  lastSuccessAt: number;
  lastFailureAt: number;
  cooldownUntil: number;
  lastError: string;
}

export interface ParsedMagnetCandidate {
  magnetLink: string;
  size: number;
  displayName: string;
}

export const KNOWN_AJAX_MIRROR_BASES = [
  'https://www.busjav.cyou',
  'https://www.fanbus.bond',
  'https://www.cdnbus.bond',
  'https://www.dmmbus.cyou'
];
