// Shared version-history text used by the active desktop hero panels.
// Renderer fallback text must stay aligned with this list so packaging/runtime
// startup failures do not silently revert operators to stale release notes.
//
// Ownership summary:
// 1) define the shared release/version timeline text shown in desktop surfaces
// 2) keep renderer fallback and packaged text aligned to one source
// 3) avoid scattering version-history copy across multiple UI modules
//
// File map for maintainers:
// 1) shared version-history entry list
// 2) exported version-history payload registry

(function registerDesktopVersionHistory(globalScope) {
  const VERSION_HISTORY = [
    { version: '0.1', summary: '修复安装流程，实现基础运行能力' },
    { version: '0.2', summary: '桌面 GUI 上线，Windows 即开即用' },
    { version: '0.3', summary: '新增配置选项，界面布局优化' },
    { version: '0.4', summary: '增强分页校验，优化补抓逻辑' },
    { version: '0.5', summary: '全面汉化界面与交互提示' },
    { version: '0.6', summary: '新增补爬功能，支持磁力导出' },
    { version: '0.7', summary: '修复日志乱码问题' },
    { version: '0.8', summary: '任务状态落盘，支持断点续爬' },
    { version: '0.9', summary: '代码注释完善，显示体验优化' },
    { version: '0.10', summary: '优化解析容错，强化补抓去重' },
    { version: '0.11', summary: '新增备用网址，修复多项已知问题' },
    { version: '0.12', summary: '大任务稳定性增强，操作体验优化' },
    { version: '0.13', summary: '分页缺口补查，批量写盘减少 IO' },
    { version: '0.14', summary: '三段式补抓队列，提升补抓效率' },
    { version: '0.15', summary: '抓取速度大幅提升，动态任务栏上线' },
    { version: '0.16', summary: '精简输出文件，升级重试策略' },
    { version: '0.17', summary: '全新界面设计，修复入队问题' },
    { version: '0.18', summary: 'FANZA 女优排行榜，一键参数填充' },
    { version: '0.19', summary: '代码解耦优化，支持自定义背景' },
    { version: '0.20', summary: '多渠道榜单获取、榜单多元化，并修复抓取优先级等已知问题' },
    { version: '0.21', summary: '新增磁力内容校验（广告过滤），自动跳过广告包/杂文件包并切换下一条候选磁力' },
    { version: '0.22', summary: '爬虫与视频整理一体化增强，新增广告处理方式可视化切换、遗漏番号补抓对账与学习链路优化' },
    { version: '0.23', summary: '内嵌 AI 广告检测模型上线，新增启停切换与模型选择（MobileNetV3/SqueezeNet/YOLOv8n）' },
    { version: '0.24', summary: '模块联动增强，整理版本更新与爬虫同步，配置面板精简与日志保留策略改进' },
    { version: '0.25', summary: '抓取进度面板视觉升级，运行状态配色复刻整理结果风格，两模块 UI 统一' },
    { version: '0.26', summary: '代码架构模块化解耦：IPC通道常量化、统一错误分类体系、Proxy响应式状态管理、UI控制器拆分（form/organizer分层），统一日志格式规范' },
    { version: '0.27', summary: 'AV订阅板块相关内容更新：主女优识别、手动订阅兜底、清空订阅与界面联动优化' },
    { version: '0.30', summary: '核心架构迁移至Go语言开发，软件体积从200MB减小至15MB，爬虫执行引擎全面Go原生接管' },
    { version: '0.4.0', summary: '品牌升级为 JavFlow，发布日期过滤、反封锁增强、媒体库刮削修复、UI 与交互优化' },
    { version: '0.4.1', summary: '视频整理磁力别名严格匹配、媒体库刮削修复、A/B 分集共享元数据' },
    { version: '0.4.2', summary: '视频整理、媒体库刮削与 AV 订阅体验优化' },
    { version: '0.4.3', summary: '演员图鉴上线：真实榜单、演员资料、作品浏览与爬虫联动' },
    { version: '0.4.31', summary: '演员搜索、订阅与视频整理工作流优化' }
  ];

  const payload = { VERSION_HISTORY };
  const registry = (globalScope.__desktopTextModules = globalScope.__desktopTextModules || {});
  Object.assign(registry, payload);

  if (typeof module !== 'undefined' && module.exports) {
    module.exports = payload;
  }
})(typeof globalThis !== 'undefined' ? globalThis : window);
