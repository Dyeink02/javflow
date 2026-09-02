package appupdate

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestLiveUpdateFrom032To044 exercises the real release-update chain end to
// end against the published GitHub v0.4.4 release:
//
//	0.4.32 build → Check (detects v0.4.4) → Download (SHA verified) →
//	helper replace (backup + in-place swap) → replaced EXE hash matches release
//
// Opt-in because it contacts GitHub through the user's proxy and spawns the
// real update helper. The launched 0.4.4 process exits immediately in the
// sandbox (no repo root) by design.
func TestLiveUpdateFrom032To044(t *testing.T) {
	if os.Getenv("JAVFLOW_UPDATE_LIVE_TEST") != "1" {
		t.Skip("set JAVFLOW_UPDATE_LIVE_TEST=1 to run the live release update check")
	}

	proxyValue := os.Getenv("JAVFLOW_UPDATE_LIVE_PROXY")
	if proxyValue == "" {
		proxyValue = "http://127.0.0.1:7897"
	}
	proxyURL, err := url.Parse(proxyValue)
	if err != nil {
		t.Fatalf("proxy url: %v", err)
	}
	proxyClient := &http.Client{
		Timeout:   10 * time.Minute,
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
	}

	// 1. Build the current source as a 0.4.32-identified EXE in a sandbox.
	// The sandbox simulates an INSTALLER deployment: an Uninstall.exe marker
	// sits beside the app so in-app updates stay enabled.
	sandbox := t.TempDir()
	oldExe := filepath.Join(sandbox, "javflow.exe")
	if err := os.WriteFile(filepath.Join(sandbox, "Uninstall.exe"), []byte("MZ-uninstall-marker"), 0o700); err != nil {
		t.Fatal(err)
	}
	moduleRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build",
		"-ldflags", "-X javflow/internal/appupdate.BuildVersion=0.4.32",
		"-o", oldExe, ".")
	build.Dir = moduleRoot
	if output, buildErr := build.CombinedOutput(); buildErr != nil {
		t.Fatalf("build 0.4.32 exe failed: %v\n%s", buildErr, output)
	}
	oldExeSHA := mustFileSHA256(t, oldExe)
	t.Logf("built 0.4.32 exe sha256=%s size=%d", oldExeSHA, fileSize(t, oldExe))

	ctx := context.Background()

	// 1b. A portable layout (no Uninstall.exe) must short-circuit with the
	// guidance message and never reach GitHub.
	portableDir := t.TempDir()
	portableExe := filepath.Join(portableDir, "javflow.exe")
	portableService := NewService(ServiceOptions{
		CurrentVersion: "0.4.32",
		ExecutablePath: portableExe,
		Client:         proxyClient,
	})
	portableInfo, err := portableService.Check(ctx)
	if err != nil {
		t.Fatalf("portable check failed: %v", err)
	}
	if portableInfo.InAppUpdateSupported || portableInfo.InstallKind != installKindPortable {
		t.Fatalf("portable layout must refuse in-app updates: %+v", portableInfo)
	}
	t.Logf("portable check: %s", portableInfo.Message)

	// 2. Same-version service must NOT offer an update.
	current := NewService(ServiceOptions{
		CurrentVersion: "0.4.4",
		ExecutablePath: oldExe,
		Client:         proxyClient,
	})
	currentInfo, err := current.Check(ctx)
	if err != nil {
		t.Fatalf("check with current version failed: %v", err)
	}
	if currentInfo.UpdateAvailable {
		t.Fatalf("0.4.4 must be reported as up to date, got %+v", currentInfo)
	}
	t.Logf("same-version check: %s", currentInfo.Message)

	// 3. 0.4.32 service must detect the published v0.4.4 release.
	service := NewService(ServiceOptions{
		CurrentVersion: "0.4.32",
		ExecutablePath: oldExe,
		Client:         proxyClient,
	})
	info, err := service.Check(ctx)
	if err != nil {
		t.Fatalf("update check failed: %v", err)
	}
	if !info.UpdateAvailable {
		t.Fatalf("expected an available update for 0.4.32, got %+v", info)
	}
	if info.LatestVersion != "0.4.4" {
		t.Fatalf("latest version = %q, want 0.4.4", info.LatestVersion)
	}
	if info.AssetName != "javflow.exe" {
		t.Fatalf("asset name = %q, want javflow.exe", info.AssetName)
	}
	t.Logf("check ok: latest=%s asset=%s size=%d sha256=%s", info.LatestVersion, info.AssetName, info.AssetSize, info.SHA256)

	// 4. Download stages the release EXE with checksum verification.
	downloaded, err := service.Download(ctx, "0.4.4")
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	if !downloaded.DownloadReady || downloaded.DownloadedPath == "" {
		t.Fatalf("download not ready: %+v", downloaded)
	}
	stagedExe := filepath.Join(downloaded.DownloadedPath, "javflow.exe")
	stagedExeSHA := mustFileSHA256(t, stagedExe)
	if stagedExeSHA != info.SHA256 {
		t.Fatalf("staged exe sha %s != release digest %s", stagedExeSHA, info.SHA256)
	}
	t.Logf("download staged at %s (sha256 verified)", downloaded.DownloadedPath)

	// 5. Run the real helper path: the 0.4.32 exe replaces itself with the
	// staged 0.4.4 build, keeping a backup of the old binary.
	backupPath := filepath.Join(sandbox, "javflow.previous.exe")
	treeSHA, err := packageTreeSHA256(downloaded.DownloadedPath)
	if err != nil {
		t.Fatal(err)
	}
	helper := exec.Command(oldExe,
		"--javflow-update-helper",
		"--source", oldExe,
		"--staged-dir", downloaded.DownloadedPath,
		"--backup", backupPath,
		"--package-sha256", treeSHA,
	)
	helper.Dir = sandbox
	if output, helperErr := helper.CombinedOutput(); helperErr != nil {
		t.Fatalf("update helper failed: %v\n%s", helperErr, output)
	}

	replacedSHA := mustFileSHA256(t, oldExe)
	if replacedSHA != info.SHA256 {
		t.Fatalf("replaced exe sha %s != release digest %s", replacedSHA, info.SHA256)
	}
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("backup of the previous exe is missing: %v", err)
	}
	if mustFileSHA256(t, backupPath) != oldExeSHA {
		t.Fatalf("backup content does not match the original 0.4.32 exe")
	}
	if _, err := os.Stat(downloaded.DownloadedPath); !os.IsNotExist(err) {
		t.Fatalf("staged directory should be cleaned after a successful update, stat err=%v", err)
	}
	t.Logf("helper replace ok: old=%s backup=%s new=%s", oldExeSHA[:16], oldExeSHA[:16], replacedSHA[:16])
}

func mustFileSHA256(t *testing.T, path string) string {
	t.Helper()
	sum, err := fileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	return sum
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}
