package bridge

import (
	"testing"

	"javflow/internal/contracts/subscriptiontarget"
)

func TestBuildSubscriptionFromProfilePrefersFillCountForCurrentCount(t *testing.T) {
	profile := subscriptiontarget.TargetProfile{
		ActressName:         "瀬戸環奈",
		ResolvedActressName: "瀬戸環奈",
		ResolvedBase:        "https://www.javbus.com/star/138y",
		LookupBaseOrigin:    "https://www.javbus.com",
		MagnetCount:         28,
		AllCount:            59,
		FillCount:           28,
		PreferredCount:      28,
		ItemsPerPage:        28,
		TotalPages:          1,
	}

	item := buildSubscriptionFromProfile(map[string]any{
		"actressName": "瀬戸環奈",
		"targetUrl":   "https://www.javbus.com/star/138y",
		"syncedCount": 27,
		"source":      "manual",
	}, profile)

	if item.CurrentCount != 28 {
		t.Fatalf("expected currentCount=28, got %d", item.CurrentCount)
	}
	if item.PendingCount != 0 && item.PendingCount != 1 {
		t.Fatalf("unexpected pendingCount=%d", item.PendingCount)
	}
}
