// Ownership summary:
//   This file implements domain logic for the javflow backend.
//
// File map for maintainers:
//   1) Domain-specific types and helpers.
//   2) Internal service implementation.
//

package librarymetadata

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	libraryMetadataLogDirName = "librarymetadata-logs"
	historyFileName           = "history.json"
	libraryLogFilePrefix      = "刮削"
	libraryLogRetentionCount  = 10
	maxHistoryPaths           = 2
)

// LogManager writes media-library scraping logs to a hidden application-data
// directory instead of the user's media root, and keeps only the most recent
// two library root paths to avoid unbounded disk growth.
type LogManager struct {
	mu       sync.Mutex
	basePath string
	// runFiles maps a library root path to its current per-run log file
	// (刮削-<timestamp>.txt), created by InitLog at the start of each scrape.
	runFiles map[string]string
}

// SetBasePath routes media-library logs into the portable installation log
// tree. Empty values retain the legacy per-user fallback for compatibility.
func (m *LogManager) SetBasePath(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.basePath = strings.TrimSpace(path)
}

// logHistoryEntry records one library root path that has been initialized.
type logHistoryEntry struct {
	RootPath  string `json:"rootPath"`
	DirName   string `json:"dirName"`
	CreatedAt int64  `json:"createdAt"`
}

// baseDir returns the hidden directory that contains all library-metadata logs.
func (m *LogManager) baseDir() string {
	if strings.TrimSpace(m.basePath) != "" {
		return filepath.Join(m.basePath, "log", "媒体库刮削")
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = os.TempDir()
	}
	return filepath.Join(configDir, "jav-auto", libraryMetadataLogDirName)
}

// dirNameForRoot returns a stable, filesystem-safe identifier for a root path.
func (m *LogManager) dirNameForRoot(rootPath string) string {
	hash := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(rootPath))))
	return hex.EncodeToString(hash[:])[:12]
}

// logDirForRoot returns the per-root log directory.
func (m *LogManager) logDirForRoot(rootPath string) string {
	return filepath.Join(m.baseDir(), m.dirNameForRoot(rootPath))
}

// historyPath returns the path to the history tracking file.
func (m *LogManager) historyPath() string {
	return filepath.Join(m.baseDir(), historyFileName)
}

// readHistory loads the persisted history list.
func (m *LogManager) readHistory() ([]logHistoryEntry, error) {
	path := m.historyPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []logHistoryEntry{}, nil
		}
		return nil, err
	}

	var entries []logHistoryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// writeHistory persists the history list.
func (m *LogManager) writeHistory(entries []logHistoryEntry) error {
	if err := os.MkdirAll(m.baseDir(), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(m.historyPath(), data, 0o644)
}

// InitLog prepares the log directory for the given library root path and
// prunes logs from older root paths when more than maxHistoryPaths are kept.
// It returns the log directory path for the active root.
func (m *LogManager) InitLog(rootPath string) (string, error) {
	rootPath = strings.TrimSpace(rootPath)
	if rootPath == "" {
		return "", fmt.Errorf("library root path is empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	entries, err := m.readHistory()
	if err != nil {
		return "", err
	}

	targetDirName := m.dirNameForRoot(rootPath)
	logDir := m.logDirForRoot(rootPath)

	// Remove any existing entry for the same root path; it will be re-added as
	// the most recent entry below.
	filtered := make([]logHistoryEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.EqualFold(strings.TrimSpace(entry.RootPath), rootPath) {
			continue
		}
		filtered = append(filtered, entry)
	}

	// If we are about to exceed the limit, drop the oldest directory.
	if len(filtered) >= maxHistoryPaths {
		sort.SliceStable(filtered, func(i, j int) bool {
			return filtered[i].CreatedAt < filtered[j].CreatedAt
		})
		for len(filtered) >= maxHistoryPaths {
			oldest := filtered[0]
			oldestDir := filepath.Join(m.baseDir(), oldest.DirName)
			_ = os.RemoveAll(oldestDir)
			filtered = filtered[1:]
		}
	}

	filtered = append(filtered, logHistoryEntry{
		RootPath:  rootPath,
		DirName:   targetDirName,
		CreatedAt: time.Now().Unix(),
	})

	if err := m.writeHistory(filtered); err != nil {
		return "", err
	}

	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return "", err
	}

	// 单份日志策略：每次刮削运行创建一个 刮削-<时间戳>.txt，目录内只保留
	// 最近 libraryLogRetentionCount 份。
	if m.runFiles == nil {
		m.runFiles = map[string]string{}
	}
	runPath := filepath.Join(logDir, fmt.Sprintf("%s-%s.txt", libraryLogFilePrefix, time.Now().Format("20060102-150405")))
	if err := os.WriteFile(runPath, []byte(""), 0o644); err != nil {
		return "", err
	}
	m.runFiles[rootPath] = runPath
	pruneLibraryRunLogs(logDir, libraryLogRetentionCount)

	return logDir, nil
}

// pruneLibraryRunLogs keeps only the newest `keep` 刮削-*.txt run files in one
// root's log directory (by modification time).
func pruneLibraryRunLogs(logDir string, keep int) {
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return
	}
	type logFile struct {
		path    string
		modTime time.Time
	}
	matches := make([]logFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), libraryLogFilePrefix) || !strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		matches = append(matches, logFile{path: filepath.Join(logDir, entry.Name()), modTime: info.ModTime()})
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

// AppendLog writes one timestamped line to both the rotating session log and
// the latest.txt summary for the given library root path.
func (m *LogManager) AppendLog(rootPath, line string) error {
	rootPath = strings.TrimSpace(rootPath)
	if rootPath == "" {
		return fmt.Errorf("library root path is empty")
	}
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	logDir := m.logDirForRoot(rootPath)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return err
	}

	timestamped := fmt.Sprintf("[%s] %s\n", time.Now().Format("15:04:05"), line)

	if m.runFiles == nil {
		m.runFiles = map[string]string{}
	}
	runPath := m.runFiles[rootPath]
	if runPath == "" {
		// 兜底：未经过 InitLog 的调用也应有落点（惰性建一份运行文件）。
		logDir := m.logDirForRoot(rootPath)
		if err := os.MkdirAll(logDir, 0o755); err != nil {
			return err
		}
		runPath = filepath.Join(logDir, fmt.Sprintf("%s-%s.txt", libraryLogFilePrefix, time.Now().Format("20060102-150405")))
		m.runFiles[rootPath] = runPath
	}
	return appendToFile(runPath, timestamped)
}

// OpenLogFolder returns the log directory for the given root path, creating it
// if necessary. Callers can open this directory with the desktop shell service.
func (m *LogManager) OpenLogFolder(rootPath string) (string, error) {
	rootPath = strings.TrimSpace(rootPath)
	if rootPath == "" {
		return "", fmt.Errorf("library root path is empty")
	}

	logDir := m.logDirForRoot(rootPath)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return "", err
	}
	return logDir, nil
}

func appendToFile(path, text string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(text)
	return err
}
