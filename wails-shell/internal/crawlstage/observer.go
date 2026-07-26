package crawlstage

import (
	"encoding/json"
	"sync"
)

// Stage observer owns deduplicated event emission for the renderer-facing
// crawl phase/stage panel.
//
// Ownership summary:
// 1) observe crawl state and emit deduplicated stage-panel updates
// 2) rebuild stage-panel projections in Go before emitter fanout
// 3) keep stage observer boilerplate separate from stage service logic
//
// File map for maintainers:
// 1) stage observer struct and constructor
// 2) crawl event intake and dedupe guard
// 3) stage panel emission helpers
//
// Boundary rule:
// stage sequencing/presentation belongs in Service. This observer should stay
// on "observe -> rebuild -> dedupe -> emit" only.

type eventEmitter interface {
	Emit(name string, payload any)
}

type Observer struct {
	service *Service
	emitter eventEmitter

	mu            sync.Mutex
	lastSignature string
}

func NewObserver(service *Service, emitter eventEmitter) *Observer {
	if service == nil || emitter == nil {
		return nil
	}

	return &Observer{
		service: service,
		emitter: emitter,
	}
}

func (o *Observer) ObserveEvent(eventName string, rawData json.RawMessage) {
	if o == nil || o.service == nil || o.emitter == nil {
		return
	}
	if eventName != "crawl.state" {
		return
	}

	panel, ok := o.service.ApplyRawMessage(rawData)
	if !ok {
		return
	}

	signature := o.service.Signature(panel)

	o.mu.Lock()
	if signature == o.lastSignature {
		o.mu.Unlock()
		return
	}
	o.lastSignature = signature
	o.mu.Unlock()

	o.emitter.Emit(EventName, panel)
}
