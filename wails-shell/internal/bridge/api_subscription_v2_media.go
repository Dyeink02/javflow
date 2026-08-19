// Ownership summary:
//   This file implements domain logic for the javflow backend.
//
// File map for maintainers:
//   1) Domain-specific types and helpers.
//   2) Internal service implementation.
//

package bridge

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"javflow/internal/avsubscriptionv2"
	"javflow/internal/netguard"
	"javflow/internal/proxy"
)

const (
	subscriptionMediaCacheTTL  = 7 * 24 * time.Hour
	subscriptionMediaMaxBytes  = 12 * 1024 * 1024
	subscriptionMediaURLPrefix = "/subscription-media/"
)

// Kept as a package variable so media cache behavior can be tested without
// routing test fixtures through a real public network connection.
var newSubscriptionMediaHTTPClient = subscriptionMediaHTTPClient

func (a *API) reorderSubscriptionsV2Result(payload map[string]any) (string, error) {
	if a.lookup.avSubscriptionsV2 == nil {
		return "", fmt.Errorf("AV subscription V2 service is not initialized")
	}
	items, err := a.lookup.avSubscriptionsV2.Reorder(stringSliceValue(payload["ids"]))
	if err != nil {
		return "", err
	}
	return marshalSubscriptionCollectionV2(items, nil)
}

func (a *API) enrichSubscriptionV2MediaResult(payload map[string]any) (string, error) {
	if a.lookup.avSubscriptionsV2 == nil || a.libraryMetadata.library == nil {
		return "", fmt.Errorf("subscription media service is not initialized")
	}
	id := strings.TrimSpace(nonEmptyString(payload["id"]))
	if id == "" {
		return "", fmt.Errorf("subscription id is required")
	}

	items, err := a.lookup.avSubscriptionsV2.List()
	if err != nil {
		return "", err
	}
	var target avsubscriptionv2.Subscription
	found := false
	for _, item := range items {
		if item.ID == id {
			target = item
			found = true
			break
		}
	}
	if !found {
		return "", fmt.Errorf("subscription not found: %s", id)
	}

	force := boolValue(payload["force"], false)
	if !force && subscriptionMediaIsFresh(target, a.runtime.paths.UserData) {
		return marshalResult(target)
	}

	ctx, cancel := a.requestContext(45 * time.Second)
	defer cancel()
	proxyValue := strings.TrimSpace(nonEmptyString(payload["proxy"]))
	media, resolveErr := a.libraryMetadata.library.ResolveActorMedia(ctx, target.ActressName, proxyValue)
	checkedAt := time.Now().Format(time.RFC3339)
	if resolveErr != nil && len(media.Images) == 0 {
		updated, saveErr := a.lookup.avSubscriptionsV2.SetMedia(target.ID, "", nil, checkedAt)
		if saveErr != nil {
			return "", saveErr
		}
		return marshalResult(updated)
	}

	mediaDir := filepath.Join(a.runtime.paths.UserData, "subscriptions-v2", "media", filepath.Base(target.ID))
	localURLs, cacheErr := cacheSubscriptionMedia(ctx, media.Images, mediaDir, proxyValue)
	if cacheErr != nil && len(localURLs) == 0 {
		updated, saveErr := a.lookup.avSubscriptionsV2.SetMedia(target.ID, "", nil, checkedAt)
		if saveErr != nil {
			return "", saveErr
		}
		return marshalResult(updated)
	}

	avatarURL := ""
	if len(localURLs) > 0 {
		avatarURL = localURLs[0]
	}
	updated, err := a.lookup.avSubscriptionsV2.SetMedia(target.ID, avatarURL, localURLs, checkedAt)
	if err != nil {
		return "", err
	}
	return marshalResult(updated)
}

func subscriptionMediaIsFresh(item avsubscriptionv2.Subscription, userDataDir string) bool {
	checkedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(item.MediaUpdatedAt))
	if err != nil || time.Since(checkedAt) >= subscriptionMediaCacheTTL {
		return false
	}
	if strings.TrimSpace(item.AvatarURL) == "" {
		// A recent negative result is cached too, so startup does not repeatedly
		// query actor providers for names with no available media.
		return true
	}
	return localSubscriptionMediaURLExists(item.AvatarURL, userDataDir)
}

func localSubscriptionMediaURLExists(rawURL string, userDataDir string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	if parsed.Scheme == "" && strings.HasPrefix(parsed.Path, subscriptionMediaURLPrefix) {
		relativePath := strings.TrimPrefix(parsed.Path, subscriptionMediaURLPrefix)
		targetPath := filepath.Join(
			userDataDir,
			"subscriptions-v2",
			"media",
			filepath.FromSlash(relativePath),
		)
		_, err = os.Stat(targetPath)
		return err == nil
	}
	if !strings.EqualFold(parsed.Scheme, "file") {
		return false
	}
	pathValue, err := url.PathUnescape(parsed.Path)
	if err != nil {
		return false
	}
	pathValue = filepath.FromSlash(strings.TrimPrefix(pathValue, "/"))
	_, err = os.Stat(pathValue)
	return err == nil
}

func cacheSubscriptionMedia(ctx context.Context, imageURLs []string, mediaDir string, proxyValue string) ([]string, error) {
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		return nil, err
	}
	client, err := newSubscriptionMediaHTTPClient(proxyValue)
	if err != nil {
		return nil, err
	}

	capacity := len(imageURLs)
	if capacity > 8 {
		capacity = 8
	}
	localURLs := make([]string, 0, capacity)
	var lastErr error
	for index, imageURL := range imageURLs {
		if index >= 8 {
			break
		}
		localURL, err := cacheNamedSubscriptionMedia(ctx, client, imageURL, "", mediaDir, fmt.Sprintf("photo-%02d", len(localURLs)+1))
		if err != nil {
			lastErr = err
			continue
		}
		localURLs = append(localURLs, localURL)
	}
	if len(localURLs) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return localURLs, nil
}

// cacheNamedSubscriptionMedia stores one provider image under a controlled
// application filename. Callers may supply the page URL as Referer when a
// cover provider rejects unreferenced image requests.
func cacheNamedSubscriptionMedia(ctx context.Context, client *http.Client, imageURL, referer, mediaDir, fileStem string) (string, error) {
	contents, extension, err := downloadSubscriptionMedia(ctx, client, imageURL, referer)
	if err != nil {
		return "", err
	}
	filePath := filepath.Join(mediaDir, fileStem+extension)
	if err := os.WriteFile(filePath, contents, 0o644); err != nil {
		return "", err
	}
	return subscriptionMediaAssetURL(mediaDir, filePath), nil
}

func subscriptionMediaHTTPClient(proxyValue string) (*http.Client, error) {
	transport := netguard.NewPublicTransport()
	normalizedProxy := proxy.NormalizeProxyValue(proxyValue)
	if normalizedProxy != "" {
		proxyURL, err := url.Parse(normalizedProxy)
		if err != nil {
			return nil, err
		}
		// A user-selected local proxy is permitted; URL and redirect validation
		// still runs below, while the proxy itself owns the remote connection.
		transport.DialContext = nil
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	client := &http.Client{Timeout: 15 * time.Second, Transport: transport}
	netguard.ApplyRedirectPolicy(client)
	return client, nil
}

func downloadSubscriptionMedia(ctx context.Context, client *http.Client, imageURL, referer string) ([]byte, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(imageURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, "", fmt.Errorf("unsupported actor image URL")
	}
	if err := netguard.ValidatePublicURL(ctx, parsed); err != nil {
		return nil, "", fmt.Errorf("unsafe actor image URL: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, "", err
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/135.0.0.0 Safari/537.36")
	request.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	if parsedReferer, refererErr := url.Parse(strings.TrimSpace(referer)); refererErr == nil && (parsedReferer.Scheme == "http" || parsedReferer.Scheme == "https") && parsedReferer.Host != "" {
		request.Header.Set("Referer", parsedReferer.String())
	}

	clientCopy := *client
	netguard.ApplyRedirectPolicy(&clientCopy)
	response, err := clientCopy.Do(request)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, "", fmt.Errorf("actor image HTTP %d", response.StatusCode)
	}
	contents, err := io.ReadAll(io.LimitReader(response.Body, subscriptionMediaMaxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(contents) == 0 || len(contents) > subscriptionMediaMaxBytes {
		return nil, "", fmt.Errorf("actor image is empty or too large")
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(http.DetectContentType(contents), ";")[0]))
	extensionByType := map[string]string{
		"image/jpeg": ".jpg",
		"image/png":  ".png",
		"image/webp": ".webp",
		"image/gif":  ".gif",
		"image/avif": ".avif",
	}
	extension, ok := extensionByType[contentType]
	if !ok {
		return nil, "", fmt.Errorf("unsupported actor image type: %s", contentType)
	}
	return contents, extension, nil
}

func subscriptionMediaAssetURL(mediaDir string, filePath string) string {
	return (&url.URL{Path: subscriptionMediaURLPrefix + filepath.Base(mediaDir) + "/" + filepath.Base(filePath)}).String()
}
