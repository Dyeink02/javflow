package crawlexecution

import "strings"

// phase_transition.go owns normalized phase graph planning for the crawler
// state machine.
//
// Ownership summary:
// 1) normalize the crawler phase graph and next-phase mapping
// 2) validate/repair phase transition metadata for the state machine
// 3) keep phase graph planning separate from runtime state mutation
//
// File map for maintainers:
// 1) normalized phase transition DTOs
// 2) phase graph validation and repair helpers
// 3) initial/final/stop redirect resolution helpers

type PhaseTransitionPlan struct {
	PhaseKeys            []string          `json:"phaseKeys"`
	NextPhaseByKey       map[string]string `json:"nextPhaseByKey,omitempty"`
	StopRedirectPhaseKey string            `json:"stopRedirectPhaseKey,omitempty"`
	InitialPhaseKey      string            `json:"initialPhaseKey,omitempty"`
	FinalPhaseKey        string            `json:"finalPhaseKey,omitempty"`
}

func NormalizePhaseTransitionPlan(plan PhaseTransitionPlan) PhaseTransitionPlan {
	phaseKeys := NormalizePhaseKeys(plan.PhaseKeys)
	orderedPhaseSet := make(map[string]struct{}, len(phaseKeys))
	for _, key := range phaseKeys {
		orderedPhaseSet[key] = struct{}{}
	}

	nextPhaseByKey := make(map[string]string, len(phaseKeys))
	for index := 0; index < len(phaseKeys)-1; index++ {
		current := phaseKeys[index]
		next := phaseKeys[index+1]
		if current == "" || next == "" {
			continue
		}
		nextPhaseByKey[current] = next
	}

	for key, next := range plan.NextPhaseByKey {
		normalizedKey := normalizePhaseKeyForPlan(key, orderedPhaseSet)
		normalizedNext := normalizePhaseKeyForPlan(next, orderedPhaseSet)
		if normalizedKey == "" || normalizedNext == "" {
			continue
		}
		nextPhaseByKey[normalizedKey] = normalizedNext
	}

	initialPhaseKey := normalizePhaseKeyForPlan(plan.InitialPhaseKey, orderedPhaseSet)
	if initialPhaseKey == "" && len(phaseKeys) > 0 {
		initialPhaseKey = phaseKeys[0]
	}

	finalPhaseKey := normalizePhaseKeyForPlan(plan.FinalPhaseKey, orderedPhaseSet)
	if finalPhaseKey == "" && len(phaseKeys) > 0 {
		finalPhaseKey = phaseKeys[len(phaseKeys)-1]
	}

	stopRedirectPhaseKey := normalizePhaseKeyForPlan(plan.StopRedirectPhaseKey, orderedPhaseSet)
	if stopRedirectPhaseKey == "" {
		stopRedirectPhaseKey = finalPhaseKey
	}

	return PhaseTransitionPlan{
		PhaseKeys:            phaseKeys,
		NextPhaseByKey:       nextPhaseByKey,
		StopRedirectPhaseKey: stopRedirectPhaseKey,
		InitialPhaseKey:      initialPhaseKey,
		FinalPhaseKey:        finalPhaseKey,
	}
}

func ResolvePhaseTransitionNext(plan PhaseTransitionPlan, currentPhaseKey string, fallbackNextPhase string, isStopping bool) string {
	normalizedPlan := NormalizePhaseTransitionPlan(plan)
	if isStopping {
		if normalizedPlan.StopRedirectPhaseKey != "" {
			return normalizedPlan.StopRedirectPhaseKey
		}
		return "final_drain"
	}

	trimmedCurrentPhaseKey := strings.TrimSpace(currentPhaseKey)
	if trimmedCurrentPhaseKey != "" {
		if nextPhaseKey, ok := normalizedPlan.NextPhaseByKey[trimmedCurrentPhaseKey]; ok && nextPhaseKey != "" {
			return nextPhaseKey
		}
	}

	trimmedFallbackNextPhase := strings.TrimSpace(fallbackNextPhase)
	if trimmedFallbackNextPhase != "" {
		return trimmedFallbackNextPhase
	}

	if normalizedPlan.FinalPhaseKey != "" {
		return normalizedPlan.FinalPhaseKey
	}

	return "final_drain"
}

func ResolveStructuredPhaseKey(plan PhaseTransitionPlan, currentPhaseKey string, status string) string {
	normalizedPlan := NormalizePhaseTransitionPlan(plan)
	phaseLookup := make(map[string]struct{}, len(normalizedPlan.PhaseKeys))
	for _, phaseKey := range normalizedPlan.PhaseKeys {
		phaseLookup[phaseKey] = struct{}{}
	}

	trimmedCurrentPhaseKey := strings.TrimSpace(currentPhaseKey)
	if _, ok := phaseLookup[trimmedCurrentPhaseKey]; ok {
		return trimmedCurrentPhaseKey
	}

	switch NormalizeStatus(status, "") {
	case "completed", "incomplete", "stopped", "error":
		if normalizedPlan.FinalPhaseKey != "" {
			return normalizedPlan.FinalPhaseKey
		}
	case "stopping":
		if normalizedPlan.StopRedirectPhaseKey != "" {
			return normalizedPlan.StopRedirectPhaseKey
		}
	default:
		if normalizedPlan.InitialPhaseKey != "" {
			return normalizedPlan.InitialPhaseKey
		}
	}

	return "boot"
}

func normalizePhaseKeyForPlan(value string, orderedPhaseSet map[string]struct{}) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}

	if _, ok := orderedPhaseSet[trimmed]; !ok {
		return ""
	}

	return trimmed
}
