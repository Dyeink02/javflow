package crawlexecution

import "testing"

func TestResolveIndexValidationSampleProgressPromotesImprovedSample(t *testing.T) {
	result := ResolveIndexValidationSampleProgress(IndexValidationSampleProgressInput{
		PreviousBestCount:      24,
		MergedCount:            28,
		BestDiagnosticReason:   "",
		SampleDiagnosticReason: "sample improved",
	})

	if !result.ShouldPromoteMergedLinks || result.CurrentBestCount != 28 || result.BestDiagnosticReason != "sample improved" {
		t.Fatalf("unexpected sample progress: %#v", result)
	}
}

func TestResolveIndexValidationSampleProgressKeepsExistingBestDiagnostic(t *testing.T) {
	result := ResolveIndexValidationSampleProgress(IndexValidationSampleProgressInput{
		PreviousBestCount:      30,
		MergedCount:            30,
		BestDiagnosticReason:   "existing best",
		SampleDiagnosticReason: "new sample",
	})

	if result.ShouldPromoteMergedLinks || result.CurrentBestCount != 30 || result.BestDiagnosticReason != "existing best" || result.EffectiveDiagnosticReason != "existing best" {
		t.Fatalf("unexpected sample progress: %#v", result)
	}
}

func TestResolveIndexValidationSampleProgressBackfillsBestDiagnostic(t *testing.T) {
	result := ResolveIndexValidationSampleProgress(IndexValidationSampleProgressInput{
		PreviousBestCount:      30,
		MergedCount:            30,
		BestDiagnosticReason:   "",
		SampleDiagnosticReason: "duplicate ids detected",
	})

	if result.ShouldPromoteMergedLinks || result.BestDiagnosticReason != "duplicate ids detected" || result.EffectiveDiagnosticReason != "duplicate ids detected" {
		t.Fatalf("unexpected sample progress: %#v", result)
	}
}

func TestResolveIndexValidationEffectiveDiagnosticReason(t *testing.T) {
	if ResolveIndexValidationEffectiveDiagnosticReason("best", "sample") != "best" {
		t.Fatal("expected best diagnostic reason to win")
	}
	if ResolveIndexValidationEffectiveDiagnosticReason("", "sample") != "sample" {
		t.Fatal("expected fallback diagnostic reason to be used")
	}
	if ResolveIndexValidationEffectiveDiagnosticReason("", "") != "" {
		t.Fatal("expected empty diagnostic reason")
	}
}
