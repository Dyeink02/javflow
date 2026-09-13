package bridge

import (
	"fmt"
	"strings"
	"time"

	"javflow/internal/organizer"
	"javflow/internal/modulelog"
)

// runOrganizer is intentionally kept thin. Organizer execution now reads as a
// single bridge workflow while sidecar/ad-risk wiring and lifecycle events live
// in dedicated helpers.
//
// This file owns organizer execution only. Crawl-artifact import for expected
// codes lives in a separate bridge helper so future troubleshooting can tell
// apart:
// - "organizer consumed crawl data wrong"
// - "organizer execution failed after inputs were ready"
//
// Ownership summary:
// 1) expose bridge-side organizer run entrypoints
// 2) keep organizer execution lifecycle separate from artifact import
// 3) isolate organizer runtime binding/emission from the service internals
//
// File map for maintainers:
// 1) organizer run entrypoint wrappers
// 2) prepared-run lifecycle/binding helpers
func (a *API) runOrganizer(payload map[string]any) (organizer.RunResult, error) {
	options, err := a.prepareOrganizerRunOptions(payload)
	if err != nil {
		return organizer.RunResult{}, err
	}
	return a.runPreparedOrganizer(options)
}

// runPreparedOrganizer owns only bridge-side lifecycle emission and runtime
// wiring. Actual organizer file decisions must stay inside organizerService so
// bridge changes do not silently fork organizer behavior.
func (a *API) runPreparedOrganizer(options organizer.RunOptions) (organizer.RunResult, error) {
	a.beginOrganizerModuleLog(options)
	taskID := newOrganizerTaskID()
	a.emitOrganizerStartState(taskID, options.DryRun)
	a.configureOrganizerAdRisk(&options, taskID)
	a.bindOrganizerRuntimeHandlers(&options, taskID)

	result, err := a.organizer.organizerService.RunOrganizer(options)
	if err != nil {
		a.emitOrganizerFailureState(taskID, err)
		return organizer.RunResult{}, err
	}

	a.emitOrganizerCompletionState(taskID, result)
	return result, nil
}

// beginOrganizerModuleLog starts the organizer's one-per-run log file and
// writes a settings snapshot header so 整理-*.txt is self-describing when a
// troubleshooting report comes back without the original form state.
func (a *API) beginOrganizerModuleLog(options organizer.RunOptions) {
	appPath := a.runtime.paths.AppPath
	if _, err := modulelog.BeginRun(appPath, modulelog.Organizer, time.Now()); err != nil {
		return
	}
	modeLabel := "移入待删除"
	if options.AdFileAction == "delete-directly" {
		modeLabel = "直接删除广告文件"
	}
	header := fmt.Sprintf(
		"整理运行开始 | 根目录: %s | 广告处理: %s | 批量删除: %s | 删除间隔: %dms | 整理间隔: %dms | 预览模式: %s | 扩展名: %s",
		options.RootPath,
		modeLabel,
		map[bool]string{true: "开", false: "关"}[options.BatchDelete],
		options.DeleteIntervalMs,
		options.OrganizeIntervalMs,
		map[bool]string{true: "是", false: "否"}[options.DryRun],
		strings.TrimSpace(options.VideoExtensions),
	)
	_ = modulelog.Append(appPath, modulelog.Organizer, "info", header, time.Now())
}
