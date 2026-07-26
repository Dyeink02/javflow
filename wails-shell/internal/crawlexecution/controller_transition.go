package crawlexecution

// Observed-state transitions own the controller's response to runner final
// states. This is the boundary between "runner emitted status X" and "bridge/UI
// should now behave like Y".
//
// Ownership summary:
// 1) map observed runner final states into controller/UI transition behavior
// 2) centralize pending-restart and completion-notice decisions
// 3) keep observed-state transition policy separate from runner status emission
//
// File map for maintainers:
// 1) observed final-state transition DTO
// 2) final notice level helpers
// 3) pending-restart transition resolution

type ObservedStateTransition struct {
	NormalizedStatus       string `json:"normalizedStatus"`
	ControllerStatus       string `json:"controllerStatus"`
	ClearCurrentOutput     bool   `json:"clearCurrentOutput"`
	ConsumePendingRestart  bool   `json:"consumePendingRestart"`
	NotifyFinalStateWaiter bool   `json:"notifyFinalStateWaiter"`
	ResumePendingRestart   bool   `json:"resumePendingRestart"`
	EmitCompletionNotice   bool   `json:"emitCompletionNotice"`
	CompletionNoticeLevel  string `json:"completionNoticeLevel"`
}

func FinalNoticeLevel(status string) string {
	switch NormalizeStatus(status, "") {
	case "completed":
		return "info"
	case "error":
		return "error"
	case "stopped", "incomplete":
		return "warn"
	default:
		return ""
	}
}

func ResolveObservedStateTransition(status string, hasPendingRestart bool) ObservedStateTransition {
	normalizedStatus := NormalizeStatus(status, "")
	transition := ObservedStateTransition{
		NormalizedStatus: normalizedStatus,
		ControllerStatus: normalizedStatus,
	}

	if !IsFinalStatus(normalizedStatus) {
		return transition
	}

	transition.ControllerStatus = "idle"
	transition.ClearCurrentOutput = true
	transition.ConsumePendingRestart = hasPendingRestart
	transition.NotifyFinalStateWaiter = true
	transition.ResumePendingRestart = hasPendingRestart
	transition.EmitCompletionNotice = !hasPendingRestart
	transition.CompletionNoticeLevel = FinalNoticeLevel(normalizedStatus)

	return transition
}
