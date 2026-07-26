// Organizer learning view owns ad-learning shell decoration and the optional
// mode hint element. The controller decides when AI compatibility UI should be
// shown; the view only mutates DOM structure inside the learning box.
//
// Ownership summary:
// 1) render organizer ad-learning shell decoration and mode-hint DOM
// 2) keep learning-box structural decoration idempotent
// 3) separate organizer learning view DOM from controller state logic
//
// File map for maintainers:
// 1) learning-box query helpers
// 2) mode-hint node creation
// 3) learning-box decoration helpers
(function initializeOrganizerLearningView(globalScope) {
  function getLearningBox() {
    return document.querySelector('#organizer-workspace .organizer-learning-box');
  }

  function ensureModeHintElement() {
    // The mode hint is pure shell decoration. Its wording/state comes from the
    // controller; this view only ensures the node exists in the right place.
    const box = getLearningBox();
    if (!box) {
      return null;
    }

    let hint = box.querySelector('#organizer-ai-mode-hint');
    if (hint) {
      return hint;
    }

    hint = document.createElement('small');
    hint.id = 'organizer-ai-mode-hint';
    hint.className = 'organizer-mode-hint';
    const label = box.querySelector('.message-label');
    if (label && label.parentNode) {
      label.parentNode.insertBefore(hint, label.nextSibling);
    } else {
      box.insertBefore(hint, box.firstChild);
    }
    return hint;
  }

  function decorateLearningBox(elements) {
    // Decoration stays structural and idempotent so repeated compatibility
    // state refreshes do not keep mutating the learning box layout.
    const box = getLearningBox();
    if (!box || box.dataset.aiDecorated === '1') {
      return box;
    }

    const advancedNodes = [
      box.querySelector('.organizer-learning-tip'),
      box.querySelector('.organizer-learning-grid'),
      elements.organizerLearningCodes ? elements.organizerLearningCodes.closest('label.field') : null,
      elements.organizerAdModelType ? elements.organizerAdModelType.closest('label.field') : null,
      box.querySelector('.organizer-learning-actions'),
      box.querySelector('.organizer-learning-guide'),
      elements.organizerLearningSummary
    ].filter(Boolean);

    advancedNodes.forEach((node) => node.classList.add('organizer-learning-advanced'));
    box.dataset.aiDecorated = '1';
    return box;
  }

  globalScope.desktopOrganizerLearningView = {
    decorateLearningBox,
    ensureModeHintElement,
    getLearningBox
  };
})(typeof globalThis !== 'undefined' ? globalThis : window);
