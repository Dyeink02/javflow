package crawlrunner

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"javflow/internal/contracts/crawlartifact"
	"javflow/internal/crawlfetch"
	"javflow/internal/crawlrequest"
)

// TestHTTPVRFilterEndToEnd runs the Go-native crawler against a local HTTP
// server so the verification exercises real network parsing and queue logic
// without depending on javbus availability or slow browser fallbacks.
func TestHTTPVRFilterEndToEnd(t *testing.T) {
	codes := []string{"MDVR-393", "MDVR-352", "ABP-001", "HNVR-137", "TPPN-001"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.Trim(r.URL.Path, "/")
		if path == "star/vb3" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			base := "http://" + r.Host
			var links strings.Builder
			for _, code := range codes {
				links.WriteString(fmt.Sprintf(`<a class="movie-box" href="%s/%s"><div><img src="%s/%s/cover.jpg"/></div></a>`, base, code, base, code))
			}
			fmt.Fprintf(w, `<html><body>%s</body></html>`, links.String())
			return
		}

		code := strings.Trim(path, "/")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html>
<head><title>%s title</title></head>
<body>
<h3>%s sample title</h3>
<script>
var gid = %d, uc = %d, img = '/%s/cover.jpg';
</script>
</body></html>`, code, code, hashCode(code), hashCode(code)+1, code)
	}))
	defer server.Close()

	outputDir := t.TempDir()
	cfg := Config{
		BaseURL:                     server.URL + "/star/vb3",
		Base:                        server.URL + "/star/vb3",
		Parallel:                    3,
		Timeout:                     10 * time.Second,
		Limit:                       10,
		TotalPages:                  1,
		ItemsPerPage:                30,
		Delay:                       1,
		RetryCount:                  1,
		RetryDelay:                  1 * time.Second,
		Nomag:                       false,
		Allmag:                      false,
		Nopic:                       true,
		SecondValidation:            false,
		ActressCountFilterThreshold: 0,
		FilmCodeFilterThreshold:     "VR",
		Output:                      outputDir,
	}

	runner, err := NewRunner(cfg, outputDir)
	if err != nil {
		t.Fatalf("create runner: %v", err)
	}

	fetchService, err := crawlfetch.NewService(crawlfetch.ServiceOptions{
		Timeout:    cfg.Timeout,
		RetryCount: cfg.RetryCount,
		RetryDelay: cfg.RetryDelay,
	})
	if err != nil {
		t.Fatalf("create fetch service: %v", err)
	}
	BindFetchService(runner, fetchService)

	// Mock magnet fetch so the test stays fast and deterministic.
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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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
	normalLines := []string{}
	for _, line := range strings.Split(string(magnetBytes), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.Contains(strings.ToLower(trimmed), "vr") {
			vrLines = append(vrLines, trimmed)
		} else {
			normalLines = append(normalLines, trimmed)
		}
	}
	filmDataPath := filepath.Join(outputDir, crawlartifact.CrawlFilmDataFile)
	filmDataBytes, err := os.ReadFile(filmDataPath)
	if err != nil {
		t.Fatalf("read filmData.json: %v", err)
	}
	filmDataText := string(filmDataBytes)

	if len(vrLines) > 0 {
		t.Fatalf("magnet-links.txt contains %d VR lines:\n%s", len(vrLines), strings.Join(vrLines, "\n"))
	}
	if len(normalLines) == 0 {
		t.Logf("filmData.json content:\n%s", filmDataText)
		t.Fatalf("magnet-links.txt should contain at least one normal entry")
	}

	for _, code := range []string{"MDVR-393", "MDVR-352", "HNVR-137"} {
		if !strings.Contains(filmDataText, code) {
			t.Fatalf("filmData.json should keep filtered record for %s", code)
		}
	}
	if !strings.Contains(filmDataText, "filteredByFilmCode") {
		t.Fatalf("filmData.json should contain filteredByFilmCode records")
	}

	filteredCodesPath := filepath.Join(outputDir, crawlartifact.DefaultFilteredCodesTxt)
	filteredBytes, err := os.ReadFile(filteredCodesPath)
	if err != nil {
		t.Fatalf("read filtered-codes.txt: %v", err)
	}
	filteredText := string(filteredBytes)
	for _, code := range []string{"MDVR-393", "MDVR-352", "HNVR-137"} {
		if !strings.Contains(filteredText, code) {
			t.Fatalf("filtered-codes.txt should list %s", code)
		}
	}

	t.Logf("PASS: magnet-links.txt has %d lines, 0 VR; filmData keeps filtered records", len(normalLines))
}

func hashCode(code string) int {
	h := 0
	for _, r := range strings.ToUpper(code) {
		h = h*31 + int(r)
	}
	if h < 0 {
		h = -h
	}
	return h
}
