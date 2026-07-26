// Ownership summary:
//   This file implements domain logic for the javflow backend.
//
// File map for maintainers:
//   1) Domain-specific types and helpers.
//   2) Internal service implementation.
//

package bridge

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"javflow/internal/common"
	"javflow/internal/modulelog"
)

func resolveSubscriptionV2RootDir(appPath string) string {
	base := strings.TrimSpace(appPath)
	if base == "" {
		return ""
	}
	return filepath.Join(base, "AV订阅")
}

func resolveSubscriptionV2LogDir(appPath string) string {
	base := strings.TrimSpace(appPath)
	if base == "" {
		return ""
	}
	return modulelog.Directory(base, modulelog.Subscription)
}

func sanitizeSubscriptionOutputName(value string) string {
	cleaned := strings.TrimSpace(value)
	cleaned = strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	).Replace(cleaned)
	cleaned = strings.TrimRight(cleaned, ". ")
	if cleaned == "" {
		return "未命名订阅"
	}
	return cleaned
}

func resolveSubscriptionV2DefaultOutputDir(rootPath string, actressName string) string {
	root := resolveSubscriptionV2RootDir(rootPath)
	if root == "" {
		return ""
	}
	return filepath.Join(root, sanitizeSubscriptionOutputName(actressName))
}

// prepareSubscriptionV2OutputDir removes obsolete crawler by-products while
// retaining a previous dated TXT until the next update completes successfully.
func prepareSubscriptionV2OutputDir(appPath string, actressName string) (string, error) {
	root := resolveSubscriptionV2RootDir(appPath)
	outputDir := resolveSubscriptionV2DefaultOutputDir(appPath, actressName)
	if root == "" || outputDir == "" {
		return "", fmt.Errorf("无法解析 AV 订阅输出目录")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	if err := cleanupSubscriptionRootArtifacts(root); err != nil {
		return "", err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", err
	}
	if err := cleanupSubscriptionActorArtifacts(outputDir, ""); err != nil {
		return "", err
	}
	if logDir := resolveSubscriptionV2LogDir(appPath); logDir != "" {
		if err := os.MkdirAll(logDir, 0o755); err != nil {
			return "", err
		}
	}
	return outputDir, nil
}

func cleanupSubscriptionRootArtifacts(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		target := filepath.Join(root, entry.Name())
		if !entry.IsDir() {
			if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
		lowerName := strings.ToLower(strings.TrimSpace(entry.Name()))
		if lowerName == "logs" || strings.HasPrefix(lowerName, "run-") {
			if err := os.RemoveAll(target); err != nil {
				return err
			}
		}
	}
	return nil
}

// cleanupSubscriptionActorArtifacts keeps dated TXT exports during startup,
// or only keepName during successful finalization. All crawler JSON, generic
// magnet files, logs, reports, and run directories are managed by this folder
// and are intentionally removed from the user-visible output.
func cleanupSubscriptionActorArtifacts(outputDir string, keepName string) error {
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if keepName != "" && strings.EqualFold(entry.Name(), keepName) {
			continue
		}
		if keepName == "" && isSubscriptionDatedTextFile(entry.Name()) && !entry.IsDir() {
			continue
		}
		if err := os.RemoveAll(filepath.Join(outputDir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func writeSubscriptionDatedMagnetFile(outputDir string, magnetLines []string, completedCount int, now time.Time) (string, error) {
	if now.IsZero() {
		now = time.Now()
	}
	if completedCount <= 0 || len(magnetLines) == 0 {
		return "", nil
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", err
	}
	fileName := fmt.Sprintf("%d\u5e74%d\u6708%d\u53f7\u66f4\u65b0%d\u90e8.txt", now.Year(), int(now.Month()), now.Day(), completedCount)
	filePath := filepath.Join(outputDir, fileName)
	content := strings.Join(magnetLines, "\r\n")
	if content != "" {
		content += "\r\n"
	}
	if err := common.WriteUTF8TextFile(filePath, content); err != nil {
		return "", err
	}
	if err := cleanupSubscriptionActorArtifacts(outputDir, fileName); err != nil {
		return "", err
	}
	return filePath, nil
}

func isSubscriptionDatedTextFile(fileName string) bool {
	_, ok := parseSubscriptionDateFileName(fileName)
	return ok
}

func parseSubscriptionDateFileName(fileName string) (time.Time, bool) {
	base := strings.TrimSuffix(strings.TrimSpace(fileName), filepath.Ext(fileName))
	if !strings.EqualFold(filepath.Ext(fileName), ".txt") {
		return time.Time{}, false
	}
	var year, month, day, count int
	if _, err := fmt.Sscanf(base, "%d\u5e74%d\u6708%d\u53f7\u66f4\u65b0%d\u90e8", &year, &month, &day, &count); err == nil && count > 0 {
		parsed := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.Local)
		if parsed.Year() == year && int(parsed.Month()) == month && parsed.Day() == day {
			return parsed, true
		}
	}
	parts := strings.Split(base, ".")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	year, yearErr := strconv.Atoi(parts[0])
	month, monthErr := strconv.Atoi(parts[1])
	day, dayErr := strconv.Atoi(parts[2])
	if yearErr != nil || monthErr != nil || dayErr != nil || year < 2000 || month < 1 || month > 12 || day < 1 || day > 31 {
		return time.Time{}, false
	}
	parsed := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.Local)
	if parsed.Year() != year || int(parsed.Month()) != month || parsed.Day() != day {
		return time.Time{}, false
	}
	return parsed, true
}

func latestSubscriptionDatedTextFile(outputDir string) string {
	entries, err := os.ReadDir(strings.TrimSpace(outputDir))
	if err != nil {
		return ""
	}
	type candidate struct {
		path string
		date time.Time
	}
	candidates := make([]candidate, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		date, ok := parseSubscriptionDateFileName(entry.Name())
		if !ok {
			continue
		}
		candidates = append(candidates, candidate{path: filepath.Join(outputDir, entry.Name()), date: date})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].date.After(candidates[j].date) })
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0].path
}
