// Package desktop wraps Wails desktop shell integrations such as dialogs and local path actions.
//
// Ownership summary:
// 1) expose Wails desktop shell actions behind a narrow service facade
// 2) centralize path opening, external URL opening, and dialog interactions
// 3) keep shell integration concerns separate from bridge/domain logic
//
// Boundary rule:
// desktop shell helpers may wrap Wails runtime APIs, but feature policy about
// what to open/select belongs in bridge/domain layers instead of this package.
//
// File map for maintainers:
// 1) desktop shell service facade and context helpers
// 2) file/directory chooser helpers
// 3) local path and external URL open helpers
package desktop

import (
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type Service struct {
	ctxProvider func() context.Context
}

func NewService(ctxProvider func() context.Context) *Service {
	return &Service{ctxProvider: ctxProvider}
}

func (s *Service) ctx() (context.Context, error) {
	if s.ctxProvider == nil {
		return nil, errors.New("Wails 上下文尚未初始化")
	}
	ctx := s.ctxProvider()
	if ctx == nil {
		return nil, errors.New("Wails 上下文尚未初始化")
	}
	return ctx, nil
}

func dialogTypeFromString(value string) runtime.DialogType {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "error":
		return runtime.ErrorDialog
	case "question":
		return runtime.QuestionDialog
	case "warning":
		return runtime.WarningDialog
	default:
		return runtime.InfoDialog
	}
}

func (s *Service) ShowAlert(options map[string]any) (map[string]any, error) {
	ctx, err := s.ctx()
	if err != nil {
		return nil, err
	}

	title, _ := options["title"].(string)
	message, _ := options["message"].(string)
	buttons := dialogButtons(options["buttons"])
	if len(buttons) == 0 {
		buttonLabel, _ := options["buttonLabel"].(string)
		if buttonLabel == "" {
			buttonLabel = "我知道了"
		}
		buttons = []string{buttonLabel}
	}

	result, err := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
		Type:          dialogTypeFromString(stringValue(options["type"])),
		Title:         title,
		Message:       message,
		Buttons:       buttons,
		DefaultButton: buttons[0],
		CancelButton:  buttons[len(buttons)-1],
	})
	if err != nil {
		return nil, err
	}

	return map[string]any{
		// Windows MessageBox returns the fixed semantic values "Yes" and
		// "No" even when the UI displays localized text. Map them back to
		// the caller's labels so renderer code can compare its own buttons.
		"selection": normalizeDialogSelection(result, buttons),
	}, nil
}

func normalizeDialogSelection(result string, buttons []string) string {
	selection := strings.TrimSpace(result)
	if len(buttons) < 2 {
		return selection
	}

	switch strings.ToLower(selection) {
	case "yes", "ok", "确定", "是", "是(y)":
		return buttons[0]
	case "no", "cancel", "否", "否(n)":
		return buttons[len(buttons)-1]
	default:
		return selection
	}
}

func dialogButtons(value any) []string {
	items, ok := value.([]any)
	if !ok {
		if typed, typedOK := value.([]string); typedOK {
			items = make([]any, 0, len(typed))
			for _, item := range typed {
				items = append(items, item)
			}
		} else {
			return nil
		}
	}

	buttons := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		label, itemOK := item.(string)
		if !itemOK {
			continue
		}
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		if _, exists := seen[label]; exists {
			continue
		}
		seen[label] = struct{}{}
		buttons = append(buttons, label)
	}
	return buttons
}

func (s *Service) ChooseDirectory(title string, defaultDirectory string) (string, error) {
	ctx, err := s.ctx()
	if err != nil {
		return "", err
	}

	return runtime.OpenDirectoryDialog(ctx, runtime.OpenDialogOptions{
		Title:                title,
		DefaultDirectory:     defaultDirectory,
		CanCreateDirectories: true,
	})
}

func (s *Service) ChooseFile(title string, defaultDirectory string, filters []runtime.FileFilter) (string, error) {
	ctx, err := s.ctx()
	if err != nil {
		return "", err
	}

	return runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{
		Title:            title,
		DefaultDirectory: defaultDirectory,
		Filters:          filters,
	})
}

func (s *Service) ChooseMultipleFiles(title string, defaultDirectory string, filters []runtime.FileFilter) ([]string, error) {
	ctx, err := s.ctx()
	if err != nil {
		return nil, err
	}

	return runtime.OpenMultipleFilesDialog(ctx, runtime.OpenDialogOptions{
		Title:            title,
		DefaultDirectory: defaultDirectory,
		Filters:          filters,
	})
}

func (s *Service) OpenExternal(targetURL string) (string, error) {
	ctx, err := s.ctx()
	if err != nil {
		return "", err
	}

	runtime.BrowserOpenURL(ctx, targetURL)
	return targetURL, nil
}

func (s *Service) OpenPath(targetPath string) (string, error) {
	trimmed := strings.TrimSpace(targetPath)
	if trimmed == "" {
		return "", nil
	}

	if parsed, err := url.Parse(trimmed); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		return s.OpenExternal(trimmed)
	}

	absolutePath, err := filepath.Abs(trimmed)
	if err != nil {
		return "", err
	}

	if _, err := os.Stat(absolutePath); err != nil {
		return "", err
	}

	command := exec.Command("explorer.exe", absolutePath)
	if err := command.Start(); err != nil {
		return "", err
	}

	return absolutePath, nil
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
