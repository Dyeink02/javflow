// Ownership summary:
//   This file implements manual V2 subscription creation and baseline scan.
//
// File map for maintainers:
//   1) Manual create request validation and subscription build.
//   2) Page-1 baseline snapshot scan.
//   3) Manual subscription persistence helpers.
//
package avsubscriptionv2

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// CreateManual builds a V2 subscription from explicit user input plus an
// immediate baseline scan.
//
// Manual subscriptions have no complete data chain. The system only scans
// page 1 as a snapshot baseline for future diff detection. The user's
// declared total is preserved as-is and displayed as "基数".
func (s *Service) CreateManual(ctx context.Context, req ManualCreateRequest) (Subscription, error) {
	actressName := strings.TrimSpace(req.ActressName)
	crawlURL := strings.TrimSpace(req.CrawlURL)
	if actressName == "" {
		return Subscription{}, fmt.Errorf("actress name is required")
	}
	if crawlURL == "" {
		return Subscription{}, fmt.Errorf("target URL is required")
	}

	itemsPerPage := maxInt(defaultItemsPerPage, req.DeclaredPerPage)
	declaredTotal := maxInt(req.DeclaredTotal, 0)
	declaredPages := req.DeclaredPages
	if declaredPages <= 0 {
		declaredPages = calcPages(maxInt(declaredTotal, 1), itemsPerPage)
	}
	if declaredPages <= 0 {
		declaredPages = 1
	}

	preferredBase := strings.TrimSpace(req.PreferredBase)
	if preferredBase == "" {
		preferredBase = inferBaseFromURL(crawlURL)
	}

	// Only scan page 1 as snapshot baseline (not all declared pages).
	runtimeOptions := req.RuntimeOptions
	if strings.TrimSpace(runtimeOptions.Proxy) == "" {
		runtimeOptions.Proxy = strings.TrimSpace(req.Proxy)
	}
	baselineCodes, err := s.baselineScan(ctx, crawlURL, preferredBase, 1, itemsPerPage, runtimeOptions)
	if err != nil {
		return Subscription{}, err
	}
	if len(baselineCodes) == 0 {
		return Subscription{}, fmt.Errorf("未能建立订阅基线，请检查地址、代理或页数设置")
	}

	now := time.Now().Format(time.RFC3339)
	next := Subscription{
		ActressName:           actressName,
		CrawlURL:              crawlURL,
		PreferredBase:         preferredBase,
		SourceType:            normalizeSourceType(req.SourceType),
		BaselineCodes:         baselineCodes,
		BaselineCount:         len(baselineCodes),
		CurrentObservedCount:  declaredTotal,
		CurrentTotal:          declaredTotal,
		ItemsPerPage:          itemsPerPage,
		TotalPages:            declaredPages,
		CreatedAt:             now,
		BaselineSnapshotAt:    now,
		LastUpdatedAt:         now,
		PreferredOutputDir:    strings.TrimSpace(req.PreferredOutDir),
		ManualDeclaredTotal:   declaredTotal,
		ManualDeclaredPages:   declaredPages,
		ManualDeclaredPerPage: itemsPerPage,
	}
	return s.Upsert(next)
}
