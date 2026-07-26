package crawlexecution

import "testing"

func TestFinalNoticeLevel(t *testing.T) {
	if got := FinalNoticeLevel("completed"); got != "info" {
		t.Fatalf("expected completed notice level info, got %q", got)
	}
	if got := FinalNoticeLevel("error"); got != "error" {
		t.Fatalf("expected error notice level error, got %q", got)
	}
	if got := FinalNoticeLevel("stopped"); got != "warn" {
		t.Fatalf("expected stopped notice level warn, got %q", got)
	}
	if got := FinalNoticeLevel("incomplete"); got != "warn" {
		t.Fatalf("expected incomplete notice level warn, got %q", got)
	}
}

func TestResolveObservedStateTransitionKeepsActiveStatuses(t *testing.T) {
	transition := ResolveObservedStateTransition("running", false)

	if transition.NormalizedStatus != "running" {
		t.Fatalf("expected normalized status running, got %q", transition.NormalizedStatus)
	}
	if transition.ControllerStatus != "running" {
		t.Fatalf("expected controller status running, got %q", transition.ControllerStatus)
	}
	if transition.ClearCurrentOutput {
		t.Fatal("did not expect active status to clear current output")
	}
	if transition.NotifyFinalStateWaiter {
		t.Fatal("did not expect active status to notify final state waiters")
	}
	if transition.EmitCompletionNotice {
		t.Fatal("did not expect active status to emit a completion notice")
	}
}

func TestResolveObservedStateTransitionConsumesPendingRestartOnFinalState(t *testing.T) {
	transition := ResolveObservedStateTransition("stopped", true)

	if transition.NormalizedStatus != "stopped" {
		t.Fatalf("expected normalized status stopped, got %q", transition.NormalizedStatus)
	}
	if transition.ControllerStatus != "idle" {
		t.Fatalf("expected final state to move controller to idle, got %q", transition.ControllerStatus)
	}
	if !transition.ClearCurrentOutput {
		t.Fatal("expected final state to clear current output")
	}
	if !transition.ConsumePendingRestart {
		t.Fatal("expected final state to consume pending restart")
	}
	if !transition.NotifyFinalStateWaiter {
		t.Fatal("expected final state to notify waiters")
	}
	if !transition.ResumePendingRestart {
		t.Fatal("expected final state to resume pending restart")
	}
	if transition.EmitCompletionNotice {
		t.Fatal("did not expect completion notice when a pending restart exists")
	}
}

func TestResolveObservedStateTransitionEmitsNoticeWhenNoPendingRestart(t *testing.T) {
	transition := ResolveObservedStateTransition("completed", false)

	if transition.ControllerStatus != "idle" {
		t.Fatalf("expected completed state to move controller to idle, got %q", transition.ControllerStatus)
	}
	if !transition.NotifyFinalStateWaiter {
		t.Fatal("expected completed state to notify waiters")
	}
	if transition.ConsumePendingRestart {
		t.Fatal("did not expect pending restart consumption when none exists")
	}
	if transition.ResumePendingRestart {
		t.Fatal("did not expect pending restart resume when none exists")
	}
	if !transition.EmitCompletionNotice {
		t.Fatal("expected completed state to emit a completion notice")
	}
	if transition.CompletionNoticeLevel != "info" {
		t.Fatalf("expected completed notice level info, got %q", transition.CompletionNoticeLevel)
	}
}
