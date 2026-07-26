package bridge

// handleDialogCommand keeps file pickers and open-path helpers in one place so
// path-resolution assumptions are easier to audit during desktop packaging work.
//
// Dialog boundary rule:
// this domain owns selection/open interactions only. It should not absorb
// feature-specific validation or artifact parsing rules.
//
// Ownership summary:
// 1) route dialog selection/open commands
// 2) keep user-facing path selection separate from feature workflows
// 3) preserve one audit point for desktop open-path assumptions
//
// File map for maintainers:
// 1) top-level dialog command dispatcher
// 2) selection dialog command handlers
// 3) open-path dialog command handlers
func (a *API) handleDialogCommand(command string, payload map[string]any) (string, bool, error) {
	if result, handled, err := a.handleDialogSelectionCommand(command, payload); handled {
		return result, true, err
	}
	if result, handled, err := a.handleDialogOpenCommand(command, payload); handled {
		return result, true, err
	}

	return "", false, nil
}

func (a *API) handleDialogSelectionCommand(command string, payload map[string]any) (string, bool, error) {
	switch command {
	case "app:show-alert":
		alertResult, err := a.runtime.dialogs.ShowAlert(payload)
		if err != nil {
			return "", true, err
		}
		result, err := marshalResult(alertResult)
		return result, true, err

	case "app:choose-output":
		result, err := a.chooseDirectoryDialog("Choose output directory", "")
		return result, true, err

	case "app:choose-background-image":
		result, err := a.handleChooseBackgroundImage()
		return result, true, err

	case "app:clear-background-image":
		result, err := a.handleClearBackgroundImage()
		return result, true, err

	case "app:choose-organizer-root":
		result, err := a.chooseDirectoryDialog("Choose organizer root", "")
		return result, true, err

	case "app:choose-learning-samples":
		result, err := a.handleChooseLearningSamples()
		return result, true, err
	}

	return "", false, nil
}

func (a *API) handleDialogOpenCommand(command string, payload map[string]any) (string, bool, error) {
	switch command {
	case "app:open-path":
		result, err := a.openDialogPath(stringValue(payload["targetPath"]))
		return result, true, err
	case "app:open-external":
		result, err := a.openExternalURL(stringValue(payload["targetUrl"]))
		return result, true, err
	case "app:open-output-dir":
		result, err := a.handleOpenOutputDir(payload)
		return result, true, err
	case "app:open-log-folder":
		result, err := a.handleOpenLogFolder()
		return result, true, err
	case "app:open-magnet-file":
		result, err := a.handleOpenMagnetFile(payload)
		return result, true, err
	case "app:open-organizer-path":
		result, err := a.handleOpenOrganizerPath(payload)
		return result, true, err
	}

	return "", false, nil
}
