package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	runtimepaths "javflow/internal/runtime"
)

func TestSubscriptionMediaHandlerServesOnlyManagedMedia(t *testing.T) {
	userDataDir := t.TempDir()
	mediaPath := filepath.Join(userDataDir, "subscriptions-v2", "media", "actor", "photo-01.jpg")
	if err := os.MkdirAll(filepath.Dir(mediaPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mediaPath, []byte("image-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := &App{paths: runtimepaths.Paths{UserData: userDataDir}}
	request := httptest.NewRequest(http.MethodGet, "/subscription-media/actor/photo-01.jpg", nil)
	response := httptest.NewRecorder()
	app.subscriptionMediaHandler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "image-bytes" {
		t.Fatalf("unexpected media response: status=%d body=%q", response.Code, response.Body.String())
	}

	missingRequest := httptest.NewRequest(http.MethodGet, "/not-media/desktop-settings.json", nil)
	missingResponse := httptest.NewRecorder()
	app.subscriptionMediaHandler().ServeHTTP(missingResponse, missingRequest)
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("unexpected non-media response status: %d", missingResponse.Code)
	}
}
