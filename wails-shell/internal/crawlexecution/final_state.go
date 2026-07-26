package crawlexecution

import (
	"fmt"
	"strings"
)

// Final-state synthesis converts reconciliation/recovery outcomes into the
// controller-visible terminal status and operator-facing summary message.
//
// Ownership summary:
// 1) synthesize terminal crawl status from reconciliation and recovery inputs
// 2) shape operator-facing completion/incomplete summary messages
// 3) keep terminal-state policy separate from live runner orchestration
//
// File map for maintainers:
// 1) terminal state input/output DTOs
// 2) final status synthesis helpers
// 3) completion shortfall and summary message builders

type FinalStateInput struct {
	UnresolvedCount         int
	QueueGapCount           int
	ProcessedGapCount       int
	FailedCount             int
	LowConfidencePageCount  int
	DuplicateExpectedCount  int
	DuplicateItemIDs        []string
	DuplicateItemSummary    string
	UnfinishedItems         []string
	ExpectedEntryCount      int
	RawDuplicateEntryCount  int
	DuplicateSummary        string
	ConfiguredTargetCount   int
	ValidationPassed        bool
	SecondValidationEnabled bool
	CompletedCount          int
	SkippedByPolicyCount    int
	ExpectedUniqueCount     int
}

type FinalState struct {
	Status  string
	Message string
}

func BuildFinalState(input FinalStateInput) FinalState {
	targetShortfall := 0
	if input.ConfiguredTargetCount > 0 {
		targetShortfall = maxInt(input.ConfiguredTargetCount-input.ExpectedEntryCount, 0)
	}

	completionTargetCount := input.ExpectedUniqueCount
	if input.ConfiguredTargetCount > 0 {
		completionTargetCount = maxInt(input.ConfiguredTargetCount-input.RawDuplicateEntryCount, 0)
	}

	resolvedCount := input.CompletedCount + input.SkippedByPolicyCount
	completionShortfall := maxInt(completionTargetCount-resolvedCount, 0)

	hasGap := input.UnresolvedCount > 0 ||
		input.QueueGapCount > 0 ||
		input.ProcessedGapCount > 0 ||
		input.FailedCount > 0 ||
		input.LowConfidencePageCount > 0 ||
		targetShortfall > 0 ||
		completionShortfall > 0 ||
		!input.ValidationPassed

	// completed / incomplete 仍然是内部状态机值；
	// 这里负责把“是否真正补齐目标”翻译成统一的对外中文文案。
	if !hasGap {
		return FinalState{
			Status:  "completed",
			Message: buildFinishedMessage(input),
		}
	}

	messages := make([]string, 0, 12)
	if input.ValidationPassed && input.SecondValidationEnabled {
	pushFinalStateMessage(&messages, "输出结果已通过二次校验，但目标条数仍未补齐")
	}

	if targetShortfall > 0 {
		if input.RawDuplicateEntryCount > 0 && input.DuplicateSummary != "" {
			pushFinalStateMessage(
				&messages,
				fmt.Sprintf(
					"站点原始分页仅解析到 %d 条，较目标 %d 条少 %d 条；其中重复番号 %d 条（%s）",
					input.ExpectedEntryCount,
					input.ConfiguredTargetCount,
					targetShortfall,
					input.RawDuplicateEntryCount,
					input.DuplicateSummary,
				),
			)
		} else {
			pushFinalStateMessage(
				&messages,
				fmt.Sprintf(
					"站点原始分页仅解析到 %d 条，较目标 %d 条少 %d 条",
					input.ExpectedEntryCount,
					input.ConfiguredTargetCount,
					targetShortfall,
				),
			)
		}
	} else if input.RawDuplicateEntryCount > 0 && input.DuplicateSummary != "" {
		pushFinalStateMessage(
			&messages,
			fmt.Sprintf("站点原始分页存在 %d 条重复番号（%s）", input.RawDuplicateEntryCount, input.DuplicateSummary),
		)
	}

	if completionShortfall > 0 {
		if input.ConfiguredTargetCount > 0 {
			skipHint := ""
			if input.SkippedByPolicyCount > 0 {
				skipHint = fmt.Sprintf("，按配置跳过 %d 条", input.SkippedByPolicyCount)
			}
			pushFinalStateMessage(
				&messages,
				fmt.Sprintf(
					"按唯一番号计算理论应完成 %d 条，当前已完成 %d 条%s，仍少 %d 条",
					completionTargetCount,
					input.CompletedCount,
					skipHint,
					completionShortfall,
				),
			)
		} else {
			pushFinalStateMessage(&messages, fmt.Sprintf("完成结果仍比理论目标少 %d 条", completionShortfall))
		}
	} else if input.SkippedByPolicyCount > 0 {
			pushFinalStateMessage(&messages, fmt.Sprintf("已按当前配置跳过 %d 条无磁力影片", input.SkippedByPolicyCount))
	}

	if len(input.UnfinishedItems) > 0 {
		preview := joinPreviewItems(input.UnfinishedItems, 6)
		previewSuffix := ""
		if preview != "" {
			extra := ""
			if len(input.UnfinishedItems) > 6 {
				extra = " 等"
			}
			previewSuffix = fmt.Sprintf("（%s%s）", preview, extra)
		}
		pushFinalStateMessage(
			&messages,
			fmt.Sprintf("已定位 %d 条未完成番号%s", len(input.UnfinishedItems), previewSuffix),
		)
	} else if input.UnresolvedCount > 0 || completionShortfall > 0 {
		pushFinalStateMessage(&messages, "剩余缺口暂未定位到具体番号，请结合分页缺口与失败详情页核对")
	}

	if input.QueueGapCount > 0 {
		pushFinalStateMessage(&messages, fmt.Sprintf("存在 %d 条入队缺口", input.QueueGapCount))
	}
	if input.ProcessedGapCount > 0 {
		pushFinalStateMessage(&messages, fmt.Sprintf("存在 %d 条已处理但未落盘项目", input.ProcessedGapCount))
	}
	if input.FailedCount > 0 {
		pushFinalStateMessage(&messages, fmt.Sprintf("存在 %d 条失败详情页", input.FailedCount))
	}
	if input.LowConfidencePageCount > 0 {
		pushFinalStateMessage(&messages, fmt.Sprintf("存在 %d 个低可信分页", input.LowConfidencePageCount))
	}
	if !input.ValidationPassed {
		pushFinalStateMessage(&messages, "输出结果二次校验未通过")
	}
	if len(input.DuplicateItemIDs) > 0 {
		pushFinalStateMessage(
			&messages,
			fmt.Sprintf("发现 %d 条重复番号（%s）", len(input.DuplicateItemIDs), input.DuplicateItemSummary),
		)
	} else if input.DuplicateExpectedCount > 0 && input.RawDuplicateEntryCount == 0 {
		pushFinalStateMessage(&messages, fmt.Sprintf("发现 %d 条重复分页编号", input.DuplicateExpectedCount))
	}

	return FinalState{
		Status:  "incomplete",
		Message: "任务未完成：" + joinMessages(messages),
	}
}

func buildFinishedMessage(input FinalStateInput) string {
	validationText := ""
	if input.SecondValidationEnabled {
		validationText = "，已二次校验完成"
	}
	skippedText := buildSkippedMessage(input.SkippedByPolicyCount)

	if input.RawDuplicateEntryCount > 0 {
		return fmt.Sprintf(
			"抓取任务已完成%s。站点原始条目 %d 条，其中重复番号 %d 条（%s），按唯一番号完成 %d 条%s。",
			validationText,
			input.ExpectedEntryCount,
			input.RawDuplicateEntryCount,
			input.DuplicateSummary,
			input.ExpectedUniqueCount,
			skippedText,
		)
	}

	return fmt.Sprintf("抓取任务已完成%s%s。", validationText, skippedText)
}

func buildSkippedMessage(skippedByPolicyCount int) string {
	if skippedByPolicyCount <= 0 {
		return ""
	}
	return fmt.Sprintf("；按当前配置跳过无磁力影片 %d 条", skippedByPolicyCount)
}

func pushFinalStateMessage(target *[]string, message string) {
	if target == nil {
		return
	}
	if normalized := strings.TrimSpace(message); normalized != "" {
		*target = append(*target, normalized)
	}
}

func joinMessages(messages []string) string {
	if len(messages) == 0 {
		return "存在未收敛的抓取缺口。"
	}
	return strings.Join(messages, "；") + "。"
}

func joinPreviewItems(items []string, limit int) string {
	if len(items) == 0 || limit <= 0 {
		return ""
	}

	if len(items) < limit {
		limit = len(items)
	}
	return strings.Join(items[:limit], "、")
}
