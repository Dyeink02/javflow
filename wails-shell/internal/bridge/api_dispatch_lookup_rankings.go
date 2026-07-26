package bridge

import "fmt"

// Lookup ranking commands stay separate from target resolution so ranking/cache
// regressions do not get mixed into actress target parsing issues.
//
// Ranking rule:
// this lane returns ranking/read-cache data only. Any future command that
// changes crawler, subscription, or organizer state should not be added here
// just because the UI entrypoint starts from the ranking page.
//
// Ownership summary:
// 1) route ranking-only lookup commands
// 2) keep ranking/read-cache queries separate from target lookup and mutations
// 3) centralize ranking dispatch away from the broader lookup branch
//
// File map for maintainers:
// 1) ranking command dispatcher
// 2) ranking query branch routing
func (a *API) handleLookupRankingCommand(command string, payload map[string]any) (string, bool, error) {
	switch command {
	case "app:get-actress-rankings":
		if a.lookup.actressRanking == nil {
			return "", true, fmt.Errorf("actress ranking service is not initialized")
		}
		rankings, err := a.lookup.actressRanking.GetActressRankings(a.buildActressRankingOptions(payload))
		if err != nil {
			return "", true, err
		}
		result, err := marshalResult(rankings)
		return result, true, err
	}

	return "", false, nil
}
