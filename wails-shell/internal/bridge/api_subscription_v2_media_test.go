package bridge

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"javflow/internal/contracts/subscriptiontarget"
	runtimepaths "javflow/internal/runtime"
)

func TestCacheSubscriptionMediaWritesHiddenLocalImage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "image/png")
		img := image.NewRGBA(image.Rect(0, 0, 2, 2))
		img.Set(0, 0, color.RGBA{R: 80, G: 120, B: 160, A: 255})
		_ = png.Encode(writer, img)
	}))
	defer server.Close()

	mediaDir := filepath.Join(t.TempDir(), "subscriptions-v2", "media", "actor")
	urls, err := cacheSubscriptionMedia(context.Background(), []string{server.URL + "/avatar.png"}, mediaDir, "")
	if err != nil {
		t.Fatal(err)
	}
	userDataDir := filepath.Dir(filepath.Dir(filepath.Dir(mediaDir)))
	if len(urls) != 1 || !localSubscriptionMediaURLExists(urls[0], userDataDir) {
		t.Fatalf("expected one readable application media URL, got %#v", urls)
	}
	if urls[0] != "/subscription-media/actor/photo-01.png" {
		t.Fatalf("unexpected application media URL: %q", urls[0])
	}
	if _, err := os.Stat(filepath.Join(mediaDir, "photo-01.png")); err != nil {
		t.Fatal(err)
	}
}

func TestActressAtlasMediaKeyIsStableAndFilesystemSafe(t *testing.T) {
	first := actressAtlasMediaKey("瀬戸環奈")
	second := actressAtlasMediaKey("瀬戸環奈")
	if first == "" || first != second || len(first) != len("atlas-000000000000") {
		t.Fatalf("unexpected atlas media key: %q %q", first, second)
	}
	if filepath.Base(first) != first {
		t.Fatalf("media key escaped its directory: %q", first)
	}
}

func TestCacheActressAtlasWorkCoversUsesLocalRouteAndWorkReferer(t *testing.T) {
	var receivedReferer string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		receivedReferer = request.Referer()
		writer.Header().Set("Content-Type", "image/png")
		img := image.NewRGBA(image.Rect(0, 0, 2, 2))
		img.Set(0, 0, color.RGBA{R: 220, G: 120, B: 170, A: 255})
		_ = png.Encode(writer, img)
	}))
	defer server.Close()

	userData := t.TempDir()
	api := &API{runtime: runtimeFacade{paths: runtimepaths.Paths{UserData: userData}}}
	profile := subscriptiontarget.TargetProfile{
		ResolvedActressName: "测试演员",
		Works: []subscriptiontarget.ActressWork{{
			Code:     "TEST-001",
			URL:      server.URL + "/TEST-001",
			CoverURL: server.URL + "/cover.png",
		}},
	}

	updated := api.cacheActressAtlasWorkCovers(context.Background(), profile, "")
	if len(updated.Works) != 1 || updated.Works[0].CoverURL == profile.Works[0].CoverURL {
		t.Fatalf("expected cached cover URL, got %#v", updated.Works)
	}
	if updated.Works[0].SourceCoverURL != profile.Works[0].CoverURL {
		t.Fatalf("source cover URL was not retained: %#v", updated.Works[0])
	}
	const prefix = "/subscription-media/atlas-"
	if len(updated.Works[0].CoverURL) <= len(prefix) || updated.Works[0].CoverURL[:len(prefix)] != prefix {
		t.Fatalf("unexpected cached cover route: %q", updated.Works[0].CoverURL)
	}
	if receivedReferer != profile.Works[0].URL {
		t.Fatalf("cover request referer = %q, want %q", receivedReferer, profile.Works[0].URL)
	}
	relativePath := strings.TrimPrefix(updated.Works[0].CoverURL, "/subscription-media/")
	cachePath := filepath.Join(userData, "subscriptions-v2", "media", filepath.FromSlash(relativePath))
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("cached cover file is missing: %v", err)
	}

	// A second visit receives the local route but must still use the retained
	// public source URL for a real background refresh.
	secondProfile := subscriptiontarget.TargetProfile{
		ResolvedActressName: updated.ResolvedActressName,
		Works:               updated.Works,
	}
	second := api.cacheActressAtlasWorkCovers(context.Background(), secondProfile, "")
	if second.Works[0].CoverURL == "" || second.Works[0].SourceCoverURL != profile.Works[0].CoverURL {
		t.Fatalf("second refresh lost local/source cover pair: %#v", second.Works[0])
	}
}

func TestActressAtlasWorkCoverStemKeepsDifferentPagesDistinct(t *testing.T) {
	first := actressAtlasWorkCoverStem(subscriptiontarget.ActressWork{URL: "https://www.javbus.com/TEST-001", CoverURL: "https://www.javbus.com/pics/001.jpg"}, 0)
	second := actressAtlasWorkCoverStem(subscriptiontarget.ActressWork{URL: "https://www.javbus.com/TEST-002", CoverURL: "https://www.javbus.com/pics/002.jpg"}, 0)
	if first == second {
		t.Fatalf("different work pages must not overwrite the same cover cache file: %q", first)
	}
}

func TestActressAtlasWorkCoverProfileAllowsThreePagePrefetch(t *testing.T) {
	works := make([]subscriptiontarget.ActressWork, 0, actressAtlasWorkCoverLimit)
	for index := 0; index < actressAtlasWorkCoverLimit; index++ {
		works = append(works, subscriptiontarget.ActressWork{
			Code:     "TEST-" + fmt.Sprintf("%03d", index+1),
			URL:      "https://www.javbus.com/TEST-" + fmt.Sprintf("%03d", index+1),
			CoverURL: "https://pics.dmm.co.jp/digital/video/test/testpl.jpg",
		})
	}
	profile, err := actressAtlasWorkCoverProfile(map[string]any{
		"actressName": "测试演员",
		"works":       works,
	})
	if err != nil {
		t.Fatalf("three-page prefetch payload should be accepted: %v", err)
	}
	if len(profile.Works) != actressAtlasWorkCoverLimit {
		t.Fatalf("unexpected prefetch work count: %d", len(profile.Works))
	}
}

func TestDistinctAtlasProfileMediaRemovesDuplicateCachedImageBytes(t *testing.T) {
	userData := t.TempDir()
	mediaDir := filepath.Join(userData, "subscriptions-v2", "media", "atlas-test")
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mediaDir, "photo-a.jpg"), []byte("same-image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mediaDir, "photo-b.jpg"), []byte("same-image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mediaDir, "photo-c.jpg"), []byte("different-image"), 0o644); err != nil {
		t.Fatal(err)
	}
	photos := distinctAtlasProfileMedia(userData, "/subscription-media/atlas-test/avatar.jpg", []string{
		"/subscription-media/atlas-test/photo-a.jpg",
		"/subscription-media/atlas-test/photo-b.jpg",
		"/subscription-media/atlas-test/photo-c.jpg",
	})
	if len(photos) != 2 || photos[0] != "/subscription-media/atlas-test/photo-a.jpg" || photos[1] != "/subscription-media/atlas-test/photo-c.jpg" {
		t.Fatalf("duplicate cached photo was not removed: %#v", photos)
	}
}
