package bridge

import (
	"time"

	"javflow/internal/adlearning"
	"javflow/internal/organizer"
)

// Ad-risk bridge wiring is separated from organizer execution so future
// detector changes do not force edits in the organizer command flow.
//
// Current product rule:
// - organizer execution in the Wails desktop runtime is a Go-owned path
// - optional ad-risk evaluation may use the local Go adlearning service
// - organizer must not fall back to the Node sidecar anymore
//
// That keeps video organizer behavior independent from the crawler's remaining
// Cloudflare / sidecar compatibility lanes.
//
// Ownership summary:
// 1) wire optional organizer ad-risk evaluation into the Go organizer path
// 2) keep organizer ad-risk configuration separate from organizer run orchestration
// 3) preserve independence from the crawler's sidecar compatibility lane
//
// File map for maintainers:
// 1) organizer ad-risk configuration entrypoint
// 2) local ad-risk evaluator binding helper

func (a *API) configureOrganizerAdRisk(options *organizer.RunOptions, taskID string) {
	if options == nil || !options.AdDetectionEnabled || options.DryRun {
		return
	}

	if a.organizer.adLearning != nil {
		a.configureOrganizerLocalAdRisk(options, taskID)
	}
}

func (a *API) configureOrganizerLocalAdRisk(options *organizer.RunOptions, taskID string) {
	threshold := options.AdThreshold
	if _, err := a.organizer.adLearning.UpdateModel(adlearning.UpdateModelOptions{
		Keywords:  normalizeKeywordList(options.AdKeywords),
		AdScore:   &threshold,
		ModelType: options.AdModelType,
	}); err != nil {
		a.emitOrganizerLog(organizer.LogEntry{
			Level:     "warn",
			Message:   "ad model cache warmup failed: " + err.Error(),
			Timestamp: time.Now().Format(time.RFC3339),
		}, taskID)
	}

	options.EvaluateAdRisk = func(request organizer.AdRiskRequest) (organizer.AdRiskResult, error) {
		result, err := a.organizer.adLearning.EvaluateVideoRisk(adlearning.EvaluateOptions{
			VideoPath:   request.VideoPath,
			StreamURL:   request.StreamURL,
			FilmCode:    request.FilmCode,
			AdThreshold: request.AdThreshold,
			ModelType:   request.ModelType,
		})
		if err != nil {
			return organizer.AdRiskResult{}, err
		}
		return organizer.AdRiskResult{
			IsAd:      result.IsAd,
			Score:     result.Score,
			Threshold: result.Threshold,
			Reasons:   result.Reasons,
			Evidence:  result.Evidence,
		}, nil
	}
}
