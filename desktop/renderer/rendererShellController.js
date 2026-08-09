// Renderer shell controller owns non-business UI shell behavior:
// 1) workspace tab switching
// 2) crawler ops panel promotion/copy normalization
// 3) lightweight global HTML handlers that should stay outside crawl logic
//
// Keep this separate from renderer.js so later crawler/runtime refactors do not
// need to wade through workspace shell and static presentation glue.
//
// Text ownership rule:
// - shell-level runtime copy fixes belong here only when the label depends on
//   dynamic shell layout or card structure
// - ordinary static wording should live in uiTextSource.js / HTML partials
// - if this file starts carrying too much product wording, future text-encoding
//   and duplicate-copy bugs become much harder to classify
//
// Ownership summary:
// 1) bootstrap non-business shell wiring and workspace switching
// 2) own shell-level panel promotion and static shell copy normalization
// 3) keep shell/global handlers separate from domain-controller behavior
//
// File map for maintainers:
// 1) shell bootstrap and workspace wiring
// 2) crawler-ops copy/panel shell normalization
// 3) global handler exposure for shell-only UI affordances
(function initializeRendererShellController(globalScope) {
  const shellView = globalScope.desktopRendererShellView || null;

  if (!shellView) {
    throw new Error('desktopRendererShellView is required before rendererShellController');
  }

  function createRendererShellController(options) {
    const { elements, initialWorkspace = 'crawler', onWorkspaceChange } = options || {};
    let workspaceEventsBound = false;
    let bootstrapCompleted = false;
    let currentWorkspace = initialWorkspace;

    // Shell-level copy normalization intentionally funnels through one place so
    // business controllers do not each patch shared headings/hero copy.
    function normalizeCrawlerOpsCopy() {
      shellView.normalizeCrawlerOpsCopy(elements);
      shellView.applySubscriptionHeroCopy(elements.subscriptionHeroCopy);
    }

    // DOM promotion is a shell composition concern, not a crawler concern.
    // Keep the relocation here so crawl runtime/state code never depends on
    // where the shell chooses to mount the panel.
    function promoteCrawlOpsPanel() {
      shellView.promoteCrawlOpsPanel(elements.crawlerWorkspace);
    }

    function setWorkspace(targetWorkspace) {
      // Workspace switching is purely presentational. Domain controllers should
      // react to visibility changes only through their own bootstrap/state
      // paths rather than attaching business logic here.
      //
      // 审计 H-06/H-07：当离开需要持续动画或轮询的工作区时，通过
      // onWorkspaceChange 回调通知业务控制器释放资源，避免后台泄漏。
      const previousWorkspace = currentWorkspace;
      currentWorkspace = targetWorkspace;

      if (typeof onWorkspaceChange === 'function') {
        onWorkspaceChange(targetWorkspace, previousWorkspace);
      }

      const showOrganizer = targetWorkspace === 'organizer';
      const showActressAtlas = targetWorkspace === 'actressatlas';
      const showSubscription = targetWorkspace === 'subscription';
      const showLibraryMetadata = targetWorkspace === 'librarymetadata';
      const showCrawler = !showActressAtlas && !showOrganizer && !showSubscription && !showLibraryMetadata;

      if (elements.actressAtlasWorkspace) {
        elements.actressAtlasWorkspace.classList.toggle('hidden', !showActressAtlas);
      }

      if (elements.crawlerWorkspace) {
        elements.crawlerWorkspace.classList.toggle('hidden', !showCrawler);
      }

      if (elements.organizerWorkspace) {
        elements.organizerWorkspace.classList.toggle('hidden', !showOrganizer);
      }

      if (elements.subscriptionWorkspace) {
        elements.subscriptionWorkspace.classList.toggle('hidden', !showSubscription);
      }

      if (elements.libraryMetadataWorkspace) {
        elements.libraryMetadataWorkspace.classList.toggle('hidden', !showLibraryMetadata);
      }

      if (elements.navCrawlerButton) {
        elements.navCrawlerButton.classList.toggle('is-active', showCrawler);
      }

      if (elements.navActressAtlasButton) {
        elements.navActressAtlasButton.classList.toggle('is-active', showActressAtlas);
      }

      if (elements.navOrganizerButton) {
        elements.navOrganizerButton.classList.toggle('is-active', showOrganizer);
      }

      if (elements.navSubscriptionButton) {
        elements.navSubscriptionButton.classList.toggle('is-active', showSubscription);
      }

      if (elements.navLibraryMetadataButton) {
        elements.navLibraryMetadataButton.classList.toggle('is-active', showLibraryMetadata);
      }
    }

    function toggleInfoCard(cardId) {
      shellView.toggleInfoCard(cardId);
    }

    function exposeGlobalHandlers() {
      // HTML-inline handlers are kept here as a compatibility shim for the shell
      // template only. Business modules should continue using explicit listeners.
      if (typeof globalThis !== 'undefined') {
        globalThis.toggleInfoCard = toggleInfoCard;
      }
    }

    function bindWorkspaceSwitch() {
      // Tab-button binding is shell-only wiring. Feature controllers should not
      // attach their own competing workspace-switch listeners.
      if (workspaceEventsBound) {
        return;
      }
      workspaceEventsBound = true;

      if (elements.navCrawlerButton) {
        elements.navCrawlerButton.addEventListener('click', () => setWorkspace('crawler'));
      }

      if (elements.navActressAtlasButton) {
        elements.navActressAtlasButton.addEventListener('click', () => setWorkspace('actressatlas'));
      }

      if (elements.navOrganizerButton) {
        elements.navOrganizerButton.addEventListener('click', () => setWorkspace('organizer'));
      }

      if (elements.navSubscriptionButton) {
        elements.navSubscriptionButton.addEventListener('click', () => setWorkspace('subscription'));
      }

      if (elements.navLibraryMetadataButton) {
        elements.navLibraryMetadataButton.addEventListener('click', () => setWorkspace('librarymetadata'));
      }
    }

    function bootstrap() {
      if (bootstrapCompleted) {
        return;
      }

      // Bootstrap order stays shell-first: expose global handlers, wire tab
      // switching, normalize shell copy/layout, then reveal the initial
      // workspace.
      exposeGlobalHandlers();
      bindWorkspaceSwitch();
      promoteCrawlOpsPanel();
      normalizeCrawlerOpsCopy();
      setWorkspace(initialWorkspace);
      bootstrapCompleted = true;
    }

    return {
      bootstrap,
      setWorkspace
    };
  }

  globalScope.desktopRendererShellController = {
    createRendererShellController
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
