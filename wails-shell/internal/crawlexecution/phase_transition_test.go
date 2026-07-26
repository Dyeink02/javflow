package crawlexecution

import (
	"reflect"
	"testing"
)

func TestNormalizePhaseTransitionPlan(t *testing.T) {
	normalized := NormalizePhaseTransitionPlan(PhaseTransitionPlan{
		PhaseKeys: []string{
			"queue_setup",
			"index_discovery",
			"index_discovery",
			"queue_drain",
			"detail_recovery",
		},
		NextPhaseByKey: map[string]string{
			"queue_drain":     "detail_recovery",
			"detail_recovery": "unknown",
			"unknown":         "final_drain",
		},
		StopRedirectPhaseKey: "queue_drain",
	})

	expectedPhaseKeys := []string{
		"boot",
		"queue_setup",
		"index_discovery",
		"queue_drain",
		"detail_recovery",
		"final_drain",
	}
	if !reflect.DeepEqual(normalized.PhaseKeys, expectedPhaseKeys) {
		t.Fatalf("unexpected phase keys: %#v", normalized.PhaseKeys)
	}

	expectedNextPhaseByKey := map[string]string{
		"boot":            "queue_setup",
		"queue_setup":     "index_discovery",
		"index_discovery": "queue_drain",
		"queue_drain":     "detail_recovery",
		"detail_recovery": "final_drain",
	}
	if !reflect.DeepEqual(normalized.NextPhaseByKey, expectedNextPhaseByKey) {
		t.Fatalf("unexpected next phase map: %#v", normalized.NextPhaseByKey)
	}

	if normalized.InitialPhaseKey != "boot" {
		t.Fatalf("expected initial phase boot, got %q", normalized.InitialPhaseKey)
	}
	if normalized.FinalPhaseKey != "final_drain" {
		t.Fatalf("expected final phase final_drain, got %q", normalized.FinalPhaseKey)
	}
	if normalized.StopRedirectPhaseKey != "queue_drain" {
		t.Fatalf("expected stop redirect phase queue_drain, got %q", normalized.StopRedirectPhaseKey)
	}
}

func TestResolvePhaseTransitionNext(t *testing.T) {
	plan := PhaseTransitionPlan{
		PhaseKeys: []string{
			"boot",
			"queue_setup",
			"index_discovery",
			"queue_drain",
			"detail_recovery",
			"final_drain",
		},
		NextPhaseByKey: map[string]string{
			"queue_drain": "detail_recovery",
		},
		StopRedirectPhaseKey: "queue_drain",
	}

	if next := ResolvePhaseTransitionNext(plan, "queue_drain", "page_gap_recovery", false); next != "detail_recovery" {
		t.Fatalf("expected queue_drain -> detail_recovery, got %q", next)
	}
	if next := ResolvePhaseTransitionNext(plan, "missing", "final_drain", false); next != "final_drain" {
		t.Fatalf("expected fallback final_drain, got %q", next)
	}
	if next := ResolvePhaseTransitionNext(plan, "index_discovery", "", true); next != "queue_drain" {
		t.Fatalf("expected stop redirect queue_drain, got %q", next)
	}
}

func TestResolveStructuredPhaseKey(t *testing.T) {
	plan := PhaseTransitionPlan{
		PhaseKeys: []string{
			"boot",
			"queue_setup",
			"index_discovery",
			"queue_drain",
			"final_drain",
		},
		StopRedirectPhaseKey: "queue_drain",
	}

	if key := ResolveStructuredPhaseKey(plan, "index_discovery", "running"); key != "index_discovery" {
		t.Fatalf("expected current phase index_discovery, got %q", key)
	}
	if key := ResolveStructuredPhaseKey(plan, "", "stopping"); key != "queue_drain" {
		t.Fatalf("expected stopping phase queue_drain, got %q", key)
	}
	if key := ResolveStructuredPhaseKey(plan, "", "completed"); key != "final_drain" {
		t.Fatalf("expected completed phase final_drain, got %q", key)
	}
	if key := ResolveStructuredPhaseKey(plan, "", "running"); key != "boot" {
		t.Fatalf("expected running phase boot, got %q", key)
	}
}
