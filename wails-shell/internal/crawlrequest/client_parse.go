package crawlrequest

import (
	"context"
	"fmt"

	"javflow/internal/crawlparse"
)

// client_parse.go owns the thin fetch-plus-parse adapters built on top of the
// lower-level HTTP page client.
//
// Ownership summary:
// 1) compose lower-level page fetches with parse helpers
// 2) provide thin client-facing fetch-plus-parse adapters
// 3) keep fetch/parse composition separate from raw request and parser layers
//
// File map for maintainers:
// 1) index/detail fetch-plus-parse adapters
// 2) page usability guard usage for parser callers
// 3) normalized fetch result handoff into crawlparse

func (c *Client) FetchIndexPageLinks(ctx context.Context, targetURL string, cookieOverride string) ([]string, PageResponse, error) {
	response, err := c.GetPage(ctx, targetURL, cookieOverride)
	if err != nil {
		return nil, PageResponse{}, err
	}
	if !IsUsablePageResponse(response) {
		return nil, response, fmt.Errorf("%s", DescribePageFallbackReason(&response))
	}
	parsed := crawlparse.ParsePageLinksWithDiagnostics(response.Body)
	response.IndexRawLinkCount = parsed.RawLinkCount
	response.IndexDuplicateCount = parsed.DuplicateEntryCount
	response.IndexDuplicateItemIDs = append([]string(nil), parsed.DuplicateItemIDs...)
	return parsed.Links, response, nil
}

func (c *Client) FetchMetadata(ctx context.Context, targetURL string, cookieOverride string) (crawlparse.Metadata, PageResponse, error) {
	response, err := c.GetPage(ctx, targetURL, cookieOverride)
	if err != nil {
		return crawlparse.Metadata{}, PageResponse{}, err
	}
	if !IsUsablePageResponse(response) {
		return crawlparse.Metadata{}, response, fmt.Errorf("%s", DescribePageFallbackReason(&response))
	}
	metadata, err := crawlparse.ParseMetadata(response.Body)
	if err != nil {
		return crawlparse.Metadata{}, response, err
	}
	return metadata, response, nil
}

func (c *Client) FetchFilmData(ctx context.Context, targetURL string, cookieOverride string) (crawlparse.FilmData, PageResponse, error) {
	metadata, response, err := c.FetchMetadata(ctx, targetURL, cookieOverride)
	if err != nil {
		return crawlparse.FilmData{}, response, err
	}
	return crawlparse.ParseFilmData(metadata, targetURL), response, nil
}
