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
	"javflow/internal/crawlparse"
	"javflow/internal/crawlrequest"
)

// TestVRCrawlFilterEndToEnd runs the full Go-native crawl pipeline with fixture
// fetchers and verifies that film-code filter entries (VR) do not leak into
// magnet-links.txt. This is deterministic and exercises the same code path the
// Wails EXE uses.
func TestVRCrawlFilterEndToEnd(t *testing.T) {
	outputDir := t.TempDir()
	cfg := Config{
		BaseURL:                     "https://www.javbus.com/star/vb3",
		Base:                        "https://www.javbus.com/star/vb3",
		Parallel:                    2,
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

	runner.SetFetchFuncs(
		func(ctx context.Context, baseURL string, search string, pageNumber int) ([]string, crawlrequest.PageResponse, error) {
			return []string{
				"https://www.javbus.com/MDVR-393",
				"https://www.javbus.com/MDVR-352",
				"https://www.javbus.com/ABP-001",
				"https://www.javbus.com/HNVR-137",
				"https://www.javbus.com/AJVR-137",
			}, crawlrequest.PageResponse{StatusCode: 200}, nil
		},
		func(ctx context.Context, detailURL string) (crawlparse.Metadata, crawlrequest.PageResponse, error) {
			meta := crawlparse.Metadata{
				Title: detailURL,
			}
			// Titles mirror real javbus shape so isFilmCodeFiltered catches VR.
			switch {
			case strings.Contains(detailURL, "MDVR-393"):
				meta.Title = "MDVR-393 【VR】sample title 393"
			case strings.Contains(detailURL, "MDVR-352"):
				meta.Title = "MDVR-352 【VR】sample title 352"
			case strings.Contains(detailURL, "HNVR-137"):
				meta.Title = "HNVR-137 【VR】sample title 137"
			case strings.Contains(detailURL, "AJVR-137"):
				meta.Title = "AJVR-137 【VR】sample title 137"
			case strings.Contains(detailURL, "ABP-001"):
				meta.Title = "ABP-001 normal sample title"
			}
			return meta, crawlrequest.PageResponse{StatusCode: 200}, nil
		},
	)

	runner.fetchMagnetFn = func(ctx context.Context, gid string, uc string, img string, title string) (*crawlrequest.MagnetResult, error) {
		code := "UNKNOWN"
		if strings.Contains(title, "MDVR-393") {
			code = "MDVR-393"
		} else if strings.Contains(title, "MDVR-352") {
			code = "MDVR-352"
		} else if strings.Contains(title, "HNVR-137") {
			code = "HNVR-137"
		} else if strings.Contains(title, "AJVR-137") {
			code = "AJVR-137"
		} else if strings.Contains(title, "ABP-001") {
			code = "ABP-001"
		}
		return &crawlrequest.MagnetResult{
			Magnet: fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", strings.Repeat("0", 40-len(code))+code, code),
			MagnetLinks: []crawlrequest.MagnetLink{
				{Link: fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", strings.Repeat("0", 40-len(code))+code, code), Size: "1GB"},
			},
		}, nil
	}

	runner.createFetchClient = func(proxy string, timeout time.Duration) (*crawlrequest.Client, error) {
		return crawlrequest.NewClient(crawlrequest.PageRequestOptions{Timeout: timeout})
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

	lines := strings.Split(string(magnetBytes), "\n")
	vrLines := make([]string, 0)
	normalLines := make([]string, 0)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "vr") {
			vrLines = append(vrLines, trimmed)
		} else {
			normalLines = append(normalLines, trimmed)
		}
	}

	if len(vrLines) > 0 {
		t.Fatalf("magnet-links.txt contains %d VR entries:\n%s", len(vrLines), strings.Join(vrLines, "\n"))
	}
	if len(normalLines) == 0 {
		t.Fatalf("magnet-links.txt should contain at least one normal entry")
	}

	filmDataPath := filepath.Join(outputDir, crawlartifact.CrawlFilmDataFile)
	filmDataBytes, err := os.ReadFile(filmDataPath)
	if err != nil {
		t.Fatalf("read filmData.json: %v", err)
	}
	filmDataText := string(filmDataBytes)
	if !strings.Contains(filmDataText, "MDVR-393") || !strings.Contains(filmDataText, "filteredByFilmCode") {
		t.Fatalf("filmData.json should keep filtered records for audit")
	}

	filteredCodesPath := filepath.Join(outputDir, crawlartifact.DefaultFilteredCodesTxt)
	filteredBytes, err := os.ReadFile(filteredCodesPath)
	if err != nil {
		t.Fatalf("read filtered-codes.txt: %v", err)
	}
	filteredText := string(filteredBytes)
	if !strings.Contains(filteredText, "MDVR-393") || !strings.Contains(filteredText, "MDVR-352") || !strings.Contains(filteredText, "HNVR-137") || !strings.Contains(filteredText, "AJVR-137") {
		t.Fatalf("filtered-codes.txt should list filtered VR codes: %s", filteredText)
	}

	fmt.Printf("PASS: magnet-links.txt has %d lines, 0 VR; filmData keeps %d filtered records\n", len(normalLines), strings.Count(filmDataText, "filteredByFilmCode"))
}

func TestFilmCodeFilterAcceptsChineseSeparators(t *testing.T) {
	runner, err := NewRunner(Config{FilmCodeFilterThreshold: "VR、OFJE"}, t.TempDir())
	if err != nil {
		t.Fatalf("create runner: %v", err)
	}
	for _, code := range []string{"AJVR-137", "OFJE-999"} {
		if !runner.isFilmCodeLinkFiltered("https://www.javbus.com/" + code) {
			t.Fatalf("expected %s to match Chinese-punctuation filter", code)
		}
	}
	if runner.isFilmCodeLinkFiltered("https://www.javbus.com/ABP-001") {
		t.Fatal("normal code must not match Chinese-punctuation filter")
	}
}
