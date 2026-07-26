// Package interfaces defines internal core service interfaces and sentinel
// errors used for decoupling and testing.
//
// Ownership summary:
// 1) define narrow cross-module interfaces used for decoupling and mocks
// 2) centralize shared sentinel errors for cross-package error matching
// 3) keep interface contracts separate from concrete service implementations
//
// File map for maintainers:
// 1) shared event/settings/task-facing interfaces
// 2) sentinel error values for cross-package matching
// 3) compatibility/deprecation sentinel markers
package interfaces

import (
	"context"
	"encoding/json"
)

// EventBus 应用事件总线接口。
type EventBus interface {
	Emit(name string, payload any)
	Publish(version string, kind string, eventName string, domain string, action string, taskID string, timestamp string, rawData json.RawMessage)
}

// SettingsStore 持久化设置存储接口。
type SettingsStore interface {
	Load() (map[string]any, error)
	Save(settings map[string]any) error
}

// ErrSentinel 用于 errors.Is 判断的可导出哨兵错误。
var (
	ErrNotInitialized       = SentinelError{"服务未初始化"}
	ErrAlreadyRunning       = SentinelError{"任务已在运行中"}
	ErrSidecarNotStarted    = SentinelError{"侧载进程尚未启动"}
	ErrOutputDirEmpty       = SentinelError{"抓取输出目录不能为空"}
	ErrTaskStopTimeout      = SentinelError{"等待抓取任务停止超时"}
)

// SentinelError 实现 error 接口的哨兵值，调用方可用 SentinelError.ErrorMatch 或 any 断言。
type SentinelError struct{ Msg string }

func (e SentinelError) Error() string { return e.Msg }

// DeprecatedOldSidecar 标记旧 Node sidecar 调用路径为过时。
// Go 原生路径稳定后应移除此 fallback。
var (
	// DeprecatedLegacyCrawlSidecarUse 当代码仍使用 Node sidecar 做抓取时使用。
	DeprecatedLegacyCrawlSidecarUse = SentinelError{"[Deprecated] 当前使用 Node sidecar 执行抓取，Go 原生路径稳定后应移除此调用"}
	// DeprecatedLegacySidecarPath 旧 Node sidecar 路径。
	// Keep the legacy sidecar path visible in one place while the last Electron
	// compatibility traces are being retired from the tree.
	DeprecatedLegacySidecarPath = "desktop/sidecar/index.js"
)

// ObservabilityType 事件观察者接口。
// 任何可通过事件总线推送结构化信息到前端面板的服务都应实现此接口。
type ObservabilityType interface {
	EventKind() string
}

// RenderPanel 前端面板数据提供接口。
type RenderPanel interface {
	Build() (any, error)
}

// TaskRuntime 获取运行状态快照。
type TaskRuntime interface {
	Snapshot() map[string]any
}

var _ context.Context = nil
