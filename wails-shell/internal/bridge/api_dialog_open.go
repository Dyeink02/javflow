package bridge

import (
	"fmt"
	"os"
)

// Dialog open helpers resolve desktop paths and then delegate to the desktop
// service. Path resolution lives here so packaging/path bugs stay isolated.
//
// Opening helpers may ensure a target path exists when that is part of the
// generic shell/open contract, but they should not create feature-specific
// artifacts or infer business data.
//
// Ownership summary:
// 1) resolve and open desktop paths/URLs through the shell service
// 2) keep generic open-path behavior centralized
// 3) separate shell-open helpers from feature-specific artifact logic
//
// File map for maintainers:
// 1) generic open-path/open-url helpers
// 2) crawl/organizer output open helpers
// 3) log/artifact convenience open helpers

func (a *API) openDialogPath(targetPath string) (string, error) {
	openedPath, err := a.runtime.dialogs.OpenPath(targetPath)
	if err != nil {
		return "", err
	}
	return marshalResult(openedPath)
}

func (a *API) openExternalURL(targetURL string) (string, error) {
	openedURL, err := a.runtime.dialogs.OpenExternal(targetURL)
	if err != nil {
		return "", err
	}
	return marshalResult(openedURL)
}

func (a *API) handleOpenOutputDir(payload map[string]any) (string, error) {
	targetPath := a.resolveOutputContext(nonEmptyString(payload["targetPath"])).OutputDir
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		return "", err
	}
	return a.openDialogPath(targetPath)
}

func (a *API) handleOpenLogFolder() (string, error) {
	logDir := a.resolveOutputContext("").LogDir
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return "", err
	}
	return a.openDialogPath(logDir)
}

func (a *API) handleOpenMagnetFile(payload map[string]any) (string, error) {
	outputCtx := a.resolveOutputContext(nonEmptyString(payload["targetOutput"]))
	magnetPath := outputCtx.MagnetPath
	if _, err := os.Stat(magnetPath); err != nil {
		magnetPath = latestSubscriptionDatedTextFile(outputCtx.OutputDir)
		if magnetPath == "" {
			return "", fmt.Errorf("magnet file not found: %s", outputCtx.MagnetPath)
		}
	}
	return a.openDialogPath(magnetPath)
}

func (a *API) handleOpenOrganizerPath(payload map[string]any) (string, error) {
	targetPath := a.organizer.organizerService.ResolveTargetPath(stringValue(payload["rootPath"]), stringValue(payload["kind"]))
	if targetPath == "" {
		return "", fmt.Errorf("organizer target path is empty")
	}

	kind := stringValue(payload["kind"])
	if kind != "root" && kind != "reports" {
		if err := os.MkdirAll(targetPath, 0o755); err != nil {
			return "", err
		}
	}

	return a.openDialogPath(targetPath)
}
