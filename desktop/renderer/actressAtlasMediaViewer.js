// Actor Atlas image viewer.
//
// This module owns only the temporary photo dialog. It has no ranking,
// profile, crawler, subscription, or bridge state, so image UI changes cannot
// accidentally affect actor selection or other workspaces.
//
// Ownership summary:
// 1) build and dispose the Actor Atlas media dialog
// 2) manage image selection and keyboard navigation inside that dialog
// 3) keep photo viewing independent from Actor Atlas data loading
//
// File map for maintainers:
// 1) URL normalization
// 2) dialog construction and lifecycle
(function initializeActressAtlasMediaViewer(globalScope) {
  function uniqueMediaURLs(urls) {
    return Array.from(new Set((urls || []).map((url) => String(url || '').trim()).filter(Boolean)));
  }

  function open(title, urls, initialIndex = 0) {
    const mediaURLs = uniqueMediaURLs(urls);
    if (!mediaURLs.length) return;

    const existing = document.getElementById('atlas-media-viewer');
    if (existing) existing.remove();

    const viewer = document.createElement('div');
    viewer.id = 'atlas-media-viewer';
    viewer.className = 'subscription-media-viewer atlas-media-viewer';
    viewer.setAttribute('role', 'dialog');
    viewer.setAttribute('aria-modal', 'true');

    const panel = document.createElement('div');
    panel.className = 'subscription-media-panel';
    const header = document.createElement('div');
    header.className = 'subscription-media-header';
    const heading = document.createElement('strong');
    heading.textContent = title || '演员公开照片';
    const closeButton = document.createElement('button');
    closeButton.type = 'button';
    closeButton.className = 'subscription-media-close';
    closeButton.textContent = '×';
    closeButton.title = '关闭';

    const stage = document.createElement('div');
    stage.className = 'subscription-media-stage';
    const mainImage = document.createElement('img');
    mainImage.alt = title || '演员公开照片';
    stage.appendChild(mainImage);
    const thumbnails = document.createElement('div');
    thumbnails.className = 'subscription-media-thumbnails';
    let activeIndex = 0;

    const showImage = (index) => {
      activeIndex = Math.max(0, Math.min(mediaURLs.length - 1, Number(index) || 0));
      mainImage.src = mediaURLs[activeIndex];
      Array.from(thumbnails.children).forEach((button, buttonIndex) => {
        button.classList.toggle('is-active', buttonIndex === activeIndex);
      });
    };

    mediaURLs.forEach((url, index) => {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'subscription-media-thumbnail';
      const image = document.createElement('img');
      image.src = url;
      image.alt = `${title || '演员'}照片 ${index + 1}`;
      button.appendChild(image);
      button.addEventListener('click', () => showImage(index));
      thumbnails.appendChild(button);
    });

    const close = () => viewer.remove();
    closeButton.addEventListener('click', close);
    viewer.addEventListener('click', (event) => { if (event.target === viewer) close(); });
    viewer.addEventListener('keydown', (event) => {
      if (event.key === 'Escape') close();
      if (event.key === 'ArrowLeft') showImage(activeIndex - 1);
      if (event.key === 'ArrowRight') showImage(activeIndex + 1);
    });

    header.append(heading, closeButton);
    panel.append(header, stage, thumbnails);
    viewer.appendChild(panel);
    document.body.appendChild(viewer);
    viewer.tabIndex = -1;
    viewer.focus();
    showImage(initialIndex);
  }

  globalScope.desktopActressAtlasMediaViewer = { open };
})(typeof globalThis !== 'undefined' ? globalThis : window);
