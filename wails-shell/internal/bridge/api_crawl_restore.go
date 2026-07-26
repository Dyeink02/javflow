package bridge

import (
	"encoding/json"

	"javflow/internal/crawltaskstate"
)

// Restored state decoding is kept as a dedicated adapter because payload shape
// drift between UI/runtime versions should be debugged separately from runner
// construction.
//
// Ownership summary:
// 1) decode restored task-state payloads back into typed restore structures
// 2) isolate payload-shape compatibility concerns from runner construction
// 3) centralize restore payload adaptation in one bridge helper
//
// File map for maintainers:
// 1) restored payload decode helper
func restoredTaskStateValue(value any) *crawltaskstate.RestoredState {
	if value == nil {
		return nil
	}
	if typed, ok := value.(*crawltaskstate.RestoredState); ok {
		return typed
	}
	if typed, ok := value.(crawltaskstate.RestoredState); ok {
		return &typed
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var restored crawltaskstate.RestoredState
	if err := json.Unmarshal(payload, &restored); err != nil || !restored.ShouldRestore {
		return nil
	}
	return &restored
}
