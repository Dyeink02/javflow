package antiblock

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestExtractAntiBlockURLs(t *testing.T) {
	t.Parallel()

	htmlSource := `
<div class="alert alert-info">
  <div class="col-xs-12 col-md-6 col-lg-3 text-center">
    <strong>防屏蔽地址 1</strong>
    <a href="https://a.example.com">A</a>
  </div>
  <div class="col-xs-12 col-md-6 col-lg-3 text-center">
    <strong>防屏蔽地址 2</strong>
    <a href="https://b.example.com">B</a>
  </div>
  <div class="col-xs-12 col-md-6 col-lg-3 text-center">
    <strong>其他入口</strong>
    <a href="https://ignore.example.com">ignore</a>
  </div>
</div>`

	expected := []string{
		"https://a.example.com",
		"https://b.example.com",
	}

	if actual := extractAntiBlockURLs(htmlSource); !reflect.DeepEqual(actual, expected) {
		t.Fatalf("extractAntiBlockURLs() = %#v, expected %#v", actual, expected)
	}
}

func TestServiceUpdateMergesAndWritesURLs(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, defaultAntiBlockFile)
	if err := os.WriteFile(filePath, []byte(`["https://existing.example.com"]`), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	service := &Service{
		fetchHTML: func(targetURL string, proxyValue string) (string, error) {
			return `
<div class="col-xs-12 col-md-6 col-lg-3 text-center">
  <strong>防屏蔽地址</strong>
  <a href="https://new.example.com">new</a>
</div>`, nil
		},
		userHomeDir: func() (string, error) {
			return tempDir, nil
		},
	}

	result, err := service.Update(UpdateOptions{
		Base:  "https://www.javbus.com",
		Proxy: "127.0.0.1:7890",
	})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	expected := []string{
		"https://existing.example.com",
		"https://new.example.com",
	}
	if !reflect.DeepEqual(result.AntiBlockURLs, expected) {
		t.Fatalf("result.AntiBlockURLs = %#v, expected %#v", result.AntiBlockURLs, expected)
	}
	if result.FilePath != filePath {
		t.Fatalf("result.FilePath = %q, expected %q", result.FilePath, filePath)
	}

	written, err := readExistingURLs(filePath)
	if err != nil {
		t.Fatalf("readExistingURLs returned error: %v", err)
	}
	if !reflect.DeepEqual(written, expected) {
		t.Fatalf("written URLs = %#v, expected %#v", written, expected)
	}
}

func TestServiceUpdateRequiresWorkingHomeDir(t *testing.T) {
	t.Parallel()

	service := &Service{
		fetchHTML: func(targetURL string, proxyValue string) (string, error) {
			return "<html></html>", nil
		},
		userHomeDir: func() (string, error) {
			return "", errors.New("home dir unavailable")
		},
	}

	if _, err := service.Update(UpdateOptions{}); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestLoadURLsReadsPersistedFile(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, defaultAntiBlockFile)
	if err := os.WriteFile(filePath, []byte(`["https://a.example.com", "https://b.example.com", "https://a.example.com"]`), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	urls, err := LoadURLs(tempDir)
	if err != nil {
		t.Fatalf("LoadURLs returned error: %v", err)
	}

	expected := []string{"https://a.example.com", "https://b.example.com"}
	if !reflect.DeepEqual(urls, expected) {
		t.Fatalf("LoadURLs() = %#v, expected %#v", urls, expected)
	}
}

func TestLoadURLsReturnsEmptyForMissingFile(t *testing.T) {
	tempDir := t.TempDir()

	urls, err := LoadURLs(tempDir)
	if err != nil {
		t.Fatalf("LoadURLs returned error: %v", err)
	}
	if len(urls) != 0 {
		t.Fatalf("expected empty URLs, got %#v", urls)
	}
}
