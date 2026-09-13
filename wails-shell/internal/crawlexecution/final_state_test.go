package crawlexecution

import (
	"strings"
	"testing"
)

func TestBuildFinalStateReturnsCompletedWhenNoGap(t *testing.T) {
	result := BuildFinalState(FinalStateInput{
		ExpectedEntryCount:      250,
		ExpectedUniqueCount:     250,
		ValidationPassed:        true,
		SecondValidationEnabled: true,
		CompletedCount:          250,
	})

	if result.Status != "completed" {
		t.Fatalf("expected completed, got %q", result.Status)
	}
	if !strings.Contains(result.Message, "抓取任务已完成") {
		t.Fatalf("unexpected message: %q", result.Message)
	}
	if !strings.Contains(result.Message, "已二次校验完成") {
		t.Fatalf("expected validation hint, got %q", result.Message)
	}
}

func TestBuildFinalStateReturnsIncompleteWhenGapExists(t *testing.T) {
	result := BuildFinalState(FinalStateInput{
		ExpectedEntryCount:     250,
		ExpectedUniqueCount:    240,
		ConfiguredTargetCount:  250,
		ValidationPassed:       false,
		CompletedCount:         230,
		UnfinishedItems:        []string{"ABP-001", "ABP-002", "ABP-003"},
		QueueGapCount:          2,
		FailedCount:            1,
		RawDuplicateEntryCount: 4,
		DuplicateSummary:       "示例摘要",
		DuplicateItemIDs:       []string{"ABP-009"},
		DuplicateItemSummary:   "ABP-009 x2",
	})

	if result.Status != "incomplete" {
		t.Fatalf("expected incomplete, got %q", result.Status)
	}
	expectedSnippets := []string{
		"任务未完成：",
		"已定位 3 条未完成番号",
		"存在 2 条入队缺口",
		"存在 1 条失败详情页",
		"输出结果二次校验未通过",
	}
	for _, snippet := range expectedSnippets {
		if !strings.Contains(result.Message, snippet) {
			t.Fatalf("expected %q in message %q", snippet, result.Message)
		}
	}
}

// TestBuildFinalStateExcludesConfigFilteredFromGaps reproduces the operator
// scenario: 目标 358、站点原始 357（重复 21）、番号过滤 85、完成 251。
// 过滤是主动排除：不应再报“仍少 86 条”，任务应判定完成。
func TestBuildFinalStateExcludesConfigFilteredFromGaps(t *testing.T) {
	result := BuildFinalState(FinalStateInput{
		UnresolvedCount:         0,
		QueueGapCount:           0,
		FailedCount:             0,
		DuplicateItemIDs:        []string{"DV-1195", "DV-1225"},
		DuplicateItemSummary:    "DV-1195、DV-1225 等 21 个番号",
		ExpectedEntryCount:      357,
		RawDuplicateEntryCount:  21,
		DuplicateSummary:        "DV-1195、DV-1225 等 21 个番号",
		ConfiguredTargetCount:   358,
		ValidationPassed:        true,
		SecondValidationEnabled: true,
		CompletedCount:          251,
		SkippedByPolicyCount:    0,
		ExpectedUniqueCount:     336,
		ConfigFilteredCount:     85,
	})

	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed（过滤不再是缺口）", result.Status)
	}
	if strings.Contains(result.Message, "仍少") {
		t.Fatalf("message should not report shortfall: %q", result.Message)
	}
	if !strings.Contains(result.Message, "按当前配置过滤 85 条") {
		t.Fatalf("message should state filtered count: %q", result.Message)
	}
}

// TestBuildFinalStateStillReportsRealShortfall keeps genuine gaps visible:
// 完成少于有效目标时必须如实报告缺口数量。
func TestBuildFinalStateStillReportsRealShortfall(t *testing.T) {
	result := BuildFinalState(FinalStateInput{
		ExpectedEntryCount:      357,
		RawDuplicateEntryCount:  21,
		ConfiguredTargetCount:   358,
		ValidationPassed:        true,
		SecondValidationEnabled: true,
		CompletedCount:          250,
		SkippedByPolicyCount:    0,
		ExpectedUniqueCount:     336,
		ConfigFilteredCount:     85,
	})

	if result.Status != "incomplete" {
		t.Fatalf("status = %q, want incomplete", result.Status)
	}
	if !strings.Contains(result.Message, "仍少 1 条") {
		t.Fatalf("message should report exactly 1 shortfall: %q", result.Message)
	}
	if !strings.Contains(result.Message, "按当前配置过滤 85 条") {
		t.Fatalf("message should state filtered count: %q", result.Message)
	}
}
