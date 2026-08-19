// Ownership summary:
//   This file implements domain logic for the javflow backend.
//
// File map for maintainers:
//   1) Domain-specific types and helpers.
//   2) Internal service implementation.
//

package bridge

import (
	"fmt"
	"time"

	"javflow/internal/common"
	"javflow/internal/subcrawl"
)

func (a *API) resolveSubcrawlProxy(payload map[string]any) string {
	proxyValue := common.CleanString(payload["proxy"])
	if proxyValue != "" {
		return proxyValue
	}
	settings := a.loadBridgeSettingsSnapshot()
	return common.CleanString(settings["proxy"])
}

func (a *API) resolveSubcrawlCookie(payload map[string]any) string {
	return common.CleanString(payload["configCookie"])
}

func (a *API) startSubscriptionCrawlResult(payload map[string]any) (string, error) {
	if a.lookup.subCrawl == nil {
		return "", fmt.Errorf("订阅抓取服务未初始化")
	}

	subscriptionID := common.CleanString(payload["subscriptionId"])
	actressName := common.CleanString(payload["actressName"])
	crawlURL := common.CleanString(payload["crawlUrl"])
	preferredBase := common.CleanString(payload["preferredBase"])
	outputDir := common.CleanString(payload["outputDir"])
	proxy := a.resolveSubcrawlProxy(payload)
	configCookie := a.resolveSubcrawlCookie(payload)
	targetCount := common.IntValue(payload["targetCount"], 0)
	timeoutMs := common.IntValue(payload["timeout"], 30000)

	if crawlURL == "" {
		return "", fmt.Errorf("抓取地址不能为空")
	}
	if outputDir == "" {
		return "", fmt.Errorf("输出目录不能为空")
	}

	req := subcrawl.CrawlRequest{
		SubscriptionID: subscriptionID,
		ActressName:    actressName,
		CrawlURL:       crawlURL,
		PreferredBase:  preferredBase,
		OutputDir:      outputDir,
		TargetCount:    targetCount,
		UserDataDir:    a.runtime.store.UserDataDir(),
		Proxy:          proxy,
		ConfigCookie:   configCookie,
		Timeout:        time.Duration(timeoutMs) * time.Millisecond,
	}

	if subscriptionID != "" && a.lookup.avSubscriptions != nil {
		items, err := a.lookup.avSubscriptions.List()
		if err != nil {
			return "", err
		}
		for _, item := range items {
			if item.ID != subscriptionID {
				continue
			}
			req.BaselineCodes = append([]string{}, item.BaselineCodes...)
			if req.ActressName == "" {
				req.ActressName = item.ActressName
			}
			if req.CrawlURL == "" {
				req.CrawlURL = item.CrawlURL
			}
			if req.PreferredBase == "" {
				req.PreferredBase = item.PreferredBase
			}
			break
		}
	}

	if err := a.lookup.subCrawl.StartSingle(a.wailsCtx, req); err != nil {
		return "", err
	}

	return marshalResult(map[string]any{"started": true, "actressName": actressName})
}

func (a *API) startSubscriptionBatchCrawlResult(payload map[string]any) (string, error) {
	if a.lookup.subCrawl == nil {
		return "", fmt.Errorf("订阅抓取服务未初始化")
	}

	outputDir := common.CleanString(payload["outputDir"])
	proxy := a.resolveSubcrawlProxy(payload)
	configCookie := a.resolveSubcrawlCookie(payload)
	timeoutMs := common.IntValue(payload["timeout"], 30000)

	if outputDir == "" {
		return "", fmt.Errorf("输出目录不能为空")
	}

	req := subcrawl.BatchCrawlRequest{
		OutputDir:    outputDir,
		Proxy:        proxy,
		ConfigCookie: configCookie,
		Timeout:      time.Duration(timeoutMs) * time.Millisecond,
	}

	if err := a.lookup.subCrawl.StartBatch(a.wailsCtx, req); err != nil {
		return "", err
	}

	return marshalResult(map[string]any{"started": true, "batch": true})
}

func (a *API) stopSubscriptionCrawlResult() (string, error) {
	if a.lookup.subCrawl == nil {
		return "", fmt.Errorf("订阅抓取服务未初始化")
	}
	if err := a.lookup.subCrawl.Stop(); err != nil {
		return "", err
	}
	return marshalResult(map[string]any{"stopped": true})
}

func (a *API) subscriptionCrawlStatusResult() (string, error) {
	if a.lookup.subCrawl == nil {
		return marshalResult(subcrawl.CrawlStatus{Phase: "idle", Status: "idle"})
	}
	return marshalResult(a.lookup.subCrawl.Status())
}
