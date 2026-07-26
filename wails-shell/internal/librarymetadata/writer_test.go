package librarymetadata

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteMetadata_NFOOnly(t *testing.T) {
	tmpDir := t.TempDir()
	item := LibraryMediaItem{
		Code:         "BBAN-452",
		NfoPath:      filepath.Join(tmpDir, "BBAN-452.nfo"),
		PosterPath:   filepath.Join(tmpDir, "BBAN-452-poster.jpg"),
		BackdropPath: filepath.Join(tmpDir, "BBAN-452-backdrop.jpg"),
	}
	info := &MovieInfo{
		Number:    "BBAN-452",
		Title:     "Written Title",
		Plot:      "Plot",
		CoverURL:  "", // skip download
		BackdropURL: "",
	}

	result := WriteMetadata(WriteOptions{Item: item, Info: info})
	if result.Error != "" {
		t.Fatalf("WriteMetadata error: %s", result.Error)
	}
	if result.NfoPath != item.NfoPath {
		t.Errorf("expected NfoPath %s, got %s", item.NfoPath, result.NfoPath)
	}
	if _, err := os.Stat(item.NfoPath); err != nil {
		t.Errorf("NFO file not written: %v", err)
	}
	if result.PosterPath != "" || result.BackdropPath != "" || result.LandscapePath != "" {
		t.Error("expected no image paths when URLs are empty")
	}
}

func TestWriteMetadata_SkipImages_BBAN452(t *testing.T) {
	tmpDir := t.TempDir()
	item := LibraryMediaItem{
		Code:         "BBAN-452",
		NfoPath:      filepath.Join(tmpDir, "BBAN-452.nfo"),
		PosterPath:   filepath.Join(tmpDir, "BBAN-452-poster.jpg"),
		BackdropPath: filepath.Join(tmpDir, "BBAN-452-backdrop.jpg"),
	}
	info := &MovieInfo{
		Number:      "BBAN-452",
		Title:       "Local BBAN-452",
		CoverURL:    "https://example.com/cover.jpg",
		BackdropURL: "https://example.com/backdrop.jpg",
	}

	result := WriteMetadata(WriteOptions{Item: item, Info: info, SkipImages: true})
	if result.Error != "" {
		t.Fatalf("WriteMetadata error: %s", result.Error)
	}
	if result.NfoPath == "" {
		t.Error("expected NFO path")
	}
	if result.PosterPath != "" || result.BackdropPath != "" {
		t.Error("expected images skipped")
	}
}

func makeTestJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("failed to encode test jpeg: %v", err)
	}
	return buf.Bytes()
}

func TestWriteMetadata_BackdropUsesImageFetcher(t *testing.T) {
	tmpDir := t.TempDir()
	item := LibraryMediaItem{
		Code:         "BBAN-452",
		NfoPath:      filepath.Join(tmpDir, "BBAN-452.nfo"),
		PosterPath:   filepath.Join(tmpDir, "BBAN-452-poster.jpg"),
		BackdropPath: filepath.Join(tmpDir, "BBAN-452-backdrop.jpg"),
	}
	jpegBytes := makeTestJPEG(t)
	fetcher := func(imageURL, providerName string) ([]byte, error) {
		if imageURL == "https://example.com/backdrop.jpg" && providerName == "JavBus" {
			return jpegBytes, nil
		}
		return nil, fmt.Errorf("unexpected fetch: %s %s", imageURL, providerName)
	}
	info := &MovieInfo{
		Number:      "BBAN-452",
		Title:       "Fetched Backdrop",
		Provider:    "JavBus",
		CoverURL:    "",
		BackdropURL: "https://example.com/backdrop.jpg",
	}

	result := WriteMetadata(WriteOptions{Item: item, Info: info, ImageFetcher: fetcher})
	if result.Error != "" {
		t.Fatalf("WriteMetadata error: %s", result.Error)
	}
	if result.BackdropPath != item.BackdropPath {
		t.Errorf("expected backdrop path %s, got %s", item.BackdropPath, result.BackdropPath)
	}
	if _, err := os.Stat(item.BackdropPath); err != nil {
		t.Errorf("backdrop file not written: %v", err)
	}
}

func TestWriteMetadata_BackdropFallsBackToHTTP(t *testing.T) {
	tmpDir := t.TempDir()
	item := LibraryMediaItem{
		Code:         "BBAN-452",
		NfoPath:      filepath.Join(tmpDir, "BBAN-452.nfo"),
		PosterPath:   filepath.Join(tmpDir, "BBAN-452-poster.jpg"),
		BackdropPath: filepath.Join(tmpDir, "BBAN-452-backdrop.jpg"),
	}
	jpegBytes := makeTestJPEG(t)
	fetcher := func(imageURL, providerName string) ([]byte, error) {
		return nil, fmt.Errorf("provider fetch failed")
	}
	info := &MovieInfo{
		Number:      "BBAN-452",
		Title:       "Fallback Backdrop",
		Provider:    "JavBus",
		CoverURL:    "",
		BackdropURL: "https://example.com/backdrop.jpg",
	}

	// Because the fetcher fails, the writer falls back to HTTP. The HTTP path
	// requires a real server, so this test verifies the fallback branch by
	// ensuring no panic and that the error is reported when both paths fail.
	result := WriteMetadata(WriteOptions{Item: item, Info: info, ImageFetcher: fetcher})
	if result.Error == "" {
		t.Fatal("expected error when both fetcher and HTTP fail")
	}
	if result.BackdropPath != "" {
		t.Error("expected no backdrop path when download fails")
	}
	_ = jpegBytes
}

func TestValidateImageBytes(t *testing.T) {
	if err := validateImageBytes(nil); err == nil {
		t.Error("expected error for nil data")
	}
	if err := validateImageBytes([]byte("not an image")); err == nil {
		t.Error("expected error for invalid image data")
	}
	jpegBytes := makeTestJPEG(t)
	if err := validateImageBytes(jpegBytes); err != nil {
		t.Errorf("expected valid jpeg, got error: %v", err)
	}
}

func TestWriteImageData(t *testing.T) {
	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "test.jpg")
	jpegBytes := makeTestJPEG(t)
	if err := writeImageData(destPath, jpegBytes); err != nil {
		t.Fatalf("writeImageData error: %v", err)
	}
	if _, err := os.Stat(destPath); err != nil {
		t.Errorf("expected file to exist: %v", err)
	}
}

func TestDownloadImageWithEngineFallback_ContextCancel(t *testing.T) {
	fetcher := func(string, string) ([]byte, error) {
		return makeTestJPEG(&testing.T{}), nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "cancel.jpg")
	err := downloadImageWithEngineFallback(ctx, fetcher, []string{"https://example.com/a.jpg"}, destPath, "JavBus")
	if err == nil {
		t.Error("expected context canceled error")
	}
}
