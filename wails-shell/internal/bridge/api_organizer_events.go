package bridge

import (
	"time"

	"javflow/internal/organizer"
)

// Organizer event emission is isolated so future log/state format changes stay
// away from bridge bootstrap and dispatch concerns.
//
// Ownership summary:
// 1) centralize organizer log/state event emission onto the shared bus
// 2) keep organizer runtime event envelopes stable for UI consumers
// 3) separate organizer event wiring from organizer service logic
//
// File map for maintainers:
// 1) organizer log event emitter
// 2) organizer state/progress event emitter

// emitOrganizerLog maps organizer log entries onto the shared bus contract
// without leaking bus details back into organizer service code.
func (a *API) emitOrganizerLog(entry organizer.LogEntry, taskID string) {
	a.runtime.bus.Publish(
		"1",
		"event",
		"organizer.log",
		"organizer",
		"run",
		taskID,
		entry.Timestamp,
		mustRawJSON(map[string]any{
			"level":     entry.Level,
			"message":   entry.Message,
			"timestamp": entry.Timestamp,
		}),
	)
}

// emitOrganizerState is the single event envelope writer for organizer runtime
// state transitions and progress payloads.
func (a *API) emitOrganizerState(payload map[string]any, taskID string) {
	timestamp := time.Now().Format(time.RFC3339)
	if value := nonEmptyString(payload["timestamp"]); value != "" {
		timestamp = value
	} else {
		payload["timestamp"] = timestamp
	}
	a.runtime.bus.Publish(
		"1",
		"event",
		"organizer.state",
		"organizer",
		"run",
		taskID,
		timestamp,
		mustRawJSON(payload),
	)
}
