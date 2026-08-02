package bridge

import "javflow/internal/organizer"

// handleOrganizerCommand keeps organizer-only flows out of the crawler and
// runtime dispatch groups.
// Organizer bridge entrypoints are split into:
// 1) crawl-artifact import
// 2) organizer execution
//
// Ownership summary:
// 1) route organizer-only bridge commands
// 2) preserve the split between crawl-artifact import and organizer execution
// 3) keep organizer dispatch separate from crawler/runtime command groups
//
// File map for maintainers:
// 1) organizer command dispatcher
// 2) import/run branch routing
func (a *API) handleOrganizerCommand(command string, payload map[string]any) (string, bool, error) {
	// Organizer dispatch is intentionally small: bridge routing only. Artifact
	// import and organizer execution semantics stay in dedicated helpers/services.
	switch command {
	case "app:discover-organizer-codes":
		options := organizer.DiscoverOptions{
			RootPath:              nonEmptyString(payload["rootPath"]),
			IncludeSubdirectories: boolValue(payload["includeSubdirectories"], true),
			VideoExtensions:       nonEmptyString(payload["videoExtensions"]),
		}
		result, err := a.organizer.organizerService.DiscoverLocalCodes(options)
		if err != nil {
			return "", true, err
		}
		encoded, err := marshalResult(result)
		return encoded, true, err

	case "app:load-crawl-film-codes":
		result, err := a.loadOrganizerCrawlFilmCodesResult(payload)
		return result, true, err

	case "app:run-organizer":
		runResult, err := a.runOrganizer(payload)
		if err != nil {
			return "", true, err
		}
		result, err := marshalResult(runResult)
		return result, true, err

	case "app:rescue-organizer-names":
		options := a.buildOrganizerRunOptions(payload)
		rescueResult, err := a.organizer.organizerService.RescueNames(options)
		if err != nil {
			return "", true, err
		}
		result, err := marshalResult(rescueResult)
		return result, true, err
	}

	return "", false, nil
}
