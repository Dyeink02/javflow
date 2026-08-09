package actresslookup

// Ownership summary:
// 1) load one real JAVBus actress work source page
// 2) normalize page counts and work cards for the Actor Atlas bridge
// 3) keep lazy work paging independent from ranking and subscription state
//
// File map for maintainers:
// 1) WorksPage response contract
// 2) verified source-page fetch and total-page calculation
// 3) small numeric normalization helper

import (
	"context"
	"fmt"
	"strings"

	"javflow/internal/contracts/subscriptiontarget"
	"javflow/internal/crawlindex"
)

// WorksPage is the real, source-page-sized result used by Actor Atlas lazy
// paging. It carries the actor count again so the renderer can keep the full
// page count even before every work card has been requested.
type WorksPage struct {
	ActressName string                           `json:"actressName"`
	AllCount    int                              `json:"allCount"`
	TotalPages  int                              `json:"totalPages"`
	Page        int                              `json:"page"`
	Works       []subscriptiontarget.ActressWork `json:"works"`
}

// FetchWorksPage loads one JAVBus actress source page. The caller supplies a
// previously resolved /star/... URL; no arbitrary host is accepted because the
// same verified lookup path performs source validation and challenge fallback.
func (s *Service) FetchWorksPage(ctx context.Context, targetURL string, page int, proxyValue string) (WorksPage, error) {
	if s == nil {
		return WorksPage{}, fmt.Errorf("actress lookup service is not initialized")
	}
	page = maxInt(page, 1)
	pageURL := crawlindex.BuildIndexPageURL(strings.TrimSpace(targetURL), "", "", page)
	if pageURL == "" {
		return WorksPage{}, fmt.Errorf("actress target URL is empty")
	}
	body, resolved, err := s.fetchLookupPage(ctx, pageURL, proxyValue)
	if err != nil {
		return WorksPage{}, err
	}
	parsed, err := parseStarPage(body, resolved)
	if err != nil {
		return WorksPage{}, err
	}
	if !isUsableStarPage(parsed) {
		return WorksPage{}, fmt.Errorf("JAVBus actress page %d returned no usable works", page)
	}
	totalPages := 1
	if parsed.AllCount > 0 && parsed.ItemsPerPage > 0 {
		totalPages = (parsed.AllCount + parsed.ItemsPerPage - 1) / parsed.ItemsPerPage
	}
	return WorksPage{ActressName: parsed.ActressName, AllCount: parsed.AllCount, TotalPages: totalPages, Page: page, Works: parsed.Works}, nil
}

func maxInt(value, minimum int) int {
	if value < minimum {
		return minimum
	}
	return value
}
