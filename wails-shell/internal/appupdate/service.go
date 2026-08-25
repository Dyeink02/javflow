// Package appupdate owns the GitHub release check/download portion of the
// portable desktop update flow.
//
// Ownership summary:
// 1) query the configured GitHub release and compare product versions
// 2) download and verify the portable ZIP package
// 3) keep proxy selection and file staging separate from the Wails bridge
//
// File map for maintainers:
// 1) service construction and current-version resolution
// 2) GitHub release/asset discovery
// 3) checksum validation and same-directory update staging
//
// Boundary rule:
// this package only talks to GitHub Releases. It must not call crawler fetch,
// anti-block, Cloudflare, or age-verification code.
package appupdate

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"javflow/internal/netguard"
	proxyservice "javflow/internal/proxy"
	"javflow/internal/settings"
)

type Service struct {
	currentVersion string
	repository     string
	apiBaseURL     string
	store          *settings.Store
	executablePath string
	client         *http.Client

	mu      sync.Mutex
	pending *pendingUpdate
}

func NewService(options ServiceOptions) *Service {
	currentVersion := strings.TrimSpace(options.CurrentVersion)
	if currentVersion == "" {
		currentVersion = ProductVersion()
	}
	repository := strings.Trim(strings.TrimSpace(options.Repository), "/")
	if repository == "" {
		repository = defaultRepository
	}
	apiBaseURL := strings.TrimRight(strings.TrimSpace(options.APIBaseURL), "/")
	if apiBaseURL == "" {
		apiBaseURL = defaultAPIBaseURL
	}

	return &Service{
		currentVersion: currentVersion,
		repository:     repository,
		apiBaseURL:     apiBaseURL,
		store:          options.Store,
		executablePath: strings.TrimSpace(options.ExecutablePath),
		client:         options.Client,
	}
}

func (s *Service) CurrentVersion() string {
	if s == nil || strings.TrimSpace(s.currentVersion) == "" {
		return ProductVersion()
	}
	return s.currentVersion
}

func (s *Service) Check(ctx context.Context) (UpdateInfo, error) {
	if s == nil {
		return UpdateInfo{}, fmt.Errorf("在线更新服务未初始化")
	}
	requestContext, cancel := context.WithTimeout(normalizeContext(ctx), checkTimeout)
	defer cancel()

	info, _, _, err := s.checkRelease(requestContext)
	return info, err
}

func (s *Service) checkRelease(ctx context.Context) (UpdateInfo, githubRelease, githubAsset, error) {
	currentVersion, err := ParseVersion(s.CurrentVersion())
	if err != nil {
		return UpdateInfo{}, githubRelease{}, githubAsset{}, fmt.Errorf("当前版本无效：%w", err)
	}

	release, err := s.fetchLatestRelease(ctx)
	if err != nil {
		return UpdateInfo{}, githubRelease{}, githubAsset{}, err
	}
	latestVersion, err := ParseVersion(release.TagName)
	if err != nil {
		return UpdateInfo{}, githubRelease{}, githubAsset{}, fmt.Errorf("GitHub Release 版本无效：%w", err)
	}

	info := UpdateInfo{
		CurrentVersion: s.CurrentVersion(),
		LatestVersion:  strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(release.TagName), "v"), "V"),
		ReleaseName:    strings.TrimSpace(release.Name),
		ReleaseURL:     strings.TrimSpace(release.HTMLURL),
		PublishedAt:    strings.TrimSpace(release.PublishedAt),
	}
	if info.LatestVersion == "" {
		info.LatestVersion = latestVersion.String()
	}

	if latestVersion.Compare(currentVersion) <= 0 {
		info.Message = "当前已是最新版本"
		return info, release, githubAsset{}, nil
	}

	asset, ok := selectPortableAsset(release.Assets)
	if !ok {
		return UpdateInfo{}, githubRelease{}, githubAsset{}, fmt.Errorf("Release %s 未找到 JavFlow 便携包 ZIP 资产", release.TagName)
	}
	info.UpdateAvailable = true
	info.AssetName = asset.Name
	info.AssetSize = asset.Size
	info.SHA256 = normalizeSHA256(asset.Digest)
	info.Message = "发现新版本"
	return info, release, asset, nil
}

func (s *Service) Download(ctx context.Context, requestedVersion string) (UpdateInfo, error) {
	if s == nil {
		return UpdateInfo{}, fmt.Errorf("在线更新服务未初始化")
	}
	requestContext, cancel := context.WithTimeout(normalizeContext(ctx), downloadTimeout)
	defer cancel()

	info, release, asset, err := s.checkRelease(requestContext)
	if err != nil {
		return UpdateInfo{}, err
	}
	if !info.UpdateAvailable {
		return info, fmt.Errorf("当前已是最新版本，无需下载")
	}
	if requested := strings.TrimSpace(requestedVersion); requested != "" {
		comparison, compareErr := CompareVersions(requested, info.LatestVersion)
		if compareErr != nil {
			return UpdateInfo{}, fmt.Errorf("待下载版本无效：%w", compareErr)
		}
		if comparison != 0 {
			return UpdateInfo{}, fmt.Errorf("待下载版本已变化，请重新检查更新")
		}
	}

	checksum, err := s.resolveExpectedChecksum(requestContext, release, asset)
	if err != nil {
		return UpdateInfo{}, err
	}
	info.SHA256 = checksum

	targetPath, err := s.runningExecutablePath()
	if err != nil {
		return UpdateInfo{}, err
	}
	targetDirectory := filepath.Dir(targetPath)
	if asset.Size < 0 || asset.Size > maxAssetBytes {
		return UpdateInfo{}, fmt.Errorf("更新文件大小异常，已拒绝下载")
	}

	assetURL, err := s.assetURL(asset)
	if err != nil {
		return UpdateInfo{}, err
	}
	client, err := s.httpClient(downloadTimeout)
	if err != nil {
		return UpdateInfo{}, err
	}
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, assetURL, nil)
	if err != nil {
		return UpdateInfo{}, err
	}
	setGitHubHeaders(request, "application/octet-stream")
	response, err := client.Do(request)
	if err != nil {
		return UpdateInfo{}, fmt.Errorf("下载更新文件失败：%w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return UpdateInfo{}, responseError(response)
	}
	if response.ContentLength > maxAssetBytes {
		return UpdateInfo{}, fmt.Errorf("更新文件超过大小限制，已拒绝下载")
	}

	temporaryFile, err := os.CreateTemp(targetDirectory, ".javflow-update-*.zip.part")
	if err != nil {
		return UpdateInfo{}, fmt.Errorf("无法在 EXE 目录创建更新临时文件：%w", err)
	}
	temporaryPath := temporaryFile.Name()
	keepTemporary := false
	defer func() {
		_ = temporaryFile.Close()
		if !keepTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	hasher := sha256.New()
	limitedBody := io.LimitReader(response.Body, maxAssetBytes+1)
	written, err := io.Copy(io.MultiWriter(temporaryFile, hasher), limitedBody)
	if err != nil {
		return UpdateInfo{}, fmt.Errorf("写入更新文件失败：%w", err)
	}
	if written > maxAssetBytes {
		return UpdateInfo{}, fmt.Errorf("更新文件超过大小限制，已拒绝保存")
	}
	if asset.Size > 0 && written != asset.Size {
		return UpdateInfo{}, fmt.Errorf("更新文件大小校验失败：收到 %d 字节，Release 声明 %d 字节", written, asset.Size)
	}
	if err := temporaryFile.Sync(); err != nil {
		return UpdateInfo{}, fmt.Errorf("更新文件落盘失败：%w", err)
	}
	if err := temporaryFile.Close(); err != nil {
		return UpdateInfo{}, fmt.Errorf("关闭更新临时文件失败：%w", err)
	}

	actualChecksum := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(actualChecksum, checksum) {
		return UpdateInfo{}, fmt.Errorf("更新文件 SHA-256 校验失败，文件可能已损坏或被替换")
	}
	stagedDirectory, err := os.MkdirTemp(targetDirectory, ".javflow-update-staging-*")
	if err != nil {
		return UpdateInfo{}, fmt.Errorf("无法创建便携包临时目录：%w", err)
	}
	if err := extractPortableArchive(temporaryPath, stagedDirectory); err != nil {
		_ = os.RemoveAll(stagedDirectory)
		return UpdateInfo{}, err
	}
	_ = os.Remove(temporaryPath)
	packageSHA256, err := packageTreeSHA256(stagedDirectory)
	if err != nil {
		_ = os.RemoveAll(stagedDirectory)
		return UpdateInfo{}, fmt.Errorf("无法校验解压后的便携包：%w", err)
	}
	if err := validatePortableExecutable(filepath.Join(stagedDirectory, "javflow.exe")); err != nil {
		_ = os.RemoveAll(stagedDirectory)
		return UpdateInfo{}, err
	}

	info.DownloadedPath = stagedDirectory
	info.DownloadReady = true
	s.mu.Lock()
	s.pending = &pendingUpdate{
		Info:            info,
		DownloadedPath:  stagedDirectory,
		StagedDirectory: stagedDirectory,
		TargetPath:      targetPath,
		SHA256:          checksum,
		PackageSHA256:   packageSHA256,
	}
	s.mu.Unlock()
	return info, nil
}

func (s *Service) fetchLatestRelease(ctx context.Context) (githubRelease, error) {
	endpoint := strings.TrimRight(s.apiBaseURL, "/") + "/repos/" + s.repository + "/releases/latest"
	client, err := s.httpClient(checkTimeout)
	if err != nil {
		return githubRelease{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return githubRelease{}, err
	}
	setGitHubHeaders(request, "application/vnd.github+json")
	response, err := client.Do(request)
	if err != nil {
		return githubRelease{}, fmt.Errorf("检查 GitHub 更新失败：%w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return githubRelease{}, responseError(response)
	}

	var release githubRelease
	decoder := json.NewDecoder(io.LimitReader(response.Body, 4*1024*1024))
	if err := decoder.Decode(&release); err != nil {
		return githubRelease{}, fmt.Errorf("解析 GitHub Release 失败：%w", err)
	}
	if strings.TrimSpace(release.TagName) == "" {
		return githubRelease{}, fmt.Errorf("GitHub 未返回有效的 Release 版本")
	}
	return release, nil
}

func (s *Service) resolveExpectedChecksum(ctx context.Context, release githubRelease, packageAsset githubAsset) (string, error) {
	if checksum := normalizeSHA256(packageAsset.Digest); checksum != "" {
		return checksum, nil
	}

	for _, asset := range release.Assets {
		if !isChecksumAssetForAsset(asset.Name, packageAsset.Name) {
			continue
		}
		assetURL, err := s.assetURL(asset)
		if err != nil {
			continue
		}
		client, err := s.httpClient(checkTimeout)
		if err != nil {
			return "", err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
		if err != nil {
			return "", err
		}
		setGitHubHeaders(request, "text/plain")
		response, err := client.Do(request)
		if err != nil {
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024))
		response.Body.Close()
		if readErr != nil || response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			continue
		}
		if checksum := parseChecksumText(string(body), packageAsset.Name); checksum != "" {
			return checksum, nil
		}
	}

	return "", fmt.Errorf("Release 未提供便携包的 SHA-256 校验值，请上传 digest 或 .sha256 校验文件")
}

func (s *Service) assetURL(asset githubAsset) (string, error) {
	value := strings.TrimSpace(asset.BrowserDownloadURL)
	if value == "" {
		value = strings.TrimSpace(asset.URL)
	}
	if value == "" {
		return "", fmt.Errorf("Release 资产缺少下载地址")
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("Release 资产地址无效：%w", err)
	}
	if !parsed.IsAbs() {
		base, baseErr := url.Parse(strings.TrimRight(s.apiBaseURL, "/") + "/")
		if baseErr != nil {
			return "", fmt.Errorf("Release 资产地址无效：%w", baseErr)
		}
		parsed = base.ResolveReference(parsed)
	}
	if !strings.EqualFold(parsed.Scheme, "https") && !strings.EqualFold(parsed.Scheme, "http") {
		return "", fmt.Errorf("Release 资产只支持 HTTP(S) 下载")
	}
	if strings.EqualFold(s.apiBaseURL, defaultAPIBaseURL) && !strings.EqualFold(parsed.Scheme, "https") {
		return "", fmt.Errorf("GitHub Release 资产必须使用 HTTPS")
	}
	return parsed.String(), nil
}

func (s *Service) httpClient(timeout time.Duration) (*http.Client, error) {
	if s.client != nil {
		return s.client, nil
	}

	proxyValue := ""
	if s.store != nil {
		if settings, err := s.store.Load(); err == nil {
			proxyValue, _ = settings["proxy"].(string)
		}
	}
	proxyValue = proxyservice.NormalizeProxyValue(proxyValue)
	if proxyValue != "" {
		parsedProxy, err := url.Parse(proxyValue)
		if err != nil || (!strings.EqualFold(parsedProxy.Scheme, "http") && !strings.EqualFold(parsedProxy.Scheme, "https")) {
			return nil, fmt.Errorf("在线更新只支持 HTTP/HTTPS 代理，请检查全局代理设置")
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = http.ProxyURL(parsedProxy)
		return &http.Client{Timeout: timeout, Transport: transport}, nil
	}

	client := &http.Client{Timeout: timeout, Transport: netguard.NewPublicTransport()}
	netguard.ApplyRedirectPolicy(client)
	return client, nil
}

func (s *Service) runningExecutablePath() (string, error) {
	pathValue := strings.TrimSpace(s.executablePath)
	if pathValue == "" {
		resolved, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("无法定位当前 EXE：%w", err)
		}
		pathValue = resolved
	}
	absolute, err := filepath.Abs(pathValue)
	if err != nil {
		return "", fmt.Errorf("当前 EXE 路径无效：%w", err)
	}
	if info, err := os.Stat(absolute); err != nil || info.IsDir() {
		if err == nil {
			err = fmt.Errorf("路径是目录")
		}
		return "", fmt.Errorf("当前 EXE 不可用：%w", err)
	}
	return filepath.Clean(absolute), nil
}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func setGitHubHeaders(request *http.Request, accept string) {
	request.Header.Set("Accept", accept)
	request.Header.Set("User-Agent", "JavFlow-Update-Client/0.4")
}

func responseError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 8*1024))
	detail := strings.TrimSpace(string(body))
	if detail == "" {
		detail = response.Status
	}
	return fmt.Errorf("远程更新服务返回异常：%s", detail)
}

func selectPortableAsset(assets []githubAsset) (githubAsset, bool) {
	for _, asset := range assets {
		name := strings.ToLower(strings.TrimSpace(asset.Name))
		if strings.HasPrefix(name, "javflow-portable-") && strings.HasSuffix(name, ".zip") {
			return asset, true
		}
	}
	return githubAsset{}, false
}

func isChecksumAsset(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	return strings.Contains(lower, "sha256") || strings.Contains(lower, "checksum")
}

func isChecksumAssetForAsset(checksumName string, assetName string) bool {
	if !isChecksumAsset(checksumName) {
		return false
	}

	checksumBase := strings.ToLower(filepath.Base(strings.TrimSpace(checksumName)))
	assetBase := strings.ToLower(filepath.Base(strings.TrimSpace(assetName)))
	if assetBase != "" && strings.Contains(checksumBase, assetBase) {
		return true
	}

	// Aggregate checksum manifests are allowed; parseChecksumText will still
	// require the executable name when the manifest contains multiple entries.
	switch checksumBase {
	case "sha256sums", "sha256sums.txt", "checksums", "checksums.txt":
		return true
	default:
		return false
	}
}

func normalizeSHA256(value string) string {
	cleaned := strings.TrimSpace(strings.ToLower(value))
	cleaned = strings.TrimPrefix(cleaned, "sha256:")
	cleaned = strings.Trim(cleaned, " \t\r\n=():[]{}\"")
	if len(cleaned) != sha256.Size*2 {
		return ""
	}
	if _, err := hex.DecodeString(cleaned); err != nil {
		return ""
	}
	return cleaned
}

func parseChecksumText(contents string, assetName string) string {
	requestedName := strings.ToLower(filepath.Base(strings.TrimSpace(assetName)))
	candidates := make([]string, 0, 2)
	for _, line := range strings.Split(contents, "\n") {
		cleanLine := strings.TrimSpace(line)
		if cleanLine == "" {
			continue
		}
		fields := strings.Fields(cleanLine)
		for _, field := range fields {
			candidate := normalizeSHA256(field)
			if candidate == "" {
				continue
			}
			if requestedName == "" || strings.Contains(strings.ToLower(cleanLine), requestedName) {
				return candidate
			}
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) == 1 {
		return candidates[0]
	}
	return ""
}

func validatePortableExecutable(pathValue string) error {
	file, err := os.Open(pathValue)
	if err != nil {
		return fmt.Errorf("无法验证更新文件：%w", err)
	}
	defer file.Close()
	header := make([]byte, 2)
	if _, err := io.ReadFull(file, header); err != nil || header[0] != 'M' || header[1] != 'Z' {
		return fmt.Errorf("更新文件不是有效的 Windows EXE")
	}
	return nil
}

func extractPortableArchive(archivePath string, targetDirectory string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("更新文件不是有效的便携包 ZIP：%w", err)
	}
	defer reader.Close()

	seen := make(map[string]struct{}, len(reader.File))
	var extractedBytes int64
	for _, entry := range reader.File {
		relativePath, isDirectory, err := portableArchivePath(entry.Name)
		if err != nil {
			return err
		}
		if relativePath == "" {
			continue
		}
		if _, duplicate := seen[relativePath]; duplicate {
			return fmt.Errorf("便携包包含重复文件：%s", entry.Name)
		}
		seen[relativePath] = struct{}{}
		absolutePath := filepath.Join(targetDirectory, filepath.FromSlash(relativePath))
		if !pathWithinDirectory(targetDirectory, absolutePath) {
			return fmt.Errorf("便携包路径越界：%s", entry.Name)
		}
		if isDirectory {
			if err := os.MkdirAll(absolutePath, 0o755); err != nil {
				return fmt.Errorf("无法创建便携包目录：%w", err)
			}
			continue
		}
		if entry.FileInfo().Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("便携包不允许包含符号链接：%s", entry.Name)
		}
		if entry.UncompressedSize64 > uint64(maxAssetBytes) || extractedBytes+int64(entry.UncompressedSize64) > maxAssetBytes {
			return fmt.Errorf("便携包解压后超过大小限制，已拒绝处理")
		}
		if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
			return fmt.Errorf("无法创建便携包文件目录：%w", err)
		}
		input, err := entry.Open()
		if err != nil {
			return fmt.Errorf("无法读取便携包文件 %s：%w", entry.Name, err)
		}
		output, err := os.OpenFile(absolutePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, portableFileMode(relativePath))
		if err != nil {
			input.Close()
			return fmt.Errorf("无法写入便携包文件 %s：%w", entry.Name, err)
		}
		written, copyErr := io.Copy(output, io.LimitReader(input, maxAssetBytes+1))
		closeInputErr := input.Close()
		closeOutputErr := output.Close()
		if copyErr != nil || closeInputErr != nil || closeOutputErr != nil || written != int64(entry.UncompressedSize64) {
			return fmt.Errorf("便携包文件写入校验失败：%s", entry.Name)
		}
		extractedBytes += written
	}

	if _, err := os.Stat(filepath.Join(targetDirectory, "javflow.exe")); err != nil {
		return fmt.Errorf("便携包缺少根目录 javflow.exe")
	}
	return nil
}

func portableArchivePath(rawPath string) (string, bool, error) {
	cleaned := strings.ReplaceAll(strings.TrimSpace(rawPath), "\\", "/")
	if cleaned == "" {
		return "", false, nil
	}
	if strings.HasPrefix(cleaned, "/") || strings.Contains(cleaned, ":") {
		return "", false, fmt.Errorf("便携包路径无效：%s", rawPath)
	}
	normalized := path.Clean(cleaned)
	if normalized == "." || normalized == ".." || strings.HasPrefix(normalized, "../") {
		return "", false, fmt.Errorf("便携包路径越界：%s", rawPath)
	}
	return normalized, strings.HasSuffix(cleaned, "/"), nil
}

func portableFileMode(relativePath string) os.FileMode {
	if strings.EqualFold(filepath.Base(filepath.FromSlash(relativePath)), "javflow.exe") {
		return 0o755
	}
	return 0o644
}

func pathWithinDirectory(directory string, candidate string) bool {
	root, err := filepath.Abs(directory)
	if err != nil {
		return false
	}
	target, err := filepath.Abs(candidate)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(root, target)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func packageTreeSHA256(directory string) (string, error) {
	files := make([]string, 0)
	err := filepath.WalkDir(directory, func(pathValue string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("便携包临时目录不允许包含符号链接：%s", pathValue)
		}
		relative, err := filepath.Rel(directory, pathValue)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	hasher := sha256.New()
	for _, relative := range files {
		if _, err := io.WriteString(hasher, relative+"\x00"); err != nil {
			return "", err
		}
		file, err := os.Open(filepath.Join(directory, filepath.FromSlash(relative)))
		if err != nil {
			return "", err
		}
		_, copyErr := io.Copy(hasher, file)
		closeErr := file.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func fileSHA256(pathValue string) (string, error) {
	file, err := os.Open(pathValue)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
