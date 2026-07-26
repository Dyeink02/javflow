package bridge

import (
	"context"
	"fmt"

	"javflow/internal/crawlfetch"
)

// Crawl page-test helpers are isolated from runner startup so fetch parsing
// diagnostics can be verified without reading lifecycle or state code.
//
// Ownership summary:
// 1) expose crawl page-test diagnostic commands
// 2) exercise fetch/parser paths without mutating live crawl state
// 3) keep diagnostic probes separate from lifecycle command code
//
// File map for maintainers:
// 1) index/detail page probe entrypoints
// 2) test payload normalization helpers
func (a *API) handleGoCrawlTestIndex(payload map[string]any) (string, error) {
	baseURL := nonEmptyString(payload["baseUrl"])
	if baseURL == "" {
		baseURL = nonEmptyString(payload["base"])
	}
	if baseURL == "" {
		baseURL = "https://www.javbus.com/"
	}
	search := nonEmptyString(payload["search"])
	pageNumber := intValue(payload["pageNumber"], 1)

	result, err := a.crawl.crawlFetch.FetchIndexPage(context.Background(), crawlfetch.IndexPageOptions{
		BaseURL:    baseURL,
		Search:     search,
		PageNumber: pageNumber,
	})
	if err != nil {
		return "", err
	}
	return marshalResult(result)
}

func (a *API) handleGoCrawlTestDetail(payload map[string]any) (string, error) {
	detailURL := nonEmptyString(payload["detailUrl"])
	if detailURL == "" {
		return "", fmt.Errorf("detailUrl is required")
	}

	result, err := a.crawl.crawlFetch.FetchDetail(context.Background(), detailURL, "")
	if err != nil {
		return "", err
	}
	return marshalResult(result)
}
