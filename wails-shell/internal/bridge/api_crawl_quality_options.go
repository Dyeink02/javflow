package bridge

import "javflow/internal/crawlquality"

// Quality-summary option shaping is kept separate from the runtime panel
// readers because output-path bugs and summary-stat bugs are usually debugged
// together without needing the rest of the bridge read model.
//
// Ownership summary:
// 1) build normalized crawl-quality option payloads
// 2) combine output-context fallbacks with explicit override fields
// 3) keep quality option shaping separate from panel assembly
//
// File map for maintainers:
// 1) quality option builder entrypoint
// 2) output-context fallback merge helpers
func (a *API) buildCrawlQualityOptions(payload map[string]any) (crawlquality.Options, error) {
	outputCtx := a.resolveOutputContext(nonEmptyString(payload["outputDir"]))
	filmDataPath := nonEmptyString(payload["filmDataPath"])
	magnetPath := nonEmptyString(payload["magnetPath"])
	logDir := nonEmptyString(payload["logDir"])
	latestLogPath := nonEmptyString(payload["latestLogPath"])
	if filmDataPath == "" {
		filmDataPath = outputCtx.FilmDataPath
	}
	if magnetPath == "" {
		magnetPath = outputCtx.MagnetPath
	}
	if logDir == "" {
		logDir = outputCtx.LogDir
	}
	if latestLogPath == "" {
		latestLogPath = outputCtx.LatestLogPath
	}

	return crawlquality.Options{
		OutputDir:     outputCtx.OutputDir,
		LogDir:        logDir,
		LatestLogPath: latestLogPath,
		FilmDataPath:  filmDataPath,
		MagnetPath:    magnetPath,
		WriteReport:   boolValue(payload["writeReport"], false),
	}, nil
}
