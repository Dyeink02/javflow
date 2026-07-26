// Proxy validation helper retained for desktop compatibility flows.
// compatibility-owner: active crawl-compatible proxy validator; marker=compat-mainservices-proxy-validation
// It probes connectivity through the configured proxy without coupling the UI
// to the crawler runtime, which keeps proxy diagnosis independent from crawl
// bugs. Keep this helper narrow: it is a transport check, not crawler logic.
//
// Ownership summary:
// 1) normalize proxy input
// 2) run a lightweight HTTPS connectivity probe
// 3) return transport-oriented validation results for UI/compat callers
//
// File map for maintainers:
// 1) proxy/target normalization helpers
// 2) proxy-agent construction
// 3) connectivity probe and result shaping

const https = require('https');
const tunnel = require('tunnel');

function createProxyValidationService() {
  function normalizeProxyValue(value) {
    const rawValue = String(value || '').trim();
    if (!rawValue) {
      return '';
    }

    const proxyValue = /^[a-z][a-z0-9+.-]*:\/\//i.test(rawValue)
      ? rawValue
      : /^[^/\s]+:\d+$/.test(rawValue)
        ? `http://${rawValue}`
        : rawValue;

    try {
      const parsed = new URL(proxyValue);
      if (!parsed.hostname) {
        return '';
      }

      return parsed.toString().replace(/\/$/, '');
    } catch {
      return '';
    }
  }

  function normalizeTargetUrl(targetUrl) {
    const fallbackUrl = 'https://www.javbus.com/';
    const rawValue = String(targetUrl || '').trim();
    if (!rawValue) {
      return fallbackUrl;
    }

    try {
      const parsed = new URL(rawValue);
      if (parsed.protocol !== 'https:') {
        return fallbackUrl;
      }

      return parsed.toString();
    } catch {
      return fallbackUrl;
    }
  }

  function createProxyAgent(proxyUrl) {
    const parsedProxy = new URL(proxyUrl);
    const port =
      Number.parseInt(parsedProxy.port, 10) || (parsedProxy.protocol === 'https:' ? 443 : 80);
    const proxyOptions = {
      proxy: {
        host: parsedProxy.hostname,
        port
      }
    };

    if (parsedProxy.username || parsedProxy.password) {
      proxyOptions.proxy.proxyAuth = `${decodeURIComponent(parsedProxy.username)}:${decodeURIComponent(parsedProxy.password)}`;
    }

    if (parsedProxy.protocol === 'http:') {
      return tunnel.httpsOverHttp(proxyOptions);
    }

    if (parsedProxy.protocol === 'https:') {
      return tunnel.httpsOverHttps(proxyOptions);
    }

    throw new Error('Only HTTP/HTTPS proxies are supported.');
  }

  function probeProxy(proxyUrl, targetUrl) {
    // This is intentionally a lightweight transport probe. It confirms whether
    // HTTPS traffic can traverse the configured proxy, but it does not try to
    // emulate the crawler, solve Cloudflare, or validate age-gated sessions.
    return new Promise((resolve, reject) => {
      const parsedTarget = new URL(targetUrl);
      const agent = createProxyAgent(proxyUrl);
      const startedAt = Date.now();
      const request = https.request(
        {
          protocol: parsedTarget.protocol,
          host: parsedTarget.hostname,
          port: Number.parseInt(parsedTarget.port, 10) || 443,
          path: `${parsedTarget.pathname || '/'}${parsedTarget.search || ''}`,
          method: 'HEAD',
          agent,
          timeout: 6000,
          rejectUnauthorized: false,
          headers: {
            'User-Agent':
              'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36',
            Accept: 'text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8'
          }
        },
        (response) => {
          const statusCode = Number(response.statusCode) || 0;
          response.resume();

          if (statusCode === 407) {
            reject(new Error('Proxy authentication failed.'));
            return;
          }

          if (statusCode > 0 && statusCode < 500) {
            resolve({
              statusCode,
              latencyMs: Date.now() - startedAt
            });
            return;
          }

          reject(new Error(`Target site returned an unexpected status: ${statusCode || 'unknown'}.`));
        }
      );

      request.on('timeout', () => {
        request.destroy(new Error('Connection timed out.'));
      });

      request.on('error', (error) => {
        reject(error);
      });

      request.end();
    });
  }

  async function validateProxy(proxyValue, options = {}) {
    const rawValue = String(proxyValue || '').trim();
    if (!rawValue) {
      return {
        status: 'empty',
        normalizedProxy: '',
        message: 'Proxy not set',
        detail: 'The app will continue with a direct connection.'
      };
    }

    const normalizedProxy = normalizeProxyValue(rawValue);
    if (!normalizedProxy) {
      return {
        status: 'invalid',
        normalizedProxy: '',
        message: 'Proxy validation failed',
        detail: 'The proxy address format is invalid. Check scheme, host, and port.'
      };
    }

    try {
      // Keep validation output transport-oriented so renderer code can present
      // proxy diagnostics without coupling to crawler-specific wording/state.
      const result = await probeProxy(normalizedProxy, normalizeTargetUrl(options.targetUrl));
      return {
        status: 'valid',
        normalizedProxy,
        message: 'Proxy reachable',
        detail: `Connectivity check passed. Current latency is about ${result.latencyMs} ms.`,
        latencyMs: result.latencyMs
      };
    } catch (error) {
      return {
        status: 'invalid',
        normalizedProxy,
        message: 'Proxy validation failed',
        detail: error instanceof Error ? error.message : String(error)
      };
    }
  }

  return {
    normalizeProxyValue,
    validateProxy
  };
}

module.exports = {
  createProxyValidationService
};
