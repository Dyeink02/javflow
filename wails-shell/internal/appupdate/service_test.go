package appupdate

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckAndDownloadReleaseWithAssetDigest(t *testing.T) {
	payload := buildPortableZip(t, map[string][]byte{
		"javflow.exe": []byte("MZ-javflow-test-executable"),
	})
	digest := sha256Hex(payload)
	server := newReleaseServer(t, releaseFixture{
		tag: "v0.4.40",
		assets: []githubAsset{{
			Name:               "JavFlow-Portable-0.4.40.zip",
			BrowserDownloadURL: "/download/JavFlow-Portable-0.4.40.zip",
			Digest:             "sha256:" + digest,
			Size:               int64(len(payload)),
		}},
		downloadPath: "/download/JavFlow-Portable-0.4.40.zip",
		downloadBody: payload,
	})
	defer server.Close()

	targetDirectory := t.TempDir()
	targetPath := filepath.Join(targetDirectory, "javflow.exe")
	if err := os.WriteFile(targetPath, []byte("MZ-old"), 0o700); err != nil {
		t.Fatal(err)
	}
	service := NewService(ServiceOptions{
		CurrentVersion: "0.4.32",
		APIBaseURL:     server.URL,
		ExecutablePath: targetPath,
		Client:         server.Client(),
	})

	info, err := service.Check(nil)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if !info.UpdateAvailable || info.LatestVersion != "0.4.40" {
		t.Fatalf("unexpected check result: %#v", info)
	}

	downloaded, err := service.Download(nil, "0.4.40")
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	if !downloaded.DownloadReady || downloaded.SHA256 != digest {
		t.Fatalf("unexpected download result: %#v", downloaded)
	}
	if _, err := os.Stat(downloaded.DownloadedPath); err != nil {
		t.Fatalf("downloaded file missing: %v", err)
	}
	if err := os.RemoveAll(downloaded.DownloadedPath); err != nil {
		t.Fatalf("cleanup downloaded file: %v", err)
	}
}

func TestDownloadReadsSha256SidecarWhenAssetDigestMissing(t *testing.T) {
	payload := buildPortableZip(t, map[string][]byte{
		"javflow.exe": []byte("MZ-sidecar-test-executable"),
	})
	digest := sha256Hex(payload)
	server := newReleaseServer(t, releaseFixture{
		tag: "v0.4.40",
		assets: []githubAsset{
			{
				Name:               "JavFlow-Portable-0.4.40.zip",
				BrowserDownloadURL: "/download/JavFlow-Portable-0.4.40.zip",
				Size:               int64(len(payload)),
			},
			{
				Name:               "JavFlow-Portable-0.4.40.zip.sha256",
				BrowserDownloadURL: "/download/JavFlow-Portable-0.4.40.zip.sha256",
			},
		},
		downloadPath: "/download/JavFlow-Portable-0.4.40.zip",
		downloadBody: payload,
		checksumPath: "/download/JavFlow-Portable-0.4.40.zip.sha256",
		checksumBody: []byte(digest + "  *JavFlow-Portable-0.4.40.zip\n"),
	})
	defer server.Close()

	targetPath := filepath.Join(t.TempDir(), "javflow.exe")
	if err := os.WriteFile(targetPath, []byte("MZ-old"), 0o700); err != nil {
		t.Fatal(err)
	}
	service := NewService(ServiceOptions{
		CurrentVersion: "0.4.32",
		APIBaseURL:     server.URL,
		ExecutablePath: targetPath,
		Client:         server.Client(),
	})

	result, err := service.Download(nil, "0.4.4")
	if err != nil {
		t.Fatalf("Download with shorthand version failed: %v", err)
	}
	if result.SHA256 != digest {
		t.Fatalf("sidecar digest = %q, want %q", result.SHA256, digest)
	}
	_ = os.RemoveAll(result.DownloadedPath)
}

func TestDownloadRejectsChecksumMismatch(t *testing.T) {
	payload := buildPortableZip(t, map[string][]byte{
		"javflow.exe": []byte("MZ-mismatch-test-executable"),
	})
	server := newReleaseServer(t, releaseFixture{
		tag: "v0.4.40",
		assets: []githubAsset{{
			Name:               "JavFlow-Portable-0.4.40.zip",
			BrowserDownloadURL: "/download/JavFlow-Portable-0.4.40.zip",
			Digest:             strings.Repeat("0", sha256.Size*2),
			Size:               int64(len(payload)),
		}},
		downloadPath: "/download/JavFlow-Portable-0.4.40.zip",
		downloadBody: payload,
	})
	defer server.Close()

	targetPath := filepath.Join(t.TempDir(), "javflow.exe")
	if err := os.WriteFile(targetPath, []byte("MZ-old"), 0o700); err != nil {
		t.Fatal(err)
	}
	service := NewService(ServiceOptions{
		CurrentVersion: "0.4.32",
		APIBaseURL:     server.URL,
		ExecutablePath: targetPath,
		Client:         server.Client(),
	})
	if _, err := service.Download(nil, "0.4.40"); err == nil {
		t.Fatal("Download unexpectedly accepted a checksum mismatch")
	}
}

func TestDownloadDoesNotUseChecksumForUnrelatedZipAsset(t *testing.T) {
	payload := buildPortableZip(t, map[string][]byte{
		"javflow.exe": []byte("MZ-executable-without-a-matching-checksum"),
	})
	server := newReleaseServer(t, releaseFixture{
		tag: "v0.4.40",
		assets: []githubAsset{
			{
				Name:               "JavFlow-Portable-0.4.40.zip",
				BrowserDownloadURL: "/download/JavFlow-Portable-0.4.40.zip",
				Size:               int64(len(payload)),
			},
			{
				Name:               "JavFlow-Portable-0.4.40.zip.sha256",
				BrowserDownloadURL: "/download/JavFlow-Portable-0.4.40.zip.sha256",
			},
		},
		downloadPath: "/download/JavFlow-Portable-0.4.40.zip",
		downloadBody: payload,
		checksumPath: "/download/JavFlow-Portable-0.4.40.zip.sha256",
		checksumBody: []byte(sha256Hex([]byte("zip-content")) + "  JavFlow-Portable-0.4.40.zip\n"),
	})
	defer server.Close()

	targetPath := filepath.Join(t.TempDir(), "javflow.exe")
	if err := os.WriteFile(targetPath, []byte("MZ-old"), 0o700); err != nil {
		t.Fatal(err)
	}
	service := NewService(ServiceOptions{
		CurrentVersion: "0.4.32",
		APIBaseURL:     server.URL,
		ExecutablePath: targetPath,
		Client:         server.Client(),
	})
	if _, err := service.Download(nil, "0.4.40"); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("Download unexpectedly accepted unrelated checksum: %v", err)
	}
}

func TestReplaceExecutableKeepsBackup(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "javflow.exe")
	stagedDirectory := filepath.Join(directory, "staged")
	backupPath := filepath.Join(directory, "javflow.previous.exe")
	sourcePayload := []byte("MZ-old-executable")
	replacementPayload := []byte("MZ-new-executable")
	if err := os.MkdirAll(filepath.Join(stagedDirectory, "frontend"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, sourcePayload, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagedDirectory, "javflow.exe"), replacementPayload, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagedDirectory, "frontend", "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "user-data.json"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	expectedSHA, err := packageTreeSHA256(stagedDirectory)
	if err != nil {
		t.Fatal(err)
	}

	if err := replacePortablePackage(sourcePath, stagedDirectory, backupPath, expectedSHA); err != nil {
		t.Fatalf("replacePortablePackage failed: %v", err)
	}
	if actual, err := os.ReadFile(sourcePath); err != nil || string(actual) != string(replacementPayload) {
		t.Fatalf("source after replacement = %q, err=%v", actual, err)
	}
	if actual, err := os.ReadFile(backupPath); err != nil || string(actual) != string(sourcePayload) {
		t.Fatalf("backup after replacement = %q, err=%v", actual, err)
	}
	if actual, err := os.ReadFile(filepath.Join(directory, "frontend", "new.txt")); err != nil || string(actual) != "new" {
		t.Fatalf("new package file = %q, err=%v", actual, err)
	}
	if actual, err := os.ReadFile(filepath.Join(directory, "user-data.json")); err != nil || string(actual) != "keep" {
		t.Fatalf("user data after replacement = %q, err=%v", actual, err)
	}
}

func TestExtractPortableArchiveRejectsTraversalAndRequiresRootExecutable(t *testing.T) {
	tests := []struct {
		name  string
		files map[string][]byte
		want  string
	}{
		{
			name:  "path traversal",
			files: map[string][]byte{"../outside.txt": []byte("unsafe")},
			want:  "路径越界",
		},
		{
			name:  "missing executable",
			files: map[string][]byte{"frontend/index.html": []byte("html")},
			want:  "缺少根目录 javflow.exe",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			archivePath := filepath.Join(t.TempDir(), "update.zip")
			if err := os.WriteFile(archivePath, buildPortableZip(t, test.files), 0o600); err != nil {
				t.Fatal(err)
			}
			targetDirectory := filepath.Join(t.TempDir(), "staged")
			if err := os.MkdirAll(targetDirectory, 0o755); err != nil {
				t.Fatal(err)
			}
			err := extractPortableArchive(archivePath, targetDirectory)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("extractPortableArchive error = %v, want text %q", err, test.want)
			}
		})
	}
}

func buildPortableZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	pathValue := filepath.Join(t.TempDir(), "portable.zip")
	file, err := os.Create(pathValue)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, contents := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(pathValue)
	if err != nil {
		t.Fatal(err)
	}
	return contents
}

type releaseFixture struct {
	tag          string
	assets       []githubAsset
	downloadPath string
	downloadBody []byte
	checksumPath string
	checksumBody []byte
}

func newReleaseServer(t *testing.T, fixture releaseFixture) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/repos/Dyeink02/javflow/releases/latest":
			payload, err := json.Marshal(githubRelease{
				TagName: fixture.tag,
				Name:    "JavFlow test release",
				Assets:  fixture.assets,
			})
			if err != nil {
				http.Error(response, err.Error(), http.StatusInternalServerError)
				return
			}
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write(payload)
		case fixture.downloadPath:
			_, _ = response.Write(fixture.downloadBody)
		case fixture.checksumPath:
			_, _ = response.Write(fixture.checksumBody)
		default:
			http.NotFound(response, request)
		}
	}))
}

func sha256Hex(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}
