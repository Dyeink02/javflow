package bridge

import (
	"bytes"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/draw"
)

// TestAnalyzeEmbeddedAvatars is a read-only survey of the embedded avatar
// directory: dimensions, file sizes, and downsizing headroom.
func TestAnalyzeEmbeddedAvatars(t *testing.T) {
	dir := filepath.Join("data", "atlas-avatars")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var totalBytes int64
	small, medium, large := 0, 0, 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jpg" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		totalBytes += int64(len(raw))
		if sniffImageKind(raw) != "jpeg" {
			t.Logf("non-jpeg payload (kept as-is): %s %dKB", entry.Name(), len(raw)/1024)
			continue
		}
		img, err := jpeg.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("%s: decode: %v", entry.Name(), err)
		}
		b := img.Bounds()
		maxDim := b.Dx()
		if b.Dy() > maxDim {
			maxDim = b.Dy()
		}
		switch {
		case maxDim <= 260:
			small++
		case maxDim <= 400:
			medium++
		default:
			large++
		}
		if maxDim > 500 || len(raw) > 60*1024 {
			t.Logf("big: %s %dx%d %dKB", entry.Name(), b.Dx(), b.Dy(), len(raw)/1024)
		}
	}
	t.Logf("total=%d files, %.1fMB; small(<=260)=%d medium(261-400)=%d large(>400)=%d", len(entries)-1, float64(totalBytes)/(1024*1024), small, medium, large)
}

// sniffImageKind reports the actual payload format regardless of extension.
func sniffImageKind(raw []byte) string {
	switch {
	case len(raw) >= 2 && raw[0] == 0xFF && raw[1] == 0xD8:
		return "jpeg"
	case len(raw) >= 4 && raw[0] == 0x89 && raw[1] == 'P' && raw[2] == 'N' && raw[3] == 'G':
		return "png"
	case len(raw) >= 12 && string(raw[0:4]) == "RIFF" && string(raw[8:12]) == "WEBP":
		return "webp"
	default:
		return "unknown"
	}
}

// TestResizeEmbeddedAvatars downscales oversized embedded JPEG avatars into a
// staging directory. The embed directory is untouched; the caller reviews the
// staging output before swapping. Non-JPEG payloads (png/webp/mislabeled)
// pass through unchanged.
func TestResizeEmbeddedAvatars(t *testing.T) {
	if os.Getenv("JAVFLOW_AVATAR_RESIZE") != "1" {
		t.Skip("set JAVFLOW_AVATAR_RESIZE=1 to generate downscaled avatars")
	}
	outDir := os.Getenv("JAVFLOW_AVATAR_OUTPUT")
	if outDir == "" {
		t.Fatal("set JAVFLOW_AVATAR_OUTPUT to a staging directory")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join("data", "atlas-avatars")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	const maxDimension = 240
	const quality = 82
	var originalBytes, resizedBytes int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		source := filepath.Join(dir, entry.Name())
		raw, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		originalBytes += int64(len(raw))
		target := filepath.Join(outDir, entry.Name())

		if filepath.Ext(entry.Name()) != ".jpg" || sniffImageKind(raw) != "jpeg" {
			if err := os.WriteFile(target, raw, 0o644); err != nil {
				t.Fatal(err)
			}
			resizedBytes += int64(len(raw))
			continue
		}

		img, err := jpeg.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("%s: decode failed despite SOI: %v", entry.Name(), err)
		}
		bounds := img.Bounds()
		width, height := bounds.Dx(), bounds.Dy()
		maxDim := width
		if height > maxDim {
			maxDim = height
		}
		var output []byte
		if maxDim <= maxDimension+20 {
			// Already at or near display resolution: keep original bytes so
			// avatars that need no change never suffer a generation loss.
			output = raw
		} else {
			scale := float64(maxDimension) / float64(maxDim)
			newWidth := max(1, int(float64(width)*scale))
			newHeight := max(1, int(float64(height)*scale))
			scaled := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
			draw.CatmullRom.Scale(scaled, scaled.Bounds(), img, bounds, draw.Over, nil)
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, scaled, &jpeg.Options{Quality: quality}); err != nil {
				t.Fatal(err)
			}
			// Verify the produced bytes decode before accepting them.
			if _, err := jpeg.Decode(bytes.NewReader(buf.Bytes())); err != nil {
				t.Fatalf("%s: resized output failed verification: %v", entry.Name(), err)
			}
			output = buf.Bytes()
		}
		if err := os.WriteFile(target, output, 0o644); err != nil {
			t.Fatal(err)
		}
		resizedBytes += int64(len(output))
	}
	t.Logf("original %.2fMB -> resized %.2fMB", float64(originalBytes)/(1024*1024), float64(resizedBytes)/(1024*1024))
}
