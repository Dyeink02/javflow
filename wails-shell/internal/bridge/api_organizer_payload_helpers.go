// Package bridge is the Wails command boundary for current desktop features.
//
// This file owns organizer-only payload coercion so bridge request parsing can
// evolve without mixing organizer DTO shaping into generic shared helpers.
//
// Ownership summary:
// 1) decode organizer-specific payload slices/maps from bridge requests
// 2) translate them into organizer DTO contracts
// 3) keep generic string/value coercion separate from organizer semantics
//
// File map for maintainers:
// 1) organizer payload slice/map coercion helpers
// 2) DTO-level magnet/code payload translators
package bridge

import (
	"strings"

	"javflow/internal/organizer"
)

// Organizer payload parsing stays separate from the generic bridge value
// helpers so organizer-only DTO shaping can evolve without bloating the
// shared coercion layer.

// magnetEntriesValue keeps bridge-side payload coercion local to organizer DTO
// shaping and away from generic shared value helpers.
func magnetEntriesValue(value any) []organizer.MagnetEntry {
	rawItems, ok := value.([]any)
	if !ok {
		if typed, typedOk := value.([]organizer.MagnetEntry); typedOk {
			return typed
		}
		return nil
	}
	result := make([]organizer.MagnetEntry, 0, len(rawItems))
	for _, item := range rawItems {
		switch typed := item.(type) {
		case map[string]any:
			link := cleanAnyString(typed["link"])
			if link == "" {
				link = cleanAnyString(typed["magnet"])
			}
			if link == "" {
				continue
			}
			result = append(result, organizer.MagnetEntry{
				Link:        link,
				Size:        cleanAnyString(typed["size"]),
				DisplayName: cleanAnyString(typed["displayName"]),
			})
		case string:
			if link := strings.TrimSpace(typed); link != "" {
				result = append(result, organizer.MagnetEntry{Link: link})
			}
		}
	}
	return result
}

// codeEntriesValue converts bridge payload slices into the organizer-side
// expected-code contract.
func codeEntriesValue(value any) []organizer.CodeEntry {
	rawItems, ok := value.([]any)
	if !ok {
		if typed, typedOk := value.([]organizer.CodeEntry); typedOk {
			return typed
		}
		return nil
	}
	result := make([]organizer.CodeEntry, 0, len(rawItems))
	for _, item := range rawItems {
		entryMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		code := cleanAnyString(entryMap["code"])
		if code == "" {
			continue
		}
		result = append(result, organizer.CodeEntry{
			Code:    code,
			Title:   cleanAnyString(entryMap["title"]),
			Maker:   cleanAnyString(entryMap["maker"]),
			Label:   cleanAnyString(entryMap["label"]),
			Series:  cleanAnyString(entryMap["series"]),
			Magnets: magnetEntriesValue(entryMap["magnets"]),
		})
	}
	return result
}

// preloadedExpectedValue decodes the preferred organizer preload snapshot shape
// before legacy supplements are merged.
func preloadedExpectedValue(value any) organizer.PreloadedExpectedCodes {
	payload, ok := value.(map[string]any)
	if !ok {
		if typed, typedOk := value.(organizer.PreloadedExpectedCodes); typedOk {
			return typed
		}
		return organizer.PreloadedExpectedCodes{}
	}

	return organizer.PreloadedExpectedCodes{
		SourceType:         cleanAnyString(payload["sourceType"]),
		SourcePath:         cleanAnyString(payload["sourcePath"]),
		OutputDir:          cleanAnyString(payload["outputDir"]),
		FilmDataPath:       cleanAnyString(payload["filmDataPath"]),
		OrganizerCodesPath: cleanAnyString(payload["organizerCodesPath"]),
		ActressName:        cleanAnyString(payload["actressName"]),
		TotalRecords:       intValue(payload["totalRecords"], 0),
		CodeCount:          intValue(payload["codeCount"], 0),
		Codes:              stringSliceValue(payload["codes"]),
		CodeEntries:        codeEntriesValue(payload["codeEntries"]),
	}
}

// normalizeOrganizerExpectedPayload collapses organizer crawl-code fields into
// one snapshot before the bridge calls into organizer service code. This keeps
// the Wails path on one main payload shape while older field names remain as a
// compatibility input surface only.
//
// Precedence rule:
//  1. preloadedExpected is the primary snapshot contract
//  2. expectedCodes / expectedCodeEntries are legacy supplements only
//  3. crawlOutputDir is not interpreted here; organizer service decides whether
//     it still needs to lazy-load artifacts from disk
//
// That split matters for debugging because a payload may legally contain both
// an explicit snapshot and older compatibility slices; ComposePreloadedExpectedCodes
// is the only place that should merge them.
func normalizeOrganizerExpectedPayload(payload map[string]any, crawlOutputDir string) organizer.PreloadedExpectedCodes {
	legacyCodes := stringSliceValue(payload["expectedCodes"])
	legacyEntries := codeEntriesValue(payload["expectedCodeEntries"])
	return organizer.ComposePreloadedExpectedCodes(
		preloadedExpectedValue(payload["preloadedExpected"]),
		crawlOutputDir,
		legacyCodes,
		legacyEntries,
	)
}
