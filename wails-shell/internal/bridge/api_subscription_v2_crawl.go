// Ownership summary:
//   This file implements domain logic for the javflow backend.
//
// File map for maintainers:
//   1) Domain-specific types and helpers.
//   2) Internal service implementation.
//

package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"javflow/internal/common"
	"javflow/internal/subcrawlv2"
)

func (a *API) startSubscriptionV2CrawlResult(payload map[string]any) (string, error) {
	if a.lookup.avSubscriptionsV2 == nil {
		return "", fmt.Errorf("AV subscription V2 service is not initialized")
	}

	subscriptionID := common.CleanString(payload["subscriptionId"])
	proxy := a.resolveSubcrawlProxy(payload)
	targetCount := common.IntValue(payload["targetCount"], 0)
	parallel := common.IntValue(payload["parallel"], 2)
	if parallel <= 0 {
		parallel = 2
	}
	delay := common.IntValue(payload["delay"], 2)
	if delay < 0 {
		delay = 2
	}
	timeout := common.IntValue(payload["timeout"], 30000)
	if timeout < 1000 {
		timeout = 30000
	}

	items, err := a.lookup.avSubscriptionsV2.List()
	if err != nil {
		return "", err
	}

	for _, item := range items {
		if item.ID != subscriptionID {
			continue
		}

		targetCodes := normalizeSubscriptionTargetCodes(stringSliceValue(payload["targetCodes"]))
		if len(targetCodes) == 0 {
			targetCodes = normalizeSubscriptionTargetCodes(item.PendingCodes)
		}
		if targetCount <= 0 {
			targetCount = len(targetCodes)
		}
		if targetCount <= 0 {
			targetCount = 1
		}
		itemsPerPage := 30
		crawlPages := resolveSubscriptionCrawlPages(item.LastStoppedOnPage, targetCount, len(targetCodes))
		crawlCapacity := calcSubscriptionCrawlLimit(crawlPages, itemsPerPage)
		outputDir, prepareErr := prepareSubscriptionV2OutputDir(a.runtime.paths.AppPath, item.ActressName)
		if prepareErr != nil {
			return "", fmt.Errorf("准备 AV 订阅输出目录失败：%w", prepareErr)
		}
		logDir := resolveSubscriptionV2LogDir(a.runtime.paths.AppPath)

		crawlPayload := map[string]any{
			"frontendPayloadVersion": "2026-05-24-av-subscription-v2-page-scope-bridge",
			"base":                   item.CrawlURL,
			"baseUrl":                item.CrawlURL,
			"output":                 outputDir,
			"outputDir":              outputDir,
			"logDir":                 logDir,
			"limit":                  0,
			"itemsPerPage":           itemsPerPage,
			"totalPages":             crawlPages,
			"parallel":               parallel,
			"delay":                  delay,
			"timeout":                timeout,
			"proxy":                  proxy,
			"cookie":                 a.resolveSubcrawlCookie(payload),
			"cloudflare":             true,
			"secondValidation":       true,
			"nomag":                  false,
			"allmag":                 false,
			// AV 订阅只需要番号、影片元数据和磁力链接；图片会增加
			// Cloudflare/年龄检测压力，也会拖慢“更新后筛选”的收尾流程。
			"nopic":                       true,
			"magnetContentValidation":     false,
			"magnetExcludeKeywords":       "",
			"actressCountFilterThreshold": item.ActressCountFilterThreshold,
			"goTaskController":            true,
		}

		prepared := a.normalizeActressThresholdPayload(crawlPayload)
		preparedPages := common.IntValue(prepared["totalPages"], 1)
		mode := a.resolveCrawlExecutionMode(prepared)
		a.setGoTaskExecutionMode(mode)
		a.emitSubscriptionV2Log("info", fmt.Sprintf(
			"开始订阅爬取：%s，待更新 %d 部；实际抓取第 1-%d 页全部影片（页面容量约 %d，按页抓满），完成后仅保留：%s。模式 %s（并行=%d, 延迟=%d, 超时=%d，跳过图片=是）",
			item.ActressName,
			targetCount,
			preparedPages,
			crawlCapacity,
			describeSubscriptionTargetCodes(targetCodes),
			mode,
			common.IntValue(prepared["parallel"], parallel),
			common.IntValue(prepared["delay"], delay),
			common.IntValue(prepared["timeout"], timeout),
		))
		a.emitLogEntry("info", fmt.Sprintf(
			"[diagnostic] AV订阅桥接主爬虫 start output=%v cloudflare=%v executionMode=%s controllerMode=%s",
			prepared["output"],
			payloadBool(prepared["cloudflare"]),
			mode,
			crawlControllerModeGoTask,
		))
		actualOutputDir := outputDir
		if a.crawl.crawlTask != nil {
			raw, err := a.crawl.crawlTask.Start(context.Background(), prepared)
			if err != nil {
				return "", err
			}
			actualOutputDir = firstNonEmpty(
				extractStringFromJSON(raw, "currentTaskOutputDir"),
				extractStringFromJSON(raw, "outputDir"),
				extractStringFromJSON(raw, "output"),
				outputDir,
			)
		} else {
			raw, err := a.dispatchPreparedCrawlStart(prepared)
			if err != nil {
				return "", err
			}
			actualOutputDir = firstNonEmpty(
				extractStringFromJSON(json.RawMessage(raw), "currentTaskOutputDir"),
				extractStringFromJSON(json.RawMessage(raw), "outputDir"),
				extractStringFromJSON(json.RawMessage(raw), "output"),
				outputDir,
			)
		}

		return marshalResult(map[string]any{
			"started":        true,
			"subscription":   item,
			"outputDir":      actualOutputDir,
			"baseOutputDir":  resolveSubscriptionV2RootDir(a.runtime.paths.AppPath),
			"logDir":         logDir,
			"nopic":          true,
			"targetCodes":    targetCodes,
			"targetCount":    targetCount,
			"crawlLimit":     crawlCapacity,
			"totalPages":     preparedPages,
			"itemsPerPage":   common.IntValue(prepared["itemsPerPage"], 30),
			"parallel":       common.IntValue(prepared["parallel"], 2),
			"delay":          common.IntValue(prepared["delay"], 2),
			"timeout":        common.IntValue(prepared["timeout"], 30000),
			"executionMode":  a.currentExecutionMode(),
			"controllerMode": crawlControllerModeGoTask,
		})
	}

	return "", fmt.Errorf("subscription not found: %s", subscriptionID)
}

func (a *API) startSubscriptionV2BatchCrawlResult(payload map[string]any) (string, error) {
	if a.lookup.avSubscriptionsV2 == nil || a.lookup.subCrawlV2 == nil {
		return "", fmt.Errorf("AV subscription batch crawler is not initialized")
	}
	concurrency := common.IntValue(payload["actorConcurrency"], 2)
	if concurrency < 1 {
		concurrency = 2
	}
	if concurrency > 3 {
		concurrency = 3
	}
	timeoutMs := common.IntValue(payload["timeout"], 30000)
	if timeoutMs < 1000 {
		timeoutMs = 30000
	}
	proxy := a.resolveSubcrawlProxy(payload)
	rootDir := resolveSubscriptionV2RootDir(a.runtime.paths.AppPath)
	items, err := a.lookup.avSubscriptionsV2.List()
	if err != nil {
		return "", err
	}
	requests := make([]subcrawlv2.CrawlRequest, 0, len(items))
	for _, item := range items {
		targetCodes := normalizeSubscriptionTargetCodes(item.PendingCodes)
		if len(targetCodes) == 0 {
			continue
		}
		if _, err := prepareSubscriptionV2OutputDir(a.runtime.paths.AppPath, item.ActressName); err != nil {
			return "", fmt.Errorf("prepare subscription output for %s: %w", item.ActressName, err)
		}
		requests = append(requests, subcrawlv2.CrawlRequest{
			SubscriptionID:              item.ID,
			ActressName:                 item.ActressName,
			CrawlURL:                    item.CrawlURL,
			PreferredBase:               item.PreferredBase,
			OutputDir:                   rootDir,
			TargetCount:                 len(targetCodes),
			TargetCodes:                 targetCodes,
			ActressCountFilterThreshold: item.ActressCountFilterThreshold,
			UserDataDir:                 a.runtime.store.UserDataDir(),
			Proxy:                       proxy,
			ConfigCookie:                a.resolveSubcrawlCookie(payload),
			Timeout:                     time.Duration(timeoutMs) * time.Millisecond,
		})
	}
	if len(requests) == 0 {
		return "", fmt.Errorf("\u5f53\u524d\u6ca1\u6709\u5f85\u66f4\u65b0\u7684\u8ba2\u9605")
	}
	if err := a.lookup.subCrawlV2.StartMany(context.Background(), requests, concurrency); err != nil {
		return "", err
	}
	a.emitSubscriptionV2Log("info", fmt.Sprintf("\u4e00\u952e\u5168\u90e8\u66f4\u65b0\u5df2\u542f\u52a8\uff1a%d \u4f4d\u6f14\u5458\uff0c\u540c\u65f6\u66f4\u65b0 %d \u4f4d", len(requests), concurrency))
	return marshalResult(map[string]any{
		"started":          true,
		"batch":            true,
		"total":            len(requests),
		"actorConcurrency": concurrency,
		"outputDir":        rootDir,
	})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func extractStringFromJSON(raw json.RawMessage, key string) string {
	if len(raw) == 0 || strings.TrimSpace(key) == "" {
		return ""
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return ""
	}
	value, exists := decoded[key]
	if !exists || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func (a *API) stopSubscriptionV2CrawlResult() (string, error) {
	a.emitSubscriptionV2Log("info", "订阅爬取已停止。")
	if a.lookup.subCrawlV2 != nil {
		if err := a.lookup.subCrawlV2.Stop(); err != nil {
			return "", err
		}
	}
	if _, err := a.handleTaskControlledCrawlStop(); err != nil {
		return "", err
	}
	return marshalResult(map[string]any{"stopped": true})
}

func (a *API) subscriptionV2CrawlStatusResult() (string, error) {
	if a.lookup.subCrawlV2 != nil {
		status := a.lookup.subCrawlV2.Status()
		if status.Status != "idle" {
			return marshalResult(status)
		}
	}
	status := "idle"
	if mode := strings.TrimSpace(a.currentExecutionMode()); mode != "" && mode != crawlExecutionModeIdle {
		status = "running"
	}
	return marshalResult(map[string]any{
		"phase":          status,
		"status":         status,
		"executionMode":  a.currentExecutionMode(),
		"controllerMode": crawlControllerModeGoTask,
	})
}

func calcSubscriptionScanPages(targetCount int, targetCodeCount int) int {
	if targetCodeCount <= 0 && targetCount <= 0 {
		return 1
	}
	referenceCount := targetCount
	if targetCodeCount > referenceCount {
		referenceCount = targetCodeCount
	}
	pages := (referenceCount + 29) / 30
	if pages < 1 {
		pages = 1
	}
	if pages > 50 {
		pages = 50
	}
	return pages
}

func resolveSubscriptionCrawlPages(lastStoppedOnPage int, targetCount int, targetCodeCount int) int {
	scanPages := calcSubscriptionScanPages(targetCount, targetCodeCount)
	if lastStoppedOnPage > scanPages {
		return clampSubscriptionCrawlPages(lastStoppedOnPage)
	}
	return scanPages
}

func calcSubscriptionCrawlLimit(totalPages int, itemsPerPage int) int {
	totalPages = clampSubscriptionCrawlPages(totalPages)
	if itemsPerPage <= 0 {
		itemsPerPage = 30
	}
	return totalPages * itemsPerPage
}

func clampSubscriptionCrawlPages(totalPages int) int {
	if totalPages < 1 {
		return 1
	}
	if totalPages > 50 {
		return 50
	}
	return totalPages
}

func describeSubscriptionTargetCodes(targetCodes []string) string {
	normalized := normalizeSubscriptionTargetCodes(targetCodes)
	if len(normalized) == 0 {
		return "未指定，保留全部输出"
	}
	return strings.Join(normalized, "、")
}
