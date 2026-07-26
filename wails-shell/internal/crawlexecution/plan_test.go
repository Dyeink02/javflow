package crawlexecution

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildRunPlanForFreshStart(t *testing.T) {
	plan := BuildRunPlan(RunPlanOptions{
		ResumeExisting:     false,
		HasRestoreState:    false,
		PendingDetailCount: 0,
		SecondValidation:   false,
	})

	expected := []string{
		"boot",
		"queue_setup",
		"index_discovery",
		"queue_drain",
		"page_gap_recovery",
		"queue_gap_recovery",
		"detail_recovery",
		"final_drain",
	}

	if !reflect.DeepEqual(plan.PhaseKeys, expected) {
		t.Fatalf("unexpected phase keys: %#v", plan.PhaseKeys)
	}
	if plan.ResumePendingFirst {
		t.Fatalf("expected ResumePendingFirst=false")
	}
	if plan.InitialPhaseKey != "boot" {
		t.Fatalf("expected InitialPhaseKey=boot, got %q", plan.InitialPhaseKey)
	}
	if plan.FinalPhaseKey != "final_drain" {
		t.Fatalf("expected FinalPhaseKey=final_drain, got %q", plan.FinalPhaseKey)
	}
	if plan.StopRedirectPhaseKey != "final_drain" {
		t.Fatalf("expected StopRedirectPhaseKey=final_drain, got %q", plan.StopRedirectPhaseKey)
	}
	expectedNext := map[string]string{
		"boot":               "queue_setup",
		"queue_setup":        "index_discovery",
		"index_discovery":    "queue_drain",
		"queue_drain":        "page_gap_recovery",
		"page_gap_recovery":  "queue_gap_recovery",
		"queue_gap_recovery": "detail_recovery",
		"detail_recovery":    "final_drain",
	}
	if !reflect.DeepEqual(plan.NextPhaseByKey, expectedNext) {
		t.Fatalf("unexpected next phase map: %#v", plan.NextPhaseByKey)
	}
	if !strings.Contains(plan.LogMessage, "标准抓取计划") {
		t.Fatalf("unexpected log message: %q", plan.LogMessage)
	}
}

func TestBuildRunPlanForResumeWithPendingRecovery(t *testing.T) {
	plan := BuildRunPlan(RunPlanOptions{
		ResumeExisting:     true,
		HasRestoreState:    true,
		PendingDetailCount: 3,
		SecondValidation:   true,
	})

	expected := []string{
		"boot",
		"queue_setup",
		"resume_pending",
		"index_discovery",
		"queue_drain",
		"page_gap_recovery",
		"queue_gap_recovery",
		"detail_recovery",
		"second_validation",
		"final_drain",
	}

	if !reflect.DeepEqual(plan.PhaseKeys, expected) {
		t.Fatalf("unexpected phase keys: %#v", plan.PhaseKeys)
	}
	if !plan.ResumePendingFirst {
		t.Fatalf("expected ResumePendingFirst=true")
	}
	if got := plan.NextPhaseByKey["resume_pending"]; got != "index_discovery" {
		t.Fatalf("expected resume_pending -> index_discovery, got %q", got)
	}
	if got := plan.NextPhaseByKey["detail_recovery"]; got != "second_validation" {
		t.Fatalf("expected detail_recovery -> second_validation, got %q", got)
	}
	if got := plan.NextPhaseByKey["second_validation"]; got != "final_drain" {
		t.Fatalf("expected second_validation -> final_drain, got %q", got)
	}
	if !strings.Contains(plan.LogMessage, "先补 3 条未完成详情") {
		t.Fatalf("unexpected log message: %q", plan.LogMessage)
	}
}

func TestBuildRunPlanForResumeWithoutPendingRecovery(t *testing.T) {
	plan := BuildRunPlan(RunPlanOptions{
		ResumeExisting:     true,
		HasRestoreState:    true,
		PendingDetailCount: 0,
		SecondValidation:   false,
	})

	for _, key := range plan.PhaseKeys {
		if key == "resume_pending" {
			t.Fatalf("did not expect resume_pending in phase keys: %#v", plan.PhaseKeys)
		}
	}
	if plan.ResumePendingFirst {
		t.Fatalf("expected ResumePendingFirst=false")
	}
	if got := plan.NextPhaseByKey["queue_drain"]; got != "page_gap_recovery" {
		t.Fatalf("expected queue_drain -> page_gap_recovery, got %q", got)
	}
	if !strings.Contains(plan.LogMessage, "未发现待补详情") {
		t.Fatalf("unexpected log message: %q", plan.LogMessage)
	}
}
