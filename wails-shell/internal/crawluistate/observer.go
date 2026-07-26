package crawluistate

import (
	"encoding/json"
	"sync"

	"javflow/internal/events"
)

// UI-state observer owns deduplicated event emission for the renderer's live
// crawl state model.
//
// Ownership summary:
// 1) observe crawl state and emit deduplicated UI-state updates
// 2) rebuild UI-state projections in Go before bus fanout
// 3) keep UI-state observer boilerplate separate from ui-state service logic
//
// File map for maintainers:
// 1) UI-state observer struct and constructor
// 2) crawl state event intake and dedupe guard
// 3) renderer bus emission helpers for UI-state updates
//
// Boundary rule:
// UI-state shaping belongs in Service. This observer should remain a thin
// deduplicated emitter instead of turning into another state reducer.

type Observer struct {
	service *Service
	bus     *events.Bus

	mu            sync.Mutex
	lastSignature string
}

func NewObserver(service *Service, bus *events.Bus) *Observer {
	if service == nil || bus == nil {
		return nil
	}

	return &Observer{
		service: service,
		bus:     bus,
	}
}

func (o *Observer) ObserveEvent(eventName string, rawData json.RawMessage) {
	if o == nil || o.service == nil || o.bus == nil {
		return
	}
	if eventName != "crawl.state" {
		return
	}

	state, ok := o.service.FromRawMessage(rawData)
	if !ok {
		return
	}

	signature := o.service.Signature(state)

	o.mu.Lock()
	if signature == o.lastSignature {
		o.mu.Unlock()
		return
	}
	o.lastSignature = signature
	o.mu.Unlock()

	o.bus.Emit(EventName, state)
}
