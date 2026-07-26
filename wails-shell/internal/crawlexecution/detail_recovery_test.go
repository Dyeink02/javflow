package crawlexecution

import "testing"

func TestCountBudgetExhaustedDetailEntries(t *testing.T) {
	count := CountBudgetExhaustedDetailEntries([]DetailRecoveryBudgetEntry{
		{AttemptsUsed: 0, Budget: 0},
		{AttemptsUsed: 1, Budget: 2},
		{AttemptsUsed: 2, Budget: 2},
		{AttemptsUsed: 3, Budget: 1},
	})

	if count != 3 {
		t.Fatalf("expected exhausted count 3, got %d", count)
	}
}

func TestResolveDetailRecoveryPassStartForCompletedPass(t *testing.T) {
	decision := ResolveDetailRecoveryPassStart(2, 0, 0, 0)
	if decision.Status != "completed" || decision.ShouldRunPass || !decision.StopRecovery {
		t.Fatalf("expected completed stop decision, got %#v", decision)
	}
	if len(decision.LogMessages) != 1 {
		t.Fatalf("expected one completion message, got %#v", decision.LogMessages)
	}
}

func TestResolveDetailRecoveryPassStartForBudgetExhaustedPass(t *testing.T) {
	decision := ResolveDetailRecoveryPassStart(1, 4, 0, 3)
	if decision.Status != "budget_exhausted" || decision.ShouldRunPass || !decision.StopRecovery {
		t.Fatalf("expected budget exhausted stop decision, got %#v", decision)
	}
	if len(decision.LogMessages) != 2 {
		t.Fatalf("expected two stop messages, got %#v", decision.LogMessages)
	}
}

func TestResolveDetailRecoveryPassEnd(t *testing.T) {
	highPriorityRetry := ResolveDetailRecoveryPassEnd(2, 5, 5, 2)
	if highPriorityRetry.Status != "high_priority_retry" || highPriorityRetry.StopRecovery {
		t.Fatalf("expected high priority retry decision, got %#v", highPriorityRetry)
	}

	budgetExhausted := ResolveDetailRecoveryPassEnd(3, 4, 4, 0)
	if budgetExhausted.Status != "budget_exhausted" || !budgetExhausted.StopRecovery {
		t.Fatalf("expected budget exhausted stop decision, got %#v", budgetExhausted)
	}

	completed := ResolveDetailRecoveryPassEnd(1, 3, 0, 0)
	if completed.Status != "completed" || !completed.StopRecovery {
		t.Fatalf("expected completed stop decision, got %#v", completed)
	}
}
