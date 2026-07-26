import { parentPort, workerData } from 'worker_threads';
import CloudflareBypass from '../utils/cloudflareBypass';

type WorkerRequestType = 'prewarm' | 'executeAjax' | 'shutdown';

interface WorkerRequest {
  id: string;
  type: WorkerRequestType;
  payload?: {
    url?: string;
  };
}

interface WorkerResponse {
  id: string;
  ok: boolean;
  result?: unknown;
  error?: string;
}

interface WorkerRuntimeConfig {
  BASE_URL: string;
  base: string | null;
  proxy?: string;
  timeout?: number;
}

let bypass: CloudflareBypass | null = null;
let queue = Promise.resolve();
const config = (workerData?.config || {}) as WorkerRuntimeConfig;

async function ensureBypass(): Promise<CloudflareBypass> {
  if (bypass) {
    return bypass;
  }

  bypass = new CloudflareBypass({
    headless: true,
    timeout: config.timeout || 45000,
    proxy: config.proxy
  });

  await bypass.init();
  await bypass.setAgeVerificationCookies();
  return bypass;
}

async function handlePrewarm(): Promise<{ cookies: string | null }> {
  const instance = await ensureBypass();
  await instance.bypassCloudflare(config.base || config.BASE_URL);
  const cookies = await instance.getCookies();
  return { cookies: cookies || null };
}

async function handleExecuteAjax(url: string): Promise<string | null> {
  if (!url) {
    throw new Error('Cloudflare AJAX Worker 缺少目标 URL');
  }

  const instance = await ensureBypass();
  return instance.executeAjax(url);
}

async function handleShutdown(): Promise<void> {
  if (bypass) {
    await bypass.close();
    bypass = null;
  }
}

function respond(message: WorkerResponse): void {
  parentPort?.postMessage(message);
}

async function processMessage(request: WorkerRequest): Promise<void> {
  try {
    switch (request.type) {
      case 'prewarm': {
        const result = await handlePrewarm();
        respond({ id: request.id, ok: true, result });
        return;
      }
      case 'executeAjax': {
        const result = await handleExecuteAjax(String(request.payload?.url || ''));
        respond({ id: request.id, ok: true, result });
        return;
      }
      case 'shutdown': {
        await handleShutdown();
        respond({ id: request.id, ok: true, result: null });
        return;
      }
      default:
        throw new Error(`未知的 Worker 请求类型：${(request as WorkerRequest).type}`);
    }
  } catch (error) {
    respond({
      id: request.id,
      ok: false,
      error: error instanceof Error ? error.message : String(error)
    });
  }
}

parentPort?.on('message', (request: WorkerRequest) => {
  queue = queue.then(() => processMessage(request), () => processMessage(request));
});
