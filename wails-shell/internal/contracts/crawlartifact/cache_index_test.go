package crawlartifact

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchiveCompletedCacheSnapshotKeepsReadableHistory(t *testing.T) {
	userDataDir := t.TempDir()
	outputDir := filepath.Join(t.TempDir(), "actor-output")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	paths := ResolveInternalArtifactPaths(userDataDir, outputDir)
	if err := os.MkdirAll(filepath.Dir(paths.FilmDataPath), 0o755); err != nil {
		t.Fatal(err)
	}
	profile := CrawlProfileArtifact{
		SchemaVersion:  CurrentSchemaVersion,
		RunID:          "run-20260722-1",
		CompletedAt:    "2026-07-22T12:00:00+08:00",
		ActressName:    "Actor A",
		OutputDir:      outputDir,
		FilmDataPath:   paths.FilmDataPath,
		CompletedCount: 3,
	}
	profilePayload, _ := json.Marshal(profile)
	if err := os.WriteFile(paths.FilmDataPath, []byte(`[{"title":"AAA-001"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.CrawlProfilePath, profilePayload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpsertCacheSnapshot(userDataDir, BuildCacheSnapshot(paths, profile, "crawler")); err != nil {
		t.Fatal(err)
	}
	if err := ArchiveCompletedCacheSnapshot(userDataDir, paths, profile, "crawler"); err != nil {
		t.Fatal(err)
	}
	items, err := ListCacheSnapshots(userDataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one archived history item, got %#v", items)
	}
	if items[0].DisplayName != "7\u670822\u65e5 Actor A 3\u90e8" {
		t.Fatalf("unexpected display name: %s", items[0].DisplayName)
	}
	if _, err := os.Stat(items[0].FilmDataPath); err != nil {
		t.Fatalf("archived film data is not readable: %v", err)
	}
}

func TestRemoveCacheSnapshotRemovesHistoryAndStableAlias(t *testing.T) {
	userDataDir := t.TempDir()
	outputDir := filepath.Join(t.TempDir(), "actor-output")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	paths := ResolveInternalArtifactPaths(userDataDir, outputDir)
	if err := os.MkdirAll(filepath.Dir(paths.FilmDataPath), 0o755); err != nil {
		t.Fatal(err)
	}
	profile := CrawlProfileArtifact{
		SchemaVersion:  CurrentSchemaVersion,
		RunID:          "run-20260826-1",
		CompletedAt:    "2026-08-26T12:00:00+08:00",
		ActressName:    "Actor A",
		OutputDir:      outputDir,
		FilmDataPath:   paths.FilmDataPath,
		CompletedCount: 1,
	}
	profilePayload, _ := json.Marshal(profile)
	if err := os.WriteFile(paths.FilmDataPath, []byte(`[{"title":"AAA-001"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.CrawlProfilePath, profilePayload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpsertCacheSnapshot(userDataDir, BuildCacheSnapshot(paths, profile, "crawler")); err != nil {
		t.Fatal(err)
	}
	if err := ArchiveCompletedCacheSnapshot(userDataDir, paths, profile, "crawler"); err != nil {
		t.Fatal(err)
	}

	items, err := ListCacheSnapshots(userDataDir)
	if err != nil || len(items) != 1 {
		t.Fatalf("expected one archived snapshot, items=%#v err=%v", items, err)
	}
	archiveDir := filepath.Dir(items[0].FilmDataPath)
	if removed, err := RemoveCacheSnapshot(userDataDir, items[0].CacheKey); err != nil || removed != 1 {
		t.Fatalf("RemoveCacheSnapshot: removed=%d err=%v", removed, err)
	}
	if _, err := os.Stat(ResolveInternalArtifactRoot(userDataDir, outputDir)); !os.IsNotExist(err) {
		t.Fatalf("stable alias still exists after deletion: %v", err)
	}
	if _, err := os.Stat(archiveDir); !os.IsNotExist(err) {
		t.Fatalf("history archive still exists after deletion: %v", err)
	}
	if _, err := os.Stat(outputDir); err != nil {
		t.Fatalf("deleting a snapshot must not delete user output: %v", err)
	}
	reloaded, err := DiscoverCacheSnapshots(userDataDir, nil)
	if err != nil || len(reloaded) != 0 {
		t.Fatalf("deleted snapshot reappeared after discovery: %#v err=%v", reloaded, err)
	}
}

func TestRemovedSnapshotStaysHiddenWhenVisibleOutputIsRediscovered(t *testing.T) {
	userDataDir := t.TempDir()
	outputDir := filepath.Join(t.TempDir(), "actor-output")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	visibleFilmData := filepath.Join(outputDir, CrawlFilmDataFile)
	if err := os.WriteFile(visibleFilmData, []byte(`[{"title":"AAA-001"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	internalPaths := ResolveInternalArtifactPaths(userDataDir, outputDir)
	if err := os.MkdirAll(filepath.Dir(internalPaths.FilmDataPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(internalPaths.FilmDataPath, []byte(`[{"title":"AAA-001"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := CrawlProfileArtifact{
		SchemaVersion:  CurrentSchemaVersion,
		RunID:          "run-delete-visible-1",
		CompletedAt:    "2026-08-28T12:00:00Z",
		ActressName:    "Actor A",
		OutputDir:      outputDir,
		FilmDataPath:   internalPaths.FilmDataPath,
		CompletedCount: 1,
	}
	profilePayload, _ := json.Marshal(profile)
	if err := os.WriteFile(internalPaths.CrawlProfilePath, profilePayload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpsertCacheSnapshot(userDataDir, BuildCacheSnapshot(internalPaths, profile, "discovered")); err != nil {
		t.Fatal(err)
	}
	items, err := ListCacheSnapshots(userDataDir)
	if err != nil || len(items) != 1 {
		t.Fatalf("expected one snapshot before deletion, items=%#v err=%v", items, err)
	}
	if removed, err := RemoveCacheSnapshot(userDataDir, items[0].CacheKey); err != nil || removed != 1 {
		t.Fatalf("RemoveCacheSnapshot: removed=%d err=%v", removed, err)
	}

	// A different workspace (for example media-library bootstrap) can ask the
	// discovery path to scan the still-existing visible output directory. That
	// scan must honor the explicit deletion tombstone.
	reloaded, err := DiscoverCacheSnapshots(userDataDir, []string{outputDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded) != 0 {
		t.Fatalf("deleted snapshot was rebuilt from visible output: %#v", reloaded)
	}
	if _, err := os.Stat(visibleFilmData); err != nil {
		t.Fatalf("visible user output must remain after deletion: %v", err)
	}
}

func TestUpsertCacheSnapshotClearsDeletionTombstoneForNewRun(t *testing.T) {
	userDataDir := t.TempDir()
	outputDir := filepath.Join(t.TempDir(), "actor-output")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	visibleFilmData := filepath.Join(outputDir, CrawlFilmDataFile)
	if err := os.WriteFile(visibleFilmData, []byte(`[{"title":"AAA-001"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	internalPaths := ResolveInternalArtifactPaths(userDataDir, outputDir)
	if err := os.MkdirAll(filepath.Dir(internalPaths.FilmDataPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(internalPaths.FilmDataPath, []byte(`[{"title":"AAA-001"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := CrawlProfileArtifact{
		SchemaVersion:  CurrentSchemaVersion,
		RunID:          "run-delete-visible-2",
		CompletedAt:    "2026-08-28T12:00:00Z",
		ActressName:    "Actor A",
		OutputDir:      outputDir,
		FilmDataPath:   internalPaths.FilmDataPath,
		CompletedCount: 1,
	}
	if err := os.WriteFile(internalPaths.CrawlProfilePath, []byte(`{"actressName":"Actor A","outputDir":"`+strings.ReplaceAll(outputDir, `\`, `\\`)+`","completedCount":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpsertCacheSnapshot(userDataDir, BuildCacheSnapshot(internalPaths, profile, "discovered")); err != nil {
		t.Fatal(err)
	}
	items, err := ListCacheSnapshots(userDataDir)
	if err != nil || len(items) != 1 {
		t.Fatalf("expected one snapshot before deletion, items=%#v err=%v", items, err)
	}
	if _, err := RemoveCacheSnapshot(userDataDir, items[0].CacheKey); err != nil {
		t.Fatal(err)
	}

	// A subsequent real crawl writes the same output root through Upsert. This
	// is the explicit signal that the user wants that source visible again.
	if err := os.MkdirAll(filepath.Dir(internalPaths.FilmDataPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(internalPaths.FilmDataPath, []byte(`[{"title":"AAA-002"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpsertCacheSnapshot(userDataDir, BuildCacheSnapshot(internalPaths, profile, "discovered")); err != nil {
		t.Fatal(err)
	}
	reloaded, err := DiscoverCacheSnapshots(userDataDir, nil)
	if err != nil || len(reloaded) != 1 {
		t.Fatalf("new upserted run should be discoverable again: items=%#v err=%v", reloaded, err)
	}
}

func TestDiscoverCacheSnapshotsRemovesMissingOutputRoot(t *testing.T) {
	userDataDir := t.TempDir()
	outputDir := filepath.Join(t.TempDir(), "actor-output")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	paths := ResolveInternalArtifactPaths(userDataDir, outputDir)
	if err := os.MkdirAll(filepath.Dir(paths.FilmDataPath), 0o755); err != nil {
		t.Fatal(err)
	}
	profile := CrawlProfileArtifact{
		SchemaVersion:  CurrentSchemaVersion,
		RunID:          "run-20260826-2",
		CompletedAt:    "2026-08-26T12:00:00+08:00",
		ActressName:    "Actor B",
		OutputDir:      outputDir,
		FilmDataPath:   paths.FilmDataPath,
		CompletedCount: 1,
	}
	profilePayload, _ := json.Marshal(profile)
	if err := os.WriteFile(paths.FilmDataPath, []byte(`[{"title":"BBB-001"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.CrawlProfilePath, profilePayload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpsertCacheSnapshot(userDataDir, BuildCacheSnapshot(paths, profile, "crawler")); err != nil {
		t.Fatal(err)
	}
	if err := ArchiveCompletedCacheSnapshot(userDataDir, paths, profile, "crawler"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(outputDir); err != nil {
		t.Fatal(err)
	}

	items, err := DiscoverCacheSnapshots(userDataDir, nil)
	if err != nil || len(items) != 0 {
		t.Fatalf("missing output root should remove stale snapshot: %#v err=%v", items, err)
	}
	if _, err := os.Stat(ResolveInternalArtifactRoot(userDataDir, outputDir)); !os.IsNotExist(err) {
		t.Fatalf("stable alias remains after stale cleanup: %v", err)
	}
}

func TestDiscoverCacheSnapshotsRebuildsMissingOrEmptyIndex(t *testing.T) {
	for _, seedEmptyIndex := range []bool{false, true} {
		name := "missing-index"
		if seedEmptyIndex {
			name = "empty-index"
		}
		t.Run(name, func(t *testing.T) {
			userDataDir := t.TempDir()
			outputParent := t.TempDir()
			for _, actress := range []string{"松本いちか", "深田えいみ"} {
				outputDir := filepath.Join(outputParent, actress)
				if err := os.MkdirAll(outputDir, 0o755); err != nil {
					t.Fatal(err)
				}
				filmData := `[{
					"title": "ABP-001 title"
				}, {
					"title": "ABP-002 title"
				}]`
				if err := os.WriteFile(filepath.Join(outputDir, CrawlFilmDataFile), []byte(filmData), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			if seedEmptyIndex {
				if err := writeCacheIndex(userDataDir, []CacheSnapshot{}); err != nil {
					t.Fatal(err)
				}
			}

			items, err := DiscoverCacheSnapshots(userDataDir, []string{outputParent})
			if err != nil {
				t.Fatalf("DiscoverCacheSnapshots: %v", err)
			}
			if len(items) != 2 {
				t.Fatalf("expected 2 rebuilt snapshots, got %d", len(items))
			}
			for _, item := range items {
				if item.CompletedCount != 2 {
					t.Fatalf("expected film count 2 for %s, got %d", item.OutputDir, item.CompletedCount)
				}
				internalPaths := ResolveInternalArtifactPaths(userDataDir, item.OutputDir)
				if item.FilmDataPath != internalPaths.FilmDataPath {
					t.Fatalf("expected hidden filmData path %q, got %q", internalPaths.FilmDataPath, item.FilmDataPath)
				}
				if _, err := os.Stat(internalPaths.FilmDataPath); err != nil {
					t.Fatalf("hidden filmData was not backfilled: %v", err)
				}
			}

			payload, err := os.ReadFile(CacheIndexPath(userDataDir))
			if err != nil {
				t.Fatalf("repaired index missing: %v", err)
			}
			var repaired []CacheSnapshot
			if err := json.Unmarshal(payload, &repaired); err != nil {
				t.Fatalf("repaired index invalid: %v", err)
			}
			if len(repaired) != 2 {
				t.Fatalf("expected repaired index to contain 2 snapshots, got %d", len(repaired))
			}

			reloaded, err := DiscoverCacheSnapshots(userDataDir, []string{outputParent})
			if err != nil {
				t.Fatalf("second discovery failed: %v", err)
			}
			if len(reloaded) != 2 {
				t.Fatalf("hidden cache directories must not become duplicate output snapshots, got %d", len(reloaded))
			}
		})
	}
}

func TestReadFilmDataRecordsWithUserDataPrefersHiddenThenFallsBackToVisible(t *testing.T) {
	userDataDir := t.TempDir()
	outputDir := t.TempDir()
	visiblePath := filepath.Join(outputDir, CrawlFilmDataFile)
	if err := os.WriteFile(visiblePath, []byte(`[{"title":"PUBLIC-001"}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	internalPaths := ResolveInternalArtifactPaths(userDataDir, outputDir)
	if err := os.MkdirAll(filepath.Dir(internalPaths.FilmDataPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(internalPaths.FilmDataPath, []byte(`[{"title":"HIDDEN-001"}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	paths, records, err := ReadFilmDataRecordsWithUserData(outputDir, userDataDir)
	if err != nil {
		t.Fatal(err)
	}
	if paths.FilmDataPath != internalPaths.FilmDataPath || len(records) != 1 || records[0]["title"] != "HIDDEN-001" {
		t.Fatalf("hidden snapshot was not preferred: paths=%+v records=%+v", paths, records)
	}

	if err := os.Remove(internalPaths.FilmDataPath); err != nil {
		t.Fatal(err)
	}
	paths, records, err = ReadFilmDataRecordsWithUserData(outputDir, userDataDir)
	if err != nil {
		t.Fatal(err)
	}
	if paths.FilmDataPath != visiblePath || len(records) != 1 || records[0]["title"] != "PUBLIC-001" {
		t.Fatalf("visible fallback was not used: paths=%+v records=%+v", paths, records)
	}
}
