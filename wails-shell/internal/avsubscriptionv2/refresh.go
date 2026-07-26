// Ownership summary:
//   This file implements V2 subscription refresh and update detection.
//
// File map for maintainers:
//   1) RefreshAll batch orchestration.
//   2) Per-subscription refresh and page scanning.
//   3) Refresh summary aggregation.
//
package avsubscriptionv2

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// RefreshAll runs V2 update detection for every saved subscription.
func (s *Service) RefreshAll(ctx context.Context, runtimeOptions ScanRuntimeOptions, logger scanLogger) (RefreshSummary, error) {
	items, err := s.load()
	if err != nil {
		return RefreshSummary{}, err
	}

	summary := RefreshSummary{
		Subscriptions: make([]Subscription, 0, len(items)),
	}
	now := time.Now().Format(time.RFC3339)

	for _, item := range items {
		itemLogger := loggerForSubscription(logger, item)
		if itemLogger != nil {
			itemLogger("info", fmt.Sprintf("开始检测订阅：%s", displaySubscriptionName(item)))
		}
		result, refreshErr := s.refreshOneInternal(ctx, item, runtimeOptions, now, itemLogger)
		if refreshErr != nil {
			summary.FailedCount++
			if itemLogger != nil {
				itemLogger("error", fmt.Sprintf("检测失败：%s", refreshErr.Error()))
			}
		}
		if result.HasUpdate {
			summary.UpdatedCount++
		}
		summary.CheckedCount++
		summary.TotalPending += result.Subscription.PendingCount
		summary.Subscriptions = append(summary.Subscriptions, result.Subscription)
	}

	if err := s.ReplaceAll(summary.Subscriptions); err != nil {
		return RefreshSummary{}, err
	}
	return summary, nil
}

// RefreshOne runs V2 update detection for one subscription.
func (s *Service) RefreshOne(ctx context.Context, id string, runtimeOptions ScanRuntimeOptions, logger scanLogger) (RefreshResult, error) {
	items, err := s.load()
	if err != nil {
		return RefreshResult{}, err
	}
	index := findSubscriptionIndexByID(items, id)
	if index < 0 {
		return RefreshResult{}, fmt.Errorf("subscription not found: %s", id)
	}

	now := time.Now().Format(time.RFC3339)
	itemLogger := loggerForSubscription(logger, items[index])
	if itemLogger != nil {
		itemLogger("info", fmt.Sprintf("开始检测订阅：%s", displaySubscriptionName(items[index])))
	}
	result, refreshErr := s.refreshOneInternal(ctx, items[index], runtimeOptions, now, itemLogger)
	items[index] = result.Subscription
	if err := s.ReplaceAll(items); err != nil {
		return RefreshResult{}, err
	}
	if refreshErr != nil && itemLogger != nil {
		itemLogger("error", fmt.Sprintf("检测失败：%s", refreshErr.Error()))
	}
	return result, refreshErr
}

func (s *Service) refreshOneInternal(ctx context.Context, item Subscription, runtimeOptions ScanRuntimeOptions, now string, logger scanLogger) (RefreshResult, error) {
	if strings.TrimSpace(item.CrawlURL) == "" {
		item.LastCheckedAt = now
		item.LastUpdatedAt = now
		item.LastError = "subscription target URL is empty"
		item.Status = statusError
		return RefreshResult{Subscription: item}, fmt.Errorf("subscription target URL is empty")
	}

	maxPages := item.TotalPages
	if maxPages <= 0 {
		maxPages = calcPages(maxInt(item.CurrentObservedCount, item.BaselineCount), item.ItemsPerPage)
	}
	if maxPages <= 0 {
		maxPages = 1
	}
	if maxPages > defaultMaxScanPages {
		maxPages = defaultMaxScanPages
	}

	pendingCodes, pageSnapshots, latestItemURL, stoppedOnPage, scanErr := s.scanUntilRepeat(
		ctx,
		item.CrawlURL,
		item.PreferredBase,
		maxPages,
		item.BaselineCodes,
		runtimeOptions,
		logger,
	)
	if scanErr != nil {
		item.LastCheckedAt = now
		item.LastUpdatedAt = now
		item.LastError = scanErr.Error()
		item.Status = statusError
		return RefreshResult{Subscription: normalizeSubscription(item, now)}, scanErr
	}

	scannedPages := len(pageSnapshots)
	item.PendingCodes = normalizeCodes(pendingCodes)
	item.PendingCount = len(item.PendingCodes)
	// CurrentObservedCount is the operator-facing "remote page currently shows"
	// number. Prefer the actual page scan footprint over stale saved counts so
	// a one-page actress with baseline 7 and page scan 8 is displayed as 8.
	item.CurrentObservedCount = resolveObservedCountFromSnapshots(item, pageSnapshots)
	item.LastScanPages = scannedPages
	item.LastStoppedOnPage = stoppedOnPage
	item.LastCheckedAt = now
	item.LastUpdatedAt = now
	item.LastError = ""
	item.LatestItemURL = latestItemURL
	if item.PendingCount > 0 {
		item.Status = statusUpdated
	} else {
		item.Status = statusIdle
	}
	item = normalizeSubscription(item, now)

	if logger != nil {
		if item.CurrentObservedCount > item.BaselineCount+item.PendingCount {
			logger("warn", fmt.Sprintf(
				"基数异常：用户基线 %d 部，但实际观测到 %d 部（含待更新 %d 部）。建议修正基线数据。",
				item.BaselineCount,
				item.CurrentObservedCount,
				item.PendingCount,
			))
		}
		logger("info", fmt.Sprintf(
			"检测结果：基线 %d 部，当前 %d 部，待更新 %d 部，扫描 %d 页，停止于第 %d 页。",
			item.BaselineCount,
			item.CurrentObservedCount,
			item.PendingCount,
			scannedPages,
			stoppedOnPage,
		))
	}

	return RefreshResult{
		Subscription:  item,
		HasUpdate:     item.PendingCount > 0,
		ObservedCount: item.CurrentObservedCount,
		PendingCodes:  append([]string{}, item.PendingCodes...),
		ScannedPages:  scannedPages,
		StoppedOnPage: stoppedOnPage,
		PageSnapshots: pageSnapshots,
	}, nil
}

func resolveObservedCountFromSnapshots(item Subscription, snapshots []RefreshPageSnapshot) int {
	observedCodes := make([]string, 0)
	for _, snapshot := range snapshots {
		observedCodes = append(observedCodes, snapshot.ObservedCodes...)
	}
	uniqueObserved := len(normalizeCodes(observedCodes))
	if uniqueObserved > 0 {
		if len(snapshots) >= item.TotalPages || item.TotalPages <= 1 {
			return uniqueObserved
		}
		return maxInt(uniqueObserved, item.BaselineCount+item.PendingCount)
	}
	return maxInt(item.BaselineCount+item.PendingCount, item.CurrentObservedCount)
}

func displaySubscriptionName(item Subscription) string {
	if strings.TrimSpace(item.ActressName) != "" {
		return strings.TrimSpace(item.ActressName)
	}
	if strings.TrimSpace(item.CrawlURL) != "" {
		return strings.TrimSpace(item.CrawlURL)
	}
	return "未命名订阅"
}

func loggerForSubscription(logger scanLogger, item Subscription) scanLogger {
	if logger == nil {
		return nil
	}
	name := displaySubscriptionName(item)
	return func(level string, message string) {
		logger(level, fmt.Sprintf("[%s] %s", name, strings.TrimSpace(message)))
	}
}
