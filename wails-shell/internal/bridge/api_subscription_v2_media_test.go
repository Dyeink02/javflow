package bridge

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
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
