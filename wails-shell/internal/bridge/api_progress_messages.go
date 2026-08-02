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
	operation := strings.TrimSpace(fmt.Sprint(progress["operation"]))
	currentPath := strings.TrimSpace(fmt.Sprint(progress["currentPath"]))
	if len([]rune(currentPath)) > 96 {
		runes := []rune(currentPath)
		currentPath = string(runes[:64]) + "..." + string(runes[len(runes)-24:])
	}
	heartbeat := ""
	if value, ok := progress["heartbeat"].(bool); ok && value {
		heartbeat = "（仍在处理）"
	}
	detail := ""
	if operation != "" {
		detail += "，" + operation
	}
	if currentPath != "" {
		detail += "，当前路径：" + currentPath
	}
	detail += heartbeat
	waitingTotal := intValue(progress["waitingTotal"], 0)
	deleteTotal := intValue(progress["deleteTotal"], 0)
	introAdTotal := intValue(progress["introAdTotal"], 0)
	switch phase {
	case "starting":
		return "正在准备视频整理"
	case "scanning", "scan-start", "scan-progress":
		return fmt.Sprintf("扫描目录：%d/%d%s", processed, total, detail)
	case "matched", "scan-completed":
		return fmt.Sprintf("扫描完成：待整理 %d，待删除 %d，开头广告 %d%s", waitingTotal, deleteTotal, introAdTotal, detail)
	case "waiting-start", "waiting-progress":
		return fmt.Sprintf("移入待整理：%d/%d%s", processed, total, detail)
	case "delete-start", "delete-progress":
		return fmt.Sprintf("处理待删除内容：%d/%d%s", processed, total, detail)
	case "intro-ad-start", "intro-ad-progress":
		return fmt.Sprintf("开头广告复核：%d/%d%s", processed, total, detail)
	case "finalize-start", "finalize-progress":
		finalizeTotal := intValue(progress["finalizeTotal"], 4)
		finalizeProcessed := intValue(progress["finalizeProcessed"], 0)
		subTotal := intValue(progress["subTotal"], 0)
		subProcessed := intValue(progress["subProcessed"], 0)
		subDetail := ""
		if subTotal > 0 {
			subDetail = fmt.Sprintf("，子任务 %d/%d", subProcessed, subTotal)
		}
		return fmt.Sprintf("整理收尾：%d/%d%s%s", finalizeProcessed, finalizeTotal, subDetail, detail)
	case "completed":
		return fmt.Sprintf("整理完成：待整理 %d，待删除 %d，开头广告 %d%s", waitingTotal, deleteTotal, introAdTotal, detail)
	default:
		return "视频整理正在运行" + detail
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
