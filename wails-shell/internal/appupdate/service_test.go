package appupdate

import (
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
	payload := []byte("MZ-javflow-test-executable")
	digest := sha256Hex(payload)
	server := newReleaseServer(t, releaseFixture{
		tag: "v0.4.40",
		assets: []githubAsset{{
			Name:               "javflow.exe",
			BrowserDownloadURL: "/download/javflow.exe",
			Digest:             "sha256:" + digest,
			Size:               int64(len(payload)),
		}},
		downloadPath: "/download/javflow.exe",
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
	if err := os.Remove(downloaded.DownloadedPath); err != nil {
		t.Fatalf("cleanup downloaded file: %v", err)
	}
}

func TestDownloadReadsSha256SidecarWhenAssetDigestMissing(t *testing.T) {
	payload := []byte("MZ-sidecar-test-executable")
	digest := sha256Hex(payload)
	server := newReleaseServer(t, releaseFixture{
		tag: "v0.4.40",
		assets: []githubAsset{
			{
				Name:               "javflow.exe",
				BrowserDownloadURL: "/download/javflow.exe",
				Size:               int64(len(payload)),
			},
			{
				Name:               "javflow.exe.sha256",
				BrowserDownloadURL: "/download/javflow.exe.sha256",
			},
		},
		downloadPath: "/download/javflow.exe",
		downloadBody: payload,
		checksumPath: "/download/javflow.exe.sha256",
		checksumBody: []byte(digest + "  *javflow.exe\n"),
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
	_ = os.Remove(result.DownloadedPath)
}

func TestDownloadRejectsChecksumMismatch(t *testing.T) {
	payload := []byte("MZ-mismatch-test-executable")
	server := newReleaseServer(t, releaseFixture{
		tag: "v0.4.40",
		assets: []githubAsset{{
			Name:               "javflow.exe",
			BrowserDownloadURL: "/download/javflow.exe",
			Digest:             strings.Repeat("0", sha256.Size*2),
			Size:               int64(len(payload)),
		}},
		downloadPath: "/download/javflow.exe",
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
	payload := []byte("MZ-executable-without-a-matching-checksum")
	server := newReleaseServer(t, releaseFixture{
		tag: "v0.4.40",
		assets: []githubAsset{
			{
				Name:               "javflow.exe",
				BrowserDownloadURL: "/download/javflow.exe",
				Size:               int64(len(payload)),
			},
			{
				Name:               "JavFlow-Portable-0.4.40.zip.sha256",
				BrowserDownloadURL: "/download/JavFlow-Portable-0.4.40.zip.sha256",
			},
		},
		downloadPath: "/download/javflow.exe",
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
	replacementPath := filepath.Join(directory, "javflow-update.exe")
	backupPath := filepath.Join(directory, "javflow.previous.exe")
	sourcePayload := []byte("MZ-old-executable")
	replacementPayload := []byte("MZ-new-executable")
	if err := os.WriteFile(sourcePath, sourcePayload, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replacementPath, replacementPayload, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := replaceExecutable(sourcePath, replacementPath, backupPath, sha256Hex(replacementPayload)); err != nil {
		t.Fatalf("replaceExecutable failed: %v", err)
	}
	if actual, err := os.ReadFile(sourcePath); err != nil || string(actual) != string(replacementPayload) {
		t.Fatalf("source after replacement = %q, err=%v", actual, err)
	}
	if actual, err := os.ReadFile(backupPath); err != nil || string(actual) != string(sourcePayload) {
		t.Fatalf("backup after replacement = %q, err=%v", actual, err)
	}
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
