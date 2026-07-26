package crawlreview

import "testing"

func TestFromPayloadNormalizesReviewPanelItems(t *testing.T) {
	service := NewService()

	panel := service.FromPayload(map[string]any{
		"status":              "running",
		"message":             "processing",
		"duplicateItems":      []any{"ABP-001", "ABP-001", " ABP-002 ", "", "ABP-003"},
		"duplicateItemsTotal": float64(9),
		"unfinishedItems":     []any{"IPZZ-001", "IPZZ-001", " IPZZ-002 "},
		"missingItemsTotal":   float64(6),
		"pageGapItems":        []any{"第 1 页缺口 3 条", "第 1 页缺口 3 条", "第 2 页缺口 1 条"},
	})

	if panel.DuplicateItemsTotal != 9 {
		t.Fatalf("expected duplicate total 9, got %d", panel.DuplicateItemsTotal)
	}
	if len(panel.DuplicateItems) != 3 {
		t.Fatalf("expected 3 normalized duplicate items, got %d", len(panel.DuplicateItems))
	}
	if panel.DuplicateItems[0] != "ABP-001" || panel.DuplicateItems[1] != "ABP-002" || panel.DuplicateItems[2] != "ABP-003" {
		t.Fatalf("unexpected duplicate items: %#v", panel.DuplicateItems)
	}

	if panel.UnfinishedItemsTotal != 6 {
		t.Fatalf("expected unfinished total 6, got %d", panel.UnfinishedItemsTotal)
	}
	if len(panel.UnfinishedItems) != 2 {
		t.Fatalf("expected 2 normalized unfinished items, got %d", len(panel.UnfinishedItems))
	}

	if panel.PageGapItemsTotal != 3 {
		t.Fatalf("expected page gap total fallback to source length 3, got %d", panel.PageGapItemsTotal)
	}
	if len(panel.PageGapItems) != 2 {
		t.Fatalf("expected 2 normalized page gap items, got %d", len(panel.PageGapItems))
	}
}

func TestFromPayloadNormalizesFailedDetails(t *testing.T) {
	service := NewService()

	panel := service.FromPayload(map[string]any{
		"failedDetailsTotal": float64(5),
		"failedDetails": []any{
			map[string]any{
				"item":         "ABP-001",
				"sourceLink":   "https://example.com/1",
				"reason":       "timeout",
				"category":     "network",
				"retryCount":   float64(2),
				"retryAdvice":  "稍后重试",
				"recoverable":  false,
				"lastFailedAt": "2026-04-30T18:00:00Z",
			},
			map[string]any{
				"item":       "ABP-001",
				"sourceLink": "https://example.com/1",
				"reason":     "timeout",
			},
			map[string]any{
				"item":        "ABP-002",
				"sourceLink":  "https://example.com/2",
				"reason":      "empty response",
				"recoverable": true,
			},
		},
	})

	if panel.FailedDetailsTotal != 5 {
		t.Fatalf("expected failed total 5, got %d", panel.FailedDetailsTotal)
	}
	if len(panel.FailedDetails) != 2 {
		t.Fatalf("expected 2 normalized failed details, got %d", len(panel.FailedDetails))
	}
	if panel.FailedDetails[0].RetryCount != 2 {
		t.Fatalf("expected retry count 2, got %d", panel.FailedDetails[0].RetryCount)
	}
	if panel.FailedDetails[0].Recoverable == nil || *panel.FailedDetails[0].Recoverable != false {
		t.Fatalf("expected first item recoverable=false, got %#v", panel.FailedDetails[0].Recoverable)
	}
	if panel.FailedDetails[1].Recoverable == nil || *panel.FailedDetails[1].Recoverable != true {
		t.Fatalf("expected second item recoverable=true, got %#v", panel.FailedDetails[1].Recoverable)
	}
}

func TestFromPayloadReadsFilteredItemsFromNestedStats(t *testing.T) {
	service := NewService()

	panel := service.FromPayload(map[string]any{
		"status":  "running",
		"message": "processing",
		"stats": map[string]any{
			"filteredItemIds":        []any{"DAZD-277", " PBD-512 ", "DAZD-277", "MKCK-407"},
			"filteredByActressCount": float64(3),
		},
	})

	if panel.FilteredItemsTotal != 3 {
		t.Fatalf("expected filtered total 3, got %d", panel.FilteredItemsTotal)
	}
	if len(panel.FilteredItems) != 3 {
		t.Fatalf("expected 3 normalized filtered items, got %d", len(panel.FilteredItems))
	}
	if panel.FilteredItems[0] != "DAZD-277" || panel.FilteredItems[1] != "PBD-512" || panel.FilteredItems[2] != "MKCK-407" {
		t.Fatalf("unexpected filtered items: %#v", panel.FilteredItems)
	}
}

func TestApplyPayloadPersistsLatestReviewPanel(t *testing.T) {
	service := NewService()

	applied := service.ApplyPayload(map[string]any{
		"status":  "completed",
		"message": "done",
		"stats": map[string]any{
			"filteredItemIds":        []any{"DAZD-277", "PBD-512"},
			"filteredByActressCount": float64(2),
		},
		"unfinishedItems": []any{"ABC-001"},
	})

	if applied.FilteredItemsTotal != 2 {
		t.Fatalf("expected applied filtered total 2, got %d", applied.FilteredItemsTotal)
	}

	built := service.Build()
	if built.Status != "completed" {
		t.Fatalf("expected persisted status completed, got %q", built.Status)
	}
	if built.FilteredItemsTotal != 2 {
		t.Fatalf("expected persisted filtered total 2, got %d", built.FilteredItemsTotal)
	}
	if len(built.FilteredItems) != 2 || built.FilteredItems[0] != "DAZD-277" || built.FilteredItems[1] != "PBD-512" {
		t.Fatalf("unexpected persisted filtered items: %#v", built.FilteredItems)
	}
	if len(built.UnfinishedItems) != 1 || built.UnfinishedItems[0] != "ABC-001" {
		t.Fatalf("unexpected persisted unfinished items: %#v", built.UnfinishedItems)
	}
}
