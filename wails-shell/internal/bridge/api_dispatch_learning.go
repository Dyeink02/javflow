package bridge

// handleDependencyLearningCommand isolates dependency setup and ad-learning
// commands because they are high-volume in settings work but unrelated to crawl
// dispatch or file browsing.
//
// Pairing rule:
// these two domains share a routing group because they are configured together
// in organizer settings, but dependency mechanics and learning/model behavior
// must still stay split behind their own handlers.
//
// Ownership summary:
// 1) route dependency and ad-learning command groups together at one bridge step
// 2) keep their shared settings-entrypoint separate from crawl dispatch
// 3) preserve split handlers behind the combined routing group
//
// File map for maintainers:
// 1) dependency-learning combined dispatcher
// 2) dependency vs ad-learning branch routing
func (a *API) handleDependencyLearningCommand(command string, payload map[string]any) (string, bool, error) {
	if result, handled, err := a.handleDependencyCommand(command, payload); handled {
		return result, true, err
	}
	if result, handled, err := a.handleAdLearningCommand(command, payload); handled {
		return result, true, err
	}

	return "", false, nil
}
