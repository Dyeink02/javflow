package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	runtimepaths "javflow/internal/runtime"
)

func TestAttachBackgroundURLUsesBundledDefault(t *testing.T) {
	store := NewStore(runtimepaths.Paths{UserData: t.TempDir()})

	settings := store.AttachBackgroundURL(map[string]any{"backgroundImage": ""})

	if got := settings["backgroundImageUrl"]; got != defaultBackgroundImageURL {
		t.Fatalf("default background URL = %v, want %q", got, defaultBackgroundImageURL)
	}
}

func TestAttachBackgroundURLUsesCustomImageWhenAvailable(t *testing.T) {
	store := NewStore(runtimepaths.Paths{UserData: t.TempDir()})
	imagePath := filepath.Join(t.TempDir(), "custom.png")
	if err := os.WriteFile(imagePath, []byte("test-image"), 0o600); err != nil {
		t.Fatalf("write custom image: %v", err)
	}

	settings := store.AttachBackgroundURL(map[string]any{"backgroundImage": imagePath})
	backgroundURL, _ := settings["backgroundImageUrl"].(string)

	if !strings.HasPrefix(backgroundURL, "data:image/png;base64,") {
		t.Fatalf("custom background URL = %q, want PNG data URL", backgroundURL)
	}
}

func TestAttachBackgroundURLFallsBackWhenCustomImageIsMissing(t *testing.T) {
	store := NewStore(runtimepaths.Paths{UserData: t.TempDir()})

	settings := store.AttachBackgroundURL(map[string]any{
		"backgroundImage": filepath.Join(t.TempDir(), "missing.jpg"),
	})

	if got := settings["backgroundImageUrl"]; got != defaultBackgroundImageURL {
		t.Fatalf("missing custom background URL = %v, want %q", got, defaultBackgroundImageURL)
	}
}
