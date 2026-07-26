package bridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"javflow/internal/contracts/crawlartifact"
	"javflow/internal/crawlruncontext"
	"javflow/internal/runtimecache"
	"javflow/internal/settings"
)

func TestNormalizeArtifactAwareOutputDir(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{input: `C:\crawl\run-001\filmData.json`, expected: `C:\crawl\run-001`},
		{input: `C:\crawl\run-001\crawl-profile.json`, expected: `C:\crawl\run-001`},
		{input: `C:\crawl\run-001\organizer-codes.json`, expected: `C:\crawl\run-001`},
		{input: `C:\crawl\run-001`, expected: `C:\crawl\run-001`},
		{input: ``, expected: ``},
	}

	for _, tc := range tests {
		if got := normalizeArtifactAwareOutputDir(tc.input); got != tc.expected {
			t.Fatalf("normalizeArtifactAwareOutputDir(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

type outputContextTestOutputProvider struct {
	outputDir string
}

func (s outputContextTestOutputProvider) GetCurrentOutputDir() (string, error) {
	return s.outputDir, nil
}

func TestResolveOutputContextUsesExplicitOutputDirArtifacts(t *testing.T) {
	tempDir := t.TempDir()
	explicitOutputDir := filepath.Join(tempDir, "explicit-run")
	runContextDir := filepath.Join(tempDir, "active-run")

	state := runtimecache.NewState()
	state.ApplyIntegrationContext(map[string]any{
		"currentTaskOutputDir": runContextDir,
		"lastTaskOutputDir":    runContextDir,
	})
	state.ApplyLogContext(map[string]any{
		"logDir":         filepath.Join(runContextDir, crawlartifact.DefaultLogDirName),
		"sessionLogPath": filepath.Join(runContextDir, "logs", "crawl-session-1.txt"),
		"latestLogPath":  crawlartifact.DefaultLatestLogPath(filepath.Join(runContextDir, crawlartifact.DefaultLogDirName)),
	})

	api := &API{
		runtime: runtimeFacade{
			store:        &settings.Store{},
			runtimeState: state,
		},
		crawl: crawlFacade{
			crawlRunContext: crawlruncontext.NewService(
				outputContextTestOutputProvider{outputDir: runContextDir},
				state,
			),
		},
	}

	ctx := api.resolveOutputContext(explicitOutputDir)
	if ctx.OutputDir != explicitOutputDir {
		t.Fatalf("expected explicit output dir %q, got %q", explicitOutputDir, ctx.OutputDir)
	}
	if ctx.FilmDataPath != filepath.Join(explicitOutputDir, crawlartifact.CrawlFilmDataFile) {
		t.Fatalf("expected explicit filmData path, got %q", ctx.FilmDataPath)
	}
	if ctx.CrawlProfilePath != filepath.Join(explicitOutputDir, "crawl-profile.json") {
		t.Fatalf("expected explicit crawl-profile path, got %q", ctx.CrawlProfilePath)
	}
	if ctx.OrganizerCodesPath != filepath.Join(explicitOutputDir, "organizer-codes.json") {
		t.Fatalf("expected explicit organizer-codes path, got %q", ctx.OrganizerCodesPath)
	}
	if ctx.MagnetPath != filepath.Join(explicitOutputDir, crawlartifact.DefaultMagnetTxt) {
		t.Fatalf("expected explicit magnet path, got %q", ctx.MagnetPath)
	}
	if ctx.LogDir != filepath.Join(explicitOutputDir, crawlartifact.DefaultLogDirName) {
		t.Fatalf("expected explicit log dir, got %q", ctx.LogDir)
	}
	if ctx.LatestLogPath != crawlartifact.DefaultLatestLogPath(filepath.Join(explicitOutputDir, crawlartifact.DefaultLogDirName)) {
		t.Fatalf("expected explicit latest-log path, got %q", ctx.LatestLogPath)
	}
}

func TestResolveOutputContextFallsBackToRunContext(t *testing.T) {
	tempDir := t.TempDir()
	runContextDir := filepath.Join(tempDir, "active-run")

	state := runtimecache.NewState()
	state.ApplyIntegrationContext(map[string]any{
		"currentTaskOutputDir": runContextDir,
		"lastTaskOutputDir":    runContextDir,
	})
	state.ApplyLogContext(map[string]any{
		"logDir":         filepath.Join(runContextDir, crawlartifact.DefaultLogDirName),
		"sessionLogPath": filepath.Join(runContextDir, "logs", "crawl-session-1.txt"),
		"latestLogPath":  crawlartifact.DefaultLatestLogPath(filepath.Join(runContextDir, crawlartifact.DefaultLogDirName)),
	})

	api := &API{
		runtime: runtimeFacade{
			store:        &settings.Store{},
			runtimeState: state,
		},
		crawl: crawlFacade{
			crawlRunContext: crawlruncontext.NewService(
				outputContextTestOutputProvider{outputDir: runContextDir},
				state,
			),
		},
	}

	ctx := api.resolveOutputContext("")
	if ctx.OutputDir != runContextDir {
		t.Fatalf("expected run-context output dir %q, got %q", runContextDir, ctx.OutputDir)
	}
	if ctx.FilmDataPath != filepath.Join(runContextDir, crawlartifact.CrawlFilmDataFile) {
		t.Fatalf("expected run-context filmData path, got %q", ctx.FilmDataPath)
	}
	if ctx.LogDir != filepath.Join(runContextDir, crawlartifact.DefaultLogDirName) {
		t.Fatalf("expected run-context log dir, got %q", ctx.LogDir)
	}
	if ctx.LatestLogPath != crawlartifact.DefaultLatestLogPath(filepath.Join(runContextDir, crawlartifact.DefaultLogDirName)) {
		t.Fatalf("expected run-context latest-log path, got %q", ctx.LatestLogPath)
	}
}

func TestResolveArtifactInputOutputDirPrefersArtifactInputOverLegacyAliases(t *testing.T) {
	api := &API{}

	payload := map[string]any{
		"artifactInput":  `C:\crawl\active-run\filmData.json`,
		"outputDir":      `C:\crawl\legacy-run`,
		"crawlOutputDir": `C:\crawl\older-run`,
	}

	got := api.resolveArtifactInputOutputDir(payload, "outputDir", "crawlOutputDir")
	want := `C:\crawl\active-run`
	if got != want {
		t.Fatalf("resolveArtifactInputOutputDir() = %q, want %q", got, want)
	}
}

func TestResolveArtifactInputOutputDirAcceptsLegacyAliasFallback(t *testing.T) {
	api := &API{}

	payload := map[string]any{
		"crawlOutputDir": `C:\crawl\alias-run`,
	}

	got := api.resolveArtifactInputOutputDir(payload, "outputDir", "crawlOutputDir")
	want := `C:\crawl\alias-run`
	if got != want {
		t.Fatalf("resolveArtifactInputOutputDir() = %q, want %q", got, want)
	}
}

func TestResolveArtifactInputOutputDirReadsOutputDirFromInternalCrawlProfileArtifact(t *testing.T) {
	tempDir := t.TempDir()
	realOutputDir := filepath.Join(tempDir, "crawl-run")
	internalDir := filepath.Join(tempDir, "user-data", "crawl-artifacts", "run-1")
	artifactPath := filepath.Join(internalDir, crawlartifact.CrawlProfileFile)

	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatalf("mkdir internal artifact dir: %v", err)
	}
	payload := []byte(`{"outputDir":"` + strings.ReplaceAll(realOutputDir, `\`, `\\`) + `"}`)
	if err := os.WriteFile(artifactPath, payload, 0o644); err != nil {
		t.Fatalf("write internal crawl profile: %v", err)
	}

	api := &API{}
	got := api.resolveArtifactInputOutputDir(map[string]any{
		"artifactInput": artifactPath,
	}, "outputDir")
	if got != realOutputDir {
		t.Fatalf("resolveArtifactInputOutputDir() = %q, want %q", got, realOutputDir)
	}
}
