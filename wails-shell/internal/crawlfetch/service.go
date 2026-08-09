// Package crawlfetch wraps index, detail, and magnet fetch operations for the Go crawl path.
//
// Ownership summary:
// 1) compose request, index parsing, and detail parsing into one fetch facade
// 2) expose Go crawl fetch operations behind one runner-facing service
// 3) keep fetch orchestration separate from runner state management
//
// File map for maintainers:
// 1) fetch facade and options DTOs
// 2) index/detail/magnet fetch entrypoints
// 3) request client construction and option normalization
package crawlfetch

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"javflow/internal/crawlindex"
	"javflow/internal/crawlparse"
	"javflow/internal/crawlrequest"
)

// Service is the thin facade that composes request, index parsing, and detail
// parsing for the Go crawl path.
type Service struct {
	client        *crawlrequest.Client
	options       crawlrequest.PageRequestOptions
	antiBlockURLs []string
}

// ServiceOptions is the bridge/runner-facing fetch configuration contract.
type ServiceOptions struct {
	Headers           map[string]string
	ConfigCookie      string
	CloudflareCookies string
	Proxy             string
	Timeout           time.Duration
	UserAgent         string
	RetryCount        int
	RetryDelay        time.Duration
	AntiBlockURLs     []string
}

// IndexPageOptions is the narrow input for one index-page fetch attempt.
type IndexPageOptions struct {
	BaseURL        string
	Search         string
	SearchURL      string
	PageNumber     int
	CookieOverride string
}

// IndexPageResult and DetailResult are the normalized fetch-layer outputs before
// runner/quality code adds queue or reconciliation semantics.
type IndexPageResult struct {
	URL      string                    `json:"url"`
	Links    []string                  `json:"links"`
	Response crawlrequest.PageResponse `json:"response"`
}

type DetailResult struct {
	URL      string                    `json:"url"`
	Metadata crawlparse.Metadata       `json:"metadata"`
	FilmData crawlparse.FilmData       `json:"filmData"`
	Response crawlrequest.PageResponse `json:"response"`
}

// FetchLookupHTML exposes the already-configured page client to read-only
// actor lookup. It deliberately returns only a normalized document snapshot;
// actor lookup keeps ownership of its own search and profile parsing.
//
// The method reuses the crawler's verified browser/age-check session instead
// of creating another challenge-bypass implementation for actor search.
func (s *Service) FetchLookupHTML(ctx context.Context, targetURL string) (string, string, error) {
	return s.FetchLookupHTMLWithProxy(ctx, targetURL, "")
}

// FetchLookupHTMLWithProxy reads an actress lookup page through the crawler's
// established verification recovery while honoring a proxy selected by the
// caller. Actress Atlas can change its proxy after the app starts, so reusing
// the startup client here would silently send the verification request through
// an outdated route.
func (s *Service) FetchLookupHTMLWithProxy(ctx context.Context, targetURL string, proxyValue string) (string, string, error) {
	if s == nil || s.client == nil {
		return "", "", fmt.Errorf("抓取页面客户端未初始化")
	}
	client := s.client
	if strings.TrimSpace(proxyValue) != "" && strings.TrimSpace(proxyValue) != strings.TrimSpace(s.options.Proxy) {
		options := s.options
		options.Proxy = strings.TrimSpace(proxyValue)
		var err error
		client, err = crawlrequest.NewClient(options)
		if err != nil {
			return "", "", err
		}
	}
	response, err := client.GetPage(ctx, targetURL, "")
	if err != nil {
		return "", "", err
	}
	return response.Body, response.URL, nil
}

func NewService(options ServiceOptions) (*Service, error) {
	requestOptions := crawlrequest.PageRequestOptions{
		Headers:           options.Headers,
		ConfigCookie:      options.ConfigCookie,
		CloudflareCookies: options.CloudflareCookies,
		Proxy:             options.Proxy,
		Timeout:           options.Timeout,
		UserAgent:         options.UserAgent,
		RetryCount:        options.RetryCount,
		RetryDelay:        options.RetryDelay,
	}
	client, err := crawlrequest.NewClient(requestOptions)
	if err != nil {
		return nil, err
	}
	return &Service{
		client:        client,
		options:       requestOptions,
		antiBlockURLs: uniqueStrings(options.AntiBlockURLs),
	}, nil
}

// FetchIndexPage fetches one listing page and returns already parsed detail
// links together with the raw response. When the configured base URL fails,
// persisted anti-block mirror bases are tried in order before giving up.
func (s *Service) FetchIndexPage(ctx context.Context, options IndexPageOptions) (IndexPageResult, error) {
	bases := s.baseURLList(options.BaseURL)
	var lastErr error
	for _, baseURL := range bases {
		pageURL := crawlindex.BuildIndexPageURL(baseURL, options.Search, options.SearchURL, options.PageNumber)
		links, response, err := s.client.FetchIndexPageLinks(ctx, pageURL, options.CookieOverride)
		if err == nil {
			return IndexPageResult{
				URL:      pageURL,
				Links:    links,
				Response: response,
			}, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = context.Canceled
	}
	return IndexPageResult{}, lastErr
}

// FetchDetail fetches one detail page and projects it into metadata plus the
// persisted filmData artifact shape. Anti-block mirror bases are used when the
// original detail URL cannot be reached.
func (s *Service) FetchDetail(ctx context.Context, detailURL string, cookieOverride string) (DetailResult, error) {
	urls := s.detailURLList(detailURL)
	var lastErr error
	for _, targetURL := range urls {
		metadata, response, err := s.client.FetchMetadata(ctx, targetURL, cookieOverride)
		if err == nil {
			return DetailResult{
				URL:      targetURL,
				Metadata: metadata,
				FilmData: crawlparse.ParseFilmData(metadata, targetURL),
				Response: response,
			}, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = context.Canceled
	}
	return DetailResult{}, lastErr
}

// Client exposes the underlying request client for specialized fetch paths that
// still belong to the crawl fetch lane.
func (s *Service) Client() *crawlrequest.Client {
	if s == nil {
		return nil
	}
	return s.client
}

// Close releases resources held by the fetch service, including any shared
// browser allocator used for the chromedp fallback path.
func (s *Service) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}

// baseURLList returns the primary base URL followed by unique anti-block
// mirror bases, preserving order and avoiding duplicates.
func (s *Service) baseURLList(primary string) []string {
	primary = strings.TrimRight(strings.TrimSpace(primary), "/")
	if primary == "" {
		primary = "https://www.javbus.com"
	}
	seen := map[string]struct{}{primary: {}}
	result := []string{primary}
	for _, item := range s.antiBlockURLs {
		item = strings.TrimRight(strings.TrimSpace(item), "/")
		if item == "" || item == primary {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

// detailURLList returns the original detail URL followed by variants where the
// scheme/host are replaced by each anti-block mirror base.
func (s *Service) detailURLList(primary string) []string {
	primary = strings.TrimSpace(primary)
	if primary == "" {
		return nil
	}
	parsed, err := url.Parse(primary)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return []string{primary}
	}
	originalBase := parsed.Scheme + "://" + parsed.Host
	bases := s.baseURLList(originalBase)
	result := make([]string, 0, len(bases))
	for _, base := range bases {
		parsedBase, err := url.Parse(base)
		if err != nil {
			continue
		}
		variant := *parsed
		variant.Scheme = parsedBase.Scheme
		variant.Host = parsedBase.Host
		result = append(result, variant.String())
	}
	return result
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, item := range values {
		text := strings.TrimSpace(item)
		if text == "" {
			continue
		}
		if _, ok := seen[text]; ok {
			continue
		}
		seen[text] = struct{}{}
		result = append(result, text)
	}
	return result
}
