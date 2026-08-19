// Renderer fallback text bootstrap for the current desktop UI.
// This file is a startup resilience layer and must stay aligned with the
// active shared text modules under desktop/common/text.
//
// Ownership summary:
// 1) provide a renderer-boot fallback text bundle
// 2) keep fallback schema aligned with shared text source modules
// 3) let the renderer survive text-module load failures without becoming the
//    normal wording source of truth
//
// File map for maintainers:
// 1) fallback shared text schema
// 2) shared text source resolution and merge helpers
// 3) exported renderer UI text payload
(function initializeDesktopUiText(globalScope) {
  // Prefer `desktop/common/text/*` as the source of truth. The large fallback
  // bundle below exists so the renderer can still boot if shared text modules
  // fail to load during packaging or runtime startup.
  //
  // Maintenance rule:
  // - fallback copy here should stay schema-aligned with uiTextSource.js
  // - do not move normal UI wording ownership into this fallback block
  // - if one side changes, verify both source-of-truth and fallback text
  //   together so packaging/startup failures do not surface stale wording
  // - do not let feature modules start depending on fallback-only strings
  const FALLBACK_SHARED_TEXT = {
    APP_INFO: {
      title: 'JavFlow',
      version: '0.4.32',
      subtitle: '基于开源项目：raawaa',
      eyebrow: 'Windows EXE',
      defaultBaseUrl: 'https://www.javbus.com'
    },
    URL_SUGGESTIONS: [
      'https://www.javbus.com/',
      'https://www.busjav.cyou',
      'https://www.fanbus.bond',
      'https://www.cdnbus.bond'
    ],
    STATUS_LABELS: {
      idle: '待机',
      starting: '启动中',
      running: '运行中',
      stopping: '终止中',
      completed: '已完成',
      incomplete: '未完成',
      stopped: '已终止',
      error: '异常'
    },
    FAILURE_CATEGORY_LABELS: {
      blocked: '验证拦截',
      network: '网络超时',
      empty: '空响应',
      parse: '解析失败',
      cloudflare: 'Cloudflare',
      unknown: '未知异常',
      stopped: '已终止'
    },
    TASK_TEMPLATES: {
      balanced: {
        label: '均衡模板',
        parallel: 2,
        delay: 2,
        timeout: 30000,
        itemsPerPage: 30,
        cloudflare: false,
        secondValidation: true
      },
      stable: {
        label: '稳定模板',
        parallel: 1,
        delay: 4,
        timeout: 45000,
        itemsPerPage: 30,
        cloudflare: true,
        secondValidation: true
      },
      recovery: {
        label: '恢复模板',
        parallel: 1,
        delay: 3,
        timeout: 45000,
        itemsPerPage: 30,
        cloudflare: true,
        secondValidation: true
      }
    },
    VERSION_HISTORY: [
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
      { version: '0.23', summary: '广告检测策略上线（支持MobileNetV3/SqueezeNet/YOLOv8n三种轻量策略配置），新增启停切换与策略选择' },
      { version: '0.24', summary: '模块联动增强，整理版本更新与爬虫同步，配置面板精简与日志保留策略改进' },
      { version: '0.25', summary: '抓取进度面板视觉升级，运行状态配色复刻整理结果风格，两模块 UI 统一' },
      { version: '0.26', summary: '代码架构模块化解耦：IPC通道常量化、统一错误分类体系、Proxy响应式状态管理、UI控制器拆分（form/organizer分层），统一日志格式规范' },
      { version: '0.30', summary: '核心架构迁移至Go语言开发，软件体积从200MB减小至15MB，爬虫执行引擎全面Go原生接管' },
      { version: '0.4.0', summary: '品牌升级为 JavFlow，发布日期过滤、反封锁增强、媒体库刮削修复、UI 与交互优化' },
      { version: '0.4.1', summary: '视频整理磁力别名严格匹配、媒体库刮削修复、A/B 分集共享元数据' },
      { version: '0.4.2', summary: '视频整理、媒体库刮削与 AV 订阅体验优化' },
      { version: '0.4.3', summary: '演员图鉴上线：真实榜单、演员资料、作品浏览与爬虫联动' },
      { version: '0.4.31', summary: '演员搜索、订阅与视频整理工作流优化' },
      { version: '0.4.32', summary: '代理配置提示优化与媒体库刮削稳定性修正' }
    ],
    UI_TEXT_SOURCE: {
      hero: {
        eyebrow: 'Windows EXE',
        title: 'JavFlow',
        subtitle: '基于开源项目：raawaa',
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
        base: 'https://www.javbus.com',
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
        nopicHelp: '仅抓取影片信息和磁力，不下载图片。',
        metadataOnlyTitle: '仅抓取影片信息（不获取磁力）',
        metadataOnlyHelp: '只访问详情页并保存番号、演员、片商、标签和系列；适合为旧视频库建立识别快照。'
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
        completedLabel: '已完成番号',
        failedLabel: '失败与重复',
        defaultMessage: '等待开始抓取。',
        ready: '准备就绪，等待开始。',
        activeEmpty: '当前没有正在执行的项目。',
        unfinishedEmpty: '当前没有已定位未完成番号。',
        duplicateEmpty: '当前没有重复番号。',
        pageGapEmpty: '当前没有未定位分页缺口。',
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
    }
  };

  function isPlainObject(value) {
    return Object.prototype.toString.call(value) === '[object Object]';
  }

  function deepClone(value) {
    if (Array.isArray(value)) {
      return value.map((item) => deepClone(item));
    }

    if (isPlainObject(value)) {
      return Object.entries(value).reduce((result, [key, nestedValue]) => {
        result[key] = deepClone(nestedValue);
        return result;
      }, {});
    }

    return value;
  }

  function deepMerge(baseValue, overrideValue) {
    const baseClone = deepClone(baseValue);

    if (overrideValue == null) {
      return baseClone;
    }

    if (Array.isArray(overrideValue)) {
      return overrideValue.map((item) => deepClone(item));
    }

    if (!isPlainObject(overrideValue)) {
      return deepClone(overrideValue);
    }

    const nextValue = isPlainObject(baseClone) ? baseClone : {};

    Object.entries(overrideValue).forEach(([key, value]) => {
      if (value === undefined) {
        return;
      }

      if (Array.isArray(value)) {
        nextValue[key] = value.map((item) => deepClone(item));
        return;
      }

      if (isPlainObject(value)) {
        nextValue[key] = deepMerge(nextValue[key], value);
        return;
      }

      nextValue[key] = value;
    });

    return nextValue;
  }

  const registrySharedText = isPlainObject(globalScope.__desktopTextModules) ? globalScope.__desktopTextModules : {};
  const appSharedText = isPlainObject(globalScope.desktopAppText) ? globalScope.desktopAppText : {};
  const sharedText = deepMerge(deepMerge(FALLBACK_SHARED_TEXT, registrySharedText), appSharedText);
  const appInfo = sharedText.APP_INFO || FALLBACK_SHARED_TEXT.APP_INFO;
  const versionHistory = Array.isArray(sharedText.VERSION_HISTORY)
    ? sharedText.VERSION_HISTORY
    : FALLBACK_SHARED_TEXT.VERSION_HISTORY;
  const urlSuggestions = Array.isArray(sharedText.URL_SUGGESTIONS)
    ? sharedText.URL_SUGGESTIONS
    : FALLBACK_SHARED_TEXT.URL_SUGGESTIONS;
  const taskTemplates = isPlainObject(sharedText.TASK_TEMPLATES)
    ? sharedText.TASK_TEMPLATES
    : FALLBACK_SHARED_TEXT.TASK_TEMPLATES;
  const statusLabels = isPlainObject(sharedText.STATUS_LABELS)
    ? sharedText.STATUS_LABELS
    : FALLBACK_SHARED_TEXT.STATUS_LABELS;
  const failureCategoryLabels = isPlainObject(sharedText.FAILURE_CATEGORY_LABELS)
    ? sharedText.FAILURE_CATEGORY_LABELS
    : FALLBACK_SHARED_TEXT.FAILURE_CATEGORY_LABELS;
  const uiTextSource = isPlainObject(sharedText.UI_TEXT_SOURCE)
    ? sharedText.UI_TEXT_SOURCE
    : FALLBACK_SHARED_TEXT.UI_TEXT_SOURCE;
  const hasExternalUiTextSource =
    (isPlainObject(appSharedText.UI_TEXT_SOURCE) && Object.keys(appSharedText.UI_TEXT_SOURCE).length > 0) ||
    (isPlainObject(registrySharedText.UI_TEXT_SOURCE) && Object.keys(registrySharedText.UI_TEXT_SOURCE).length > 0);

  if (!hasExternalUiTextSource && typeof console !== 'undefined' && typeof console.warn === 'function') {
    console.warn('桌面共享文案模块未完整加载，已启用内置中文兜底文案。');
  }

  const UI_TEXT = deepMerge(
    {
      appTitle: appInfo.title || 'JAV自动化整理归纳视频软件',
      version: appInfo.version || '0.4.32',
      source: {
        href: appInfo.sourceUrl || 'https://www.javbus.com/star/okq',
        name: appInfo.sourceName || '三上悠亜'
      }
    },
    uiTextSource
  );

  function getValueByPath(source, valuePath) {
    return String(valuePath || '')
      .split('.')
      .filter(Boolean)
      .reduce((currentValue, currentKey) => (currentValue == null ? undefined : currentValue[currentKey]), source);
  }

  function clearChildren(container) {
    while (container && container.firstChild) {
      container.removeChild(container.firstChild);
    }
  }

  function applyDatasetText(root) {
    root.querySelectorAll('[data-ui-text]').forEach((node) => {
      const value = getValueByPath(UI_TEXT, node.dataset.uiText);
      if (typeof value === 'string') {
        node.textContent = value;
      }
    });

    root.querySelectorAll('[data-ui-placeholder]').forEach((node) => {
      const value = getValueByPath(UI_TEXT, node.dataset.uiPlaceholder);
      if (typeof value === 'string') {
        node.setAttribute('placeholder', value);
      }
    });
  }

  function renderTaskTemplateOptions(selectElement) {
    if (!selectElement) {
      return;
    }

    clearChildren(selectElement);

    Object.entries(taskTemplates).forEach(([value, template]) => {
      const option = document.createElement('option');
      option.value = value;
      option.textContent = template.label;
      selectElement.appendChild(option);
    });
  }

  function renderVersionHistory(listElement) {
    if (!listElement) {
      return;
    }

    clearChildren(listElement);

    versionHistory.forEach((item) => {
      const entry = document.createElement('li');
      const version = document.createElement('strong');

      version.textContent = item.version;
      entry.appendChild(version);
      entry.appendChild(document.createTextNode(item.summary));
      listElement.appendChild(entry);
    });
  }

  function renderBaseUrlChips(container) {
    if (!container) {
      return;
    }

    clearChildren(container);

    urlSuggestions.forEach((url) => {
      const button = document.createElement('button');
      button.className = 'base-url-chip';
      button.type = 'button';
      button.dataset.url = url;
      button.textContent = url;
      container.appendChild(button);
    });
  }

  function applyStaticText(root = document) {
    // Resolve section-level text once here so DOM writes stay simple and later
    // copy ownership changes do not spread fallback checks across the file.
    const stateText = isPlainObject(UI_TEXT.state) ? UI_TEXT.state : FALLBACK_SHARED_TEXT.UI_TEXT_SOURCE.state;
    const adviceText = isPlainObject(UI_TEXT.advice) ? UI_TEXT.advice : FALLBACK_SHARED_TEXT.UI_TEXT_SOURCE.advice;
    const logText = isPlainObject(UI_TEXT.log) ? UI_TEXT.log : FALLBACK_SHARED_TEXT.UI_TEXT_SOURCE.log;
    const limits = isPlainObject(UI_TEXT.limits) ? UI_TEXT.limits : FALLBACK_SHARED_TEXT.UI_TEXT_SOURCE.limits;

    document.title = UI_TEXT.appTitle;
    applyDatasetText(root);

    const crawlerTopbarVersion = root.getElementById('crawler-topbar-version');
    if (crawlerTopbarVersion) {
      crawlerTopbarVersion.textContent = `v${UI_TEXT.version}`;
    }

    const organizerVersionBadge = root.getElementById('organizer-version-badge');
    if (organizerVersionBadge) {
      organizerVersionBadge.textContent = `v${UI_TEXT.version}`;
    }

    const subscriptionVersionBadge = root.getElementById('subscription-version-badge');
    if (subscriptionVersionBadge) {
      subscriptionVersionBadge.textContent = `v${UI_TEXT.version}`;
    }

    const sourceLink = root.getElementById('source-link');
    if (sourceLink) {
      sourceLink.textContent = UI_TEXT.source.name;
      sourceLink.href = UI_TEXT.source.href;
    }

    const statusPill = root.getElementById('status-pill');
    if (statusPill) {
      statusPill.textContent = statusLabels.idle || '待机';
    }

    const stateMessage = root.getElementById('state-message');
    if (stateMessage) {
      stateMessage.textContent = stateText.defaultMessage;
    }

    const totalPagesAdvice = root.getElementById('total-pages-advice');
    if (totalPagesAdvice) {
      totalPagesAdvice.textContent = adviceText.defaultPrimary;
    }

    const totalPagesMeta = root.getElementById('total-pages-meta');
    if (totalPagesMeta) {
      totalPagesMeta.textContent = `${adviceText.defaultSecondaryPrefix}${limits.defaultItemsPerPage}${adviceText.defaultSecondarySuffix}`;
    }

    const logFilePath = root.getElementById('log-file-path');
    if (logFilePath) {
      logFilePath.textContent = logText.initialPathHint;
    }

    renderTaskTemplateOptions(root.getElementById('taskTemplate'));
    renderVersionHistory(root.getElementById('version-history-list'));
    renderVersionHistory(root.getElementById('organizer-version-history-list'));
    renderBaseUrlChips(root.getElementById('base-url-hints'));
  }

  globalScope.desktopUiText = {
    UI_TEXT,
    STATUS_LABELS: statusLabels,
    TASK_TEMPLATES: taskTemplates,
    FAILURE_CATEGORY_LABELS: failureCategoryLabels,
    isUsingFallbackBundle: !hasExternalUiTextSource,
    applyStaticText,
    renderTaskTemplateOptions,
    renderVersionHistory,
    renderBaseUrlChips
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
