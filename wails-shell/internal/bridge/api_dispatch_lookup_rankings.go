// Lookup ranking commands and progress events stay separate from target
// resolution so ranking/cache regressions remain isolated at the bridge edge.
//
// Ownership summary:
// 1) route ranking-only lookup commands
// 2) publish ranking progress events without owning ranking business rules
// 3) keep ranking/read-cache transport separate from workflow mutations
//
// File map for maintainers:
// 1) ranking progress event envelope
// 2) ranking command dispatcher
package bridge

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"javflow/internal/actressranking"
)

func (a *API) emitActressRankingLog(level string, message string, details map[string]any) {
	if a.runtime.bus == nil {
		return
	}
	payload := map[string]any{
		"level":   level,
		"message": message,
		"domain":  "actress-ranking",
		"time":    time.Now().Format(time.RFC3339),
	}
	for key, value := range details {
		payload[key] = value
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	// Keep the existing bridged event for compatibility and also expose the
	// lightweight Atlas-specific stream used by the current renderer.
	a.runtime.bus.Emit("atlas:ranking:log", payload)
	a.runtime.bus.Publish("v1", "log", "actressranking.log", "actress-ranking", "load", "", payload["time"].(string), raw)
}

// Lookup ranking commands stay separate from target resolution so ranking/cache
// regressions do not get mixed into actress target parsing issues.
//
// Ranking rule:
// this lane returns ranking/read-cache data only. Any future command that
// changes crawler, subscription, or organizer state should not be added here
// just because the UI entrypoint starts from the ranking page.
//
// Ownership summary:
// 1) route ranking-only lookup commands
// 2) keep ranking/read-cache queries separate from target lookup and mutations
// 3) centralize ranking dispatch away from the broader lookup branch
//
// File map for maintainers:
// 1) ranking command dispatcher
// 2) ranking query branch routing
func (a *API) handleLookupRankingCommand(command string, payload map[string]any) (string, bool, error) {
	switch command {
	case "app:get-actress-rankings":
		if a.lookup.actressRanking == nil {
			return "", true, fmt.Errorf("actress ranking service is not initialized")
		}
		rankings, err := a.lookup.actressRanking.GetActressRankings(a.buildActressRankingOptions(payload))
		if err != nil {
			return "", true, err
		}
		// 已缓存头像直接解析为本地媒体路由：历史月份切换时头像即时显示，
		// 不再进入按需下载流程。
		rankings.Items = a.resolveActressRankingAvatarURLs(rankings.Items)
		result, err := marshalResult(rankings)
		return result, true, err

	case "app:save-actress-ranking-history":
		if a.lookup.actressRanking == nil {
			return "", true, fmt.Errorf("actress ranking service is not initialized")
		}
		encoded, err := marshalResult(payload["ranking"])
		if err != nil {
			return "", true, fmt.Errorf("invalid ranking history payload: %w", err)
		}
		var ranking actressranking.Result
		if err := json.Unmarshal([]byte(encoded), &ranking); err != nil {
			return "", true, fmt.Errorf("invalid ranking history payload: %w", err)
		}
		options := a.buildActressRankingOptions(payload)
		saved, alreadySaved, err := a.lookup.actressRanking.SaveMonthlyHistory(ranking, options.HistoryDirectories)
		if err != nil {
			return "", true, err
		}
		a.emitActressRankingLog("success", fmt.Sprintf("本地月榜已%s：%s。", map[bool]string{true: "保留原记录", false: "保存"}[alreadySaved], saved.PeriodLabel), map[string]any{
			"stage": "history.save", "year": saved.PeriodYear, "month": saved.PeriodMonth, "alreadySaved": alreadySaved,
		})
		result, err := marshalResult(map[string]any{
			"periodLabel":  saved.PeriodLabel,
			"periodYear":   saved.PeriodYear,
			"periodMonth":  saved.PeriodMonth,
			"alreadySaved": alreadySaved,
		})
		return result, true, err

	case "app:backfill-ranking-history":
		if a.lookup.actressRanking == nil {
			return "", true, fmt.Errorf("actress ranking service is not initialized")
		}
		options := a.buildActressRankingOptions(payload)
		yearsBack := intValue(payload["yearsBack"], 5)
		results, err := a.lookup.actressRanking.BackfillAnnualHistory(options, yearsBack)
		if err != nil {
			return "", true, err
		}

		// 逐年缓存榜单头像：缓存按源 URL 去重，跨年份的同一演员只下载一次。
		ctx, cancel := a.requestContext(12 * time.Minute)
		defer cancel()
		totalCached := 0
		totalFailed := 0
		yearSummaries := make([]map[string]any, 0, len(results))
		for _, result := range results {
			if len(result.Items) == 0 {
				continue
			}
			if _, cached, failed, cacheErr := a.cacheActressAtlasRankingAvatars(ctx, result.Items, options.Proxy); cacheErr == nil {
				totalCached += cached
				totalFailed += failed
			}
			yearSummaries = append(yearSummaries, map[string]any{
				"year":  result.PeriodYear,
				"total": result.Total,
			})
		}

		result, marshalErr := marshalResult(map[string]any{
			"fetchedYears":  len(results),
			"years":         yearSummaries,
			"avatarsCached": totalCached,
			"avatarsFailed": totalFailed,
		})
		return result, true, marshalErr

	case "app:cache-actress-ranking-avatars":
		encoded, err := marshalResult(payload["items"])
		if err != nil {
			return "", true, err
		}
		var items []actressranking.RankingItem
		if err := json.Unmarshal([]byte(encoded), &items); err != nil {
			return "", true, fmt.Errorf("invalid ranking avatar items: %w", err)
		}
		if len(items) == 0 {
			result, marshalErr := marshalResult(map[string]any{"items": []actressranking.RankingItem{}, "cached": 0, "failed": 0})
			return result, true, marshalErr
		}
		a.emitActressRankingLog("info", fmt.Sprintf("开始缓存榜单头像：%d 张，并发 %d。", len(items), actressAtlasRankingParallelism), map[string]any{
			"phase": "avatar-cache-start",
			"total": len(items),
		})
		ctx, cancel := a.requestContext(40 * time.Second)
		defer cancel()
		updated, cached, failed, cacheErr := a.cacheActressAtlasRankingAvatars(ctx, items, strings.TrimSpace(nonEmptyString(payload["proxy"])))
		if cacheErr != nil {
			return "", true, cacheErr
		}
		a.emitActressRankingLog("success", fmt.Sprintf("榜单头像缓存完成：成功 %d 张，失败 %d 张。", cached, failed), map[string]any{
			"phase":  "avatar-cache-finish",
			"total":  len(items),
			"cached": cached,
			"failed": failed,
		})
		result, marshalErr := marshalResult(map[string]any{"items": updated, "cached": cached, "failed": failed})
		return result, true, marshalErr
	}

	return "", false, nil
}
