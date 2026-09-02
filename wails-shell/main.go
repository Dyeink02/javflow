package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"golang.org/x/sys/windows"
	"javflow/internal/appupdate"
)

//go:embed all:frontend
var assets embed.FS

func isRepoRoot(candidate string) bool {
	if strings.TrimSpace(candidate) == "" {
		return false
	}

	packageFile := filepath.Join(candidate, "package.json")

	if info, err := os.Stat(packageFile); err != nil || info.IsDir() {
		return false
	}

	return true
}

func resolveRepoRoot() (string, error) {
	candidates := make([]string, 0, 4)

	if envRoot := strings.TrimSpace(os.Getenv("JAVFLOW_REPO_ROOT")); envRoot != "" {
		candidates = append(candidates, envRoot)
	}

	if executablePath, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(executablePath))
	}

	if workingDir, err := os.Getwd(); err == nil {
		candidates = append(candidates, workingDir)
	}

	candidates = append(candidates, ".")

	visited := map[string]struct{}{}
	for _, candidate := range candidates {
		absoluteCandidate, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}

		current := absoluteCandidate
		for depth := 0; depth < 5; depth += 1 {
			if _, seen := visited[current]; !seen {
				visited[current] = struct{}{}
				if isRepoRoot(current) {
					return current, nil
				}
			}

			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}

	return "", fmt.Errorf("无法定位 JavFlow 项目根目录，请确保 exe 仍位于源码目录内，或手动设置 JAVFLOW_REPO_ROOT")
}

func main() {
	if handled, helperErr := appupdate.RunUpdateHelper(os.Args[1:]); handled {
		if helperErr != nil {
			log.Printf("JavFlow 更新辅助进程失败：%v", helperErr)
			os.Exit(1)
		}
		return
	}

	// Redirect this process' stdout/stderr into a log file BEFORE anything else
	// can panic. Go runtime panics in ANY goroutine write to stderr; without
	// this redirect a desktop launch loses them, which is exactly why
	// "the window closed by itself" used to leave no evidence.
	logFilePath := redirectProcessOutput()
	log.SetOutput(os.Stderr)

	// Capture unexpected panics into a crash log beside the log directory so a
	// silent window close (e.g. after task completion) always leaves evidence.
	defer func() {
		if recovered := recover(); recovered != nil {
			writeCrashLog(recovered, debug.Stack())
			panic(recovered)
		}
	}()
	log.Printf("JavFlow 启动，版本 %s，stderr 日志：%s", appupdate.ProductVersion(), logFilePath)

	repoRoot, err := resolveRepoRoot()
	if err != nil {
		log.Fatal(err)
	}

	app := NewApp(repoRoot)

	err = wails.Run(&options.App{
		Title:     "JavFlow",
		Width:     1440,
		Height:    920,
		MinWidth:  1120,
		MinHeight: 720,
		AssetServer: &assetserver.Options{
			Assets:  assets,
			Handler: app.subscriptionMediaHandler(),
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		writeCrashLog("wails.Run returned error: "+err.Error(), debug.Stack())
		log.Fatal(err)
	}
}

// stderrLogPath resolves the diagnostic log path next to the running
// executable (installer dirs carry a Users-modify ACL; portable dirs are
// user-owned), falling back to the system temp directory.
func stderrLogPath() string {
	base := "javflow-stderr.log"
	dir := "log"
	if executable, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(executable), dir, base)
	}
	return filepath.Join(os.TempDir(), base)
}

// redirectProcessOutput points the process-wide stdout/stderr handles at a log
// file so runtime panics from any goroutine are captured on disk.
func redirectProcessOutput() string {
	logFilePath := stderrLogPath()
	if err := os.MkdirAll(filepath.Dir(logFilePath), 0o755); err != nil {
		return ""
	}
	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return ""
	}
	handle := windows.Handle(logFile.Fd())
	_ = windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, handle)
	_ = windows.SetStdHandle(windows.STD_ERROR_HANDLE, handle)
	os.Stdout = os.NewFile(uintptr(handle), "stdout")
	os.Stderr = os.NewFile(uintptr(handle), "stderr")
	return logFilePath
}

// writeCrashLog records panic details so "the window closed by itself" reports
// always come with a traceable reason instead of silent exits.
func writeCrashLog(recovered any, stack []byte) {
	directory := filepath.Dir(stderrLogPath())
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return
	}
	payload := fmt.Sprintf("time: %s\npanic: %v\nstack:\n%s\n", time.Now().Format("2006-01-02 15:04:05"), recovered, stack)
	_ = os.WriteFile(filepath.Join(directory, fmt.Sprintf("crash-%s.log", time.Now().Format("20060102-150405"))), []byte(payload), 0o644)
}
