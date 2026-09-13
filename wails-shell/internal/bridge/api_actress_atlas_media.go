package bridge

// Ownership summary:
// 1) cache actor-atlas work covers behind the local subscription-media route
// 2) preserve direct public URLs whenever optional visual enrichment fails
// 3) isolate WebView2 hotlink handling from crawl, NFO and subscription state
//
// File map for maintainers:
// 1) bounded Actor Atlas work-cover cache
// 2) local media route conversion for the selected actor detail page

// This file owns the Actor Atlas-only conversion from remote portraits and JAV
// work covers to app-served media. It intentionally does not persist crawler,
// NFO, or subscription records; these files are display caches in user data.

import (
	"context"
	"crypto/sha1"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"javflow/internal/actresslookup"
	"javflow/internal/actressranking"
	"javflow/internal/contracts/subscriptiontarget"
)

// 内嵌榜单头像：随 EXE 发布的基线头像库（按源 URL 哈希命名的 avatar-*.jpg）。
// 启动后释放到用户数据媒体目录，榜单头像即本地秒开，无需联网下载。
//
//go:embed data/atlas-avatars
var embeddedAtlasAvatars embed.FS

const embeddedAtlasAvatarsRoot = "data/atlas-avatars"

// ensureEmbeddedAtlasAvatarsExtracted releases any embedded baseline avatar
// that is not yet present in the user-data media directory. Runtime downloads
// are never overwritten; extraction only fills missing files. The operation is
// idempotent (one stat per embedded file), so no once-guard is needed — a once
// guard would silently skip extraction for any media directory other than the
// first caller's.
var (
	atlasAvatarNameIndexMu sync.Mutex
	atlasAvatarNameIndex   map[string]string
)

func normalizeAtlasAvatarNameKey(name string) string {
	key := strings.ToLower(strings.TrimSpace(name))
	if idx := strings.IndexAny(key, "（("); idx > 0 {
		key = strings.TrimSpace(key[:idx])
	}
	return key
}

// atlasAvatarFileByName resolves an actress display name to an embedded
// baseline avatar file. New month listings may reference different portrait
// URL variants, but the actress name is stable across periods, so the name
// index keeps cross-source avatar reuse working.
func atlasAvatarFileByName(name string) string {
	atlasAvatarNameIndexMu.Lock()
	defer atlasAvatarNameIndexMu.Unlock()
	if atlasAvatarNameIndex == nil {
		atlasAvatarNameIndex = map[string]string{}
		payload, err := fs.ReadFile(embeddedAtlasAvatars, embeddedAtlasAvatarsRoot+"/index.json")
		if err != nil {
			return ""
		}
		var rawIndex map[string]string
		if err := json.Unmarshal(payload, &rawIndex); err != nil {
			return ""
		}
		for indexedName, fileName := range rawIndex {
			key := strings.ToLower(strings.TrimSpace(indexedName))
			if key == "" || strings.TrimSpace(fileName) == "" {
				continue
			}
			atlasAvatarNameIndex[key] = fileName
			// Official names may carry a former name in parentheses. Keep the
			// base name as a secondary key so either display form resolves the
			// same embedded portrait.
			baseKey := normalizeAtlasAvatarNameKey(key)
			if baseKey != "" {
				if _, exists := atlasAvatarNameIndex[baseKey]; !exists {
					atlasAvatarNameIndex[baseKey] = fileName
				}
			}
		}
	}
	key := strings.ToLower(strings.TrimSpace(name))
	if fileName := atlasAvatarNameIndex[key]; fileName != "" {
		return fileName
	}
	return atlasAvatarNameIndex[normalizeAtlasAvatarNameKey(key)]
}

func ensureEmbeddedAtlasAvatarsExtracted(mediaDir string) {
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		return
	}
	entries, err := fs.ReadDir(embeddedAtlasAvatars, embeddedAtlasAvatarsRoot)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "index.json" {
			continue
		}
		target := filepath.Join(mediaDir, entry.Name())
		if _, err := os.Stat(target); err == nil {
			continue
		}
		payload, readErr := fs.ReadFile(embeddedAtlasAvatars, embeddedAtlasAvatarsRoot+"/"+entry.Name())
		if readErr != nil {
			continue
		}
		_ = os.WriteFile(target, payload, 0o644)
	}
}

const (
	// One visible page (8) plus the next three pages (24) are accepted in one
	// bridge call. The worker itself still caps network concurrency at three.
	actressAtlasWorkCoverLimit       = 24
	actressAtlasWorkCoverParallelism = 3
	actressAtlasRankingAvatarLimit   = 100
	actressAtlasRankingParallelism   = 5
)

// cacheActressAtlasRankingAvatars converts the ranking's remote portraits to
// same-origin application media URLs. Ranking results are intentionally passed
// in by the renderer: this keeps source selection and historical ranking cache
// ownership inside actressranking while the bridge owns only media transport.
// The cache is keyed by source URL, so the same actor shared by several
// historical periods is stored once.
func (a *API) cacheActressAtlasRankingAvatars(ctx context.Context, items []actressranking.RankingItem, proxyValue string) ([]actressranking.RankingItem, int, int, error) {
	updated := append([]actressranking.RankingItem(nil), items...)
	if a == nil || a.runtime.paths.UserData == "" || len(updated) == 0 {
		return updated, 0, 0, nil
	}
	if len(updated) > actressAtlasRankingAvatarLimit {
		updated = updated[:actressAtlasRankingAvatarLimit]
	}
	mediaDir := filepath.Join(a.runtime.paths.UserData, "subscriptions-v2", "media", "atlas-ranking")
	ensureEmbeddedAtlasAvatarsExtracted(mediaDir)
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		return updated, 0, 0, err
	}
	client, err := newSubscriptionMediaHTTPClient(proxyValue)
	if err != nil {
		return updated, 0, 0, err
	}

	// Collapse duplicate portraits before starting workers. This both avoids
	// duplicate downloads and prevents two goroutines writing the same file.
	bySource := make(map[string][]int, len(updated))
	for index, item := range updated {
		if strings.HasPrefix(item.ImageURL, "data:") {
			// 已内联的头像不进入下载/回写流程，保持 data URL 原样。
			continue
		}
		source := strings.TrimSpace(item.SourceImageURL)
		if source == "" && !isLocalSubscriptionMediaURL(item.ImageURL) {
			source = strings.TrimSpace(item.ImageURL)
		}
		if source == "" {
			continue
		}
		bySource[source] = append(bySource[source], index)
	}
	if len(bySource) == 0 {
		return updated, 0, 0, nil
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	type avatarJob struct {
		source  string
		indexes []int
	}
	jobs := make([]avatarJob, 0, len(bySource))
	for source, indexes := range bySource {
		jobs = append(jobs, avatarJob{source: source, indexes: indexes})
	}
	semaphore := make(chan struct{}, actressAtlasRankingParallelism)
	var workers sync.WaitGroup
	var mu sync.Mutex
	cachedCount := 0
	failedCount := 0
	for _, job := range jobs {
		job := job
		workers.Add(1)
		go func() {
			defer workers.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-timeoutCtx.Done():
				mu.Lock()
				failedCount += len(job.indexes)
				mu.Unlock()
				return
			}

			localURL := findActressAtlasRankingAvatar(mediaDir, job.source)
			var cacheErr error
			if localURL == "" {
				fileStem := actressAtlasRankingAvatarStem(job.source)
				localURL, cacheErr = cacheNamedSubscriptionMedia(timeoutCtx, client, job.source, "", mediaDir, fileStem)
			}
			mu.Lock()
			defer mu.Unlock()
			if cacheErr != nil || localURL == "" {
				failedCount += len(job.indexes)
				return
			}
			for _, index := range job.indexes {
				updated[index].SourceImageURL = job.source
				updated[index].ImageURL = localURL
				cachedCount++
			}
		}()
	}
	workers.Wait()
	return updated, cachedCount, failedCount, nil
}

func isLocalSubscriptionMediaURL(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	return err == nil && parsed.Scheme == "" && strings.HasPrefix(parsed.Path, subscriptionMediaURLPrefix)
}

func actressAtlasRankingAvatarStem(source string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(source)))
	return fmt.Sprintf("avatar-%x", sum[:8])
}

// resolveActressRankingAvatarURLs rewrites ranking portraits to the cached
// same-origin media route whenever the avatar file already exists locally.
// Cached periods then render instantly from disk instead of re-entering the
// avatar download flow on every month switch.
func (a *API) resolveActressRankingAvatarURLs(items []actressranking.RankingItem) []actressranking.RankingItem {
	if a == nil || a.runtime.paths.UserData == "" {
		return items
	}
	mediaDir := filepath.Join(a.runtime.paths.UserData, "subscriptions-v2", "media", "atlas-ranking")
	ensureEmbeddedAtlasAvatarsExtracted(mediaDir)
	if len(items) == 0 {
		return items
	}
	for index := range items {
		source := strings.TrimSpace(items[index].ImageURL)
		var inline string
		if source != "" && strings.HasPrefix(source, subscriptionMediaURLPrefix) {
			// Older cache entries may already contain a local route. Read that
			// file directly so WebView2 never has to resolve a stale route.
			if avatarFile := findActressAtlasLocalMediaFile(mediaDir, source); avatarFile != "" {
				inline = inlineImageDataURL(avatarFile)
			}
		} else if source != "" {
			if avatarFile := findActressAtlasRankingAvatarFile(mediaDir, source); avatarFile != "" {
				inline = inlineImageDataURL(avatarFile)
			}
		}
		if inline == "" {
			nameFile := atlasAvatarFileByName(items[index].ActressName)
			if nameFile == "" {
				continue
			}
			// 新月份的图片 URL 形式可能变化，但演员名不变：
			// 按名字命中内嵌基线库，跨数据源/跨月也能复用同一张头像。
			inline = inlineImageDataURL(filepath.Join(mediaDir, nameFile))
		}
		if inline == "" {
			continue
		}
		if strings.TrimSpace(items[index].SourceImageURL) == "" && source != "" {
			items[index].SourceImageURL = source
		}
		items[index].ImageURL = inline
	}
	return items
}

// findActressAtlasLocalMediaFile validates a previously serialized local
// subscription-media route and returns its file only within atlas-ranking.
func findActressAtlasLocalMediaFile(mediaDir, rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme != "" {
		return ""
	}
	prefix := subscriptionMediaURLPrefix + filepath.Base(mediaDir) + "/"
	if !strings.HasPrefix(parsed.Path, prefix) {
		return ""
	}
	filePath := filepath.Join(mediaDir, filepath.Base(parsed.Path))
	info, err := os.Stat(filePath)
	if err != nil || info.IsDir() {
		return ""
	}
	return filePath
}

// findActressAtlasRankingAvatarFile returns the on-disk path of the cached
// ranking avatar for the given source URL, or "" when not cached yet.
func findActressAtlasRankingAvatarFile(mediaDir, source string) string {
	stem := actressAtlasRankingAvatarStem(source)
	for _, extension := range []string{".jpg", ".png", ".webp", ".gif", ".avif"} {
		filePath := filepath.Join(mediaDir, stem+extension)
		if _, err := os.Stat(filePath); err == nil {
			return filePath
		}
	}
	return ""
}

// inlineImageDataURL encodes a local image as a base64 data URL so the
// renderer never issues a network request for baseline portraits.
func inlineImageDataURL(filePath string) string {
	payload, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}
	mimeType := "image/jpeg"
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".png":
		mimeType = "image/png"
	case ".webp":
		mimeType = "image/webp"
	case ".gif":
		mimeType = "image/gif"
	case ".avif":
		mimeType = "image/avif"
	}
	return fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(payload))
}

func findActressAtlasRankingAvatar(mediaDir, source string) string {
	stem := actressAtlasRankingAvatarStem(source)
	for _, extension := range []string{".jpg", ".png", ".webp", ".gif", ".avif"} {
		filePath := filepath.Join(mediaDir, stem+extension)
		if _, err := os.Stat(filePath); err == nil {
			return subscriptionMediaAssetURL(mediaDir, filePath)
		}
	}
	return ""
}

// cacheActressAtlasWorkCovers preserves the original source URL whenever a
// download fails, but replaces successful cover URLs with the same-origin
// /subscription-media/ route. WebView2 can reliably render that local route
// even when JAVBus rejects a direct hotlink.
func (a *API) cacheActressAtlasWorkCovers(ctx context.Context, profile subscriptiontarget.TargetProfile, proxyValue string) subscriptiontarget.TargetProfile {
	if a == nil || a.runtime.paths.UserData == "" || len(profile.Works) == 0 {
		return profile
	}

	limit := len(profile.Works)
	if limit > actressAtlasWorkCoverLimit {
		limit = actressAtlasWorkCoverLimit
	}
	mediaDir := filepath.Join(a.runtime.paths.UserData, "subscriptions-v2", "media", actressAtlasMediaKey(profile.ResolvedActressName)+"-works")
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		return profile
	}
	client, err := newSubscriptionMediaHTTPClient(proxyValue)
	if err != nil {
		return profile
	}

	// Artwork is optional visual enrichment. Bound the work so a slow provider
	// cannot delay a selected actor's basic profile indefinitely.
	cacheCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	updated := append([]subscriptiontarget.ActressWork(nil), profile.Works...)
	semaphore := make(chan struct{}, actressAtlasWorkCoverParallelism)
	var workers sync.WaitGroup
	for index := 0; index < limit; index++ {
		remoteCoverURL := actressAtlasRemoteCoverURL(updated[index])
		if remoteCoverURL == "" {
			continue
		}
		workers.Add(1)
		go func(workIndex int) {
			defer workers.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-cacheCtx.Done():
				return
			}
			localURL, cacheErr := cacheNamedSubscriptionMedia(
				cacheCtx,
				client,
				actressAtlasRemoteCoverURL(updated[workIndex]),
				updated[workIndex].URL,
				mediaDir,
				actressAtlasWorkCoverStem(updated[workIndex], workIndex),
			)
			if cacheErr == nil {
				// Keep the public source alongside the same-origin preview URL. The
				// renderer can display the old local file immediately, then ask this
				// bounded cache route to refresh it in the background on the next
				// actor visit instead of treating a cache hit as permanent data.
				updated[workIndex].SourceCoverURL = actressAtlasRemoteCoverURL(updated[workIndex])
				updated[workIndex].CoverURL = localURL
			}
		}(index)
	}
	workers.Wait()
	profile.Works = updated
	return profile
}

// cacheActressAtlasProfileMedia stores images already parsed from verified
// actress profile pages behind the same local media route used by AV
// subscriptions. The metadata engine remains a supplementary source: a
// provider timeout must not replace a usable profile portrait with a broken
// direct hotlink.
func (a *API) cacheActressAtlasProfileMedia(ctx context.Context, profile subscriptiontarget.TargetProfile, proxyValue string) subscriptiontarget.TargetProfile {
	if a == nil || a.runtime.paths.UserData == "" || strings.TrimSpace(profile.ResolvedActressName) == "" {
		return profile
	}
	mediaDir := filepath.Join(a.runtime.paths.UserData, "subscriptions-v2", "media", actressAtlasMediaKey(profile.ResolvedActressName))
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		return profile
	}
	client, err := newSubscriptionMediaHTTPClient(proxyValue)
	if err != nil {
		return profile
	}
	cacheCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	if source := strings.TrimSpace(profile.AvatarURL); source != "" {
		if localURL, cacheErr := cacheNamedSubscriptionMedia(cacheCtx, client, source, profile.ResolvedBase, mediaDir, "profile-avatar"); cacheErr == nil {
			profile.AvatarURL = localURL
		}
	}
	photos := make([]string, 0, len(profile.PromotionImageURLs))
	for index, source := range profile.PromotionImageURLs {
		if index >= 8 || strings.TrimSpace(source) == "" {
			continue
		}
		localURL, cacheErr := cacheNamedSubscriptionMedia(cacheCtx, client, source, profile.ResolvedBase, mediaDir, fmt.Sprintf("profile-photo-%02d", len(photos)+1))
		if cacheErr == nil {
			photos = append(photos, localURL)
		}
	}
	if len(photos) > 0 {
		profile.PromotionImageURLs = distinctAtlasProfileMedia(a.runtime.paths.UserData, profile.AvatarURL, photos)
	}
	return profile
}

// distinctAtlasProfileMedia removes both identical local routes and source
// images that have identical downloaded bytes. Providers often publish the
// same publicity image under several size/query URLs.
func distinctAtlasProfileMedia(userDataDir string, avatarURL string, photos []string) []string {
	avatarKey := strings.TrimSpace(avatarURL)
	seenURL := map[string]struct{}{}
	seenDigest := map[string]struct{}{}
	result := make([]string, 0, len(photos))
	for _, value := range photos {
		key := strings.TrimSpace(value)
		if key == "" || key == avatarKey {
			continue
		}
		if _, exists := seenURL[key]; exists {
			continue
		}
		if digest := actressAtlasCachedMediaDigest(userDataDir, key); digest != "" {
			if _, exists := seenDigest[digest]; exists {
				continue
			}
			seenDigest[digest] = struct{}{}
		}
		seenURL[key] = struct{}{}
		result = append(result, key)
	}
	return result
}

func actressAtlasCachedMediaDigest(userDataDir string, localURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(localURL))
	if err != nil || parsed.Scheme != "" || !strings.HasPrefix(parsed.Path, subscriptionMediaURLPrefix) {
		return ""
	}
	relativePath := strings.TrimPrefix(parsed.Path, subscriptionMediaURLPrefix)
	contents, err := os.ReadFile(filepath.Join(userDataDir, "subscriptions-v2", "media", filepath.FromSlash(relativePath)))
	if err != nil || len(contents) == 0 {
		return ""
	}
	sum := sha1.Sum(contents)
	return fmt.Sprintf("%x", sum[:])
}

func actressAtlasRemoteCoverURL(work subscriptiontarget.ActressWork) string {
	if source := strings.TrimSpace(work.SourceCoverURL); source != "" {
		return source
	}
	return strings.TrimSpace(work.CoverURL)
}

func isAllowedActressAtlasWork(work subscriptiontarget.ActressWork) bool {
	workURL, err := url.Parse(strings.TrimSpace(work.URL))
	if err != nil || workURL.Scheme != "https" || !actresslookup.IsAllowedActressLookupHost(workURL.Hostname()) {
		return false
	}
	coverURL, err := url.Parse(actressAtlasRemoteCoverURL(work))
	if err != nil || coverURL.Scheme != "https" || !isAllowedActressAtlasCoverHost(coverURL.Hostname()) {
		return false
	}
	return true
}

func isAllowedActressAtlasCoverHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return actresslookup.IsAllowedActressLookupHost(host) || host == "pics.dmm.co.jp" || strings.HasSuffix(host, ".dmm.co.jp")
}

// A hash-based stem lets every work page coexist in the actor cache. The old
// sequential cover-01 name caused page two to overwrite page one's image.
func actressAtlasWorkCoverStem(work subscriptiontarget.ActressWork, index int) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(work.URL) + "\n" + actressAtlasRemoteCoverURL(work)))
	return fmt.Sprintf("cover-%02d-%x", index+1, sum[:5])
}
