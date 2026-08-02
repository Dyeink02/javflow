package organizer

import (
	"sync"
	"testing"
	"time"
)

func TestRunWithProgressHeartbeatEmitsDuringSlowOperation(t *testing.T) {
	var mu sync.Mutex
	entries := make([]ProgressEntry, 0, 2)
	emit := func(entry ProgressEntry) {
		mu.Lock()
		defer mu.Unlock()
		entries = append(entries, entry)
	}

	if err := runWithProgressHeartbeat(
		"模拟网络盘删除",
		`Z:\\slow-folder`,
		ProgressEntry{"phase": progressPhaseFinalizeProgress},
		emit,
		func() error {
			time.Sleep(900 * time.Millisecond)
			return nil
		},
	); err != nil {
		t.Fatalf("runWithProgressHeartbeat returned error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(entries) < 2 {
		t.Fatalf("expected heartbeat and completion events, got %d", len(entries))
	}
	if heartbeat, _ := entries[0]["heartbeat"].(bool); !heartbeat {
		t.Fatalf("first event should be a heartbeat: %#v", entries[0])
	}
	if completed, _ := entries[len(entries)-1]["operationCompleted"].(bool); !completed {
		t.Fatalf("last event should mark operation completion: %#v", entries[len(entries)-1])
	}
}
