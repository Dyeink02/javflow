package bridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"javflow/internal/contracts/crawlartifact"
)

// api_output_context.go owns bridge-side canonical crawl-output path resolution
// shared by organizer, subscription, diagnostics, and open-path actions.
//
// Ownership summary:
// 1) collapse artifactInput and legacy aliases into one canonical output dir
// 2) derive sibling artifact paths from the shared crawlartifact contract
// 3) keep path fallback order in one bridge-local helper layer
//
// File map for maintainers:
// 1) output-context DTOs
// 2) artifact-aware output-dir normalization helpers
// 3) sibling artifact path derivation and fallback order helpers

// outputContext is the stable handoff shape shared by crawl-adjacent modules.
//
// Organizer, subscriptions, diagnostics, and "open path" actions should read
// output-derived paths through this boundary instead of rebuilding fallback
// rules independently.
//
// Resolution order:
// 1) explicit caller input, with artifact-aware normalization
// 2) preferred crawl run-context paths
// 3) runtime store fallback
//
// "artifact-aware normalization" means callers may hand in:
// - crawl-profile.json
// - filmData.json
// - organizer-codes.json
// - the crawl output directory itself
//
// The boundary always collapses those forms to one canonical output dir.
type outputContext struct {
	OutputDir          string
	FilmDataPath       string
	CrawlProfilePath   string
	OrganizerCodesPath string
	MagnetPath         string
	LogDir             string
	LatestLogPath      string
}

// resolveArtifactInputAlias keeps artifact-input compatibility priority in one
// place for bridge callers that accept a preferred artifactInput field plus one
// or more older alias names.
//
// It only selects the raw caller-provided value; the follow-up outputContext
// normalization is still responsible for collapsing file paths such as
// filmData.json into the canonical crawl output directory.
func resolveArtifactInputAlias(payload map[string]any, aliasKeys ...string) string {
	values := make([]string, 0, len(aliasKeys)+1)
	values = append(values, nonEmptyString(payload["artifactInput"]))
	for _, key := range aliasKeys {
		if normalizedKey := strings.TrimSpace(key); normalizedKey != "" {
			values = append(values, nonEmptyString(payload[normalizedKey]))
		}
	}
	return firstNonEmptyString(values...)
}

// resolveArtifactInputOutputDir is the shared bridge helper for modules that
// accept a preferred artifactInput field plus one or more legacy aliases, but
// ultimately need the canonical crawl output directory for downstream service
// work.
//
// It keeps the compatibility rule and artifact-path normalization in one place
// so organizer/subscription entrypoints do not each rebuild the same "artifact
// file or output dir" conversion.
//
// Output-context rule:
// this helper resolves path relationships only. It should not start deciding
// whether a caller is allowed to read/use a given artifact.
func (a *API) resolveArtifactInputOutputDir(payload map[string]any, aliasKeys ...string) string {
	return a.resolveOutputContext(resolveArtifactAwareOutputDir(resolveArtifactInputAlias(payload, aliasKeys...))).OutputDir
}

// resolveOutputContext assembles the canonical crawl-output path snapshot used
// by organizer/subscription/diagnostic bridge queries.
func (a *API) resolveOutputContext(explicitOutputDir string) outputContext {
	ctx := outputContext{
		OutputDir: normalizeArtifactAwareOutputDir(strings.TrimSpace(explicitOutputDir)),
	}
	// Collapse a parent/root directory that contains no artifacts but has
	// timestamped run subfolders down to the newest concrete crawl run.
	ctx.OutputDir = crawlartifact.ResolveEffectiveCrawlOutputDir(ctx.OutputDir)
	ctx = a.applyRunContextOutputFallback(ctx)
	ctx = a.applyRuntimeStoreOutputFallback(ctx)
	ctx = deriveArtifactPaths(ctx)

	return ctx
}

// applyRunContextOutputFallback pulls preferred artifact paths from the current
// crawl run-context only when the caller did not already choose an explicit
// artifact/output location.
func (a *API) applyRunContextOutputFallback(ctx outputContext) outputContext {
	if ctx.OutputDir != "" {
		return ctx
	}
	runContext, err := a.getCrawlRunContext()
	if err != nil {
		return ctx
	}
	ctx.OutputDir = strings.TrimSpace(runContext.PreferredOutputDir)
	ctx.FilmDataPath = strings.TrimSpace(runContext.PreferredFilmDataPath)
	ctx.CrawlProfilePath = strings.TrimSpace(runContext.PreferredCrawlProfilePath)
	ctx.OrganizerCodesPath = strings.TrimSpace(runContext.PreferredOrganizerCodesPath)
	ctx.MagnetPath = strings.TrimSpace(runContext.PreferredMagnetPath)
	ctx.LogDir = strings.TrimSpace(runContext.LogDir)
	ctx.LatestLogPath = strings.TrimSpace(runContext.LatestLogPath)
	return ctx
}

// applyRuntimeStoreOutputFallback is the final compatibility fallback for older
// UI paths that only knew "current output dir".
func (a *API) applyRuntimeStoreOutputFallback(ctx outputContext) outputContext {
	if ctx.OutputDir != "" || a == nil || a.runtime.store == nil {
		return ctx
	}
	if outputDir, err := a.runtime.store.GetCurrentOutputDir(); err == nil {
		ctx.OutputDir = strings.TrimSpace(outputDir)
	}
	return ctx
}

// deriveArtifactPaths expands a canonical output directory into sibling crawl
// artifact paths through the shared crawlartifact contract.
func deriveArtifactPaths(ctx outputContext) outputContext {
	if ctx.OutputDir == "" {
		return ctx
	}
	runPaths := crawlartifact.ResolveCrawlRunPaths(ctx.OutputDir)
	if ctx.FilmDataPath == "" {
		ctx.FilmDataPath = runPaths.FilmDataPath
	}
	if ctx.CrawlProfilePath == "" {
		ctx.CrawlProfilePath = runPaths.CrawlProfilePath
	}
	if ctx.OrganizerCodesPath == "" {
		ctx.OrganizerCodesPath = runPaths.OrganizerCodesPath
	}
	if ctx.MagnetPath == "" {
		ctx.MagnetPath = runPaths.MagnetPath
	}
	if ctx.LogDir == "" {
		ctx.LogDir = runPaths.LogDir
	}
	if ctx.LatestLogPath == "" {
		if ctx.LogDir == runPaths.LogDir {
			ctx.LatestLogPath = runPaths.LatestLogPath
		} else {
			ctx.LatestLogPath = crawlartifact.DefaultLatestLogPath(ctx.LogDir)
		}
	}
	return ctx
}

// normalizeArtifactAwareOutputDir collapses accepted artifact file inputs back
// to the owning crawl output directory.
func normalizeArtifactAwareOutputDir(value string) string {
	return resolveArtifactAwareOutputDir(value)
}

func resolveArtifactAwareOutputDir(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}

	lowerBase := strings.ToLower(filepath.Base(trimmed))
	switch lowerBase {
	case "crawl-profile.json":
		if outputDir := extractOutputDirFromArtifactFile(trimmed); outputDir != "" {
			return outputDir
		}
		return strings.TrimSpace(filepath.Dir(trimmed))
	case "organizer-codes.json":
		if outputDir := extractOutputDirFromArtifactFile(trimmed); outputDir != "" {
			return outputDir
		}
		return strings.TrimSpace(filepath.Dir(trimmed))
	case "filmdata.json":
		return strings.TrimSpace(filepath.Dir(trimmed))
	default:
		return trimmed
	}
}

func extractOutputDirFromArtifactFile(filePath string) string {
	payload, err := os.ReadFile(strings.TrimSpace(filePath))
	if err != nil || len(payload) == 0 {
		return ""
	}

	var parsed map[string]any
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return ""
	}

	outputDir, _ := parsed["outputDir"].(string)
	return strings.TrimSpace(outputDir)
}

// firstNonEmptyString keeps bridge fallback chains readable at call sites.
func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		trimmed := nonEmptyString(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
