package crawlexecution

import "testing"

func TestFindKeyByLabel(t *testing.T) {
	if key := FindKeyByLabel("抓取索引页"); key != "index_discovery" {
		t.Fatalf("expected index_discovery, got %q", key)
	}
}

func TestInferKeyFromMessage(t *testing.T) {
	if key := InferKeyFromMessage("开始补查第 12 页分页缺口，当前 18/30。"); key != "page_gap_recovery" {
		t.Fatalf("expected page_gap_recovery, got %q", key)
	}
}

func TestFinalTitleForStatus(t *testing.T) {
	if title := FinalTitleForStatus("completed"); title != "抓取完成" {
		t.Fatalf("unexpected final title: %q", title)
	}
}

func TestNormalizePhaseKeys(t *testing.T) {
	normalized := NormalizePhaseKeys([]string{
		"queue_setup",
		"index_discovery",
		"index_discovery",
		"final_drain",
		"unknown",
	})

	expected := []string{
		"boot",
		"queue_setup",
		"index_discovery",
		"final_drain",
	}

	if len(normalized) != len(expected) {
		t.Fatalf("unexpected normalized count: %#v", normalized)
	}
	for index, key := range expected {
		if normalized[index] != key {
			t.Fatalf("expected %q at index %d, got %#v", key, index, normalized)
		}
	}
}

func TestFindPlanIndex(t *testing.T) {
	planKeys := []string{
		"boot",
		"queue_setup",
		"index_discovery",
		"final_drain",
	}

	if index := FindPlanIndex(planKeys, "index_discovery"); index != 3 {
		t.Fatalf("expected plan index 3, got %d", index)
	}
	if index := FindPlanIndex(planKeys, "detail_recovery"); index != 0 {
		t.Fatalf("expected missing phase index 0, got %d", index)
	}
}
