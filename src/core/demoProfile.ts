import fs from 'fs';
import path from 'path';

export type DemoMode = 'base' | 'ae' | 'aed';

export interface DemoProfile {
  mode: DemoMode;
  label: string;
  isDemoBuild: boolean;
  productDisplayName: string;
  enableFastAjaxFallback: boolean;
  enableCloudflarePrewarm: boolean;
  enableDedicatedCloudflareLane: boolean;
  enableCloudflareWorker: boolean;
}

interface PackageMetadata {
  demoMode?: string;
  demoLabel?: string;
  productDisplayName?: string;
}

const cachedProfiles = new Map<string, DemoProfile>();

function resolvePackageMetadata(): PackageMetadata {
  const candidates = [
    path.resolve(__dirname, '../../package.json'),
    path.resolve(process.cwd(), 'package.json')
  ];

  for (const candidate of candidates) {
    try {
      if (fs.existsSync(candidate)) {
        return JSON.parse(fs.readFileSync(candidate, 'utf8')) as PackageMetadata;
      }
    } catch {
      // 忽略单个候选路径的读取失败，继续尝试下一个路径。
    }
  }

  return {};
}

function normalizeMode(rawMode: string | undefined): DemoMode {
  switch (String(rawMode || '').trim().toLowerCase()) {
    case 'ae':
      return 'ae';
    case 'aed':
      return 'aed';
    default:
      return 'base';
  }
}

export function getDemoProfile(overrides: PackageMetadata = {}): DemoProfile {
  const packageMetadata = resolvePackageMetadata();
  const metadata: PackageMetadata = {
    ...packageMetadata,
    ...Object.fromEntries(
      Object.entries(overrides).filter(([, value]) => typeof value === 'string' && value.trim().length > 0)
    )
  };
  const cacheKey = JSON.stringify({
    demoMode: process.env.JAV_DEMO_MODE || metadata.demoMode || '',
    demoLabel: process.env.JAV_DEMO_LABEL || metadata.demoLabel || '',
    productDisplayName: metadata.productDisplayName || ''
  });

  if (cachedProfiles.has(cacheKey)) {
    return cachedProfiles.get(cacheKey)!;
  }

  const mode = normalizeMode(process.env.JAV_DEMO_MODE || metadata.demoMode);
  const label = String(process.env.JAV_DEMO_LABEL || metadata.demoLabel || '').trim();
  const isDemoBuild = Boolean(label || process.env.JAV_DEMO_MODE || metadata.demoMode);
  const productDisplayName =
    String(metadata.productDisplayName || 'JavFlow').trim() || 'JavFlow';
  const enableAeFeatures = mode === 'ae' || mode === 'aed';

  const profile: DemoProfile = {
    mode,
    label,
    isDemoBuild,
    productDisplayName,
    enableFastAjaxFallback: enableAeFeatures,
    enableCloudflarePrewarm: enableAeFeatures,
    enableDedicatedCloudflareLane: enableAeFeatures,
    enableCloudflareWorker: mode === 'aed'
  };

  cachedProfiles.set(cacheKey, profile);
  return profile;
}
