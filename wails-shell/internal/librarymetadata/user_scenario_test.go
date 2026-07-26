package librarymetadata

import (
	"os"
	"testing"
)

// TestUserScenario_MatsumotoIchika reproduces the user's media library scan
// against their actual directories. It is meant to diagnose why the film list
// is empty and why crawl artifacts are not loaded.
func TestUserScenario_MatsumotoIchika(t *testing.T) {
	if os.Getenv("RUN_USER_SCENARIO_TESTS") != "1" {
		t.Skip("user filesystem scenario disabled; set RUN_USER_SCENARIO_TESTS=1 to enable")
	}
	root := `Y:\docker\emby-nginx\strm\av\限制级\女优\女优影视库\松本一香（松本いちか）`
	crawlOutputDir := `C:\Users\<username>\Desktop\磁力链接\新松本いちか`

	if _, err := os.Stat(root); err != nil {
		t.Skipf("media library root not accessible: %v", err)
	}
	if _, err := os.Stat(crawlOutputDir); err != nil {
		t.Skipf("crawl output dir not accessible: %v", err)
	}

	crawlSource, err := BuildCrawlSource(crawlOutputDir, "")
	if err != nil {
		t.Fatalf("BuildCrawlSource error: %v", err)
	}
	t.Logf("BuildCrawlSource loaded %d records", len(crawlSource.records))

	result := ScanLibrary(ScanOptions{
		Root:           root,
		Extensions:     []string{"strm", "mp4", "mkv", "avi", "mov", "flv", "wmv", "ts", "m4v", "iso"},
		OutputMode:     "inplace",
		CrawlOutputDir: crawlOutputDir,
	})

	if result.Error != "" {
		t.Fatalf("ScanLibrary error: %s", result.Error)
	}

	t.Logf("ScanLibrary found %d local items, %d missing items", len(result.Items), len(result.MissingItems))
	if len(result.Items) == 0 {
		t.Fatal("expected local items but got none")
	}
}
