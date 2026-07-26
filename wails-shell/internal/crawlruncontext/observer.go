package crawlruncontext

import (
	"encoding/json"
	"sync"
)

// Run-context observer owns deduplicated event emission for crawl artifact path
// context and output-directory state.
//
// Ownership summary:
// 1) observe crawl state and emit deduplicated run-context updates
// 2) rebuild crawl artifact path context in Go before emitter fanout
// 3) keep run-context observer boilerplate separate from run-context service logic
//
// File map for maintainers:
// 1) run-context observer struct and constructor
// 2) crawl event intake and dedupe guard
// 3) artifact path context emission helpers
//
// Boundary rule:
// path/artifact inference belongs in Service. The observer should stay focused
// on event selection and deduplicated emission only.

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

	switch eventName {
	case "crawl.state", "crawl.log-context":
	default:
		return
	}

	context := o.service.Build()
	signature := o.service.Signature(context)

	o.mu.Lock()
	if signature == o.lastSignature {
		o.mu.Unlock()
		return
	}
	o.lastSignature = signature
	o.mu.Unlock()

	o.emitter.Emit(EventName, context)
}
