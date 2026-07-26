// @ts-nocheck
"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.QueueEventType = void 0;
const async_1 = __importDefault(require("async"));
const logger_1 = __importDefault(require("./logger"));
const requestHandler_1 = __importDefault(require("./requestHandler"));
const fileHandler_1 = __importDefault(require("./fileHandler"));
const parser_1 = __importDefault(require("./parser"));
const errorHandler_1 = require("../utils/errorHandler");
const delayManager_1 = require("../utils/delayManager");
const puppeteerPool_1 = require("./puppeteerPool");
const resourceMonitor_1 = require("./resourceMonitor");
const queueManagerImageUtils_1 = require("./queueManagerImageUtils");
const queueManagerMagnetQueueUtils_1 = require("./queueManagerMagnetQueueUtils");
var QueueEventType;
(function (QueueEventType) {
    QueueEventType["INDEX_PAGE_START"] = "index_page_start";
    QueueEventType["INDEX_PAGE_PROCESSED"] = "index_page_processed";
    QueueEventType["DETAIL_PAGE_START"] = "detail_page_start";
    QueueEventType["DETAIL_PAGE_PROCESSED"] = "detail_page_processed";
    QueueEventType["DETAIL_PAGE_FAILED"] = "detail_page_failed";
    QueueEventType["FILM_DATA_SAVED"] = "film_data_saved";
})(QueueEventType || (exports.QueueEventType = QueueEventType = {}));
/**
 * 队列管理器，负责创建和管理不同类型的异步任务队列
 * @class
 */
class QueueManager {
    /**
    * 创建队列管理器实例
    * @constructor
    * @param {Config} config - 配置对象
    */
    constructor(config) {
        this.resourceMonitor = null;
        // 队列相关
        this.fileWriteQueue = null;
        this.detailPageQueue = null;
        this.magnetFastQueue = null;
        this.magnetRecoveryQueue = null;
        this.indexPageQueue = null;
        this.imageDownloadQueue = null;
        // 事件相关
        this.eventHandlers = new Map();
        // 监控相关
        this.queueStatsInterval = null;
        this.lastTaskStartTimes = new Map();
        this.isShuttingDown = false;
        this.config = config;
        // 初始化Puppeteer池（如果需要Cloudflare绕过）
        if (config.useCloudflareBypass) {
            this.puppeteerPool = puppeteerPool_1.PuppeteerPool.getInstance({
                maxSize: Math.max(2, Math.floor(config.parallel / 1.5)), // 根据并发数动态调整
                maxIdleTime: 5 * 60 * 1000, // 5分钟
                healthCheckInterval: 30 * 1000, // 30秒
                requestTimeout: 60000, // 1分钟
                retryAttempts: 3
            });
            // 启动资源监控
            this.resourceMonitor = resourceMonitor_1.ResourceMonitor.getInstance(this.puppeteerPool);
            this.resourceMonitor.startMonitoring(30000); // 30秒监控间隔
            logger_1.default.info('QueueManager: 已启用Puppeteer池和资源监控');
        }
        else {
            // 仍然创建池实例以保持兼容性，但不启用监控
            this.puppeteerPool = puppeteerPool_1.PuppeteerPool.getInstance({
                maxSize: 1,
                maxIdleTime: 10 * 60 * 1000,
                healthCheckInterval: 60 * 1000,
                requestTimeout: 60000,
                retryAttempts: 3
            });
        }
        this.requestHandler = new requestHandler_1.default(config);
        this.fileHandler = new fileHandler_1.default(config.output, {
            actressCountFilterThreshold: config.actressCountFilterThreshold,
            filmCodeFilterThreshold: config.filmCodeFilterThreshold
        });
        this.prewarmRuntime();
        // 启动队列状态监控
        this.startQueueMonitoring();
        logger_1.default.debug('QueueManager: 队列管理器初始化完成，已启动状态监控');
    }
    prewarmRuntime() {
        if (!this.config.useCloudflareBypass) {
            return;
        }
        void this.requestHandler.prewarmCloudflareSession().catch((error) => {
            if (this.isShuttingDown) {
                return;
            }
            logger_1.default.warn(`QueueManager: Cloudflare 预热失败，运行时将继续按需恢复：${error instanceof Error ? error.message : String(error)}`);
        });
    }
    getFileHandler() {
        return this.fileHandler;
    }
    // 队列获取方法
    /**
     * 获取图片下载队列
     * @returns {async.QueueObject<Metadata>} 图片下载队列实例
     */
    getImageDownloadQueue() {
        if (!this.imageDownloadQueue) {
            logger_1.default.debug('QueueManager: 创建图片下载队列');
            // 图片下载队列使用较低的并发数，避免被检测
            const imageConcurrency = Math.max(1, Math.floor(this.config.parallel / 2));
            logger_1.default.debug(`QueueManager: 图片下载队列并发数: ${imageConcurrency}`);
            this.imageDownloadQueue = async_1.default.queue(async (metadata) => {
                if (this.isShuttingDown) {
                    logger_1.default.info(`QueueManager: [图片下载] 已收到终止指令，跳过任务: ${metadata.title}`);
                    return;
                }
                const taskKey = `image-${metadata.title}`;
                const startTime = Date.now();
                this.lastTaskStartTimes.set(taskKey, startTime);
                logger_1.default.debug(`QueueManager: [图片下载] 开始任务: ${metadata.title}`);
                try {
                    const baseUrl = this.config.base || this.config.BASE_URL;
                    const runtimeImageTarget = (0, queueManagerImageUtils_1.resolveImageDownloadTarget)({
                        configuredBaseUrl: baseUrl,
                        imageSource: metadata.img,
                        preferredPageOrigin: this.requestHandler.preferredPageBaseOrigin,
                        preferredAjaxOrigin: this.requestHandler.preferredAjaxBaseOrigin
                    });
                    const resolvedImageUrl = runtimeImageTarget.imageUrl;
                    const resolvedRefererUrl = runtimeImageTarget.refererUrl;
                    logger_1.default.debug(`QueueManager: [鍥剧墖涓嬭浇] 鏋勫缓鍥剧墖URL: ${resolvedImageUrl}`);
                    logger_1.default.debug(`QueueManager: [鍥剧墖涓嬭浇] 浣跨敤Referer: ${resolvedRefererUrl}`);
                    await this.requestHandler.downloadImage(resolvedImageUrl, metadata.title + '.jpg', resolvedRefererUrl);
                    const runtimeDownloadTime = Date.now() - startTime;
                    logger_1.default.debug(`QueueManager: [鍥剧墖涓嬭浇] 瀹屾垚涓嬭浇: ${metadata.title} (鑰楁椂: ${Math.round(runtimeDownloadTime / 1000)}s)`);
                    logger_1.default.debug(`QueueManager: [鍥剧墖涓嬭浇] 浠诲姟瀹屾垚: ${metadata.title}`);
                }
                catch (error) {
                    const failedTime = Date.now() - startTime;
                    logger_1.default.error(`QueueManager: [图片下载] 任务失败: ${metadata.title} (耗时: ${Math.round(failedTime / 1000)}s), 错误: ${error instanceof Error ? error.message : String(error)}`);
                    throw error;
                }
                finally {
                    this.lastTaskStartTimes.delete(taskKey);
                }
            }, imageConcurrency);
            // 添加队列事件监听
            this.imageDownloadQueue.error((error, task) => {
                logger_1.default.error(`QueueManager: [图片下载] 队列错误，任务: ${task.title}，错误: ${error instanceof Error ? error.message : String(error)}`);
            });
            logger_1.default.debug('QueueManager: 图片下载队列创建完成');
        }
        return this.imageDownloadQueue;
    }
    /**
     * 获取文件写入队列
     * @returns {async.QueueObject<FilmData>} 文件写入队列实例
     */
    getFileWriteQueue() {
        if (!this.fileWriteQueue) {
            logger_1.default.debug('QueueManager: 创建文件写入队列');
            // 文件写入已改为“内存合并 + 批量落盘”，单并发可保证顺序稳定。
            const fileWriteConcurrency = 1;
            logger_1.default.debug(`QueueManager: 文件写入队列并发数: ${fileWriteConcurrency}`);
            this.fileWriteQueue = async_1.default.queue(async (filmData) => {
                if (this.isShuttingDown) {
                    logger_1.default.info(`QueueManager: [文件写入] 已收到终止指令，跳过写入: ${filmData.title}`);
                    return;
                }
                const taskKey = `file-${filmData.title}`;
                const startTime = Date.now();
                this.lastTaskStartTimes.set(taskKey, startTime);
                logger_1.default.debug(`QueueManager: [文件写入] 开始任务: ${filmData.title}`);
                try {
                    await this.fileHandler.writeFilmDataToFile(filmData);
                    const persistedFilmData = this.fileHandler.getPersistedFilmData(filmData) || filmData;
                    this.emit({
                        type: QueueEventType.FILM_DATA_SAVED,
                        data: { filmData: persistedFilmData }
                    });
                    const writeTime = Date.now() - startTime;
                    logger_1.default.debug(`QueueManager: [文件写入] 完成写入: ${filmData.title} (耗时: ${Math.round(writeTime / 1000)}s)`);
                }
                catch (error) {
                    const failedTime = Date.now() - startTime;
                    logger_1.default.error(`QueueManager: [文件写入] 任务失败: ${filmData.title} (耗时: ${Math.round(failedTime / 1000)}s), 错误: ${error instanceof Error ? error.message : String(error)}`);
                    throw error;
                }
                finally {
                    this.lastTaskStartTimes.delete(taskKey);
                }
            }, fileWriteConcurrency);
            // 添加队列事件监听
            this.fileWriteQueue.error((error, task) => {
                logger_1.default.error(`QueueManager: [文件写入] 队列错误，任务: ${task.title}，错误: ${error instanceof Error ? error.message : String(error)}`);
            });
            logger_1.default.debug('QueueManager: 文件写入队列创建完成');
        }
        return this.fileWriteQueue;
    }
    /**
     * 获取详情页处理队列
     * @returns {async.QueueObject<DetailPageTask>} 详情页处理队列实例
     */
    getDetailPageQueue() {
        if (!this.detailPageQueue) {
            logger_1.default.debug('QueueManager: 创建详情页处理队列');
            // 详情页默认尽量贴近用户配置的真实并发，仅在 Puppeteer 共享池完全占满时轻微收敛。
            let detailPageConcurrency = Math.max(1, this.config.parallel);
            if (this.config.useCloudflareBypass && this.resourceMonitor) {
                const poolStats = this.puppeteerPool.getStats();
                if (poolStats.total > 0 && poolStats.inUse >= poolStats.total) {
                    detailPageConcurrency = Math.max(1, Math.min(detailPageConcurrency, poolStats.total + 1));
                    logger_1.default.debug(`QueueManager: Puppeteer池已满，临时收敛详情页队列并发数至 ${detailPageConcurrency}`);
                }
            }
            logger_1.default.debug(`QueueManager: 详情页队列并发数: ${detailPageConcurrency}`);
            this.detailPageQueue = async_1.default.queue(async (task) => {
                if (this.isShuttingDown) {
                    logger_1.default.info(`QueueManager: [详情页] 已收到终止指令，跳过任务: ${task.link}`);
                    return;
                }
                const taskKey = `detail-${task.link}`;
                const startTime = Date.now();
                this.lastTaskStartTimes.set(taskKey, startTime);
                logger_1.default.debug(`QueueManager: [详情页] 开始处理: ${task.link}`);
                try {
                    this.emit({ type: QueueEventType.DETAIL_PAGE_START, data: { link: task.link } });
                    logger_1.default.debug(`QueueManager: [详情页] 触发页面请求事件: ${task.link}`);
                    logger_1.default.debug(`QueueManager: [详情页] 开始请求页面内容: ${task.link}`);
                    const response = await this.requestHandler.getPage(task.link);
                    const requestTime = Date.now() - startTime;
                    logger_1.default.debug(`QueueManager: [详情页] 页面请求完成: ${task.link} (耗时: ${Math.round(requestTime / 1000)}s)`);
                    if (this.isShuttingDown) {
                        logger_1.default.info(`QueueManager: [详情页] 请求后检测到终止指令，停止后续处理: ${task.link}`);
                        return;
                    }
                    if (response?.body) {
                        logger_1.default.debug(`QueueManager: [详情页] 成功获取页面内容，长度: ${response.body.length}`);
                        logger_1.default.debug(`QueueManager: [详情页] 开始解析元数据: ${task.link}`);
                        const metadata = parser_1.default.parseMetadata(response.body);
                        const parseTime = Date.now() - startTime;
                        logger_1.default.debug(`QueueManager: [详情页] 元数据解析完成: ${metadata.title} (总耗时: ${Math.round(parseTime / 1000)}s)`);
                        const filmData = parser_1.default.parseFilmData(metadata, task.link);
                        if (this.isShuttingDown) {
                            logger_1.default.info(`QueueManager: [详情页] 元数据解析后检测到终止指令，停止后续处理: ${task.link}`);
                            return;
                        }
                        const magnetTask = {
                            sourceLink: task.link,
                            metadata,
                            filmData
                        };
                        if (this.config.useCloudflareBypass && this.requestHandler.shouldRouteMagnetTaskToRecoveryQueue()) {
                            logger_1.default.debug(`QueueManager: [详情页] 快速磁力已熔断，直接转入 Cloudflare 补抓队列: ${metadata.title}`);
                            this.getMagnetRecoveryQueue().push(magnetTask);
                        }
                        else {
                            logger_1.default.debug(`QueueManager: [详情页] 影片数据解析完成，已转入快速磁力队列: ${metadata.title}`);
                            this.getMagnetFastQueue().push(magnetTask);
                        }
                    }
                    else {
                        logger_1.default.warn(`QueueManager: [详情页] 页面响应为空: ${task.link}`);
                    }
                    // 延迟由外部管理器处理，任务完成后立即释放
                    logger_1.default.debug(`QueueManager: [详情页] 任务完成: ${task.link}`);
                }
                catch (error) {
                    const message = error instanceof Error ? error.message : String(error);
                    if (this.isShuttingDown || message === 'Request cancelled') {
                        logger_1.default.info(`QueueManager: [详情页] 任务已终止: ${task.link}`);
                        return;
                    }
                    const failedTime = Date.now() - startTime;
                    logger_1.default.error(`QueueManager: [详情页] 任务失败: ${task.link} (耗时: ${Math.round(failedTime / 1000)}s), 错误: ${message}`);
                    this.emit({
                        type: QueueEventType.DETAIL_PAGE_FAILED,
                        data: { link: task.link, reason: message }
                    });
                    errorHandler_1.ErrorHandler.handleGenericError(error, `处理详情页 ${task.link}`);
                    // 不中断队列处理，继续处理下一个任务
                }
                finally {
                    this.lastTaskStartTimes.delete(taskKey);
                }
            }, detailPageConcurrency);
            // 添加队列事件监听
            this.detailPageQueue.error((error, task) => {
                logger_1.default.error(`QueueManager: [详情页] 队列错误，任务: ${task.link}，错误: ${error instanceof Error ? error.message : String(error)}`);
            });
            logger_1.default.debug('QueueManager: 详情页处理队列创建完成');
        }
        return this.detailPageQueue;
    }
    getMagnetFastQueue() {
        if (!this.magnetFastQueue) {
            const concurrency = (0, queueManagerMagnetQueueUtils_1.getFastMagnetQueueConcurrency)(this.config.parallel);
            logger_1.default.debug(`QueueManager: [\u5FEB\u901F\u78C1\u529B] \u521B\u5EFA\u961F\u5217\uFF0C\u5E76\u53D1\u6570: ${concurrency}`);
            this.magnetFastQueue = async_1.default.queue(async (task) => {
                if (this.isShuttingDown) {
                    logger_1.default.info(`QueueManager: [\u5FEB\u901F\u78C1\u529B] \u5DF2\u6536\u5230\u7EC8\u6B62\u6307\u4EE4\uFF0C\u8DF3\u8FC7\u4EFB\u52A1: ${task.sourceLink}`);
                    return;
                }
                const taskKey = `magnet-fast-${task.sourceLink}`;
                const startTime = Date.now();
                this.lastTaskStartTimes.set(taskKey, startTime);
                try {
                    await (0, queueManagerMagnetQueueUtils_1.processFastMagnetTask)({
                        task,
                        requestHandler: this.requestHandler,
                        useCloudflareBypass: Boolean(this.config.useCloudflareBypass),
                        isShuttingDown: () => this.isShuttingDown,
                        routeToRecoveryQueue: (recoveryTask) => this.getMagnetRecoveryQueue().push(recoveryTask),
                        emitProcessed: (data) => {
                            this.emit({
                                type: QueueEventType.DETAIL_PAGE_PROCESSED,
                                data
                            });
                        }
                    });
                }
                finally {
                    this.lastTaskStartTimes.delete(taskKey);
                }
            }, concurrency);
            this.magnetFastQueue.error((error, task) => {
                logger_1.default.error(`QueueManager: [\u5FEB\u901F\u78C1\u529B] \u961F\u5217\u9519\u8BEF\uFF0C\u4EFB\u52A1: ${task.sourceLink}\uFF0C\u9519\u8BEF: ${error instanceof Error ? error.message : String(error)}`);
            });
        }
        return this.magnetFastQueue;
    }
    getMagnetRecoveryQueue() {
        if (!this.magnetRecoveryQueue) {
            const concurrency = (0, queueManagerMagnetQueueUtils_1.getRecoveryMagnetQueueConcurrency)(Boolean(this.config.useCloudflareBypass), this.config.parallel);
            logger_1.default.debug(`QueueManager: [Cloudflare\u8865\u6293] \u521B\u5EFA\u961F\u5217\uFF0C\u5E76\u53D1\u6570: ${concurrency}`);
            this.magnetRecoveryQueue = async_1.default.queue(async (task) => {
                if (this.isShuttingDown) {
                    logger_1.default.info(`QueueManager: [Cloudflare\u8865\u6293] \u5DF2\u6536\u5230\u7EC8\u6B62\u6307\u4EE4\uFF0C\u8DF3\u8FC7\u4EFB\u52A1: ${task.sourceLink}`);
                    return;
                }
                const taskKey = `magnet-recovery-${task.sourceLink}`;
                this.lastTaskStartTimes.set(taskKey, Date.now());
                try {
                    await (0, queueManagerMagnetQueueUtils_1.processRecoveryMagnetTask)({
                        task,
                        requestHandler: this.requestHandler,
                        isShuttingDown: () => this.isShuttingDown,
                        emitProcessed: (data) => {
                            this.emit({
                                type: QueueEventType.DETAIL_PAGE_PROCESSED,
                                data
                            });
                        }
                    });
                }
                finally {
                    this.lastTaskStartTimes.delete(taskKey);
                }
            }, concurrency);
            this.magnetRecoveryQueue.error((error, task) => {
                logger_1.default.error(`QueueManager: [Cloudflare\u8865\u6293] \u961F\u5217\u9519\u8BEF\uFF0C\u4EFB\u52A1: ${task.sourceLink}\uFF0C\u9519\u8BEF: ${error instanceof Error ? error.message : String(error)}`);
            });
        }
        return this.magnetRecoveryQueue;
    }
    /**
     * 获取索引页处理队列
     * @returns {async.QueueObject<IndexPageTask>} 索引页处理队列实例
     */
    getIndexPageQueue() {
        if (!this.indexPageQueue) {
            logger_1.default.debug('QueueManager: 创建索引页队列');
            // 根据资源状态动态调整并发数
            let concurrency = this.config.parallel;
            if (this.config.useCloudflareBypass && this.resourceMonitor) {
                const poolStats = this.puppeteerPool.getStats();
                // 如果Puppeteer池使用率过高，降低并发
                if (poolStats.inUse >= poolStats.total * 0.8) {
                    concurrency = Math.max(1, Math.floor(concurrency * 0.7));
                    logger_1.default.debug(`QueueManager: Puppeteer池使用率高，降低索引页队列并发数至 ${concurrency}`);
                }
            }
            logger_1.default.debug(`QueueManager: 索引页队列并发数: ${concurrency}`);
            this.indexPageQueue = async_1.default.queue(async (task) => {
                if (this.isShuttingDown) {
                    logger_1.default.info(`QueueManager: [索引页] 已收到终止指令，跳过任务: ${task.url}`);
                    task.resolve?.([]);
                    return;
                }
                const taskKey = `index-${task.url}`;
                const startTime = Date.now();
                this.lastTaskStartTimes.set(taskKey, startTime);
                logger_1.default.debug(`QueueManager: [索引页] 开始处理: ${task.url}`);
                logger_1.default.debug(`QueueManager: [索引页] 队列当前状态 - 等待: ${this.indexPageQueue?.length()}, 运行: ${this.indexPageQueue?.running()}`);
                try {
                    this.emit({ type: QueueEventType.INDEX_PAGE_START, data: { link: task.url } });
                    logger_1.default.debug(`QueueManager: [索引页] 触发页面请求事件: ${task.url}`);
                    const response = await this.requestHandler.getPage(task.url);
                    if (this.isShuttingDown) {
                        logger_1.default.info(`QueueManager: [索引页] 请求后检测到终止指令，停止后续处理: ${task.url}`);
                        task.resolve?.([]);
                        return;
                    }
                    const requestTime = Date.now() - startTime;
                    logger_1.default.debug(`QueueManager: [索引页] 页面请求完成: ${task.url} (耗时: ${Math.round(requestTime / 1000)}s)`);
                    if (!response || !response.body) {
                        logger_1.default.warn(`QueueManager: [索引页] 页面响应为空: ${task.url}`);
                        task.resolve?.([]);
                        this.emit({
                            type: QueueEventType.INDEX_PAGE_PROCESSED,
                            data: { url: task.url, links: [] }
                        });
                        return;
                    }
                    logger_1.default.debug(`QueueManager: [索引页] 开始解析页面链接: ${task.url}`);
                    const links = parser_1.default.parsePageLinks(response.body);
                    const parseTime = Date.now() - startTime;
                    logger_1.default.debug(`QueueManager: [索引页] 页面解析完成: ${task.url}，找到 ${links.length} 条链接 (总耗时: ${Math.round(parseTime / 1000)}s)`);
                    if (links.length === 0) {
                        logger_1.default.warn(`QueueManager: [索引页] 未解析到影片链接: ${task.url}`);
                        logger_1.default.debug(`QueueManager: [索引页] 页面内容片段 (前1000字符): ${response.body.substring(0, 1000)}`);
                    }
                    this.emit({
                        type: QueueEventType.INDEX_PAGE_PROCESSED,
                        data: { url: task.url, links }
                    });
                    task.resolve?.(links);
                    // 延迟由外部管理器处理，任务完成后立即释放
                    logger_1.default.debug(`QueueManager: [索引页] 任务完成: ${task.url}`);
                }
                catch (error) {
                    const message = error instanceof Error ? error.message : String(error);
                    if (this.isShuttingDown || message === 'Request cancelled') {
                        logger_1.default.info(`QueueManager: [索引页] 任务已终止: ${task.url}`);
                        task.resolve?.([]);
                        return;
                    }
                    const failedTime = Date.now() - startTime;
                    logger_1.default.error(`QueueManager: [索引页] 任务失败: ${task.url} (耗时: ${Math.round(failedTime / 1000)}s), 错误: ${message}`);
                    task.reject?.(error instanceof Error ? error : new Error(String(error)));
                    throw error;
                }
                finally {
                    this.lastTaskStartTimes.delete(taskKey);
                }
            }, concurrency);
            // 添加队列事件监听
            this.indexPageQueue.error((error, task) => {
                logger_1.default.error(`QueueManager: [索引页] 队列错误，任务: ${task.url}，错误: ${error instanceof Error ? error.message : String(error)}`);
            });
            logger_1.default.debug('QueueManager: 索引页队列创建完成');
        }
        return this.indexPageQueue;
    }
    async fetchIndexPageLinks(url) {
        return new Promise((resolve, reject) => {
            this.getIndexPageQueue().push({
                url,
                resolve,
                reject
            });
        });
    }
    /**
     * 创建一个错误处理函数，用于处理队列任务执行过程中发生的错误。
     * @static
     * @param {string} queueName - 队列的名称，用于在日志中标识出错的队列。
     * @returns {(err: Error, task: any) => void} 一个错误处理函数，接收错误对象和任务对象作为参数。
     */
    static createErrorHandler(queueName) {
        return (err, task) => {
            logger_1.default.error(`[${queueName}] 处理任务时出错: ${err.message}`);
            // logger.debug(`错误详情: ${err.stack}`);
        };
    }
    /**
     * 注册事件监听器
     * @param {QueueEventType} eventType - 事件类型
     * @param {EventHandler} handler - 事件处理函数
     * @returns {void}
     */
    on(eventType, handler) {
        if (!this.eventHandlers.has(eventType)) {
            this.eventHandlers.set(eventType, []);
        }
        this.eventHandlers.get(eventType)?.push(handler);
    }
    /**
     * 移除所有事件监听器，防止 ScraperRunner 与 QueueManager 之间形成循环引用。
     */
    removeAllListeners() {
        this.eventHandlers.clear();
    }
    /**
     * 获取队列状态统计信息
     * @returns {Object} 包含各队列统计信息的对象
     */
    getQueueStats() {
        return {
            indexPageQueue: {
                waiting: this.indexPageQueue?.length() || 0,
                running: this.indexPageQueue?.running() || 0
            },
            detailPageQueue: {
                waiting: this.detailPageQueue?.length() || 0,
                running: this.detailPageQueue?.running() || 0
            },
            magnetFastQueue: {
                waiting: this.magnetFastQueue?.length() || 0,
                running: this.magnetFastQueue?.running() || 0
            },
            magnetRecoveryQueue: {
                waiting: this.magnetRecoveryQueue?.length() || 0,
                running: this.magnetRecoveryQueue?.running() || 0
            },
            fileWriteQueue: {
                waiting: this.fileWriteQueue?.length() || 0,
                running: this.fileWriteQueue?.running() || 0
            },
            imageDownloadQueue: {
                waiting: this.imageDownloadQueue?.length() || 0,
                running: this.imageDownloadQueue?.running() || 0
            }
        };
    }
    /**
     * 检查是否所有队列都已完成
     * @returns {boolean} 如果所有队列都已完成返回 true
     */
    areAllQueuesFinished() {
        const stats = this.getQueueStats();
        return (stats.indexPageQueue.waiting === 0 && stats.indexPageQueue.running === 0 &&
            stats.detailPageQueue.waiting === 0 && stats.detailPageQueue.running === 0 &&
            stats.magnetFastQueue.waiting === 0 && stats.magnetFastQueue.running === 0 &&
            stats.magnetRecoveryQueue.waiting === 0 && stats.magnetRecoveryQueue.running === 0 &&
            stats.fileWriteQueue.waiting === 0 && stats.fileWriteQueue.running === 0 &&
            stats.imageDownloadQueue.waiting === 0 && stats.imageDownloadQueue.running === 0);
    }
    emit(event) {
        const handlers = this.eventHandlers.get(event.type);
        handlers?.forEach(handler => handler(event));
    }
    /**
     * 启动队列状态监控
     * @private
     */
    startQueueMonitoring() {
        this.queueStatsInterval = setInterval(() => {
            if (this.isShuttingDown)
                return;
            const stats = this.getQueueStats();
            const runningTasks = this.lastTaskStartTimes.size;
            const currentTime = Date.now();
            // 检查长时间运行的任务
            const longRunningTasks = [];
            for (const [taskKey, startTime] of this.lastTaskStartTimes.entries()) {
                const runTime = currentTime - startTime;
                if (runTime > 60000) { // 超过1分钟的任务
                    longRunningTasks.push(`${taskKey} (${Math.round(runTime / 1000)}s)`);
                }
            }
            // 每30秒输出一次状态报告
            if (runningTasks > 0 || longRunningTasks.length > 0) {
                logger_1.default.info(`QueueManager: [心跳] 队列状态 - 索引(等待:${stats.indexPageQueue.waiting}, 运行:${stats.indexPageQueue.running}) ` +
                    `详情(等待:${stats.detailPageQueue.waiting}, 运行:${stats.detailPageQueue.running}) ` +
                    `快速磁力(等待:${stats.magnetFastQueue.waiting}, 运行:${stats.magnetFastQueue.running}) ` +
                    `Cloudflare补抓(等待:${stats.magnetRecoveryQueue.waiting}, 运行:${stats.magnetRecoveryQueue.running}) ` +
                    `文件(等待:${stats.fileWriteQueue.waiting}, 运行:${stats.fileWriteQueue.running}) ` +
                    `图片(等待:${stats.imageDownloadQueue.waiting}, 运行:${stats.imageDownloadQueue.running}) ` +
                    `活跃任务:${runningTasks}`);
                if (longRunningTasks.length > 0) {
                    logger_1.default.warn(`QueueManager: [警告] 长时间运行的任务: ${longRunningTasks.join(', ')}`);
                }
            }
        }, 30000); // 每30秒检查一次
    }
    /**
     * 停止队列监控并清理资源
     */
    async shutdown() {
        logger_1.default.info('QueueManager: 开始关闭队列管理器...');
        this.isShuttingDown = true;
        // 停止资源监控器，避免进程级定时器泄漏
        if (this.config.useCloudflareBypass && this.resourceMonitor) {
            logger_1.default.debug('QueueManager: 正在停止资源监控器...');
            this.resourceMonitor.stopMonitoring();
        }
        // 关闭延迟管理器
        logger_1.default.debug('QueueManager: 正在关闭延迟管理器...');
        this.interruptAllDelays();
        try {
            await this.requestHandler.close();
        }
        catch (error) {
            logger_1.default.warn(`QueueManager: 关闭请求处理器失败: ${error instanceof Error ? error.message : String(error)}`);
        }
        if (this.queueStatsInterval) {
            clearInterval(this.queueStatsInterval);
            this.queueStatsInterval = null;
        }
        // 清理所有队列
        const queues = [
            { queue: this.indexPageQueue, name: '索引页队列' },
            { queue: this.detailPageQueue, name: '详情页队列' },
            { queue: this.magnetFastQueue, name: '快速磁力队列' },
            { queue: this.magnetRecoveryQueue, name: 'Cloudflare补抓队列' },
            { queue: this.fileWriteQueue, name: '文件写入队列' },
            { queue: this.imageDownloadQueue, name: '图片下载队列' }
        ];
        for (const { queue, name } of queues) {
            if (queue) {
                queue.empty();
                queue.kill();
                logger_1.default.debug(`QueueManager: 已清理${name}`);
            }
        }
        // 检查未完成的任务
        if (this.lastTaskStartTimes.size > 0) {
            logger_1.default.warn(`QueueManager: 关闭时仍有 ${this.lastTaskStartTimes.size} 个任务在运行`);
            for (const [taskKey, startTime] of this.lastTaskStartTimes.entries()) {
                const runTime = Date.now() - startTime;
                logger_1.default.warn(`QueueManager: 未完成任务: ${taskKey} (运行时间: ${Math.round(runTime / 1000)}s)`);
            }
        }
        try {
            await this.fileHandler.close();
        }
        catch (error) {
            logger_1.default.warn(`QueueManager: 刷新输出文件失败: ${error instanceof Error ? error.message : String(error)}`);
        }
        logger_1.default.info('QueueManager: 队列管理器关闭完成');
    }
    async flushOutputs() {
        await this.fileHandler.flush(true);
    }
    /**
     * 创建延迟任务
     */
    async createDelay(type, id) {
        return delayManager_1.delayManager.createDelay(type, id);
    }
    /**
     * 获取延迟统计信息
     */
    getDelayStats() {
        return delayManager_1.delayManager.getDelayStats();
    }
    /**
     * 检查是否有活跃的延迟
     */
    hasActiveDelays() {
        return delayManager_1.delayManager.hasActiveDelays();
    }
    /**
     * 等待所有延迟完成
     */
    async waitForDelays() {
        await delayManager_1.delayManager.waitForAllDelays();
    }
    /**
     * 中断所有延迟
     */
    interruptAllDelays() {
        return delayManager_1.delayManager.interruptAllDelays();
    }
    /**
     * 改进的队列完成检查 - 区分实际工作和延迟
     */
    areWorkQueuesFinished() {
        const stats = this.getQueueStats();
        return (stats.indexPageQueue.waiting === 0 &&
            stats.indexPageQueue.running === 0 &&
            stats.detailPageQueue.waiting === 0 &&
            stats.detailPageQueue.running === 0 &&
            stats.magnetFastQueue.waiting === 0 &&
            stats.magnetFastQueue.running === 0 &&
            stats.magnetRecoveryQueue.waiting === 0 &&
            stats.magnetRecoveryQueue.running === 0 &&
            stats.fileWriteQueue.waiting === 0 &&
            stats.fileWriteQueue.running === 0 &&
            stats.imageDownloadQueue.waiting === 0 &&
            stats.imageDownloadQueue.running === 0);
    }
    extractItemFromTaskValue(value) {
        const normalizedValue = String(value || '').trim();
        if (!normalizedValue) {
            return '';
        }
        try {
            const parsed = new URL(normalizedValue);
            const segments = parsed.pathname.split('/').filter(Boolean);
            return segments.length > 0 ? segments[segments.length - 1] : normalizedValue;
        }
        catch {
            return normalizedValue;
        }
    }
    buildRunningTaskSummary(taskKey, startTime) {
        const taskName = String(taskKey || '');
        const runtimeSeconds = Math.max(0, Math.round((Date.now() - startTime) / 1000));
        const taskPatterns = [
            { prefix: 'detail-', stage: '详情页抓取' },
            { prefix: 'magnet-fast-', stage: '快速磁力抓取' },
            { prefix: 'magnet-recovery-', stage: 'Cloudflare 补抓' },
            { prefix: 'file-', stage: '结果写入' },
            { prefix: 'image-', stage: '图片下载' }
        ];
        for (const pattern of taskPatterns) {
            if (!taskName.startsWith(pattern.prefix)) {
                continue;
            }
            const rawValue = taskName.slice(pattern.prefix.length);
            return {
                taskKey: taskName,
                stage: pattern.stage,
                item: this.extractItemFromTaskValue(rawValue),
                rawValue,
                runtimeSeconds
            };
        }
        return {
            taskKey: taskName,
            stage: '未知阶段',
            item: this.extractItemFromTaskValue(taskName),
            rawValue: taskName,
            runtimeSeconds
        };
    }
    getRunningTaskSummaries() {
        return Array.from(this.lastTaskStartTimes.entries())
            .map(([taskKey, startTime]) => this.buildRunningTaskSummary(taskKey, startTime))
            .sort((left, right) => right.runtimeSeconds - left.runtimeSeconds);
    }
    /**
     * 获取详细的队列状态信息
     */
    getDetailedQueueStatus() {
        const stats = this.getQueueStats();
        const runningTasks = Array.from(this.lastTaskStartTimes.entries())
            .map(([key, start]) => `${key} (${Math.round((Date.now() - start) / 1000)}s)`);
        return `队列状态详细报告:
索引页队列: 等待${stats.indexPageQueue.waiting}, 运行${stats.indexPageQueue.running}
详情页队列: 等待${stats.detailPageQueue.waiting}, 运行${stats.detailPageQueue.running}
快速磁力队列: 等待${stats.magnetFastQueue.waiting}, 运行${stats.magnetFastQueue.running}
Cloudflare补抓队列: 等待${stats.magnetRecoveryQueue.waiting}, 运行${stats.magnetRecoveryQueue.running}
文件写入队列: 等待${stats.fileWriteQueue.waiting}, 运行${stats.fileWriteQueue.running}
图片下载队列: 等待${stats.imageDownloadQueue.waiting}, 运行${stats.imageDownloadQueue.running}
活跃任务: ${runningTasks.length > 0 ? runningTasks.join(', ') : '无'}`;
    }
}
exports.default = QueueManager;
export { QueueEventType, QueueManager as default };
//# sourceMappingURL=queueManager.js.map
