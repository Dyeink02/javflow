package crawlresult

import (
	"encoding/json"
	"sync"
)

// Result observer owns deduplicated event emission for the renderer-facing
// crawl result panel.
//
// Ownership summary:
// 1) observe crawl state and emit deduplicated result-panel updates
// 2) rebuild result-panel projections in Go before emitter fanout
// 3) keep result observer boilerplate separate from result service logic
//
// File map for maintainers:
// 1) result observer struct and constructor
// 2) crawl event intake and dedupe guard
// 3) renderer emission helpers for result panel updates
//
// Boundary rule:
// observer code should stay on "observe -> rebuild -> dedupe -> emit". If
// result business rules need to change, move that work into Service instead of
// growing branch logic here.

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
