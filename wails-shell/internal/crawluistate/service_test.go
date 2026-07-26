package crawluistate

import "testing"

func TestFromPayloadNormalizesUiState(t *testing.T) {
	service := NewService()

	state := service.FromPayload(map[string]any{
		"status":           "running",
		"message":          "processing",
		"outputDir":        "C:\\test\\run-001",
		"activeItems":      []any{"ABP-001", " ABP-001 ", "", "ABP-002", "ABP-003"},
		"activeItemsTotal": float64(8),
		"stats": map[string]any{
			"queued":    float64(10),
			"attempted": float64(6),
			"completed": float64(4),
			"pageIndex": float64(2),
		},
	})

	if state.Status != "running" {
		t.Fatalf("expected status running, got %q", state.Status)
	}
	if state.OutputDir != "C:\\test\\run-001" {
		t.Fatalf("expected output dir, got %q", state.OutputDir)
	}
	if state.ActiveItemsTotal != 8 {
		t.Fatalf("expected active total 8, got %d", state.ActiveItemsTotal)
	}
	if len(state.ActiveItems) != 3 {
		t.Fatalf("expected 3 active items, got %d", len(state.ActiveItems))
	}
	if state.ActiveItems[0] != "ABP-001" || state.ActiveItems[1] != "ABP-002" || state.ActiveItems[2] != "ABP-003" {
		t.Fatalf("unexpected active items: %#v", state.ActiveItems)
	}
	if state.Stats.Queued != 10 || state.Stats.Attempted != 6 || state.Stats.Completed != 4 || state.Stats.PageIndex != 2 {
		t.Fatalf("unexpected stats: %#v", state.Stats)
	}
}

func TestFromPayloadUsesFallbackOutputDirAndCount(t *testing.T) {
	service := NewService()

	state := service.FromPayload(map[string]any{
		"status":               "completed",
		"currentTaskOutputDir": "C:\\test\\run-002",
		"activeItems":          []any{"HMN-001"},
	})

	if state.OutputDir != "C:\\test\\run-002" {
		t.Fatalf("expected fallback output dir, got %q", state.OutputDir)
	}
	if state.ActiveItemsTotal != 1 {
		t.Fatalf("expected active total fallback to 1, got %d", state.ActiveItemsTotal)
	}
}
