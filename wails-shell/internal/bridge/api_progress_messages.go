package bridge

import (
	"fmt"
	"strings"

	"javflow/internal/organizer"
)

// Progress message helpers are isolated from command dispatch so future text
// cleanup does not require touching bridge control flow again. They format
// bridge-facing summaries only; organizer/learning business meaning belongs
// to the underlying services and progress payload producers.
//
// Ownership summary:
// 1) translate organizer/learning progress payloads into bridge-facing summary text
// 2) centralize lightweight progress wording away from dispatch code
// 3) keep business semantics with the underlying progress producers
//
// File map for maintainers:
// 1) organizer progress message formatter
// 2) ad-learning progress message formatter

func organizerProgressMessage(progress organizer.ProgressEntry) string {
	phase := strings.TrimSpace(fmt.Sprint(progress["phase"]))
	total := intValue(progress["total"], 0)
	processed := intValue(progress["processed"], 0)
	waitingTotal := intValue(progress["waitingTotal"], 0)
	deleteTotal := intValue(progress["deleteTotal"], 0)
	introAdTotal := intValue(progress["introAdTotal"], 0)
	switch phase {
	case "starting":
		return "organizer task is starting"
	case "scanning":
		return fmt.Sprintf("organizer scanning: %d/%d", processed, total)
	case "matched":
		return fmt.Sprintf("organizer matched: waiting %d, delete %d, intro ad %d", waitingTotal, deleteTotal, introAdTotal)
	case "completed":
		return fmt.Sprintf("organizer completed: waiting %d, delete %d, intro ad %d", waitingTotal, deleteTotal, introAdTotal)
	default:
		return "organizer task is running"
	}
}

func learningProgressMessage(progress map[string]any) string {
	phase := strings.TrimSpace(fmt.Sprint(progress["phase"]))
	totalVideos := intValue(progress["totalVideos"], 0)
	processedVideos := intValue(progress["processedVideos"], 0)
	matchedVideoCount := intValue(progress["matchedVideoCount"], 0)
	importedSampleCount := intValue(progress["importedSampleCount"], 0)
	requestedCodeCount := intValue(progress["requestedCodeCount"], 0)
	switch phase {
	case "starting":
		return fmt.Sprintf("code learning started: target codes %d", requestedCodeCount)
	case "scan-ready":
		return fmt.Sprintf("learning scan ready: videos %d", totalVideos)
	case "matching":
		return fmt.Sprintf("learning matching: %d/%d, matched %d, samples %d", processedVideos, totalVideos, matchedVideoCount, importedSampleCount)
	case "learning":
		return fmt.Sprintf("learning: matched %d, samples %d", matchedVideoCount, importedSampleCount)
	case "completed":
		missingCodeCount := intValue(progress["missingCodeCount"], 0)
		hitRate := floatValue(progress["hitRate"], 0)
		falsePositiveRate := floatValue(progress["falsePositiveRate"], 0)
		sampleIncrement := intValue(progress["sampleIncrement"], importedSampleCount)
		return fmt.Sprintf("learning completed: matched %d, samples %d, missing %d, hit %.2f%%, false positive %.2f%%", matchedVideoCount, sampleIncrement, missingCodeCount, hitRate, falsePositiveRate)
	default:
		return "learning is running"
	}
}
