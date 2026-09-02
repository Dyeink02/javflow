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

func TestLoadDefaultsMagnetContentValidationToDisabledAndPreservesSavedChoice(t *testing.T) {
	tempDir := t.TempDir()
	store := NewStore(runtimepaths.Paths{UserData: tempDir, Documents: tempDir})

	defaults, err := store.Load()
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	if got := defaults["magnetContentValidation"]; got != false {
		t.Fatalf("default magnetContentValidation = %v, want false", got)
	}

	if err := store.Save(map[string]any{"magnetContentValidation": true}); err != nil {
		t.Fatalf("save explicit setting: %v", err)
	}
	preserved, err := store.Load()
	if err != nil {
		t.Fatalf("load saved setting: %v", err)
	}
	if got := preserved["magnetContentValidation"]; got != true {
		t.Fatalf("saved magnetContentValidation = %v, want true", got)
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
