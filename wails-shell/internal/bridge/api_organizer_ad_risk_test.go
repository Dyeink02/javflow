package bridge

import (
	"testing"

	"javflow/internal/organizer"
)

func TestConfigureOrganizerAdRiskDoesNotFallbackToSidecarWhenLocalAdLearningMissing(t *testing.T) {
	api := &API{}
	options := &organizer.RunOptions{
		AdDetectionEnabled: true,
		DryRun:             false,
	}

	api.configureOrganizerAdRisk(options, "test-task")

	if options.EvaluateAdRisk != nil {
		t.Fatalf("expected EvaluateAdRisk to stay nil when local Go adlearning is unavailable")
	}
}

func TestConfigureOrganizerAdRiskSkipsDryRun(t *testing.T) {
	api := &API{}
	options := &organizer.RunOptions{
		AdDetectionEnabled: true,
		DryRun:             true,
	}

	api.configureOrganizerAdRisk(options, "test-task")

	if options.EvaluateAdRisk != nil {
		t.Fatalf("expected dry-run organizer task to skip ad-risk evaluator wiring")
	}
}
