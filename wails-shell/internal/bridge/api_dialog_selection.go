package bridge

import "github.com/wailsapp/wails/v2/pkg/runtime"

// Dialog selection helpers own user-initiated pickers and small settings
// updates tied to those pickers.
//
// Keep these helpers narrow:
// - select a path/file set
// - optionally persist the selected value into generic settings state
// - return a normalized dialog result
// They should not absorb feature-specific post-processing.
//
// Ownership summary:
// 1) handle user-initiated file/directory selection dialogs
// 2) persist generic settings updates tied to those selections when needed
// 3) keep dialog selection helpers separate from feature-specific workflows
//
// File map for maintainers:
// 1) generic directory/file picker helpers
// 2) selection-persisted settings helpers
// 3) concrete dialog selection entrypoints

func (a *API) chooseDirectoryDialog(title string, defaultDir string) (string, error) {
	selected, err := a.runtime.dialogs.ChooseDirectory(title, defaultDir)
	if err != nil {
		return "", err
	}
	return marshalResult(selected)
}

func (a *API) saveBackgroundImageSelection(path string) (string, error) {
	currentSettings, err := a.mutateBridgeSettings(func(settings map[string]any) {
		settings["backgroundImage"] = path
	})
	if err != nil {
		return "", err
	}
	return marshalResult(a.runtime.store.AttachBackgroundURL(currentSettings))
}

func (a *API) handleChooseBackgroundImage() (string, error) {
	selected, err := a.runtime.dialogs.ChooseFile(
		"Choose background image",
		"",
		[]runtime.FileFilter{
			{DisplayName: "Images", Pattern: "*.jpg;*.jpeg;*.png;*.webp;*.bmp"},
		},
	)
	if err != nil {
		return "", err
	}
	if selected == "" {
		return marshalResult(nil)
	}
	return a.saveBackgroundImageSelection(selected)
}

func (a *API) handleClearBackgroundImage() (string, error) {
	return a.saveBackgroundImageSelection("")
}

func (a *API) handleChooseLearningSamples() (string, error) {
	selected, err := a.runtime.dialogs.ChooseMultipleFiles(
		"Choose learning samples",
		"",
		[]runtime.FileFilter{
			{DisplayName: "Media or image files", Pattern: "*.jpg;*.jpeg;*.png;*.bmp;*.webp;*.mp4;*.mkv;*.avi;*.mov;*.wmv"},
		},
	)
	if err != nil {
		return "", err
	}
	return marshalResult(selected)
}
