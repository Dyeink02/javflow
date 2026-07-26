package crawlrunner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"javflow/internal/contracts/crawlartifact"
	"javflow/internal/crawlfetch"
	"javflow/internal/crawlrequest"
)

// TestRealNetworkVRFilter runs the Go-native crawler against javbus with the
// real HTTP stack for index/detail pages and the user's "VR" film-code filter.
// Magnet fetching is mocked to keep the verification fast and deterministic;
// the filter decision happens before any detail/magnet work, so this still
// exercises the actual production code path. It requires a local proxy
// (default http://127.0.0.1:7897) and network access, so it is skipped unless
// RUN_NETWORK_TEST is set.
func TestRealNetworkVRFilter(t *testing.T) {
	if os.Getenv("RUN_NETWORK_TEST") == "" {
		t.Skip("set RUN_NETWORK_TEST=1 to run this real-network verification")
	}

	proxy := os.Getenv("TEST_PROXY")
	if proxy == "" {
		proxy = "http://127.0.0.1:7897"
	}

	outputDir := t.TempDir()
	cfg := Config{
		BaseURL:                     "https://www.javbus.com/star/vb3",
		Base:                        "https://www.javbus.com/star/vb3",
		Parallel:                    1,
		Timeout:                     30 * time.Second,
		Limit:                       1,
		TotalPages:                  1,
		ItemsPerPage:                30,
		Delay:                       0,
		RetryCount:                  1,
		RetryDelay:                  2 * time.Second,
		Nomag:                       false,
		Allmag:                      false,
		Nopic:                       true,
		SecondValidation:            false,
		ActressCountFilterThreshold: 0,
		FilmCodeFilterThreshold:     "VR",
		Output:                      outputDir,
		Proxy:                       proxy,
	}

	runner, err := NewRunner(cfg, outputDir)
	if err != nil {
		t.Fatalf("create runner: %v", err)
	}

	fetchService, err := crawlfetch.NewService(crawlfetch.ServiceOptions{
		Proxy:      cfg.Proxy,
		Timeout:    cfg.Timeout,
		RetryCount: cfg.RetryCount,
		RetryDelay: cfg.RetryDelay,
	})
	if err != nil {
		t.Fatalf("create fetch service: %v", err)
	}
	defer fetchService.Close()
	BindFetchService(runner, fetchService)

	// Mock magnet fetch so this verification focuses on the filter path without
	// waiting for the slow browser magnet fallback.
	runner.fetchMagnetFn = func(ctx context.Context, gid string, uc string, img string, title string) (*crawlrequest.MagnetResult, error) {
		code := "UNKNOWN"
		if c := extractFilmIDFromLink(title); c != "" {
			code = c
		}
		magnet := fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", strings.Repeat("0", 40-len(code))+code, code)
		return &crawlrequest.MagnetResult{
			Magnet: magnet,
			MagnetLinks: []crawlrequest.MagnetLink{
				{Link: magnet, Size: "1GB"},
			},
		}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	defer cancel()

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("runner returned error: %v", err)
	}

	magnetPath := filepath.Join(outputDir, crawlartifact.DefaultMagnetTxt)
	magnetBytes, err := os.ReadFile(magnetPath)
	if err != nil {
		t.Fatalf("read magnet-links.txt: %v", err)
	}
	vrLines := []string{}
	for _, line := range strings.Split(string(magnetBytes), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.Contains(strings.ToLower(trimmed), "vr") {
			vrLines = append(vrLines, trimmed)
		}
	}
	if len(vrLines) > 0 {
		t.Fatalf("magnet-links.txt contains %d VR lines:\n%s", len(vrLines), strings.Join(vrLines, "\n"))
	}

	runPaths := crawlartifact.ResolveCrawlRunPaths(outputDir)
	filmDataBytes, err := os.ReadFile(runPaths.FilmDataPath)
	if err != nil {
		t.Fatalf("read filmData.json: %v", err)
	}
	filmDataText := string(filmDataBytes)
	if !strings.Contains(filmDataText, "filteredByFilmCode") {
		t.Fatalf("filmData.json should contain at least one filteredByFilmCode record")
	}

	t.Logf("PASS: magnet-links.txt has 0 VR lines; filmData keeps filtered records")
}



