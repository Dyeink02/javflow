package crawlartifact

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveCompletedCacheSnapshotKeepsReadableHistory(t *testing.T) {
	userDataDir := t.TempDir()
	outputDir := filepath.Join(t.TempDir(), "actor-output")
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
