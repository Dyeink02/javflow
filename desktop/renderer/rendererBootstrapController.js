// Renderer bootstrap controller owns startup orchestration:
// 1) bind shell/runtime listeners in the correct order
// 2) retry async controller bootstrap during desktop bridge warmup
// 3) funnel bootstrap failures back into visible crawl status/log surfaces
//
// Keep this separate from renderer.js so renderer.js stays an assembly root,
// while startup sequencing remains isolated and easier to debug.
//
// Ownership summary:
// 1) bind shell/runtime listeners in the correct order
// 2) orchestrate ordered async controller bootstrap
// 3) funnel bootstrap failures back into visible crawl status/log surfaces
//
// Boundary rule:
// - bootstrap sequencing and retry timing live here
// - workspace controllers stay focused on steady-state behavior after startup
//
// File map for maintainers:
// 1) one-time static feed binding
// 2) ordered controller bootstrap orchestration
// 3) retry/failure reporting during bridge warmup
(function initializeRendererBootstrapController(globalScope) {
  function createRendererBootstrapController(options) {
    const {
      shellController,
      heroBorderFlowController,
      appUpdateController,
      crawlRuntimeController,
      formController,
      actressAtlasController,
      rankingController,
      organizerController,
      subscriptionController,
      libraryMetadataController,
      stateController,
      sourceLink,
      uiText,
      retryDelays = [0, 200, 400, 800, 1200, 1800, 2800, 4000]
    } = options || {};
    let staticFeedsBound = false;
    let bootstrapCompleted = false;
    let bootstrapPromise = null;

    function delay(ms) {
      return new Promise((resolve) => setTimeout(resolve, ms));
    }

    function bindStaticFeeds() {
      if (staticFeedsBound) {
        return;
      }

      // Static feeds bind once before async controller bootstrap retries so
      // shell/runtime listeners do not get duplicated on bridge warmup loops.
      if (shellController && typeof shellController.bootstrap === 'function') {
        shellController.bootstrap();
      }
      if (heroBorderFlowController && typeof heroBorderFlowController.bootstrap === 'function') {
        heroBorderFlowController.bootstrap();
      }
      if (crawlRuntimeController && typeof crawlRuntimeController.bindExternalLink === 'function') {
        crawlRuntimeController.bindExternalLink(sourceLink);
      }
      if (crawlRuntimeController && typeof crawlRuntimeController.bindEventFeeds === 'function') {
        crawlRuntimeController.bindEventFeeds();
      }
      staticFeedsBound = true;
    }

    // Controller bootstrap remains the single ordered startup contract for the
    // three workspaces plus crawl panels. If future startup work is added,
    // prefer inserting it here instead of each controller trying to warm up
    // siblings ad hoc.
    async function bootstrapControllers() {
      // Every workspace owns its own background hydration. Start them together
      // so a slow ranking, subscription, FFmpeg, or metadata-provider request
      // cannot keep unrelated buttons unbound.
      await Promise.all([
        Promise.resolve().then(() => formController.bootstrap()),
        // Preload the actor ranking cache during application startup. The
        // controller remains view-local, but its cache-first load must finish
        // before the user opens the workspace so the July ranking is visible
        // immediately and never waits on an online source by default.
        Promise.resolve().then(() => actressAtlasController.bootstrap()),
        Promise.resolve().then(() => rankingController.bootstrap()),
        Promise.resolve().then(() => subscriptionController.bootstrap()),
        Promise.resolve().then(() => organizerController.bootstrap()),
        Promise.resolve().then(() => libraryMetadataController.bootstrap()),
        Promise.resolve().then(() => appUpdateController && appUpdateController.bootstrap()),
        Promise.resolve().then(() => crawlRuntimeController.bootstrapPanels())
      ]);
    }

    // Startup failure reporting is centralized here so bootstrap-time bridge
    // warmup issues surface through the same visible log/status path.
    function reportBootstrapFailure(error) {
      const prefix =
        uiText && uiText.UI_TEXT && uiText.UI_TEXT.runtime
          ? uiText.UI_TEXT.runtime.bootstrapFailedPrefix
          : 'Desktop bootstrap failed: ';
      const message = `${prefix}${error instanceof Error ? error.message : String(error)}`;
      console.error(message, error);
      crawlRuntimeController.appendLog('error', message);
      stateController.setStatus('error', message);
    }

    function bootstrap() {
      if (bootstrapCompleted) {
        return Promise.resolve();
      }

      if (bootstrapPromise) {
        return bootstrapPromise;
      }

      bootstrapPromise = Promise.resolve()
        .then(async () => {
          let lastError = null;

          bindStaticFeeds();

          // Retry is limited to bootstrap-time dependency warmup. Once all
          // controllers are ready, later runtime errors should surface through
          // their own event/state channels instead of re-entering bootstrap.
          for (let index = 0; index < retryDelays.length; index += 1) {
            if (retryDelays[index] > 0) {
              await delay(retryDelays[index]);
            }

            try {
              await bootstrapControllers();
              bootstrapCompleted = true;
              return;
            } catch (error) {
              lastError = error;
            }
          }

          reportBootstrapFailure(lastError);
          bootstrapPromise = null;
        })
        .catch((error) => {
          bootstrapPromise = null;
          throw error;
        });

      return bootstrapPromise;
    }

    return {
      bootstrap
    };
  }

  globalScope.desktopRendererBootstrapController = {
    createRendererBootstrapController
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
