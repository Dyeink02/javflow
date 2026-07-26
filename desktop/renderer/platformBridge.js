// Thin selector that exposes the active desktop bridge implementation.
// In the current product this should normally resolve to the Wails bridge.
//
// Selector rule:
// keep environment selection here only. Feature controllers should not start
// branching on bridge flavor by themselves.
//
// Ownership summary:
// 1) choose the active renderer-facing transport implementation
// 2) expose optional query passthroughs that can degrade gracefully
// 3) keep bridge-flavor checks out of feature controllers and view helpers
//
// Protocol packet shaping belongs to bridgeProtocol.js plus the concrete bridge
// implementation. This selector should stay a thin routing layer only.
//
// File map for maintainers:
// 1) active bridge selection helpers
// 2) optional artifact/query passthrough helpers
// 3) exported desktop bridge selector surface
(function registerPlatformBridge(globalScope) {
  function getWailsBridge() {
    return globalScope.desktopWailsPlatformBridge &&
      typeof globalScope.desktopWailsPlatformBridge.createDesktopApi === 'function'
      ? globalScope.desktopWailsPlatformBridge
      : null;
  }

  function getArtifactInputResolver() {
    // Artifact input helpers are an optional convenience surface. Feature
    // controllers may consume the resolver, but they should not infer platform
    // identity from its presence or absence.
    const bridge = getWailsBridge();
    if (bridge && typeof bridge.getArtifactInputResolver === 'function') {
      return bridge.getArtifactInputResolver();
    }
    return globalScope.desktopArtifactInputResolver || null;
  }

  function createDesktopApi() {
    const bridge = getWailsBridge();
    if (bridge) {
      return bridge.createDesktopApi();
    }
    return null;
  }

  function invokeOptionalQuery(queryFn) {
    const bridge = getWailsBridge();
    if (bridge && typeof bridge.invokeOptionalQuery === 'function') {
      return bridge.invokeOptionalQuery(queryFn);
    }
    return typeof queryFn === 'function' ? queryFn().catch(() => null) : Promise.resolve(null);
  }

  globalScope.desktopPlatformBridge = {
    createDesktopApi,
    invokeOptionalQuery,
    getArtifactInputResolver
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
