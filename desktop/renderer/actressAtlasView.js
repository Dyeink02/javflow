// Actor Atlas detail renderer.
//
// This module renders already-resolved Actor Atlas state. Network requests,
// cache mutation, workspace routing, and event orchestration intentionally
// remain in actressAtlasController.js.
//
// Ownership summary:
// 1) render rankings, profiles, photos, and work cards
// 2) expose rendering callbacks without owning application state
// 3) show deterministic loading and failure feedback for image cards
//
// File map for maintainers:
// 1) DOM formatting and profile helpers
// 2) ranking/detail rendering
// 3) work-card rendering and image feedback
(function initializeActressAtlasView(globalScope) {
  function createActressAtlasView(options) {
    const { elements, openMedia } = options || {};
    const nodeText = (node, value) => {
      if (node) node.textContent = value == null ? '' : String(value);
    };
    const setEnabled = (node, enabled) => {
      if (node) node.disabled = !enabled;
    };

    function profileField(profile, key) {
      return String(profile && profile.profileFields && profile.profileFields[key] || '').trim();
    }

    function isExternalHTTPURL(value) {
      try {
        const url = new URL(String(value || '').trim());
        return url.protocol === 'http:' || url.protocol === 'https:';
      } catch (_) {
        return false;
      }
    }

    function profileItem(label, value) {
      const item = document.createElement('div');
      item.className = 'atlas-profile-item';
      const title = document.createElement('span');
      title.textContent = label;
      const canOpen = (label === '博客' || label === '官方网站') && isExternalHTTPURL(value);
      const content = document.createElement(canOpen ? 'a' : 'strong');
      if (canOpen) {
        content.className = 'atlas-profile-link';
        content.href = value;
        content.target = '_blank';
        content.rel = 'noreferrer';
        content.title = value;
      }
      content.textContent = value || '暂无';
      item.append(title, content);
      return item;
    }

    function imageKey(url) {
      try {
        const parsed = new URL(url, globalScope.location && globalScope.location.href);
        parsed.search = '';
        parsed.hash = '';
        return parsed.href.toLowerCase();
      } catch (_) {
        return String(url || '').toLowerCase();
      }
    }

    function rankingDisplayName(ranking) {
      const labels = {
        smart: '智能推荐',
        fanza: 'FANZA',
        dmm: 'DMM',
        avfan: 'AVfan',
        local: '本地历史'
      };
      const channel = String(ranking && (ranking.sourceChannel || ranking.resolvedSource) || '').trim().toLowerCase();
      if (labels[channel]) return labels[channel];
      return String(ranking && ranking.sourceName || '榜单')
        .replace(/\s*(官方|第三方参考|第三方|在线)\s*/g, '')
        .trim() || '榜单';
    }

    function neutralizeRankingNotice(value) {
      return String(value || '')
        .replace(/官方(?:榜单|月榜|年榜|渠道)?/g, '榜单')
        .replace(/第三方参考/g, '')
        .replace(/第三方/g, '')
        .replace(/\s{2,}/g, ' ')
        .trim();
    }

    function renderRanking(ranking, onSelect) {
      const items = Array.isArray(ranking && ranking.items) ? ranking.items : [];
      nodeText(elements.atlasRankingTotal, `${items.length} 位`);
      const notice = neutralizeRankingNotice(ranking && ranking.notice);
      nodeText(elements.atlasRankingNotice, notice || `${rankingDisplayName(ranking)} · ${ranking && ranking.periodLabel || ''}`);
      nodeText(elements.atlasRankingFooter, `显示榜单返回的 ${items.length} 位演员`);
      if (!elements.atlasRankingList) return;

      elements.atlasRankingList.replaceChildren();
      items.forEach((item) => {
        const button = document.createElement('button');
        button.type = 'button';
        button.className = 'atlas-ranking-item';
        button.setAttribute('role', 'listitem');
        const rank = document.createElement('strong');
        rank.className = 'atlas-ranking-rank';
        rank.textContent = `#${item.rank || '-'}`;
        const image = document.createElement('img');
        image.className = 'atlas-ranking-avatar';
        image.src = item.imageUrl || '';
        image.alt = '';
        image.loading = 'lazy';
        const copy = document.createElement('span');
        copy.className = 'atlas-ranking-copy';
        const name = document.createElement('strong');
        name.textContent = item.actressName || '未知演员';
        const meta = document.createElement('small');
        meta.textContent = item.latestTitle || '';
        copy.append(name, meta);
        button.append(rank, image, copy);
        button.addEventListener('click', () => onSelect(item));
        elements.atlasRankingList.appendChild(button);
      });
    }

    function renderProfile(profile) {
      const body = profileField(profile, '身材');
      const height = (body.match(/T\s*(\d{2,3})/i) || [])[1];
      const bust = (body.match(/B\s*(\d{2,3})/i) || [])[1];
      const waist = (body.match(/W\s*(\d{2,3})/i) || [])[1];
      const hip = (body.match(/H\s*(\d{2,3})/i) || [])[1];
      const cupMatch = ((body.match(/\(([^)]+)\)/) || [])[1] || '').match(/[A-Z]+/i);
      const fields = [
        ['身高', height ? `${height} cm` : ''],
        ['三围', [bust && `B${bust}`, waist && `W${waist}`, hip && `H${hip}`].filter(Boolean).join(' / ')],
        ['罩杯', cupMatch ? cupMatch[0].toUpperCase() : ''],
        ['生日', profileField(profile, '生日')],
        ['年龄', profileField(profile, '年龄')],
        ['别名', profileField(profile, '别名')],
        ['出生地', profileField(profile, '出生地')],
        ['所属', profileField(profile, '所属')],
        ['出演期间', profileField(profile, '出演期间')],
        ['博客', profileField(profile, '博客')],
        ['官方网站', profileField(profile, '官方网站')]
      ];
      if (elements.atlasActorProfile) elements.atlasActorProfile.replaceChildren(...fields.map(([label, value]) => profileItem(label, value)));
    }

    function renderStats(item, profile, worksLength) {
      const total = Number(profile && profile.allCount) || Number(item && item.worksCount) || 0;
      const source = Array.isArray(profile && profile.dataSources) && profile.dataSources.length ? profile.dataSources.join('、') : '公开资料';
      const values = [
        ['榜单名次', item && item.rank ? `#${item.rank}` : '搜索结果'],
        ['作品总数', total ? `${total} 部` : '暂无'],
        ['已加载作品', `${worksLength || 0} 部`],
        ['数据来源', source]
      ];
      if (elements.atlasActorStats) elements.atlasActorStats.replaceChildren(...values.map(([label, value]) => profileItem(label, value)));
    }

    function renderPromotions(item, profile) {
      if (!elements.atlasPromoGrid) return;
      const avatar = imageKey(profile && profile.avatarUrl);
      const seen = new Set();
      const urls = (profile && profile.promotionImageUrls || []).filter((url) => {
        const key = imageKey(url);
        if (!key || key === avatar || seen.has(key)) return false;
        seen.add(key);
        return true;
      });
      elements.atlasPromoGrid.replaceChildren();
      if (!urls.length) {
        const empty = document.createElement('p');
        empty.className = 'atlas-ranking-empty';
        empty.textContent = '资料源未提供独立公开照片。';
        elements.atlasPromoGrid.appendChild(empty);
        return;
      }
      urls.forEach((url, index) => {
        const tile = document.createElement('button');
        tile.type = 'button';
        tile.className = 'atlas-photo-tile';
        const image = document.createElement('img');
        image.src = url;
        image.alt = `${item.actressName || '演员'}公开照片`;
        image.loading = 'lazy';
        const label = document.createElement('span');
        label.textContent = '公开照片';
        tile.append(image, label);
        tile.addEventListener('click', () => openMedia(`${item.actressName || '演员'}公开照片`, urls, index));
        elements.atlasPromoGrid.appendChild(tile);
      });
    }

    function renderWorks(state) {
      const works = Array.isArray(state && state.works) ? state.works : [];
      const total = Math.max(Number(state && state.workTotal) || 0, works.length);
      const pageSize = Math.max(1, Number(state && state.pageSize) || 8);
      const pageCount = Math.max(1, Math.ceil(total / pageSize));
      const page = Math.max(0, Math.min(Number(state && state.workPage) || 0, pageCount - 1));
      nodeText(elements.atlasWorkPageCurrent, `第 ${page + 1} / ${pageCount} 页`);
      setEnabled(elements.atlasWorkPrevious, page > 0);
      setEnabled(elements.atlasWorkNext, page < pageCount - 1);
      nodeText(elements.atlasWorkCount, total ? `已加载 ${works.length} / ${total} 部真实作品` : '正在读取真实作品列表');
      if (!elements.atlasWorks) return { page, pageCount };

      const visibleWorks = works.slice(page * pageSize, page * pageSize + pageSize);
      elements.atlasWorks.replaceChildren();
      visibleWorks.forEach((work) => {
        const card = document.createElement('button');
        card.type = 'button';
        card.className = 'atlas-work-card';
        const frame = document.createElement('span');
        frame.className = 'atlas-work-cover-frame';
        const placeholder = document.createElement('span');
        placeholder.className = `atlas-work-cover-placeholder${work.coverUrl ? ' is-loading' : ''}`;
        placeholder.textContent = work.coverUrl ? '正在加载封面...' : '暂无封面';
        frame.appendChild(placeholder);
        if (work.coverUrl) {
          const cover = document.createElement('img');
          cover.className = 'atlas-work-cover';
          cover.src = work.coverUrl;
          cover.alt = '';
          cover.addEventListener('load', () => {
            work.coverLoadFailed = false;
            work.coverRetrying = false;
            frame.classList.add('is-loaded');
          });
          cover.addEventListener('error', () => {
            work.coverLoadFailed = true;
            work.coverRetrying = false;
            placeholder.classList.remove('is-loading');
            placeholder.classList.add('is-retryable');
            placeholder.textContent = '封面加载失败，点击重试';
            cover.remove();
          });
          frame.appendChild(cover);
        }
        frame.addEventListener('click', (event) => {
          if (!work.coverLoadFailed || typeof state.onRetryCover !== 'function') return;
          event.preventDefault();
          event.stopPropagation();
          work.coverRetrying = true;
          placeholder.classList.remove('is-retryable');
          placeholder.classList.add('is-loading');
          placeholder.textContent = '正在重新加载封面...';
          state.onRetryCover(work);
        });
        const copy = document.createElement('span');
        copy.className = 'atlas-work-copy';
        const title = document.createElement('span');
        title.className = 'atlas-work-title';
        title.textContent = work.title || work.code || '未命名作品';
        const meta = document.createElement('span');
        meta.className = 'atlas-work-meta';
        meta.textContent = [work.code, work.releaseDate].filter(Boolean).join(' · ') || '公开资料';
        copy.append(title, meta);
        card.append(frame, copy);
        if (work.url) card.addEventListener('click', () => state.onOpenWork(work.url));
        elements.atlasWorks.appendChild(card);
      });
      return { page, pageCount };
    }

    function renderSelectedState(state) {
      const item = state.item || {};
      const profile = state.profile || {};
      if (elements.atlasDetailEmpty) elements.atlasDetailEmpty.classList.add('hidden');
      if (elements.atlasDetail) elements.atlasDetail.classList.remove('hidden');
      if (elements.atlasActivityLogPanel) elements.atlasActivityLogPanel.classList.remove('hidden');
      nodeText(elements.atlasActorName, item.actressName || '未知演员');
      nodeText(elements.atlasActorMeta, profile.resolvedBase || '正在读取公开演员目录');
      const avatar = profile.avatarUrl || item.imageUrl || '';
      if (elements.atlasActorAvatar) {
        elements.atlasActorAvatar.src = avatar;
        elements.atlasActorAvatar.alt = `${item.actressName || '演员'}头像`;
        elements.atlasActorAvatar.onclick = avatar ? () => openMedia(`${item.actressName || '演员'}公开照片`, [avatar, ...(profile.promotionImageUrls || [])]) : null;
      }
      renderProfile(profile);
      renderStats(item, profile, state.worksLength);
      renderPromotions(item, profile);
      return renderWorks(state);
    }

    return { renderRanking, renderProfile, renderStats, renderWorks, renderSelectedState };
  }

  globalScope.desktopActressAtlasView = { createActressAtlasView };
})(typeof globalThis !== 'undefined' ? globalThis : window);
