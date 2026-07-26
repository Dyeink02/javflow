// @ts-nocheck
"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
const axios_1 = __importDefault(require("axios"));
const axios_retry_1 = __importDefault(require("axios-retry"));
const https_1 = __importDefault(require("https"));
const path_1 = __importDefault(require("path"));
const fs_1 = __importDefault(require("fs"));
const constants_1 = require("./constants");
const errorHandler_1 = require("../utils/errorHandler");
const logger_1 = __importDefault(require("./logger"));
const cloudflareBypass_1 = __importDefault(require("../utils/cloudflareBypass"));
const puppeteerPool_1 = require("./puppeteerPool");
const cloudflareAjaxWorkerClient_1 = __importDefault(require("./cloudflareAjaxWorkerClient"));
const demoProfile_1 = require("./demoProfile");
const requestHandlerPageUtils_1 = require("./requestHandlerPageUtils");
const requestHandlerCloudflareUtils_1 = require("./requestHandlerCloudflareUtils");
const requestHandlerMagnetUtils_1 = require("./requestHandlerMagnetUtils");
const magnetContentValidation_1 = require("./magnetContentValidation");
const requestHandlerBaseOriginUtils_1 = require("./requestHandlerBaseOriginUtils");
const requestHandlerLifecycleUtils_1 = require("./requestHandlerLifecycleUtils");
const requestHandlerProxyManager_1 = require("./requestHandlerProxyManager");
const requestHandlerTypes_1 = require("./requestHandlerTypes");
/**
 * RequestHandler 负责页面请求、磁力 AJAX 获取与 Cloudflare 恢复链路调度。
 */
class RequestHandler {
    /**
     * 构造函数
     * @param config 配置对象
     */
    constructor(config) {
        this.cloudflareBypass = null;
        this.cloudflareCookies = null;
        this.lastCookieRefresh = 0;
        this.cookieRefreshInterval = 30 * 60 * 1000; // 30分钟刷新一次cookies
        this.activeAbortControllers = new Set();
        this.isClosed = false;
        this.cloudflareAjaxWorkerClients = [];
        this.cloudflareCookieRefreshPromise = null;
        this.cloudflareAjaxGateState = { inFlight: 0, waiters: [] };
        this.cloudflarePrewarmPromise = null;
        this.cloudflareRecoveryFailureThreshold = 2;
        this.cloudflareRecoveryCooldownMs = 5 * 60 * 1000;
        this.cloudflareRecoveryRuntime = {
            consecutiveFailures: 0,
            cooldownUntil: 0,
            lastError: '',
            noticeShown: false
        };
        this.fastHttpMagnetBypassNoticeShown = false;
        this.cloudflareWorkerDisabled = false;
        this.cloudflareWorkerDisableReason = '';
        this.preferredAjaxBaseOrigin = null;
        this.preferredPageBaseOrigin = null;
        this.ajaxBaseOriginHealth = new Map();
        this.pageBaseOriginHealth = new Map();
        this.ajaxBaseOriginCooldownMs = 10 * 60 * 1000;
        this.baseOriginCandidateLimit = 4;
        this.proxyFailureThreshold = 2;
        this.proxyCooldownMs = 30 * 1000;
        this.defaultCookieHeader = 'existmag=mag; age=verified; dv=1; age_verified=1; adult_verified=1; age_verification=1; age_verification_passed=true; is_adult=true; javbus_age=1';
        this.fastHttpMagnetCircuit = {
            attempts: 0,
            successes: 0,
            failures: 0,
            consecutiveFailures: 0,
            disabled: false,
            disabledAt: null,
            disableReason: ''
        };
        this.magnetValidationAbortController = new AbortController();
        this.magnetValidationCooldownMs = 2 * 60 * 1000;
        this.magnetValidationRuntime = {
            inspectedCandidates: 0,
            unverifiedCandidates: 0,
            timeoutCandidates: 0,
            disabled: false,
            disabledAt: null,
            cooldownUntil: 0,
            disableReason: '',
            disableNoticeShown: false
        };
        this.magnetValidationInspectLimit = 2;
        this.magnetValidationBudgetMs = 9000;
        this.config = config;
        this.demoProfile = (0, demoProfile_1.getDemoProfile)({
            demoMode: this.config.demoMode,
            demoLabel: this.config.demoLabel,
            productDisplayName: this.config.productDisplayName
        });
        const hasProxy = Boolean(this.config.proxy);
        logger_1.default.debug(`RequestHandler constructor - proxy configured: ${hasProxy}`);
        const userAgent = constants_1.USER_AGENTS[Math.floor(Math.random() * constants_1.USER_AGENTS.length)];
        const isChrome = userAgent.includes('Chrome');
        const isFirefox = userAgent.includes('Firefox');
        const isEdge = userAgent.includes('Edge');
        // 提取浏览器版本号
        const versionMatch = userAgent.match(/(Chrome|Firefox|Edge|Edg)[\/\s](\d+)/);
        const browserVersion = versionMatch ? versionMatch[2] : '119';
        // 设置 Sec-Ch-Ua 和浏览器指纹
        let secChUa = '';
        let platform = '"Windows"';
        let secChUaMobile = '?0';
        let secChUaPlatform = platform;
        if (isChrome || isEdge) {
            const brandVersion = isEdge ? 'Microsoft Edge' : 'Chromium';
            secChUa = `"${brandVersion}";v="${browserVersion}", "Not?A_Brand";v="99"`;
        }
        else if (isFirefox) {
            secChUa = `"Not.A/Brand";v="8", "Chromium";v="${browserVersion}", "Google Chrome";v="${browserVersion}"`;
        }
        else {
            secChUa = `"Chromium";v="${browserVersion}", "Not?A_Brand";v="99"`;
        }
        ;
        // 设置请求配置
        this.requestConfig = {
            timeout: config.timeout || 30000, // 默认30秒超时
            proxy: config.proxy,
            headers: {
                'authority': new URL(this.config.base || this.config.BASE_URL).hostname,
                'method': 'GET',
                'path': '/',
                'scheme': 'https',
                'accept': 'text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7',
                'accept-encoding': 'gzip, deflate, br',
                'accept-language': 'zh-CN,zh;q=0.9,en;q=0.8,en-US;q=0.7',
                'cache-control': 'no-cache',
                'sec-ch-ua': secChUa,
                'sec-ch-ua-mobile': secChUaMobile,
                'sec-ch-ua-platform': secChUaPlatform,
                'sec-fetch-dest': 'document',
                'sec-fetch-mode': 'navigate',
                'sec-fetch-site': 'none',
                'sec-fetch-user': '?1',
                'upgrade-insecure-requests': '1',
                'user-agent': userAgent,
                'referer': new URL(this.config.base || this.config.BASE_URL).origin,
                'Cookie': this.config.headers.Cookie || this.defaultCookieHeader,
                'Connection': 'keep-alive'
            }
        };
        this.proxyManager = new requestHandlerProxyManager_1.RequestHandlerProxyManager({
            config: this.config,
            requestConfig: this.requestConfig,
            logger: logger_1.default,
            proxyFailureThreshold: this.proxyFailureThreshold,
            proxyCooldownMs: this.proxyCooldownMs,
            onProxySwitch: (reason) => this.resetCloudflareTransportForProxySwitch(reason)
        });
        this.initializeProxyPool(config.proxy);
        if (this.proxyManager.getProxyPool().length === 0) {
            this.requestConfig.proxy = undefined;
            this.config.proxy = undefined;
        }
        this.retries = 3; // 减少重试次数，避免过度请求
        this.retryDelay = Math.max(this.config.delay || 2000, 3000); // 增加基础延迟，至少3秒
        // 获取共享的 PuppeteerPool 实例（不创建新实例，由 QueueManager 统一管理）
        this.puppeteerPool = puppeteerPool_1.PuppeteerPool.getInstance();
        // Cloudflare 绕过器将在需要时异步初始化
        // 配置axios重试
        (0, axios_retry_1.default)(axios_1.default, {
            retries: this.retries, // 使用配置的重试次数
            retryDelay: (retryCount) => {
                // 指数退避策略，加上随机延迟，基础延迟更长
                const baseDelay = Math.max(this.retryDelay, 3000); // 至少3秒基础延迟
                const exponentialDelay = Math.min(baseDelay * Math.pow(1.5, retryCount), 30000); // 1.5倍指数增长
                const randomDelay = Math.floor(Math.random() * 2000); // 0-2秒随机延迟
                const totalDelay = exponentialDelay + randomDelay;
                logger_1.default.debug(`重试延迟计算: 基础=${Math.round(baseDelay / 1000)}秒, 指数增长=${Math.round(exponentialDelay / 1000)}秒, 随机=${Math.round(randomDelay / 1000)}秒, 总计=${Math.round(totalDelay / 1000)}秒 (重试次数: ${retryCount})`);
                return totalDelay;
            },
            retryCondition: (error) => {
                // 在以下情况下重试：
                // 1. 网络错误
                // 2. 5xx服务器错误
                // 3. 429 Too Many Requests
                // 4. 403 Forbidden (Cloudflare拦截)
                // 5. 超时错误
                // 6. SSL证书错误（首次重试时自动跳过验证）
                const currentRetry = (error.config && error.config['axios-retry'] && error.config['axios-retry'].retryCount) || 0;
                const isSSLError = error.code === 'UNABLE_TO_GET_ISSUER_CERT_LOCALLY' ||
                    error.code === 'SELF_SIGNED_CERT_IN_CHAIN' ||
                    error.code === 'UNABLE_TO_VERIFY_LEAF_SIGNATURE';
                // 首次SSL错误：自动降级并跳过SSL验证
                if (isSSLError && currentRetry === 0 && this.config.strictSSL !== false) {
                    logger_1.default.warn(`检测到SSL证书错误: ${error.code}，将自动跳过验证进行重试`);
                    this.config.strictSSL = false; // 临时禁用SSL验证
                    return true; // 允许重试
                }
                const shouldRetry = axios_retry_1.default.isNetworkOrIdempotentRequestError(error) ||
                    (error.response?.status && [500, 502, 503, 504, 429, 403].includes(error.response.status)) ||
                    (error.code && ['ECONNRESET', 'ETIMEDOUT', 'ENOTFOUND', 'UNABLE_TO_GET_ISSUER_CERT_LOCALLY', 'SELF_SIGNED_CERT_IN_CHAIN', 'UNABLE_TO_VERIFY_LEAF_SIGNATURE'].includes(error.code));
                if (shouldRetry) {
                    // 计算延迟时间
                    const baseDelay = Math.max(this.retryDelay, 3000);
                    const exponentialDelay = Math.min(baseDelay * Math.pow(1.5, currentRetry), 30000);
                    const randomDelay = Math.floor(Math.random() * 2000);
                    const totalDelay = exponentialDelay + randomDelay;
                    if (isSSLError) {
                        logger_1.default.warn(`SSL证书错误，正在重试 (${currentRetry + 1}/5)，${Math.round(totalDelay / 1000)}秒后重试: ${error.config?.url || '未知URL'} - 错误: ${error.code} (已自动跳过验证)`);
                    }
                    else {
                        logger_1.default.warn(`请求失败，正在重试 (${currentRetry + 1}/5)，${Math.round(totalDelay / 1000)}秒后重试: ${error.config?.url || '未知URL'} - 错误: ${error.code || error.message}`);
                    }
                }
                return shouldRetry;
            },
            // 添加重试时更换User-Agent的逻辑
            onRetry: (retryCount, error, requestConfig) => {
                // 每次重试时更换User-Agent
                const newUserAgent = constants_1.USER_AGENTS[Math.floor(Math.random() * constants_1.USER_AGENTS.length)];
                if (requestConfig.headers) {
                    requestConfig.headers['User-Agent'] = newUserAgent;
                    // 同时更新 sec-ch-ua 以匹配新的 User-Agent
                    const isChrome = newUserAgent.includes('Chrome');
                    const isFirefox = newUserAgent.includes('Firefox');
                    const isEdge = newUserAgent.includes('Edge');
                    // 提取浏览器版本号
                    const versionMatch = newUserAgent.match(/(Chrome|Firefox|Edge|Edg)[\/\s](\d+)/);
                    const browserVersion = versionMatch ? versionMatch[2] : '119';
                    // 设置 Sec-Ch-Ua 和浏览器指纹
                    let secChUa = '';
                    if (isChrome || isEdge) {
                        const brandVersion = isEdge ? 'Microsoft Edge' : 'Chromium';
                        secChUa = `"${brandVersion}";v="${browserVersion}", "Not?A_Brand";v="99"`;
                    }
                    else if (isFirefox) {
                        secChUa = `"Not.A/Brand";v="8", "Chromium";v="${browserVersion}", "Google Chrome";v="${browserVersion}"`;
                    }
                    else {
                        secChUa = `"Chromium";v="${browserVersion}", "Not?A_Brand";v="99"`;
                    }
                    requestConfig.headers['Sec-Ch-Ua'] = secChUa;
                }
            }
        });
        // 添加HTTPS代理拦截器
        axios_1.default.interceptors.request.use((config) => {
            if (this.requestConfig.proxy && config.url?.startsWith('https')) {
                try {
                    const agent = this.createProxyAgent(this.requestConfig.proxy);
                    if (agent) {
                        config.httpsAgent = agent;
                        config.proxy = false; // 必须设置为false，否则axios会尝试使用自己的代理逻辑
                    }
                }
                catch (proxyError) {
                    errorHandler_1.ErrorHandler.handleNetworkError(proxyError, '配置代理');
                    // 可选择抛出错误或允许请求在没有代理的情况下继续
                    // throw proxyError;
                }
            }
            return config;
        });
        if (this.demoProfile.isDemoBuild) {
            logger_1.default.info(`RequestHandler: 当前运行方案 ${this.demoProfile.label || this.demoProfile.mode}，快速回退=${this.demoProfile.enableFastAjaxFallback}，Cloudflare Worker=${this.demoProfile.enableCloudflareWorker}`);
        }
    }
    initializeProxyPool(proxyValue) {
        this.proxyManager.initializeProxyPool(proxyValue);
    }
    get proxyPool() {
        return this.proxyManager.getProxyPool();
    }
    parseProxyCandidates(proxyValue) {
        return this.proxyManager.parseProxyCandidates(proxyValue);
    }
    normalizeProxyValue(value) {
        return this.proxyManager.normalizeProxyValue(value);
    }
    applyActiveProxy(proxyUrl) {
        this.proxyManager.applyActiveProxy(proxyUrl);
    }
    warnInvalidProxyValueOnce(rawValue) {
        this.proxyManager.warnInvalidProxyValueOnce(rawValue);
    }
    warnProxyAgentFailureOnce(proxyValue, reason) {
        this.proxyManager.warnProxyAgentFailureOnce(proxyValue, reason);
    }
    getActiveProxyState() {
        return this.proxyManager.getActiveProxyState();
    }
    getProxySummary(state) {
        return this.proxyManager.getProxySummary(state);
    }
    recordProxySuccess(latencyMs) {
        this.proxyManager.recordProxySuccess(latencyMs);
    }
    async recordProxyFailure(error, context) {
        await this.proxyManager.recordProxyFailure(error, context);
    }
    shouldRotateProxy(message) {
        return this.proxyManager.shouldRotateProxy(message);
    }
    async switchProxy(reason) {
        return this.proxyManager.switchProxy(reason);
    }
    selectNextProxyIndex(now = Date.now()) {
        return this.proxyManager.selectNextProxyIndex(now);
    }
    async resetCloudflareTransportForProxySwitch(reason) {
        this.cloudflareCookies = null;
        this.lastCookieRefresh = 0;
        this.cloudflarePrewarmPromise = null;
        this.cloudflareCookieRefreshPromise = null;
        this.resetCloudflareRecoveryRuntime(true);
        await this.closeCloudflareWorkerClients();
        await this.resetCloudflareBypass(reason);
    }
    async getPage(url, options = {}) {
        this.ensureRequestAvailable();
        // 验证URL格式，防止SSRF攻击
        try {
            new URL(url);
        }
        catch (error) {
            logger_1.default.error(`无效的URL格式: ${url}`);
            return null;
        }
        const initialResponse = await this.requestPageWithMirrorFallback(url, options);
        const shouldAttemptBypassRecovery = this.config.useCloudflareBypass || this.isAgeVerificationResponse(initialResponse?.body);
        if (!shouldAttemptBypassRecovery) {
            return initialResponse;
        }
        if (this.isUsablePageResponse(initialResponse)) {
            return initialResponse;
        }
        logger_1.default.info(`getPage: ${this.describePageFallbackReason(initialResponse)}，准备启用 Cloudflare 恢复链路: ${url}`);
        let recoveredCookies = null;
        try {
            recoveredCookies = await this.getCloudflareCookies();
        }
        catch (error) {
            logger_1.default.warn(`getPage: Cloudflare 会话恢复失败，将继续尝试浏览器兜底：${error instanceof Error ? error.message : String(error)}`);
        }
        if (recoveredCookies) {
            const cookieResponse = await this.requestPageWithMirrorFallback(url, options, recoveredCookies);
            if (this.isUsablePageResponse(cookieResponse)) {
                logger_1.default.info(`getPage: Cloudflare 会话恢复后，普通请求已成功获取页面: ${url}`);
                return cookieResponse;
            }
        }
        if (!this.cloudflareBypass) {
            logger_1.default.debug('getPage: Cloudflare 绕过器未初始化，开始初始化...');
            await this.initCloudflareBypass();
        }
        if (!this.cloudflareBypass) {
            logger_1.default.error(`Cloudflare 绕过器初始化失败：${url}`);
            return null;
        }
        try {
            logger_1.default.debug(`getPage: 开始使用 Puppeteer 获取页面: ${url}`);
            const pageAccessStartTime = Date.now();
            const pageContent = await this.cloudflareBypass.bypassCloudflare(url);
            const pageAccessTime = Date.now() - pageAccessStartTime;
            logger_1.default.debug(`getPage: Puppeteer 获取页面成功 (耗时: ${pageAccessTime}ms), 内容长度: ${pageContent.length}`);
            if (!this.isUsablePageBody(pageContent)) {
                logger_1.default.warn(`getPage: Puppeteer 返回页面疑似无效，已放弃本次页面结果: ${url}`);
                return null;
            }
            return { statusCode: 200, body: pageContent };
        }
        catch (error) {
            logger_1.default.error(`Cloudflare 绕过获取页面失败：${url}，错误：${error instanceof Error ? error.message : String(error)}`);
            logger_1.default.error(`错误类型：${error instanceof Error ? error.constructor.name : 'Unknown'}`);
            logger_1.default.debug(`getPage: 完整错误对象: ${JSON.stringify(error, Object.getOwnPropertyNames(error), 2)}`);
            if (this.isRecoverableBypassError(error)) {
                await this.resetCloudflareBypass(`页面获取失败，回退前重置绕过器: ${url}`);
                return null;
            }
            throw error;
        }
    }
    async getXMLHttpRequest(url, options = {}) {
        this.ensureRequestAvailable();
        const skipCloudflareCookies = Boolean(options.skipCloudflareCookies);
        const fastFallback = Boolean(options.fastFallback);
        const suppressDetailedErrorLogs = Boolean(options.suppressDetailedErrorLogs);
        // 验证URL格式，防止SSRF攻击
        try {
            new URL(url);
        }
        catch (error) {
            logger_1.default.error(`无效的URL格式: ${url}`);
            throw new Error(`无效的URL格式: ${url}`);
        }
        try {
            logger_1.default.debug(`开始发送AJAX请求: ${url}`);
            // 如果启用 Cloudflare 绕过且还没有获取 Cloudflare Cookies，先获取
            if (this.config.useCloudflareBypass && !skipCloudflareCookies && !this.cloudflareCookies) {
                await this.getCloudflareCookiesWithLane();
            }
            // 构建AJAX专用请求头
            const urlObj = new URL(url);
            const headers = {
                'authority': urlObj.hostname,
                'method': 'GET',
                'path': urlObj.pathname + urlObj.search,
                'scheme': 'https',
                'accept': '*/*',
                'accept-encoding': 'gzip, deflate, br',
                'accept-language': 'zh-CN,zh;q=0.9,en;q=0.8,en-US;q=0.7',
                'cache-control': 'no-cache',
                'sec-ch-ua': this.requestConfig.headers['sec-ch-ua'] || '"Chromium";v="120", "Not?A_Brand";v="99"',
                'sec-ch-ua-mobile': '?0',
                'sec-ch-ua-platform': '"Windows"',
                'sec-fetch-dest': 'empty',
                'sec-fetch-mode': 'cors',
                'sec-fetch-site': 'same-origin',
                'user-agent': this.requestConfig.headers['user-agent'],
                'referer': urlObj.origin + '/',
                'X-Requested-With': 'XMLHttpRequest',
                'Connection': 'keep-alive'
            };
            const requestBaseOrigin = urlObj.origin;
            const configuredBaseOrigin = this.normalizeBaseOrigin(this.config.base || this.config.BASE_URL);
            const isCrossOriginAjax = Boolean(configuredBaseOrigin &&
                requestBaseOrigin &&
                configuredBaseOrigin.toLowerCase() !== requestBaseOrigin.toLowerCase());
            // Cookie 优先级：手动设置 > Cloudflare Cookies > 默认 Cookies
            const hasManualCookies = this.config.headers && this.config.headers.Cookie &&
                this.config.headers.Cookie !== 'existmag=mag' &&
                this.config.headers.Cookie !== this.defaultCookieHeader;
            let cookieSet = false;
            if (hasManualCookies) {
                // 使用安全Cookie设置方法
                cookieSet = this.setCookieHeader(headers, this.config.headers.Cookie);
                if (cookieSet) {
                    logger_1.default.debug(`在XMLHttpRequest中使用手动设置的 Cookies`);
                }
            }
            if (!cookieSet && this.cloudflareCookies && !skipCloudflareCookies && !isCrossOriginAjax) {
                // 使用 Cloudflare 绕过获取的 Cookies
                cookieSet = this.setCookieHeader(headers, this.cloudflareCookies);
                if (cookieSet) {
                    logger_1.default.debug('在XMLHttpRequest中使用 Cloudflare Cookies');
                }
            }
            else if (!cookieSet && this.cloudflareCookies && !skipCloudflareCookies && isCrossOriginAjax) {
                logger_1.default.debug('AJAX 请求命中备用网址域名，已跳过主域 Cloudflare Cookies，改用默认年龄认证 Cookie。');
            }
            // 如果所有Cookie都无效，使用默认Cookie
            if (!cookieSet) {
                headers.Cookie = this.defaultCookieHeader;
                logger_1.default.debug('在XMLHttpRequest中使用默认年龄认证 Cookie 组合');
            }
            logger_1.default.debug(`AJAX请求头信息: ${JSON.stringify({ ...headers, Cookie: headers.Cookie ? '[已设置]' : '[未设置]' })}`);
            // 构建请求配置
            const requestConfig = {
                timeout: fastFallback ? Math.min(this.requestConfig.timeout || 30000, 12000) : this.requestConfig.timeout,
                headers,
                // 添加重试配置
                'axios-retry': {
                    retries: fastFallback ? 0 : this.retries,
                    retryDelay: (retryCount) => {
                        // AJAX请求使用更长的延迟，避免被检测
                        const baseDelay = Math.max(this.retryDelay, fastFallback ? 1500 : 4000);
                        const exponentialDelay = Math.min(baseDelay * Math.pow(1.8, retryCount), 25000); // 1.8倍指数增长
                        const randomDelay = Math.floor(Math.random() * 3000); // 0-3秒随机延迟
                        const totalDelay = exponentialDelay + randomDelay;
                        logger_1.default.debug(`AJAX重试延迟计算: 基础=${Math.round(baseDelay / 1000)}秒, 指数增长=${Math.round(exponentialDelay / 1000)}秒, 随机=${Math.round(randomDelay / 1000)}秒, 总计=${Math.round(totalDelay / 1000)}秒 (重试次数: ${retryCount})`);
                        return totalDelay;
                    },
                    retryCondition: (error) => {
                        const shouldRetry = !fastFallback &&
                            (axios_retry_1.default.isNetworkOrIdempotentRequestError(error) ||
                                (error.response?.status && [500, 502, 503, 504, 429, 403].includes(error.response.status)));
                        if (shouldRetry) {
                            const currentRetry = (error.config && error.config['axios-retry'] && error.config['axios-retry'].retryCount) || 0;
                            // 计算AJAX延迟时间
                            const baseDelay = Math.max(this.retryDelay, 4000);
                            const exponentialDelay = Math.min(baseDelay * Math.pow(1.8, currentRetry), 25000);
                            const randomDelay = Math.floor(Math.random() * 3000);
                            const totalDelay = exponentialDelay + randomDelay;
                            logger_1.default.warn(`AJAX请求失败，正在重试 (${currentRetry + 1}/3)，${Math.round(totalDelay / 1000)}秒后重试: ${url} - 错误: ${error.code || error.message}`);
                        }
                        return shouldRetry;
                    }
                }
            };
            const controller = this.createAbortController();
            requestConfig.signal = controller.signal;
            // 如果有代理配置，添加代理设置
            if (this.requestConfig.proxy) {
                try {
                    const agent = this.createProxyAgent(this.requestConfig.proxy);
                    if (agent) {
                        requestConfig.httpsAgent = agent;
                    }
                }
                catch (proxyError) {
                    logger_1.default.warn(`AJAX请求代理配置失败: ${proxyError instanceof Error ? proxyError.message : String(proxyError)}`);
                }
            }
            // 发送请求
            try {
                const requestStartTime = Date.now();
                const response = await axios_1.default.get(url, requestConfig);
                this.recordProxySuccess(Date.now() - requestStartTime);
                logger_1.default.debug(`AJAX请求成功: ${url}, 状态码: ${response.status}`);
                return { statusCode: response.status, body: response.data };
            }
            finally {
                this.releaseAbortController(controller);
            }
        }
        catch (err) {
            if (this.isAbortError(err)) {
                throw new Error('Request cancelled');
            }
            const error = err;
            await this.recordProxyFailure(error, 'AJAX 请求');
            if (suppressDetailedErrorLogs) {
                logger_1.default.debug(`AJAX请求失败(已抑制详细堆栈): ${url} | ${error.message}`);
            }
            else {
                logger_1.default.error(`AJAX请求失败: ${url}`);
                logger_1.default.error(`错误详情: ${error.message}`);
                if (error.response) {
                    logger_1.default.error(`响应状态码: ${error.response.status}`);
                    logger_1.default.error(`响应头: ${JSON.stringify(error.response.headers)}`);
                    if (error.response.data) {
                        const responseData = error.response.data;
                        if (typeof responseData === 'string' && responseData.length < 500) {
                            logger_1.default.error(`响应内容: ${responseData}`);
                        }
                        else if (typeof responseData === 'object') {
                            logger_1.default.error(`响应内容: ${JSON.stringify(responseData).substring(0, 500)}`);
                        }
                    }
                }
                errorHandler_1.ErrorHandler.handleNetworkError(error, `发送 XMLHttpRequest 到 ${url}`);
            }
            throw error;
        }
    }
    /**
     * 从指定页面提取磁力链接，并返回最大文件大小对应的磁力链接
     * @param metadata 元数据对象
     * @returns 包含磁力链接信息的对象，如果没有找到则返回 null
     */
    async fetchMagnet(metadata, options = {}) {
        this.ensureRequestAvailable();
        const fetchStartTime = Date.now();
        logger_1.default.debug(`fetchMagnet: 开始获取磁力链接，影片: ${metadata.title}`);
        const mode = options.mode || 'auto';
        const ajaxUrls = this.buildMagnetAjaxUrls(metadata);
        if (ajaxUrls.length === 0) {
            return null;
        }
        let response = null;
        const fastFallback = typeof options.fastFallback === 'boolean'
            ? options.fastFallback
            : this.demoProfile.enableFastAjaxFallback;
        if (mode !== 'cloudflare-only') {
            if (this.shouldBypassFastHttpMagnet(mode)) {
                if (!this.fastHttpMagnetBypassNoticeShown) {
                    logger_1.default.info('fetchMagnet: 快速 HTTP 磁力通道已在本轮任务中自动熔断，后续将直接切换恢复链路。');
                    this.fastHttpMagnetBypassNoticeShown = true;
                }
            }
            else {
                response = await this.fetchMagnetViaHttp(ajaxUrls, metadata.title, fetchStartTime, fastFallback, mode);
            }
        }
        if (!response && mode !== 'http-only' && this.config.useCloudflareBypass) {
            response = await this.fetchMagnetViaCloudflare(ajaxUrls[0], metadata.title, fetchStartTime);
        }
        if (!response) {
            const totalTime = Date.now() - fetchStartTime;
            logger_1.default.error(`fetchMagnet: 响应为空，返回null (总耗时: ${Math.round(totalTime / 1000)}s)`);
            return null;
        }
        return await this.parseMagnetResult(metadata.title, response.body, fetchStartTime);
    }
    buildMagnetAjaxUrls(metadata) {
        const normalizedImageParam = this.normalizeAjaxImageParam(metadata.img);
        if (!metadata.gid || !/^[a-zA-Z0-9]+$/.test(metadata.gid)) {
            logger_1.default.error(`fetchMagnet: 无效的gid参数: ${metadata.gid}`);
            return [];
        }
        if (!normalizedImageParam) {
            logger_1.default.error(`fetchMagnet: 无效的img参数: ${metadata.img}`);
            return [];
        }
        if (!metadata.uc || !/^[a-zA-Z0-9]+$/.test(metadata.uc)) {
            logger_1.default.error(`fetchMagnet: 无效的uc参数: ${metadata.uc}`);
            return [];
        }
        const query = new URLSearchParams({
            gid: metadata.gid,
            lang: 'zh',
            img: normalizedImageParam,
            uc: metadata.uc,
            floor: String(Math.floor(1e3 * Math.random() + 1))
        });
        const urls = this.getAjaxBaseOrigins().map((baseOrigin) => `${baseOrigin}/ajax/uncledatoolsbyajax.php?${query.toString()}`);
        if (urls[0]) {
            logger_1.default.debug(`fetchMagnet: 构建AJAX URL: ${urls[0]}`);
        }
        if (urls.length > 1) {
            logger_1.default.info(`fetchMagnet: 已启用 AJAX 备用网址回退，共 ${urls.length} 个候选域名。`);
        }
        return urls;
    }
    async fetchMagnetViaHttp(urls, title, fetchStartTime, fastFallback, mode) {
        let lastError = null;
        for (let index = 0; index < urls.length; index += 1) {
            const url = urls[index];
            const targetOrigin = this.normalizeBaseOrigin(url);
            const attemptStartedAt = Date.now();
            try {
                logger_1.default.debug(`fetchMagnet: 优先尝试常规 AJAX 请求: ${title}`);
                if (index > 0) {
                    logger_1.default.info(`fetchMagnet: 主通道异常，正在切换 AJAX 备用网址：${targetOrigin}`);
                }
                const regularAjaxStart = Date.now();
                const regularResponse = await this.getXMLHttpRequest(url, {
                    fastFallback,
                    // 主通道失败但仍有备用网址可回退时，不重复输出完整错误堆栈，
                    // 避免实时日志被“可恢复异常”刷屏误导为任务失败。
                    suppressDetailedErrorLogs: index + 1 < urls.length
                });
                const regularAjaxTime = Date.now() - regularAjaxStart;
                logger_1.default.debug(`fetchMagnet: 常规 AJAX 请求完成: ${title} (耗时: ${Math.round(regularAjaxTime / 1000)}s)`);
                if (regularResponse?.body && this.hasUsableMagnetPayload(regularResponse.body)) {
                    this.recordFastHttpMagnetResult(true, 'success');
                    this.recordAjaxBaseOriginSuccess(targetOrigin, Date.now() - attemptStartedAt);
                    if (targetOrigin && targetOrigin !== this.preferredAjaxBaseOrigin) {
                        this.preferredAjaxBaseOrigin = targetOrigin;
                        logger_1.default.info(`fetchMagnet: 已锁定可用 AJAX 域名：${targetOrigin}`);
                    }
                    return regularResponse;
                }
                if (regularResponse?.body) {
                    lastError = new Error('payload-invalid');
                    this.recordAjaxBaseOriginFailure(targetOrigin, 'payload-invalid');
                    logger_1.default.warn(`fetchMagnet: 常规 AJAX 未解析到有效磁力，准备切换恢复链路: ${title}`);
                }
                else {
                    lastError = new Error('empty-response');
                    this.recordAjaxBaseOriginFailure(targetOrigin, 'empty-response');
                }
            }
            catch (error) {
                lastError = error;
                this.recordAjaxBaseOriginFailure(targetOrigin, error instanceof Error ? error.message : String(error));
                const regularFailedTime = Date.now() - fetchStartTime;
                if (index + 1 < urls.length) {
                    logger_1.default.warn(`fetchMagnet: 常规 AJAX 请求失败（可回退）: ${title} (耗时: ${Math.round(regularFailedTime / 1000)}s), 错误: ${error instanceof Error ? error.message : String(error)}`);
                }
                else {
                    logger_1.default.warn(`fetchMagnet: 常规 AJAX 请求失败（已无可用回退域名）: ${title} (耗时: ${Math.round(regularFailedTime / 1000)}s), 错误: ${error instanceof Error ? error.message : String(error)}`);
                }
                if (fastFallback && this.isFastFallbackAjaxError(error)) {
                    logger_1.default.info(`fetchMagnet: 已触发快速分流，准备直接切换恢复链路: ${title}`);
                }
            }
        }
        this.recordFastHttpMagnetResult(false, lastError instanceof Error ? lastError.message : String(lastError || 'unknown'));
        if (mode === 'auto' &&
            !this.config.useCloudflareBypass &&
            lastError &&
            !this.isNonThrowingMagnetFailure(lastError)) {
            throw lastError;
        }
        return null;
    }
    isNonThrowingMagnetFailure(error) {
        const message = error instanceof Error ? error.message : String(error || '');
        return ['payload-invalid', 'empty-response', 'unknown'].includes(message);
    }
    getAjaxBaseOrigins() {
        const candidates = (0, requestHandlerBaseOriginUtils_1.collectAjaxBaseOrigins)({
            preferredOrigin: this.preferredAjaxBaseOrigin,
            configuredBase: this.config.base || this.config.BASE_URL,
            antiBlockOrigins: this.loadAntiBlockBaseOrigins(),
            knownMirrorOrigins: requestHandlerTypes_1.KNOWN_AJAX_MIRROR_BASES
        });
        return this.rankAjaxBaseOrigins(candidates);
    }
    loadAntiBlockBaseOrigins() {
        return (0, requestHandlerBaseOriginUtils_1.loadAjaxAntiBlockBaseOrigins)();
    }
    normalizeBaseOrigin(value) {
        return (0, requestHandlerBaseOriginUtils_1.normalizeRequestHandlerBaseOrigin)(value);
    }
    shouldBypassFastHttpMagnet(mode) {
        if (mode === 'cloudflare-only') {
            return true;
        }
        if (!this.config.useCloudflareBypass) {
            return false;
        }
        return this.fastHttpMagnetCircuit.disabled;
    }
    createAjaxBaseOriginHealthState(origin) {
        return (0, requestHandlerBaseOriginUtils_1.createBaseOriginHealthState)(origin);
    }
    ensureAjaxBaseOriginHealthState(origin) {
        return (0, requestHandlerBaseOriginUtils_1.ensureBaseOriginHealthState)(this.ajaxBaseOriginHealth, origin, (value) => this.normalizeBaseOrigin(value));
    }
    rankAjaxBaseOrigins(origins) {
        return (0, requestHandlerBaseOriginUtils_1.rankBaseOrigins)({
            origins,
            healthMap: this.ajaxBaseOriginHealth,
            preferredOrigin: this.preferredAjaxBaseOrigin,
            normalizeOrigin: (value) => this.normalizeBaseOrigin(value),
            limit: this.baseOriginCandidateLimit,
            label: 'AJAX '
        });
    }
    getAjaxBaseOriginScore(origin, state, now = Date.now()) {
        return (0, requestHandlerBaseOriginUtils_1.getBaseOriginScore)(origin, state, this.preferredAjaxBaseOrigin, now);
    }
    recordAjaxBaseOriginSuccess(origin, latencyMs) {
        (0, requestHandlerBaseOriginUtils_1.recordBaseOriginSuccess)({
            healthMap: this.ajaxBaseOriginHealth,
            origin,
            latencyMs,
            normalizeOrigin: (value) => this.normalizeBaseOrigin(value)
        });
    }
    recordAjaxBaseOriginFailure(origin, reason) {
        (0, requestHandlerBaseOriginUtils_1.recordBaseOriginFailure)({
            healthMap: this.ajaxBaseOriginHealth,
            origin,
            reason,
            cooldownMs: this.ajaxBaseOriginCooldownMs,
            label: 'AJAX ',
            normalizeOrigin: (value) => this.normalizeBaseOrigin(value)
        });
    }
    getAjaxBaseOriginCooldownThreshold(reason) {
        return (0, requestHandlerBaseOriginUtils_1.getBaseOriginCooldownThreshold)(reason);
    }
    getPageBaseOrigins(url) {
        const candidates = (0, requestHandlerBaseOriginUtils_1.collectAjaxBaseOrigins)({
            preferredOrigin: this.preferredPageBaseOrigin,
            configuredBase: url,
            antiBlockOrigins: this.loadAntiBlockBaseOrigins(),
            knownMirrorOrigins: requestHandlerTypes_1.KNOWN_AJAX_MIRROR_BASES
        });
        return (0, requestHandlerBaseOriginUtils_1.rankBaseOrigins)({
            origins: candidates,
            healthMap: this.pageBaseOriginHealth,
            preferredOrigin: this.preferredPageBaseOrigin,
            normalizeOrigin: (value) => this.normalizeBaseOrigin(value),
            limit: this.baseOriginCandidateLimit,
            label: '\u9875\u9762\u8bf7\u6c42'
        });
    }
    buildPageFallbackUrls(url) {
        try {
            const parsed = new URL(url);
            const candidateOrigins = this.getPageBaseOrigins(url);
            const candidateUrls = candidateOrigins.map((origin) => `${origin}${parsed.pathname}${parsed.search}`);
            const primaryUrl = `${parsed.origin}${parsed.pathname}${parsed.search}`;
            if (!candidateUrls.includes(primaryUrl)) {
                candidateUrls.push(primaryUrl);
            }
            return Array.from(new Set(candidateUrls));
        }
        catch {
            return [url];
        }
    }
    recordPageBaseOriginSuccess(origin, latencyMs) {
        (0, requestHandlerBaseOriginUtils_1.recordBaseOriginSuccess)({
            healthMap: this.pageBaseOriginHealth,
            origin,
            latencyMs,
            normalizeOrigin: (value) => this.normalizeBaseOrigin(value)
        });
    }
    recordPageBaseOriginFailure(origin, reason) {
        (0, requestHandlerBaseOriginUtils_1.recordBaseOriginFailure)({
            healthMap: this.pageBaseOriginHealth,
            origin,
            reason,
            cooldownMs: this.ajaxBaseOriginCooldownMs,
            label: '\u9875\u9762\u8bf7\u6c42',
            normalizeOrigin: (value) => this.normalizeBaseOrigin(value)
        });
    }
    recordFastHttpMagnetResult(success, reason) {
        if (!this.config.useCloudflareBypass) {
            return;
        }
        this.fastHttpMagnetCircuit.attempts += 1;
        if (success) {
            this.fastHttpMagnetCircuit.successes += 1;
            this.fastHttpMagnetCircuit.consecutiveFailures = 0;
            return;
        }
        this.fastHttpMagnetCircuit.failures += 1;
        this.fastHttpMagnetCircuit.consecutiveFailures += 1;
        if (this.fastHttpMagnetCircuit.disabled) {
            return;
        }
        const { attempts, successes, consecutiveFailures } = this.fastHttpMagnetCircuit;
        const successRate = attempts > 0 ? successes / attempts : 0;
        const severeFailure = this.isSevereFastHttpMagnetFailure(reason);
        const shouldDisable = (severeFailure && consecutiveFailures >= 4) ||
            (attempts >= 6 && successes === 0) ||
            consecutiveFailures >= 6 ||
            (attempts >= 20 && successRate < 0.12);
        if (!shouldDisable) {
            return;
        }
        this.fastHttpMagnetCircuit.disabled = true;
        this.fastHttpMagnetCircuit.disabledAt = Date.now();
        this.fastHttpMagnetCircuit.disableReason = reason;
        logger_1.default.warn(`快速 HTTP 磁力通道命中率过低（成功 ${successes}/${attempts}，连续失败 ${consecutiveFailures} 次），本轮已自动切换为 Cloudflare 直连模式。最后失败原因：${reason}`);
    }
    isSevereFastHttpMagnetFailure(reason) {
        const normalized = String(reason || '').toLowerCase();
        return (normalized.includes('403') ||
            normalized.includes('forbidden') ||
            normalized.includes('bad request') ||
            normalized.includes('err_bad_request'));
    }
    shouldRouteMagnetTaskToRecoveryQueue() {
        return Boolean(this.config.useCloudflareBypass && this.fastHttpMagnetCircuit.disabled);
    }
    async fetchMagnetViaCloudflare(url, title, fetchStartTime) {
        logger_1.default.debug(`fetchMagnet: 启用 Cloudflare AJAX 兜底: ${title}`);
        try {
            const cfAjaxStart = Date.now();
            const cloudflareResponse = await this.executeAjaxWithCloudflare(url);
            const cfAjaxTime = Date.now() - cfAjaxStart;
            if (cloudflareResponse) {
                logger_1.default.info(`Cloudflare 绕过 AJAX 请求成功：${title}（耗时：${Math.round(cfAjaxTime / 1000)} 秒）`);
                return { statusCode: 200, body: cloudflareResponse };
            }
            logger_1.default.warn(`Cloudflare 绕过返回空响应：${title}`);
        }
        catch (cfError) {
            const cfFailedTime = Date.now() - fetchStartTime;
            logger_1.default.error(`Cloudflare 绕过 AJAX 请求失败：${title}（耗时：${Math.round(cfFailedTime / 1000)} 秒）`);
            logger_1.default.error(`错误类型: ${cfError instanceof Error ? cfError.constructor.name : '未知类型'}`);
            logger_1.default.error(`错误信息: ${cfError instanceof Error ? cfError.message : String(cfError)}`);
            logger_1.default.error(`错误堆栈: ${cfError instanceof Error ? cfError.stack : '无堆栈信息'}`);
        }
        return null;
    }
    async parseMagnetResult(title, responseBody, fetchStartTime) {
        const responseTime = Date.now() - fetchStartTime;
        logger_1.default.debug(`fetchMagnet: AJAX响应获取成功: ${title} (总耗时: ${Math.round(responseTime / 1000)}s)`);
        logger_1.default.debug(`fetchMagnet: AJAX响应内容长度: ${responseBody.length}`);
        logger_1.default.debug(`fetchMagnet: AJAX响应内容前500字符: ${responseBody.substring(0, 500)}`);
        logger_1.default.debug(`fetchMagnet: 开始解析磁力链接: ${title}`);
        const parseStartTime = Date.now();
        const magnetLinks = this.extractMagnetLinks(responseBody);
        const sizes = this.extractSizeTokens(responseBody);
        const parseTime = Date.now() - parseStartTime;
        logger_1.default.debug(`fetchMagnet: 解析完成: ${title} (耗时: ${Math.round(parseTime / 1000)}s)`);
        logger_1.default.debug(`fetchMagnet: 解析到 ${magnetLinks.length} 个磁力链接`);
        logger_1.default.debug(`fetchMagnet: 解析到 ${sizes.length} 个文件大小`);
        if (magnetLinks.length === 0 || sizes.length === 0) {
            const totalTime = Date.now() - fetchStartTime;
            logger_1.default.error(`fetchMagnet: 未找到磁力链接或文件大小: ${title} (总耗时: ${Math.round(totalTime / 1000)}s)`);
            logger_1.default.debug(`fetchMagnet: 响应内容片段: ${responseBody.substring(0, 500)}`);
            return null;
        }
        // 打印所有解析到的磁力链接
        magnetLinks.forEach((link, index) => {
            logger_1.default.debug(`fetchMagnet: 磁力链接 ${index + 1}: ${link.substring(0, 100)}...`);
        });
        logger_1.default.debug(`fetchMagnet: 开始计算最大文件大小: ${title}`);
        const calculateStartTime = Date.now();
        const parsedPairs = (0, requestHandlerMagnetUtils_1.buildParsedMagnetCandidates)(magnetLinks, sizes);
        const filteredPairs = this.applyMagnetExcludeFilter(title, parsedPairs);
        const contentValidatedPairs = await this.filterMagnetCandidatesByContent(title, filteredPairs);
        const calculateTime = Date.now() - calculateStartTime;
        const totalTime = Date.now() - fetchStartTime;
        logger_1.default.debug(`fetchMagnet: 磁力链接处理完成: ${title} (耗时: ${Math.round(calculateTime / 1000)}s)`);
        if (contentValidatedPairs.length === 0) {
            logger_1.default.warn(`fetchMagnet: 磁力候选已全部被过滤或内容校验排除: ${title}`);
            return null;
        }
        const result = (0, requestHandlerMagnetUtils_1.buildMagnetResult)(contentValidatedPairs, Boolean(this.config.allmag), (size) => this.formatFileSize(size), Number(this.config.supplementMagnetTopN) || 3);
        if (!result) {
            logger_1.default.error(`fetchMagnet: 未能确定有效磁力结果: ${title} (总耗时: ${Math.round(totalTime / 1000)}s)`);
            return null;
        }
        if (this.config.allmag) {
            logger_1.default.info(`fetchMagnet: 成功获取所有磁力链接: ${title} (共${contentValidatedPairs.length}个) (总耗时: ${Math.round(totalTime / 1000)}s)`);
            logger_1.default.debug(`fetchMagnet: 返回所有磁力链接，预览: ${result.magnet.substring(0, 200)}...`);
        }
        else {
            logger_1.default.info(`fetchMagnet: 成功获取磁力链接: ${title} (总耗时: ${Math.round(totalTime / 1000)}s)`);
            logger_1.default.info(`fetchMagnet: 返回最大磁力链接: ${result.magnet.substring(0, 100)}...`);
        }
        return result;
    }
    getMagnetExcludeKeywords() {
        return (0, requestHandlerMagnetUtils_1.getMagnetExcludeKeywords)(this.config.magnetExcludeKeywords);
    }
    getMagnetDisplayName(magnetLink) {
        return (0, requestHandlerMagnetUtils_1.getMagnetDisplayName)(magnetLink);
    }
    applyMagnetExcludeFilter(title, candidates) {
        return (0, requestHandlerMagnetUtils_1.applyMagnetExcludeFilter)(title, candidates, this.config.magnetExcludeKeywords);
    }
    async filterMagnetCandidatesByContent(title, candidates) {
        if (!Boolean(this.config.magnetContentValidation) || candidates.length === 0) {
            return candidates;
        }
        if (!this.isMagnetValidationEnabled()) {
            this.logMagnetValidationDisabledNotice();
            return candidates;
        }
        const result = await (0, magnetContentValidation_1.filterMagnetCandidatesByContentDetailed)({
            title,
            candidates,
            enabled: true,
            keepAll: Boolean(this.config.allmag),
            maxInspectCount: this.magnetValidationInspectLimit,
            maxValidationTimeMs: this.magnetValidationBudgetMs,
            abortSignal: this.magnetValidationAbortController.signal
        });
        this.recordMagnetValidationStats(title, result.stats);
        return result.candidates;
    }
    isMagnetValidationEnabled() {
        if (!this.magnetValidationRuntime.disabled) {
            return true;
        }
        if (this.magnetValidationRuntime.cooldownUntil > Date.now()) {
            return false;
        }
        this.resetMagnetValidationCooldown();
        logger_1.default.info('fetchMagnet: 磁力内容校验冷却结束，恢复广告过滤探测。');
        return true;
    }
    resetMagnetValidationCooldown() {
        this.magnetValidationRuntime.inspectedCandidates = 0;
        this.magnetValidationRuntime.unverifiedCandidates = 0;
        this.magnetValidationRuntime.timeoutCandidates = 0;
        this.magnetValidationRuntime.disabled = false;
        this.magnetValidationRuntime.disabledAt = null;
        this.magnetValidationRuntime.cooldownUntil = 0;
        this.magnetValidationRuntime.disableReason = '';
        this.magnetValidationRuntime.disableNoticeShown = false;
    }
    logMagnetValidationDisabledNotice() {
        if (this.magnetValidationRuntime.disableNoticeShown) {
            return;
        }
        this.magnetValidationRuntime.disableNoticeShown = true;
        const remainingMs = Math.max(0, this.magnetValidationRuntime.cooldownUntil - Date.now());
        const remainingSeconds = remainingMs > 0 ? Math.ceil(remainingMs / 1000) : 0;
        const cooldownText = remainingSeconds > 0 ? `，约 ${remainingSeconds} 秒后自动恢复` : '';
        logger_1.default.info(`fetchMagnet: 磁力内容校验当前处于冷却期，暂时直接保留快速候选${cooldownText}。原因：${this.magnetValidationRuntime.disableReason || '超时率过高'}`);
    }
    recordMagnetValidationStats(title, stats) {
        if (!stats.validationApplied || stats.inspectedCount <= 0) {
            return;
        }
        this.magnetValidationRuntime.inspectedCandidates += stats.inspectedCount;
        this.magnetValidationRuntime.unverifiedCandidates += stats.unverifiedCount;
        this.magnetValidationRuntime.timeoutCandidates += stats.timeoutCount;
        if (this.magnetValidationRuntime.disabled) {
            return;
        }
        const { inspectedCandidates, unverifiedCandidates, timeoutCandidates } = this.magnetValidationRuntime;
        const timeoutRatio = inspectedCandidates > 0 ? timeoutCandidates / inspectedCandidates : 0;
        const unverifiedRatio = inspectedCandidates > 0 ? unverifiedCandidates / inspectedCandidates : 0;
        const shouldDisable = inspectedCandidates >= 8 && (timeoutRatio >= 0.6 || unverifiedRatio >= 0.75);
        if (!shouldDisable) {
            return;
        }
        const disableReason = stats.validationSkippedReason === 'budget-exhausted'
            ? '单片校验预算耗尽'
            : `超时/未验证比例过高（超时 ${timeoutCandidates}/${inspectedCandidates}，未验证 ${unverifiedCandidates}/${inspectedCandidates}）`;
        this.magnetValidationRuntime.disabled = true;
        this.magnetValidationRuntime.disabledAt = Date.now();
        this.magnetValidationRuntime.cooldownUntil = Date.now() + this.magnetValidationCooldownMs;
        this.magnetValidationRuntime.disableReason = disableReason;
        this.magnetValidationRuntime.disableNoticeShown = false;
        logger_1.default.warn(`fetchMagnet: 磁力内容校验命中临时冷却，后续将先直通快速候选，${Math.ceil(this.magnetValidationCooldownMs / 1000)} 秒后自动恢复探测。当前影片：${title}；原因：${disableReason}`);
    }
    formatFileSize(sizeInMB) {
        return (0, requestHandlerMagnetUtils_1.formatFileSize)(sizeInMB);
    }
    normalizeAjaxImageParam(value) {
        return (0, requestHandlerMagnetUtils_1.normalizeAjaxImageParam)(value);
    }
    async requestPageWithMirrorFallback(url, options = {}, cookieOverride) {
        const candidateUrls = this.buildPageFallbackUrls(url);
        let lastResponse = null;
        for (let index = 0; index < candidateUrls.length; index += 1) {
            const candidateUrl = candidateUrls[index];
            const targetOrigin = this.normalizeBaseOrigin(candidateUrl);
            const requestStartedAt = Date.now();
            try {
                if (index > 0) {
                    logger_1.default.info(`页面请求：主域名异常，正在切换备用网址 ${targetOrigin}`);
                }
                const response = await this.requestPageViaHttp(candidateUrl, options, cookieOverride);
                lastResponse = response;
                if (this.isUsablePageResponse(response)) {
                    this.recordPageBaseOriginSuccess(targetOrigin, Date.now() - requestStartedAt);
                    if (targetOrigin && targetOrigin !== this.preferredPageBaseOrigin) {
                        this.preferredPageBaseOrigin = targetOrigin;
                        logger_1.default.info(`页面请求：已锁定可用页面域名：${targetOrigin}`);
                    }
                    if (targetOrigin && targetOrigin !== this.preferredAjaxBaseOrigin) {
                        this.preferredAjaxBaseOrigin = targetOrigin;
                    }
                    return response;
                }
                this.recordPageBaseOriginFailure(targetOrigin, this.describePageFallbackReason(response));
            }
            catch (error) {
                this.recordPageBaseOriginFailure(targetOrigin, error instanceof Error ? error.message : String(error));
                if (index === candidateUrls.length - 1) {
                    throw error;
                }
            }
        }
        return lastResponse;
    }
    async requestPageViaHttp(url, options = {}, cookieOverride) {
        let attempts = 0;
        while (attempts <= this.config.retryCount) {
            try {
                logger_1.default.debug(`开始请求页面: ${url} (尝试 ${attempts + 1}/${this.config.retryCount + 1})`);
                const headers = this.buildPageRequestHeaders(cookieOverride);
                logger_1.default.debug(`请求头信息: ${JSON.stringify({ ...headers, Cookie: headers.Cookie ? '[已设置]' : '[未设置]' })}`);
                const mergedOptions = {
                    ...this.requestConfig,
                    ...options,
                    url,
                    headers
                };
                const controller = this.createAbortController();
                try {
                    const requestStartTime = Date.now();
                    const response = await axios_1.default.get(mergedOptions.url, {
                        timeout: mergedOptions.timeout,
                        headers: mergedOptions.headers,
                        signal: controller.signal
                    });
                    this.recordProxySuccess(Date.now() - requestStartTime);
                    return {
                        statusCode: response.status,
                        body: typeof response.data === 'string' ? response.data : JSON.stringify(response.data)
                    };
                }
                finally {
                    this.releaseAbortController(controller);
                }
            }
            catch (error) {
                if (this.isAbortError(error)) {
                    throw new Error('Request cancelled');
                }
                const err = error;
                await this.recordProxyFailure(err, '页面请求');
                logger_1.default.error(`请求页面 ${url} 失败: ${err.message}`);
                if (err.response) {
                    logger_1.default.error(`响应状态码: ${err.response.status}`);
                    if (err.response.data) {
                        const responseData = err.response.data;
                        if (typeof responseData === 'string' && responseData.length < 1000) {
                            logger_1.default.error(`响应内容 (前500字符): ${responseData.substring(0, 500)}`);
                        }
                    }
                }
                if (axios_retry_1.default.isNetworkOrIdempotentRequestError(err) ||
                    (err.response?.status && [500, 429, 403, 503].includes(err.response.status))) {
                    const retryDelay = (this.config.retryDelay || this.retryDelay) * Math.pow(2, attempts);
                    await new Promise((resolve) => setTimeout(resolve, retryDelay));
                    attempts += 1;
                    continue;
                }
                throw err;
            }
        }
        return null;
    }
    buildPageRequestHeaders(cookieOverride) {
        return (0, requestHandlerPageUtils_1.buildPageRequestHeaders)({
            requestHeaders: this.requestConfig.headers,
            configCookie: this.config.headers?.Cookie,
            cookieOverride,
            cloudflareCookies: this.cloudflareCookies,
            defaultCookieHeader: this.defaultCookieHeader
        });
    }
    getManualCookieHeader() {
        return (0, requestHandlerPageUtils_1.getManualCookieHeader)(this.config.headers?.Cookie, this.defaultCookieHeader);
    }
    isUsablePageResponse(response) {
        return (0, requestHandlerPageUtils_1.isUsablePageResponse)(response);
    }
    isUsablePageBody(body) {
        return (0, requestHandlerPageUtils_1.isUsablePageBody)(body);
    }
    isAgeVerificationResponse(body) {
        return (0, requestHandlerPageUtils_1.isAgeVerificationResponse)(body);
    }
    isCloudflareChallengeResponse(statusCode, body) {
        return (0, requestHandlerPageUtils_1.isCloudflareChallengeResponse)(statusCode, body);
    }
    describePageFallbackReason(response) {
        return (0, requestHandlerPageUtils_1.describePageFallbackReason)(response);
    }
    isRecoverableBypassError(error) {
        return (0, requestHandlerPageUtils_1.isRecoverableBypassError)(error);
    }
    hasUsableMagnetPayload(body) {
        return this.extractMagnetLinks(body).length > 0 && this.extractSizeTokens(body).length > 0;
    }
    extractMagnetLinks(responseBody) {
        return (0, requestHandlerMagnetUtils_1.extractMagnetLinks)(responseBody);
    }
    extractSizeTokens(responseBody) {
        return (0, requestHandlerMagnetUtils_1.extractSizeTokens)(responseBody);
    }
    async downloadImage(url, filename, referer, outputDirOverride) {
        const dirPath = typeof outputDirOverride === 'string' && outputDirOverride.trim()
            ? outputDirOverride.trim()
            : this.config.output; // 获取输出目录路径
        // 防止路径遍历攻击
        const sanitizedFilename = path_1.default.basename(filename);
        const originalFilePath = path_1.default.join(dirPath, sanitizedFilename);
        const ext = path_1.default.extname(sanitizedFilename); // 获取文件扩展名
        const baseFilename = path_1.default.basename(sanitizedFilename, ext); // 获取不带扩展名的文件名
        // 检查并创建目录 (如果之前没有添加的话)
        if (!fs_1.default.existsSync(dirPath)) {
            try {
                await fs_1.default.promises.mkdir(dirPath, { recursive: true });
            }
            catch (mkdirError) {
                errorHandler_1.ErrorHandler.handleFileError(mkdirError, `创建输出目录: ${dirPath}`);
                throw mkdirError; // 如果目录创建失败，后续操作也无法进行
            }
        }
        // 尝试保存文件
        try {
            if (fs_1.default.existsSync(originalFilePath)) {
                // console.log(`图片 ${filename} 已存在，跳过下载。`);
                return false;
            }
            // 准备请求头
            let headers = { ...this.requestConfig.headers };
            // 设置Referer头，优先使用传入的referer参数，否则从图片URL自动生成
            let refererUrl;
            if (referer) {
                refererUrl = referer;
                logger_1.default.debug(`downloadImage: 使用传入的Referer: ${refererUrl}`);
            }
            else {
                const imageUrl = new URL(url);
                refererUrl = `${imageUrl.protocol}//${imageUrl.hostname}/`;
                logger_1.default.debug(`downloadImage: 从图片URL自动生成Referer: ${refererUrl}`);
            }
            headers.referer = refererUrl;
            // 如果启用 Cloudflare 绕过，确保使用有效的 cookies
            if (this.config.useCloudflareBypass) {
                if (!this.cloudflareCookies) {
                    logger_1.default.debug(`downloadImage: 获取 Cloudflare Cookies 用于图片下载`);
                    await this.getCloudflareCookies();
                }
                if (this.cloudflareCookies) {
                    headers.Cookie = this.cloudflareCookies;
                    logger_1.default.debug(`downloadImage: 使用 Cloudflare Cookies 下载图片: ${filename}, Referer: ${refererUrl}`);
                }
            }
            // 创建 HTTPS Agent 用于图片下载（支持 SSL 配置）
            const httpsAgent = this.createHttpsAgent();
            const controller = this.createAbortController();
            try {
                const response = await axios_1.default.get(url, {
                    responseType: 'arraybuffer',
                    timeout: this.requestConfig.timeout,
                    headers,
                    httpsAgent,
                    signal: controller.signal
                });
                fs_1.default.writeFileSync(originalFilePath, Buffer.from(response.data, 'binary'));
                return true;
            }
            finally {
                this.releaseAbortController(controller);
            }
        }
        catch (err) {
            if (this.isAbortError(err)) {
                return false;
            }
            const error = err; // 明确类型为文件系统错误
            // 检查是否是文件路径过长或非法字符导致的错误
            if (error.code === 'ENOENT' || error.code === 'ENAMETOOLONG') {
                logger_1.default.warn(`保存图片失败，文件名可能过长或包含非法字符，尝试简化文件名: ${filename}`);
                // 简化文件名，例如保留部分原文件名和哈希值
                const simplifiedFilename = `${baseFilename.substring(0, 50)}_..._${baseFilename.substring(baseFilename.length - 10)}${ext}`.replace(/[^a-zA-Z0-9_\-. ]/g, '_'); // 简单替换非法字符
                const simplifiedFilePath = path_1.default.join(dirPath, simplifiedFilename);
                try {
                    if (fs_1.default.existsSync(simplifiedFilePath)) {
                        logger_1.default.info(`简化后的图片 ${simplifiedFilename} 已存在，跳过下载。`);
                        return false;
                    }
                    // 准备请求头（简化文件名情况下）
                    let simplifiedHeaders = { ...this.requestConfig.headers };
                    // 设置Referer头，优先使用传入的referer参数，否则从图片URL自动生成
                    let simplifiedRefererUrl;
                    if (referer) {
                        simplifiedRefererUrl = referer;
                        logger_1.default.debug(`downloadImage (简化): 使用传入的Referer: ${simplifiedRefererUrl}`);
                    }
                    else {
                        const imageUrl = new URL(url);
                        simplifiedRefererUrl = `${imageUrl.protocol}//${imageUrl.hostname}/`;
                        logger_1.default.debug(`downloadImage (简化): 从图片URL自动生成Referer: ${simplifiedRefererUrl}`);
                    }
                    simplifiedHeaders.referer = simplifiedRefererUrl;
                    // 如果启用 Cloudflare 绕过，确保使用有效的 cookies
                    if (this.config.useCloudflareBypass) {
                        if (!this.cloudflareCookies) {
                            logger_1.default.debug(`downloadImage (简化): 获取 Cloudflare Cookies 用于图片下载`);
                            await this.getCloudflareCookies();
                        }
                        if (this.cloudflareCookies) {
                            simplifiedHeaders.Cookie = this.cloudflareCookies;
                            logger_1.default.debug(`downloadImage (简化): 使用 Cloudflare Cookies 下载图片: ${simplifiedFilename}, Referer: ${simplifiedRefererUrl}`);
                        }
                    }
                    // 创建 HTTPS Agent 用于简化文件名图片下载
                    const httpsAgent = this.createHttpsAgent();
                    const controller = this.createAbortController();
                    try {
                        const response = await axios_1.default.get(url, {
                            responseType: 'arraybuffer',
                            timeout: this.requestConfig.timeout,
                            headers: simplifiedHeaders,
                            httpsAgent,
                            signal: controller.signal
                        });
                        await fs_1.default.promises.writeFile(simplifiedFilePath, Buffer.from(response.data, 'binary'));
                        logger_1.default.info(`图片已使用简化文件名保存: ${simplifiedFilename}`);
                        return true;
                    }
                    finally {
                        this.releaseAbortController(controller);
                    }
                }
                catch (simplifyErr) {
                    const simplifyError = simplifyErr; // Simplification attempt error
                    // 记录简化文件名尝试的详细错误信息
                    logger_1.default.error(`使用简化文件名下载图片失败: ${simplifiedFilename}`);
                    logger_1.default.error(`错误类型: ${simplifyError.constructor.name}`);
                    logger_1.default.error(`错误信息: ${simplifyError.message}`);
                    // 如果是AxiosError，记录更详细的响应信息
                    if (simplifyError.name === 'AxiosError') {
                        const axiosError = simplifyError;
                        logger_1.default.error(`响应状态码: ${axiosError.response?.status || 'N/A'}`);
                        if (axiosError.response) {
                            logger_1.default.error(`完整响应数据: ${JSON.stringify(axiosError.response.data, null, 2)}`);
                            logger_1.default.error(`响应头: ${JSON.stringify(axiosError.response.headers || {}, null, 2)}`);
                        }
                        if (axiosError.config) {
                            logger_1.default.debug(`请求方法: ${axiosError.config.method || 'N/A'}`);
                            logger_1.default.debug(`请求头: ${JSON.stringify(axiosError.config.headers || {}, null, 2)}`);
                        }
                    }
                    logger_1.default.error(`错误堆栈: ${simplifyError.stack}`);
                    errorHandler_1.ErrorHandler.handleFileError(simplifyError, `使用简化文件名保存图片: ${simplifiedFilename}`);
                    throw simplifyError; // 简化文件名后仍然失败，抛出错误
                }
            }
            else {
                // 其他类型的错误，直接抛出
                errorHandler_1.ErrorHandler.handleFileError(error, `保存图片: ${filename}`);
                throw error;
            }
        }
    }
    /**
       * 初始化Cloudflare绕过器
       */
    async initCloudflareBypass() {
        try {
            logger_1.default.info('正在初始化Cloudflare绕过器...');
            // 不再创建新的CloudflareBypass实例，而是使用共享池
            this.cloudflareBypass = new cloudflareBypass_1.default({
                headless: true,
                timeout: this.requestConfig.timeout,
                proxy: this.requestConfig.proxy,
                puppeteerPool: this.puppeteerPool // 传入共享池
            });
            await this.cloudflareBypass.init();
      logger_1.default.info('Cloudflare绕过器初始化成功');
      // 设置年龄认证相关Cookie
      // 共享池模式下 init 时还没有 page，Cookie 会在 bypassCloudflare 获取到页面后设置
      if (this.puppeteerPool) {
        logger_1.default.info('使用共享 Puppeteer 池，年龄认证 Cookie 将在获取页面后延迟设置');
      } else {
        logger_1.default.info('正在设置年龄认证相关Cookie...');
        await this.cloudflareBypass.setAgeVerificationCookies();
        logger_1.default.info('年龄认证Cookie设置完成');
      }
        }
        catch (error) {
            logger_1.default.error('Cloudflare绕过器初始化失败:', error);
            this.cloudflareBypass = null;
            throw error;
        }
    }
    async prewarmCloudflareSession() {
        if (!this.config.useCloudflareBypass || !this.demoProfile.enableCloudflarePrewarm) {
            return;
        }
        if (this.cloudflarePrewarmPromise) {
            return this.cloudflarePrewarmPromise;
        }
        this.cloudflarePrewarmPromise = (async () => {
            logger_1.default.info('RequestHandler: 开始预热 Cloudflare 快速通道...');
            const cookies = await this.getCloudflareCookiesWithLane();
            if (cookies) {
                logger_1.default.info(`RequestHandler: Cloudflare 会话预热成功，Cookie 数量 ${cookies.split(';').length}`);
            }
            else {
                logger_1.default.warn('RequestHandler: Cloudflare 会话预热未拿到可复用 Cookie');
            }
            if (this.demoProfile.enableCloudflareWorker) {
                try {
                    const workerSlots = this.getCloudflareAjaxWorkerClients();
                    const workerResults = await Promise.allSettled(workerSlots.map((slot) => slot.client.prewarm()));
                    const successCount = workerResults.filter((result) => result.status === 'fulfilled').length;
                    const firstCookies = workerResults.find((result) => result.status === 'fulfilled')?.value;
                    if (successCount === 0) {
                        const firstError = workerResults.find((result) => result.status === 'rejected')?.reason;
                        throw firstError instanceof Error ? firstError : new Error('Cloudflare Worker 预热全部失败');
                    }
                    if (firstCookies) {
                        logger_1.default.info(`RequestHandler: Cloudflare Worker 预热成功，Cookie 数量 ${firstCookies.split(';').length}`);
                    }
                    if (successCount > 1) {
                        logger_1.default.info(`RequestHandler: Cloudflare Worker 池已预热 ${successCount} 个并发槽位`);
                    }
                }
                catch (error) {
                    this.disableCloudflareWorker(error);
                    logger_1.default.warn(`RequestHandler: Cloudflare Worker 预热失败，将自动回退进程内通道：${error instanceof Error ? error.message : String(error)}`);
                }
            }
        })().finally(() => {
            this.cloudflarePrewarmPromise = null;
        });
        return this.cloudflarePrewarmPromise;
    }
    /**
     * 获取 Cloudflare Cookies
     */
    async getCloudflareCookies() {
        if (!this.config.useCloudflareBypass) {
            logger_1.default.debug('Cloudflare 绕过未启用，跳过获取会话 Cookie');
            return null;
        }
        const currentTime = Date.now();
        if (this.cloudflareCookies && (currentTime - this.lastCookieRefresh) < this.cookieRefreshInterval) {
            logger_1.default.debug(`使用缓存的 Cloudflare Cookies（剩余有效时间：${Math.floor((this.cookieRefreshInterval - (currentTime - this.lastCookieRefresh)) / 1000 / 60)} 分钟）`);
            return this.cloudflareCookies;
        }
        if (this.isCloudflareRecoveryCoolingDown('Cloudflare 会话恢复')) {
            return null;
        }
        const baseUrl = this.config.base || this.config.BASE_URL;
        const maxAttempts = 3;
        let lastFailureMessage = '';
        for (let attempt = 1; attempt <= maxAttempts; attempt += 1) {
            const attemptStartTime = Date.now();
            if (!this.cloudflareBypass) {
                logger_1.default.debug('Cloudflare 绕过器未初始化，准备创建新会话...');
                await this.initCloudflareBypass();
            }
            if (!this.cloudflareBypass) {
                logger_1.default.warn('Cloudflare 绕过器初始化失败，无法获取会话 Cookie');
                return null;
            }
            try {
                logger_1.default.info(attempt === 1
                    ? '正在通过 Cloudflare 绕过获取会话 Cookie...'
                    : `正在重新获取 Cloudflare 会话 Cookie（第 ${attempt}/${maxAttempts} 次）...`);
                logger_1.default.debug(`目标URL: ${baseUrl}`);
                await this.cloudflareBypass.bypassCloudflare(baseUrl);
                logger_1.default.debug('正在从页面提取 Cloudflare Cookies...');
                const cookies = await this.cloudflareBypass.getCookies();
                if (!cookies || cookies.trim().length === 0) {
                    logger_1.default.warn(`第 ${attempt} 次未获取到有效的 Cloudflare Cookies`);
                }
                else if (!this.isValidCookieString(cookies)) {
                    logger_1.default.warn(`第 ${attempt} 次获取到的 Cloudflare Cookies 校验失败`);
                }
                else {
                    this.cloudflareCookies = cookies;
                    this.lastCookieRefresh = Date.now();
                    this.resetCloudflareRecoveryRuntime(true);
                    this.recordProxySuccess(Date.now() - attemptStartTime);
                    logger_1.default.info(`Cloudflare 会话恢复成功，共获取 ${cookies.split(';').length} 个 Cookie，可复用 ${this.cookieRefreshInterval / 1000 / 60} 分钟`);
                    logger_1.default.debug(`获取到的 Cookies: ${cookies}`);
                    return cookies;
                }
            }
            catch (error) {
                const message = error instanceof Error ? error.message : String(error);
                lastFailureMessage = message;
                await this.recordProxyFailure(error, 'Cloudflare 会话恢复');
                logger_1.default.warn(`获取 Cloudflare 会话失败（第 ${attempt}/${maxAttempts} 次）：${message}`);
                logger_1.default.debug(`Cloudflare 会话恢复错误堆栈: ${error instanceof Error ? error.stack : '无堆栈信息'}`);
                if (!this.isRecoverableAjaxError(error) && !message.toLowerCase().includes('cloudflare')) {
                    return null;
                }
            }
            if (attempt < maxAttempts) {
                await this.resetCloudflareBypass(`Cloudflare 会话恢复失败，第 ${attempt} 次准备重建绕过器`);
                const recoveryDelay = this.getCloudflareRecoveryDelayMs(attempt);
                logger_1.default.info(`等待 ${Math.ceil(recoveryDelay / 1000)} 秒后继续重试 Cloudflare 会话恢复。`);
                await new Promise((resolve) => setTimeout(resolve, recoveryDelay));
            }
        }
        this.markCloudflareRecoveryFailure(lastFailureMessage || 'Cloudflare 会话恢复多次失败');
        if (this.isCloudflareRecoveryCoolingDown('Cloudflare 会话恢复')) {
            logger_1.default.error(`Cloudflare 会话恢复多次失败，已进入冷却期，后续请求将优先回退到普通请求链路。最近错误：${this.cloudflareRecoveryRuntime.lastError || '未知错误'}`);
        }
        else {
            logger_1.default.error('Cloudflare 会话恢复多次失败，后续请求将尝试回退到普通请求链路。');
        }
        return null;
    }
    async getCloudflareCookiesWithLane() {
        if (!this.demoProfile.enableDedicatedCloudflareLane) {
            return this.getCloudflareCookies();
        }
        if (this.cloudflareCookieRefreshPromise) {
            return this.cloudflareCookieRefreshPromise;
        }
        this.cloudflareCookieRefreshPromise = this.getCloudflareCookies().finally(() => {
            this.cloudflareCookieRefreshPromise = null;
        });
        return this.cloudflareCookieRefreshPromise;
    }
    /**
     * 使用 Cloudflare 绕过器执行 AJAX 请求
     */
    async executeAjaxWithCloudflare(url) {
        const runWithSelectedChannel = async () => {
            if (this.demoProfile.enableCloudflareWorker && !this.cloudflareWorkerDisabled) {
                try {
                    return await this.executeAjaxWithCloudflareUsingWorker(url);
                }
                catch (error) {
                    this.disableCloudflareWorker(error);
                    logger_1.default.warn(`Cloudflare Worker 通道失败，将回退到进程内通道：${error instanceof Error ? error.message : String(error)}`);
                }
            }
            return this.executeAjaxWithCloudflareInProcess(url);
        };
        if (this.demoProfile.enableDedicatedCloudflareLane) {
            return this.runWithCloudflareAjaxSlot(runWithSelectedChannel);
        }
        return runWithSelectedChannel();
    }
    async executeAjaxWithCloudflareUsingWorker(url) {
        const slot = this.acquireCloudflareAjaxWorkerSlot();
        const startedAt = Date.now();
        try {
            const result = await slot.client.executeAjax(url);
            if (result) {
                this.recordProxySuccess(Date.now() - startedAt);
            }
            return result;
        }
        catch (error) {
            await this.recordProxyFailure(error, 'Cloudflare Worker AJAX');
            throw error;
        }
        finally {
            slot.inFlight = Math.max(0, slot.inFlight - 1);
        }
    }
    async executeAjaxWithCloudflareInProcess(url) {
        if (this.isCloudflareRecoveryCoolingDown('Cloudflare AJAX 恢复')) {
            logger_1.default.warn('Cloudflare AJAX 恢复当前处于冷却期，本次直接回退到普通 AJAX 链路。');
            return this.fallbackToRegularAjax(url);
        }
        const maxAttempts = 3;
        let lastFailureMessage = '';
        for (let attempt = 1; attempt <= maxAttempts; attempt += 1) {
            const cfStartTime = Date.now();
            if (!this.cloudflareBypass) {
                await this.initCloudflareBypass();
            }
            if (!this.cloudflareBypass) {
                logger_1.default.warn('Cloudflare 绕过器未初始化，无法执行 AJAX 请求');
                break;
            }
            try {
                logger_1.default.info(`正在通过 Cloudflare 会话执行 AJAX 请求（第 ${attempt}/${maxAttempts} 次）...`);
                logger_1.default.debug(`Cloudflare AJAX 请求 URL: ${url}`);
                const executeStart = Date.now();
                const result = await this.cloudflareBypass.executeAjax(url);
                const executeTime = Date.now() - executeStart;
                const totalTime = Date.now() - cfStartTime;
                if (result) {
                    this.resetCloudflareRecoveryRuntime(true);
                    this.recordProxySuccess(totalTime);
                    logger_1.default.info(`Cloudflare AJAX 请求成功（耗时：${Math.round(totalTime / 1000)} 秒）`);
                    logger_1.default.debug(`Cloudflare AJAX 响应长度: ${result.length}`);
                    logger_1.default.debug(`Cloudflare AJAX 执行耗时: ${Math.round(executeTime / 1000)} 秒，总耗时: ${Math.round(totalTime / 1000)} 秒`);
                    return result;
                }
                logger_1.default.warn(`第 ${attempt} 次 Cloudflare AJAX 返回空响应`);
            }
            catch (error) {
                const totalTime = Date.now() - cfStartTime;
                lastFailureMessage = error instanceof Error ? error.message : String(error);
                await this.recordProxyFailure(error, 'Cloudflare AJAX');
                logger_1.default.warn(`Cloudflare AJAX 请求失败（第 ${attempt}/${maxAttempts} 次，耗时：${Math.round(totalTime / 1000)} 秒）：${error instanceof Error ? error.message : String(error)}`);
                logger_1.default.debug(`Cloudflare AJAX 错误类型: ${error instanceof Error ? error.constructor.name : 'Unknown'}`);
                logger_1.default.debug(`Cloudflare AJAX 错误堆栈: ${error instanceof Error ? error.stack : '无堆栈信息'}`);
                if (!this.isRecoverableAjaxError(error) || attempt >= maxAttempts) {
                    break;
                }
            }
            await this.resetCloudflareBypass(`Cloudflare AJAX 第 ${attempt} 次失败，准备重建绕过器`);
            if (attempt < maxAttempts) {
                const recoveryDelay = this.getCloudflareRecoveryDelayMs(attempt);
                logger_1.default.info(`等待 ${Math.ceil(recoveryDelay / 1000)} 秒后继续重试 Cloudflare AJAX。`);
                await new Promise((resolve) => setTimeout(resolve, recoveryDelay));
            }
        }
        this.markCloudflareRecoveryFailure(lastFailureMessage || 'Cloudflare AJAX 多次失败');
        if (this.isCloudflareRecoveryCoolingDown('Cloudflare AJAX 恢复')) {
            logger_1.default.warn('Cloudflare AJAX 已进入冷却期，将直接使用普通 AJAX 回退链路。');
        }
        return this.fallbackToRegularAjax(url);
    }
    async fallbackToRegularAjax(url) {
        if (this.demoProfile.enableFastAjaxFallback) {
            logger_1.default.warn('Cloudflare AJAX 多次失败，快速方案不再回退到普通 AJAX 重试。');
            return null;
        }
        try {
            logger_1.default.warn('Cloudflare AJAX 多次失败，已回退到普通 AJAX 请求。');
            const fallbackResponse = await this.getXMLHttpRequest(url, { skipCloudflareCookies: true });
            return fallbackResponse?.body || null;
        }
        catch (fallbackError) {
            logger_1.default.error(`普通 AJAX 回退请求也失败：${fallbackError instanceof Error ? fallbackError.message : String(fallbackError)}`);
            return null;
        }
    }
    getCloudflareRecoveryDelayMs(attempt) {
        const baseDelay = Math.max(this.retryDelay, 3000);
        return Math.min(15000, baseDelay + attempt * 2500);
    }
    resetCloudflareRecoveryRuntime(clearCooldown = false) {
        this.cloudflareRecoveryRuntime.consecutiveFailures = 0;
        this.cloudflareRecoveryRuntime.lastError = '';
        this.cloudflareRecoveryRuntime.noticeShown = false;
        if (clearCooldown) {
            this.cloudflareRecoveryRuntime.cooldownUntil = 0;
        }
    }
    markCloudflareRecoveryFailure(message) {
        this.cloudflareRecoveryRuntime.consecutiveFailures += 1;
        this.cloudflareRecoveryRuntime.lastError = message;
        this.cloudflareRecoveryRuntime.noticeShown = false;
        if (this.cloudflareRecoveryRuntime.consecutiveFailures < this.cloudflareRecoveryFailureThreshold) {
            return;
        }
        this.cloudflareRecoveryRuntime.cooldownUntil = Date.now() + this.cloudflareRecoveryCooldownMs;
        logger_1.default.warn(`Cloudflare 恢复链路连续失败 ${this.cloudflareRecoveryRuntime.consecutiveFailures} 次，已进入 ${Math.ceil(this.cloudflareRecoveryCooldownMs / 1000)} 秒冷却期。原因：${message}`);
    }
    isCloudflareRecoveryCoolingDown(contextLabel) {
        const remainingMs = this.cloudflareRecoveryRuntime.cooldownUntil - Date.now();
        if (remainingMs <= 0) {
            if (this.cloudflareRecoveryRuntime.cooldownUntil > 0) {
                this.cloudflareRecoveryRuntime.cooldownUntil = 0;
                this.cloudflareRecoveryRuntime.noticeShown = false;
            }
            return false;
        }
        if (!this.cloudflareRecoveryRuntime.noticeShown) {
            logger_1.default.info(`${contextLabel} 当前处于冷却期，剩余 ${Math.ceil(remainingMs / 1000)} 秒，暂时跳过 Cloudflare 恢复。`);
            this.cloudflareRecoveryRuntime.noticeShown = true;
        }
        return true;
    }
    isRecoverableAjaxError(error) {
        return (0, requestHandlerPageUtils_1.isRecoverableAjaxError)(error);
    }
    isFastFallbackAjaxError(error) {
        return (0, requestHandlerPageUtils_1.isFastFallbackAjaxError)(error);
    }
    async resetCloudflareBypass(reason) {
        logger_1.default.warn(`准备重置 Cloudflare 绕过器：${reason}`);
        if (this.cloudflareBypass) {
            try {
                await this.cloudflareBypass.close();
            }
            catch (error) {
                logger_1.default.warn(`关闭 Cloudflare 绕过器时出错：${error instanceof Error ? error.message : String(error)}`);
            }
        }
        this.cloudflareBypass = null;
        this.cloudflareCookies = null;
        this.lastCookieRefresh = 0;
    }
    getCloudflareAjaxConcurrencyLimit() {
        return (0, requestHandlerCloudflareUtils_1.getCloudflareAjaxConcurrencyLimit)({
            useCloudflareBypass: Boolean(this.config.useCloudflareBypass),
            parallel: Number(this.config.parallel) || 1
        });
    }
    async runWithCloudflareAjaxSlot(task) {
        const limit = this.getCloudflareAjaxConcurrencyLimit();
        if (limit <= 1) {
            this.ensureRequestAvailable();
            return task();
        }
        await this.acquireCloudflareAjaxSlot(limit);
        try {
            this.ensureRequestAvailable();
            return await task();
        }
        finally {
            this.releaseCloudflareAjaxSlot();
        }
    }
    async acquireCloudflareAjaxSlot(limit) {
        await (0, requestHandlerCloudflareUtils_1.acquireCloudflareAjaxSlot)(this.cloudflareAjaxGateState, limit);
    }
    releaseCloudflareAjaxSlot() {
        (0, requestHandlerCloudflareUtils_1.releaseCloudflareAjaxSlot)(this.cloudflareAjaxGateState);
    }
    getCloudflareAjaxWorkerClients() {
        const targetSize = this.getCloudflareAjaxConcurrencyLimit();
        return (0, requestHandlerCloudflareUtils_1.ensureCloudflareAjaxWorkerClients)({
            slots: this.cloudflareAjaxWorkerClients,
            targetSize,
            createSlot: (slotId) => ({
                client: new cloudflareAjaxWorkerClient_1.default(this.config),
                inFlight: 0,
                slotId
            })
        });
    }
    acquireCloudflareAjaxWorkerSlot() {
        return (0, requestHandlerCloudflareUtils_1.acquireLeastBusyCloudflareWorkerSlot)(this.getCloudflareAjaxWorkerClients());
    }
    disableCloudflareWorker(error) {
        if (this.cloudflareWorkerDisabled) {
            return;
        }
        this.cloudflareWorkerDisabled = true;
        this.cloudflareWorkerDisableReason = error instanceof Error ? error.message : String(error);
        void this.closeCloudflareWorkerClients().catch(() => undefined);
    }
    isValidCookieValue(value) {
        return (0, requestHandlerPageUtils_1.isValidCookieValue)(value);
    }
    setCookieHeader(headers, cookieString) {
        const applied = (0, requestHandlerPageUtils_1.setCookieHeader)(headers, cookieString);
        if (!applied) {
            logger_1.default.warn('Cookie 字符串无效，已跳过写入请求头。');
            return false;
        }
        logger_1.default.debug('Cookie 已安全写入请求头。');
        return true;
    }
    createProxyAgent(proxyUrl) {
        return this.proxyManager.createProxyAgent(proxyUrl);
    }
    isValidCookieString(cookieString) {
        return (0, requestHandlerPageUtils_1.isValidCookieString)(cookieString);
    }
    createHttpsAgent() {
        const strictSSL = this.config.strictSSL !== false;
        const proxyOptions = {};
        if (this.config.proxy) {
            const normalizedProxy = this.normalizeProxyValue(this.config.proxy);
            if (!normalizedProxy) {
                this.config.proxy = undefined;
                this.requestConfig.proxy = undefined;
            }
            else {
                try {
                    const proxyUrl = new URL(normalizedProxy);
                    this.config.proxy = normalizedProxy;
                    this.requestConfig.proxy = normalizedProxy;
                    proxyOptions.proxy = {
                        host: proxyUrl.hostname,
                        port: parseInt(proxyUrl.port, 10) || (proxyUrl.protocol === 'https:' ? 443 : 80)
                    };
                    if (proxyUrl.username && proxyUrl.password) {
                        proxyOptions.proxy.headers = {
                            'Proxy-Authorization': 'Basic ' + Buffer.from(proxyUrl.username + ':' + proxyUrl.password).toString('base64')
                        };
                    }
                }
                catch (error) {
                    this.warnProxyAgentFailureOnce(normalizedProxy, error instanceof Error ? error.message : String(error));
                }
            }
        }
        return new https_1.default.Agent({
            rejectUnauthorized: strictSSL,
            ...proxyOptions
        });
    }
    async closeCloudflareWorkerClients() {
        await (0, requestHandlerLifecycleUtils_1.closeCloudflareWorkerSlots)(this.cloudflareAjaxWorkerClients);
    }
    async close() {
        this.isClosed = true;
        this.magnetValidationAbortController.abort();
        (0, requestHandlerLifecycleUtils_1.abortTrackedControllers)(this.activeAbortControllers);
        (0, requestHandlerCloudflareUtils_1.drainCloudflareAjaxWaiters)(this.cloudflareAjaxGateState);
        if (this.cloudflareBypass) {
            await this.cloudflareBypass.close();
            this.cloudflareBypass = null;
        }
        await this.closeCloudflareWorkerClients();
    }
    createAbortController() {
        return (0, requestHandlerLifecycleUtils_1.createTrackedAbortController)(this.activeAbortControllers);
    }
    releaseAbortController(controller) {
        (0, requestHandlerLifecycleUtils_1.releaseTrackedAbortController)(this.activeAbortControllers, controller);
    }
    isAbortError(error) {
        return (0, requestHandlerLifecycleUtils_1.isAbortLikeError)(error);
    }
    ensureRequestAvailable() {
        if (this.isClosed) {
            throw new Error('Request cancelled');
        }
    }
}
exports.default = RequestHandler;
export { RequestHandler as default };
//# sourceMappingURL=requestHandler.js.map
