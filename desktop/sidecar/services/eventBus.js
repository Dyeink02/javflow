// Sidecar event bus that translates local service signals into the shared
// bridge protocol packet format. Keep event names centralized here so
// compatibility code does not spread raw protocol literals everywhere.
// compatibility-owner: active crawl-compatible protocol adapter; marker=compat-sidecar-event-bus
//
// This file is protocol plumbing for the Node compatibility lane. It should
// not become a second source of product state or message wording.
//
// Ownership summary:
// 1) encode sidecar lifecycle/domain signals into bridge protocol packets
// 2) centralize packet metadata such as version, timestamp, domain, and taskId
// 3) keep protocol shaping out of sidecar business facades
//
// File map for maintainers:
// 1) bridge packet emit helpers
// 2) lifecycle/log/state packet adapters
// 3) exported sidecar event-bus surface
const { BRIDGE_VERSION, BRIDGE_EVENTS } = require('../../common/bridgeProtocol.js');

function createEventBus({ writePacket }) {
  // Fast troubleshooting split:
  // 1) sidecar event packet shape/version/domain issue -> inspect this helper
  // 2) sidecar business facade chose the wrong event payload -> inspect the
  //    calling facade/service
  // 3) Go bridge event contract issue without sidecar -> inspect the Go bridge
  //    event publishers instead
  function nowIso() {
    return new Date().toISOString();
  }

  function emitPacket(packet) {
    if (typeof writePacket !== 'function') {
      return;
    }

    writePacket({
      version: BRIDGE_VERSION,
      kind: 'event',
      timestamp: nowIso(),
      ...packet
    });
  }

  function emitEvent(event, domain, data, extra = {}) {
    // Packet shaping stays centralized here so sidecar facades can speak in
    // domain terms without each re-encoding protocol version/timestamp/taskId.
    emitPacket({
      event,
      domain,
      action: String(extra.action || ''),
      taskId: String(extra.taskId || ''),
      data: data && typeof data === 'object' ? data : {}
    });
  }

  function emitLifecycle(state, message, extra = {}) {
    emitEvent(
      BRIDGE_EVENTS.sidecarLifecycle,
      'system',
      {
        state,
        pid: process.pid,
        message: String(message || '')
      },
      extra
    );
  }

  function emitLog(scope, entry = {}, extra = {}) {
    // Log routing is intentionally scope-driven. Keep event-name selection here
    // so compatibility callers do not spread bridge protocol literals around.
    const eventName =
      scope === 'crawl'
        ? BRIDGE_EVENTS.crawlLog
        : scope === 'learning'
          ? BRIDGE_EVENTS.learningLog
          : BRIDGE_EVENTS.organizerLog;
    const domain = scope === 'crawl' ? 'crawl' : scope === 'learning' ? 'learning' : 'organizer';

    emitEvent(
      eventName,
      domain,
      {
        level: String(entry.level || 'info'),
        message: String(entry.message || ''),
        timestamp: entry.timestamp || nowIso()
      },
      extra
    );
  }

  function emitState(scope, payload = {}, extra = {}) {
    // State packets follow the same rule as log packets: normalize timestamp
    // and event name here, not in each sidecar-facing service.
    const eventName =
      scope === 'crawl'
        ? BRIDGE_EVENTS.crawlState
        : scope === 'learning'
          ? BRIDGE_EVENTS.learningState
          : BRIDGE_EVENTS.organizerState;
    const domain = scope === 'crawl' ? 'crawl' : scope === 'learning' ? 'learning' : 'organizer';

    emitEvent(
      eventName,
      domain,
      {
        ...payload,
        timestamp: payload.timestamp || nowIso()
      },
      extra
    );
  }

  function emitLogContext(payload = {}, extra = {}) {
    emitEvent(BRIDGE_EVENTS.crawlLogContext, 'crawl', payload, extra);
  }

  return {
    emitEvent,
    emitLifecycle,
    emitLog,
    emitState,
    emitLogContext
  };
}

module.exports = {
  createEventBus
};
