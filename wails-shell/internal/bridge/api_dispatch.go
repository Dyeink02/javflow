package bridge

import "fmt"

// Call keeps the top-level bridge dispatch shallow so startup and runtime
// issues can be isolated by command domain instead of one large switch.
//
// Debugging rule:
// 1) find the owning step in `dispatchChain()`
// 2) inspect that domain's `handle*Command`
// 3) only if no Go-owned domain handled the command, inspect sidecar fallback
//
// Keep this file as the routing boundary, not a place to add business logic.
//
// Maintenance rule:
// if a new module/command family is introduced, register a dedicated
// `handle*Command` in the chain rather than expanding per-command routing here.
//
// Ownership summary:
// 1) execute the top-level bridge command dispatch sequence
// 2) fall through to explicit sidecar legacy commands only when no Go domain handled a command
// 3) keep top-level routing shallow and free of business logic
//
// File map for maintainers:
// 1) top-level bridge call entrypoint
// 2) sidecar fallback after Go-owned chain exhaustion
func (a *API) Call(command string, payload map[string]any) (string, error) {
	for _, step := range a.dispatchChain() {
		if result, handled, err := step.handler(command, payload); handled {
			return result, err
		}
	}

	if domain, action, ok := legacySidecarCommand(command); ok {
		return a.callSidecar(domain, action, payload)
	}

	return "", fmt.Errorf("unsupported command: %s", command)
}
