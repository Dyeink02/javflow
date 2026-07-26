package bridge

import (
	"context"
	"fmt"
)

// Dependency command handling stays separate from ad-learning so setup/install
// issues can be triaged without scanning organizer-facing model workflows.
//
// Dependency ownership rule:
// this domain manages install/probe/remove mechanics only. Policy about when a
// dependency is required belongs to the owning feature controller/service.
//
// Ownership summary:
// 1) route dependency probe/install/uninstall bridge commands
// 2) keep dependency command dispatch separate from feature-domain logic
// 3) centralize bridge-facing dependency error handling
//
// Boundary rule:
// dependency installer/probe policy belongs in the dependency service. This
// file should remain a thin command router plus payload normalization shell.
//
// File map for maintainers:
// 1) dependency command dispatcher
// 2) dependency payload normalization helpers
func (a *API) handleDependencyCommand(command string, payload map[string]any) (string, bool, error) {
	switch command {
	case "app:get-dependency-status":
		if a.runtime.dependency == nil {
			return "", true, fmt.Errorf("dependency service is not initialized")
		}
		result, err := marshalResult(a.runtime.dependency.GetStatus())
		return result, true, err

	case "app:install-dependency":
		if a.runtime.dependency == nil {
			return "", true, fmt.Errorf("dependency service is not initialized")
		}
		status, err := a.runtime.dependency.Install(context.Background(), dependencyNameFromPayload(payload), dependencyDownloadURLFromPayload(payload))
		if err != nil {
			return "", true, err
		}
		result, err := marshalResult(status)
		return result, true, err

	case "app:uninstall-dependency":
		if a.runtime.dependency == nil {
			return "", true, fmt.Errorf("dependency service is not initialized")
		}
		status, err := a.runtime.dependency.Uninstall(dependencyNameFromPayload(payload))
		if err != nil {
			return "", true, err
		}
		result, err := marshalResult(status)
		return result, true, err
	}

	return "", false, nil
}

func dependencyNameFromPayload(payload map[string]any) string {
	name := nonEmptyString(payload["name"])
	if name == "" {
		name = nonEmptyString(payload["dependency"])
	}
	return name
}

func dependencyDownloadURLFromPayload(payload map[string]any) string {
	downloadURL := nonEmptyString(payload["downloadUrl"])
	if downloadURL == "" {
		downloadURL = nonEmptyString(payload["download_url"])
	}
	return downloadURL
}
