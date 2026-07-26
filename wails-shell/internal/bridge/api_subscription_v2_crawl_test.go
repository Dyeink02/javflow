package bridge

import "testing"

func TestResolveSubscriptionCrawlPagesUsesDetectedStopPage(t *testing.T) {
	if got := resolveSubscriptionCrawlPages(1, 1, 1); got != 1 {
		t.Fatalf("expected one detected page for one pending code, got %d", got)
	}
	if got := calcSubscriptionCrawlLimit(1, 30); got != 30 {
		t.Fatalf("expected one-page subscription crawl to grab 30 slots, got %d", got)
	}
	if got := resolveSubscriptionCrawlPages(3, 1, 1); got != 3 {
		t.Fatalf("expected detection stop page to widen crawl scope, got %d", got)
	}
	if got := calcSubscriptionCrawlLimit(3, 30); got != 90 {
		t.Fatalf("expected three-page subscription crawl to grab 90 slots, got %d", got)
	}
}

func TestDescribeSubscriptionTargetCodes(t *testing.T) {
	if got := describeSubscriptionTargetCodes([]string{"FWAY-087", "MIDA-438"}); got != "FWAY-087、MIDA-438" {
		t.Fatalf("unexpected normalized target-code description: %q", got)
	}
	if got := describeSubscriptionTargetCodes(nil); got != "未指定，保留全部输出" {
		t.Fatalf("unexpected empty target-code description: %q", got)
	}
}
