// local_discovery.go owns the read-only first pass used to identify film codes in
// legacy video libraries before the mutating organizer workflow is started.
//
// Ownership summary:
// 1) scan local video names and derive deterministic, normalized candidates
// 2) persist the hidden discovery snapshot and unmatched review report
// 3) expose a safe handoff into organizer code-list loading without renaming
//    or deleting any user files
//
// This file deliberately does not perform crawl requests or organizer moves.
//
// File map for maintainers:
// 1) discovery request/result and persisted artifact contracts
// 2) read-only local scan and candidate aggregation
// 3) hidden snapshot/report persistence
package organizer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const discoveredCodesFileName = "discovered-codes.json"

// DiscoverOptions describes the read-only local identification pass. It is
// deliberately separate from RunOptions so scanning a legacy library cannot
// accidentally move, rename, or delete a file.
type DiscoverOptions struct {
	RootPath              string `json:"rootPath"`
	IncludeSubdirectories bool   `json:"includeSubdirectories"`
	VideoExtensions       string `json:"videoExtensions"`
}

type DiscoveredCode struct {
	Code         string   `json:"code"`
	Count        int      `json:"count"`
	ExamplePaths []string `json:"examplePaths,omitempty"`
}

type UnidentifiedVideo struct {
	Path       string   `json:"path"`
	Candidates []string `json:"candidates,omitempty"`
	Reason     string   `json:"reason"`
}

type localDiscoveryArtifact struct {
	SchemaVersion int                 `json:"schemaVersion"`
	GeneratedAt   string              `json:"generatedAt"`
	RootPath      string              `json:"rootPath"`
	Codes         []DiscoveredCode    `json:"codes"`
	Unidentified  []UnidentifiedVideo `json:"unidentified,omitempty"`
}

type DiscoverLocalCodesResult struct {
	RootPath          string              `json:"rootPath"`
	StatePath         string              `json:"statePath"`
	ReportPath        string              `json:"reportPath,omitempty"`
	ScannedFiles      int                 `json:"scannedFiles"`
	VideoFiles        int                 `json:"videoFiles"`
	IdentifiedFiles   int                 `json:"identifiedFiles"`
	UnidentifiedFiles int                 `json:"unidentifiedFiles"`
	CodeCount         int                 `json:"codeCount"`
	Codes             []string            `json:"codes"`
	CodeEntries       []CodeEntry         `json:"codeEntries"`
	Unidentified      []UnidentifiedVideo `json:"unidentified,omitempty"`
}

// DiscoverLocalCodes performs one deterministic, read-only pass over the
// selected library. A candidate is accepted only when the filename/path
// yields exactly one normalized code; ambiguous or missing candidates are
// returned for review and never become an implicit rename rule.
func (s *Service) DiscoverLocalCodes(options DiscoverOptions) (DiscoverLocalCodesResult, error) {
	root := strings.TrimSpace(options.RootPath)
	if root == "" {
		return DiscoverLocalCodesResult{}, fmt.Errorf("本地番号识别需要先选择视频根目录")
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return DiscoverLocalCodesResult{}, err
	}
	info, err := os.Stat(absoluteRoot)
	if err != nil {
		return DiscoverLocalCodesResult{}, fmt.Errorf("视频根目录不可用: %w", err)
	}
	if !info.IsDir() {
		return DiscoverLocalCodesResult{}, fmt.Errorf("视频根目录不是文件夹: %s", absoluteRoot)
	}

	files := collectFiles(absoluteRoot, options.IncludeSubdirectories, normalizeVideoExtensions(options.VideoExtensions))
	result := DiscoverLocalCodesResult{
		RootPath:     absoluteRoot,
		ScannedFiles: len(files),
		Codes:        []string{},
		CodeEntries:  []CodeEntry{},
		Unidentified: []UnidentifiedVideo{},
	}
	codeMap := map[string]*DiscoveredCode{}
	for _, file := range files {
		if !file.IsVideo {
			continue
		}
		result.VideoFiles++
		baseName := filepath.Base(file.Path)
		baseStem := strings.TrimSuffix(baseName, filepath.Ext(baseName))
		relativeStem := strings.TrimSuffix(file.RelativePath, filepath.Ext(file.RelativePath))
		values := []string{baseStem, file.TopDirName, relativeStem}
		seen := map[string]struct{}{}
		candidates := make([]string, 0, 2)
		for _, value := range values {
			for _, candidate := range extractFilmCodeCandidates(value) {
				if _, exists := seen[candidate]; exists {
					continue
				}
				seen[candidate] = struct{}{}
				candidates = append(candidates, candidate)
			}
		}
		sort.Strings(candidates)
		if len(candidates) != 1 {
			reason := "未识别番号"
			if len(candidates) > 1 {
				reason = "发现多个候选番号，需要人工核实"
			}
			result.Unidentified = append(result.Unidentified, UnidentifiedVideo{Path: file.Path, Candidates: candidates, Reason: reason})
			continue
		}

		code := candidates[0]
		entry := codeMap[code]
		if entry == nil {
			entry = &DiscoveredCode{Code: code}
			codeMap[code] = entry
		}
		entry.Count++
		if len(entry.ExamplePaths) < 3 {
			entry.ExamplePaths = append(entry.ExamplePaths, file.Path)
		}
		result.IdentifiedFiles++
	}
	result.UnidentifiedFiles = len(result.Unidentified)

	orderedCodes := make([]string, 0, len(codeMap))
	for code := range codeMap {
		orderedCodes = append(orderedCodes, code)
	}
	sort.Strings(orderedCodes)
	result.Codes = orderedCodes
	result.CodeCount = len(orderedCodes)
	result.CodeEntries = make([]CodeEntry, 0, len(orderedCodes))
	artifactCodes := make([]DiscoveredCode, 0, len(orderedCodes))
	for _, code := range orderedCodes {
		item := *codeMap[code]
		result.CodeEntries = append(result.CodeEntries, CodeEntry{Code: code})
		artifactCodes = append(artifactCodes, item)
	}

	statePath := filepath.Join(absoluteRoot, stateDirName, discoveredCodesFileName)
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		return DiscoverLocalCodesResult{}, err
	}
	artifact := localDiscoveryArtifact{
		SchemaVersion: 1,
		GeneratedAt:   time.Now().Format(time.RFC3339),
		RootPath:      absoluteRoot,
		Codes:         artifactCodes,
		Unidentified:  result.Unidentified,
	}
	encoded, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return DiscoverLocalCodesResult{}, err
	}
	if err := os.WriteFile(statePath, append(encoded, '\r', '\n'), 0o644); err != nil {
		return DiscoverLocalCodesResult{}, err
	}
	result.StatePath = statePath
	if len(result.Unidentified) > 0 {
		result.ReportPath = filepath.Join(absoluteRoot, unmatchedName)
		lines := []string{"# 本地番号识别未命中清单", "# 这些文件没有唯一番号，严格整理时不会自动改名。"}
		for _, item := range result.Unidentified {
			candidateText := strings.Join(item.Candidates, ", ")
			if candidateText == "" {
				candidateText = "无候选"
			}
			lines = append(lines, fmt.Sprintf("[%s] 候选：%s | %s", item.Reason, candidateText, item.Path))
		}
		if err := os.WriteFile(result.ReportPath, []byte(strings.Join(lines, "\r\n")+"\r\n"), 0o644); err != nil {
			return DiscoverLocalCodesResult{}, err
		}
	}
	return result, nil
}
