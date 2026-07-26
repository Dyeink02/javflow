package crawlexecution

import (
	"fmt"
	"strings"
)

// Controller messages centralize operator-facing wording for task-controller
// lifecycle events.
//
// Ownership summary:
// 1) centralize task-controller lifecycle wording for logs and status messages
// 2) keep controller text separate from controller command decisions
// 3) provide one update point for operator-facing task-controller phrases
//
// File map for maintainers:
// 1) start/stop/restart controller message builders
// 2) task-log and output-directory wording helpers
// 3) sidecar/runtime fallback status text helpers

func normalizeControllerCommand(command string) string {
	return strings.ToLower(strings.TrimSpace(command))
}

func ResolveControllerStartMessage(action string) string {
	if normalizeControllerCommand(action) == "restart" {
		return "继续抓取任务启动中..."
	}
	return "抓取任务启动中..."
}

func ResolveTaskLogCreatedMessage(sessionLogPath string) string {
	trimmedPath := strings.TrimSpace(sessionLogPath)
	if trimmedPath == "" {
		return "本次运行日志已创建。"
	}
	return fmt.Sprintf("本次运行日志已创建：%s", trimmedPath)
}

func ResolveOutputRedirectLogMessage(outputDir string) string {
	return fmt.Sprintf("检测到输出目录已有历史结果，本次任务已自动切换到独立输出目录：%s", strings.TrimSpace(outputDir))
}

func ResolveQueuedRestartControllerMessage() string {
	return "已记录继续抓取请求，等待当前任务停止后自动衔接..."
}

func ResolveQueuedRestartRefreshLogMessage() string {
	return "继续抓取请求已更新，当前任务停止后将按最新设置衔接。"
}

func ResolveRestartStopRequestedLogMessage() string {
	return "已发送继续抓取指令，当前任务停止后将自动衔接。"
}

func ResolveStopRequestedControllerMessage() string {
	return "已发送停止指令，等待当前任务收尾..."
}

func ResolveSidecarNotStartedControllerMessage(command string) string {
	switch normalizeControllerCommand(command) {
	case "restart":
		return "当前执行器已停止，直接按继续抓取重新启动。"
	case "stop":
		return "当前没有正在运行的抓取任务。"
	case "shutdown":
		return "抓取执行器已停止，应用将直接退出。"
	default:
		return "抓取执行器尚未启动。"
	}
}

func ResolveShutdownWaitLogMessage() string {
	return "桌面程序准备关闭，正在等待当前抓取任务安全收尾。"
}

func ResolveShutdownCompletedLogMessage(finalStatus string) string {
	return fmt.Sprintf("抓取任务已完成关停收尾，最终状态：%s。", strings.TrimSpace(finalStatus))
}

func ResolvePendingRestartResumeLogMessage() string {
	return "当前任务已停止，开始继续抓取任务..."
}

func ResolvePendingRestartErrorMessage(err error) string {
	if err == nil {
		return "继续抓取任务失败。"
	}
	return fmt.Sprintf("继续抓取任务失败：%s", strings.TrimSpace(err.Error()))
}
