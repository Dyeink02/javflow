package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"javflow/internal/actresslookup"
	"javflow/internal/avsubscriptionv2"
	"javflow/internal/contracts/crawlartifact"
	"javflow/internal/librarymetadata"
	"javflow/internal/organizer"
	runtimepaths "javflow/internal/runtime"
)

// TestLiveChainSmoke_HorikitaMomoa covers the production handoff contracts
// without contacting an external site or touching a user's media/subscriptions.
// The local HTTP server stands in for the actress directory, then the real
// organizer, NFO writer, and subscription importer work in isolated temp paths.
func TestLiveChainSmoke_HorikitaMomoa(t *testing.T) {
	const actressName = "堀北桃愛"
	const firstCode = "TEST-001"
	const secondCode = "TEST-002"

	lookupServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/searchstar/堀北桃愛":
			_, _ = writer.Write([]byte(`<html><body><a class="avatar-box" href="/star/horikita-momoa"><img title="堀北桃愛" /></a></body></html>`))
		case "/star/horikita-momoa":
			_, _ = writer.Write([]byte(chainStarPageHTML(actressName, 2, 2)))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer lookupServer.Close()

	profile, err := actresslookup.NewService().ResolveTarget(actresslookup.ResolveOptions{
		ActressName:   actressName,
		PreferredBase: lookupServer.URL,
		FallbackBases: []string{lookupServer.URL},
	})
	if err != nil {
		t.Fatalf("actor atlas lookup: %v", err)
	}
	if profile.ResolvedActressName != actressName || profile.ResolvedBase != lookupServer.URL+"/star/horikita-momoa" {
		t.Fatalf("unexpected actor atlas profile: %+v", profile)
	}

	root := t.TempDir()
	userDataDir := filepath.Join(root, "app-data")
	crawlOutputDir := filepath.Join(root, "crawl-output")
	mediaDir := filepath.Join(root, "media")
	for _, dir := range []string{userDataDir, crawlOutputDir, mediaDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create temp directory %s: %v", dir, err)
		}
	}

	filmData := []map[string]any{
		{"title": firstCode + " 测试作品一", "actress": []string{actressName}},
		{"title": secondCode + " 测试作品二", "actress": []string{actressName}},
	}
	chainWriteJSON(t, filepath.Join(crawlOutputDir, crawlartifact.CrawlFilmDataFile), filmData)
	chainWriteJSON(t, filepath.Join(crawlOutputDir, crawlartifact.CrawlProfileFile), map[string]any{
		"schemaVersion":  1,
		"runId":          "chain-smoke-horikita-momoa",
		"completedAt":    "2026-08-15T00:00:00Z",
		"actressName":    profile.ResolvedActressName,
		"crawlURL":       profile.ResolvedBase,
		"targetCount":    2,
		"completedCount": 2,
		"itemsPerPage":   profile.ItemsPerPage,
		"totalPages":     profile.TotalPages,
		"outputDir":      crawlOutputDir,
		"filmDataPath":   filepath.Join(crawlOutputDir, crawlartifact.CrawlFilmDataFile),
		"siteBase":       lookupServer.URL,
	})

	videoPath := filepath.Join(mediaDir, firstCode+".mp4")
	chainWriteSparseFile(t, videoPath, 1024*1024)
	paths := runtimepaths.Paths{UserData: userDataDir}
	organizeResult, err := organizer.NewService(paths).RunOrganizer(organizer.RunOptions{
		RootPath:              mediaDir,
		MinSizeMB:             1,
		Suffix:                "-A",
		VideoExtensions:       "mp4",
		AdFileAction:          "move-to-delete",
		IncludeSubdirectories: true,
		StrictExpectedCodes:   true,
		CrawlOutputDir:        crawlOutputDir,
		AdDetectionEnabled:    false,
	})
	if err != nil {
		t.Fatalf("video organizer: %v", err)
	}
	if organizeResult.Summary.MovedToWaiting != 1 || organizeResult.ExpectedCodeCount != 2 {
		t.Fatalf("unexpected organizer summary: %+v", organizeResult.Summary)
	}
	waitingVideoPath := filepath.Join(organizeResult.Paths["waitingDir"], firstCode+".mp4")
	if _, err := os.Stat(waitingVideoPath); err != nil {
		t.Fatalf("organized video is missing: %v", err)
	}

	nfoPath := strings.TrimSuffix(waitingVideoPath, filepath.Ext(waitingVideoPath)) + ".nfo"
	metadataResult := librarymetadata.WriteMetadata(librarymetadata.WriteOptions{
		Item: librarymetadata.LibraryMediaItem{
			Code:      firstCode,
			MediaPath: waitingVideoPath,
			NfoPath:   nfoPath,
			HasMedia:  true,
		},
		Info:       &librarymetadata.MovieInfo{Number: firstCode, Title: "测试作品一", Actors: []string{actressName}},
		SkipImages: true,
	})
	if metadataResult.Error != "" {
		t.Fatalf("metadata write: %s", metadataResult.Error)
	}
	nfoBytes, err := os.ReadFile(nfoPath)
	if err != nil {
		t.Fatalf("read generated NFO: %v", err)
	}
	if !strings.Contains(string(nfoBytes), firstCode) || !strings.Contains(string(nfoBytes), actressName) {
		t.Fatalf("generated NFO does not preserve code and actress: %s", nfoBytes)
	}

	importResult, err := avsubscriptionv2.NewService(paths, nil).ImportFromOutput(crawlOutputDir)
	if err != nil {
		t.Fatalf("auto subscribe from crawler output: %v", err)
	}
	if !importResult.Added || importResult.Subscription.ActressName != actressName {
		t.Fatalf("unexpected subscription import: %+v", importResult)
	}
	if strings.Join(importResult.Subscription.BaselineCodes, ",") != firstCode+","+secondCode {
		t.Fatalf("unexpected subscription baseline: %+v", importResult.Subscription.BaselineCodes)
	}
}

func chainStarPageHTML(actressName string, magnetCount int, allCount int) string {
	return `<html><head><title>` + actressName + ` - JAVBus</title></head><body>` +
		`<div class="star-box"><span class="star-name">` + actressName + `</span></div>` +
		`<div>已有磁力 ` + strconv.Itoa(magnetCount) + ` 部</div>` +
		`<div>全部影片 ` + strconv.Itoa(allCount) + ` 部</div>` +
		`<div class="movies"><a class="movie-box" href="/movie/test">测试</a></div></body></html>`
}

func chainWriteJSON(t *testing.T, path string, value any) {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func chainWriteSparseFile(t *testing.T, path string, size int64) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create test video: %v", err)
	}
	if err := file.Truncate(size); err != nil {
		_ = file.Close()
		t.Fatalf("size test video: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close test video: %v", err)
	}
}
