// Package messages centralizes user-facing message constants shared by the
// current Go desktop modules.
//
// Ownership summary:
// 1) keep operator-facing wording stable across crawl, organizer, and
//    subscription flows
// 2) reduce scattered hard-coded UI/status text in services
// 3) provide one review point when wording or terminology needs to change
//
// File map for maintainers:
// 1) shared common wording constants
// 2) crawl lifecycle/status/fallback messages
// 3) organizer/subscription/operator-facing phrase catalog
package messages

// ---- 通用 ----
const (
	Unavailable            = "不可用"
	OK                     = "正常"
	Info                   = "信息"
	Warning                = "警告"
	Error                  = "错误"
	Unknown                = "未知"
	NotConfigured          = "未配置"
	Disabled               = "未启用"
)

// ---- 抓取任务 ----
const (
	CrawlTaskStarted                = "抓取任务已启动"
	CrawlTaskStopped                = "抓取任务已停止"
	CrawlTaskAlreadyStopped         = "抓取任务已处于停止状态"
	CrawlTaskRestartQueued          = "已登记重启请求，当前任务完成后将自动重新开始抓取"
	CrawlTaskRunning                = "当前已有抓取任务在运行，请先停止后再启动"
	CrawlTaskStartError             = "启动抓取任务失败"
	CrawlTaskStopTimeout            = "等待抓取任务停止超时"
	CrawlTaskCompleted              = "抓取任务已完成"
	CrawlTaskIncomplete             = "任务未完成"
	CrawlTaskNoActiveRunner         = "Go 原生运行器未激活"
	CrawlOutputDirEmpty             = "抓取输出目录不能为空"
	CrawlOutputDirResolvedToRun     = "非续跑模式：已创建独立输出目录 %s"
	CrawlSidecarNotStarted          = "侧载进程尚未启动"
	CrawlCloudflareFallbackEnabled  = "[Cloudflare] 用户已启用绕过 → 使用 Node 侧载执行"
	CrawlCloudflareDetected         = "[Cloudflare] 目标站点检测到年龄验证/Cloudflare挑战 → 使用 Node 侧载执行"
	CrawlCloudflareFallbackUnavail  = "[Cloudflare] 目标站点需要绕过但 Node 侧载不可用，仍以 Go 原生方式尝试（可能失败）"
	CrawlGoNativeEngine             = "[Go Native] 使用 Go 原生抓取引擎执行"
	CrawlFetchUnavailable           = "[Fallback] Go 抓取服务不可用 → 回退 Node 侧载执行"
	CrawlGoNativeStopped            = "Go 原生抓取已停止"
	CrawlGoNativeStopAttempt        = "Go 原生运行器未激活，尝试通过 Node 侧载停止任务"
	CrawlResumeExisting             = "任务输出目录已存在，将尝试续跑"
	CrawlPageGapRecorded            = "索引页存在分页缺口"
	CrawlPageEmptyStopped           = "索引页为空，停止继续抓取"
	CrawlLimitReached               = "已达到目标抓取数量上限，当前页面处理完后将结束索引页扫描"
	CrawlLastPageReached            = "已达到配置或推算的最后一页"
	CrawlNoNewLinks                 = "索引页无新链接"
)

// ---- 阶段状态 ----
const (
	PhaseStarting        = "启动中"
	PhaseRunning         = "运行中"
	PhaseStopping        = "终止中"
	PhaseCompleted       = "已完成"
	PhaseIncomplete      = "未完成"
	PhaseError           = "异常"
	PhaseIdle            = "待机"
	PhaseWaiting         = "等待中"
)

// ---- 视频整理 ----
const (
	OrganizerRunning          = "视频整理任务运行中"
	OrganizerStarting         = "视频整理任务启动中"
	OrganizerCompleted        = "整理完成"
	OrganizerPreviewCompleted = "预览完成"
	OrganizerScanProgress     = "扫描进度 %d/%d"
	OrganizerScanCompleted    = "扫描完成：待整理 %d 个，待删除 %d 个"
)

// ---- 广告学习 ----
const (
	AdStrategyMobileNet  = "MobileNetV3 Lite（轻量策略，推荐）"
	AdStrategySqueezeNet = "SqueezeNet Fast（轻量策略，更快）"
	AdStrategyYOLOv8n    = "YOLOv8n Balanced（轻量策略，更严格）"
	AdLearningStarted    = "按番号学习启动"
	AdLearningCompleted  = "按番号学习完成"
	AdLearningMatching   = "按番号匹配中：%d/%d，已命中 %d"
)

// ---- 代理验证 ----
const (
	ProxyEmpty      = "代理未填写"
	ProxyInvalid    = "代理失败"
	ProxyOK         = "代理正常"
	ProxyDirect     = "当前将使用直连方式运行"
	ProxyFormatErr  = "代理地址格式无效，请检查协议、地址和端口"
	ProxyAuthFailed = "代理认证失败"
	ProxyConnTimeout = "连接超时"
	ProxyConnRefused = "代理连接被拒绝"
)

// ---- 依赖管理 ----
const (
	DependencyFFmpegRequired  = "FFmpeg 未检测到，视频整理和广告检测需要 FFmpeg"
	DependencyONNXNotRequired = "ONNX Runtime 未安装（当前使用轻量识别策略，无需 ONNX）"
)

// ---- 前端面板 ----
const (
	PanelSetupTitle   = "抓取设置"
	PanelStatusTitle  = "抓取进度"
	PanelLogTitle     = "实时日志"
	PanelPhaseKicker  = "任务配置"
)

// ---- 弃用标记 ----
const (
	DeprecatedOldPackageJSON = "// Deprecated: package.json 中的 main/bin/files 字段为 Electron/CLI 时代遗留，当前仅作为 Node sidecar 兼容层保留"
	// Keep the legacy sidecar identifier readable until the final cleanup pass
	// removes the last archived Node compatibility traces.
	DeprecatedLegacySidecar  = "// Deprecated: desktop/sidecar/index.js 为旧 Node 侧载入口，Go 原生路径稳定后移除"
	DeprecatedSrcCore        = "// Deprecated: src/core/ 为旧 TypeScript 爬虫核心，Go 原生路径稳定后移除"
)
