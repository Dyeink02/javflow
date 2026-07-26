// Ownership summary:
//   This file provides per-module, session-scoped log directories for the application.
//
// File map for maintainers:
//   1) Module name constants.
//   2) Directory and session-path resolution.
//   3) Line-oriented log appending helpers.
//
package modulelog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Module names are deliberately user-facing Chinese names. They are part of
// the portable application's on-disk log contract.
const (
	JAVCrawl     = "JAV\u722c\u53d6"
	Organizer    = "\u89c6\u9891\u6574\u7406"
	Subscription = "AV\u8ba2\u9605"
	Library      = "\u5a92\u4f53\u5e93\u522e\u524a"
)

var (
	writeMu      sync.Mutex
	sessionPaths = map[string]string{}
)

func Directory(appPath, module string) string {
	if strings.TrimSpace(appPath) == "" || strings.TrimSpace(module) == "" {
		return ""
	}
	return filepath.Join(appPath, "log", module)
}

func DailyPath(appPath, module string, now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	return filepath.Join(Directory(appPath, module), fmt.Sprintf("%s-%s.txt", module, now.Format("2006\u5e741\u67082\u65e5")))
}

func SessionPath(appPath, module string, now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	return filepath.Join(Directory(appPath, module), fmt.Sprintf("\u8fd0\u884c\u65e5\u5fd7-%s.txt", now.Format("20060102-150405")))
}

func Append(appPath, module, level, message string, now time.Time) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	dir := Directory(appPath, module)
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	line := fmt.Sprintf("[%s] [%s] %s\r\n", now.Format("2006-01-02 15:04:05"), strings.ToUpper(strings.TrimSpace(level)), message)
	writeMu.Lock()
	defer writeMu.Unlock()
	sessionPath := sessionPaths[dir]
	if sessionPath == "" {
		sessionPath = SessionPath(appPath, module, now)
		sessionPaths[dir] = sessionPath
	}
	paths := []string{
		DailyPath(appPath, module, now),
		sessionPath,
		filepath.Join(dir, "\u8fd0\u884c\u65e5\u5fd7.txt"),
	}
	for _, path := range paths {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		_, writeErr := file.WriteString(line)
		_ = file.Close()
		if writeErr != nil {
			return writeErr
		}
	}
	return nil
}
