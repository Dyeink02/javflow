// Package dependency manages runtime prerequisites such as FFmpeg and ONNX.
//
// Ownership summary:
// 1) inspect whether required desktop dependencies are already available
// 2) download, extract, and install missing runtime prerequisites
// 3) emit installation/status progress without leaking dependency policy into
//    feature-domain services
package dependency

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	runtimepaths "javflow/internal/runtime"
)

// File map for maintainers:
// 1) dependency status/read-model contracts
// 2) runtime detection helpers for FFmpeg / ONNX
// 3) download/extract/install flows
// 4) progress emission and uninstall cleanup
//
// Troubleshooting rule:
// - install/download/archive failures should start in this file
// - feature modules should consume dependency status, not reproduce detection logic

const InstallProgressEvent = "dependency.install-progress"

const (
	ffmpegDownloadURL = "https://lz.qaiu.top/parser?url=https://wwbhp.lanzoul.com/i6zfC3ok78tc"
	ffmpegMirrorURL   = "https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip"
	githubAPIRoot     = "https://api.github.com/repos/microsoft/onnxruntime/releases/latest"
	onnxDownloadURL   = "https://lz.qaiu.top/parser?url=https://wwbhp.lanzoul.com/inG8t3ok792b"
	onnxMirrorURL     = "https://mirror.ghproxy.com/https://github.com/microsoft/onnxruntime/releases/download/v1.19.2/onnxruntime-win-x64-1.19.2.zip"
)

var ffmpegVersionPattern = regexp.MustCompile(`(?i)ffmpeg version\s+([^\s]+)`)

// validateDependencyDownloadURL 校验用户自定义依赖下载地址。
// 仅允许 http/https，禁止指向本机或私有网络，防止通过自定义 URL 实施 RCE/SSRF。
func validateDependencyDownloadURL(rawURL string) error {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("无效的下载地址：%s", err.Error())
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("下载地址仅支持 http/https 协议")
	}
	if parsed.Host == "" {
		return fmt.Errorf("下载地址缺少主机名")
	}
	hostWithoutPort := parsed.Hostname()
	if hostWithoutPort == "" {
		return fmt.Errorf("下载地址主机名无效")
	}
	if hostWithoutPort == "localhost" || hostWithoutPort == "127.0.0.1" || hostWithoutPort == "::1" {
		return fmt.Errorf("下载地址不能指向本机")
	}
	if ip := net.ParseIP(hostWithoutPort); ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()) {
		return fmt.Errorf("下载地址不能指向私有网络")
	}
	return nil
}

// eventEmitter is the minimal progress-notification dependency used by the
// runtime prerequisite installer.
type eventEmitter interface {
	Emit(name string, payload any)
}

// ItemStatus and Status are the read-model contracts returned to the bridge/UI.
type ItemStatus struct {
	Name          string `json:"name"`
	DisplayName   string `json:"displayName"`
	Available     bool   `json:"available"`
	Required      bool   `json:"required"`
	InstalledPath string `json:"installedPath"`
	Source        string `json:"source"`
	Version       string `json:"version,omitempty"`
	Message       string `json:"message"`
}

type Status struct {
	GeneratedAt string     `json:"generatedAt"`
	Summary     string     `json:"summary"`
	FFmpeg      ItemStatus `json:"ffmpeg"`
	ONNX        ItemStatus `json:"onnx"`
}

type InstallProgress struct {
	Name            string `json:"name"`
	DisplayName     string `json:"displayName"`
	Stage           string `json:"stage"`
	Message         string `json:"message"`
	Percent         int    `json:"percent"`
	DownloadedBytes int64  `json:"downloadedBytes"`
	TotalBytes      int64  `json:"totalBytes"`
	TargetPath      string `json:"targetPath,omitempty"`
	Done            bool   `json:"done"`
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// Service owns prerequisite detection/install/uninstall flows. It should remain
// independent from crawler/organizer business logic.
type Service struct {
	paths  runtimepaths.Paths
	bus    eventEmitter
	client *http.Client
}

func NewService(paths runtimepaths.Paths, bus eventEmitter) *Service {
	return &Service{
		paths: paths,
		bus:   bus,
		client: &http.Client{
			Timeout: 30 * time.Minute,
		},
	}
}

// GetStatus is the pure read-model path for prerequisite availability.
func (s *Service) GetStatus() Status {
	ffmpegStatus := s.detectFFmpeg()
	onnxStatus := s.detectONNX()

	summaryParts := []string{
		ffmpegStatus.DisplayName + "：" + statusSummaryText(ffmpegStatus.Available, "已就绪", "未安装"),
		onnxStatus.DisplayName + "：" + statusSummaryText(onnxStatus.Available, "已就绪", "未安装（可选增强）"),
	}

	return Status{
		GeneratedAt: time.Now().Format(time.RFC3339),
		Summary:     strings.Join(summaryParts, " | "),
		FFmpeg:      ffmpegStatus,
		ONNX:        onnxStatus,
	}
}

// Install and Uninstall are the only mutating entrypoints for prerequisite
// management. Callers should not reach directly for installFFmpeg/installONNX.
func (s *Service) Install(ctx context.Context, name string, downloadURL string) (Status, error) {
	switch normalizeDependencyName(name) {
	case "ffmpeg":
		if err := s.installFFmpeg(ctx, downloadURL); err != nil {
			return s.GetStatus(), err
		}
	case "onnx":
		if err := s.installONNX(ctx, downloadURL); err != nil {
			return s.GetStatus(), err
		}
	default:
		return s.GetStatus(), fmt.Errorf("不支持的依赖项：%s", strings.TrimSpace(name))
	}

	return s.GetStatus(), nil
}

func (s *Service) Uninstall(name string) (Status, error) {
	switch normalizeDependencyName(name) {
	case "ffmpeg":
		if err := s.uninstallFFmpeg(); err != nil {
			return s.GetStatus(), err
		}
	case "onnx":
		if err := s.uninstallONNX(); err != nil {
			return s.GetStatus(), err
		}
	default:
		return s.GetStatus(), fmt.Errorf("不支持的依赖项：%s", strings.TrimSpace(name))
	}

	return s.GetStatus(), nil
}

func (s *Service) uninstallFFmpeg() error {
	targetDir := filepath.Join(s.paths.UserData, "tools", "ffmpeg")
	_ = os.RemoveAll(targetDir)
	return nil
}

func (s *Service) uninstallONNX() error {
	targetDir := filepath.Join(s.paths.UserData, "tools", "onnxruntime")
	_ = os.RemoveAll(targetDir)
	return nil
}

// detectFFmpeg and detectONNX own local/bundled/system discovery policy.
func (s *Service) detectFFmpeg() ItemStatus {
	candidates := []string{
		filepath.Join(s.paths.UserData, "tools", "ffmpeg", "ffmpeg.exe"),
		filepath.Join(s.paths.ResourcesPath, "tools", "ffmpeg", "ffmpeg.exe"),
		filepath.Join(s.paths.AppPath, "tools", "ffmpeg", "ffmpeg.exe"),
		filepath.Join(s.paths.AppPath, "resources", "ffmpeg", "win-x64", "ffmpeg.exe"),
		filepath.Join(s.paths.ResourcesPath, "resources", "ffmpeg", "win-x64", "ffmpeg.exe"),
	}

	for _, candidate := range uniqueStrings(candidates) {
		if version, ok := probeFFmpeg(candidate); ok {
			return ItemStatus{
				Name:          "ffmpeg",
				DisplayName:   "FFmpeg",
				Available:     true,
				Required:      true,
				InstalledPath: candidate,
				Source:        statusSourceForPath(candidate, s.paths),
				Version:       version,
				Message:       "已检测到 FFmpeg，可用于 AI 广告检测抽帧。",
			}
		}
	}

	if resolved, err := exec.LookPath("ffmpeg"); err == nil {
		if version, ok := probeFFmpeg(resolved); ok {
			return ItemStatus{
				Name:          "ffmpeg",
				DisplayName:   "FFmpeg",
				Available:     true,
				Required:      true,
				InstalledPath: resolved,
				Source:        "system",
				Version:       version,
				Message:       "已检测到系统 FFmpeg，可用于 AI 广告检测抽帧。",
			}
		}
	}

	return ItemStatus{
		Name:          "ffmpeg",
		DisplayName:   "FFmpeg",
		Available:     false,
		Required:      true,
		InstalledPath: filepath.Join(s.paths.UserData, "tools", "ffmpeg", "ffmpeg.exe"),
		Source:        "missing",
		Message:       "未检测到 FFmpeg。启用 AI 广告检测前必须先安装 FFmpeg。",
	}
}

func (s *Service) detectONNX() ItemStatus {
	candidates := append([]string{
		filepath.Join(s.paths.UserData, "tools", "onnxruntime", "onnxruntime.dll"),
		filepath.Join(s.paths.UserData, "tools", "onnxruntime", "lib", "onnxruntime.dll"),
		filepath.Join(s.paths.ResourcesPath, "tools", "onnxruntime", "onnxruntime.dll"),
		filepath.Join(s.paths.ResourcesPath, "tools", "onnxruntime", "lib", "onnxruntime.dll"),
		filepath.Join(s.paths.AppPath, "tools", "onnxruntime", "onnxruntime.dll"),
		filepath.Join(s.paths.AppPath, "tools", "onnxruntime", "lib", "onnxruntime.dll"),
	}, globCandidates(filepath.Join(s.paths.UserData, "tools", "onnxruntime", "*", "lib", "onnxruntime.dll"))...)

	for _, candidate := range uniqueStrings(candidates) {
		if fileExists(candidate) {
			return ItemStatus{
				Name:          "onnx",
				DisplayName:   "ONNX Runtime",
				Available:     true,
				Required:      false,
				InstalledPath: candidate,
				Source:        statusSourceForPath(candidate, s.paths),
				Message:       "已检测到 ONNX Runtime，可用于后续增强模型推理。",
			}
		}
	}

	return ItemStatus{
		Name:          "onnx",
		DisplayName:   "ONNX Runtime",
		Available:     false,
		Required:      false,
		InstalledPath: filepath.Join(s.paths.UserData, "tools", "onnxruntime", "onnxruntime.dll"),
		Source:        "missing",
		Message:       "当前未安装 ONNX Runtime。现阶段不阻塞规则链路，但建议提前安装。",
	}
}

// installFFmpeg and installONNX keep download/extract behavior local to the
// dependency domain so the bridge only sees progress and final status.
func (s *Service) installFFmpeg(ctx context.Context, customURL string) error {
	targetDir := filepath.Join(s.paths.UserData, "tools", "ffmpeg")
	targetPath := filepath.Join(targetDir, "ffmpeg.exe")
	tempZip := filepath.Join(s.paths.Temp, fmt.Sprintf("jav-auto-ffmpeg-%d.zip", time.Now().UnixNano()))

	s.emitProgress("ffmpeg", "prepare", "开始准备 FFmpeg 下载任务。", 0, 0, 0, targetPath, false)

	// Build URL list: custom URL > 蓝奏云解析 > gyan.dev > GitHub BtbN
	urls := []string{ffmpegDownloadURL, ffmpegMirrorURL, "https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-win64-gpl.zip"}
	if trimmed := strings.TrimSpace(customURL); trimmed != "" {
		if err := validateDependencyDownloadURL(trimmed); err != nil {
			s.emitProgress("ffmpeg", "error", "自定义下载地址无效："+err.Error(), 0, 0, 0, targetPath, true)
			return err
		}
		urls = append([]string{trimmed}, urls...)
	}

	var downloadErr error
	for _, url := range urls {
		s.emitProgress("ffmpeg", "download", fmt.Sprintf("正在下载 FFmpeg..."), 0, 0, 0, targetPath, false)
		downloadErr = s.downloadToFile(ctx, url, tempZip, "ffmpeg", "FFmpeg")
		if downloadErr == nil {
			break
		}
		s.emitProgress("ffmpeg", "retry", "当前源下载失败，尝试备用源...", 0, 0, 0, targetPath, false)
	}
	if downloadErr != nil {
		s.emitProgress("ffmpeg", "error", "FFmpeg 下载失败（所有源均不可用）："+downloadErr.Error(), 0, 0, 0, targetPath, true)
		return downloadErr
	}
	defer os.Remove(tempZip)

	s.emitProgress("ffmpeg", "extract", "FFmpeg 下载完成，开始解压核心文件。", 90, 0, 0, targetPath, false)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		s.emitProgress("ffmpeg", "error", "FFmpeg 目录创建失败："+err.Error(), 90, 0, 0, targetPath, true)
		return err
	}

	if _, err := extractSingleFileFromZip(tempZip, func(name string) bool {
		normalized := strings.ToLower(strings.ReplaceAll(name, "\\", "/"))
		return strings.HasSuffix(normalized, "/bin/ffmpeg.exe") || strings.HasSuffix(normalized, "ffmpeg.exe")
	}, targetPath); err != nil {
		s.emitProgress("ffmpeg", "error", "FFmpeg 解压失败："+err.Error(), 90, 0, 0, targetPath, true)
		return err
	}

	s.emitProgress("ffmpeg", "completed", "FFmpeg 已安装完成。", 100, 0, 0, targetPath, true)
	return nil
}

func (s *Service) installONNX(ctx context.Context, customURL string) error {
	targetDir := filepath.Join(s.paths.UserData, "tools", "onnxruntime")
	tempZip := filepath.Join(s.paths.Temp, fmt.Sprintf("jav-auto-onnx-%d.zip", time.Now().UnixNano()))

	// 优先级: 自定义URL > 蓝奏云解析 > 镜像
	downloadURL := onnxDownloadURL
	assetName := "onnxruntime-custom.zip"

	customTrimmed := strings.TrimSpace(customURL)
	if customTrimmed != "" {
		if err := validateDependencyDownloadURL(customTrimmed); err != nil {
			s.emitProgress("onnx", "error", "自定义下载地址无效："+err.Error(), 0, 0, 0, targetDir, true)
			return err
		}
		downloadURL = customTrimmed
		s.emitProgress("onnx", "prepare", "使用自定义下载地址安装 ONNX Runtime。", 0, 0, 0, targetDir, false)
	} else {
		s.emitProgress("onnx", "prepare", "使用蓝奏云解析地址安装 ONNX Runtime。", 0, 0, 0, targetDir, false)
	}
	// fallback URLs when download fails
	defaultURLs := []string{}
	if customTrimmed == "" {
		// Only attempt GitHub API + mirror as fallback when not using custom URL
		defaultURLs = []string{onnxMirrorURL}
	}

	s.emitProgress("onnx", "prepare", "开始准备 ONNX Runtime 下载任务。", 0, 0, 0, targetDir, false)
	if err := s.downloadToFile(ctx, downloadURL, tempZip, "onnx", "ONNX Runtime"); err != nil {
		// Try defaults as fallback
		if len(defaultURLs) > 0 {
			s.emitProgress("onnx", "retry", "主源下载失败，尝试备用源...", 0, 0, 0, targetDir, false)
			var lastErr error
			for _, url := range defaultURLs {
				lastErr = s.downloadToFile(ctx, url, tempZip, "onnx", "ONNX Runtime")
				if lastErr == nil {
					downloadURL = url
					break
				}
				s.emitProgress("onnx", "retry", "当前源下载失败，尝试下一个...", 0, 0, 0, targetDir, false)
			}
			if lastErr != nil {
				s.emitProgress("onnx", "error", "ONNX Runtime 下载失败（所有源均不可用）："+lastErr.Error(), 0, 0, 0, targetDir, true)
				return lastErr
			}
		} else {
			s.emitProgress("onnx", "error", "ONNX Runtime 下载失败："+err.Error(), 0, 0, 0, targetDir, true)
			return err
		}
	}
	defer os.Remove(tempZip)

	s.emitProgress("onnx", "extract", "ONNX Runtime 下载完成，开始解压运行时文件。", 90, 0, 0, filepath.Join(targetDir, assetName), false)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		s.emitProgress("onnx", "error", "ONNX Runtime 目录创建失败："+err.Error(), 90, 0, 0, targetDir, true)
		return err
	}

	if err := extractONNXRuntimeZip(tempZip, targetDir); err != nil {
		s.emitProgress("onnx", "error", "ONNX Runtime 解压失败："+err.Error(), 90, 0, 0, targetDir, true)
		return err
	}

	s.emitProgress("onnx", "completed", "ONNX Runtime 已安装完成。", 100, 0, 0, targetDir, true)
	return nil
}

func (s *Service) resolveLatestONNXDownloadURL(ctx context.Context) (string, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, githubAPIRoot, nil)
	if err != nil {
		return "", "", err
	}
	request.Header.Set("User-Agent", "JavFlow/0.4.0")
	request.Header.Set("Accept", "application/vnd.github+json")

	response, err := s.client.Do(request)
	if err != nil {
		return "", "", err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", "", fmt.Errorf("GitHub API 返回状态码 %d", response.StatusCode)
	}

	release := githubRelease{}
	if err := json.NewDecoder(response.Body).Decode(&release); err != nil {
		return "", "", err
	}

	for _, asset := range release.Assets {
		name := strings.ToLower(strings.TrimSpace(asset.Name))
		if strings.HasPrefix(name, "onnxruntime-win-x64-") && strings.HasSuffix(name, ".zip") {
			return strings.TrimSpace(asset.URL), strings.TrimSpace(asset.Name), nil
		}
	}

	return "", "", fmt.Errorf("未找到适用于 Windows x64 的 ONNX Runtime 压缩包")
}

// downloadToFile centralizes streamed download plus progress emission for all
// prerequisite installers.
func (s *Service) downloadToFile(ctx context.Context, sourceURL string, targetPath string, name string, displayName string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "JavFlow/0.4.0")

	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("下载返回状态码 %d", response.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}

	file, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	defer file.Close()

	totalBytes := response.ContentLength
	downloadedBytes := int64(0)
	buffer := make([]byte, 1024*1024)
	lastPercent := -1

	for {
		readBytes, readErr := response.Body.Read(buffer)
		if readBytes > 0 {
			if _, err := file.Write(buffer[:readBytes]); err != nil {
				return err
			}
			downloadedBytes += int64(readBytes)
			percent := progressPercent(downloadedBytes, totalBytes, 85)
			if percent != lastPercent {
				s.emitProgress(
					name,
					"download",
					fmt.Sprintf("正在下载 %s。", displayName),
					percent,
					downloadedBytes,
					totalBytes,
					targetPath,
					false,
				)
				lastPercent = percent
			}
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	s.emitProgress(
		name,
		"download",
		fmt.Sprintf("%s 下载完成。", displayName),
		85,
		downloadedBytes,
		totalBytes,
		targetPath,
		false,
	)
	return nil
}

// emitProgress is the single bridge-facing progress envelope writer for
// dependency installation.
func (s *Service) emitProgress(name string, stage string, message string, percent int, downloadedBytes int64, totalBytes int64, targetPath string, done bool) {
	if s == nil || s.bus == nil {
		return
	}

	displayName := "FFmpeg"
	if normalizeDependencyName(name) == "onnx" {
		displayName = "ONNX Runtime"
	}

	s.bus.Emit(InstallProgressEvent, InstallProgress{
		Name:            normalizeDependencyName(name),
		DisplayName:     displayName,
		Stage:           strings.TrimSpace(stage),
		Message:         strings.TrimSpace(message),
		Percent:         percent,
		DownloadedBytes: downloadedBytes,
		TotalBytes:      totalBytes,
		TargetPath:      strings.TrimSpace(targetPath),
		Done:            done,
	})
}

func normalizeDependencyName(name string) string {
	value := strings.ToLower(strings.TrimSpace(name))
	switch value {
	case "ffmpeg":
		return "ffmpeg"
	case "onnxruntime", "onnx-runtime", "onnx":
		return "onnx"
	default:
		return ""
	}
}

func probeFFmpeg(command string) (string, bool) {
	if !fileExists(command) && !strings.EqualFold(command, "ffmpeg") {
		return "", false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	output, err := exec.CommandContext(ctx, command, "-version").CombinedOutput()
	if err != nil {
		return "", false
	}

	match := ffmpegVersionPattern.FindStringSubmatch(string(output))
	if len(match) == 2 {
		return strings.TrimSpace(match[1]), true
	}
	return "", true
}

func progressPercent(downloadedBytes int64, totalBytes int64, maxPercent int) int {
	if maxPercent <= 0 {
		maxPercent = 85
	}
	if totalBytes <= 0 || downloadedBytes <= 0 {
		return 5
	}
	percent := int((downloadedBytes * int64(maxPercent)) / totalBytes)
	if percent < 5 {
		return 5
	}
	if percent > maxPercent {
		return maxPercent
	}
	return percent
}

func extractSingleFileFromZip(zipPath string, matcher func(name string) bool, targetPath string) (string, error) {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	for _, file := range reader.File {
		if matcher != nil && !matcher(file.Name) {
			continue
		}
		if err := extractZipFile(file, targetPath); err != nil {
			return "", err
		}
		return targetPath, nil
	}

	return "", fmt.Errorf("压缩包中未找到目标文件")
}

func extractONNXRuntimeZip(zipPath string, targetDir string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	extracted := 0
	for _, file := range reader.File {
		normalized := strings.ToLower(strings.ReplaceAll(file.Name, "\\", "/"))
		baseName := strings.ToLower(filepath.Base(normalized))

		switch {
		case strings.HasSuffix(normalized, "/lib/onnxruntime.dll"):
			if err := extractZipFile(file, filepath.Join(targetDir, "lib", filepath.Base(file.Name))); err != nil {
				return err
			}
			extracted++
		case strings.Contains(normalized, "/lib/") && strings.HasSuffix(baseName, ".dll"):
			if err := extractZipFile(file, filepath.Join(targetDir, "lib", filepath.Base(file.Name))); err != nil {
				return err
			}
			extracted++
		case strings.Contains(normalized, "/bin/") && strings.HasSuffix(baseName, ".dll"):
			if err := extractZipFile(file, filepath.Join(targetDir, "bin", filepath.Base(file.Name))); err != nil {
				return err
			}
			extracted++
		case baseName == "license" || baseName == "license.txt" || baseName == "version_number" || baseName == "thirdpartynotices.txt":
			if err := extractZipFile(file, filepath.Join(targetDir, filepath.Base(file.Name))); err != nil {
				return err
			}
			extracted++
		}
	}

	if extracted == 0 {
		return fmt.Errorf("压缩包中未找到 ONNX Runtime 运行时文件")
	}
	return nil
}

func extractZipFile(file *zip.File, targetPath string) error {
	reader, err := file.Open()
	if err != nil {
		return err
	}
	defer reader.Close()

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}

	writer, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	defer writer.Close()

	_, err = io.Copy(writer, reader)
	return err
}

func statusSummaryText(available bool, availableText string, missingText string) string {
	if available {
		return availableText
	}
	return missingText
}

func statusSourceForPath(targetPath string, paths runtimepaths.Paths) string {
	normalizedPath := strings.ToLower(cleanPath(targetPath))
	switch {
	case strings.HasPrefix(normalizedPath, strings.ToLower(cleanPath(paths.UserData))):
		return "userData"
	case strings.HasPrefix(normalizedPath, strings.ToLower(cleanPath(paths.ResourcesPath))):
		return "bundle"
	case strings.HasPrefix(normalizedPath, strings.ToLower(cleanPath(paths.AppPath))):
		return "app"
	default:
		return "custom"
	}
}

func cleanPath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if absolute, err := filepath.Abs(trimmed); err == nil {
		return absolute
	}
	return filepath.Clean(trimmed)
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		normalized := strings.TrimSpace(value)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func globCandidates(pattern string) []string {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	return matches
}

func fileExists(targetPath string) bool {
	info, err := os.Stat(targetPath)
	return err == nil && !info.IsDir()
}
