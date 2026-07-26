import path from 'path';
import { Worker } from 'worker_threads';
import logger from './logger';
import { Config } from '../types/interfaces';

type WorkerRequestType = 'prewarm' | 'executeAjax' | 'shutdown';

interface WorkerRequest {
  id: string;
  type: WorkerRequestType;
  payload?: Record<string, unknown>;
}

interface WorkerResponse {
  id: string;
  ok: boolean;
  result?: unknown;
  error?: string;
}

interface PendingRequest {
  resolve: (value: any) => void;
  reject: (reason?: unknown) => void;
  timeout: NodeJS.Timeout;
}

interface WorkerRuntimeConfig {
  BASE_URL: string;
  base: string | null;
  proxy?: string;
  timeout?: number;
}

class CloudflareAjaxWorkerClient {
  private worker: Worker | null = null;
  private startPromise: Promise<void> | null = null;
  private pendingRequests = new Map<string, PendingRequest>();
  private sequence = 0;
  private isClosing = false;
  private readonly runtimeConfig: WorkerRuntimeConfig;

  constructor(config: Config) {
    this.runtimeConfig = {
      BASE_URL: config.BASE_URL,
      base: config.base,
      proxy: config.proxy,
      timeout: config.timeout
    };
  }

  public async prewarm(): Promise<string | null> {
    const response = await this.sendRequest<{ cookies?: string | null }>('prewarm', undefined, 120000);
    return response?.cookies || null;
  }

  public async executeAjax(url: string): Promise<string | null> {
    const response = await this.sendRequest<string>('executeAjax', { url }, 120000);
    return response || null;
  }

  public async close(): Promise<void> {
    this.isClosing = true;

    try {
      await this.sendRequest('shutdown', undefined, 5000);
    } catch {
      // 关闭时允许静默降级到 terminate。
    }

    if (this.worker) {
      await this.worker.terminate().catch(() => undefined);
      this.worker = null;
    }

    this.rejectAllPending('Cloudflare Worker 已关闭');
  }

  private async ensureWorker(): Promise<void> {
    if (this.worker) {
      return;
    }

    if (this.startPromise) {
      return this.startPromise;
    }

    this.startPromise = new Promise<void>((resolve, reject) => {
      try {
        const workerPath = path.resolve(__dirname, '../workers/cloudflareAjaxWorker.js');
        const worker = new Worker(workerPath, {
          workerData: {
            config: this.runtimeConfig
          }
        });

        let settled = false;

        worker.on('message', (message: WorkerResponse) => {
          this.handleMessage(message);
        });

        worker.once('online', () => {
          settled = true;
          this.worker = worker;
          logger.info('Cloudflare AJAX Worker 已启动');
          resolve();
        });

        worker.once('error', (error) => {
          if (!settled) {
            reject(error);
          } else {
            logger.warn(`Cloudflare AJAX Worker 运行失败：${error.message}`);
          }
          this.worker = null;
          this.rejectAllPending(error.message);
        });

        worker.once('exit', (code) => {
          this.worker = null;
          if (!this.isClosing && code !== 0) {
            logger.warn(`Cloudflare AJAX Worker 已退出，退出码：${code}`);
          }
          this.rejectAllPending(`Cloudflare AJAX Worker 已退出（code=${code}）`);
        });
      } catch (error) {
        reject(error);
      }
    }).finally(() => {
      this.startPromise = null;
    });

    return this.startPromise || Promise.resolve();
  }

  private async sendRequest<T>(
    type: WorkerRequestType,
    payload?: Record<string, unknown>,
    timeoutMs = 60000
  ): Promise<T> {
    await this.ensureWorker();

    if (!this.worker) {
      throw new Error('Cloudflare AJAX Worker 未就绪');
    }

    const id = `cf-worker-${Date.now()}-${++this.sequence}`;
    const request: WorkerRequest = {
      id,
      type,
      payload
    };

    return new Promise<T>((resolve, reject) => {
      const timeout = setTimeout(() => {
        this.pendingRequests.delete(id);
        reject(new Error(`${type} 请求超时`));
      }, timeoutMs);

      this.pendingRequests.set(id, { resolve, reject, timeout });
      this.worker!.postMessage(request);
    });
  }

  private handleMessage(response: WorkerResponse): void {
    const pending = this.pendingRequests.get(response.id);
    if (!pending) {
      return;
    }

    clearTimeout(pending.timeout);
    this.pendingRequests.delete(response.id);

    if (response.ok) {
      pending.resolve(response.result);
      return;
    }

    pending.reject(new Error(response.error || 'Cloudflare AJAX Worker 返回未知错误'));
  }

  private rejectAllPending(message: string): void {
    for (const [id, pending] of this.pendingRequests.entries()) {
      clearTimeout(pending.timeout);
      pending.reject(new Error(message));
      this.pendingRequests.delete(id);
    }
  }
}

export default CloudflareAjaxWorkerClient;
