package bridge

import (
	"fmt"

	"javflow/internal/contracts/crawlartifact"
)

// Cache snapshot bridge helpers expose the shared internal crawl-cache index to
// crawler/subscription/organizer UIs without leaking file-path logic into
// renderer controllers.
//
// Ownership summary:
// 1) list internal crawl cache snapshots for UI preload selectors
// 2) remove one snapshot or clear all snapshots
// 3) keep snapshot-index wiring separate from subscription mutations
//
// File map for maintainers:
// 1) Cache snapshot list, remove, and clear handlers.
// 2) Marshalling helpers for snapshot results.

func (a *API) listCrawlCacheSnapshotsResult() (string, error) {
	if a.runtime.store == nil {
		return marshalResult(map[string]any{"items": []crawlartifact.CacheSnapshot{}})
	}
	items, err := crawlartifact.DiscoverCacheSnapshots(a.runtime.store.UserDataDir(), nil)
	if err != nil {
		return "", err
	}
	return marshalResult(map[string]any{
		"items": items,
		"total": len(items),
	})
}

func (a *API) removeCrawlCacheSnapshotResult(payload map[string]any) (string, error) {
	if a.runtime.store == nil {
		return "", fmt.Errorf("settings store is not initialized")
	}
	removedCount, err := crawlartifact.RemoveCacheSnapshot(a.runtime.store.UserDataDir(), nonEmptyString(payload["cacheKey"]))
	if err != nil {
		return "", err
	}
	items, discoverErr := crawlartifact.DiscoverCacheSnapshots(a.runtime.store.UserDataDir(), nil)
	if discoverErr != nil {
		return "", discoverErr
	}
	return marshalResult(map[string]any{
		"removedCount": removedCount,
		"items":        items,
		"total":        len(items),
	})
}

func (a *API) clearCrawlCacheSnapshotsResult() (string, error) {
	if a.runtime.store == nil {
		return "", fmt.Errorf("settings store is not initialized")
	}
	clearedCount, err := crawlartifact.ClearAllCacheSnapshots(a.runtime.store.UserDataDir())
	if err != nil {
		return "", err
	}
	return marshalResult(map[string]any{
		"clearedCount": clearedCount,
		"items":        []crawlartifact.CacheSnapshot{},
		"total":        0,
	})
}
