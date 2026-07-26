package organizer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// legacyReportFileNames keeps pre-split organizer output filenames that may
// still remain in user directories from Electron and transition builds. Cleanup
// stays explicit so current reports are never removed by a broad pattern.
var legacyReportFileNames = []string{
	"\u5220\u9664\u6e05\u5355.txt",
	"\u5e7f\u544a\u9ad8\u98ce\u9669\u756a\u53f7.txt",
	"\u5e7f\u544a\u98ce\u9669\u5206\u7ea7\u660e\u7ec6.txt",
	"\u5e7f\u544a\u9ad8\u98ce\u9669\u78c1\u529b\u8865\u6293.txt",
}

// This file owns filesystem-side helpers for organizer execution.
//
// Boundary rules:
// - file enumeration / move / delete / cleanup live here
// - filename-to-code recognition stays in code_rules.go
// - user-facing summaries stay in run/report files
//
// Ownership summary:
// 1) centralize organizer filesystem enumeration/move/delete helpers
// 2) keep managed-directory and cleanup safety rules in one file
// 3) separate filesystem side effects from code matching and report text
//
// File map for maintainers:
// 1) managed-directory and legacy-report safety helpers
// 2) file enumeration / move / ensure-directory helpers
// 3) cleanup/delete path and stale-artifact handling helpers

func isManagedDirectoryName(value string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, strings.ToLower(batchDeleteStagingPrefix)) {
		return true
	}
	for _, name := range []string{waitingDirName, unmatchedDirName, toDeleteDirName, introAdDirName, logsDirName, stateDirName} {
		if strings.ToLower(name) == trimmed {
			return true
		}
	}
	return false
}

func pathExists(targetPath string) bool {
	_, err := os.Stat(targetPath)
	return err == nil
}

func ensureDirectory(targetPath string) error {
	return os.MkdirAll(targetPath, 0o755)
}

func copyThenRemove(srcPath string, targetPath string) error {
	if err := ensureDirectory(filepath.Dir(targetPath)); err != nil {
		return err
	}

	source, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer source.Close()

	target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(target, source)
	closeErr := target.Close()
	if copyErr != nil {
		_ = os.Remove(targetPath)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(targetPath)
		return closeErr
	}
	if err := os.Remove(srcPath); err != nil {
		return fmt.Errorf("移动文件后删除源文件失败 %s: %w", srcPath, err)
	}
	return nil
}

func moveWithUnique(srcPath string, desiredTargetPath string) (string, error) {
	if err := ensureDirectory(filepath.Dir(desiredTargetPath)); err != nil {
		return "", err
	}

	// Duplicate suffixing happens only at the final move point so earlier phases
	// can reason about the intended target name without filesystem side effects.
	targetPath := desiredTargetPath
	if pathExists(targetPath) {
		extension := filepath.Ext(desiredTargetPath)
		baseName := strings.TrimSuffix(filepath.Base(desiredTargetPath), extension)
		parentDir := filepath.Dir(desiredTargetPath)
		for index := 1; index <= 9999; index++ {
			candidate := filepath.Join(parentDir, baseName+"_DUP"+intToString(index)+extension)
			if !pathExists(candidate) {
				targetPath = candidate
				break
			}
		}
	}

	if err := os.Rename(srcPath, targetPath); err == nil {
		return targetPath, nil
	}
	if err := copyThenRemove(srcPath, targetPath); err != nil {
		return "", err
	}
	return targetPath, nil
}

// moveDirectoryWithUnique mirrors moveWithUnique for whole directory trees.
// Organizer uses it in delete-stage bulk moves so cloud-drive mounts do not get
// hammered by many single-file rename requests when one source folder can move
// as a unit.
func moveDirectoryWithUnique(srcPath string, desiredTargetPath string) (string, error) {
	if err := ensureDirectory(filepath.Dir(desiredTargetPath)); err != nil {
		return "", err
	}

	targetPath := desiredTargetPath
	if pathExists(targetPath) {
		parentDir := filepath.Dir(desiredTargetPath)
		baseName := filepath.Base(desiredTargetPath)
		for index := 1; index <= 9999; index++ {
			candidate := filepath.Join(parentDir, baseName+"_DUP"+intToString(index))
			if !pathExists(candidate) {
				targetPath = candidate
				break
			}
		}
	}

	if err := os.Rename(srcPath, targetPath); err == nil {
		return targetPath, nil
	}
	if err := copyDirectoryThenRemove(srcPath, targetPath); err != nil {
		return "", err
	}
	return targetPath, nil
}

func copyDirectoryThenRemove(srcPath string, targetPath string) error {
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(targetPath, srcInfo.Mode()); err != nil {
		return err
	}

	if err := filepath.Walk(srcPath, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcPath, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(targetPath, rel)
		if info.IsDir() {
			if path == srcPath {
				return nil
			}
			return os.MkdirAll(dst, info.Mode())
		}
		if err := ensureDirectory(filepath.Dir(dst)); err != nil {
			return err
		}
		source, err := os.Open(path)
		if err != nil {
			return err
		}
		defer source.Close()
		target, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		if _, copyErr := io.Copy(target, source); copyErr != nil {
			_ = target.Close()
			_ = os.Remove(dst)
			return copyErr
		}
		if closeErr := target.Close(); closeErr != nil {
			_ = os.Remove(dst)
			return closeErr
		}
		return nil
	}); err != nil {
		_ = os.RemoveAll(targetPath)
		return fmt.Errorf("跨文件系统移动目录失败 %s -> %s: %w", srcPath, targetPath, err)
	}

	if err := os.RemoveAll(srcPath); err != nil {
		return fmt.Errorf("移动目录后删除源目录失败 %s: %w", srcPath, err)
	}
	return nil
}

// collectFiles is the organizer's one filesystem inventory pass. Managed
// directories are excluded here so later phases do not need to keep rechecking
// whether a file was already produced by the organizer itself.
func collectFiles(rootPath string, includeSubdirectories bool, videoExtensionSet map[string]struct{}) []FileEntry {
	files := []FileEntry{}
	var walk func(currentPath string, topDirName string)
	walk = func(currentPath string, topDirName string) {
		entries, err := os.ReadDir(currentPath)
		if err != nil {
			return
		}
		for _, entry := range entries {
			entryPath := filepath.Join(currentPath, entry.Name())
			if entry.Type().IsRegular() {
				relativePath, _ := filepath.Rel(rootPath, entryPath)
				files = append(files, FileEntry{
					Path:         entryPath,
					RelativePath: relativePath,
					TopDirName:   topDirName,
					IsRootLevel:  relativePath != "" && !strings.Contains(relativePath, string(os.PathSeparator)),
					IsVideo:      isVideoFile(entryPath, videoExtensionSet),
				})
				continue
			}
			if !entry.IsDir() {
				continue
			}
			nextTopDirName := topDirName
			if nextTopDirName == "" {
				nextTopDirName = entry.Name()
			}
			if isManagedDirectoryName(nextTopDirName) || isManagedDirectoryName(entry.Name()) {
				continue
			}
			if !includeSubdirectories {
				continue
			}
			walk(entryPath, nextTopDirName)
		}
	}

	walk(rootPath, "")
	sort.Slice(files, func(i int, j int) bool {
		return strings.ToLower(files[i].Path) < strings.ToLower(files[j].Path)
	})
	return files
}

func removeDirectoryWithRetry(targetPath string, maxAttempts int) error {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		lastErr = os.RemoveAll(targetPath)
		if !pathExists(targetPath) {
			return nil
		}
		time.Sleep(time.Duration(40*(attempt+1)) * time.Millisecond)
	}
	if lastErr != nil {
		return fmt.Errorf("删除目录失败 %s: %w", targetPath, lastErr)
	}
	return fmt.Errorf("删除目录后仍可见 %s", targetPath)
}

func cleanupEmptyDirectories(rootPath string, preservedTopDirs map[string]struct{}, logf func(string, string)) int {
	removedCount := 0
	var walk func(currentPath string, isRoot bool)
	walk = func(currentPath string, isRoot bool) {
		entries, err := os.ReadDir(currentPath)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if entry.IsDir() {
				walk(filepath.Join(currentPath, entry.Name()), false)
			}
		}
		if isRoot {
			return
		}
		relativePath, err := filepath.Rel(rootPath, currentPath)
		if err != nil || relativePath == "" || strings.HasPrefix(relativePath, "..") {
			return
		}
		topDirName := strings.Split(relativePath, string(os.PathSeparator))[0]
		if _, preserved := preservedTopDirs[topDirName]; preserved {
			return
		}
		restEntries, err := os.ReadDir(currentPath)
		if err != nil || len(restEntries) > 0 {
			return
		}
		if err := os.Remove(currentPath); err == nil {
			removedCount++
			if logf != nil {
				logf("info", "\u5df2\u5220\u9664\u7a7a\u76ee\u5f55\uff1a"+currentPath)
			}
		}
	}
	walk(rootPath, true)
	return removedCount
}

// cleanupLegacyReportFiles removes explicit pre-split historical artifacts only.
// The cleanup intentionally avoids wildcard deletion so current reports and user
// files cannot be swept away by a broad compatibility rule.
func cleanupLegacyReportFiles(rootPath string, logf func(string, string)) int {
	removedCount := 0
	if rootPath == "" || !filepath.IsAbs(rootPath) {
		return 0
	}
	for _, fileName := range legacyReportFileNames {
		legacyPath := filepath.Join(rootPath, fileName)
		fileInfo, err := os.Stat(legacyPath)
		if err != nil || fileInfo.IsDir() {
			continue
		}
		if err := os.Remove(legacyPath); err != nil {
			if logf != nil {
				logf("warn", fmt.Sprintf("清理历史报告失败：%s，原因：%s", legacyPath, err.Error()))
			}
			continue
		}
		removedCount++
		if logf != nil {
			logf("info", "\u5df2\u6e05\u7406\u5386\u53f2\u62a5\u544a\uff1a"+legacyPath)
		}
	}
	return removedCount
}
