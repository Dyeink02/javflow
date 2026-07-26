// Package observer provides a generic event-observer helper shared by several
// read-model packages.
//
// Ownership summary:
// 1) provide reusable observer plumbing for event-driven read models
// 2) centralize JSON decode + signature dedupe + updater invocation
// 3) keep observer boilerplate out of individual read-model packages
//
// File map for maintainers:
// 1) generic observer/updater interfaces
// 2) constructor and event intake flow
// 3) JSON decode, dedupe, and publish helpers
package observer

import (
	"encoding/json"
	"sync"

	"javflow/internal/events"
)

// Updater T 的具体行为由调用方通过 Updater 接口定义。
// 当收到新事件时调用 Update(data)，返回可选的输出 payload（序列化为 JSON 后 emit）。
type Updater[T any] interface {
	Update(data T) (any, error)
}

// Observer 泛型观察者：从原始 json.RawMessage 反序列化、计算签名、去重后调用 Updater。
type Observer[T any] struct {
	bus           *events.Bus
	updater       Updater[T]
	mu            sync.Mutex
	lastSignature string
}

// NewObserver 创建一个新的泛型观察者。
// signatureFn 用于生成去重签名，返回空字符串表示"始终触发"。
func NewObserver[T any](
	bus *events.Bus,
	updater Updater[T],
) *Observer[T] {
	return &Observer[T]{
		bus:     bus,
		updater: updater,
	}
}

// ObserveEvent 接收事件总线回调，反序列化 payload → 调用 Updater → 如果成功且返回非空则通过 bus.Publish 推送。
// identityFn 用于计算事件的签名（如 eventName + 关键字段组合），相同签名在连续事件中只处理一次。
func (o *Observer[T]) ObserveEvent(eventName string, identityFn func(T) string, rawData json.RawMessage) {
	var data T
	if len(rawData) > 0 && string(rawData) != "null" {
		if err := json.Unmarshal(rawData, &data); err != nil {
			return
		}
	}

	signature := ""
	if identityFn != nil {
		signature = identityFn(data)
	}

	o.mu.Lock()
	if signature != "" && signature == o.lastSignature {
		o.mu.Unlock()
		return
	}
	o.mu.Unlock()

	result, err := o.updater.Update(data)
	if err != nil || result == nil {
		return
	}

	o.mu.Lock()
	o.lastSignature = signature
	o.mu.Unlock()

	output, _ := json.Marshal(result)
	o.bus.Publish("", "state", eventName, "", "", "", "", output)
}

// ResetSignature 清除去重签名缓存，用于任务切换时强制刷新。
func (o *Observer[T]) ResetSignature() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.lastSignature = ""
}
