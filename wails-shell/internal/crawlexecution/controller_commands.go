package crawlexecution

// Controller-command decisions keep UI/bridge stop-restart requests separate
// from the runner implementation. If crawl control buttons behave strangely,
// inspect this file before touching runner shutdown/startup code.
//
// Ownership summary:
// 1) decide restart/stop/shutdown controller command behavior
// 2) normalize controller-state transitions for UI and bridge commands
// 3) keep command-decision policy separate from runner implementation details
//
// File map for maintainers:
// 1) restart/stop/shutdown decision DTOs
// 2) command-resolution helpers for controller status changes
// 3) sidecar-not-started fallback decision helpers

type RestartCommandDecision struct {
	NormalizedControllerStatus string `json:"normalizedControllerStatus"`
	ShouldStartImmediately     bool   `json:"shouldStartImmediately"`
	ShouldQueueRestart         bool   `json:"shouldQueueRestart"`
	ShouldRequestStop          bool   `json:"shouldRequestStop"`
	Restarting                 bool   `json:"restarting"`
}

type StopCommandDecision struct {
	NormalizedControllerStatus string `json:"normalizedControllerStatus"`
	AlreadyStopped             bool   `json:"alreadyStopped"`
	ShouldMarkStopping         bool   `json:"shouldMarkStopping"`
	ShouldRequestStop          bool   `json:"shouldRequestStop"`
}

type ShutdownCommandDecision struct {
	NormalizedControllerStatus string `json:"normalizedControllerStatus"`
	AlreadyInactive            bool   `json:"alreadyInactive"`
	ShouldWaitForFinalState    bool   `json:"shouldWaitForFinalState"`
}

type SidecarNotStartedFallback struct {
	Command                     string `json:"command"`
	ShouldResetControllerToIdle bool   `json:"shouldResetControllerToIdle"`
	ClearCurrentOutput          bool   `json:"clearCurrentOutput"`
	ShouldStartImmediately      bool   `json:"shouldStartImmediately"`
	TreatAsAlreadyStopped       bool   `json:"treatAsAlreadyStopped"`
	TreatAsShutdownComplete     bool   `json:"treatAsShutdownComplete"`
}

func ResolveRestartCommandDecision(controllerStatus string) RestartCommandDecision {
	normalizedStatus := NormalizeStatus(controllerStatus, "")
	decision := RestartCommandDecision{
		NormalizedControllerStatus: normalizedStatus,
	}

	if !IsActiveStatus(normalizedStatus) {
		decision.ShouldStartImmediately = true
		return decision
	}

	decision.ShouldQueueRestart = true
	decision.Restarting = true
	if normalizedStatus == "stopping" {
		return decision
	}

	decision.ShouldRequestStop = true
	return decision
}

func ResolveStopCommandDecision(controllerStatus string) StopCommandDecision {
	normalizedStatus := NormalizeStatus(controllerStatus, "")
	decision := StopCommandDecision{
		NormalizedControllerStatus: normalizedStatus,
	}

	if !IsActiveStatus(normalizedStatus) {
		decision.AlreadyStopped = true
		return decision
	}

	decision.ShouldMarkStopping = true
	decision.ShouldRequestStop = true
	return decision
}

func ResolveShutdownCommandDecision(controllerStatus string) ShutdownCommandDecision {
	normalizedStatus := NormalizeStatus(controllerStatus, "")
	if !IsActiveStatus(normalizedStatus) {
		return ShutdownCommandDecision{
			NormalizedControllerStatus: normalizedStatus,
			AlreadyInactive:            true,
			ShouldWaitForFinalState:    false,
		}
	}

	return ShutdownCommandDecision{
		NormalizedControllerStatus: normalizedStatus,
		AlreadyInactive:            false,
		ShouldWaitForFinalState:    true,
	}
}

func ResolveSidecarNotStartedFallback(command string) SidecarNotStartedFallback {
	normalizedCommand := NormalizeStatus(command, "")
	fallback := SidecarNotStartedFallback{
		Command:                     normalizedCommand,
		ShouldResetControllerToIdle: true,
		ClearCurrentOutput:          true,
	}

	switch normalizedCommand {
	case "restart":
		fallback.ShouldStartImmediately = true
	case "stop":
		fallback.TreatAsAlreadyStopped = true
	case "shutdown":
		fallback.TreatAsShutdownComplete = true
	default:
		fallback.ShouldResetControllerToIdle = false
		fallback.ClearCurrentOutput = false
	}

	return fallback
}
