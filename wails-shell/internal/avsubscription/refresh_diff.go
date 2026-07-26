package avsubscription

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"javflow/internal/crawlindex"
	"javflow/internal/crawlparse"
	"javflow/internal/crawlrequest"
)

// refresh_diff.go owns subscription-side baseline-diff refresh logic.
//
// Ownership summary:
// 1) fetch lightweight actress listing pages for subscription refresh only
// 2) compare remote page codes with persisted baselineCodes
// 3) keep "detect updates" independent from full crawler runtime/orchestration
//
// File map for maintainers:
// 1) RefreshDiffResult and per-page diff types.
// 2) Actress listing fetch and baseline comparison helpers.
// 3) Public refresh entry point used by subscription service.
//
// Boundary reminder:
// this file only decides what looks newly published for a subscription target.
// Full detail-page crawling and magnet persistence remain in subcrawl / crawler
// output paths.

type RefreshDiffResult struct {
	CurrentCount   int
	ItemsPerPage   int
	TotalPages     int
	LatestItemURL  string
	NewCodes       []string
	ObservedCodes  []string
	ObservedDetail []string
}

func BuildRefreshDiff(ctx context.Context, item Subscription, proxyValue string) (RefreshDiffResult, error) {
	if strings.TrimSpace(item.CrawlURL) == "" {
		return RefreshDiffResult{}, fmt.Errorf("subscription target URL is empty")
	}

	timeout := 20 * time.Second
	client, err := crawlrequest.NewClient(crawlrequest.PageRequestOptions{
		Proxy:   proxyValue,
		Timeout: timeout,
	})
	if err != nil {
		return RefreshDiffResult{}, err
	}

	baseURL := strings.TrimSpace(item.CrawlURL)
	pageURL := crawlindex.BuildIndexPageURL(baseURL, "", "", 1)
	resp, err := client.GetPage(ctx, pageURL, "")
	if err != nil {
		return RefreshDiffResult{}, err
	}

	links := crawlparse.ParsePageLinks(resp.Body)
	normalizedLinks := make([]string, 0, len(links))
	observedCodes := make([]string, 0, len(links))
	for _, link := range links {
		normalized := crawlindex.NormalizeDetailLink(link)
		if normalized == "" {
			continue
		}
		normalizedLinks = append(normalizedLinks, normalized)
		code := normalizeFilmCode(normalized)
		if code != "" {
			observedCodes = append(observedCodes, code)
		}
	}
	observedCodes = normalizeBaselineCodes(observedCodes)

	baselineSet := make(map[string]struct{}, len(item.BaselineCodes))
	for _, code := range normalizeBaselineCodes(item.BaselineCodes) {
		baselineSet[code] = struct{}{}
	}

	newCodes := make([]string, 0)
	for _, code := range observedCodes {
		if _, exists := baselineSet[code]; exists {
			continue
		}
		newCodes = append(newCodes, code)
	}

	currentCount := maxInt(item.CurrentCount, item.SyncedCount)
	if len(item.BaselineCodes) > 0 && currentCount < len(item.BaselineCodes) {
		currentCount = len(item.BaselineCodes)
	}
	if currentCount < len(item.BaselineCodes)+len(newCodes) {
		currentCount = len(item.BaselineCodes) + len(newCodes)
	}

	itemsPerPage := item.ItemsPerPage
	if itemsPerPage <= 0 {
		itemsPerPage = defaultItemsPerPage
	}
	totalPages := item.TotalPages
	if totalPages <= 0 {
		totalPages = calcPages(maxInt(currentCount, len(observedCodes)), itemsPerPage)
	}
	if totalPages <= 0 {
		totalPages = 1
	}

	latestItemURL := ""
	if len(normalizedLinks) > 0 {
		latestItemURL = normalizedLinks[0]
	}

	sort.Strings(newCodes)
	return RefreshDiffResult{
		CurrentCount:   maxInt(currentCount, len(item.BaselineCodes)+len(newCodes)),
		ItemsPerPage:   itemsPerPage,
		TotalPages:     totalPages,
		LatestItemURL:  latestItemURL,
		NewCodes:       newCodes,
		ObservedCodes:  observedCodes,
		ObservedDetail: normalizedLinks,
	}, nil
}
