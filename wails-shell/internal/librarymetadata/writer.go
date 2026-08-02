// Ownership summary:
//   This file implements domain logic for the javflow backend.
//
// File map for maintainers:
//   1) Domain-specific types and helpers.
//   2) Internal service implementation.
//

package librarymetadata

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"javflow/internal/proxy"
)

const maxImageBytes int64 = 25 * 1024 * 1024

var imagePathLocks sync.Map

// WriteOptions controls which files are generated for a single library item.
type WriteOptions struct {
	Item          LibraryMediaItem
	Info          *MovieInfo
	FallbackInfos []ImageInfo
	Proxy         string
	// LibraryRoot is the root of the media library. When non-empty, actor cover
	// images are cached in a single .actors directory at the library root rather
	// than next to each individual NFO file.
	LibraryRoot string
	SkipNfo     bool
	SkipImages  bool
	// SkipPoster/SkipBackdrop/SkipLandscape allow retry modes to download only
	// the missing image types instead of rewriting everything.
	SkipPoster    bool
	SkipBackdrop  bool
	SkipLandscape bool
	// ImageFetcher, when set, is used for provider-aware image downloads. It
	// mirrors MetaTube's Engine.Fetch behavior so providers can supply their own
	// headers/cookies/referers.
	ImageFetcher func(imageURL, providerName string) ([]byte, error)
}

// WriteResult reports the files that were written, or the first error encountered.
type WriteResult struct {
	NfoPath       string `json:"nfoPath,omitempty"`
	PosterPath    string `json:"posterPath,omitempty"`
	BackdropPath  string `json:"backdropPath,omitempty"`
	LandscapePath string `json:"landscapePath,omitempty"`
	Error         string `json:"error,omitempty"`
}

// WriteMetadata writes the NFO file and downloads cover/background images for a
// single library item. It is safe to call when some image URLs are empty: those
// downloads are simply skipped.
func WriteMetadata(options WriteOptions) WriteResult {
	return WriteMetadataContext(context.Background(), options)
}

// WriteMetadataContext is the cancellable writer used by active media-library
// jobs. The legacy WriteMetadata wrapper keeps existing callers compatible.
func WriteMetadataContext(parent context.Context, options WriteOptions) WriteResult {
	item := options.Item
	info := normalizeInfoForItem(item, options.Info)

	if info == nil {
		return WriteResult{Error: "缺少影片元数据"}
	}

	result := WriteResult{}

	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()

	proxyURL := proxy.NormalizeProxyValue(options.Proxy)

	transport := &http.Transport{}
	if proxyURL != "" {
		parsedProxy, err := url.Parse(proxyURL)
		if err == nil {
			transport.Proxy = http.ProxyURL(parsedProxy)
		}
	}
	imageClient := &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}

	if !options.SkipNfo {
		// 在生成 NFO 之前先下载演员封面图，这样 NFO 里可以引用本地相对路径。
		downloadActorImages(ctx, imageClient, info, item.NfoPath, options.LibraryRoot)

		if err := writeNFO(item.NfoPath, info); err != nil {
			result.Error = fmt.Sprintf("写入 NFO 失败：%s", err.Error())
			return result
		}
		result.NfoPath = item.NfoPath
	}

	if options.SkipImages {
		return result
	}

	type imageTask struct {
		urls         []string
		destPath     string
		kind         string
		providerName string
		useFetcher   bool
		setPath      func(string)
	}

	buildImageURLs := func(primaryURL string, extractors ...func(ImageInfo) string) []string {
		urls := []string{}
		if strings.TrimSpace(primaryURL) != "" {
			urls = append(urls, primaryURL)
		}
		for _, fb := range options.FallbackInfos {
			for _, extractor := range extractors {
				candidate := strings.TrimSpace(extractor(fb))
				if candidate == "" {
					continue
				}
				// 避免重复 URL。
				duplicate := false
				for _, existing := range urls {
					if strings.EqualFold(existing, candidate) {
						duplicate = true
						break
					}
				}
				if !duplicate {
					urls = append(urls, candidate)
				}
			}
		}
		return urls
	}

	providerName := strings.TrimSpace(info.Provider)
	referer := strings.TrimSpace(info.Homepage)

	tasks := []imageTask{}
	if !options.SkipPoster {
		tasks = append(tasks, imageTask{buildImageURLs(info.CoverURL, func(fb ImageInfo) string { return fb.CoverURL }), item.PosterPath, "封面", providerName, true, func(p string) { result.PosterPath = p }})
	}
	if !options.SkipBackdrop {
		// 背景图回退链：主源 BackdropURL -> 备用源 BackdropURL/BigThumbURL/ThumbURL。
		// 与 MetaTube 一致，主源优先使用 BigCoverURL/CoverURL；当主源失败时继续尝试
		// 其他可能为横图的源。
		tasks = append(tasks, imageTask{
			urls: buildImageURLs(info.BackdropURL,
				func(fb ImageInfo) string { return fb.BackdropURL },
				func(fb ImageInfo) string { return fb.BigThumbURL },
				func(fb ImageInfo) string { return fb.ThumbURL },
			),
			destPath:     item.BackdropPath,
			kind:         "背景",
			providerName: providerName,
			useFetcher:   true,
			setPath:      func(p string) { result.BackdropPath = p },
		})
	}
	if !options.SkipLandscape {
		tasks = append(tasks, imageTask{buildImageURLs(info.ThumbURL, func(fb ImageInfo) string { return fb.ThumbURL }), item.LandscapePath, "横图", providerName, true, func(p string) { result.LandscapePath = p }})
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var failedParts []string

	for _, task := range tasks {
		if len(task.urls) == 0 || task.destPath == "" {
			fmt.Printf("[DEBUG] skip task %s: urls=%d destPath=%q\n", task.kind, len(task.urls), task.destPath)
			continue
		}
		wg.Add(1)
		go func(t imageTask) {
			defer wg.Done()
			fmt.Printf("[DEBUG] task %s useFetcher=%v fetcherNil=%v urls=%v\n", t.kind, t.useFetcher, options.ImageFetcher == nil, t.urls)
			if t.useFetcher && options.ImageFetcher != nil {
				if err := downloadImageWithEngineFallback(ctx, options.ImageFetcher, t.urls, t.destPath, t.providerName); err == nil {
					mu.Lock()
					t.setPath(t.destPath)
					mu.Unlock()
					return
				}
			}
			if err := downloadImageWithFallback(ctx, imageClient, t.urls, t.destPath, referer); err != nil {
				mu.Lock()
				failedParts = append(failedParts, fmt.Sprintf("%s（%s）", t.kind, err.Error()))
				mu.Unlock()
				return
			}
			mu.Lock()
			t.setPath(t.destPath)
			mu.Unlock()
		}(task)
	}

	wg.Wait()
	if len(failedParts) > 0 {
		result.Error = "下载失败：" + strings.Join(failedParts, "；")
	}

	return result
}

// normalizeInfoForItem restores a local split suffix for output metadata while
// keeping the shared remote lookup result keyed by the base number. A/B files
// therefore reuse one scrape request but receive distinct Emby titles.
func normalizeInfoForItem(item LibraryMediaItem, info *MovieInfo) *MovieInfo {
	if info == nil {
		return nil
	}
	outputCode := strings.TrimSpace(item.DisplayCode)
	baseCode := strings.TrimSpace(item.Code)
	if outputCode == "" || baseCode == "" || strings.EqualFold(outputCode, baseCode) {
		return info
	}

	next := *info
	next.Number = outputCode
	title := strings.TrimSpace(next.Title)
	if stripped := stripLeadingMovieCode(title, outputCode); stripped != title {
		title = stripped
	} else if stripped := stripLeadingMovieCode(title, baseCode); stripped != title {
		title = stripped
	}
	next.Title = title
	return &next
}

func stripLeadingMovieCode(title, code string) string {
	title = strings.TrimSpace(title)
	code = strings.TrimSpace(code)
	if title == "" || code == "" {
		return title
	}
	if strings.EqualFold(title, code) {
		return ""
	}
	upper := strings.ToUpper(title)
	prefix := strings.ToUpper(code)
	for _, separator := range []string{" - ", "-", " : ", ": ", ":", " _ ", "_", " ", "\u3000"} {
		candidate := prefix + separator
		if strings.HasPrefix(upper, candidate) {
			return strings.TrimSpace(title[len(candidate):])
		}
	}
	return title
}

func isForbiddenImageHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		return true
	}
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
	}
	return false
}

func writeNFO(destPath string, info *MovieInfo) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	nfoBytes, err := BuildNFO(info)
	if err != nil {
		return err
	}
	return os.WriteFile(destPath, nfoBytes, 0o644)
}

// downloadImageWithEngineFallback tries the provider-aware fetcher for each URL.
// It mirrors MetaTube's Engine.Fetch + image validation flow.
func downloadImageWithEngineFallback(ctx context.Context, fetcher func(string, string) ([]byte, error), urls []string, destPath, providerName string) error {
	if len(urls) == 0 {
		return fmt.Errorf("没有可用的图片 URL")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	var lastErr error
	for _, imageURL := range urls {
		fmt.Printf("[DEBUG] engine fetcher url=%s provider=%s\n", imageURL, providerName)
		data, err := fetcher(imageURL, providerName)
		if err != nil {
			fmt.Printf("[DEBUG] engine fetcher error: %v\n", err)
			lastErr = err
			continue
		}
		fmt.Printf("[DEBUG] engine fetcher got data len=%d\n", len(data))
		if err := validateImageBytes(data); err != nil {
			fmt.Printf("[DEBUG] validate error: %v\n", err)
			lastErr = err
			continue
		}
		if err := writeImageData(destPath, data); err != nil {
			fmt.Printf("[DEBUG] write error: %v\n", err)
			lastErr = err
			continue
		}
		fmt.Printf("[DEBUG] engine fetcher success\n")
		return nil
	}
	return lastErr
}

// downloadImageWithFallback tries each URL in order until one succeeds.
func downloadImageWithFallback(ctx context.Context, client *http.Client, urls []string, destPath string, referer string) error {
	if len(urls) == 0 {
		return fmt.Errorf("没有可用的图片 URL")
	}

	var lastErr error
	for _, imageURL := range urls {
		if err := downloadImage(ctx, client, imageURL, destPath, referer); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}

func downloadImage(ctx context.Context, client *http.Client, imageURL, destPath string, referer string) error {
	if strings.TrimSpace(imageURL) == "" {
		return fmt.Errorf("图片 URL 为空")
	}

	parsedURL, err := url.Parse(strings.TrimSpace(imageURL))
	if err != nil {
		return fmt.Errorf("图片 URL 格式无效：%w", err)
	}
	if err := validateRemoteImageURL(ctx, parsedURL); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}

	clientCopy := *client
	clientCopy.CheckRedirect = func(redirectReq *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("重定向次数过多")
		}
		return validateRemoteImageURL(redirectReq.Context(), redirectReq.URL)
	}
	resp, err := clientCopy.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxImageBytes {
		return fmt.Errorf("图片超过 %d MB", maxImageBytes/(1024*1024))
	}

	lockKey := filepath.Clean(destPath)
	pathLockValue, _ := imagePathLocks.LoadOrStore(lockKey, &sync.Mutex{})
	pathLock := pathLockValue.(*sync.Mutex)
	pathLock.Lock()
	defer pathLock.Unlock()

	file, err := os.CreateTemp(filepath.Dir(destPath), "."+filepath.Base(destPath)+".*.tmp")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	bytesWritten, copyErr := io.Copy(file, io.LimitReader(resp.Body, maxImageBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if bytesWritten > maxImageBytes {
		return fmt.Errorf("图片超过 %d MB", maxImageBytes/(1024*1024))
	}

	// 某些图片 CDN 不返回 Content-Type 或返回 application/octet-stream，
	// 因此先读取已下载的内容进行图片解码校验，而不是单纯依赖响应头。
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0]))
	if contentType != "" && !strings.HasPrefix(contentType, "image/") {
		data, readErr := os.ReadFile(tempPath)
		if readErr != nil {
			return fmt.Errorf("响应不是图片（Content-Type: %s）", contentType)
		}
		if err := validateImageBytes(data); err != nil {
			return fmt.Errorf("响应不是图片（Content-Type: %s）：%w", contentType, err)
		}
	}

	if err := os.Rename(tempPath, destPath); err != nil {
		if removeErr := os.Remove(destPath); removeErr == nil {
			if retryErr := os.Rename(tempPath, destPath); retryErr == nil {
				return nil
			}
		}
		return err
	}
	return nil
}

func validateRemoteImageURL(ctx context.Context, parsedURL *url.URL) error {
	if parsedURL == nil || (!strings.EqualFold(parsedURL.Scheme, "http") && !strings.EqualFold(parsedURL.Scheme, "https")) {
		return fmt.Errorf("不支持的图片协议")
	}
	if isForbiddenImageHost(parsedURL.Hostname()) {
		return fmt.Errorf("图片 URL 不允许指向本机或私有网络")
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupIPAddr(lookupCtx, parsedURL.Hostname())
	if err != nil {
		return fmt.Errorf("图片域名解析失败：%w", err)
	}
	for _, address := range addresses {
		if address.IP.IsLoopback() || address.IP.IsPrivate() || address.IP.IsLinkLocalUnicast() || address.IP.IsLinkLocalMulticast() {
			return fmt.Errorf("图片域名解析到本机或私有网络")
		}
	}
	return nil
}

// validateImageBytes checks whether the given bytes represent a decodable image.
// It matches MetaTube's image decoding path and tolerates JPEGs that standard
// library decoding rejects.
func validateImageBytes(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("图片数据为空")
	}
	_, _, err := image.Decode(bytes.NewReader(data))
	return err
}

// writeImageData atomically writes validated image bytes to destPath.
func writeImageData(destPath string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	lockKey := filepath.Clean(destPath)
	pathLockValue, _ := imagePathLocks.LoadOrStore(lockKey, &sync.Mutex{})
	pathLock := pathLockValue.(*sync.Mutex)
	pathLock.Lock()
	defer pathLock.Unlock()

	file, err := os.CreateTemp(filepath.Dir(destPath), "."+filepath.Base(destPath)+".*.tmp")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, destPath); err != nil {
		if removeErr := os.Remove(destPath); removeErr == nil {
			if retryErr := os.Rename(tempPath, destPath); retryErr == nil {
				return nil
			}
		}
		return err
	}
	return nil
}

// downloadActorImages downloads actor portraits into a single .actors folder at
// the library root and updates info.ActorImages URLs to relative paths from the
// NFO file so that the generated NFO can reference them.
func downloadActorImages(ctx context.Context, client *http.Client, info *MovieInfo, nfoPath, libraryRoot string) {
	if info == nil || len(info.ActorImages) == 0 || nfoPath == "" {
		return
	}

	actorsDir := filepath.Join(filepath.Dir(nfoPath), ".actors")
	if libraryRoot != "" {
		actorsDir = filepath.Join(libraryRoot, ".actors")
	}
	if err := os.MkdirAll(actorsDir, 0o755); err != nil {
		return
	}

	// Compute the relative path from the NFO directory to the global actors
	// directory so NFO references work regardless of how deep the movie sits.
	actorsRel := ".actors"
	if rel, err := filepath.Rel(filepath.Dir(nfoPath), actorsDir); err == nil && rel != "" {
		actorsRel = filepath.ToSlash(rel)
	}

	for i := range info.ActorImages {
		ai := &info.ActorImages[i]
		if ai.URL == "" {
			continue
		}
		fileName := sanitizeActorFileName(ai.Name) + ".jpg"
		destPath := filepath.Join(actorsDir, fileName)
		if err := downloadImageWithFallback(ctx, client, []string{ai.URL}, destPath, ""); err != nil {
			continue
		}
		ai.URL = actorsRel + "/" + fileName
	}
}

func sanitizeActorFileName(name string) string {
	replacer := strings.NewReplacer(
		"\\", "_",
		"/", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	return strings.TrimSpace(replacer.Replace(name))
}
