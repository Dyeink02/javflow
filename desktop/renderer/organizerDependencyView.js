// Organizer dependency view owns shell-only DOM queries inside the dependency
// box. The controller keeps dependency state and actions, while this view hides
// the layout-specific selectors used to decorate the optional compatibility UI.
//
// Ownership summary:
// 1) manage organizer dependency-box DOM lookup and decoration
// 2) keep optional compatibility UI selectors/layout details centralized
// 3) separate organizer dependency view DOM concerns from controller logic
//
// File map for maintainers:
// 1) dependency box query helpers
// 2) advanced-node lookup helpers
// 3) idempotent dependency-box decoration
(function initializeOrganizerDependencyView(globalScope) {
  function getDependencyBox(organizerWorkspace) {
    if (!organizerWorkspace) {
      return null;
    }
    return organizerWorkspace.querySelector('.organizer-dependency-box');
  }

  function getDependencyAdvancedNodes(organizerWorkspace, elements) {
    // Advanced-node discovery is layout-only. The controller decides whether
    // those nodes are active, hidden, or treated as optional compatibility UI.
    const box = getDependencyBox(organizerWorkspace);
    if (!box) {
      return [];
    }

    return [
      elements.organizerRefreshDependencyButton,
      box.querySelector('.organizer-dependency-url-fields'),
      box.querySelector('.organizer-dependency-actions')
    ].filter(Boolean);
  }

  function decorateDependencyBox(organizerWorkspace, elements) {
    // Decoration should stay idempotent so repeated state refreshes never
    // rebuild or duplicate compatibility-only CSS hooks.
    const box = getDependencyBox(organizerWorkspace);
    if (!box || box.dataset.compatDecorated === '1') {
      return box;
    }

    getDependencyAdvancedNodes(organizerWorkspace, elements).forEach((node) =>
      node.classList.add('organizer-dependency-advanced')
    );
    box.dataset.compatDecorated = '1';
    return box;
  }

  globalScope.desktopOrganizerDependencyView = {
    decorateDependencyBox,
    getDependencyAdvancedNodes,
    getDependencyBox
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
