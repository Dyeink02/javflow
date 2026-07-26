package bridge

import (
	"strings"

	"javflow/internal/organizer"
)

// Organizer settings helpers stay separate from crawler settings because they
// shape a different workflow and are often debugged by different symptoms.
//
// Ownership summary:
// 1) translate organizer run options into persisted settings fields
// 2) keep organizer settings persistence separate from payload parsing/execution
// 3) avoid mixing organizer defaults with crawler settings rules
//
// File map for maintainers:
// 1) organizer setting writer contracts
// 2) option-to-settings field writers
// 3) persisted organizer settings save helpers

type organizerSettingWriter func(currentSettings map[string]any, options organizer.RunOptions)

var organizerSettingWriters = []organizerSettingWriter{
	func(currentSettings map[string]any, options organizer.RunOptions) {
		currentSettings["organizerRoot"] = strings.TrimSpace(options.RootPath)
	},
	func(currentSettings map[string]any, options organizer.RunOptions) {
		currentSettings["organizerMinSizeMB"] = options.MinSizeMB
	},
	func(currentSettings map[string]any, options organizer.RunOptions) {
		currentSettings["organizerSuffix"] = strings.TrimSpace(options.Suffix)
	},
	func(currentSettings map[string]any, options organizer.RunOptions) {
		currentSettings["organizerVideoExtensions"] = strings.TrimSpace(options.VideoExtensions)
	},
	func(currentSettings map[string]any, options organizer.RunOptions) {
		currentSettings["organizerAdFileAction"] = options.AdFileAction
	},
	func(currentSettings map[string]any, options organizer.RunOptions) {
		currentSettings["organizerDryRun"] = options.DryRun
	},
	func(currentSettings map[string]any, options organizer.RunOptions) {
		currentSettings["organizerIncludeSubdirectories"] = options.IncludeSubdirectories
	},
	func(currentSettings map[string]any, options organizer.RunOptions) {
		currentSettings["organizerStrictCodeMatch"] = options.StrictExpectedCodes
	},
	func(currentSettings map[string]any, options organizer.RunOptions) {
		currentSettings["organizerCrawlOutput"] = strings.TrimSpace(options.CrawlOutputDir)
	},
	func(currentSettings map[string]any, options organizer.RunOptions) {
		currentSettings["organizerAdDetectionEnabled"] = options.AdDetectionEnabled
	},
	func(currentSettings map[string]any, options organizer.RunOptions) {
		currentSettings["organizerAdThreshold"] = options.AdThreshold
	},
	func(currentSettings map[string]any, options organizer.RunOptions) {
		currentSettings["organizerAdKeywords"] = strings.TrimSpace(options.AdKeywords)
	},
	func(currentSettings map[string]any, options organizer.RunOptions) {
		currentSettings["organizerAdModelType"] = options.AdModelType
	},
	func(currentSettings map[string]any, options organizer.RunOptions) {
		currentSettings["organizerBatchDelete"] = options.BatchDelete
	},
	func(currentSettings map[string]any, options organizer.RunOptions) {
		currentSettings["organizerDeleteIntervalMs"] = options.DeleteIntervalMs
	},
	func(currentSettings map[string]any, options organizer.RunOptions) {
		currentSettings["organizerOrganizeIntervalMs"] = options.OrganizeIntervalMs
	},
}

func applyOrganizerSettings(currentSettings map[string]any, options organizer.RunOptions) {
	for _, writer := range organizerSettingWriters {
		writer(currentSettings, options)
	}
}

func (a *API) saveOrganizerSettings(options organizer.RunOptions) error {
	_, err := a.mutateBridgeSettings(func(currentSettings map[string]any) {
		applyOrganizerSettings(currentSettings, options)
	})
	return err
}

func (a *API) buildOrganizerRunOptions(payload map[string]any) organizer.RunOptions {
	crawlOutputDir := a.resolveOrganizerCrawlOutputDir(payload)
	preloadedExpected := normalizeOrganizerExpectedPayload(payload, crawlOutputDir)
	// Organizer payloads on the current Wails path prefer one explicit
	// preloadedExpected snapshot plus crawlOutputDir as the lazy-read fallback.
	// ExpectedCodes / ExpectedCodeEntries remain compatibility fields only while
	// older renderer or Node compatibility paths are still being trimmed.
	//
	// Practical debugging rule:
	// - if the wrong番号 set was loaded, inspect preloadedExpected first
	// - only if that snapshot is empty should organizer fall back to crawlOutputDir
	// This keeps one crawl snapshot travelling as one payload instead of multiple
	// partially-synchronized fields.
	return organizer.RunOptions{
		RootPath:              nonEmptyString(payload["rootPath"]),
		MinSizeMB:             intValue(payload["minSizeMB"], 100),
		Suffix:                nonEmptyString(payload["suffix"]),
		VideoExtensions:       nonEmptyString(payload["videoExtensions"]),
		AdFileAction:          normalizeAdFileActionBridge(nonEmptyString(payload["adFileAction"])),
		DryRun:                boolValue(payload["dryRun"], false),
		IncludeSubdirectories: boolValue(payload["includeSubdirectories"], true),
		StrictExpectedCodes:   boolValue(payload["strictExpectedCodes"], true),
		CrawlOutputDir:        crawlOutputDir,
		PreloadedExpected:     preloadedExpected,
		AdDetectionEnabled:    boolValue(payload["adDetectionEnabled"], true),
		AdModelType:           normalizeAdModelType(nonEmptyString(payload["adModelType"])),
		AdThreshold:           intValue(payload["adThreshold"], 60),
		AdKeywords:            nonEmptyString(payload["adKeywords"]),
		BatchDelete:           boolValue(payload["batchDelete"], false),
		DeleteIntervalMs:      intValue(payload["deleteIntervalMs"], 500),
		OrganizeIntervalMs:    intValue(payload["organizeIntervalMs"], 500),
	}
}
