package crawlexecution

import (
	"reflect"
	"testing"
)

func TestClassifyDetailFailure(t *testing.T) {
	if policy := ClassifyDetailFailure("Cloudflare challenge 403"); policy.Key != "blocked" {
		t.Fatalf("expected blocked policy, got %#v", policy)
	}
	if policy := ClassifyDetailFailure("用户主动终止"); policy.Key != "stopped" {
		t.Fatalf("expected stopped policy, got %#v", policy)
	}
}

func TestGetDetailRecoveryBudget(t *testing.T) {
	blockedPolicy := ClassifyDetailFailure("age verification")
	unknownPolicy := ClassifyDetailFailure("other")

	if budget := GetDetailRecoveryBudget(blockedPolicy, false); budget != 4 {
		t.Fatalf("expected blocked budget 4, got %d", budget)
	}
	if budget := GetDetailRecoveryBudget(blockedPolicy, true); budget != 3 {
		t.Fatalf("expected blocked large-task budget 3, got %d", budget)
	}
	if budget := GetDetailRecoveryBudget(unknownPolicy, true); budget != 2 {
		t.Fatalf("expected unknown large-task budget 2, got %d", budget)
	}
}

func TestBuildRecoveryCategorySummary(t *testing.T) {
	summary := BuildRecoveryCategorySummary([]DetailRecoverySummaryEntry{
		{Reason: "Cloudflare challenge"},
		{Reason: "timeout"},
		{Reason: "timeout"},
	})

	if summary != "验证拦截 1 条，网络超时 2 条" {
		t.Fatalf("unexpected summary %q", summary)
	}
}

func TestGetRecoverableMissingDetailLinks(t *testing.T) {
	links := GetRecoverableMissingDetailLinks([]DetailRecoveryCandidate{
		{Link: "c", Reason: "parse metadata", RetryCount: 3, RecoveryAttempts: 1, HasFailureRecord: true},
		{Link: "b", Reason: "Cloudflare challenge", RetryCount: 1, RecoveryAttempts: 0, HasFailureRecord: true},
		{Link: "a", Reason: "timeout", RetryCount: 1, RecoveryAttempts: 1, HasFailureRecord: true},
		{Link: "d", Reason: "用户主动终止", RetryCount: 0, RecoveryAttempts: 0, HasFailureRecord: true},
	}, false)

	if !reflect.DeepEqual(links, []string{"b", "a"}) {
		t.Fatalf("unexpected recoverable links %#v", links)
	}
}
