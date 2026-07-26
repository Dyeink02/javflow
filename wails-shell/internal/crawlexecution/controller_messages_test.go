package crawlexecution

import (
	"errors"
	"testing"
)

func TestResolveControllerStartMessage(t *testing.T) {
	if got := ResolveControllerStartMessage("start"); got != "抓取任务启动中..." {
		t.Fatalf("unexpected start message: %q", got)
	}
	if got := ResolveControllerStartMessage("restart"); got != "继续抓取任务启动中..." {
		t.Fatalf("unexpected restart message: %q", got)
	}
}

func TestResolveTaskLogAndOutputMessages(t *testing.T) {
	if got := ResolveTaskLogCreatedMessage(`C:\logs\session.log`); got != `本次运行日志已创建：C:\logs\session.log` {
		t.Fatalf("unexpected task log message: %q", got)
	}
	if got := ResolveTaskLogCreatedMessage(""); got != "本次运行日志已创建。" {
		t.Fatalf("unexpected empty task log message: %q", got)
	}
	if got := ResolveOutputRedirectLogMessage(`C:\output\run-1`); got != `检测到输出目录已有历史结果，本次任务已自动切换到独立输出目录：C:\output\run-1` {
		t.Fatalf("unexpected output redirect message: %q", got)
	}
}

func TestResolveRestartAndStopMessages(t *testing.T) {
	if got := ResolveQueuedRestartControllerMessage(); got != "已记录继续抓取请求，等待当前任务停止后自动衔接..." {
		t.Fatalf("unexpected queued restart message: %q", got)
	}
	if got := ResolveQueuedRestartRefreshLogMessage(); got != "继续抓取请求已更新，当前任务停止后将按最新设置衔接。" {
		t.Fatalf("unexpected queued restart refresh log: %q", got)
	}
	if got := ResolveRestartStopRequestedLogMessage(); got != "已发送继续抓取指令，当前任务停止后将自动衔接。" {
		t.Fatalf("unexpected restart stop log: %q", got)
	}
	if got := ResolveStopRequestedControllerMessage(); got != "已发送停止指令，等待当前任务收尾..." {
		t.Fatalf("unexpected stop requested message: %q", got)
	}
}

func TestResolveSidecarNotStartedControllerMessage(t *testing.T) {
	if got := ResolveSidecarNotStartedControllerMessage("restart"); got != "当前执行器已停止，直接按继续抓取重新启动。" {
		t.Fatalf("unexpected restart fallback message: %q", got)
	}
	if got := ResolveSidecarNotStartedControllerMessage("stop"); got != "当前没有正在运行的抓取任务。" {
		t.Fatalf("unexpected stop fallback message: %q", got)
	}
	if got := ResolveSidecarNotStartedControllerMessage("shutdown"); got != "抓取执行器已停止，应用将直接退出。" {
		t.Fatalf("unexpected shutdown fallback message: %q", got)
	}
	if got := ResolveSidecarNotStartedControllerMessage("unknown"); got != "抓取执行器尚未启动。" {
		t.Fatalf("unexpected generic fallback message: %q", got)
	}
}

func TestResolveShutdownAndResumeMessages(t *testing.T) {
	if got := ResolveShutdownWaitLogMessage(); got != "桌面程序准备关闭，正在等待当前抓取任务安全收尾。" {
		t.Fatalf("unexpected shutdown wait log: %q", got)
	}
	if got := ResolveShutdownCompletedLogMessage("stopped"); got != "抓取任务已完成关停收尾，最终状态：stopped。" {
		t.Fatalf("unexpected shutdown completed log: %q", got)
	}
	if got := ResolvePendingRestartResumeLogMessage(); got != "当前任务已停止，开始继续抓取任务..." {
		t.Fatalf("unexpected pending restart resume log: %q", got)
	}
	if got := ResolvePendingRestartErrorMessage(errors.New("boom")); got != "继续抓取任务失败：boom" {
		t.Fatalf("unexpected pending restart error message: %q", got)
	}
	if got := ResolvePendingRestartErrorMessage(nil); got != "继续抓取任务失败。" {
		t.Fatalf("unexpected nil pending restart error message: %q", got)
	}
}
