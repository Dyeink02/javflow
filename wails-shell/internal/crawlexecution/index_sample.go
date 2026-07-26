package crawlexecution

// index_sample.go owns best-sample tracking while index-page validation
// iterates across retries.
//
// Ownership summary:
// 1) track best-sample progress across index validation retries
// 2) decide when merged samples should replace previous best results
// 3) keep sample-progress bookkeeping separate from attempt/iteration policy
//
// File map for maintainers:
// 1) sample-progress input/output DTOs
// 2) best-sample promotion helpers
// 3) diagnostic reason carry-forward helpers

type IndexValidationSampleProgressInput struct {
	PreviousBestCount      int    `json:"previousBestCount"`
	MergedCount            int    `json:"mergedCount"`
	BestDiagnosticReason   string `json:"bestDiagnosticReason"`
	SampleDiagnosticReason string `json:"sampleDiagnosticReason"`
}

type IndexValidationSampleProgress struct {
	PreviousBestCount         int    `json:"previousBestCount"`
	MergedCount               int    `json:"mergedCount"`
	CurrentBestCount          int    `json:"currentBestCount"`
	ShouldPromoteMergedLinks  bool   `json:"shouldPromoteMergedLinks"`
	BestDiagnosticReason      string `json:"bestDiagnosticReason"`
	EffectiveDiagnosticReason string `json:"effectiveDiagnosticReason"`
}

func ResolveIndexValidationEffectiveDiagnosticReason(bestDiagnosticReason string, fallbackDiagnosticReason string) string {
	if bestDiagnosticReason != "" {
		return bestDiagnosticReason
	}
	if fallbackDiagnosticReason != "" {
		return fallbackDiagnosticReason
	}
	return ""
}

func ResolveIndexValidationSampleProgress(input IndexValidationSampleProgressInput) IndexValidationSampleProgress {
	shouldPromoteMergedLinks := input.MergedCount > input.PreviousBestCount
	bestDiagnosticReason := input.BestDiagnosticReason
	sampleDiagnosticReason := input.SampleDiagnosticReason

	if shouldPromoteMergedLinks {
		if sampleDiagnosticReason != "" {
			bestDiagnosticReason = sampleDiagnosticReason
		}
	} else if bestDiagnosticReason == "" && sampleDiagnosticReason != "" {
		bestDiagnosticReason = sampleDiagnosticReason
	}

	currentBestCount := input.PreviousBestCount
	if shouldPromoteMergedLinks {
		currentBestCount = input.MergedCount
	}

	return IndexValidationSampleProgress{
		PreviousBestCount:         input.PreviousBestCount,
		MergedCount:               input.MergedCount,
		CurrentBestCount:          currentBestCount,
		ShouldPromoteMergedLinks:  shouldPromoteMergedLinks,
		BestDiagnosticReason:      bestDiagnosticReason,
		EffectiveDiagnosticReason: ResolveIndexValidationEffectiveDiagnosticReason(bestDiagnosticReason, sampleDiagnosticReason),
	}
}
