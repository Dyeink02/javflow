package events

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Package events owns the lightweight in-process event bus used by bridge-side
// observers and Wails event emission.
//
// Ownership summary:
// 1) fan out in-process events to observers and the Wails runtime
// 2) centralize lightweight event publication semantics
// 3) keep observer wiring separate from domain service implementations
//
// File map for maintainers:
// 1) bus/observer contracts
// 2) context and observer registration helpers
// 3) event publish/emit fanout helpers

// Bus is the narrow in-process fanout hub between bridge/runtime emitters and
// lightweight observers.
type Bus struct {
	mu        sync.RWMutex
	ctx       context.Context
	observers []Observer
}

// Observer is the read-only callback contract for in-process event listeners.
type Observer func(eventName string, rawData json.RawMessage)

func NewBus() *Bus {
	return &Bus{}
}

func (b *Bus) SetContext(ctx context.Context) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ctx = ctx
}

func (b *Bus) currentContext() context.Context {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.ctx
}

// AddObserver registers one in-process observer. Observers are intentionally
// append-only in the current model so bootstrap wiring stays simple.
func (b *Bus) AddObserver(observer Observer) {
	if observer == nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.observers = append(b.observers, observer)
}

func (b *Bus) currentObservers() []Observer {
	b.mu.RLock()
	defer b.mu.RUnlock()

	result := make([]Observer, len(b.observers))
	copy(result, b.observers)
	return result
}

// Emit is the direct Wails-side event emitter used for simple UI pushes that do
// not need the full bridged envelope.
func (b *Bus) Emit(name string, payload any) {
	ctx := b.currentContext()
	if ctx == nil {
		return
	}
	runtime.EventsEmit(ctx, name, payload)
}

// Publish fans out one event to in-process observers first, then emits the
// bridged Wails payload. Observer failures are isolated so one listener cannot
// break the rest of the chain or the UI push path.
func (b *Bus) Publish(version string, kind string, eventName string, domain string, action string, taskID string, timestamp string, rawData json.RawMessage) {
	for _, observer := range b.currentObservers() {
		func() {
			defer func() {
				if r := recover(); r != nil {
					// 防止单个 observer panic 中断整个事件链。
					// observer 内部错误不应影响其他订阅者和前端推送。
				}
			}()
			observer(eventName, rawData)
		}()
	}

	ctx := b.currentContext()
	if ctx == nil {
		return
	}

	var payload any
	if len(rawData) > 0 && string(rawData) != "null" {
		_ = json.Unmarshal(rawData, &payload)
	}

	runtime.EventsEmit(ctx, "bridge:event", map[string]any{
		"version":   version,
		"kind":      kind,
		"event":     eventName,
		"domain":    domain,
		"action":    action,
		"taskId":    taskID,
		"timestamp": timestamp,
		"data":      payload,
	})

	if eventName != "" {
		runtime.EventsEmit(ctx, eventName, payload)
	}

	if legacyEvent := mapLegacyEvent(eventName); legacyEvent != "" {
		runtime.EventsEmit(ctx, legacyEvent, payload)
	}
}

// mapLegacyEvent keeps historical front-end listeners working while the Go
// bridge owns the canonical event names.
func mapLegacyEvent(eventName string) string {
	switch eventName {
	case "crawl.log":
		return "runner:log"
	case "crawl.state":
		return "runner:state"
	case "crawl.log-context":
		return "runner:log-context"
	case "organizer.log", "learning.log":
		return "organizer:log"
	case "organizer.state", "learning.state":
		return "organizer:state"
	default:
		return ""
	}
}
