package avsubscriptionv2

import (
	"testing"

	runtimepaths "javflow/internal/runtime"
)

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
