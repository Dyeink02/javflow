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
	"strings"

	"javflow/internal/avsubscriptionv2"
)

func (a *API) scanSubscriptionsV2FromOutputResult(payload map[string]any) (string, error) {
	if a.lookup.avSubscriptionsV2 == nil {
		return "", fmt.Errorf("AV subscription V2 service is not initialized")
	}
	result, err := a.lookup.avSubscriptionsV2.ImportFromOutput(a.resolveArtifactInputOutputDir(payload, "outputDir"))
	if err != nil {
		return "", err
	}
	items, listErr := a.lookup.avSubscriptionsV2.List()
	if listErr != nil {
		return "", listErr
	}

	runtimeOptions := a.buildSubscriptionV2RuntimeOptions(payload)
	a.triggerAutoDetectAfterImport(result.Subscription, runtimeOptions)

	return marshalSubscriptionCollectionV2(items, map[string]any{
		"addedCount":         map[bool]int{true: 1, false: 0}[result.Added],
		"updatedCount":       map[bool]int{true: 1, false: 0}[result.Updated],
		"sourceType":         result.SourceType,
		"outputDir":          result.OutputDir,
		"filmDataPath":       result.FilmDataPath,
		"scannedActresses":   1,
		"scannedActressList": []string{result.Subscription.ActressName},
	})
}

func (a *API) addSubscriptionV2ManualResult(payload map[string]any) (string, error) {
	if a.lookup.avSubscriptionsV2 == nil {
		return "", fmt.Errorf("AV subscription V2 service is not initialized")
	}
	req := avsubscriptionv2.ManualCreateRequest{
		ActressName:     nonEmptyString(payload["actressName"]),
		CrawlURL:        nonEmptyString(payload["targetUrl"]),
		PreferredBase:   nonEmptyString(payload["preferredBase"]),
		DeclaredTotal:   intValue(payload["syncedCount"], 0),
		DeclaredPages:   intValue(payload["totalPages"], 0),
		DeclaredPerPage: intValue(payload["itemsPerPage"], 0),
		PreferredOutDir: nonEmptyString(payload["preferredOutputDir"]),
		Proxy:           nonEmptyString(payload["proxy"]),
		RuntimeOptions:  a.buildSubscriptionV2RuntimeOptions(payload),
	}
	saved, err := a.lookup.avSubscriptionsV2.CreateManual(a.applicationContext(), req)
	if err != nil {
		return "", err
	}

	a.triggerAutoDetectAfterImport(saved, req.RuntimeOptions)

	return marshalResult(saved)
}

func (a *API) triggerAutoDetectAfterImport(sub avsubscriptionv2.Subscription, runtimeOptions avsubscriptionv2.ScanRuntimeOptions) {
	if a.lookup.avSubscriptionsV2 == nil || a.runtime.bus == nil {
		return
	}
	subscriptionID := strings.TrimSpace(sub.ID)
	actressName := strings.TrimSpace(sub.ActressName)
	if subscriptionID == "" {
		return
	}

	ctx := a.applicationContext()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger := a.subscriptionV2RefreshLogger()
				logger("error", fmt.Sprintf("导入后自动检测 panic: %v", r))
			}
		}()
		logger := a.subscriptionV2RefreshLogger()
		logger("info", fmt.Sprintf("导入完成，正在自动检测 %s 的更新...", actressName))
		logger("info", "[diagnostic] 自动检测桥接主爬虫请求配置 "+describeSubscriptionV2RuntimeOptions(runtimeOptions))
		refreshResult, refreshErr := a.lookup.avSubscriptionsV2.RefreshOne(
			ctx,
			subscriptionID,
			runtimeOptions,
			logger,
		)
		if refreshErr != nil {
			logger("error", fmt.Sprintf("自动检测失败：%s", refreshErr.Error()))
			return
		}
		logger("info", fmt.Sprintf("自动检测完成：待更新 %d 部。", refreshResult.Subscription.PendingCount))
		a.runtime.bus.Emit("avsubscriptionv2.list-updated", map[string]any{
			"trigger": "auto-detect-after-import",
		})
	}()
}

func (a *API) prepareSubscriptionV2CrawlResult(payload map[string]any) (string, error) {
	if a.lookup.avSubscriptionsV2 == nil {
		return "", fmt.Errorf("AV subscription V2 service is not initialized")
	}
	id := strings.TrimSpace(nonEmptyString(payload["id"]))
	if id == "" {
		return "", fmt.Errorf("subscription id is required")
	}
	items, err := a.lookup.avSubscriptionsV2.List()
	if err != nil {
		return "", err
	}
	for _, item := range items {
		if item.ID != id {
			continue
		}
		targetCount := item.PendingCount
		if targetCount <= 0 {
			targetCount = 1
		}
		return marshalResult(map[string]any{
			"id":            item.ID,
			"actressName":   item.ActressName,
			"crawlUrl":      item.CrawlURL,
			"preferredBase": item.PreferredBase,
			"targetCount":   targetCount,
			"pendingCount":  item.PendingCount,
			"pendingCodes":  item.PendingCodes,
			"outputDir":     item.PreferredOutputDir,
		})
	}
	return "", fmt.Errorf("subscription not found: %s", id)
}
