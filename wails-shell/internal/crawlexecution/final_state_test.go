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
