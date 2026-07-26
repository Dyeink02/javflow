// Package crawlrunner owns the current Go-native crawler runtime.
//
// This file owns small runner-only adapters shared across fetch, parsing,
// identity, and task-state helpers.
//
// Ownership summary:
// 1) provide small runner-local adapters across fetch/identity/task-state helpers
// 2) keep repeated normalization glue out of main runner orchestration files
// 3) prevent small helper duplication from spreading across phases
//
// File map for maintainers:
// 1) link/id normalization helpers
// 2) fetch/request conversion helpers
// 3) task-state/result glue helpers
package crawlrunner

import (
	"context"
	"net/url"
	"strings"
	"time"

	"javflow/internal/crawlfetch"
	"javflow/internal/crawlidentity"
	"javflow/internal/crawlindex"
	"javflow/internal/crawlparse"
	"javflow/internal/crawlrequest"
	"javflow/internal/crawltaskstate"
)

// Keep these helpers thin; if one grows into policy logic, it belongs in the
// dedicated runner phase or task-state layer instead.

func normalizeDetailLink(link string) string {
	return crawlindex.NormalizeDetailLink(link)
}

func getDetailItemIDFromLink(link string) string {
	return crawlindex.GetDetailItemID(link)
}

func extractFilmIDFromLink(link string) string {
	id := crawlidentity.ExtractFilmID(link)
	if id == "" {
		return crawlidentity.ExtractFilmID(normalizeSourceLink(link))
	}
	return id
}

func normalizeSourceLink(link string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(link), "/")
	if parsed, err := url.Parse(trimmed); err == nil && parsed.Path != "" {
		segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(segments) > 0 {
			return strings.ToLower(segments[len(segments)-1])
		}
	}
	return strings.ToLower(trimmed)
}

func mergePageLinks(existing []string, incoming []string) []string {
	return crawlindex.MergePageLinks(existing, incoming)
}

func analyzePageDuplicates(links []string) crawlindex.PageLinkDuplicateAnalysis {
	return crawlindex.AnalyzePageLinkDuplicates(links)
}

func buildIndexPageURL(baseURL string, search string, searchURL string, pageNumber int) string {
	return crawlindex.BuildIndexPageURL(baseURL, search, searchURL, pageNumber)
}

func inferTotalPages(limit int, expected *int) int {
	return crawlindex.GetInferredTotalPages(limit, expected)
}

func resolveIndexTargetPageState(page int, configuredPages int, limit int, expected *int) crawlindex.IndexTargetPageState {
	return crawlindex.ResolveIndexTargetPageState(page, configuredPages, limit, expected)
}

func getExpectedCountForPage(page int, targetPages int, limit int, expected *int) *int {
	return crawlindex.GetExpectedItemCountForPage(page, targetPages, limit, expected)
}

func resolveIndexQueueLimit(limit int, queued int, newLinks int) crawlindex.IndexQueueLimitDecision {
	return crawlindex.ResolveIndexQueueLimitDecision(limit, queued, newLinks)
}

func getTrackedPageLinks(links []string, limit int, expectedCount int) []string {
	return crawlindex.GetTrackedPageLinks(links, limit, expectedCount)
}

func intPtr(v int) *int {
	return &v
}

// BindFetchService wires fetch functions into the runner without importing the
// bridge/service layer into the crawl core.
func BindFetchService(r *Runner, fetchService *crawlfetch.Service) {
	// Runner owns orchestration and recovery decisions; crawlfetch owns HTTP,
	// cookie, proxy, and parsing entry points. Binding by functions keeps the
	// runner testable with fixture fetchers and avoids importing bridge/service
	// wiring into the crawl core.
	r.SetFetchFuncs(
		func(ctx context.Context, baseURL string, search string, pageNumber int) ([]string, crawlrequest.PageResponse, error) {
			result, err := fetchService.FetchIndexPage(ctx, crawlfetch.IndexPageOptions{
				BaseURL:    baseURL,
				Search:     search,
				PageNumber: pageNumber,
			})
			if err != nil {
				return nil, crawlrequest.PageResponse{}, err
			}
			return result.Links, result.Response, nil
		},
		func(ctx context.Context, detailURL string) (crawlparse.Metadata, crawlrequest.PageResponse, error) {
			result, err := fetchService.FetchDetail(ctx, detailURL, "")
			if err != nil {
				return crawlparse.Metadata{}, crawlrequest.PageResponse{}, err
			}
			return result.Metadata, result.Response, nil
		},
	)

	r.createFetchClient = func(proxy string, timeout time.Duration) (*crawlrequest.Client, error) {
		if fetchService != nil && fetchService.Client() != nil {
			opts := fetchService.Client().CloneOptions()
			if strings.TrimSpace(proxy) != "" {
				opts.Proxy = proxy
			}
			if timeout > 0 {
				opts.Timeout = timeout
			}
			return crawlrequest.NewClient(opts)
		}
		return crawlrequest.NewClient(crawlrequest.PageRequestOptions{
			Proxy:   proxy,
			Timeout: timeout,
		})
	}
}

// convertRestoredPageAudits converts persisted audit rows back to runner view.
func convertRestoredPageAudits(records []crawltaskstate.PageAuditRecord) []PageAudit {
	result := make([]PageAudit, 0, len(records))
	for _, record := range records {
		result = append(result, PageAudit{
			PageNumber:       record.PageNumber,
			URL:              record.URL,
			ExpectedCount:    record.ExpectedCount,
			ActualCount:      record.ActualCount,
			RetryCount:       record.RetryCount,
			ValidationPassed: record.ValidationPassed,
			ConfidenceScore:  int(record.ConfidenceScore),
			Confidence:       record.Confidence,
			Reason:           record.Reason,
			UpdatedAt:        record.UpdatedAt,
		})
	}
	return result
}
