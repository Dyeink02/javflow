package crawloutput

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"javflow/internal/contracts/crawlartifact"
)

func TestWriterWriteAndFlush(t *testing.T) {
	dir := t.TempDir()

	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("create writer failed: %v", err)
	}

	added, err := w.WriteFilmData(FilmData{
		Title:      "ABC-001 Sample Film",
		SourceLink: "https://example.com/ABC-001",
		Magnet:     "magnet:?xt=urn:btih:AAAA",
	})
	if err != nil {
		t.Fatalf("write film data failed: %v", err)
	}
	if !added {
		t.Fatal("expected new film record to be added")
	}

	added, err = w.WriteFilmData(FilmData{
		Title:      "ABC-001 Sample Film",
		SourceLink: "https://example.com/ABC-001",
		Magnet:     "magnet:?xt=urn:btih:BBBB",
	})
	if err != nil {
		t.Fatalf("write duplicate film data failed: %v", err)
	}
	if added {
		t.Fatal("expected duplicate film record to merge instead of append")
	}

	if err := w.Flush(); err != nil {
		t.Fatalf("flush failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, crawlartifact.CrawlFilmDataFile)); os.IsNotExist(err) {
		t.Fatal("filmData.json should exist after flush")
	}
	if _, err := os.Stat(filepath.Join(dir, crawlartifact.DefaultMagnetTxt)); os.IsNotExist(err) {
		t.Fatal("magnet-links.txt should exist after flush")
	}
	if got := w.RecordCount(); got != 1 {
		t.Fatalf("expected 1 record, got %d", got)
	}
}

func TestWriterEmptyDir(t *testing.T) {
	dir := t.TempDir()

	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("create writer failed: %v", err)
	}
	if got := w.RecordCount(); got != 0 {
		t.Fatalf("expected empty record set, got %d", got)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush on empty writer failed: %v", err)
	}
}

func TestWriterFiltersActressCountFromMagnetTextOnly(t *testing.T) {
	dir := t.TempDir()

	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("create writer failed: %v", err)
	}

	_, err = w.WriteFilmData(FilmData{
		Title:                  "ABC-001 Filtered Film",
		SourceLink:             "https://example.com/ABC-001",
		Magnet:                 "magnet:?xt=urn:btih:AAAA",
		ActressCount:           6,
		FilteredByActressCount: true,
		FilterRemark:           "actress count 6 exceeds threshold 5, skip magnet TXT only",
	})
	if err != nil {
		t.Fatalf("write filtered film failed: %v", err)
	}

	_, err = w.WriteFilmData(FilmData{
		Title:      "DEF-002 Normal Film",
		SourceLink: "https://example.com/DEF-002",
		Magnet:     "magnet:?xt=urn:btih:BBBB",
	})
	if err != nil {
		t.Fatalf("write normal film failed: %v", err)
	}

	if err := w.Flush(); err != nil {
		t.Fatalf("flush failed: %v", err)
	}

	jsonBytes, err := os.ReadFile(filepath.Join(dir, crawlartifact.CrawlFilmDataFile))
	if err != nil {
		t.Fatalf("read filmData.json failed: %v", err)
	}

	var records []FilmData
	if err := json.Unmarshal(jsonBytes, &records); err != nil {
		t.Fatalf("unmarshal filmData.json failed: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records in filmData.json, got %d", len(records))
	}

	magnetBytes, err := os.ReadFile(filepath.Join(dir, crawlartifact.DefaultMagnetTxt))
	if err != nil {
		t.Fatalf("read magnet-links.txt failed: %v", err)
	}
	magnetText := string(magnetBytes)
	if contains(magnetText, "AAAA") {
		t.Fatalf("filtered magnet should not appear in TXT: %s", magnetText)
	}
	if !contains(magnetText, "BBBB") {
		t.Fatalf("normal magnet should remain in TXT: %s", magnetText)
	}

	filteredBytes, err := os.ReadFile(filepath.Join(dir, crawlartifact.DefaultFilteredCodesTxt))
	if err != nil {
		t.Fatalf("read filtered codes txt failed: %v", err)
	}
	filteredText := string(filteredBytes)
	if !contains(filteredText, "ABC-001") {
		t.Fatalf("filtered code txt should contain ABC-001: %s", filteredText)
	}
	if contains(filteredText, "DEF-002") {
		t.Fatalf("filtered code txt should not contain unfiltered code: %s", filteredText)
	}
}

func TestWriterFiltersFilmCodeFromMagnetTextOnly(t *testing.T) {
	dir := t.TempDir()

	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("create writer failed: %v", err)
	}

	_, err = w.WriteFilmData(FilmData{
		Title:              "MDVR-393 VR Film",
		SourceLink:         "https://example.com/MDVR-393",
		Magnet:             "magnet:?xt=urn:btih:VRVR",
		ActressCount:       1,
		FilteredByFilmCode: true,
		FilterRemark:       "film code matches filter \"VR\", skip magnet-links.txt output only",
	})
	if err != nil {
		t.Fatalf("write filtered film failed: %v", err)
	}

	_, err = w.WriteFilmData(FilmData{
		Title:      "ABP-001 Normal Film",
		SourceLink: "https://example.com/ABP-001",
		Magnet:     "magnet:?xt=urn:btih:ABAB",
	})
	if err != nil {
		t.Fatalf("write normal film failed: %v", err)
	}

	if err := w.Flush(); err != nil {
		t.Fatalf("flush failed: %v", err)
	}

	jsonBytes, err := os.ReadFile(filepath.Join(dir, crawlartifact.CrawlFilmDataFile))
	if err != nil {
		t.Fatalf("read filmData.json failed: %v", err)
	}

	var records []FilmData
	if err := json.Unmarshal(jsonBytes, &records); err != nil {
		t.Fatalf("unmarshal filmData.json failed: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records in filmData.json, got %d", len(records))
	}

	magnetBytes, err := os.ReadFile(filepath.Join(dir, crawlartifact.DefaultMagnetTxt))
	if err != nil {
		t.Fatalf("read magnet-links.txt failed: %v", err)
	}
	magnetText := string(magnetBytes)
	if contains(magnetText, "VRVR") {
		t.Fatalf("film-code filtered magnet should not appear in TXT: %s", magnetText)
	}
	if !contains(magnetText, "ABAB") {
		t.Fatalf("normal magnet should remain in TXT: %s", magnetText)
	}

	filteredBytes, err := os.ReadFile(filepath.Join(dir, crawlartifact.DefaultFilteredCodesTxt))
	if err != nil {
		t.Fatalf("read filtered codes txt failed: %v", err)
	}
	filteredText := string(filteredBytes)
	if !contains(filteredText, "MDVR-393") {
		t.Fatalf("filtered code txt should contain MDVR-393: %s", filteredText)
	}
	if contains(filteredText, "ABP-001") {
		t.Fatalf("filtered code txt should not contain unfiltered code: %s", filteredText)
	}
}

func TestWriterFiltersReleaseDate(t *testing.T) {
	dir := t.TempDir()

	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("create writer failed: %v", err)
	}
	w.SetMinReleaseDate("2016-01-01")
	if !w.isReleaseDateFiltered("2015-06-15") {
		t.Fatal("writer did not apply min release date before writes")
	}

	_, err = w.WriteFilmData(FilmData{
		Title:       "OLD-001 Old Film",
		SourceLink:  "https://example.com/OLD-001",
		Magnet:      "magnet:?xt=urn:btih:OLD1",
		ReleaseDate: "2015-06-15",
	})
	if err != nil {
		t.Fatalf("write old film failed: %v", err)
	}

	_, err = w.WriteFilmData(FilmData{
		Title:       "NEW-001 New Film",
		SourceLink:  "https://example.com/NEW-001",
		Magnet:      "magnet:?xt=urn:btih:NEW1",
		ReleaseDate: "2018-03-20",
	})
	if err != nil {
		t.Fatalf("write new film failed: %v", err)
	}

	_, err = w.WriteFilmData(FilmData{
		Title:       "BDR-001 Boundary Film",
		SourceLink:  "https://example.com/BDR-001",
		Magnet:      "magnet:?xt=urn:btih:BDR1",
		ReleaseDate: "2016-01-01",
	})
	if err != nil {
		t.Fatalf("write boundary film failed: %v", err)
	}

	if err := w.Flush(); err != nil {
		t.Fatalf("flush failed: %v", err)
	}

	jsonBytes, err := os.ReadFile(filepath.Join(dir, crawlartifact.CrawlFilmDataFile))
	if err != nil {
		t.Fatalf("read filmData.json failed: %v", err)
	}

	var records []FilmData
	if err := json.Unmarshal(jsonBytes, &records); err != nil {
		t.Fatalf("unmarshal filmData.json failed: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 visible records, got %d: %+v", len(records), records)
	}

	magnetBytes, err := os.ReadFile(filepath.Join(dir, crawlartifact.DefaultMagnetTxt))
	if err != nil {
		t.Fatalf("read magnet-links.txt failed: %v", err)
	}
	magnetText := string(magnetBytes)
	if contains(magnetText, "OLD1") {
		t.Fatalf("release-date filtered magnet should not appear in TXT: %s", magnetText)
	}
	if !contains(magnetText, "NEW1") || !contains(magnetText, "BDR1") {
		t.Fatalf("magnet-links.txt should keep 2016-01-01 and later films: %s", magnetText)
	}

	filteredBytes, err := os.ReadFile(filepath.Join(dir, crawlartifact.DefaultFilteredCodesTxt))
	if err != nil {
		t.Fatalf("read filtered codes txt failed: %v", err)
	}
	filteredText := string(filteredBytes)
	if !contains(filteredText, "OLD-001") {
		t.Fatalf("filtered code txt should contain OLD-001: %s", filteredText)
	}
	if contains(filteredText, "NEW-001") || contains(filteredText, "BDR-001") {
		t.Fatalf("filtered code txt should not contain unfiltered codes: %s", filteredText)
	}
}

func TestIsReleaseDateFiltered(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("create writer failed: %v", err)
	}
	w.SetMinReleaseDate("2016-01-01")
	if !w.isReleaseDateFiltered("2015-06-15") {
		t.Fatalf("expected 2015-06-15 to be filtered")
	}
	if w.isReleaseDateFiltered("2016-01-01") {
		t.Fatalf("expected boundary date to be kept")
	}
	if w.isReleaseDateFiltered("2018-03-20") {
		t.Fatalf("expected later date to be kept")
	}
}

func TestWriterUnfinishedReport(t *testing.T) {
	dir := t.TempDir()

	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("create writer failed: %v", err)
	}

	lines := []string{"ABC-001", "DEF-002", "", "ABC-001"}
	if err := w.WriteUnfinishedReport(lines); err != nil {
		t.Fatalf("write unfinished report failed: %v", err)
	}

	path := crawlartifact.DefaultUnfinishedReportPath(dir)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read unfinished report failed: %v", err)
	}
	if !bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatal("unfinished report should start with UTF-8 BOM")
	}

	content := string(data)
	if !contains(content, "ABC-001") || !contains(content, "DEF-002") {
		t.Fatalf("unfinished report content incomplete: %s", content)
	}

	if err := w.WriteUnfinishedReport(nil); err != nil {
		t.Fatalf("clear unfinished report failed: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("unfinished report should be removed when there is no content")
	}
}

func TestWriterCleanupLegacy(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "task-state.json")
	_ = os.WriteFile(legacyPath, []byte("{}"), 0o644)

	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("create writer failed: %v", err)
	}
	w.CleanupLegacyArtifacts()

	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatal("task-state.json should be removed")
	}
}

func TestWriterFlushWritesDerivedArtifacts(t *testing.T) {
	dir := t.TempDir()

	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("create writer failed: %v", err)
	}

	_, err = w.WriteFilmData(FilmData{
		Title:      "ABP-889 Sample Film",
		SourceLink: "https://example.com/ABP-889",
		Actress:    []string{"结城りの"},
		Magnet:     "magnet:?xt=urn:btih:AAAA",
	})
	if err != nil {
		t.Fatalf("write first record failed: %v", err)
	}

	_, err = w.WriteFilmData(FilmData{
		Title:      "ABP-890 Sample Film",
		SourceLink: "https://example.com/ABP-890",
		Actress:    []string{"结城りの", "他人"},
		MagnetLinks: []struct {
			Link string `json:"link"`
			Size string `json:"size"`
		}{
			{Link: "magnet:?xt=urn:btih:BBBB", Size: "2.1GB"},
		},
	})
	if err != nil {
		t.Fatalf("write second record failed: %v", err)
	}

	w.SetArtifactMetadata(ArtifactMetadata{
		RunID:          "crawl-test-run",
		CompletedAt:    "2026-05-05T10:20:30Z",
		ActressName:    "结城りの",
		CrawlURL:       "https://www.javbus.com/star/abc?page=1",
		TargetCount:    246,
		CompletedCount: 243,
		SiteBase:       "https://www.javbus.com",
	})

	if err := w.Flush(); err != nil {
		t.Fatalf("flush failed: %v", err)
	}

	profileBytes, err := os.ReadFile(filepath.Join(dir, crawlartifact.CrawlProfileFile))
	if err != nil {
		t.Fatalf("read crawl profile failed: %v", err)
	}
	var profile crawlartifact.CrawlProfileArtifact
	if err := json.Unmarshal(profileBytes, &profile); err != nil {
		t.Fatalf("unmarshal crawl profile failed: %v", err)
	}
	if profile.SchemaVersion != crawlartifact.CurrentSchemaVersion {
		t.Fatalf("expected crawl profile schemaVersion %d, got %d", crawlartifact.CurrentSchemaVersion, profile.SchemaVersion)
	}
	if profile.RunID != "crawl-test-run" || profile.TargetCount != 246 || profile.CompletedCount != 243 {
		t.Fatalf("unexpected crawl profile: %#v", profile)
	}

	codesBytes, err := os.ReadFile(filepath.Join(dir, crawlartifact.OrganizerCodesFile))
	if err != nil {
		t.Fatalf("read organizer codes failed: %v", err)
	}
	var organizerCodes crawlartifact.OrganizerCodesArtifact
	if err := json.Unmarshal(codesBytes, &organizerCodes); err != nil {
		t.Fatalf("unmarshal organizer codes failed: %v", err)
	}
	if organizerCodes.SchemaVersion != crawlartifact.CurrentSchemaVersion {
		t.Fatalf("expected organizer codes schemaVersion %d, got %d", crawlartifact.CurrentSchemaVersion, organizerCodes.SchemaVersion)
	}
	if organizerCodes.ActressName != "结城りの" {
		t.Fatalf("expected actressName from metadata, got %#v", organizerCodes.ActressName)
	}
	if organizerCodes.UniqueCodeCount != 2 || len(organizerCodes.CodeEntries) != 2 {
		t.Fatalf("unexpected organizer code stats: %#v", organizerCodes)
	}
	if organizerCodes.CodeEntries[0].Code != "ABP-889" {
		t.Fatalf("expected first code ABP-889, got %#v", organizerCodes.CodeEntries[0])
	}
}

func TestWriterMetadataOnlyFlushUpdatesDerivedArtifacts(t *testing.T) {
	dir := t.TempDir()

	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("create writer failed: %v", err)
	}

	_, err = w.WriteFilmData(FilmData{
		Title:      "ABP-889 Sample Film",
		SourceLink: "https://example.com/ABP-889",
		Actress:    []string{"结城りの"},
	})
	if err != nil {
		t.Fatalf("write record failed: %v", err)
	}

	w.SetArtifactMetadata(ArtifactMetadata{
		RunID:          "crawl-test-run-1",
		CompletedAt:    "2026-05-05T10:20:30Z",
		ActressName:    "结城りの",
		TargetCount:    246,
		CompletedCount: 200,
	})

	if err := w.Flush(); err != nil {
		t.Fatalf("first flush failed: %v", err)
	}

	w.SetArtifactMetadata(ArtifactMetadata{
		RunID:          "crawl-test-run-1",
		CompletedAt:    "2026-05-05T10:22:30Z",
		ActressName:    "结城りの",
		TargetCount:    246,
		CompletedCount: 243,
	})

	if err := w.Flush(); err != nil {
		t.Fatalf("second flush failed: %v", err)
	}

	profileBytes, err := os.ReadFile(filepath.Join(dir, crawlartifact.CrawlProfileFile))
	if err != nil {
		t.Fatalf("read crawl profile failed: %v", err)
	}
	var profile crawlartifact.CrawlProfileArtifact
	if err := json.Unmarshal(profileBytes, &profile); err != nil {
		t.Fatalf("unmarshal crawl profile failed: %v", err)
	}
	if profile.SchemaVersion != crawlartifact.CurrentSchemaVersion {
		t.Fatalf("expected crawl profile schemaVersion %d, got %d", crawlartifact.CurrentSchemaVersion, profile.SchemaVersion)
	}
	if profile.CompletedCount != 243 || profile.CompletedAt != "2026-05-05T10:22:30Z" {
		t.Fatalf("metadata-only flush did not refresh crawl profile: %#v", profile)
	}
}

func TestWriterWithInternalArtifactPathsWritesBridgeArtifactsOutsideOutputDir(t *testing.T) {
	outputDir := t.TempDir()
	userDataDir := filepath.Join(t.TempDir(), "user-data")
	artifactPaths := crawlartifact.ResolveInternalArtifactPaths(userDataDir, outputDir)

	w, err := NewWriterWithArtifactPaths(outputDir, artifactPaths)
	if err != nil {
		t.Fatalf("create writer failed: %v", err)
	}

	_, err = w.WriteFilmData(FilmData{
		Title:      "ABP-889 Sample Film",
		SourceLink: "https://example.com/ABP-889",
		Actress:    []string{"Yuki Rino"},
		Magnet:     "magnet:?xt=urn:btih:AAAA",
	})
	if err != nil {
		t.Fatalf("write film data failed: %v", err)
	}

	w.SetArtifactMetadata(ArtifactMetadata{
		RunID:          "crawl-test-run",
		CompletedAt:    "2026-05-11T10:20:30Z",
		ActressName:    "Yuki Rino",
		CrawlURL:       "https://www.javbus.com/star/okq",
		TargetCount:    1,
		CompletedCount: 1,
	})

	if err := w.Flush(); err != nil {
		t.Fatalf("flush failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(outputDir, crawlartifact.CrawlProfileFile)); !os.IsNotExist(err) {
		t.Fatalf("crawl-profile.json should not be written to output dir")
	}
	if _, err := os.Stat(filepath.Join(outputDir, crawlartifact.OrganizerCodesFile)); !os.IsNotExist(err) {
		t.Fatalf("organizer-codes.json should not be written to output dir")
	}
	if _, err := os.Stat(artifactPaths.CrawlProfilePath); err != nil {
		t.Fatalf("expected internal crawl-profile artifact: %v", err)
	}
	if _, err := os.Stat(artifactPaths.OrganizerCodesPath); err != nil {
		t.Fatalf("expected internal organizer-codes artifact: %v", err)
	}
	if _, err := os.Stat(artifactPaths.FilmDataPath); err != nil {
		t.Fatalf("expected complete internal filmData artifact: %v", err)
	}
	if filepath.Dir(artifactPaths.FilmDataPath) == outputDir {
		t.Fatalf("internal filmData must not be stored in the visible output directory")
	}
}

func TestExtractFilmID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ABC-001", "ABC-001"},
		{"abc-001", "ABC-001"},
		{"AB-01", "AB-01"},
		{"NoDash", ""},
		{"-noLeading", ""},
		{"noTrailing-", ""},
		{"", ""},
		{"SONE-943 sample", "SONE-943"},
	}

	for _, tc := range tests {
		result := extractFilmID(tc.input)
		if result != tc.expected {
			t.Fatalf("extractFilmID(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func contains(s string, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
