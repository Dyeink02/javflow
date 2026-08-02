interface Config {
  retryCount: number;
  retryDelay: number;
  BASE_URL: string;
  baseUrl: string;
  parallel: number;
  proxy?: string;
  headers: {
    Referer: string;
    Cookie: string;
  };
  output: string;
  search: string | null;
  base: string | null;
  nomag: boolean;
  metadataOnly?: boolean;
  allmag: boolean;
  nopic: boolean;
  timeout?: number;
  searchUrl: string;
  limit: number;
  totalPages?: number;
  itemsPerPage?: number;
  delay: number;
  strictSSL?: boolean;
  useCloudflareBypass?: boolean;
  secondValidation?: boolean;
  taskTemplate?: string;
  magnetExcludeKeywords?: string;
  magnetContentValidation?: boolean;
  supplementMagnetTopN?: number;
  actressCountFilterThreshold?: number;
  filmCodeFilterThreshold?: string;
  demoMode?: string;
  demoLabel?: string;
  productDisplayName?: string;
  puppeteerPool?: {
    maxSize: number;
    maxIdleTime: number;
    healthCheckInterval: number;
    requestTimeout: number;
    retryAttempts: number;
  };
}

interface RuntimeOptions {
  parallel?: number | string | null;
  timeout?: number | string | null;
  output?: string | null;
  search?: string | null;
  base?: string | null;
  proxy?: string | null;
  nomag?: boolean | null;
  metadataOnly?: boolean | null;
  allmag?: boolean | null;
  nopic?: boolean | null;
  limit?: number | string | null;
  totalPages?: number | string | null;
  itemsPerPage?: number | string | null;
  delay?: number | string | null;
  cookies?: string | null;
  cloudflare?: boolean | null;
  strictSSL?: boolean | null;
  secondValidation?: boolean | null;
  taskTemplate?: string | null;
  magnetExcludeKeywords?: string | null;
  magnetContentValidation?: boolean | null;
  supplementMagnetTopN?: number | string | null;
  actressCountFilterThreshold?: number | string | null;
  filmCodeFilterThreshold?: string | null;
  demoMode?: string | null;
  demoLabel?: string | null;
  productDisplayName?: string | null;
}

interface IndexPageTask {
  url: string;
}

interface DetailPageTask {
  link: string;
}

interface Metadata {
  title: string;
  gid: string;
  img: string;
  uc: string;
  category: string[];
  actress: string[];
  releaseDate?: string;
  maker?: string;
  label?: string;
  series?: string;
}

interface MagnetLink {
  link: string;
  size: string;
  /** Original `dn` value decoded from the magnet URI, preserving case/suffixes. */
  displayName?: string;
}

interface MagnetResult {
  magnet: string;
  magnetLinks?: MagnetLink[];
  backupMagnetLinks?: MagnetLink[];
}

interface FilmData {
  title: string;
  sourceLink?: string;
  coverImage?: string;
  magnetLinks?: MagnetLink[];
  backupMagnetLinks?: MagnetLink[];
  category: string[];
  actress: string[];
  releaseDate?: string;
  maker?: string;
  label?: string;
  series?: string;
  actressCount?: number;
  filteredByActressCount?: boolean;
  filteredByFilmCode?: boolean;
  filterReason?: string;
}

export {
  Config,
  RuntimeOptions,
  IndexPageTask,
  DetailPageTask,
  Metadata,
  MagnetLink,
  MagnetResult,
  FilmData
};
