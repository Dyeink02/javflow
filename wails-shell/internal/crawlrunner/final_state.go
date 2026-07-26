package crawlrunner

import (
	"fmt"
	"strings"

	"javflow/internal/common"
	"javflow/internal/crawlexecution"
)

// final_state.go bridges crawlexecution final-state rules into runner-specific
// status/output payloads.
//
// Keep this file focused on the translation layer between generic final-state
// math and runner-owned status enums/messages.
//
// Ownership summary:
// 1) translate generic crawlexecution final-state output into runner status/messages
// 2) keep runner-owned terminal wording and enum mapping centralized
// 3) separate final-state translation from final-state math itself
//
// File map for maintainers:
// 1) final-state input DTOs
// 2) runner status/message translation helpers
// 3) duplicate/unresolved summary wording helpers

type FinalStateInput struct {
	UnresolvedCount           int
	QueueGapCount             int
	ProcessedGapCount         int
	FailedCount               int
	LowConfidencePageCount    int
	DuplicateExpectedCount    int
	DuplicateItemIDs          []string
	DuplicateItemSummary      string
	UnfinishedItems           []string
	ExpectedEntryCount        int
	RawDuplicateEntryCount    int
	DuplicateSummary          string
	ConfiguredTargetCount     int
	ValidationPassed          bool
	SecondValidationEnabled   bool
	CompletedCount            int
	SkippedByPolicyCount      int
	ExpectedUniqueCount       int
}

type FinalStateOutput struct {
	Status  RunnerStatus `json:"status"`
	Message string       `json:"message"`
}

// BuildFinalState converts the generic final-state rule result into runner
// status/message values.
func BuildFinalState(input FinalStateInput) FinalStateOutput {
	result := crawlexecution.BuildFinalState(crawlexecution.FinalStateInput{
		UnresolvedCount:         input.UnresolvedCount,
		QueueGapCount:           input.QueueGapCount,
		ProcessedGapCount:       input.ProcessedGapCount,
		FailedCount:             input.FailedCount,
		LowConfidencePageCount:  input.LowConfidencePageCount,
		DuplicateExpectedCount:  input.DuplicateExpectedCount,
		DuplicateItemIDs:        input.DuplicateItemIDs,
		DuplicateItemSummary:    input.DuplicateItemSummary,
		UnfinishedItems:         input.UnfinishedItems,
		ExpectedEntryCount:      input.ExpectedEntryCount,
		RawDuplicateEntryCount:  input.RawDuplicateEntryCount,
		DuplicateSummary:        input.DuplicateSummary,
		ConfiguredTargetCount:   input.ConfiguredTargetCount,
		ValidationPassed:        input.ValidationPassed,
		SecondValidationEnabled: input.SecondValidationEnabled,
		CompletedCount:          input.CompletedCount,
		SkippedByPolicyCount:    input.SkippedByPolicyCount,
		ExpectedUniqueCount:     input.ExpectedUniqueCount,
	})

	status := StatusIncomplete
	if result.Status == "completed" {
		status = StatusCompleted
	}

	return FinalStateOutput{
		Status:  status,
		Message: result.Message,
	}
}

// BuildDuplicateSummary keeps duplicate reporting short and human readable.
func BuildDuplicateSummary(groups []DuplicateGroup, limit int) string {
	if len(groups) == 0 {
		return ""
	}
	preview := groups[:common.MinInt(limit, len(groups))]
	ids := make([]string, len(preview))
	for i, g := range preview {
		ids[i] = g.ItemID
	}
	result := strings.Join(ids, "\u3001")
	if len(groups) > limit {
		result += fmt.Sprintf(" 等 %d 个番号", len(groups))
	}
	return result
}
