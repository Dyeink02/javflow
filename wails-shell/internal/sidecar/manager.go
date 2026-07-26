// Package sidecar manages the Node.js compatibility sidecar lifecycle.
//
// The ordinary crawl path is now Go-native:
//
//	Wails bridge -> crawltask.Service -> crawlrunner.
//
// This package remains for compatibility flows that still depend on the
// Node/Puppeteer chain, mainly Cloudflare / age-check bypass. Treat edits here
// as high-risk and verify both the Go-native path and the compatibility path.
//
// Ownership summary:
// 1) start, stop, and supervise the Node sidecar process
// 2) bridge request/response packets between Go and the compatibility lane
// 3) keep sidecar lifecycle concerns out of crawl-domain services
//
// File map for maintainers:
// 1) sidecar process/request envelope DTOs
// 2) manager lifecycle/process supervision methods
// 3) command send/wait and packet pump helpers
package sidecar

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"javflow/internal/events"
	runtimepaths "javflow/internal/runtime"
)

type resultEnvelope struct {
	packet ResultPacket
	err    error
}

type Manager struct {
	mu       sync.Mutex
	repoRoot string
	paths    runtimepaths.Paths
	bus      *events.Bus

	cmd       *exec.Cmd
	stdin     io.WriteCloser
	cancel    context.CancelFunc
	pending   map[string]chan resultEnvelope
	requestID uint64

	exitDone chan struct{}
	exitErr  error

	startRequested bool
	starting       bool
	ready          bool
	readyCh        chan struct{}
	startErr       error
}

func NewManager(repoRoot string, paths runtimepaths.Paths, bus *events.Bus) *Manager {
	return &Manager{
		repoRoot: repoRoot,
		paths:    paths,
		bus:      bus,
		pending:  map[string]chan resultEnvelope{},
	}
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}

	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func uniqueStrings(items []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(items))

	for _, item := range items {
		normalized := strings.TrimSpace(item)
		if normalized == "" {
			continue
		}

		if _, exists := seen[normalized]; exists {
			continue
		}

		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}

	return result
}

func (m *Manager) resolveNodeCommand() (string, error) {
	envNode := strings.TrimSpace(os.Getenv("JAV_NODE_BIN"))
	if envNode != "" {
		if fileExists(envNode) {
			return envNode, nil
		}

		if resolved, err := exec.LookPath(envNode); err == nil {
			return resolved, nil
		}
	}

	candidates := []string{}
	if executablePath, err := os.Executable(); err == nil {
		executableDir := filepath.Dir(executablePath)
		candidates = append(candidates,
			filepath.Join(executableDir, "runtime", "node", "node.exe"),
			filepath.Join(executableDir, "node.exe"),
		)
	}

	candidates = append(candidates,
		filepath.Join(m.repoRoot, "wails-shell", "runtime", "node", "node.exe"),
		filepath.Join(m.repoRoot, "runtime", "node", "node.exe"),
		filepath.Join(m.repoRoot, "tools", "node", "node.exe"),
	)

	for _, candidate := range uniqueStrings(candidates) {
		if fileExists(candidate) {
			return candidate, nil
		}
	}

	if resolved, err := exec.LookPath("node"); err == nil {
		return resolved, nil
	}

	searchSummary := append([]string{}, uniqueStrings(candidates)...)
	if envNode != "" {
		searchSummary = append([]string{fmt.Sprintf("JAV_NODE_BIN=%s", envNode)}, searchSummary...)
	}
	searchSummary = append(searchSummary, "PATH: node")

	return "", fmt.Errorf(
		"未找到 Node.js 运行时。当前 Wails 第一阶段版本仍依赖 Node sidecar，请安装 Node.js，或在 exe 同级 runtime\\node\\node.exe 放置 node.exe，或设置 JAV_NODE_BIN。已检查位置：%s",
		strings.Join(searchSummary, " | "),
	)
}

// Start 启动 Node sidecar 子进程。该方法是幂等的：
// - 如果 sidecar 已就绪（ready=true），直接返回成功
// - 如果 sidecar 正在启动中（starting=true），等待其完成
// - 否则启动新进程并通过 system.bootstrap 初始化
// closeReadyChLocked 通过将 readyCh 置 nil 防止重复 close，保证并发安全。
func (m *Manager) Start(ctx context.Context) error {
	// Start is intentionally idempotent. Multiple UI calls can race while the
	// first sidecar process is still bootstrapping; later callers wait on the
	// same ready channel instead of launching another Node process.
	m.mu.Lock()
	if m.ready && m.cmd != nil {
		m.mu.Unlock()
		return nil
	}

	if m.starting {
		readyCh := m.readyCh
		m.mu.Unlock()
		return m.waitForReady(ctx, readyCh)
	}

	m.startRequested = true
	m.starting = true
	m.ready = false
	m.startErr = nil
	m.readyCh = make(chan struct{})
	m.exitDone = make(chan struct{})
	m.exitErr = nil

	scriptPath := filepath.Join(m.repoRoot, "desktop", "sidecar", "index.js")
	if !fileExists(scriptPath) {
		m.mu.Unlock()
		err := fmt.Errorf("未找到 sidecar 启动脚本：%s", scriptPath)
		m.finishStart(err)
		return err
	}

	nodeBin, err := m.resolveNodeCommand()
	if err != nil {
		m.mu.Unlock()
		m.finishStart(err)
		return err
	}

	// The child process must outlive the short startup/bootstrap timeout.
	// Startup ctx only controls readiness waiting; shutdown owns process cancellation.
	processCtx, processCancel := context.WithCancel(context.Background())
	command := exec.CommandContext(processCtx, nodeBin, scriptPath)
	command.Dir = m.repoRoot
	command.Env = append(os.Environ(), "FORCE_COLOR=0")
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	stdin, err := command.StdinPipe()
	if err != nil {
		m.mu.Unlock()
		processCancel()
		m.finishStart(err)
		return err
	}

	stdout, err := command.StdoutPipe()
	if err != nil {
		m.mu.Unlock()
		processCancel()
		m.finishStart(err)
		return err
	}

	stderr, err := command.StderrPipe()
	if err != nil {
		m.mu.Unlock()
		processCancel()
		m.finishStart(err)
		return err
	}

	if err := command.Start(); err != nil {
		m.mu.Unlock()
		processCancel()
		m.finishStart(err)
		return err
	}

	m.cmd = command
	m.stdin = stdin
	m.cancel = processCancel
	exitDone := m.exitDone
	m.mu.Unlock()

	go m.consumeStdout(stdout)
	go m.consumeStderr(stderr)
	go m.waitForExit(command, exitDone)

	bootstrapPayload := map[string]any{
		"runtimeContext": m.paths.ToMap(),
	}
	bootstrapCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	_, err = m.Call(bootstrapCtx, "system", "bootstrap", bootstrapPayload)
	m.finishStart(err)
	return err
}

func (m *Manager) waitForExit(command *exec.Cmd, exitDone chan struct{}) {
	if command == nil {
		return
	}

	rawWaitErr := command.Wait()
	pendingErr := rawWaitErr
	if pendingErr == nil {
		pendingErr = fmt.Errorf("sidecar 进程已退出")
	}

	m.failAllPending(pendingErr)
	m.mu.Lock()
	if m.cmd == command {
		m.cmd = nil
		m.stdin = nil
		m.cancel = nil
		m.ready = false
		m.starting = false
		m.startErr = pendingErr
		m.exitErr = rawWaitErr
		m.closeReadyChLocked()
		if m.exitDone == exitDone && exitDone != nil {
			close(exitDone)
			m.exitDone = nil
		}
	}
	m.mu.Unlock()
}

func (m *Manager) closeReadyChLocked() {
	if m.readyCh == nil {
		return
	}

	close(m.readyCh)
	m.readyCh = nil
}

func (m *Manager) finishStart(err error) {
	var commandToKill *exec.Cmd
	var cancelProcess context.CancelFunc

	m.mu.Lock()
	m.starting = false
	if err != nil {
		m.ready = false
		m.startErr = err
		commandToKill = m.cmd
		cancelProcess = m.cancel
	} else {
		m.ready = true
		m.startErr = nil
	}
	m.closeReadyChLocked()
	m.mu.Unlock()

	if err != nil && commandToKill != nil && commandToKill.Process != nil {
		if cancelProcess != nil {
			cancelProcess()
		}
		_ = commandToKill.Process.Kill()
		// waitForExit is the sole owner of Cmd.Wait and performs final cleanup.
	}
}

func (m *Manager) waitForReady(ctx context.Context, readyCh chan struct{}) error {
	if readyCh != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-readyCh:
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ready {
		return nil
	}

	if m.startErr != nil {
		return m.startErr
	}

	return fmt.Errorf("sidecar 尚未启动")
}

func (m *Manager) ensureReady(ctx context.Context) error {
	m.mu.Lock()
	if m.ready {
		m.mu.Unlock()
		return nil
	}

	if !m.startRequested {
		m.mu.Unlock()
		return fmt.Errorf("sidecar 尚未启动")
	}

	if !m.starting {
		err := m.startErr
		m.mu.Unlock()
		if err != nil {
			return err
		}
		return fmt.Errorf("sidecar 尚未启动")
	}

	readyCh := m.readyCh
	m.mu.Unlock()
	return m.waitForReady(ctx, readyCh)
}

func (m *Manager) IsReady() bool {
	if m == nil {
		return false
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ready && m.cmd != nil && m.stdin != nil
}

func (m *Manager) failAllPending(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, ch := range m.pending {
		// 非阻塞发送：channel 缓冲为 1，若已满（如超时 Call 已 delete 但 channel 未读空），
		// 使用 select+default 避免死锁。
		select {
		case ch <- resultEnvelope{err: err}:
		default:
		}
		close(ch)
		delete(m.pending, id)
	}
}

func (m *Manager) consumeStdout(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	buffer := make([]byte, 0, 64*1024)
	scanner.Buffer(buffer, 8*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		var meta struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(line, &meta); err != nil {
			continue
		}

		switch meta.Kind {
		case "result":
			packet, err := DecodeResult(line)
			if err != nil {
				continue
			}

			m.mu.Lock()
			ch := m.pending[packet.ID]
			if ch != nil {
				delete(m.pending, packet.ID)
			}
			m.mu.Unlock()

			if ch != nil {
				ch <- resultEnvelope{packet: packet}
				close(ch)
			}
		case "event":
			packet, err := DecodeEvent(line)
			if err != nil {
				continue
			}
			m.bus.Publish(packet.Version, packet.Kind, packet.Event, packet.Domain, packet.Action, packet.TaskID, packet.Timestamp, packet.Data)
		}
	}

	if err := scanner.Err(); err != nil {
		m.failAllPending(err)
	}
}

func (m *Manager) consumeStderr(stderr io.Reader) {
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		m.bus.Emit("app.notice", map[string]any{
			"level":     "warn",
			"message":   line,
			"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		})
	}
}

func (m *Manager) nextCommandID() string {
	id := atomic.AddUint64(&m.requestID, 1)
	return fmt.Sprintf("cmd-%d", id)
}

func (m *Manager) Call(ctx context.Context, domain string, action string, payload any) (json.RawMessage, error) {
	if !(domain == "system" && action == "bootstrap") {
		if err := m.ensureReady(ctx); err != nil {
			return nil, err
		}
	}

	m.mu.Lock()
	stdin := m.stdin
	command := m.cmd
	m.mu.Unlock()

	if command == nil || stdin == nil {
		return nil, fmt.Errorf("sidecar 尚未启动")
	}

	commandID := m.nextCommandID()
	responseCh := make(chan resultEnvelope, 1)

	m.mu.Lock()
	m.pending[commandID] = responseCh
	m.mu.Unlock()

	packet := NewCommandPacket(commandID, domain, action, payload)
	line, err := json.Marshal(packet)
	if err != nil {
		m.mu.Lock()
		delete(m.pending, commandID)
		m.mu.Unlock()
		return nil, err
	}

	if _, err := io.WriteString(stdin, string(line)+"\n"); err != nil {
		m.mu.Lock()
		delete(m.pending, commandID)
		m.mu.Unlock()
		return nil, err
	}

	select {
	case <-ctx.Done():
		m.mu.Lock()
		delete(m.pending, commandID)
		m.mu.Unlock()
		return nil, ctx.Err()
	case response := <-responseCh:
		if response.err != nil {
			return nil, response.err
		}
		if err := ResultError(response.packet); err != nil {
			return nil, err
		}
		return response.packet.Data, nil
	}
}

func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	command := m.cmd
	cancelProcess := m.cancel
	exitDone := m.exitDone
	m.mu.Unlock()

	if command == nil {
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, gracefulErr := m.Call(shutdownCtx, "system", "shutdown", map[string]any{})
	if gracefulErr != nil {
		if cancelProcess != nil {
			cancelProcess()
		}
		if command.Process != nil {
			_ = command.Process.Kill()
		}
	}

	// waitForExit exclusively owns Cmd.Wait. Shutdown only waits for its result.
	waitCtx, waitCancel := context.WithTimeout(ctx, 10*time.Second)
	defer waitCancel()
	select {
	case <-waitCtx.Done():
		if cancelProcess != nil {
			cancelProcess()
		}
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		return fmt.Errorf("sidecar shutdown wait timed out: %w", waitCtx.Err())
	case <-exitDone:
		if gracefulErr != nil {
			return fmt.Errorf("sidecar graceful shutdown failed: %w", gracefulErr)
		}
		m.mu.Lock()
		waitErr := m.exitErr
		m.mu.Unlock()
		return waitErr
	}
}
