// Ownership summary:
//   This file implements domain logic for the javflow backend.
//
// File map for maintainers:
//   1) Domain-specific types and helpers.
//   2) Internal service implementation.
//

package avsubscriptionv2

import (
	"context"
	"fmt"
	"strings"
	"time"

	"javflow/internal/crawlidentity"
	"javflow/internal/crawlindex"
	"javflow/internal/crawlparse"
	"javflow/internal/crawlrequest"
)

type scanLogger func(level string, message string)

func (s *Service) newScanClient(proxyValue string) (*crawlrequest.Client, error) {
	return s.newScanClientWithOptions(ScanRuntimeOptions{Proxy: proxyValue})
}

func (s *Service) newScanClientWithOptions(runtimeOptions ScanRuntimeOptions) (*crawlrequest.Client, error) {
	if s.fetch != nil && s.fetch.Client() != nil {
		opts := s.fetch.Client().CloneOptions()
		if strings.TrimSpace(runtimeOptions.Proxy) != "" {
			opts.Proxy = strings.TrimSpace(runtimeOptions.Proxy)
		}
		if runtimeOptions.Timeout > 0 {
			opts.Timeout = runtimeOptions.Timeout
		}
		if strings.TrimSpace(runtimeOptions.ConfigCookie) != "" {
			opts.ConfigCookie = strings.TrimSpace(runtimeOptions.ConfigCookie)
		}
		if strings.TrimSpace(runtimeOptions.CloudflareCookies) != "" {
			opts.CloudflareCookies = strings.TrimSpace(runtimeOptions.CloudflareCookies)
		}
		if strings.TrimSpace(runtimeOptions.UserAgent) != "" {
			opts.UserAgent = strings.TrimSpace(runtimeOptions.UserAgent)
		}
		if runtimeOptions.Headers != nil {
			opts.Headers = runtimeOptions.Headers
		}
		if runtimeOptions.RetryCount > 0 {
			opts.RetryCount = runtimeOptions.RetryCount
		}
		if runtimeOptions.RetryDelay > 0 {
			opts.RetryDelay = runtimeOptions.RetryDelay
		}
		if opts.Timeout <= 0 {
			opts.Timeout = 25 * time.Second
		}
		return crawlrequest.NewClient(opts)
	}
	return crawlrequest.NewClient(crawlrequest.PageRequestOptions{
		Headers:           runtimeOptions.Headers,
		ConfigCookie:      strings.TrimSpace(runtimeOptions.ConfigCookie),
		CloudflareCookies: strings.TrimSpace(runtimeOptions.CloudflareCookies),
		Proxy:             strings.TrimSpace(runtimeOptions.Proxy),
		Timeout:           firstPositiveDuration(runtimeOptions.Timeout, 25*time.Second),
		UserAgent:         strings.TrimSpace(runtimeOptions.UserAgent),
		RetryCount:        runtimeOptions.RetryCount,
		RetryDelay:        runtimeOptions.RetryDelay,
	})
}

func firstPositiveDuration(values ...time.Duration) time.Duration {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

type scannedPage struct {
	Page        int
	Codes       []string
	Links       []string
	Diagnostics pageScanDiagnostics
}

type pageScanDiagnostics struct {
	PageURL      string
	RawLinkCount int
	StatusCode   int
	ResponseURL  string
}

type scanPageError struct {
	Page         int
	PageURL      string
	Reason       string
	RawLinkCount int
}

func (e scanPageError) Error() string {
	reason := strings.TrimSpace(e.Reason)
	if reason == "" {
		reason = "未解析到作品番号"
	}
	if strings.TrimSpace(e.PageURL) == "" {
		return fmt.Sprintf("第 %d 页%s", e.Page, reason)
	}
	return fmt.Sprintf("第 %d 页%s：%s", e.Page, reason, e.PageURL)
}

func scanIndexPage(ctx context.Context, client *crawlrequest.Client, crawlURL string, preferredBase string, page int) (scannedPage, error) {
	return scanIndexPageWithDiagnostics(ctx, client, crawlURL, preferredBase, page, ScanRuntimeOptions{})
}

func scanIndexPageWithDiagnostics(ctx context.Context, client *crawlrequest.Client, crawlURL string, preferredBase string, page int, runtimeOptions ScanRuntimeOptions) (scannedPage, error) {
	pageURL := crawlindex.BuildIndexPageURL(crawlURL, "", "", page)
	if strings.TrimSpace(pageURL) == "" {
		pageURL = strings.TrimSpace(crawlURL)
	}

	var resp crawlrequest.PageResponse
	var rawLinks []string
	if runtimeOptions.Cloudflare && runtimeOptions.PageFetcher != nil {
		fetched, err := runtimeOptions.PageFetcher(pageURL, runtimeOptions)
		if err != nil {
			return scannedPage{}, err
		}
		resp = crawlrequest.PageResponse{
			URL:        firstNonEmpty(fetched.URL, pageURL),
			StatusCode: fetched.StatusCode,
			Body:       fetched.Body,
		}
		rawLinks = append([]string{}, fetched.Links...)
		if len(rawLinks) == 0 && strings.TrimSpace(resp.Body) != "" {
			rawLinks = crawlparse.ParsePageLinks(resp.Body)
		}
	} else {
		var err error
		resp, err = client.GetPage(ctx, pageURL, "")
		if err != nil {
			return scannedPage{}, err
		}
		rawLinks = crawlparse.ParsePageLinks(resp.Body)
	}

	baseOrigin := strings.TrimSpace(preferredBase)
	if baseOrigin == "" {
		baseOrigin = inferBaseFromURL(crawlURL)
	}

	normalizedLinks := make([]string, 0, len(rawLinks))
	pageCodes := make([]string, 0, len(rawLinks))
	seenCodes := map[string]struct{}{}

	for _, link := range rawLinks {
		full := crawlindex.NormalizeDetailLink(link)
		if full == "" {
			continue
		}
		if !strings.HasPrefix(full, "http") && baseOrigin != "" {
			full = strings.TrimRight(baseOrigin, "/") + "/" + strings.TrimLeft(full, "/")
		}
		code := crawlidentity.ExtractFilmID(full)
		if strings.TrimSpace(code) == "" {
			continue
		}
		if _, exists := seenCodes[code]; exists {
			continue
		}
		seenCodes[code] = struct{}{}
		normalizedLinks = append(normalizedLinks, full)
		pageCodes = append(pageCodes, code)
	}

	return scannedPage{
		Page:  page,
		Codes: normalizeCodes(pageCodes),
		Links: normalizedLinks,
		Diagnostics: pageScanDiagnostics{
			PageURL:      pageURL,
			RawLinkCount: len(rawLinks),
			StatusCode:   resp.StatusCode,
			ResponseURL:  resp.URL,
		},
	}, nil
}

func (s *Service) baselineScan(ctx context.Context, crawlURL string, preferredBase string, totalPages int, itemsPerPage int, runtimeOptions ScanRuntimeOptions) ([]string, error) {
	if strings.TrimSpace(crawlURL) == "" {
		return nil, fmt.Errorf("target URL is required")
	}
	if totalPages <= 0 {
		totalPages = 1
	}
	if totalPages > defaultMaxScanPages {
		totalPages = defaultMaxScanPages
	}

	client, err := s.newScanClientWithOptions(runtimeOptions)
	if err != nil {
		return nil, err
	}

	collected := make([]string, 0)
	for page := 1; page <= totalPages; page++ {
		scanned, err := scanIndexPageWithDiagnostics(ctx, client, crawlURL, preferredBase, page, runtimeOptions)
		if err != nil {
			return nil, err
		}
		if len(scanned.Codes) == 0 {
			break
		}
		collected = append(collected, scanned.Codes...)
		if len(scanned.Codes) < maxInt(1, itemsPerPage) {
			break
		}
	}

	return normalizeCodes(collected), nil
}

func (s *Service) scanUntilRepeat(
	ctx context.Context,
	crawlURL string,
	preferredBase string,
	maxPages int,
	baseline []string,
	runtimeOptions ScanRuntimeOptions,
	logger scanLogger,
) ([]string, []RefreshPageSnapshot, string, int, error) {
	client, err := s.newScanClientWithOptions(runtimeOptions)
	if err != nil {
		return nil, nil, "", 0, err
	}

	baseline = normalizeCodes(baseline)
	baselineSet := map[string]struct{}{}
	for _, code := range baseline {
		baselineSet[code] = struct{}{}
	}

	pendingSet := map[string]struct{}{}
	pendingCodes := make([]string, 0)
	snapshots := make([]RefreshPageSnapshot, 0)
	latestItemURL := ""
	stoppedOnPage := 0

	if maxPages <= 0 {
		maxPages = 1
	}
	if maxPages > defaultMaxScanPages {
		maxPages = defaultMaxScanPages
	}

	if logger != nil {
		logger("info", fmt.Sprintf("开始检测更新：基线番号 %d 个，最多扫描 %d 页。", len(baseline), maxPages))
	}

	for page := 1; page <= maxPages; page++ {
		if logger != nil {
			logger("info", fmt.Sprintf("开始扫描第 %d 页番号（仅检测，不抓取磁力）。", page))
		}
		scanned, scanErr := scanIndexPageWithDiagnostics(ctx, client, crawlURL, preferredBase, page, runtimeOptions)
		if scanErr != nil {
			return nil, snapshots, latestItemURL, stoppedOnPage, scanErr
		}

		stoppedOnPage = page
		if page == 1 && len(scanned.Links) > 0 {
			latestItemURL = strings.TrimSpace(scanned.Links[0])
		}
		if len(scanned.Codes) == 0 {
			reason := fmt.Sprintf("未解析到作品番号，原始链接 %d 个，HTTP %d", scanned.Diagnostics.RawLinkCount, scanned.Diagnostics.StatusCode)
			if page == 1 {
				if logger != nil {
					logger("error", fmt.Sprintf("第 1 页检测失败：%s。响应地址：%s", reason, firstNonEmpty(scanned.Diagnostics.ResponseURL, scanned.Diagnostics.PageURL)))
				}
				return nil, snapshots, latestItemURL, stoppedOnPage, scanPageError{
					Page:         page,
					PageURL:      firstNonEmpty(scanned.Diagnostics.ResponseURL, scanned.Diagnostics.PageURL),
					Reason:       reason,
					RawLinkCount: scanned.Diagnostics.RawLinkCount,
				}
			}
			snapshots = append(snapshots, RefreshPageSnapshot{
				Page:          page,
				StopReason:    fmt.Sprintf("第 %d 页为空，停止扫描。", page),
				ObservedCount: 0,
			})
			if logger != nil {
				logger("info", fmt.Sprintf("第 %d 页为空，停止扫描。", page))
			}
			break
		}

		pageNewCodes := make([]string, 0)
		pageRepeatedCodes := make([]string, 0)
		fullPageRepeat := true
		for _, code := range scanned.Codes {
			if _, exists := baselineSet[code]; exists {
				pageRepeatedCodes = append(pageRepeatedCodes, code)
				continue
			}
			fullPageRepeat = false
			if _, exists := pendingSet[code]; exists {
				continue
			}
			pendingSet[code] = struct{}{}
			pendingCodes = append(pendingCodes, code)
			pageNewCodes = append(pageNewCodes, code)
		}

		stopReason := ""
		continueScan := true
		if fullPageRepeat {
			continueScan = false
			stopReason = fmt.Sprintf("第 %d 页全部为历史番号，停止扫描。", page)
		} else if len(pageNewCodes) == len(scanned.Codes) {
			stopReason = fmt.Sprintf("第 %d 页全部是新增番号，继续扫描下一页。", page)
		} else {
			stopReason = fmt.Sprintf("第 %d 页新增 %d 个、重复 %d 个，继续扫描下一页。", page, len(pageNewCodes), len(pageRepeatedCodes))
		}

		snapshots = append(snapshots, RefreshPageSnapshot{
			Page:           page,
			ObservedCodes:  scanned.Codes,
			NewCodes:       pageNewCodes,
			RepeatedCodes:  pageRepeatedCodes,
			FullPageRepeat: fullPageRepeat,
			ContinueScan:   continueScan,
			StopReason:     stopReason,
			ObservedCount:  len(scanned.Codes),
			NewCount:       len(pageNewCodes),
			RepeatedCount:  len(pageRepeatedCodes),
			RawLinkCount:   scanned.Diagnostics.RawLinkCount,
			PageURL:        scanned.Diagnostics.PageURL,
			ResponseURL:    scanned.Diagnostics.ResponseURL,
		})
		if logger != nil {
			logger("info", fmt.Sprintf("第 %d 页扫描完成：解析番号 %d 个，原始链接 %d 个，新增 %d 个，重复 %d 个。", page, len(scanned.Codes), scanned.Diagnostics.RawLinkCount, len(pageNewCodes), len(pageRepeatedCodes)))
			logger("info", fmt.Sprintf("第 %d 页解析番号：%s", page, strings.Join(scanned.Codes, "、")))
			if len(pageNewCodes) > 0 {
				logger("info", fmt.Sprintf("第 %d 页新增番号：%s", page, strings.Join(pageNewCodes, "、")))
			}
			logger("info", stopReason)
		}
		if !continueScan {
			break
		}
	}

	if logger != nil {
		if len(pendingCodes) > 0 {
			logger("info", fmt.Sprintf("检测完成：共发现 %d 个待更新番号。", len(pendingCodes)))
			logger("info", fmt.Sprintf("待更新番号：%s", strings.Join(normalizeCodes(pendingCodes), "、")))
		} else {
			logger("info", "检测完成：未发现待更新番号。")
		}
	}

	return normalizeCodes(pendingCodes), snapshots, latestItemURL, stoppedOnPage, nil
}

func containsCodeNormalized(values []string, target string) bool {
	normalizedTarget := normalizeCodes([]string{target})
	if len(normalizedTarget) == 0 {
		return false
	}
	targetCode := normalizedTarget[0]
	for _, value := range normalizeCodes(values) {
		if value == targetCode {
			return true
		}
	}
	return false
}
