// Package crawlresult builds the renderer-facing output/result panel read model.
//
// It is the read-model layer for "what artifacts and quality outputs exist for
// this run". It should aggregate run-context + quality information, but it must
// not mutate crawl execution state.
//
// Ownership summary:
// 1) maintain the crawl result/output panel read model
// 2) aggregate artifact existence and quality-summary status for the renderer
// 3) keep output projection separate from crawl execution and artifact writing
//
// Boundary rule:
// this package may read run-context and quality summaries, but it must not
// mutate runner/task state or take over artifact-writing ownership.
//
// File map for maintainers:
// 1) result panel DTOs and defaults
// 2) service state/update methods
// 3) artifact existence and summary projection helpers
package crawlresult

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"

	"javflow/internal/crawlexecution"
	"javflow/internal/crawlquality"
	"javflow/internal/crawlruncontext"
)

const EventName = "crawl.result-panel"

type Panel struct {
	Status             string `json:"status"`
	Message            string `json:"message"`
	GeneratedAt        string `json:"generatedAt"`
	OutputDir          string `json:"outputDir"`
	FilmDataPath       string `json:"filmDataPath"`
	MagnetPath         string `json:"magnetPath"`
	LogDir             string `json:"logDir"`
	LatestLogPath      string `json:"latestLogPath"`
	ReportPath         string `json:"reportPath"`
	OutputDirExists    bool   `json:"outputDirExists"`
	FilmDataExists     bool   `json:"filmDataExists"`
	MagnetExists       bool   `json:"magnetExists"`
	LogDirExists       bool   `json:"logDirExists"`
	LatestLogExists    bool   `json:"latestLogExists"`
	ReportExists       bool   `json:"reportExists"`
	QualityAvailable   bool   `json:"qualityAvailable"`
	QualityStatus      string `json:"qualityStatus"`
	QualityStatusText  string `json:"qualityStatusText"`
	QualityNoticeLevel string `json:"qualityNoticeLevel"`
	QualitySummaryLine string `json:"qualitySummaryLine"`
	QualityCompletedAt string `json:"qualityCompletedAt"`
	QualityDurationSec int    `json:"qualityDurationSec"`
	IsFinal            bool   `json:"isFinal"`
}

type Service struct {
	mu         sync.RWMutex
	quality    *crawlquality.Service
	runContext *crawlruncontext.Service
	panel      Panel
}

// NewService wires the result panel to run-context and quality read models.
// Keep startup defaults here so the panel has one stable idle state even before
// the first crawl event arrives.
func NewService(quality *crawlquality.Service, runContext *crawlruncontext.Service) *Service {
	return &Service{
		quality:    quality,
		runContext: runContext,
		panel: Panel{
			Status:      "idle",
			Message:     "等待输出结果。",
			GeneratedAt: time.Now().Format(time.RFC3339),
		},
	}
}

func (s *Service) ApplyRawMessage(rawData json.RawMessage) (Panel, bool) {
	if len(rawData) == 0 || string(rawData) == "null" {
		return Panel{}, false
	}

	payload := map[string]any{}
	if err := json.Unmarshal(rawData, &payload); err != nil {
		return Panel{}, false
	}

	return s.ApplyPayload(payload), true
}

// ApplyPayload accepts runtime status/message updates and reprojects the full
// result panel around the latest run-context + quality state.
func (s *Service) ApplyPayload(payload map[string]any) Panel {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := s.buildPanel(payload, true)
	s.panel = next
	return next
}

// Build refreshes the derived result panel from read-model dependencies only.
func (s *Service) Build() Panel {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := s.buildPanel(nil, false)
	s.panel = next
	return next
}

func (s *Service) Signature(panel Panel) string {
	encoded, err := json.Marshal(panel)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// buildPanel is the one place that merges:
// 1) latest runtime status/message hints
// 2) canonical run-context artifact paths
// 3) quality-summary availability and report output
//
// If the result panel disagrees with files on disk, inspect this merge point
// before patching renderer behavior.
func (s *Service) buildPanel(payload map[string]any, writeReport bool) Panel {
	previous := s.panel
	runContext := crawlruncontext.Context{}
	if s.runContext != nil {
		runContext = s.runContext.Build()
	}

	status := firstNonEmpty(strings.ToLower(cleanString(payload["status"])), strings.ToLower(previous.Status), strings.ToLower(runContext.LastStatus))
	message := firstNonEmpty(cleanString(payload["message"]), previous.Message, runContext.LastMessage)
	if status == "" {
		status = "idle"
	}
	if message == "" {
		message = "等待输出结果。"
	}

	panel := Panel{
		Status:        status,
		Message:       message,
		GeneratedAt:   time.Now().Format(time.RFC3339),
		OutputDir:     strings.TrimSpace(runContext.PreferredOutputDir),
		FilmDataPath:  strings.TrimSpace(runContext.PreferredFilmDataPath),
		MagnetPath:    strings.TrimSpace(runContext.PreferredMagnetPath),
		LogDir:        strings.TrimSpace(runContext.LogDir),
		LatestLogPath: strings.TrimSpace(runContext.LatestLogPath),
		IsFinal:       crawlexecution.IsFinalStatus(status),
	}

	panel.OutputDirExists = dirExists(panel.OutputDir)
	panel.FilmDataExists = fileExists(panel.FilmDataPath)
	panel.MagnetExists = fileExists(panel.MagnetPath)
	panel.LogDirExists = dirExists(panel.LogDir)
	panel.LatestLogExists = fileExists(panel.LatestLogPath)

	reportPath := crawlquality.DefaultReportPath(panel.OutputDir)

	if s.quality != nil && panel.OutputDir != "" {
		summary, err := s.quality.Summarize(crawlquality.Options{
			OutputDir:     panel.OutputDir,
			LogDir:        panel.LogDir,
			LatestLogPath: panel.LatestLogPath,
			FilmDataPath:  panel.FilmDataPath,
			MagnetPath:    panel.MagnetPath,
			WriteReport:   writeReport && crawlexecution.IsFinalStatus(status),
		})
		if err == nil {
			panel.QualityAvailable = summary.Available
			panel.QualityStatus = strings.TrimSpace(summary.Status)
			panel.QualityStatusText = strings.TrimSpace(summary.StatusText)
			panel.QualityNoticeLevel = strings.TrimSpace(summary.NoticeLevel)
			panel.QualitySummaryLine = strings.TrimSpace(summary.SummaryLine)
			panel.QualityCompletedAt = strings.TrimSpace(summary.CompletedAt)
			panel.QualityDurationSec = summary.DurationSeconds

			if strings.TrimSpace(summary.OutputDir) != "" {
				panel.OutputDir = strings.TrimSpace(summary.OutputDir)
				panel.OutputDirExists = dirExists(panel.OutputDir)
			}
			if strings.TrimSpace(summary.FilmDataPath) != "" {
				panel.FilmDataPath = strings.TrimSpace(summary.FilmDataPath)
				panel.FilmDataExists = fileExists(panel.FilmDataPath)
			}
			if strings.TrimSpace(summary.MagnetPath) != "" {
				panel.MagnetPath = strings.TrimSpace(summary.MagnetPath)
				panel.MagnetExists = fileExists(panel.MagnetPath)
			}
			if strings.TrimSpace(summary.LogDir) != "" {
				panel.LogDir = strings.TrimSpace(summary.LogDir)
				panel.LogDirExists = dirExists(panel.LogDir)
			}
			if strings.TrimSpace(summary.LatestLogPath) != "" {
				panel.LatestLogPath = strings.TrimSpace(summary.LatestLogPath)
				panel.LatestLogExists = fileExists(panel.LatestLogPath)
			}
			if strings.TrimSpace(summary.ReportPath) != "" {
				reportPath = strings.TrimSpace(summary.ReportPath)
			}
		}
	}

	panel.ReportPath = reportPath
	panel.ReportExists = fileExists(panel.ReportPath)
	if !panel.QualityAvailable {
		panel.QualityStatus = firstNonEmpty(panel.QualityStatus, "idle")
		panel.QualityStatusText = firstNonEmpty(panel.QualityStatusText, "尚未生成复盘摘要")
		panel.QualityNoticeLevel = firstNonEmpty(panel.QualityNoticeLevel, "info")
		panel.QualitySummaryLine = firstNonEmpty(panel.QualitySummaryLine, "等待抓取完成后生成复盘摘要。")
	}

	return panel
}

func cleanString(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func fileExists(filePath string) bool {
	if strings.TrimSpace(filePath) == "" {
		return false
	}
	info, err := os.Stat(filePath)
	return err == nil && info.Mode().IsRegular()
}

func dirExists(dirPath string) bool {
	if strings.TrimSpace(dirPath) == "" {
		return false
	}
	info, err := os.Stat(dirPath)
	return err == nil && info.IsDir()
}
