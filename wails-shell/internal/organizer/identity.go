package organizer

import "javflow/internal/crawlidentity"

// organizer identity helpers intentionally delegate to the shared crawlidentity
// rules so organizer and crawler keep one film-code normalization contract.
//
// Do not add organizer-local code normalization here unless the shared crawler
// identity contract is intentionally being changed for every consumer.
//
// Ownership summary:
// 1) delegate organizer film identity logic to shared crawlidentity rules
// 2) keep organizer and crawler on one normalization contract
// 3) avoid organizer-local identity drift
//
// File map for maintainers:
// 1) organizer-to-shared-identity delegation helpers
func normalizeFilmID(rawValue string) string {
	return crawlidentity.NormalizeFilmID(rawValue)
}

func extractFilmID(value string) string {
	return crawlidentity.ExtractFilmID(value)
}
