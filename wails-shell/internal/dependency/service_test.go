package dependency

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	runtimepaths "javflow/internal/runtime"
)

func TestValidateDependencyDownloadURLRejectsPrivateHost(t *testing.T) {
	if err := validateDependencyDownloadURL("http://127.0.0.1/archive.zip"); err == nil {
		t.Fatal("expected private download URL to be rejected")
	}
}

func TestDependencyClientRejectsPrivateRedirect(t *testing.T) {
	client := NewService(runtimepaths.Paths{}, nil).client
	request := &http.Request{URL: &url.URL{Scheme: "http", Host: "127.0.0.1", Path: "/archive.zip"}}
	if err := client.CheckRedirect(request, []*http.Request{{URL: &url.URL{Scheme: "https", Host: "example.com"}}}); err == nil {
		t.Fatal("expected redirect to a private address to be rejected")
	}
}

func TestDependencyClientBlocksPrivateConnection(t *testing.T) {
	client := NewService(runtimepaths.Paths{}, nil).client
	_, err := client.Get("http://127.0.0.1:1/archive.zip")
	if err == nil || !strings.Contains(err.Error(), "private network") {
		t.Fatalf("expected private connection to be blocked, got %v", err)
	}
}
