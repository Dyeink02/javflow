package avsubscriptionv2

import (
	"os"
	"path/filepath"
	"testing"

	runtimepaths "javflow/internal/runtime"
)

func TestApplyRefreshResultsPreservesConcurrentUserFields(t *testing.T) {
	service := NewService(runtimepaths.Paths{UserData: t.TempDir()}, nil)
	created, err := service.Upsert(Subscription{
		ActressName:        "テスト女優",
		CrawlURL:           "https://www.javbus.com/star/test",
		BaselineCodes:      []string{"AAA-001"},
		PreferredOutputDir: "D:/original-output",
		AvatarURL:          "/subscription-media/test/avatar.jpg",
		PhotoURLs:          []string{"/subscription-media/test/photo.jpg"},
	})
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	if _, err := service.Patch(created.ID, map[string]any{"preferredOutputDir": "D:/user-changed-output"}); err != nil {
		t.Fatalf("simulate concurrent user patch: %v", err)
	}
	refreshed := created
	refreshed.CurrentObservedCount = 12
	refreshed.PendingCodes = []string{"BBB-002"}
	refreshed.PendingCount = 1
	refreshed.Status = statusUpdated
	refreshed.LastCheckedAt = "2026-08-12T00:00:00Z"

	items, err := service.ApplyRefreshResults([]Subscription{refreshed})
	if err != nil {
		t.Fatalf("apply refresh results: %v", err)
	}
	index := findSubscriptionIndexByID(items, created.ID)
	if index < 0 {
		t.Fatal("subscription disappeared after refresh merge")
	}
	got := items[index]
	if got.PreferredOutputDir != "D:/user-changed-output" {
		t.Fatalf("refresh overwrote concurrent output change: %q", got.PreferredOutputDir)
	}
	if got.AvatarURL != created.AvatarURL || len(got.PhotoURLs) != 1 {
		t.Fatalf("refresh overwrote cached media: %+v", got)
	}
	if got.CurrentObservedCount != 12 || len(got.PendingCodes) != 1 || got.PendingCodes[0] != "BBB-002" {
		t.Fatalf("refresh observation was not retained: %+v", got)
	}
}

func TestListPreservesUnreadableStateInsteadOfTreatingItAsEmpty(t *testing.T) {
	userData := t.TempDir()
	service := NewService(runtimepaths.Paths{UserData: userData}, nil)
	storagePath := filepath.Join(userData, storageDirName, storageFileName)
	if err := os.MkdirAll(filepath.Dir(storagePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storagePath, []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := service.List(); err == nil {
		t.Fatal("expected unreadable subscription state to be reported")
	}
	backups, err := filepath.Glob(storagePath + ".corrupt-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected one preserved corrupt state file, got %v", backups)
	}
}

func TestReorderPersistsAcrossLaterSubscriptionUpdates(t *testing.T) {
	service := NewService(runtimepaths.Paths{UserData: t.TempDir()}, nil)
	created := make([]Subscription, 0, 3)
	for _, name := range []string{"演员一", "演员二", "演员三"} {
		item, err := service.Upsert(Subscription{ActressName: name, CrawlURL: "https://example.test/star/" + name})
		if err != nil {
			t.Fatal(err)
		}
		created = append(created, item)
	}

	reordered, err := service.Reorder([]string{created[2].ID, created[0].ID, created[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	if reordered[0].ID != created[2].ID || reordered[0].SortOrder != 1 {
		t.Fatalf("unexpected first reordered item: %+v", reordered[0])
	}

	updated := reordered[2]
	updated.PendingCodes = []string{"ABC-001"}
	updated.PendingCount = 1
	if _, err := service.Upsert(updated); err != nil {
		t.Fatal(err)
	}

	loaded, err := service.List()
	if err != nil {
		t.Fatal(err)
	}
	if loaded[0].ID != created[2].ID || loaded[1].ID != created[0].ID || loaded[2].ID != created[1].ID {
		t.Fatalf("saved user order was lost after update: %+v", loaded)
	}
}

func TestSetMediaKeepsSortOrder(t *testing.T) {
	service := NewService(runtimepaths.Paths{UserData: t.TempDir()}, nil)
	item, err := service.Upsert(Subscription{ActressName: "演员", CrawlURL: "https://example.test/star/a"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.SetMedia(item.ID, "file:///C:/App/subscriptions-v2/media/actor/avatar.jpg", []string{"file:///C:/App/subscriptions-v2/media/actor/avatar.jpg", "file:///C:/App/subscriptions-v2/media/actor/photo.jpg"}, "2026-07-19T12:00:00+08:00")
	if err != nil {
		t.Fatal(err)
	}
	if updated.SortOrder != 1 || updated.AvatarURL == "" || len(updated.PhotoURLs) != 2 {
		t.Fatalf("unexpected media update: %+v", updated)
	}
	if updated.AvatarURL != "/subscription-media/actor/avatar.jpg" {
		t.Fatalf("legacy file URL was not converted to an application URL: %q", updated.AvatarURL)
	}
}

func TestPatchPersistsActressCountFilterThreshold(t *testing.T) {
	service := NewService(runtimepaths.Paths{UserData: t.TempDir()}, nil)
	item, err := service.Upsert(Subscription{ActressName: "Filter Actor", CrawlURL: "https://example.test/star/filter"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.Patch(item.ID, map[string]any{"actressCountFilterThreshold": 18})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ActressCountFilterThreshold != 18 {
		t.Fatalf("unexpected filter threshold: %d", updated.ActressCountFilterThreshold)
	}
	loaded, err := service.List()
	if err != nil {
		t.Fatal(err)
	}
	if loaded[0].ActressCountFilterThreshold != 18 {
		t.Fatalf("filter threshold was not persisted: %+v", loaded[0])
	}
	cleared, err := service.Patch(item.ID, map[string]any{"actressCountFilterThreshold": 0})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.ActressCountFilterThreshold != 0 {
		t.Fatalf("threshold 0 must disable the filter: %d", cleared.ActressCountFilterThreshold)
	}
}

func TestPatchActressCountFilterThresholdForAllPersistsEverySubscription(t *testing.T) {
	service := NewService(runtimepaths.Paths{UserData: t.TempDir()}, nil)
	for _, item := range []Subscription{
		{ActressName: "Filter Actor A", CrawlURL: "https://example.test/star/filter-a"},
		{ActressName: "Filter Actor B", CrawlURL: "https://example.test/star/filter-b"},
	} {
		if _, err := service.Upsert(item); err != nil {
			t.Fatal(err)
		}
	}

	updated, err := service.PatchActressCountFilterThresholdForAll(18)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != 2 {
		t.Fatalf("expected two updated subscriptions, got %d", len(updated))
	}
	for _, item := range updated {
		if item.ActressCountFilterThreshold != 18 {
			t.Fatalf("unexpected bulk threshold: %+v", item)
		}
	}

	loaded, err := service.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range loaded {
		if item.ActressCountFilterThreshold != 18 {
			t.Fatalf("bulk threshold was not persisted: %+v", item)
		}
	}
}
