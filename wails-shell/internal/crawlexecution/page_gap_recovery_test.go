package crawlexecution

import "testing"

func TestResolvePageGapPassStart(t *testing.T) {
	decision := ResolvePageGapPassStart(0, 2)
	if decision.ShouldRunPass {
		t.Fatalf("expected pass to stop when no pending audits")
	}
	if !decision.StopRecovery {
		t.Fatalf("expected recovery to stop")
	}
	if decision.LogMessage == "" {
		t.Fatalf("expected completion log message")
	}
}

func TestResolvePageGapAuditFollowUp(t *testing.T) {
	decision := ResolvePageGapAuditFollowUp(4, 30, 30, 2, true)
	if decision.Action != "enqueue_new_links" {
		t.Fatalf("expected enqueue action, got %q", decision.Action)
	}

	decision = ResolvePageGapAuditFollowUp(4, 30, 30, 0, true)
	if decision.Action != "validated" {
		t.Fatalf("expected validated action, got %q", decision.Action)
	}

	decision = ResolvePageGapAuditFollowUp(4, 30, 27, 0, false)
	if decision.Action != "incomplete" {
		t.Fatalf("expected incomplete action, got %q", decision.Action)
	}
}

func TestResolvePageGapPassEnd(t *testing.T) {
	completed := ResolvePageGapPassEnd(0, 3)
	if completed.Status != "completed" || !completed.StopRecovery {
		t.Fatalf("expected completed stop decision, got %#v", completed)
	}

	stagnant := ResolvePageGapPassEnd(2, 0)
	if stagnant.Status != "stagnant" || !stagnant.StopRecovery {
		t.Fatalf("expected stagnant stop decision, got %#v", stagnant)
	}

	continuing := ResolvePageGapPassEnd(2, 1)
	if continuing.Status != "continue" || continuing.StopRecovery {
		t.Fatalf("expected continue decision, got %#v", continuing)
	}
}
