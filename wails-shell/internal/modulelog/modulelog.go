// Ownership summary:
//   This file provides per-module, one-file-per-run logs for the application.
//   Each module (crawler/organizer/scraper/subscription) keeps its own single
//   timestamped file per run; older files are pruned to a fixed retention.
//
// File map for maintainers:
//   1) Module name constants and per-module file prefixes.
//   2) Run-file lifecycle (BeginRun/Append/LatestRunPath).
//   3) Retention pruning.
//
package modulelog

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Module names are deliberately user-facing Chinese names. They are part of
// the portable application's on-disk log contract (directory names).
const (
	JAVCrawl     = "JAV\u722c\u53d6"
	Organizer    = "\u89c6\u9891\u6574\u7406"
	Subscription = "AV\u8ba2\u9605"
	Library      = "\u5a92\u4f53\u5e93\u522e\u524a"
)

// logRetentionCount keeps the newest N run files per module so one-file-per-run
// naming cannot grow the log tree without bound.
const logRetentionCount = 10

// filePrefix maps a module to the short file-name prefix users approved
// (爬虫/整理/订阅/刮削). Directory names keep the full module names.
var filePrefix = map[string]string{
	JAVCrawl:     "\u722c\u866b",
	Organizer:    "\u6574\u7406",
	Subscription: "\u8ba2\u9605",
	Library:      "\u522e\u524a",
}

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

// BeginRun starts one per-run log file named `<prefix>-<timestamp>.txt` and
// prunes older files of the same module, keeping the newest logRetentionCount.
// It returns the created file path. Subsequent Append calls go to this file
// until the next BeginRun (or process restart with a lazy BeginRun).
func BeginRun(appPath, module string, now time.Time) (string, error) {
	dir := Directory(appPath, module)
	if dir == "" {
		return "", nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	prefix, ok := filePrefix[module]
	if !ok {
		prefix = module
	}
	runPath := filepath.Join(dir, fmt.Sprintf("%s-%s.txt", prefix, now.Format("20060102-150405")))
	if err := os.WriteFile(runPath, []byte(""), 0o644); err != nil {
		return "", err
	}
	pruneRunFiles(dir, prefix, logRetentionCount)

	writeMu.Lock()
	sessionPaths[dir] = runPath
	writeMu.Unlock()
	return runPath, nil
}

// Append writes one line into the module's current run file. When no run has
// begun for this module in the current process, a run file is started lazily
// so important diagnostics are never dropped.
func Append(appPath, module, level, message string, now time.Time) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}
	dir := Directory(appPath, module)
	if dir == "" {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	writeMu.Lock()
	runPath := sessionPaths[dir]
	writeMu.Unlock()
	if runPath == "" {
		created, err := BeginRun(appPath, module, now)
		if err != nil {
			return err
		}
		runPath = created
	}

	line := fmt.Sprintf("[%s] [%s] %s\r\n", now.Format("2006-01-02 15:04:05"), strings.ToUpper(strings.TrimSpace(level)), message)
	file, err := os.OpenFile(runPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(line)
	_ = file.Close()
	return writeErr
}

// LatestRunPath returns the newest run file of a module ("" when none exists).
// Consumers that used to read a fixed-name pointer file should call this to
// locate the current log.
func LatestRunPath(appPath, module string) string {
	dir := Directory(appPath, module)
	if dir == "" {
		return ""
	}
	prefix, ok := filePrefix[module]
	if !ok {
		prefix = module
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	type logFile struct {
		path    string
		modTime time.Time
	}
	var newest *logFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) || !strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if newest == nil || info.ModTime().After(newest.modTime) {
			newest = &logFile{path: filepath.Join(dir, entry.Name()), modTime: info.ModTime()}
		}
	}
	if newest == nil {
		return ""
	}
	return newest.path
}

func pruneRunFiles(dir string, prefix string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type logFile struct {
		path    string
		modTime time.Time
	}
	matches := make([]logFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) || !strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		matches = append(matches, logFile{path: filepath.Join(dir, entry.Name()), modTime: info.ModTime()})
	}
	if len(matches) <= keep {
		return
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].modTime.After(matches[j].modTime)
	})
	for _, stale := range matches[keep:] {
		_ = os.Remove(stale.path)
	}
}
