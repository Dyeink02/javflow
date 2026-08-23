package bridge

// Ownership summary:
// 1) cache actor-atlas work covers behind the local subscription-media route
// 2) preserve direct public URLs whenever optional visual enrichment fails
// 3) isolate WebView2 hotlink handling from crawl, NFO and subscription state
//
// File map for maintainers:
// 1) bounded Actor Atlas work-cover cache
// 2) local media route conversion for the selected actor detail page

// This file owns the Actor Atlas-only conversion from remote JAV work covers
// to app-served media. It intentionally does not persist crawler, NFO, or
// subscription records; the cache is a display aid for the current detail
// page only.

import (
	"context"
	"crypto/sha1"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"javflow/internal/actresslookup"
	"javflow/internal/contracts/subscriptiontarget"
)

const (
	// One visible page (8) plus the next three pages (24) are accepted in one
	// bridge call. The worker itself still caps network concurrency at three.
	actressAtlasWorkCoverLimit       = 24
	actressAtlasWorkCoverParallelism = 3
)

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
