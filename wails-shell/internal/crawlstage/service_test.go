package crawlstage

import (
	"encoding/json"
	"testing"
)

func TestApplyPayloadUsesStructuredPhase(t *testing.T) {
	service := NewService()

	panel := service.ApplyPayload(map[string]any{
		"status":        "running",
		"message":       "当前阶段：抓取索引页。",
		"phaseKey":      "index_discovery",
		"phasePlanKeys": []any{"boot", "queue_setup", "index_discovery", "queue_drain", "final_drain"},
		"outputDir":     `C:\crawl\run-001`,
		"stats": map[string]any{
			"queued":    float64(20),
			"attempted": float64(12),
			"completed": float64(8),
			"pageIndex": float64(4),
		},
	})

	if panel.PhaseKey != "index_discovery" {
		t.Fatalf("expected index_discovery, got %q", panel.PhaseKey)
	}
	if panel.PhaseTitle != "抓取索引页" {
		t.Fatalf("unexpected phase title: %q", panel.PhaseTitle)
	}
	if panel.PhaseIndex != 3 {
		t.Fatalf("expected phase index 3, got %d", panel.PhaseIndex)
	}
	if panel.PhaseTotal != 5 {
		t.Fatalf("expected phase total 5, got %d", panel.PhaseTotal)
	}
	if panel.OutputDir != `C:\crawl\run-001` {
		t.Fatalf("unexpected output dir: %q", panel.OutputDir)
	}
	if panel.Stats.Completed != 8 {
		t.Fatalf("unexpected stats: %#v", panel.Stats)
	}
}

func TestApplyPayloadBuildsFinalStage(t *testing.T) {
	service := NewService()
	service.ApplyPayload(map[string]any{
		"status":   "running",
		"message":  "当前阶段：结果二次校验。",
		"phaseKey": "second_validation",
	})

	panel := service.ApplyPayload(map[string]any{
		"status":  "completed",
		"message": "抓取任务已完成，已二次校验完成。",
	})

	if !panel.IsFinal {
		t.Fatalf("expected final panel")
	}
	if panel.PhaseTitle != "抓取完成" {
		t.Fatalf("unexpected final title: %q", panel.PhaseTitle)
	}
	if panel.PhaseKey != "final_drain" {
		t.Fatalf("expected final_drain, got %q", panel.PhaseKey)
	}
}

func TestApplyPayloadInfersRecoveryPhaseFromMessage(t *testing.T) {
	service := NewService()

	panel := service.ApplyPayload(map[string]any{
		"status":  "running",
		"message": "开始补查第 12 页分页缺口，当前 18/30。",
	})

	if panel.PhaseKey != "page_gap_recovery" {
		t.Fatalf("expected page_gap_recovery, got %q", panel.PhaseKey)
	}
}

func TestObserverEmitsOnlyWhenStageChanges(t *testing.T) {
	service := NewService()
	emitter := &stubEmitter{}
	observer := NewObserver(service, emitter)

	rawPayload := mustRawMessage(map[string]any{
		"status":   "running",
		"message":  "当前阶段：初始化任务队列。",
		"phaseKey": "queue_setup",
	})

	observer.ObserveEvent("crawl.state", rawPayload)
	observer.ObserveEvent("crawl.state", rawPayload)

	if len(emitter.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(emitter.events))
	}
	if emitter.events[0].name != EventName {
		t.Fatalf("unexpected event name: %q", emitter.events[0].name)
	}
}

type stubEmitter struct {
	events []capturedEvent
}

type capturedEvent struct {
	name    string
	payload any
}

func (s *stubEmitter) Emit(name string, payload any) {
	s.events = append(s.events, capturedEvent{name: name, payload: payload})
}

func mustRawMessage(payload any) json.RawMessage {
	encoded, _ := json.Marshal(payload)
	return encoded
}
