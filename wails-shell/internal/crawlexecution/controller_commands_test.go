package crawlexecution

import "testing"

func TestResolveRestartCommandDecision(t *testing.T) {
	decision := ResolveRestartCommandDecision("idle")
	if !decision.ShouldStartImmediately {
		t.Fatal("expected idle restart to start immediately")
	}
	if decision.Restarting {
		t.Fatal("did not expect idle restart to report restarting=true")
	}

	decision = ResolveRestartCommandDecision("running")
	if !decision.ShouldQueueRestart {
		t.Fatal("expected running restart to queue restart")
	}
	if !decision.ShouldRequestStop {
		t.Fatal("expected running restart to request stop")
	}
	if !decision.Restarting {
		t.Fatal("expected running restart to report restarting=true")
	}

	decision = ResolveRestartCommandDecision("stopping")
	if !decision.ShouldQueueRestart {
		t.Fatal("expected stopping restart to keep queued restart")
	}
	if decision.ShouldRequestStop {
		t.Fatal("did not expect stopping restart to request another stop")
	}
	if !decision.Restarting {
		t.Fatal("expected stopping restart to report restarting=true")
	}
}

func TestResolveStopCommandDecision(t *testing.T) {
	decision := ResolveStopCommandDecision("completed")
	if !decision.AlreadyStopped {
		t.Fatal("expected completed state to be treated as already stopped")
	}
	if decision.ShouldRequestStop {
		t.Fatal("did not expect completed state to send stop")
	}

	decision = ResolveStopCommandDecision("running")
	if decision.AlreadyStopped {
		t.Fatal("did not expect running state to be already stopped")
	}
	if !decision.ShouldMarkStopping {
		t.Fatal("expected running state to mark controller as stopping")
	}
	if !decision.ShouldRequestStop {
		t.Fatal("expected running state to request stop")
	}
}

func TestResolveShutdownCommandDecision(t *testing.T) {
	decision := ResolveShutdownCommandDecision("idle")
	if !decision.AlreadyInactive {
		t.Fatal("expected idle shutdown to be already inactive")
	}
	if decision.ShouldWaitForFinalState {
		t.Fatal("did not expect idle shutdown to wait for final state")
	}

	decision = ResolveShutdownCommandDecision("stopping")
	if decision.AlreadyInactive {
		t.Fatal("did not expect stopping shutdown to be inactive")
	}
	if !decision.ShouldWaitForFinalState {
		t.Fatal("expected stopping shutdown to wait for final state")
	}
}

func TestResolveSidecarNotStartedFallback(t *testing.T) {
	restartFallback := ResolveSidecarNotStartedFallback("restart")
	if !restartFallback.ShouldResetControllerToIdle || !restartFallback.ClearCurrentOutput {
		t.Fatalf("unexpected restart fallback reset behavior: %#v", restartFallback)
	}
	if !restartFallback.ShouldStartImmediately {
		t.Fatal("expected restart fallback to start immediately")
	}

	stopFallback := ResolveSidecarNotStartedFallback("stop")
	if !stopFallback.TreatAsAlreadyStopped {
		t.Fatal("expected stop fallback to treat result as already stopped")
	}
	if stopFallback.ShouldStartImmediately {
		t.Fatal("did not expect stop fallback to start immediately")
	}

	shutdownFallback := ResolveSidecarNotStartedFallback("shutdown")
	if !shutdownFallback.TreatAsShutdownComplete {
		t.Fatal("expected shutdown fallback to complete shutdown")
	}
}
