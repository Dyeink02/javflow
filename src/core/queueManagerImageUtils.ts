import { normalizeRequestHandlerBaseOrigin } from './requestHandlerBaseOriginUtils';

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

function buildRuntimeReferer(configuredBaseUrl: string, runtimeOrigin: string): string {
  try {
    const parsed = new URL(configuredBaseUrl);
    return `${runtimeOrigin}${parsed.pathname}${parsed.search}`;
  } catch {
    return `${runtimeOrigin}/`;
  }
}

export function resolveImageDownloadTarget(params: {
  configuredBaseUrl: string;
  imageSource: string;
  preferredPageOrigin?: string | null;
  preferredAjaxOrigin?: string | null;
}): {
  imageUrl: string;
  refererUrl: string;
  runtimeOrigin: string;
} {
  const fallbackOrigin =
    normalizeRequestHandlerBaseOrigin(params.configuredBaseUrl) || 'https://www.javbus.com';
  const runtimeOrigin =
    normalizeRequestHandlerBaseOrigin(params.preferredPageOrigin) ||
    normalizeRequestHandlerBaseOrigin(params.preferredAjaxOrigin) ||
    fallbackOrigin;
  const refererUrl = buildRuntimeReferer(params.configuredBaseUrl, runtimeOrigin);
  const rawImageSource = String(params.imageSource || '').trim();

  if (!rawImageSource) {
    return {
      imageUrl: `${runtimeOrigin}/`,
      refererUrl,
      runtimeOrigin
    };
  }

  if (/^https?:\/\//i.test(rawImageSource)) {
    try {
      const parsedImage = new URL(rawImageSource);
      if (isOfficialJavbusOrigin(parsedImage.origin)) {
        return {
          imageUrl: `${runtimeOrigin}${parsedImage.pathname}${parsedImage.search}`,
          refererUrl,
          runtimeOrigin
        };
      }

      return {
        imageUrl: rawImageSource,
        refererUrl,
        runtimeOrigin
      };
    } catch {
      return {
        imageUrl: rawImageSource,
        refererUrl,
        runtimeOrigin
      };
    }
  }

  const normalizedPath = rawImageSource.startsWith('/') ? rawImageSource : `/${rawImageSource}`;
  return {
    imageUrl: `${runtimeOrigin}${normalizedPath}`,
    refererUrl,
    runtimeOrigin
  };
}
