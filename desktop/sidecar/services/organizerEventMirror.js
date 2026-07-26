// Shared organizer-side event mirroring helper for compatibility facades.
// deprecated: archived sidecar helper only; marker=archived-sidecar-organizer-event-mirror
// Keep organizer event/log timestamp normalization in one place so sidecar
// organizer and ad-learning compatibility lanes do not drift in payload shape.
//
// Ownership summary:
// 1) normalize compatibility organizer/log event payloads
// 2) timestamp mirrored organizer/ad-learning sidecar events
// 3) keep payload-shape drift out of individual facades
//
// File map for maintainers:
// 1) organizer log packet mirroring
// 2) organizer state packet mirroring
// 3) tiny timed-log constructor used by archived facades

function createOrganizerEventMirror({ eventBus }) {
  // Fast troubleshooting split:
  // 1) organizer/ad-learning mirrored event payload shape issue -> inspect
  //    this helper first
  // 2) packet-level bridge event naming/version issue -> inspect `eventBus.js`
  // 3) underlying organizer/ad-learning business state issue -> inspect the
  //    calling facade/service, not this mirror
  function nowIso() {
    return new Date().toISOString();
  }

  function emitOrganizerLog(entry, extra = {}) {
    // Mirror helpers keep compatibility payload shaping in one file so the
    // organizer and ad-learning facades can stay focused on orchestration.
    eventBus.emitEvent(
      'organizer.log',
      'organizer',
      {
        level: entry.level || 'info',
        message: String(entry.message || ''),
        timestamp: entry.timestamp || nowIso()
      },
      extra
    );
  }

  function emitOrganizerState(payload, extra = {}) {
    // Organizer state events intentionally reuse the organizer domain even when
    // learning flows mirror there for UI continuity in the archived lane.
    eventBus.emitEvent(
      'organizer.state',
      'organizer',
      {
        ...payload,
        timestamp: payload.timestamp || nowIso()
      },
      extra
    );
  }

  function createTimedOrganizerLog(level, message, timestamp = nowIso()) {
    // Provide a tiny shared constructor instead of letting each facade hand-roll
    // timestamp injection and drift in field shape.
    return {
      level,
      message,
      timestamp
    };
  }

  return {
    emitOrganizerLog,
    emitOrganizerState,
    createTimedOrganizerLog
  };
}

module.exports = {
  createOrganizerEventMirror
};
