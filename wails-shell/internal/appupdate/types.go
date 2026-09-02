// Update DTOs and service options define the portable update boundary.
//
// Ownership summary:
// 1) expose stable check/download/apply result shapes to the bridge
// 2) keep GitHub release JSON private to the appupdate package
// 3) make test seams explicit without leaking renderer concerns
//
// File map for maintainers:
// 1) service constants and construction options
// 2) renderer-facing update/apply result DTOs
// 3) private GitHub asset and pending-update records
package appupdate

import (
	"net/http"
	"time"

	"javflow/internal/settings"
)

const (
	defaultRepository = "Dyeink02/javflow"
	defaultAPIBaseURL = "https://api.github.com"
	maxAssetBytes     = int64(512 * 1024 * 1024)

	checkTimeout    = 30 * time.Second
	downloadTimeout = 30 * time.Minute
	replaceTimeout  = 45 * time.Second

	// Install kinds: the NSIS installer drops Uninstall.exe beside the app,
	// portable deployments (ZIP/Lite-Direct/manual) do not.
	installKindInstaller = "installer"
	installKindPortable  = "portable"

	portableUpdateMessage = "便携版暂不支持在线升级，请前往 GitHub Release 页面下载最新便携包。"
)

type ServiceOptions struct {
	CurrentVersion string
	Repository     string
	APIBaseURL     string

	// Store is read on every request so a proxy changed in Settings applies to
	// the next update check/download without restarting the desktop app.
	Store *settings.Store

	// ExecutablePath and Client are primarily test seams. Production leaves them
	// empty so the service resolves the running portable EXE and its transport.
	ExecutablePath string
	Client         *http.Client
}

type UpdateInfo struct {
	CurrentVersion  string `json:"currentVersion"`
	LatestVersion   string `json:"latestVersion"`
	UpdateAvailable bool   `json:"updateAvailable"`
	ReleaseName     string `json:"releaseName,omitempty"`
	ReleaseURL      string `json:"releaseUrl,omitempty"`
	PublishedAt     string `json:"publishedAt,omitempty"`
	AssetName       string `json:"assetName,omitempty"`
	AssetSize       int64  `json:"assetSize,omitempty"`
	SHA256          string `json:"sha256,omitempty"`
	DownloadedPath  string `json:"downloadedPath,omitempty"`
	DownloadReady   bool   `json:"downloadReady"`
	Message         string `json:"message,omitempty"`

	// InstallKind reports "installer" or "portable" for the running copy, and
	// InAppUpdateSupported is false for portable copies: the renderer shows the
	// portable guidance message instead of offering a download.
	InstallKind          string `json:"installKind,omitempty"`
	InAppUpdateSupported bool   `json:"inAppUpdateSupported"`
}

type ApplyResult struct {
	Applied         bool   `json:"applied"`
	RestartRequired bool   `json:"restartRequired"`
	TargetPath      string `json:"targetPath,omitempty"`
	BackupPath      string `json:"backupPath,omitempty"`
	Message         string `json:"message,omitempty"`
}

type githubRelease struct {
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	HTMLURL     string        `json:"html_url"`
	PublishedAt string        `json:"published_at"`
	Assets      []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	URL                string `json:"url"`
	Digest             string `json:"digest"`
	Size               int64  `json:"size"`
}

type pendingUpdate struct {
	Info            UpdateInfo
	DownloadedPath  string
	StagedDirectory string
	TargetPath      string
	SHA256          string
	PackageSHA256   string
}
