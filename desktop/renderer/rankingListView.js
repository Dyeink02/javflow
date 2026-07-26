// Ranking list view owns DOM rendering for ranking items and selector
// options. Business actions stay in the controller and are bound through
// callbacks so the ranking module keeps a clear controller/view boundary.
//
// Ownership summary:
// 1) render ranking rows and selector option lists
// 2) expose passive callback hooks for controller-owned actions
// 3) keep ranking item/option presentation local to the view layer
//
// File map for maintainers:
// 1) ranking option/date formatting helpers
// 2) empty-state and selector render helpers
// 3) ranking item list/card DOM builders
(function initializeRankingListView(globalScope) {
  const rendererHelpers = globalScope.desktopRendererHelpers || {};
  const clearChildren = rendererHelpers.clearChildren;

  if (!clearChildren) {
    throw new Error('desktopRendererHelpers must be loaded before rankingListView');
  }

  function createOption(value, text) {
    const option = document.createElement('option');
    option.value = String(value);
    option.textContent = text;
    return option;
  }

  function normalizeNumericValues(values, predicate) {
    return Array.from(
      new Set(
        (Array.isArray(values) ? values : [])
          .map((value) => Number.parseInt(String(value || ''), 10))
          .filter((value) => Number.isFinite(value) && (!predicate || predicate(value)))
      )
    );
  }

  function toDisplayMonth(month) {
    const numericMonth = Number.parseInt(String(month || ''), 10);
    if (!Number.isFinite(numericMonth) || numericMonth < 1 || numericMonth > 12) {
      return String(month || '');
    }

    return `${String(numericMonth).padStart(2, '0')} 月`;
  }

  function renderEmpty(container, message = '当前没有可展示的榜单数据。') {
    if (!container) {
      return;
    }

    clearChildren(container);
    const empty = document.createElement('article');
    empty.className = 'ranking-empty';
    empty.textContent = message;
    container.appendChild(empty);
  }

  // Ranking rows stay view-only: render normalized ranking payload plus button
  // placeholders. Profile open / crawler prefill actions remain callback-bound
  // controller work.
  function renderRankingItems(container, items, callbacks = {}, text = {}) {
    if (!container) {
      return;
    }

    const rankingItems = Array.isArray(items) ? items : [];
    if (rankingItems.length === 0) {
      renderEmpty(container, text.empty || '当前没有可展示的榜单数据。');
      return;
    }

    clearChildren(container);
    const fragment = document.createDocumentFragment();
    rankingItems.forEach((item) => {
      const row = document.createElement('article');
      const rank = document.createElement('span');
      const info = document.createElement('div');
      const nameButton = document.createElement('button');

      row.className = 'ranking-item';
      rank.className = 'ranking-rank';
      info.className = 'ranking-info';
      nameButton.type = 'button';
      nameButton.className = 'ranking-name-link';
      nameButton.textContent = item.actressName || text.unknownActress || '未知女优';
      nameButton.title = `${item.actressName || text.unknownActress || '未知女优'}${
        text.autoFillButtonTitleSuffix || ''
      }`;

      rank.textContent = `${item.rank}${text.rankSuffix || '名'}`;
      if (typeof callbacks.onBindFillTarget === 'function') {
        callbacks.onBindFillTarget(nameButton, item);
      }

      info.appendChild(nameButton);

      if (item.profileUrl) {
        const subtitle = document.createElement('button');
        const sourceLabel =
          typeof callbacks.formatSourceLabel === 'function'
            ? callbacks.formatSourceLabel(item.profileUrl)
            : String(item.profileUrl);

        subtitle.type = 'button';
        subtitle.className = 'ranking-link';
        subtitle.textContent = `${text.sourceItemPrefix || '目录链接：'}${sourceLabel}`;
        subtitle.title = item.profileUrl || '';
        if (typeof callbacks.onBindOpenProfile === 'function') {
          callbacks.onBindOpenProfile(subtitle, item);
        }
        info.appendChild(subtitle);
      }

      row.appendChild(rank);
      row.appendChild(info);
      fragment.appendChild(row);
    });

    container.appendChild(fragment);
  }

  function renderYearOptions(selectElement, years = [], selectedYear = '', text = {}) {
    if (!selectElement) {
      return;
    }

    const normalizedYears = normalizeNumericValues(years).sort((left, right) => right - left);
    clearChildren(selectElement);

    if (normalizedYears.length === 0) {
      selectElement.appendChild(createOption('', text.noYearData || '暂无年度数据'));
      selectElement.disabled = true;
      return;
    }

    normalizedYears.forEach((year) => {
      selectElement.appendChild(createOption(year, `${year}${text.yearOptionSuffix || ' 年'}`));
    });

    selectElement.disabled = false;
    const preferredYear = String(selectedYear || normalizedYears[0]);
    selectElement.value = normalizedYears.some((year) => String(year) === preferredYear)
      ? preferredYear
      : String(normalizedYears[0]);
  }

  function renderMonthOptions(selectElement, months = [], selectedMonth = '', text = {}) {
    if (!selectElement) {
      return;
    }

    const normalizedMonths = normalizeNumericValues(
      months,
      (value) => value >= 1 && value <= 12
    ).sort((left, right) => right - left);
    clearChildren(selectElement);

    if (normalizedMonths.length === 0) {
      selectElement.appendChild(createOption('', text.noMonthData || '暂无月份数据'));
      selectElement.disabled = true;
      return;
    }

    normalizedMonths.forEach((month) => {
      selectElement.appendChild(createOption(month, toDisplayMonth(month)));
    });

    selectElement.disabled = false;
    const preferredMonth = String(selectedMonth || normalizedMonths[0]);
    selectElement.value = normalizedMonths.some((month) => String(month) === preferredMonth)
      ? preferredMonth
      : String(normalizedMonths[0]);
  }

  function renderSourceChannelOptions(selectElement, selectedValue = 'smart', text = {}) {
    if (!selectElement) {
      return;
    }

    const allowedValues = ['smart', 'fanza', 'dmm', 'avfan', 'local'];
    clearChildren(selectElement);
    selectElement.appendChild(createOption('smart', text.channelSmart || '智能推荐'));
    selectElement.appendChild(createOption('fanza', text.channelFanza || 'FANZA 官方'));
    selectElement.appendChild(createOption('dmm', text.channelDmm || 'DMM 官方'));
    selectElement.appendChild(createOption('avfan', text.channelAvfan || 'AVfan 在线'));
    selectElement.appendChild(createOption('local', text.channelLocal || '本地历史'));
    selectElement.value = allowedValues.includes(String(selectedValue || '')) ? String(selectedValue) : 'smart';
  }

  function renderModeOptions(selectElement, selectedValue = 'monthly', text = {}) {
    if (!selectElement) {
      return;
    }

    clearChildren(selectElement);
    selectElement.appendChild(createOption('monthly', text.monthMode || '月度'));
    selectElement.appendChild(createOption('annual', text.annualMode || '年度'));
    selectElement.value = ['monthly', 'annual'].includes(String(selectedValue || ''))
      ? String(selectedValue)
      : 'monthly';
  }

  globalScope.desktopRankingListView = {
    renderEmpty,
    renderRankingItems,
    renderYearOptions,
    renderMonthOptions,
    renderSourceChannelOptions,
    renderModeOptions
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
