package crawlrunner

// phases.go owns the runner-local phase-key catalog and execution-plan shape
// used after a crawl plan has been selected.
//
// Ownership summary:
// 1) define the runner-local phase-key catalog and default execution plan
// 2) keep runner-facing phase metadata aligned with crawl plan selection
// 3) separate runner phase contracts from generic crawlexecution planning
//
// File map for maintainers:
// 1) default phase-key catalog
// 2) known-phase lookup and execution-plan DTO
// 3) next-phase resolution helpers

var DefaultPhaseKeys = []PhaseKey{
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

var knownPhases = func() map[PhaseKey]bool {
	m := map[PhaseKey]bool{}
	for _, k := range DefaultPhaseKeys {
		m[k] = true
	}
	return m
}()

// ExecutionPlan mirrors the high-level jav-scrapy crawl pipeline while giving
// the Wails UI stable phase names for progress panels. Some recovery phases are
// currently lightweight reconciliation steps; keep this plan explicit so those
// phases can be made fully active without changing the frontend contract.
type ExecutionPlan struct {
	PhaseKeys           []PhaseKey              `json:"phaseKeys"`
	NextPhaseByKey      map[PhaseKey]PhaseKey   `json:"nextPhaseByKey"`
	InitialPhaseKey     PhaseKey                `json:"initialPhaseKey"`
	FinalPhaseKey       PhaseKey                `json:"finalPhaseKey"`
	StopRedirectPhaseKey PhaseKey               `json:"stopRedirectPhaseKey"`
}

func DefaultExecutionPlan() ExecutionPlan {
	next := map[PhaseKey]PhaseKey{}
	for i := 0; i < len(DefaultPhaseKeys)-1; i++ {
		next[DefaultPhaseKeys[i]] = DefaultPhaseKeys[i+1]
	}
	return ExecutionPlan{
		PhaseKeys:            DefaultPhaseKeys,
		NextPhaseByKey:       next,
		InitialPhaseKey:      "boot",
		FinalPhaseKey:        "final_drain",
		StopRedirectPhaseKey: "final_drain",
	}
}

func (p ExecutionPlan) ResolveNextPhase(current PhaseKey, isStopping bool) PhaseKey {
	if isStopping {
		if v, ok := p.NextPhaseByKey[p.StopRedirectPhaseKey]; ok && v != "" {
			return v
		}
		return p.StopRedirectPhaseKey
	}
	if v, ok := p.NextPhaseByKey[current]; ok && v != "" {
		return v
	}
	return p.FinalPhaseKey
}
