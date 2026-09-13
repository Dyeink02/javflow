package actressranking

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
)

// TestGenerateBundledAnnualSnapshot encodes the verified annual rankings
// (official 2024/2025 + AVfan 2021/2022/2023) into the gzip+base64 payload
// consumed by bundled_annual_history.go. Opt-in: run on a machine whose cache
// holds all five verified years.
func TestGenerateBundledAnnualSnapshot(t *testing.T) {
	if os.Getenv("JAVFLOW_BUNDLE_GENERATE") != "1" {
		t.Skip("set JAVFLOW_BUNDLE_GENERATE=1 to regenerate the bundled annual snapshot")
	}
	cachePath := os.Getenv("JAVFLOW_RANKING_CACHE_PATH")
	if cachePath == "" {
		t.Fatal("set JAVFLOW_RANKING_CACHE_PATH to the app cache file")
	}
	outputPath := os.Getenv("JAVFLOW_BUNDLE_OUTPUT")
	if outputPath == "" {
		t.Fatal("set JAVFLOW_BUNDLE_OUTPUT to the output base64 file")
	}

	cache := loadCache(cachePath)
	snapshot := cacheFile{Version: cache.Version, Sources: map[string]sourceCache{}}

	official := cache.Sources["official"]
	officialAnnual := sourceCache{AnnualByYear: map[string]cacheEntry{}}
	for _, year := range []string{"2024", "2025"} {
		if entry, ok := official.AnnualByYear[year]; ok && len(entry.Data.Items) > 0 {
			officialAnnual.AnnualByYear[year] = entry
		}
	}
	if len(officialAnnual.AnnualByYear) > 0 {
		snapshot.Sources["official"] = officialAnnual
	}

	avfan := cache.Sources["avfan"]
	avfanAnnual := sourceCache{AnnualByYear: map[string]cacheEntry{}}
	for _, year := range []string{"2021", "2022", "2023"} {
		if entry, ok := avfan.AnnualByYear[year]; ok && len(entry.Data.Items) > 0 {
			avfanAnnual.AnnualByYear[year] = entry
		}
	}
	if len(avfanAnnual.AnnualByYear) > 0 {
		snapshot.Sources["avfan"] = avfanAnnual
	}

	if len(snapshot.Sources) != 2 {
		t.Fatalf("expected official+avfan buckets, got %d", len(snapshot.Sources))
	}

	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(compressed.Bytes())
	if err := os.WriteFile(outputPath, []byte(encoded), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("snapshot: %d bytes json -> %d bytes base64", len(payload), len(encoded))
}
