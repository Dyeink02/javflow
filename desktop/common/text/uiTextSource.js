// Active UI text source for the current desktop frontend.
// Static page copy should default here before any renderer-shell normalization
// or runtime summary layer tries to override wording.
//
// Ownership summary:
// 1) provide canonical shared UI text/schema for renderer modules
// 2) keep product wording centralized outside controllers
// 3) support fallback/bootstrap layers by stable schema, not duplicate policy
//
// File map for maintainers:
// 1) static hero/panel/field/toggle text schema
// 2) proxy/status/advice/help wording groups
// 3) exported UI text registry payload
(function registerDesktopUiTextSource(globalScope) {
  const registry = (globalScope.__desktopTextModules = globalScope.__desktopTextModules || {});
  const isNode = typeof module !== 'undefined' && module.exports;
  const appInfoModule = isNode ? require('./appInfo.js') : registry;
  const APP_INFO = appInfoModule.APP_INFO || {};
  const DEFAULT_BASE_URL = APP_INFO.defaultBaseUrl || 'https://www.javbus.com';

  if (!APP_INFO.defaultBaseUrl) {
    APP_INFO.defaultBaseUrl = DEFAULT_BASE_URL;
  }

  // UI text ownership rule:
  // - static Chinese product copy should live here first
  // - renderer shell/controllers may normalize a few layout-level labels
  // - Go runtime should emit run-state summaries, not become the default home
  //   for ordinary static UI wording
  //
  // If a label is wrong on screen, debug in this order:
  // 1) uiTextSource / appText
  // 2) HTML partial placeholders
  // 3) rendererShellController runtime normalization
  // 4) Go runtime summary payloads
  //
  // This ordering exists because several earlier乱码 issues were caused by
  // runtime overrides reintroducing bad literals after the source text was
  // already fixed.
  //
  // Settings/runtime-derived summaries may still append dynamic text later, but
  // they should not replace this file as the default home of static UI copy.

const UI_TEXT_SOURCE = {
    hero: {
      eyebrow: APP_INFO.eyebrow,
      title: APP_INFO.title,
      subtitle: APP_INFO.subtitle,
      sourcePrefix: APP_INFO.sourcePrefix,
      versionTitle: '版本更新',
      connectionTip: '建议全程开启稳定的 VPN / 代理环境，可有效提升抓取速度、稳定性与异常恢复成功率。'
    },
    panels: {
      setupKicker: '任务配置',
      setupTitle: '抓取设置',
      statusKicker: '运行状态',
      statusTitle: '抓取进度',
      logKicker: '运行日志',
      logTitle: '实时日志'
    },
    fields: {
      taskTemplate: '任务模板',
      taskTemplateHelp: '默认会自动带入推荐的并行、延迟和超时配置。',
      limit: '抓取数量',
      limitHelp: '填 0 表示不限制抓取数量。',
      base: '起始地址',
      output: '输出位置',
      totalPages: '总页数',
      totalPagesHelp: '填 0 表示按程序自动判断。',
      itemsPerPage: '每页条数',
      itemsPerPageHelp: '建议按网站实际每页条数填写，默认 30 条。',
      parallel: '并行量',
      parallelHelp: '建议 1 ~ 3，避免触发封控。',
      delay: '请求延迟',
      delayHelp: '单位为秒。',
      timeout: '超时时间',
      timeoutHelp: '单位为毫秒。',
      proxy: '代理服务器',
      proxyHelp: '留空则直连运行；填写后会自动检测代理是否可用，并每 30 秒刷新一次状态。',
      magnetExcludeKeywords: '磁力过滤词',
      magnetExcludeKeywordsHelp: '支持多个关键字，必须使用英文逗号分隔；命中后会自动跳过该磁力。',
      actressCountFilterThreshold: '过滤演员数量',
      actressCountFilterThresholdHelp: '演员数量大于该值时保留 filmData.json，但不输出磁力 TXT；填 0 关闭。',
      filmCodeFilterThreshold: '过滤影片番号名',
      filmCodeFilterThresholdHelp: '番号名包含这些子串时过滤（不区分大小写、连续匹配）；多个子串用英文逗号分隔。',
      minReleaseDate: '过滤发行日期',
      minReleaseDateHelp: '只保留该日期及之后发行的影片；留空则不过滤。支持 2016-01-01、2016/1/1、2016年1月1日 等格式。'
    },
    placeholders: {
      base: DEFAULT_BASE_URL,
      output: '请选择输出目录',
      proxy: 'http://127.0.0.1:7890',
      magnetExcludeKeywords: '-U, 无码',
      filmCodeFilterThreshold: 'VR, 4K',
      minReleaseDate: '2026.1.1'
      },
    proxyStatus: {
      empty: '代理未填写',
      checking: '检测中...',
      valid: '代理正常',
      invalid: '代理失败',
      emptyDetail: '当前将使用直连方式运行。',
      checkingDetail: '正在检测代理连通性，请稍候。',
      validDetail: '检测通过，可继续使用当前代理运行。',
      invalidDetail: '当前代理不可用，请检查协议、地址、端口或代理软件状态。'
    },
    advice: {
      kicker: '智能建议',
      title: '页数估算',
      applyButton: '应用建议',
      defaultPrimary: '填写抓取数量后，系统会按每页条数自动估算总页数。',
      defaultSecondaryPrefix: '当前按每页约 ',
      defaultSecondarySuffix: ' 条估算。',
      suggestedPagesPrefix: '建议总页数：',
      suggestedPagesSuffix: ' 页',
      lastPageEstimatePrefix: '按每页 ',
      lastPageEstimateMiddle: ' 条估算，最后一页约 ',
      lastPageEstimateSuffix: ' 条。',
      manualPagesPrefix: '你当前手动填写：',
      manualPagesSuffix: ' 页。'
    },
    toggles: {
      cloudflareTitle: 'Cloudflare 绕过',
      cloudflareHelp: '被验证页拦截时可开启，提高可恢复性。',
      secondValidationTitle: '结果二次校验',
      secondValidationHelp: '任务结束后对结果做一次对账校验。',
      nomagTitle: '跳过无磁力影片',
      nomagHelp: '没有磁力链接的影片将不写入结果。',
      allmagTitle: '磁力全部爬取',
      allmagHelp: '默认只保留最大磁力，开启后保存全部磁力。',
      magnetContentValidationTitle: '磁力内容校验（广告过滤）',
      magnetContentValidationHelp: '会尝试读取磁力内部文件列表，发现广告包或杂文件包时自动跳过并切换下一条候选磁力；开启后速度会稍慢。',
      nopicTitle: '跳过图片下载',
      nopicHelp: '仅抓取影片信息和磁力，不下载图片。'
    },
    actions: {
      changeBackground: '更换背景',
      resetBackground: '恢复默认',
      updateAntiBlock: '更新反屏蔽',
      openOutput: '打开输出目录',
      openMagnetFile: '打开磁力链接文档',
      browseOutput: '选择',
      start: '开始抓取',
      stop: '终止任务',
      restart: '重新爬取',
      clearLog: '清空日志',
      openLogFolder: '打开日志目录'
    },
    stats: {
      currentPage: '当前页数',
      queued: '已入队',
      attempted: '已尝试',
      completed: '已完成'
    },
    state: {
      label: '状态说明',
      currentLabel: '当前执行',
      unfinishedLabel: '已定位未完成番号',
      duplicateLabel: '已定位重复番号',
      pageGapLabel: '未定位分页缺口',
      filteredLabel: '过滤影片番号',
      completedLabel: '已完成番号',
      failedLabel: '失败详情页',
      defaultMessage: '等待开始抓取。',
      ready: '准备就绪，等待开始。',
      activeEmpty: '当前没有正在执行的项目。',
      unfinishedEmpty: '当前没有已定位未完成番号。',
      duplicateEmpty: '当前没有重复番号。',
      pageGapEmpty: '当前没有未定位分页缺口。',
      filteredEmpty: '当前没有因演员数量过滤的影片番号。',
      completedEmpty: '当前没有已完成番号。',
      failedEmpty: '当前没有失败详情页。',
      failedSummaryPrefix: '失败详情页共 ',
      failedSummaryMiddle: ' 条，当前显示 ',
      failedSummarySuffix: ' 条。',
      unknownItem: '未知项目',
      defaultFailureReason: '未记录失败原因。',
      failureCategoryPrefix: '分类：',
      failureRetryPrefix: '重试：',
      failureManualReview: '需人工复查',
      failureAdvicePrefix: '建议：',
      failureTimePrefix: '最后失败时间：'
    },
    ranking: {
      label: '参考榜单',
      title: 'FANZA 女优榜单',
      channelLabel: '信息渠道',
      modeLabel: '榜单类型',
      yearLabel: '年份',
      monthLabel: '月份',
      monthMode: '月度',
      annualMode: '年度',
      refreshButton: '刷新榜单',
      loading: '正在加载榜单数据...',
      monthHelp: '可选择指定年份与月份，查看对应月度榜单。',
      annualHelp: '可选择指定年份，查看对应年度榜单。',
      unsupportedMonthHistory: '部分来源仅提供最新月份数据，历史月份可能暂时不可用。',
      sourcePrefix: '来源：',
      fetchedAtPrefix: '抓取时间：',
      periodPrefix: '统计周期：',
      totalPrefix: '榜单数量：',
      staleSuffix: '缓存数据',
      openSource: '打开来源',
      openProfile: '打开目录',
      empty: '当前没有可展示的榜单数据。',
      loadFailedMeta: '参考榜单获取失败，请稍后重试。',
      unknownActress: '未知女优',
      autoFillButtonTitleSuffix: ' - 点击后自动填充真实女优目录与有磁力数量',
      rankSuffix: '名',
      sourceItemPrefix: '目录链接：',
      latestHint: '默认优先显示最新可用榜单。',
      annualHint: '切换为年度后将只显示年份维度榜单。',
      noYearData: '暂无年度数据',
      yearOptionSuffix: ' 年',
      noMonthData: '暂无月份数据',
      currentMonthOnly: '月度模式下会优先显示当前月份或最近可用月份。',
      fillHint: '点击女优名称可自动填充起始地址、抓取数量与总页数，便于你检查后开始抓取。',
      officialProxyTip: '该功能建议开启日本地区代理，以获得更稳定的榜单访问体验。'
    },
    log: {
      defaultHint: '任务开始后，这里会实时显示关键日志与运行提示。',
      createdPrefix: '日志文件：',
      createdEventPrefix: '运行日志已创建：',
      cleared: '日志已清空。',
      truncatedSuffix: '内容过长，已自动折叠显示。',
      visibleLevels: ['info', 'warn', 'error'],
      maxVisibleLength: 260,
      hiddenKeywords: [
        'QueueManager: [索引页]',
        'ResourceMonitor:',
        'AJAX重试延迟计算:',
        'Puppeteer池状态',
        '[TIMING]',
        '正在处理详情页:',
        '任务状态已落盘：'
      ],
      initialPathHint: '任务启动后将自动显示本次日志文件路径。'
    },
    messages: {
      templateAppliedPrefix: '已切换任务模板：',
      baseFilledPrefix: '已填充起始地址：',
      backgroundSelectedPrefix: '已切换背景图片：',
      backgroundReset: '已恢复默认背景。',
      startRunning: '抓取任务启动中...',
      restartRunning: '正在执行重新爬取，仅补抓未完成内容...',
      restartQueued: '已提交重新爬取请求，等待当前队列安全切换。',
      restartStarted: '重新爬取已开始。',
      stopRequested: '已发送终止指令，正在中断队列与请求...',
      outputRequired: '请先选择输出目录。',
      outputSelectedPrefix: '已选择输出目录：',
      outputOpenedPrefix: '已打开输出目录：',
      suggestedPagesAppliedPrefix: '已应用建议页数：',
      suggestedPagesAppliedSuffix: ' 页',
      magnetOpenedPrefix: '已打开磁力链接文档：',
      logFolderOpenedPrefix: '已打开日志目录：',
      antiBlockUpdating: '正在更新反屏蔽地址...',
      antiBlockUpdatedPrefix: '已更新反屏蔽地址，共 ',
      antiBlockUpdatedSuffix: ' 条，文件位置：',
      antiBlockUpdateFailedPrefix: '反屏蔽地址更新失败：',
      rankingLoading: '正在刷新 FANZA 榜单...',
      rankingLoadedPrefix: '榜单已刷新：',
      rankingLoadedMiddle: '，共 ',
      rankingLoadedSuffix: ' 条',
      rankingSourceOpenedPrefix: '已打开榜单来源：',
      rankingResolvingPrefix: '正在解析女优目录：',
      rankingResolvedPrefix: '已定位女优：',
      rankingResolvedMagnetPrefix: '有磁力影片 ',
      rankingResolvedAllPrefix: '总站点影片 ',
      rankingResolvedCountSuffix: ' 条',
      rankingResolvedDefaultHint: '结果已自动填充到表单，请检查无误后开始抓取。',
      rankingResolvedPagesPrefix: '预计页数 ',
      rankingResolvedPagesSuffix: ' 页',
      rankingResolveFailedPrefix: '女优目录解析失败：'
    },
    validation: {
      baseRequired: '请填写起始地址。',
      outputRequired: '请先选择输出目录。',
      itemsPerPageInvalid: '每页条数必须大于等于 1。',
      parallelInvalid: '并行量必须大于等于 1。',
      totalPagesInvalid: '总页数必须大于等于 0。',
      delayInvalid: '请求延迟必须大于等于 0。',
      proxyInvalid: '当前代理检测失败，请修正后再启动，或清空代理后直连运行。',
      magnetExcludeKeywordsInvalid: '磁力过滤词格式不正确，请使用英文逗号分隔，例如：-U,SIS001,第一會所',
      timeoutInvalidPrefix: '超时时间不能小于 ',
      timeoutInvalidSuffix: ' 毫秒。'
    },
    limits: {
      defaultItemsPerPage: 30,
      minTimeout: 1000,
      maxLogLines: 240,
      maxPanelItems: 80,
      stateRenderInterval: 180
    },
    runtime: {
      missingDependencies: '桌面端渲染依赖未完整加载。',
      bootstrapFailedPrefix: '桌面界面初始化失败：'
    }
  };

  const payload = { UI_TEXT_SOURCE };
  const registryTarget = (globalScope.__desktopTextModules = globalScope.__desktopTextModules || {});
  Object.assign(registryTarget, payload);

  if (typeof module !== 'undefined' && module.exports) {
    module.exports = payload;
  }
})(typeof globalThis !== 'undefined' ? globalThis : window);
