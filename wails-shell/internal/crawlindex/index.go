package crawlindex

import (
	"net/url"
	"sort"
	"strconv"
	"strings"

	"javflow/internal/crawlidentity"
)

// Package crawlindex owns index-page URL planning and link-level bookkeeping
// before detail-page fetch work begins.
//
// Ownership summary:
// 1) build normalized index-page URLs and target-page decisions
// 2) analyze/normalize detail links discovered from index pages
// 3) keep index planning/bookkeeping separate from network fetch and parsing
//
// File map for maintainers:
// 1) index URL and target-page DTOs
// 2) link normalization and duplicate-analysis helpers
// 3) queue limit and page boundary decision helpers

// BuildIndexPageURLOptions is the normalized input shape for index URL planning.
type BuildIndexPageURLOptions struct {
	BaseURL              string
	Search               string
	SearchURL            string
	PageNumber           int
	ExpectedItemsPerPage int
}

// PageLinkDuplicateAnalysis summarizes duplicate identities seen on one index
// page after link normalization.
type PageLinkDuplicateAnalysis struct {
	UniqueCount    int      `json:"uniqueCount"`
	DuplicateCount int      `json:"duplicateCount"`
	DuplicateIDs   []string `json:"duplicateIds"`
}

// IndexTargetPageState captures the current index-page target boundary derived
// from configured pages and inferred film limits.
type IndexTargetPageState struct {
	InferredTotalPages int  `json:"inferredTotalPages"`
	TargetTotalPages   int  `json:"targetTotalPages"`
	IsLastTargetPage   bool `json:"isLastTargetPage"`
}

// IndexQueueLimitDecision answers how many new detail links may be queued under
// the current film-limit budget.
type IndexQueueLimitDecision struct {
	QueueCount            int  `json:"queueCount"`
	RemainingSlots        int  `json:"remainingSlots"`
	ShouldStopBeforeQueue bool `json:"shouldStopBeforeQueue"`
	ShouldStopAfterQueue  bool `json:"shouldStopAfterQueue"`
}

// IndexProcessingDecision is the high-level control outcome after processing
// one index page.
type IndexProcessingDecision struct {
	Action             string `json:"action"`
	ShouldAdvancePage  bool   `json:"shouldAdvancePage"`
	ShouldStopIndexing bool   `json:"shouldStopIndexing"`
}

type IndexProcessingDecisionInput struct {
	CurrentPage      int
	TargetTotalPages int
	ExpectedCount    *int
	LinksCount       int
	NewLinksCount    int
	ResumeExisting   bool
	FilmLimit        int
	FilmsQueued      int
}

// BuildIndexPageURL owns one canonical URL-planning rule for base, search, and
// category/star/listing pages.
func BuildIndexPageURL(baseURL string, search string, searchURL string, pageNumber int) string {
	trimmedBaseURL := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	pagePart := ""
	if pageNumber > 1 {
		pagePart = "/" + strconv.Itoa(pageNumber)
	}

	if strings.TrimSpace(search) != "" {
		trimmedSearchURL := strings.Trim(strings.TrimSpace(searchURL), "/")
		if trimmedSearchURL != "" {
			return trimmedBaseURL + "/" + trimmedSearchURL + "/" + url.PathEscape(search) + pagePart
		}
		return trimmedBaseURL + "/" + url.PathEscape(search) + pagePart
	}

	parsedURL, err := url.Parse(trimmedBaseURL)
	if err != nil {
		return trimmedBaseURL + pagePart
	}

	normalizedPath := strings.TrimRight(parsedURL.Path, "/")
	if normalizedPath == "" {
		if pageNumber <= 1 {
			return trimmedBaseURL
		}
		return trimmedBaseURL + "/page/" + strconv.Itoa(pageNumber)
	}

	for _, marker := range []string{"/genre/", "/search/", "/star/", "/studio/", "/label/", "/director/", "/series/"} {
		if strings.Contains(normalizedPath, marker) {
			return trimmedBaseURL + pagePart
		}
	}

	return trimmedBaseURL + pagePart
}

// NormalizeDetailLink keeps detail-link dedupe stable across minor URL-shape
// differences.
func NormalizeDetailLink(link string) string {
	raw := strings.TrimSpace(link)
	if raw == "" {
		return ""
	}

	parsedURL, err := url.Parse(raw)
	if err == nil && parsedURL.Scheme != "" && parsedURL.Host != "" {
		normalizedPath := strings.TrimRight(parsedURL.Path, "/")
		if normalizedPath == "" {
			normalizedPath = "/"
		}
		normalizedQuery := ""
		if parsedURL.RawQuery != "" {
			normalizedQuery = "?" + parsedURL.RawQuery
		}
		return strings.ToLower(parsedURL.Scheme + "://" + parsedURL.Host + normalizedPath + normalizedQuery)
	}

	return strings.ToLower(strings.TrimRight(raw, "/"))
}

// GetDetailItemID prefers film-code identity and falls back to normalized URL
// identity when a code is unavailable.
func GetDetailItemID(value string) string {
	if filmID := crawlidentity.ExtractFilmID(value); filmID != "" {
		return filmID
	}
	if normalizedLink := NormalizeDetailLink(value); normalizedLink != "" {
		return normalizedLink
	}
	return value
}

// MergePageLinks is the shared dedupe merge used before detail queueing.
func MergePageLinks(existingLinks []string, incomingLinks []string) []string {
	result := make([]string, 0, len(existingLinks)+len(incomingLinks))
	seen := map[string]struct{}{}

	for _, link := range append(append([]string{}, existingLinks...), incomingLinks...) {
		identity := GetDetailItemID(link)
		if identity == "" {
			continue
		}
		if _, exists := seen[identity]; exists {
			continue
		}
		seen[identity] = struct{}{}
		result = append(result, link)
	}

	return result
}

func AnalyzePageLinkDuplicates(links []string) PageLinkDuplicateAnalysis {
	counts := map[string]int{}
	for _, link := range links {
		identity := GetDetailItemID(link)
		if identity == "" {
			continue
		}
		counts[identity]++
	}

	duplicateIDs := make([]string, 0)
	duplicateCount := 0
	for identity, count := range counts {
		if count > 1 {
			duplicateIDs = append(duplicateIDs, identity)
			duplicateCount += count - 1
		}
	}
	sort.Strings(duplicateIDs)

	return PageLinkDuplicateAnalysis{
		UniqueCount:    len(counts),
		DuplicateCount: duplicateCount,
		DuplicateIDs:   duplicateIDs,
	}
}

func GetExpectedItemCountForPage(currentPage int, targetTotalPages int, filmLimit int, expectedItemsPerPage *int) *int {
	if expectedItemsPerPage == nil {
		return nil
	}

	value := *expectedItemsPerPage
	if targetTotalPages > 0 && currentPage < targetTotalPages {
		return &value
	}

	if targetTotalPages > 0 && currentPage == targetTotalPages && filmLimit > 0 {
		remainder := filmLimit % value
		if remainder == 0 {
			return &value
		}
		return &remainder
	}

	return &value
}

func GetInferredTotalPages(filmLimit int, expectedItemsPerPage *int) int {
	if expectedItemsPerPage == nil || *expectedItemsPerPage <= 0 {
		return 0
	}
	if filmLimit > 0 {
		return (filmLimit + *expectedItemsPerPage - 1) / *expectedItemsPerPage
	}
	return 0
}

// ResolveIndexTargetPageState keeps index-loop page-budget calculation in one
// place so runner/controller code does not rederive it differently.
func ResolveIndexTargetPageState(currentPage int, configuredTotalPages int, filmLimit int, expectedItemsPerPage *int) IndexTargetPageState {
	inferredTotalPages := GetInferredTotalPages(filmLimit, expectedItemsPerPage)
	targetTotalPages := inferredTotalPages
	if configuredTotalPages > 0 {
		targetTotalPages = configuredTotalPages
	}

	return IndexTargetPageState{
		InferredTotalPages: inferredTotalPages,
		TargetTotalPages:   targetTotalPages,
		IsLastTargetPage:   targetTotalPages > 0 && currentPage >= targetTotalPages,
	}
}

// ResolveIndexQueueLimitDecision turns the global film limit into a per-page
// queue allowance decision.
func ResolveIndexQueueLimitDecision(filmLimit int, filmsQueued int, newLinksCount int) IndexQueueLimitDecision {
	if filmLimit <= 0 {
		return IndexQueueLimitDecision{
			QueueCount:            newLinksCount,
			RemainingSlots:        int(^uint(0) >> 1),
			ShouldStopBeforeQueue: false,
			ShouldStopAfterQueue:  false,
		}
	}

	remainingSlots := filmLimit - filmsQueued
	if remainingSlots <= 0 {
		return IndexQueueLimitDecision{
			QueueCount:            0,
			RemainingSlots:        0,
			ShouldStopBeforeQueue: true,
			ShouldStopAfterQueue:  true,
		}
	}

	queueCount := newLinksCount
	if queueCount > remainingSlots {
		queueCount = remainingSlots
	}

	return IndexQueueLimitDecision{
		QueueCount:            queueCount,
		RemainingSlots:        remainingSlots,
		ShouldStopBeforeQueue: false,
		ShouldStopAfterQueue:  filmsQueued+queueCount >= filmLimit,
	}
}

// ResolveIndexProcessingDecision is the single post-page control decision point
// for index iteration.
func ResolveIndexProcessingDecision(input IndexProcessingDecisionInput) IndexProcessingDecision {
	if input.LinksCount == 0 {
		canContinueAfterGap := input.ExpectedCount != nil && input.TargetTotalPages > 0 && input.CurrentPage < input.TargetTotalPages
		if canContinueAfterGap {
			return IndexProcessingDecision{
				Action:             "continue_after_gap",
				ShouldAdvancePage:  true,
				ShouldStopIndexing: false,
			}
		}

		return IndexProcessingDecision{
			Action:             "stop_empty_page",
			ShouldAdvancePage:  false,
			ShouldStopIndexing: true,
		}
	}

	if input.NewLinksCount == 0 {
		if input.ResumeExisting {
			return IndexProcessingDecision{
				Action:             "continue_resume_completed_page",
				ShouldAdvancePage:  true,
				ShouldStopIndexing: false,
			}
		}

		return IndexProcessingDecision{
			Action:             "stop_no_new_links",
			ShouldAdvancePage:  false,
			ShouldStopIndexing: true,
		}
	}

	if input.FilmLimit > 0 && input.FilmsQueued >= input.FilmLimit {
		return IndexProcessingDecision{
			Action:             "stop_limit_reached",
			ShouldAdvancePage:  true,
			ShouldStopIndexing: true,
		}
	}

	if input.TargetTotalPages > 0 && input.CurrentPage >= input.TargetTotalPages {
		return IndexProcessingDecision{
			Action:             "stop_target_page_reached",
			ShouldAdvancePage:  true,
			ShouldStopIndexing: true,
		}
	}

	return IndexProcessingDecision{
		Action:             "continue",
		ShouldAdvancePage:  true,
		ShouldStopIndexing: false,
	}
}

func ShouldWarnSparseIndexPage(shouldStopIndexing bool, expectedItemsPerPage *int, isLastTargetPage bool, linksCount int) bool {
	if shouldStopIndexing || expectedItemsPerPage == nil || isLastTargetPage {
		return false
	}
	return linksCount < *expectedItemsPerPage
}

func GetTrackedPageLinks(links []string, targetLimit int, expectedCount int) []string {
	if targetLimit <= 0 {
		return append([]string{}, links...)
	}

	remainingSlots := targetLimit - expectedCount
	if remainingSlots <= 0 {
		return []string{}
	}
	if remainingSlots >= len(links) {
		return append([]string{}, links...)
	}
	return append([]string{}, links[:remainingSlots]...)
}
