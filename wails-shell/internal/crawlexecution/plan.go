package crawlexecution

import "fmt"

// plan.go owns high-level crawl phase-plan construction before the state
// machine starts executing.
//
// If execution order looks wrong, inspect this file before the runner's phase
// handlers. It is the plan-level contract for resume and validation behavior.
//
// Ownership summary:
// 1) build the ordered crawl phase plan from resume/validation intent
// 2) centralize phase ordering and stop-redirect decisions
// 3) keep plan construction separate from phase execution handlers
//
// File map for maintainers:
// 1) run-plan input/output DTOs
// 2) phase ordering and redirect selection helpers
// 3) resume-first and validation-aware plan assembly

// RunPlanOptions is the narrow input used to build one execution-phase plan.
type RunPlanOptions struct {
	ResumeExisting     bool
	HasRestoreState    bool
	PendingDetailCount int
	SecondValidation   bool
}

// RunPlan is the ordered phase contract consumed by the generic state machine
// and runner orchestration.
type RunPlan struct {
	Source                  string            `json:"source"`
	PhaseKeys               []string          `json:"phaseKeys"`
	NextPhaseByKey          map[string]string `json:"nextPhaseByKey,omitempty"`
	StopRedirectPhaseKey    string            `json:"stopRedirectPhaseKey,omitempty"`
	InitialPhaseKey         string            `json:"initialPhaseKey,omitempty"`
	FinalPhaseKey           string            `json:"finalPhaseKey,omitempty"`
	ResumePendingFirst      bool              `json:"resumePendingFirst"`
	HasRestoreState         bool              `json:"hasRestoreState"`
	PendingDetailCount      int               `json:"pendingDetailCount"`
	SecondValidationEnabled bool              `json:"secondValidationEnabled"`
	LogMessage              string            `json:"logMessage,omitempty"`
}

// BuildRunPlan turns resume/validation intent into one ordered phase contract.
func BuildRunPlan(options RunPlanOptions) RunPlan {
	pendingDetailCount := options.PendingDetailCount
	if pendingDetailCount < 0 {
		pendingDetailCount = 0
	}

	resumePendingFirst := options.ResumeExisting && options.HasRestoreState && pendingDetailCount > 0

	phaseKeys := []string{
		"boot",
		"queue_setup",
	}
	if resumePendingFirst {
		phaseKeys = append(phaseKeys, "resume_pending")
	}
	phaseKeys = append(
		phaseKeys,
		"index_discovery",
		"queue_drain",
		"page_gap_recovery",
		"queue_gap_recovery",
		"detail_recovery",
	)
	if options.SecondValidation {
		phaseKeys = append(phaseKeys, "second_validation")
	}
	phaseKeys = append(phaseKeys, "final_drain")

	transitionPlan := NormalizePhaseTransitionPlan(PhaseTransitionPlan{
		PhaseKeys: phaseKeys,
	})

	plan := RunPlan{
		Source:                  "go",
		PhaseKeys:               transitionPlan.PhaseKeys,
		NextPhaseByKey:          transitionPlan.NextPhaseByKey,
		StopRedirectPhaseKey:    transitionPlan.StopRedirectPhaseKey,
		InitialPhaseKey:         transitionPlan.InitialPhaseKey,
		FinalPhaseKey:           transitionPlan.FinalPhaseKey,
		ResumePendingFirst:      resumePendingFirst,
		HasRestoreState:         options.HasRestoreState,
		PendingDetailCount:      pendingDetailCount,
		SecondValidationEnabled: options.SecondValidation,
	}

	switch {
	case resumePendingFirst:
		plan.LogMessage = fmt.Sprintf("Go 已生成恢复启动计划：先补 %d 条未完成详情，再继续索引抓取。", pendingDetailCount)
	case options.ResumeExisting && options.HasRestoreState:
		plan.LogMessage = "Go 已生成恢复启动计划：未发现待补详情，直接进入索引抓取。"
	case options.ResumeExisting:
		plan.LogMessage = "Go 已生成继续抓取计划：未发现可恢复快照，直接进入索引抓取。"
	default:
		plan.LogMessage = "Go 已生成标准抓取计划。"
	}

	return plan
}
