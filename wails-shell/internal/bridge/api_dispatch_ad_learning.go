package bridge

import (
	"fmt"
	"strings"
	"time"

	"javflow/internal/adlearning"
)

// Ad-learning command handling is isolated from dependency management because
// it has its own progress model, task state, and organizer-facing events.
//
// Ownership summary:
// 1) dispatch ad-learning bridge commands into the organizer adlearning service
// 2) normalize bridge payload fields for model update/import/learn actions
// 3) mirror learning progress/status back onto organizer-facing bridge events
//
// File map for maintainers:
// 1) ad-learning command dispatcher
// 2) model update/import option shaping
// 3) progress/status event fanout helpers

func (a *API) handleAdLearningCommand(command string, payload map[string]any) (string, bool, error) {
	switch command {
	case "app:get-ad-learning-summary":
		if a.organizer.adLearning == nil {
			return "", true, fmt.Errorf("ad learning service is not initialized")
		}
		result, err := marshalResult(a.organizer.adLearning.GetSummary())
		return result, true, err

	case "app:update-ad-learning-model":
		if a.organizer.adLearning == nil {
			return "", true, fmt.Errorf("ad learning service is not initialized")
		}
		keywords := normalizeKeywordList(cleanAnyString(payload["keywords"]))
		adScore := safeOptionalInt(payload["adScore"])
		highDistance := safeOptionalInt(payload["highSimilarityDistance"])
		mediumDistance := safeOptionalInt(payload["mediumSimilarityDistance"])
		lowDistance := safeOptionalInt(payload["lowSimilarityDistance"])
		updated, err := a.organizer.adLearning.UpdateModel(adlearning.UpdateModelOptions{
			Keywords:                 keywords,
			AdScore:                  adScore,
			HighSimilarityDistance:   highDistance,
			MediumSimilarityDistance: mediumDistance,
			LowSimilarityDistance:    lowDistance,
			ModelType:                nonEmptyString(payload["modelType"]),
		})
		if err != nil {
			return "", true, err
		}
		result, err := marshalResult(updated)
		return result, true, err

	case "app:import-ad-learning-samples":
		if a.organizer.adLearning == nil {
			return "", true, fmt.Errorf("ad learning service is not initialized")
		}
		imported, err := a.organizer.adLearning.ImportSamples(adlearning.ImportSamplesOptions{
			Label:       nonEmptyString(payload["label"]),
			SamplePaths: stringSliceValue(payload["samplePaths"]),
			ModelType:   normalizeAdModelType(nonEmptyString(payload["modelType"])),
		})
		if err != nil {
			return "", true, err
		}
		result, err := marshalResult(imported)
		return result, true, err

	case "app:learn-ad-samples-by-codes":
		return a.handleLearnAdSamplesByCodes(payload)
	}

	return "", false, nil
}

func (a *API) handleLearnAdSamplesByCodes(payload map[string]any) (string, bool, error) {
	if a.organizer.adLearning == nil {
		return "", true, fmt.Errorf("ad learning service is not initialized")
	}

	taskID := fmt.Sprintf("learning-go-%d", time.Now().UnixMilli())
	label := strings.TrimSpace(nonEmptyString(payload["label"]))
	if label != "normal" {
		label = "ad"
	}

	labelText := "ad samples"
	if label == "normal" {
		labelText = "normal samples"
	}
	a.emitOrganizerState(map[string]any{
		"status":  "running",
		"mode":    "learning",
		"message": "learning started: " + labelText,
	}, taskID)

	learned, err := a.organizer.adLearning.LearnSamplesByCodes(adlearning.LearnSamplesOptions{
		Label:                 label,
		Codes:                 stringSliceValue(payload["codes"]),
		RootPath:              nonEmptyString(payload["rootPath"]),
		IncludeSubdirectories: boolValue(payload["includeSubdirectories"], true),
		IgnoredDirNames:       stringSliceValue(payload["ignoredDirNames"]),
		ModelType:             normalizeAdModelType(nonEmptyString(payload["modelType"])),
		OnProgress: func(progress map[string]any) {
			a.emitOrganizerState(map[string]any{
				"status":   "running",
				"mode":     "learning-progress",
				"message":  learningProgressMessage(progress),
				"progress": progress,
			}, taskID)
		},
	})
	if err != nil {
		a.emitOrganizerState(map[string]any{
			"status":  "error",
			"mode":    "learning",
			"message": "learning failed: " + err.Error(),
		}, taskID)
		return "", true, err
	}

	completedProgress := map[string]any{
		"scope":               "learning",
		"phase":               "completed",
		"matchedVideoCount":   learned.MatchedVideoCount,
		"importedSampleCount": learned.ImportedSampleCount,
		"missingCodeCount":    len(learned.MissingCodes),
		"hitRate":             learned.HitRate,
		"falsePositiveRate":   learned.FalsePositiveRate,
		"sampleIncrement":     learned.SampleIncrement,
	}
	a.emitOrganizerState(map[string]any{
		"status":   "completed",
		"mode":     "learning",
		"message":  learningProgressMessage(completedProgress),
		"progress": completedProgress,
	}, taskID)

	result, marshalErr := marshalResult(learned)
	return result, true, marshalErr
}
