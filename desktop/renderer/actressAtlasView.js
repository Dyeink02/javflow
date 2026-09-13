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
        avfan: 'AVfan（FANZA DVD 月榜镜像）',
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

    function imagePlaceholder(className, text, loading = false) {
      const placeholder = document.createElement('span');
      placeholder.className = `${className}${loading ? ' is-loading' : ''}`;
      placeholder.textContent = text;
      return placeholder;
    }

    // Keep broken remote images out of the layout. The placeholder remains in
    // the fixed frame while the browser is loading and after a failed request.
    // Do not use the HTML `hidden` attribute during loading: WebView2 can defer
    // a hidden image indefinitely, even when the same local media URL renders
    // immediately in the full-screen viewer. The pending class keeps the frame
    // visually unchanged while leaving the image eligible for decoding.
    function bindSafeImage(image, placeholder, url, labels = {}) {
      const source = String(url || '').trim();
      image.alt = labels.alt || '';
      const settleLoaded = () => {
        if (image.safeImageTimeout) clearTimeout(image.safeImageTimeout);
        image.safeImageTimeout = null;
        image.classList.remove('is-pending');
        image.hidden = false;
        placeholder.classList.remove('is-loading', 'is-error');
        placeholder.hidden = true;
      };
      // A profile refresh and a cover-cache refresh can render the same detail
      // state more than once. Reassigning an identical src starts a new image
      // request in WebView2, even when it is already cached locally.
      if (image.dataset.safeImageSource === source) {
        // Cached/data images can be complete without dispatching another load
        // event. Reconcile the visual state when the binding is reused.
        if (source && image.complete && Number(image.naturalWidth) > 0) settleLoaded();
        if (source && !image.complete) {
          // The image may have been bound while detached in a DocumentFragment;
          // give WebView2 one microtask after insertion to expose its dimensions.
          Promise.resolve().then(() => {
            if (image.dataset.safeImageSource === source && image.complete && Number(image.naturalWidth) > 0) {
              settleLoaded();
            }
          });
        }
        return Boolean(source);
      }
      if (image.safeImageTimeout) clearTimeout(image.safeImageTimeout);
      image.dataset.safeImageSource = source;
      const isLocalMedia = /^(?:data:|\/subscription-media\/)/i.test(source);
      const isEmbeddedImage = /^data:/i.test(source);
      // Local media starts hidden behind its placeholder. Lazy loading hidden
      // images leaves them pending forever, while the media viewer (which does
      // not use lazy loading) can display the exact same URL. Load application
      // media eagerly and reserve lazy loading for uncached remote images.
      image.loading = isLocalMedia ? 'eager' : 'lazy';
      image.hidden = false;
      image.classList.add('is-pending');
      image.removeAttribute('src');
      placeholder.classList.toggle('is-loading', Boolean(source));
      placeholder.textContent = source ? (labels.loading || '正在加载图片...') : (labels.empty || '暂无图片');
      placeholder.classList.remove('is-error');
      if (!source) {
        image.classList.remove('is-pending');
        image.hidden = true;
        return false;
      }
      let settled = false;
      let optimisticEmbeddedImage = false;
      // Application media is already in user data (or in memory) and must not
      // be marked failed merely because a lazy-load timer elapsed.
      const timeout = isLocalMedia ? null : setTimeout(() => {
        if (settled) return;
        settled = true;
        image.safeImageTimeout = null;
        image.classList.remove('is-pending');
        image.hidden = true;
        image.removeAttribute('src');
        placeholder.hidden = false;
        placeholder.classList.remove('is-loading');
        placeholder.classList.add('is-error');
        placeholder.textContent = labels.timeout || labels.error || '图片加载超时';
      }, Number(labels.timeoutMs) || 10000);
      image.onload = () => {
        if (settled) return;
        settled = true;
        settleLoaded();
      };
      image.onerror = () => {
        // Embedded data URLs are shown optimistically because WebView2 can
        // paint them without a load callback. Preserve a later real decode
        // failure so an invalid resource still becomes an error placeholder.
        if (settled && !optimisticEmbeddedImage) return;
        settled = true;
        optimisticEmbeddedImage = false;
        clearTimeout(timeout);
        image.safeImageTimeout = null;
        image.classList.remove('is-pending');
        image.hidden = true;
        image.removeAttribute('src');
        placeholder.hidden = false;
        placeholder.classList.remove('is-loading');
        placeholder.classList.add('is-error');
        placeholder.textContent = labels.error || '图片加载失败';
      };
      placeholder.hidden = false;
      image.safeImageTimeout = timeout;
      image.src = source;
      // WebView2 may expose a decoded cached/data image synchronously without
      // dispatching `load`; settle immediately when the resource is ready.
      if (image.complete && Number(image.naturalWidth) > 0) {
        settled = true;
        settleLoaded();
      } else if (isEmbeddedImage) {
        // Baseline avatars are bundled as data URLs, not remote requests. Do
        // not leave a loading overlay over an image WebView2 has already
        // painted merely because it skipped the detached-node load callback.
        Promise.resolve().then(() => {
          if (settled || image.dataset.safeImageSource !== source) return;
          settled = true;
          optimisticEmbeddedImage = true;
          settleLoaded();
        });
      } else {
        // Ranking rows are bound before their DocumentFragment is attached to
        // the live DOM. Recheck after insertion for cached images that skipped
        // the load event while detached.
        Promise.resolve().then(() => {
          if (settled || image.dataset.safeImageSource !== source) return;
          if (image.complete && Number(image.naturalWidth) > 0) {
            settled = true;
            settleLoaded();
          }
        });
      }
      return true;
    }

    function renderRankingLoading(message = '正在加载榜单...') {
      nodeText(elements.atlasRankingTotal, '加载中');
      nodeText(elements.atlasRankingNotice, message);
      if (!elements.atlasRankingList) return;
      elements.atlasRankingList.replaceChildren();
      const fragment = document.createDocumentFragment();
      for (let index = 0; index < 8; index += 1) {
        const row = document.createElement('div');
        row.className = 'atlas-ranking-loading-row';
        row.setAttribute('aria-hidden', 'true');
        row.textContent = index === 0 ? message : '正在读取榜单内容...';
        fragment.appendChild(row);
      }
      elements.atlasRankingList.appendChild(fragment);
    }

    function renderRanking(ranking, onSelect) {
      const items = Array.isArray(ranking && ranking.items) ? ranking.items : [];
      nodeText(elements.atlasRankingTotal, `${items.length} 位`);
      const notice = neutralizeRankingNotice(ranking && ranking.notice);
      nodeText(elements.atlasRankingNotice, notice || `${rankingDisplayName(ranking)} · ${ranking && ranking.periodLabel || ''}`);
      const expected = Number(ranking && ranking.expectedTotal) || 100;
      const complete = ranking && ranking.complete === true;
      nodeText(elements.atlasRankingFooter, complete
        ? `已显示完整榜单：${items.length}/${expected} 位演员`
        : `已显示全部返回内容：${items.length}/${expected} 位演员（该来源可能未提供完整榜单）`);
      if (!elements.atlasRankingList) return;

      elements.atlasRankingList.replaceChildren();
      const fragment = document.createDocumentFragment();
      items.forEach((item) => {
        const button = document.createElement('button');
        button.type = 'button';
        button.className = 'atlas-ranking-item';
        button.setAttribute('role', 'listitem');
        const rank = document.createElement('strong');
        rank.className = 'atlas-ranking-rank';
        rank.textContent = `#${item.rank || '-'}`;
        const imageFrame = document.createElement('span');
        imageFrame.className = 'atlas-ranking-avatar-frame';
        const cacheFailed = item.avatarCacheFailed === true;
        const imagePlaceholderNode = imagePlaceholder(
          'atlas-image-placeholder',
          cacheFailed ? '头像加载失败' : '正在加载头像...',
          Boolean(item.imageUrl) && !cacheFailed
        );
        if (cacheFailed) imagePlaceholderNode.classList.add('is-error');
        const image = document.createElement('img');
        image.className = 'atlas-ranking-avatar';
        imageFrame.append(imagePlaceholderNode, image);
        if (!cacheFailed) {
          bindSafeImage(image, imagePlaceholderNode, item.imageUrl, {
            alt: `${item.actressName || '演员'}头像`,
            loading: '正在加载头像...',
            empty: '暂无头像',
            error: '头像加载失败',
            timeout: '头像加载超时'
          });
        }
        const copy = document.createElement('span');
        copy.className = 'atlas-ranking-copy';
        const name = document.createElement('strong');
        name.textContent = item.actressName || '未知演员';
        const meta = document.createElement('small');
        meta.textContent = item.latestTitle || '';
        copy.append(name, meta);
        button.append(rank, imageFrame, copy);
        button.addEventListener('click', () => onSelect(item));
        fragment.appendChild(button);
      });
      elements.atlasRankingList.appendChild(fragment);
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
        const imageFrame = document.createElement('span');
        imageFrame.className = 'atlas-photo-frame';
        const imagePlaceholderNode = imagePlaceholder('atlas-image-placeholder', '正在加载照片...', true);
        const image = document.createElement('img');
        imageFrame.append(imagePlaceholderNode, image);
        bindSafeImage(image, imagePlaceholderNode, url, {
          alt: `${item.actressName || '演员'}公开照片`,
          loading: '正在加载照片...',
          empty: '暂无照片',
          error: '照片加载失败'
        });
        const label = document.createElement('span');
        label.className = 'atlas-photo-label';
        label.textContent = '公开照片';
        tile.append(imageFrame, label);
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
            placeholder.textContent = '封面加载失败，点击重新刷新';
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
        const image = elements.atlasActorAvatar;
        let frame = image.parentElement;
        if (!frame || !frame.classList.contains('atlas-actor-avatar-frame')) {
          frame = document.createElement('span');
          frame.className = 'atlas-actor-avatar-frame';
          image.replaceWith(frame);
          frame.appendChild(image);
        }
        let placeholder = frame.querySelector('.atlas-image-placeholder');
        if (!placeholder) {
          placeholder = imagePlaceholder('atlas-image-placeholder', '正在加载头像...', Boolean(avatar));
          frame.insertBefore(placeholder, image);
        }
        bindSafeImage(image, placeholder, avatar, {
          alt: `${item.actressName || '演员'}头像`,
          loading: '正在加载头像...',
          empty: '暂无头像',
          error: '头像加载失败'
        });
        image.onclick = avatar ? () => openMedia(`${item.actressName || '演员'}公开照片`, [avatar, ...(profile.promotionImageUrls || [])]) : null;
      }
      renderProfile(profile);
      renderStats(item, profile, state.worksLength);
      renderPromotions(item, profile);
      return renderWorks(state);
    }

    return { renderRanking, renderRankingLoading, renderProfile, renderStats, renderWorks, renderSelectedState };
  }

  globalScope.desktopActressAtlasView = { createActressAtlasView };
})(typeof globalThis !== 'undefined' ? globalThis : window);
