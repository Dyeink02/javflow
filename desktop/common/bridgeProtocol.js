// Shared bridge protocol constants used by both renderer and compatibility
// runtimes. This file is transport-agnostic: it describes event/command
// envelopes, not whether the current desktop runtime is Wails or a legacy path.
//
// Ownership summary:
// 1) define transport-neutral bridge protocol constants and packet helpers
// 2) keep renderer and sidecar on one shared envelope contract
// 3) avoid duplicating protocol literals across runtimes
//
// File map for maintainers:
// 1) shared bridge event constants
// 2) command envelope helper
// 3) event packet normalization helper
(function registerBridgeProtocol(globalScope) {
  const BRIDGE_VERSION = 'bridge.v1';

  const BRIDGE_EVENTS = Object.freeze({
    generic: 'bridge:event',
    sidecarLifecycle: 'sidecar.lifecycle',
    crawlLog: 'crawl.log',
    crawlState: 'crawl.state',
    crawlUiState: 'crawl.ui-state',
    crawlStagePanel: 'crawl.stage-panel',
    crawlResultPanel: 'crawl.result-panel',
    crawlRunContext: 'crawl.run-context',
    crawlReviewPanel: 'crawl.review-panel',
    crawlQualitySummary: 'crawl.quality-summary',
    crawlLogContext: 'crawl.log-context',
    organizerLog: 'organizer.log',
    organizerState: 'organizer.state',
    learningLog: 'learning.log',
    learningState: 'learning.state',
    appNotice: 'app.notice'
  });

  function isPlainObject(value) {
    return value != null && typeof value === 'object' && !Array.isArray(value);
  }

  // createCommandEnvelope and normalizeEventPacket are the transport-neutral
  // protocol boundary. Wails and compatibility runtimes should adapt to this
  // schema rather than inventing parallel packet shapes in controllers.
  function createCommandEnvelope(command, payload) {
    // Command envelopes should stay minimal and transport-neutral. Domain
    // defaults belong in the caller or bridge handler, not in this protocol
    // helper.
    return {
      version: BRIDGE_VERSION,
      command: String(command || '').trim(),
      payload: isPlainObject(payload) ? payload : payload == null ? {} : { value: payload }
    };
  }

  function normalizeEventPacket(packet) {
    // Event normalization is intentionally tolerant so renderer listeners can
    // consume both Wails-bridge and compatibility-shaped payloads through one
    // schema without duplicating guards in each controller.
    if (!isPlainObject(packet)) {
      return {
        version: BRIDGE_VERSION,
        kind: 'event',
        event: '',
        domain: '',
        action: '',
        taskId: '',
        timestamp: new Date().toISOString(),
        data: {}
      };
    }

    return {
      version: String(packet.version || BRIDGE_VERSION),
      kind: 'event',
      event: String(packet.event || ''),
      domain: String(packet.domain || ''),
      action: String(packet.action || ''),
      taskId: String(packet.taskId || ''),
      timestamp: packet.timestamp || new Date().toISOString(),
      data: isPlainObject(packet.data) ? packet.data : {}
    };
  }

  const payload = {
    BRIDGE_VERSION,
    BRIDGE_EVENTS,
    createCommandEnvelope,
    normalizeEventPacket
  };

  globalScope.desktopBridgeProtocol = payload;

  if (typeof module !== 'undefined' && module.exports) {
    module.exports = payload;
  }
})(typeof globalThis !== 'undefined' ? globalThis : window);
