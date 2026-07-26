package bridge

// handleLookupCommand groups actress lookup/ranking reads so those failures do
// not get mixed into crawl-control or organizer command handling.
//
// Boundary rule:
// lookup remains read-oriented. If a future command mutates durable
// subscription/organizer/crawl state, it should move to that owning domain
// instead of growing the lookup surface.
//
// Ownership summary:
// 1) aggregate lookup command dispatch across rankings and target inspection
// 2) keep lookup routing separate from crawl/organizer/subscription domains
// 3) preserve the read-oriented lookup surface at the dispatch layer
//
// File map for maintainers:
// 1) lookup command aggregate dispatcher
// 2) ranking vs target branch routing
func (a *API) handleLookupCommand(command string, payload map[string]any) (string, bool, error) {
	if result, handled, err := a.handleLookupRankingCommand(command, payload); handled {
		return result, true, err
	}
	if result, handled, err := a.handleLookupTargetCommand(command, payload); handled {
		return result, true, err
	}

	return "", false, nil
}
