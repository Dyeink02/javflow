package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSubscriptionMediaHandlerServesOnlyActorMedia(t *testing.T) {
	root := t.TempDir()
	media := filepath.Join(root, "subscriptions-v2", "media", "actor")
	if err := os.MkdirAll(media, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(media, "avatar.jpg"), []byte("avatar"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &App{}
	app.paths.UserData = root
	request := httptest.NewRequest(http.MethodGet, "/subscription-media/actor/avatar.jpg", nil)
	response := httptest.NewRecorder()
	app.subscriptionMediaHandler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "avatar" {
		t.Fatalf("unexpected response: status=%d body=%q", response.Code, response.Body.String())
	}
}
