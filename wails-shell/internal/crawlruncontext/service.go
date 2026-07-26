// Package crawlruncontext owns the canonical read model for current crawl artifact paths.
//
// Ownership summary:
// 1) publish the canonical current/last crawl artifact path context
// 2) combine runtime cache hints with output-dir resolution into one read model
// 3) keep artifact-path selection out of bridge/controller duplication
//
// File map for maintainers:
// 1) output-dir provider and context DTOs
// 2) service state/update methods
// 3) preferred output/artifact-path resolution helpers
package crawlruncontext

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"javflow/internal/contracts/crawlartifact"
	"javflow/internal/crawlexecution"
	"javflow/internal/runtimecache"
)

const EventName = "crawl.run-context"

type OutputDirProvider interface {
	GetCurrentOutputDir() (string, error)
}

type Context struct {
	CurrentTaskOutputDir        string `json:"currentTaskOutputDir"`
	LastTaskOutputDir           string `json:"lastTaskOutputDir"`
	PreferredOutputDir          string `json:"preferredOutputDir"`
	PreferredFilmDataPath       string `json:"preferredFilmDataPath"`
	PreferredCrawlProfilePath   string `json:"preferredCrawlProfilePath"`
	PreferredOrganizerCodesPath string `json:"preferredOrganizerCodesPath"`
	PreferredMagnetPath         string `json:"preferredMagnetPath"`
	LogDir                      string `json:"logDir"`
	SessionLogPath              string `json:"sessionLogPath"`
	LatestLogPath               string `json:"latestLogPath"`
	LastStatus                  string `json:"lastStatus"`
	LastMessage                 string `json:"lastMessage"`
	IsRunning                   bool   `json:"isRunning"`
}

type Service struct {
	outputProvider OutputDirProvider
	runtimeState   *runtimecache.State
}

// NewService builds the canonical crawl run-context read model. This service is
// the single owner for "which crawl output/log/artifact paths should the rest
// of the app read right now", so bridge helpers should consume it rather than
// re-assembling the same defaults independently.
func NewService(outputProvider OutputDirProvider, runtimeState *runtimecache.State) *Service {
	return &Service{
		outputProvider: outputProvider,
		runtimeState:   runtimeState,
	}
}

func cleanString(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func normalizePath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if absolute, err := filepath.Abs(trimmed); err == nil {
		return absolute
	}
	return filepath.Clean(trimmed)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (s *Service) fallbackOutputDir() string {
	if s == nil || s.outputProvider == nil {
		return ""
	}

	outputDir, err := s.outputProvider.GetCurrentOutputDir()
	if err != nil {
		return ""
	}
	return normalizePath(outputDir)
}

func (s *Service) snapshot() map[string]any {
	if s == nil || s.runtimeState == nil {
		return map[string]any{}
	}
	return s.runtimeState.Snapshot()
}

func (s *Service) Build() Context {
	snapshot := s.snapshot()

	currentTaskOutputDir := normalizePath(cleanString(snapshot["currentTaskOutputDir"]))
	lastTaskOutputDir := normalizePath(cleanString(snapshot["lastTaskOutputDir"]))
	preferredOutputDir := firstNonEmpty(currentTaskOutputDir, lastTaskOutputDir, s.fallbackOutputDir())
	// If the recorded output dir is a parent/root that holds timestamped run
	// subfolders, surface the newest concrete run dir as the preferred context.
	preferredOutputDir = crawlartifact.ResolveEffectiveCrawlOutputDir(preferredOutputDir)

	runPaths := crawlartifact.ResolveCrawlRunPaths(preferredOutputDir)

	logDir := normalizePath(cleanString(snapshot["logDir"]))
	if logDir == "" {
		logDir = runPaths.LogDir
	}

	latestLogPath := normalizePath(cleanString(snapshot["latestLogPath"]))
	if latestLogPath == "" {
		if logDir == runPaths.LogDir {
			latestLogPath = runPaths.LatestLogPath
		} else {
			latestLogPath = crawlartifact.DefaultLatestLogPath(logDir)
		}
	}

	lastStatus := strings.ToLower(cleanString(snapshot["lastCrawlStatus"]))
	lastMessage := cleanString(snapshot["lastCrawlMessage"])

	return Context{
		CurrentTaskOutputDir:        currentTaskOutputDir,
		LastTaskOutputDir:           lastTaskOutputDir,
		PreferredOutputDir:          runPaths.OutputDir,
		PreferredFilmDataPath:       runPaths.FilmDataPath,
		PreferredCrawlProfilePath:   runPaths.CrawlProfilePath,
		PreferredOrganizerCodesPath: runPaths.OrganizerCodesPath,
		PreferredMagnetPath:         runPaths.MagnetPath,
		LogDir:                      logDir,
		SessionLogPath:              normalizePath(cleanString(snapshot["sessionLogPath"])),
		LatestLogPath:               latestLogPath,
		LastStatus:                  lastStatus,
		LastMessage:                 lastMessage,
		IsRunning:                   crawlexecution.IsActiveStatus(lastStatus),
	}
}

func (s *Service) Signature(context Context) string {
	encoded, err := json.Marshal(context)
	if err != nil {
		return ""
	}
	return string(encoded)
}
