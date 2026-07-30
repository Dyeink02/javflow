// Ownership summary:
//
//	This file implements the name-rescue workflow for restoring canonical filenames.
//
// File map for maintainers:
//  1. NameRescueResult type.
//  2. Video collection and rename candidate generation.
//  3. Safe rename execution without deletion paths.
package organizer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// RescueNames restores canonical filenames using only the loaded expected-code
// list and historical rename evidence. It never guesses from an unrestricted
// code parser and never invokes organizer deletion paths.
func (s *Service) RescueNames(options RunOptions) (NameRescueResult, error) {
	ctx, err := newOrganizerRunContext(s, options)
	if err != nil {
		return NameRescueResult{}, err
	}
	if len(ctx.codeSet) == 0 {
		return NameRescueResult{}, fmt.Errorf("番号名称抢救需要先加载非空番号名单")
	}
	for _, dir := range []string{ctx.paths.WaitingDir, ctx.paths.UnmatchedDir, ctx.paths.LogsDir} {
		if err := ensureDirectory(dir); err != nil {
			return NameRescueResult{}, err
		}
	}

	files := collectNameRescueVideos(ctx.normalizedRootPath, ctx.paths, ctx.options.IncludeSubdirectories, ctx.videoExtensionSet)
	result := NameRescueResult{
		ScannedTotal: len(files),
		WaitingDir:   ctx.paths.WaitingDir,
		UnmatchedDir: ctx.paths.UnmatchedDir,
		ReportPath:   ctx.paths.RescueReportPath,
		Records:      make([]NameRescueRecord, 0, len(files)),
	}

	renameHints := loadRenameReportHints(ctx.paths.RenameMapPath)
	historyPathHints, historyNameHints := loadRenameHistoryHints(ctx.paths.RenameHistoryPath)
	matched := make([]Candidate, 0, len(files))
	unmatched := make([]string, 0, len(files))
	for _, filePath := range files {
		filmCode := ""
		hint := historyPathHints[strings.ToLower(filepath.Clean(filePath))]
		if hint == "" {
			hint = renameHints[strings.ToLower(filepath.Base(filePath))]
		}
		if hint == "" {
			hint = historyNameHints[strings.ToLower(filepath.Base(filePath))]
		}
		if hint != "" {
			filmCode, _, _ = matchExpectedCodeEvidenceWithAliases(hint, ctx.codeSet, ctx.tokenSet, ctx.expectedCodeAliasIndex)
		}
		if filmCode == "" {
			filmCode, _, _ = matchExpectedCodeFromPathWithAliases(filePath, ctx.normalizedRootPath, ctx.codeSet, ctx.tokenSet, ctx.expectedCodeAliasIndex)
		}
		if filmCode == "" {
			unmatched = append(unmatched, filePath)
			continue
		}
		matched = append(matched, Candidate{
			Src:              filePath,
			FilmCode:         filmCode,
			RenameByFilmCode: true,
		})
	}

	targetNames := planTargetNames(matched, ctx.suffixStrategy)
	for index, item := range matched {
		targetPath := filepath.Join(ctx.paths.WaitingDir, targetNames[index])
		record := NameRescueRecord{OriginalPath: item.Src, FilmCode: item.FilmCode, Status: "matched"}
		if filepath.Clean(item.Src) == filepath.Clean(targetPath) {
			record.TargetPath = targetPath
			result.MatchedTotal++
			result.Records = append(result.Records, record)
			continue
		}
		movedPath, moveErr := moveWithUnique(item.Src, targetPath)
		if moveErr != nil {
			record.Status = "failed"
			record.Reason = moveErr.Error()
			result.FailedTotal++
		} else {
			record.TargetPath = movedPath
			result.MatchedTotal++
		}
		result.Records = append(result.Records, record)
	}

	for _, filePath := range unmatched {
		record := NameRescueRecord{OriginalPath: filePath, Status: "unmatched", Reason: "番号名单无法唯一命中"}
		if isPathInside(ctx.paths.UnmatchedDir, filePath) {
			record.TargetPath = filePath
			result.UnmatchedTotal++
			result.Records = append(result.Records, record)
			continue
		}
		targetPath := filepath.Join(ctx.paths.UnmatchedDir, filepath.Base(filePath))
		movedPath, moveErr := moveWithUnique(filePath, targetPath)
		if moveErr != nil {
			record.Status = "failed"
			record.Reason = moveErr.Error()
			result.FailedTotal++
		} else {
			record.TargetPath = movedPath
			result.UnmatchedTotal++
		}
		result.Records = append(result.Records, record)
	}

	if err := writeNameRescueReport(result); err != nil {
		return result, err
	}
	return result, nil
}

func collectNameRescueVideos(rootPath string, paths Paths, includeSubdirectories bool, extensionSet map[string]struct{}) []string {
	files := []string{}
	skipTopDirs := map[string]struct{}{
		strings.ToLower(filepath.Base(paths.ToDeleteDir)): {},
		strings.ToLower(filepath.Base(paths.IntroAdDir)):  {},
		strings.ToLower(filepath.Base(paths.LogsDir)):     {},
		strings.ToLower(filepath.Base(paths.StateDir)):    {},
	}
	_ = filepath.Walk(rootPath, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info == nil {
			return nil
		}
		if path == rootPath {
			return nil
		}
		relativePath, err := filepath.Rel(rootPath, path)
		if err != nil || strings.HasPrefix(relativePath, "..") {
			return nil
		}
		topDir := strings.Split(relativePath, string(os.PathSeparator))[0]
		if _, skip := skipTopDirs[strings.ToLower(topDir)]; skip || strings.HasPrefix(strings.ToLower(topDir), batchDeleteStagingPrefix) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			if !includeSubdirectories && filepath.Dir(relativePath) != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Mode().IsRegular() && isVideoFile(path, extensionSet) {
			files = append(files, path)
		}
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i]) < strings.ToLower(files[j]) })
	return files
}

func matchExpectedCodeFromPath(filePath, rootPath string, codeSet, tokenSet map[string]struct{}) string {
	matched, _, _ := matchExpectedCodeFromPathWithAliases(filePath, rootPath, codeSet, tokenSet, newExpectedCodeAliasIndex())
	return matched
}

func matchExpectedCodeFromPathWithAliases(filePath, rootPath string, codeSet, tokenSet map[string]struct{}, aliasIndex expectedCodeAliasIndex) (string, string, bool) {
	current := filepath.Clean(filePath)
	for {
		if matched, alias, ambiguous := matchExpectedCodeEvidenceWithAliases(filepath.Base(current), codeSet, tokenSet, aliasIndex); matched != "" {
			return matched, alias, false
		} else if ambiguous {
			return "", "", true
		}
		parent := filepath.Dir(current)
		if parent == current || parent == filepath.Clean(rootPath) || !isPathInside(rootPath, parent) {
			break
		}
		current = parent
	}
	return "", "", false
}

func matchExpectedCodeEvidence(value string, codeSet, tokenSet map[string]struct{}) string {
	matched, _, _ := matchExpectedCodeEvidenceWithAliases(value, codeSet, tokenSet, newExpectedCodeAliasIndex())
	return matched
}

func matchExpectedCodeEvidenceWithAliases(value string, codeSet, tokenSet map[string]struct{}, aliasIndex expectedCodeAliasIndex) (string, string, bool) {
	candidate := extractFilmCodeFromFile(value, codeSet, tokenSet)
	if candidate == "" || !containsCode(codeSet, candidate) {
		if fallback := matchExpectedCodeFallback(value, codeSet); fallback != "" {
			return fallback, "", false
		}
		matched, alias, ambiguous := matchExpectedCodeAliasFromValue(value, aliasIndex)
		return matched, alias, ambiguous
	}
	return normalizeFilmID(candidate), "", false
}

func loadRenameReportHints(reportPath string) map[string]string {
	result := map[string]string{}
	payload, err := os.ReadFile(reportPath)
	if err != nil {
		return result
	}
	for _, line := range strings.Split(strings.ReplaceAll(string(payload), "\r\n", "\n"), "\n") {
		arrowParts := strings.SplitN(line, " => ", 2)
		if len(arrowParts) != 2 {
			continue
		}
		originalName := strings.TrimSpace(arrowParts[0])
		if dot := strings.Index(originalName, ". "); dot >= 0 {
			originalName = strings.TrimSpace(originalName[dot+2:])
		}
		fields := strings.Split(arrowParts[1], " | ")
		if len(fields) < 2 {
			continue
		}
		newName := strings.ToLower(strings.TrimSpace(fields[0]))
		originalPath := strings.TrimSpace(fields[len(fields)-1])
		hint := strings.TrimSpace(originalName + " " + originalPath)
		if newName == "" || hint == "" {
			continue
		}
		if existing, exists := result[newName]; exists && existing != hint {
			result[newName] = ""
		} else {
			result[newName] = hint
		}
	}
	return result
}

func writeNameRescueReport(result NameRescueResult) error {
	lines := []string{
		"视频整理助手 - 番号名称抢救报告",
		"生成时间：" + time.Now().Format("2006-01-02 15:04:05"),
		fmt.Sprintf("扫描=%d，成功恢复=%d，移入未命中=%d，失败=%d", result.ScannedTotal, result.MatchedTotal, result.UnmatchedTotal, result.FailedTotal),
		"",
	}
	for index, record := range result.Records {
		lines = append(lines, fmt.Sprintf("%d. [%s] %s -> %s | %s", index+1, record.Status, record.OriginalPath, record.TargetPath, record.FilmCode))
		if record.Reason != "" {
			lines = append(lines, "   原因："+record.Reason)
		}
	}
	return writeTextFile(result.ReportPath, lines)
}
