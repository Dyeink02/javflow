// Portable replacement helpers own the process handoff after an update file
// has already been downloaded and verified.
//
// Ownership summary:
// 1) stage a helper copy without locking the running EXE replacement target
// 2) replace the executable with backup/restore protection
// 3) restart the updated portable application and keep the previous EXE
//
// File map for maintainers:
// 1) service apply handoff
// 2) private helper command parsing and replacement loop
// 3) executable copy/backup/path utilities
package appupdate

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Apply stages a verified update through a short-lived copy of the running
// executable. The copy is essential on Windows: a process cannot replace the
// EXE image that currently owns its file handle.
func (s *Service) Apply(ctx context.Context) (ApplyResult, error) {
	if s == nil {
		return ApplyResult{}, fmt.Errorf("在线更新服务未初始化")
	}
	if err := normalizeContext(ctx).Err(); err != nil {
		return ApplyResult{}, err
	}

	s.mu.Lock()
	if s.pending == nil {
		s.mu.Unlock()
		return ApplyResult{}, fmt.Errorf("尚未下载可用的更新文件")
	}
	pending := *s.pending
	s.mu.Unlock()
	if strings.TrimSpace(pending.DownloadedPath) == "" {
		return ApplyResult{}, fmt.Errorf("尚未下载可用的更新文件")
	}

	targetPath, err := s.runningExecutablePath()
	if err != nil {
		return ApplyResult{}, err
	}
	if filepath.Clean(targetPath) != filepath.Clean(pending.TargetPath) {
		return ApplyResult{}, fmt.Errorf("当前运行位置已变化，请重新检查并下载更新")
	}
	if err := validatePortableExecutable(pending.DownloadedPath); err != nil {
		return ApplyResult{}, err
	}
	actualChecksum, err := fileSHA256(pending.DownloadedPath)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("无法复核更新文件：%w", err)
	}
	if !strings.EqualFold(actualChecksum, pending.SHA256) {
		return ApplyResult{}, fmt.Errorf("更新文件在应用前校验失败，请重新下载")
	}

	helperPath, err := copyRunningExecutableToTemp()
	if err != nil {
		return ApplyResult{}, err
	}
	backupPath := uniqueBackupPath(targetPath)
	command := exec.Command(
		helperPath,
		"--javflow-update-helper",
		"--source", targetPath,
		"--replacement", pending.DownloadedPath,
		"--backup", backupPath,
		"--sha256", pending.SHA256,
	)
	command.Dir = filepath.Dir(targetPath)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		_ = os.Remove(helperPath)
		return ApplyResult{}, fmt.Errorf("无法启动更新辅助进程：%w", err)
	}
	_ = command.Process.Release()

	s.mu.Lock()
	s.pending = nil
	s.mu.Unlock()

	return ApplyResult{
		Applied:         true,
		RestartRequired: true,
		TargetPath:      targetPath,
		BackupPath:      backupPath,
		Message:         "更新已准备，应用即将重启。",
	}, nil
}

// RunUpdateHelper handles the private command line used by the replacement
// process. It returns handled=false for an ordinary JavFlow launch.
func RunUpdateHelper(args []string) (handled bool, err error) {
	if !containsArgument(args, "--javflow-update-helper") {
		return false, nil
	}

	flags := flag.NewFlagSet("javflow-update-helper", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	marker := flags.Bool("javflow-update-helper", false, "")
	sourcePath := flags.String("source", "", "")
	replacementPath := flags.String("replacement", "", "")
	backupPath := flags.String("backup", "", "")
	expectedSHA := flags.String("sha256", "", "")
	if parseErr := flags.Parse(args); parseErr != nil {
		return true, parseErr
	}
	if !*marker {
		return true, fmt.Errorf("更新辅助进程标记无效")
	}
	for _, value := range []*string{sourcePath, replacementPath, backupPath, expectedSHA} {
		if strings.TrimSpace(*value) == "" {
			return true, fmt.Errorf("更新辅助进程参数不完整")
		}
	}

	return true, runUpdateHelper(updateHelperOptions{
		sourcePath:      *sourcePath,
		replacementPath: *replacementPath,
		backupPath:      *backupPath,
		expectedSHA:     *expectedSHA,
	})
}

type updateHelperOptions struct {
	sourcePath      string
	replacementPath string
	backupPath      string
	expectedSHA     string
}

func runUpdateHelper(options updateHelperOptions) error {
	sourcePath, err := filepath.Abs(options.sourcePath)
	if err != nil {
		return err
	}
	replacementPath, err := filepath.Abs(options.replacementPath)
	if err != nil {
		return err
	}
	backupPath, err := filepath.Abs(options.backupPath)
	if err != nil {
		return err
	}
	if filepath.Clean(sourcePath) == filepath.Clean(replacementPath) {
		return fmt.Errorf("更新源文件与目标文件不能相同")
	}

	// Give the parent Wails process its normal shutdown window before the first
	// rename attempt. Windows will reject the rename while the old image is open.
	time.Sleep(500 * time.Millisecond)
	deadline := time.Now().Add(replaceTimeout)
	var lastErr error
	for time.Now().Before(deadline) {
		lastErr = replaceExecutable(sourcePath, replacementPath, backupPath, options.expectedSHA)
		if lastErr == nil {
			return startUpdatedExecutable(sourcePath)
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("更新替换超时：%w", lastErr)
}

func replaceExecutable(sourcePath string, replacementPath string, backupPath string, expectedSHA string) error {
	if err := validatePortableExecutable(replacementPath); err != nil {
		return err
	}
	actualSHA, err := fileSHA256(replacementPath)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actualSHA, normalizeSHA256(expectedSHA)) {
		return fmt.Errorf("辅助进程复核更新文件 SHA-256 失败")
	}
	if _, err := os.Stat(sourcePath); err != nil {
		return err
	}
	if err := os.Rename(sourcePath, backupPath); err != nil {
		return err
	}
	if err := os.Rename(replacementPath, sourcePath); err != nil {
		// Best-effort restore keeps a failed update from leaving the portable
		// directory without its original executable.
		_ = os.Rename(backupPath, sourcePath)
		return err
	}
	return nil
}

func startUpdatedExecutable(sourcePath string) error {
	command := exec.Command(sourcePath)
	command.Dir = filepath.Dir(sourcePath)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		// The replacement is already in place. The backup is intentionally kept
		// for manual rollback rather than silently deleting the previous build.
		return fmt.Errorf("更新后启动新版本失败：%w", err)
	}
	_ = command.Process.Release()
	return nil
}

func copyRunningExecutableToTemp() (string, error) {
	sourcePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("无法定位当前 EXE：%w", err)
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return "", fmt.Errorf("无法读取当前 EXE：%w", err)
	}
	defer source.Close()

	temporary, err := os.CreateTemp(os.TempDir(), "javflow-updater-*.exe")
	if err != nil {
		return "", fmt.Errorf("无法创建更新辅助进程：%w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
	}()
	if _, err := io.Copy(temporary, source); err != nil {
		_ = os.Remove(temporaryPath)
		return "", fmt.Errorf("复制更新辅助进程失败：%w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = os.Remove(temporaryPath)
		return "", fmt.Errorf("更新辅助进程落盘失败：%w", err)
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return "", fmt.Errorf("关闭更新辅助进程失败：%w", err)
	}
	return temporaryPath, nil
}

func uniqueBackupPath(sourcePath string) string {
	directory := filepath.Dir(sourcePath)
	base := filepath.Base(sourcePath)
	extension := filepath.Ext(base)
	stem := strings.TrimSuffix(base, extension)
	candidate := filepath.Join(directory, stem+".previous"+extension)
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate
	}
	return filepath.Join(directory, fmt.Sprintf("%s.previous-%d%s", stem, time.Now().UnixNano(), extension))
}

func containsArgument(args []string, expected string) bool {
	for _, arg := range args {
		if strings.EqualFold(strings.TrimSpace(arg), expected) {
			return true
		}
	}
	return false
}
