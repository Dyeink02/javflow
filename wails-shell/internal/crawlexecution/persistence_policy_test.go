package crawlexecution

import "testing"

func TestResolveSnapshotMode(t *testing.T) {
	if got := ResolveSnapshotMode("running", false, ""); got != SnapshotModeLight {
		t.Fatalf("expected light, got %q", got)
	}
	if got := ResolveSnapshotMode("completed", false, ""); got != SnapshotModeFull {
		t.Fatalf("expected full for completed, got %q", got)
	}
	if got := ResolveSnapshotMode("running", true, ""); got != SnapshotModeFull {
		t.Fatalf("expected full when forced, got %q", got)
	}
}

func TestShouldPersistSnapshot(t *testing.T) {
	if ShouldPersistSnapshot(1000, 1200, 500, false) {
		t.Fatal("expected persist to be throttled")
	}
	if !ShouldPersistSnapshot(1000, 1600, 500, false) {
		t.Fatal("expected persist to pass interval check")
	}
	if !ShouldPersistSnapshot(1000, 1001, 500, true) {
		t.Fatal("expected forced persist to pass")
	}
}

func TestBuildArtifactFinalizationPlan(t *testing.T) {
	plan := BuildArtifactFinalizationPlan("completed", 0, 0)
	if !plan.ClearUnfinishedReport || !plan.CleanupRuntimeState {
		t.Fatalf("unexpected plan: %#v", plan)
	}

	plan = BuildArtifactFinalizationPlan("incomplete", 3, 1)
	if plan.ClearUnfinishedReport || plan.CleanupRuntimeState {
		t.Fatalf("unexpected incomplete plan: %#v", plan)
	}
}
