package runtimepaths

import (
	"os"
	"path/filepath"
)

// Package runtimepaths resolves the desktop runtime's coarse-grained filesystem
// anchors. It should stay focused on process/app locations, not crawl or
// organizer artifact paths.
//
// Ownership summary:
// 1) resolve coarse-grained desktop runtime filesystem anchors
// 2) centralize repo/app/resources/user-data/documents/temp roots
// 3) keep runtime path resolution separate from feature artifact layout
//
// File map for maintainers:
// 1) runtime path DTO
// 2) repo/app/resources/user-data root resolution
// 3) home/documents/temp fallback helpers

type Paths struct {
	RepoRoot      string `json:"repoRoot"`
	AppPath       string `json:"appPath"`
	ResourcesPath string `json:"resourcesPath"`
	UserData      string `json:"userData"`
	Documents     string `json:"documents"`
	Temp          string `json:"temp"`
}

func BuildPaths(repoRoot string) (Paths, error) {
	absoluteRepoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return Paths{}, err
	}

	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		userConfigDir = filepath.Join(os.TempDir(), "javflow")
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = absoluteRepoRoot
	}

	return Paths{
		RepoRoot:      absoluteRepoRoot,
		AppPath:       absoluteRepoRoot,
		ResourcesPath: absoluteRepoRoot,
		UserData:      filepath.Join(userConfigDir, "javflow"),
		Documents:     filepath.Join(homeDir, "Documents"),
		Temp:          os.TempDir(),
	}, nil
}

func (p Paths) ToMap() map[string]any {
	return map[string]any{
		"repoRoot":      p.RepoRoot,
		"appPath":       p.AppPath,
		"resourcesPath": p.ResourcesPath,
		"userData":      p.UserData,
		"documents":     p.Documents,
		"temp":          p.Temp,
	}
}
