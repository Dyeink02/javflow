package bridge

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"javflow/internal/actressranking"
	runtimepaths "javflow/internal/runtime"
)

func TestEmbeddedAtlasAvatarsExistAndExtract(t *testing.T) {
	entries, err := fs.ReadDir(embeddedAtlasAvatars, embeddedAtlasAvatarsRoot)
	if err != nil {
		t.Fatalf("embedded avatar dir missing: %v", err)
	}
	if len(entries) < 50 {
		t.Fatalf("expected the baseline avatar library to ship, got %d files", len(entries))
	}
	for _, entry := range entries {
		if entry.Name() == "index.json" {
			continue
		}
		if !strings.HasPrefix(entry.Name(), "avatar-") {
			t.Fatalf("unexpected embedded avatar name: %s", entry.Name())
		}
	}

	target := filepath.Join(t.TempDir(), "media", "atlas-ranking")
	ensureEmbeddedAtlasAvatarsExtracted(target)

	count := 0
	err = filepath.WalkDir(target, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	mediaFiles := 0
	for _, entry := range entries {
		if entry.Name() != "index.json" {
			mediaFiles++
		}
	}
	if count != mediaFiles {
		t.Fatalf("extracted %d avatars, want %d", count, mediaFiles)
	}

	// 幂等：重复释放不覆盖已有文件（保留运行时下载的新版本）。
	probe := filepath.Join(target, entries[0].Name())
	if err := os.WriteFile(probe, []byte("runtime-marker"), 0o644); err != nil {
		t.Fatal(err)
	}
	ensureEmbeddedAtlasAvatarsExtracted(target)
	content, err := os.ReadFile(probe)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "runtime-marker" {
		t.Fatalf("existing runtime file was overwritten by embedded baseline")
	}
}

func TestResolveActressRankingAvatarURLsFallsBackToName(t *testing.T) {
	// 该 URL 从未下载过（不在 199 张基线库里），但演员名在名字索引中：
	// 名字回退应命中内嵌基线头像并改写为本地路由。
	mediaRoot := t.TempDir()
	mediaDir := filepath.Join(mediaRoot, "subscriptions-v2", "media", "atlas-ranking")
	ensureEmbeddedAtlasAvatarsExtracted(mediaDir)

	api := &API{runtime: runtimeFacade{paths: runtimepaths.Paths{UserData: mediaRoot}}}
	items := []actressranking.RankingItem{{
		Rank:        1,
		ActressName: "瀬戸環奈",
		ImageURL:    "https://pics.dmm.co.jp/mono/actjpgs/month-2099/unknown-variant.jpg",
	}}
	originalURL := items[0].ImageURL

	resolved := api.resolveActressRankingAvatarURLs(items)
	if !strings.HasPrefix(resolved[0].ImageURL, "data:image/jpeg;base64,") {
		t.Fatalf("expected an inline data URL via name fallback, got %.80q", resolved[0].ImageURL)
	}
	if resolved[0].SourceImageURL != originalURL {
		t.Fatalf("remote source URL not preserved: %q", resolved[0].SourceImageURL)
	}

	// 本地文件确实存在（名字回退命中的文件必须真实落盘）。
	relative := strings.TrimPrefix(resolved[0].ImageURL, "/subscription-media/")
	localPath := filepath.Join(os.TempDir(), "subscription-media-verify", filepath.FromSlash(relative))
	_ = localPath
	// 文件在 mediaDir 内由 ensure 释放过，直接按名字索引核对磁盘文件。
	nameFile := atlasAvatarFileByName("瀬戸環奈")
	if nameFile == "" {
		t.Fatal("name index lost the actress entry")
	}
	if _, err := os.Stat(filepath.Join(mediaDir, nameFile)); err != nil {
		t.Fatalf("name-fallback target file missing on disk: %v", err)
	}
}

func TestResolveActressRankingAvatarURLsUsesNameWhenArchiveHasNoImageURL(t *testing.T) {
	// 2026-01/02 FANZA Video archive rows intentionally contain no remote
	// portrait URL. A known actress must still resolve from the EXE's embedded
	// name index, rather than falling through to the UI's empty-avatar state.
	mediaRoot := t.TempDir()
	api := &API{runtime: runtimeFacade{paths: runtimepaths.Paths{UserData: mediaRoot}}}
	items := []actressranking.RankingItem{{
		Rank:        1,
		ActressName: "美園和花",
		ImageURL:    "",
	}}

	resolved := api.resolveActressRankingAvatarURLs(items)
	if !strings.HasPrefix(resolved[0].ImageURL, "data:image/jpeg;base64,") {
		t.Fatalf("expected blank archive image URL to resolve by actress name, got %.80q", resolved[0].ImageURL)
	}
	if resolved[0].SourceImageURL != "" {
		t.Fatalf("blank archive image URL must remain blank, got source %q", resolved[0].SourceImageURL)
	}
}

func TestResolveActressRankingAvatarURLsCoversJanuaryAndFebruaryNames(t *testing.T) {
	mediaRoot := t.TempDir()
	api := &API{runtime: runtimeFacade{paths: runtimepaths.Paths{UserData: mediaRoot}}}
	items := []actressranking.RankingItem{
		{Rank: 1, ActressName: "夕美しおん"},
		{Rank: 2, ActressName: "AIKA"},
		// The bundled archive uses the current/former-name spelling in this row.
		{Rank: 3, ActressName: "河北彩伽（河北彩花）"},
	}

	resolved := api.resolveActressRankingAvatarURLs(items)
	for _, item := range resolved {
		if !strings.HasPrefix(item.ImageURL, "data:image/") {
			t.Fatalf("expected embedded avatar for %q, got %.80q", item.ActressName, item.ImageURL)
		}
		if item.SourceImageURL != "" {
			t.Fatalf("name-only embedded avatar must not invent a remote source for %q: %q", item.ActressName, item.SourceImageURL)
		}
	}
}

func TestResolveActressRankingAvatarURLsNormalizesFormerName(t *testing.T) {
	mediaRoot := t.TempDir()
	mediaDir := filepath.Join(mediaRoot, "subscriptions-v2", "media", "atlas-ranking")
	ensureEmbeddedAtlasAvatarsExtracted(mediaDir)

	api := &API{runtime: runtimeFacade{paths: runtimepaths.Paths{UserData: mediaRoot}}}
	items := []actressranking.RankingItem{{
		Rank:        1,
		ActressName: "美谷朱音（美谷朱里）",
		ImageURL:    "https://pics.dmm.co.jp/mono/actjpgs/month-2099/unknown-variant.jpg",
	}}
	resolved := api.resolveActressRankingAvatarURLs(items)
	if !strings.HasPrefix(resolved[0].ImageURL, "data:image/jpeg;base64,") {
		t.Fatalf("expected former-name fallback to resolve an embedded avatar, got %.80q", resolved[0].ImageURL)
	}
}

func TestResolveActressRankingAvatarURLsRepairsLocalRoute(t *testing.T) {
	mediaRoot := t.TempDir()
	mediaDir := filepath.Join(mediaRoot, "subscriptions-v2", "media", "atlas-ranking")
	ensureEmbeddedAtlasAvatarsExtracted(mediaDir)

	fileName := atlasAvatarFileByName("瀬戸環奈")
	if fileName == "" {
		t.Fatal("name index missing local-route test actress")
	}
	api := &API{runtime: runtimeFacade{paths: runtimepaths.Paths{UserData: mediaRoot}}}
	items := []actressranking.RankingItem{{
		Rank:        1,
		ActressName: "瀬戸環奈",
		ImageURL:    subscriptionMediaURLPrefix + "atlas-ranking/" + fileName,
	}}

	resolved := api.resolveActressRankingAvatarURLs(items)
	if !strings.HasPrefix(resolved[0].ImageURL, "data:image/jpeg;base64,") {
		t.Fatalf("expected local route to become an inline avatar, got %.80q", resolved[0].ImageURL)
	}
}
