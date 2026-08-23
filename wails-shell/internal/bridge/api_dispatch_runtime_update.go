package bridge

import (
	"context"
	"fmt"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Runtime update handlers keep the renderer-facing update contract in the
// runtime domain. The actual release/network/file policy remains in appupdate;
// this file only supplies the Wails lifecycle context and restart handoff.
//
// Ownership summary:
// 1) expose check/download/apply commands to the active Wails renderer
// 2) keep update work bounded by the application lifecycle
// 3) schedule a graceful Wails quit after the helper process is ready
//
// File map for maintainers:
// 1) update check command
// 2) update download command
// 3) update apply/restart command
func (a *API) handleCheckAppUpdateCommand() (string, bool, error) {
	if a.runtime.appUpdate == nil {
		return "", true, fmt.Errorf("在线更新服务未初始化")
	}
	result, err := a.runtime.appUpdate.Check(a.applicationContext())
	if err != nil {
		return "", true, err
	}
	encoded, err := marshalResult(result)
	return encoded, true, err
}

func (a *API) handleDownloadAppUpdateCommand(payload map[string]any) (string, bool, error) {
	if a.runtime.appUpdate == nil {
		return "", true, fmt.Errorf("在线更新服务未初始化")
	}
	requestedVersion := nonEmptyString(payload["version"])
	result, err := a.runtime.appUpdate.Download(a.applicationContext(), requestedVersion)
	if err != nil {
		return "", true, err
	}
	encoded, err := marshalResult(result)
	return encoded, true, err
}

func (a *API) handleApplyAppUpdateCommand() (string, bool, error) {
	if a.runtime.appUpdate == nil {
		return "", true, fmt.Errorf("在线更新服务未初始化")
	}
	if a.hasActiveRunner() {
		return "", true, fmt.Errorf("当前仍有抓取任务运行，请先停止任务再更新")
	}
	result, err := a.runtime.appUpdate.Apply(a.applicationContext())
	if err != nil {
		return "", true, err
	}
	encoded, err := marshalResult(result)
	if err != nil {
		return "", true, err
	}

	// Return the JSON response first. The helper waits for the current EXE to
	// unlock; a short delayed quit gives the Wails binding time to deliver this
	// response to the renderer before the window closes.
	if result.RestartRequired && a.wailsCtx != nil {
		go func(ctx context.Context) {
			time.Sleep(300 * time.Millisecond)
			runtime.Quit(ctx)
		}(a.wailsCtx)
	}
	return encoded, true, nil
}
