// Portable replacement helpers own the process handoff after an update file
// has already been downloaded and verified.
//
// Ownership summary:
// 1) stage a helper copy without locking the running EXE replacement target
// 2) replace the portable package in place with backup/restore protection
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
	"sort"
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
	if filepath.Clean(filepath.Dir(targetPath)) != filepath.Clean(filepath.Dir(pending.TargetPath)) {
		return ApplyResult{}, fmt.Errorf("当前便携包目录已变化，请重新检查并下载更新")
	}
	if err := validatePortablePackageDirectory(pending.StagedDirectory); err != nil {
		return ApplyResult{}, err
	}
	actualPackageSHA, err := packageTreeSHA256(pending.StagedDirectory)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("无法复核便携包：%w", err)
	}
	if !strings.EqualFold(actualPackageSHA, pending.PackageSHA256) {
		return ApplyResult{}, fmt.Errorf("便携包在应用前校验失败，请重新下载")
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
		"--staged-dir", pending.StagedDirectory,
		"--backup", backupPath,
		"--package-sha256", pending.PackageSHA256,
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
		Message:         "便携包更新已准备，应用将保持原路径重启。",
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
	stagedDirectory := flags.String("staged-dir", "", "")
	backupPath := flags.String("backup", "", "")
	expectedPackageSHA := flags.String("package-sha256", "", "")
	if parseErr := flags.Parse(args); parseErr != nil {
		return true, parseErr
	}
	if !*marker {
		return true, fmt.Errorf("更新辅助进程标记无效")
	}
	for _, value := range []*string{sourcePath, stagedDirectory, backupPath, expectedPackageSHA} {
		if strings.TrimSpace(*value) == "" {
			return true, fmt.Errorf("更新辅助进程参数不完整")
		}
	}

	return true, runUpdateHelper(updateHelperOptions{
		sourcePath:         *sourcePath,
		stagedDirectory:    *stagedDirectory,
		backupPath:         *backupPath,
		expectedPackageSHA: *expectedPackageSHA,
	})
}

type updateHelperOptions struct {
	sourcePath         string
	stagedDirectory    string
	backupPath         string
	expectedPackageSHA string
}

func runUpdateHelper(options updateHelperOptions) error {
	sourcePath, err := filepath.Abs(options.sourcePath)
	if err != nil {
		return err
	}
	stagedDirectory, err := filepath.Abs(options.stagedDirectory)
	if err != nil {
		return err
	}
	backupPath, err := filepath.Abs(options.backupPath)
	if err != nil {
		return err
	}
	if filepath.Clean(filepath.Dir(sourcePath)) == filepath.Clean(stagedDirectory) {
		return fmt.Errorf("便携包临时目录不能是当前程序目录")
	}

	// Give the parent Wails process its normal shutdown window before the first
	// rename attempt. Windows will reject the rename while the old image is open.
	time.Sleep(500 * time.Millisecond)
	deadline := time.Now().Add(replaceTimeout)
	var lastErr error
	for time.Now().Before(deadline) {
		lastErr = replacePortablePackage(sourcePath, stagedDirectory, backupPath, options.expectedPackageSHA)
		if lastErr == nil {
			return startUpdatedExecutable(sourcePath)
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("更新替换超时：%w", lastErr)
}

func replacePortablePackage(sourcePath string, stagedDirectory string, backupPath string, expectedPackageSHA string) error {
	if err := validatePortablePackageDirectory(stagedDirectory); err != nil {
		return err
	}
	actualPackageSHA, err := packageTreeSHA256(stagedDirectory)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actualPackageSHA, normalizeSHA256(expectedPackageSHA)) {
		return fmt.Errorf("辅助进程复核便携包 SHA-256 失败")
	}
	if _, err := os.Stat(sourcePath); err != nil {
		return err
	}

	files, err := packageFiles(stagedDirectory)
	if err != nil {
		return err
	}
	backupDirectory := backupPath + ".files"
	_ = os.RemoveAll(backupDirectory)
	if err := os.MkdirAll(backupDirectory, 0o755); err != nil {
		return err
	}
	backedUp := make([]string, 0, len(files))
	installed := make([]string, 0, len(files))
	rollback := func() {
		for _, relative := range installed {
			_ = os.Remove(filepath.Join(filepath.Dir(sourcePath), filepath.FromSlash(relative)))
		}
		for _, relative := range backedUp {
			backupFile := filepath.Join(backupDirectory, filepath.FromSlash(relative))
			targetFile := filepath.Join(filepath.Dir(sourcePath), filepath.FromSlash(relative))
			_ = os.MkdirAll(filepath.Dir(targetFile), 0o755)
			_ = os.Remove(targetFile)
			_ = os.Rename(backupFile, targetFile)
		}
		if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
			_ = os.Rename(backupPath, sourcePath)
		}
		_ = os.RemoveAll(backupDirectory)
	}

	for _, relative := range files {
		targetFile := filepath.Join(filepath.Dir(sourcePath), filepath.FromSlash(relative))
		if !pathWithinDirectory(filepath.Dir(sourcePath), targetFile) {
			rollback()
			return fmt.Errorf("便携包目标路径越界：%s", relative)
		}
		if relative == "javflow.exe" {
			continue
		}
		if info, statErr := os.Stat(targetFile); statErr == nil {
			if info.IsDir() {
				rollback()
				return fmt.Errorf("便携包目标路径是目录：%s", relative)
			}
			backupFile := filepath.Join(backupDirectory, filepath.FromSlash(relative))
			if err := copyFile(targetFile, backupFile, 0o644); err != nil {
				rollback()
				return err
			}
			backedUp = append(backedUp, relative)
		} else if !os.IsNotExist(statErr) {
			rollback()
			return statErr
		}
	}

	if err := os.Rename(sourcePath, backupPath); err != nil {
		rollback()
		return err
	}
	for _, relative := range files {
		targetFile := filepath.Join(filepath.Dir(sourcePath), filepath.FromSlash(relative))
		stagedFile := filepath.Join(stagedDirectory, filepath.FromSlash(relative))
		if relative != "javflow.exe" {
			if err := os.Remove(targetFile); err != nil && !os.IsNotExist(err) {
				rollback()
				return err
			}
		}
		if err := os.MkdirAll(filepath.Dir(targetFile), 0o755); err != nil {
			rollback()
			return err
		}
		if err := os.Rename(stagedFile, targetFile); err != nil {
			rollback()
			return err
		}
		installed = append(installed, relative)
	}
	_ = os.RemoveAll(backupDirectory)
	_ = os.RemoveAll(stagedDirectory)
	return nil
}

func validatePortablePackageDirectory(directory string) error {
	if strings.TrimSpace(directory) == "" {
		return fmt.Errorf("便携包临时目录为空")
	}
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() {
		if err == nil {
			err = fmt.Errorf("路径不是目录")
		}
		return fmt.Errorf("便携包临时目录不可用：%w", err)
	}
	executablePath := filepath.Join(directory, "javflow.exe")
	return validatePortableExecutable(executablePath)
}

func packageFiles(directory string) ([]string, error) {
	files := make([]string, 0)
	err := filepath.WalkDir(directory, func(pathValue string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("便携包临时目录不允许包含符号链接：%s", pathValue)
		}
		relative, err := filepath.Rel(directory, pathValue)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func copyFile(sourcePath string, targetPath string, mode os.FileMode) error {
	input, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	output, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
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
