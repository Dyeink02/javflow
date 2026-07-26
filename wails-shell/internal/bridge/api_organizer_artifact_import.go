package bridge

// loadOrganizerCrawlFilmCodesResult is the organizer-side artifact import
// boundary. It reads persisted crawl output and prepares expected-code inputs
// for organizer workflows, but it does not start organizer execution.
//
// Boundary rule:
// - organizer accepts persisted crawl artifacts here
// - organizer execution should not require a live crawler runtime/session
//
// Ownership summary:
// 1) expose organizer-side crawl artifact import entrypoint
// 2) keep organizer expected-code loading separate from organizer execution
// 3) preserve the bridge boundary around organizer artifact-path resolution
//
// File map for maintainers:
// 1) organizer artifact-import result helper
// 2) call-site split note for shared crawl-input resolution
func (a *API) loadOrganizerCrawlFilmCodesResult(payload map[string]any) (string, error) {
	loaded, err := a.organizer.organizerService.LoadCrawlFilmCodes(a.resolveOrganizerCrawlOutputDir(payload))
	if err != nil {
		return "", err
	}
	return marshalResult(loaded)
}

// resolveOrganizerCrawlOutputDir stays in api_organizer_crawl_input.go so all
// organizer artifact-import entrypoints share one alias-priority rule.
// Troubleshooting should therefore split cleanly into:
// 1) wrong artifact path resolution at the bridge boundary
// 2) wrong expected-code loading inside organizer service
