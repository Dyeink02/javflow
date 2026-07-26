// Renderer shell view owns shell-only DOM composition that is unrelated to
// crawl, organizer, or subscription business state. The controller decides
// when to apply layout changes; the view only mutates shell structure.
//
// Ownership summary:
// 1) normalize shell copy and headings
// 2) perform shell-only DOM composition such as promoting the ops panel
// 3) keep static shell traversal details out of the shell controller
//
// Feature state, runtime queries, and artifact decisions must stay elsewhere.
//
// File map for maintainers:
// 1) shell text/node mutation helpers
// 2) crawler/subscription shell copy normalization
// 3) shell panel promotion and collapse helpers
(function initializeRendererShellView(globalScope) {
  function setTextContent(node, text, options = {}) {
    if (!node) {
      return;
    }
    if (options.onlyWhenEmpty && String(node.textContent || '').trim()) {
      return;
    }
    node.textContent = text;
  }

  function toggleInfoCard(cardId) {
    const card = document.getElementById(cardId);
    if (card) {
      card.classList.toggle('collapsed');
    }
  }

  // Shell copy normalization belongs here because it is pure layout/content
  // composition. Module controllers should not walk shell DOM to rewrite hero
  // copy or panel headings.
  function applySubscriptionHeroCopy(subscriptionHeroCopy) {
    if (!subscriptionHeroCopy) {
      return;
    }

    const eyebrow = subscriptionHeroCopy.querySelector('.eyebrow');
    const title = subscriptionHeroCopy.querySelector('h1');
    const subtitle = subscriptionHeroCopy.querySelector('.hero-subtitle');
    const tip = subscriptionHeroCopy.querySelector('.hero-connection-tip');

    if (eyebrow) {
      eyebrow.textContent = 'AV 订阅模块';
    }
    if (title) {
      title.textContent = '女优订阅与更新追踪';
    }
    if (subtitle) {
      subtitle.textContent = '把已抓女优沉淀为长期订阅，后续只检查新增影片并一键回填到 JAV 爬虫。';
    }
    if (tip) {
      tip.textContent = '支持扫描本地 filmData.json 自动建立订阅，也支持手动输入女优名称或目录地址新增订阅。';
    }
  }

  // Keep crawler summary copy normalization in the shell view so the shell
  // controller stays an orchestrator instead of carrying DOM traversal details.
  function normalizeCrawlerOpsCopy(elements) {
    if (!elements || !elements.crawlerWorkspace) {
      return;
    }

    const opsPanel = elements.crawlerWorkspace.querySelector('.crawl-ops-panel');
    if (opsPanel) {
      setTextContent(opsPanel.querySelector('.panel-kicker'), '抓取总览');
      setTextContent(opsPanel.querySelector('.panel-head h2'), '抓取阶段与结果入口');
    }

    const opsCards = elements.crawlerWorkspace.querySelectorAll('.crawl-ops-grid .ops-card');
    normalizeStageOpsCard(opsCards[0], elements);
    normalizeResultOpsCard(opsCards[1], elements);
  }

  function normalizeStageOpsCard(card, elements) {
    if (!card) {
      return;
    }

    const labels = card.querySelectorAll('.message-label');
    setTextContent(labels[0], '抓取阶段面板');
    setTextContent(elements.crawlStageTitle, '等待开始抓取', { onlyWhenEmpty: true });
    setTextContent(elements.crawlStageStatus, '待机');
    setTextContent(elements.crawlStageProgress, '阶段 1/10');
    setTextContent(elements.crawlStageDescription, '加载配置并初始化本次抓取运行环境。');
    setTextContent(elements.crawlStageMessage, '等待开始抓取。');

    const metricTexts = ['当前页', '已入队', '已尝试', '已完成'];
    card.querySelectorAll('.ops-metric span').forEach((node, index) => {
      if (metricTexts[index]) {
        node.textContent = metricTexts[index];
      }
    });

    setTextContent(card.querySelector('.ops-path-label'), '当前输出目录');
  }

  function normalizeResultOpsCard(card, elements) {
    if (!card) {
      return;
    }

    const labels = card.querySelectorAll('.message-label');
    setTextContent(labels[0], '结果入口面板');
    setTextContent(card.querySelector('.ops-card-head strong'), '抓取产物与复盘入口');
    setTextContent(elements.crawlResultQuality, '尚未生成复盘');
    setTextContent(elements.crawlResultSummary, '等待抓取完成后生成复盘摘要。');
    setTextContent(card.querySelector('.result-history-hint'), '默认只显示最近 2 次结果，剩余历史记录可用鼠标滚轮下拉查看。');
  }

  function promoteCrawlOpsPanel(crawlerWorkspace) {
    if (!crawlerWorkspace) {
      return;
    }

    const crawlOpsGrid = crawlerWorkspace.querySelector('.crawl-ops-grid');
    const crawlerStatsPanel = crawlerWorkspace.querySelector('.stats-panel');

    if (!crawlOpsGrid || !crawlerStatsPanel || crawlOpsGrid.closest('.crawl-ops-panel')) {
      return;
    }

    const wrapper = document.createElement('section');
    wrapper.className = 'panel crawl-ops-panel';
    wrapper.innerHTML = `
      <div class="panel-head">
        <div>
          <p class="panel-kicker">抓取总览</p>
          <h2>抓取阶段与结果入口</h2>
        </div>
      </div>
    `;
    wrapper.appendChild(crawlOpsGrid);
    crawlerStatsPanel.insertAdjacentElement('afterend', wrapper);
  }

  globalScope.desktopRendererShellView = {
    applySubscriptionHeroCopy,
    normalizeCrawlerOpsCopy,
    promoteCrawlOpsPanel,
    toggleInfoCard
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
