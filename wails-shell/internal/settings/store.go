// Package settings persists desktop settings and default path conventions for the current app.
//
// Ownership summary:
// 1) define the canonical default settings snapshot
// 2) merge persisted values onto new defaults without per-version migrations
// 3) keep path normalization and save/load policy out of bridge/controller code
//
// Boundary rule:
// settings persistence may know default values and file layout, but feature
// workflow policy belongs in bridge/domain services instead of this package.
//
// File map for maintainers:
// 1) default settings/path constants
// 2) store facade and settings path helpers
// 3) load/save/default-merge helpers
package settings

import (
	"encoding/base64"
	"encoding/json"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"javflow/internal/common"
	runtimepaths "javflow/internal/runtime"
)

// Keep this path label stable and UTF-8-safe because it is used by default
// settings, fallback output discovery, and later file-path diagnostics.
const defaultCrawlerOutputDirName = "\u004A\u0041\u0056\u81EA\u52A8\u5316\u722C\u866B\u5DE5\u5177\u8F93\u51FA"

// defaultBackgroundImageURL points at the mirrored renderer asset. The
// frontend sync step copies desktop/renderer/assets into the Wails renderer
// directory, so this relative URL works in both source-portable and packaged
// builds without exposing a developer machine's absolute path.
const defaultBackgroundImageURL = "./assets/javflow-default-background.jpg"

// Store owns desktop-settings persistence and default-value hydration.
type Store struct {
	paths runtimepaths.Paths
	mu    sync.Mutex
}

func NewStore(paths runtimepaths.Paths) *Store {
	return &Store{paths: paths}
}

func (s *Store) UserDataDir() string {
	if s == nil {
		return ""
	}
	return filepath.Clean(s.paths.UserData)
}

func (s *Store) settingsPath() string {
	return filepath.Join(s.paths.UserData, "desktop-settings.json")
}

// defaultSettings is the single default-value source used by load/save merge
// logic. New desktop settings should land here first.
func (s *Store) defaultSettings() map[string]any {
	return map[string]any{
		"base":                                 "https://www.javbus.com",
		"output":                               filepath.Join(s.paths.Documents, defaultCrawlerOutputDirName),
		"limit":                                10,
		"totalPages":                           0,
		"itemsPerPage":                         30,
		"parallel":                             2,
		"delay":                                2,
		"timeout":                              30000,
		"proxy":                                "",
		"magnetExcludeKeywords":                "",
		"actressCountFilterThreshold":          0,
		"magnetContentValidation":              false,
		"cloudflare":                           false,
		"nomag":                                false,
		"allmag":                               false,
		"nopic":                                false,
		"secondValidation":                     true,
		"taskTemplate":                         "balanced",
		"backgroundImage":                      "",
		"organizerRoot":                        "",
		"organizerMinSizeMB":                   100,
		"organizerSuffix":                      "-A",
		"organizerVideoExtensions":             "mp4, mkv, avi, mov, flv, wmv, ts, m4v, iso",
		"organizerAdFileAction":                "move-to-delete",
		"organizerDryRun":                      false,
		"organizerIncludeSubdirectories":       true,
		"organizerCrawlOutput":                 "",
		"organizerStrictCodeMatch":             true,
		"organizerAdDetectionEnabled":          false,
		"organizerAdThreshold":                 60,
		"organizerAdKeywords":                  "",
		"organizerAdModelType":                 "mobile-net-v3-lite",
		"libraryWorkspacePreferencesVersion":   0,
		"organizerWorkspacePreferencesVersion": 0,
		"libraryCompactLayout":                 false,
		"libraryShowHiddenFiles":               true,
		"libraryScrapeConcurrency":             3,
		"libraryAutoSubscribeFromOutput":       false,
		"organizerAutoSubscribeFromOutput":     false,
	}
}

func cloneMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

// Load hydrates persisted settings on top of the current default snapshot so
// newly added keys appear without a manual migration.
func (s *Store) Load() (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	defaults := s.defaultSettings()
	filePath := s.settingsPath()

	contents, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return cloneMap(defaults), nil
		}
		return nil, err
	}

	loaded := map[string]any{}
	if err := json.Unmarshal(contents, &loaded); err != nil {
		return cloneMap(defaults), nil
	}

	for key, value := range loaded {
		defaults[key] = value
	}

	delete(defaults, "resumeExisting")
	delete(defaults, "exportCoverImages")
	return cloneMap(defaults), nil
}

// Save merges the incoming partial update onto the current settings snapshot and
// writes one canonical desktop-settings.json file.
func (s *Store) Save(next map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	current := s.defaultSettings()
	filePath := s.settingsPath()

	if contents, err := os.ReadFile(filePath); err == nil {
		loaded := map[string]any{}
		if json.Unmarshal(contents, &loaded) == nil {
			for key, value := range loaded {
				current[key] = value
			}
		}
	}

	for key, value := range next {
		current[key] = value
	}

	delete(current, "resumeExisting")
	delete(current, "exportCoverImages")

	payload, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return err
	}
	// Settings are read-modify-written under one mutex. The final replacement is
	// atomic as well, so a power loss cannot turn a valid settings file into a
	// truncated JSON document that later looks like an empty configuration.
	return common.WriteFileAtomic(filePath, payload, 0o644)
}

// imagePathToDataURL reads an image file and returns a base64 data URL that
// the renderer can use directly in CSS, avoiding file:// cross-protocol issues
// inside the Wails WebView2 container.
func imagePathToDataURL(imagePath string) (string, error) {
	data, err := os.ReadFile(imagePath)
	if err != nil {
		return "", err
	}

	ext := strings.ToLower(filepath.Ext(imagePath))
	contentType := mime.TypeByExtension(ext)
	if contentType == "" {
		contentType = "image/jpeg"
	}

	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// AttachBackgroundURL derives the data URL consumed by the renderer from the
// persisted background path. Using an inline data URL avoids WebView2 file://
// restrictions and keeps the background working regardless of where the
// frontend assets are served from.
func (s *Store) AttachBackgroundURL(settings map[string]any) map[string]any {
	next := cloneMap(settings)
	backgroundImage, _ := next["backgroundImage"].(string)
	if backgroundImage == "" {
		next["backgroundImageUrl"] = defaultBackgroundImageURL
		return next
	}

	if _, err := os.Stat(backgroundImage); err != nil {
		next["backgroundImageUrl"] = defaultBackgroundImageURL
		return next
	}

	dataURL, err := imagePathToDataURL(backgroundImage)
	if err != nil {
		next["backgroundImageUrl"] = defaultBackgroundImageURL
		return next
	}

	next["backgroundImageUrl"] = dataURL
	return next
}

func (s *Store) LoadWithBackground() (map[string]any, error) {
	settings, err := s.Load()
	if err != nil {
		return nil, err
	}
	return s.AttachBackgroundURL(settings), nil
}

func (s *Store) GetCurrentOutputDir() (string, error) {
	settings, err := s.Load()
	if err != nil {
		return "", err
	}

	if outputDir, ok := settings["output"].(string); ok && outputDir != "" {
		return outputDir, nil
	}

	return s.paths.Documents, nil
}

func (s *Store) GetRankingCachePath() string {
	return filepath.Join(s.paths.UserData, "actress-ranking-cache.json")
}

func (s *Store) GetRankingHistoryDir() string {
	return filepath.Join(s.paths.UserData, "ranking-history")
}

func (s *Store) GetRankingHistoryDirectories() []string {
	return []string{s.GetRankingHistoryDir()}
}
