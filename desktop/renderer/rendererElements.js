// Renderer elements factory owns the DOM-id registry for the active desktop
// shell. Keeping it separate from renderer.js prevents the main bootstrap file
// from growing into another mixed wiring + element-definition hotspot.
//
// Boundary rule:
// this file exports a flat element bag for convenience, but behavior ownership
// still belongs to the domain controllers and view helpers that consume it.
//
// Ownership summary:
// 1) aggregate domain-scoped DOM lookups into one renderer element bag
// 2) keep DOM-id ownership centralized outside renderer bootstrap
// 3) prevent controllers from hand-rolling their own ad hoc element queries
//
// File map for maintainers:
// 1) renderer-element domain dependency guard
// 2) flat renderer element collection helper
(function initializeRendererElements(globalScope) {
  const elementDomains = globalScope.desktopRendererElementDomains || null;

  if (!elementDomains) {
    throw new Error('desktopRendererElementDomains is required before rendererElements');
  }

  function collectRendererElements(documentScope) {
    const scope = documentScope || document;

    // Element collection is intentionally flat for consumers, but ownership is
    // still segmented by domain in rendererElementDomains.js. If a future
    // module adds nodes, extend the owning domain helper first rather than
    // growing ad hoc lookups in controllers.
    return Object.assign(
      {},
      elementDomains.collectShellElements(scope),
	      elementDomains.collectActressAtlasElements(scope),
      elementDomains.collectCrawlerElements(scope),
      elementDomains.collectOrganizerElements(scope),
      elementDomains.collectSubscriptionElements(scope),
      elementDomains.collectLibraryMetadataElements(scope)
    );
  }

  globalScope.desktopRendererElements = {
    collectRendererElements
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
