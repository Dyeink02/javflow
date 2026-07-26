package crawlreview

import (
	"encoding/json"
	"sync"

	"javflow/internal/events"
)

// Review observer owns deduplicated event emission for duplicate/unfinished/
// filtered review data.
//
// Ownership summary:
// 1) observe crawl-state updates and rebuild the review panel in Go
// 2) deduplicate review-panel emissions via signature tracking
// 3) keep review observer boilerplate separate from review service logic
//
// File map for maintainers:
// 1) review observer struct and constructor
// 2) crawl state event intake and dedupe guard
// 3) review panel emission helpers
//
// Boundary rule:
// review categorization belongs in Service. This observer should remain a thin
// "observe -> rebuild -> dedupe -> emit" adapter.

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

// ObserveEvent listens for crawl.state updates, rebuilds the normalized review
// panel once in Go, and only emits when the panel meaningfully changed. This
// keeps renderer updates stable even when the runner sends frequent state ticks.
func (o *Observer) ObserveEvent(eventName string, rawData json.RawMessage) {
	if o == nil || o.service == nil || o.bus == nil {
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

	o.bus.Emit(EventName, panel)
}
