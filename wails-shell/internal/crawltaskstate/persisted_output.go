// Package crawltaskstate owns persisted runtime state used by resume,
// validation, and post-run diagnostics.
//
// persisted_output.go inspects on-disk crawl artifacts and summarizes what was
// actually persisted, independent of in-memory runner/controller state.
//
// Ownership summary:
// 1) inspect on-disk crawl artifacts as the persisted-output truth source
// 2) summarize persisted film records without depending on in-memory state
// 3) keep disk-truth inspection separate from resume schema and manager paths
//
// File map for maintainers:
// 1) persisted output record/state DTOs
// 2) disk-truth inspection entrypoint
// 3) persisted record normalization helpers
package crawltaskstate

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"javflow/internal/contracts/crawlartifact"
)

// If the operator sees a mismatch between live UI counts and saved output,
// this read model is the first place to compare against the raw files.
//
// Practical split:
// - read this file when you need to know what the crawl persisted
// - read `manager.go` when you need to know where the state should live
// - read `types.go` when you need the snapshot contract itself
//
type PersistedFilmRecord struct {
	Title      string `json:"title,omitempty"`
	SourceLink string `json:"sourceLink,omitempty"`
}

type PersistedOutputState struct {
	FilmDataPath   string                `json:"filmDataPath"`
	FilmDataExists bool                  `json:"filmDataExists"`
	RecordCount    int                   `json:"recordCount"`
	Records        []PersistedFilmRecord `json:"records,omitempty"`
	LogMessage     string                `json:"logMessage,omitempty"`
}

// InspectPersistedOutput is a read-only view of what was truly saved.
//
// It intentionally ignores in-memory task state so recovery and review can
// explain the disk-backed truth even after a crash or manual stop.
func InspectPersistedOutput(outputDir string) (PersistedOutputState, error) {
	normalizedOutputDir := strings.TrimSpace(outputDir)
	if normalizedOutputDir == "" {
		return PersistedOutputState{}, fmt.Errorf("output dir is empty")
	}

	filmDataPath := crawlartifact.ResolveCrawlOutputPaths(normalizedOutputDir).FilmDataPath
	state := PersistedOutputState{
		FilmDataPath: filmDataPath,
	}

	if !fileExists(filmDataPath) {
		return state, nil
	}

	state.FilmDataExists = true

	content, err := os.ReadFile(filmDataPath)
	if err != nil {
		return PersistedOutputState{}, err
	}

	records := []PersistedFilmRecord{}
	if err := json.Unmarshal(content, &records); err != nil {
		return PersistedOutputState{}, err
	}

	state.RecordCount = len(records)
	state.Records = normalizePersistedFilmRecords(records)
	if state.RecordCount > 0 {
		state.LogMessage = fmt.Sprintf("恢复抓取模式已启用，已识别 %d 条历史记录，将自动跳过已完成内容。", state.RecordCount)
	}

	return state, nil
}

// normalizePersistedFilmRecords drops empty shells so record counts reflect
// actual persisted entries, not placeholder objects.
func normalizePersistedFilmRecords(records []PersistedFilmRecord) []PersistedFilmRecord {
	result := make([]PersistedFilmRecord, 0, len(records))
	for _, record := range records {
		title := strings.TrimSpace(record.Title)
		sourceLink := strings.TrimSpace(record.SourceLink)
		if title == "" && sourceLink == "" {
			continue
		}
		result = append(result, PersistedFilmRecord{
			Title:      title,
			SourceLink: sourceLink,
		})
	}
	return result
}
