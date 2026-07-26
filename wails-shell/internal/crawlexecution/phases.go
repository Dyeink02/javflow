package crawlexecution

import "strings"

// phases.go owns the stable phase catalog presented to runtime panels and
// diagnostics.
//
// Ownership summary:
// 1) define the canonical execution phase catalog and labels
// 2) normalize phase-key lookup for logs, panels, and diagnostics
// 3) keep user-facing phase vocabulary centralized and stable
//
// File map for maintainers:
// 1) phase catalog DTOs and stable labels
// 2) phase lookup and normalization helpers
// 3) runtime panel and diagnostics vocabulary helpers

// Phase is the user-facing catalog entry for one execution phase.
type Phase struct {
	Key         string
	Title       string
	Description string
}

var phaseCatalog = []Phase{
	{Key: "boot", Title: "启动准备", Description: "加载配置并初始化本次抓取运行环境。"},
	{Key: "queue_setup", Title: "初始化队列", Description: "准备索引页、详情页和补抓任务队列。"},
	{Key: "resume_pending", Title: "恢复未完成任务", Description: "恢复上次未完成的详情任务并优先补齐。"},
	{Key: "index_discovery", Title: "抓取索引页", Description: "遍历列表页并持续发现唯一番号。"},
	{Key: "queue_drain", Title: "队列排空", Description: "等待已入队的详情任务全部处理完成。"},
	{Key: "page_gap_recovery", Title: "分页缺口补查", Description: "复查存在缺口的分页，降低漏抓风险。"},
	{Key: "queue_gap_recovery", Title: "入队缺口补齐", Description: "补齐已发现但尚未进入详情队列的番号。"},
	{Key: "detail_recovery", Title: "失败详情补爬", Description: "重试失败详情页并回补缺失结果。"},
	{Key: "second_validation", Title: "结果二次校验", Description: "按目标数量和输出结果做收尾校验。"},
	{Key: "final_drain", Title: "收尾输出", Description: "刷新最终输出文件并写入最后状态。"},
}

var phaseLookup = map[string]Phase{
	"boot":               phaseCatalog[0],
	"queue_setup":        phaseCatalog[1],
	"resume_pending":     phaseCatalog[2],
	"index_discovery":    phaseCatalog[3],
	"queue_drain":        phaseCatalog[4],
	"page_gap_recovery":  phaseCatalog[5],
	"queue_gap_recovery": phaseCatalog[6],
	"detail_recovery":    phaseCatalog[7],
	"second_validation":  phaseCatalog[8],
	"final_drain":        phaseCatalog[9],
}

var labelToPhaseKey = map[string]string{
	"启动抓取流程":    "boot",
	"初始化任务队列":   "queue_setup",
	"恢复未完成详情任务": "resume_pending",
	"抓取索引页":     "index_discovery",
	"等待工作队列排空":  "queue_drain",
	"分页缺口补查":    "page_gap_recovery",
	"入队缺口补齐":    "queue_gap_recovery",
	"失败详情补爬":    "detail_recovery",
	"结果二次校验":    "second_validation",
	"刷新收尾输出":    "final_drain",
}

var messageKeywordPhases = []struct {
	keywords []string
	phaseKey string
}{
	{keywords: []string{"启动抓取流程", "开始抓取", "initializing crawl"}, phaseKey: "boot"},
	{keywords: []string{"初始化任务队列", "准备索引页", "准备详情页", "queue setup"}, phaseKey: "queue_setup"},
	{keywords: []string{"恢复未完成详情任务", "恢复未完成任务", "resume pending"}, phaseKey: "resume_pending"},
	{keywords: []string{"抓取索引页", "正在抓取索引页", "索引页抓取", "index discovery"}, phaseKey: "index_discovery"},
	{keywords: []string{"等待工作队列排空", "等待详情队列", "queue drain"}, phaseKey: "queue_drain"},
	{keywords: []string{"分页缺口补查", "补查第", "page gap"}, phaseKey: "page_gap_recovery"},
	{keywords: []string{"入队缺口补齐", "未进入详情队列", "queue gap"}, phaseKey: "queue_gap_recovery"},
	{keywords: []string{"失败详情补爬", "补爬后", "detail recovery"}, phaseKey: "detail_recovery"},
	{keywords: []string{"结果二次校验", "二次校验", "second validation"}, phaseKey: "second_validation"},
	{keywords: []string{"刷新收尾输出", "保存抓取结果", "抓取任务已完成", "final drain"}, phaseKey: "final_drain"},
}

// Catalog returns the stable phase list presented to runtime panels and review
// helpers.
func Catalog() []Phase {
	result := make([]Phase, len(phaseCatalog))
	copy(result, phaseCatalog)
	return result
}

func TotalPhases() int {
	return len(phaseCatalog)
}

func Lookup(key string) (Phase, bool) {
	phase, ok := phaseLookup[strings.TrimSpace(key)]
	return phase, ok
}

func FindIndex(key string) int {
	for index, phase := range phaseCatalog {
		if phase.Key == strings.TrimSpace(key) {
			return index + 1
		}
	}
	return 0
}

func NormalizePhaseKeys(keys []string) []string {
	normalized := make([]string, 0, len(keys))
	seen := map[string]struct{}{}

	for _, key := range keys {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		if _, ok := phaseLookup[trimmed]; !ok {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}

	if len(normalized) == 0 {
		return catalogKeys()
	}

	if normalized[0] != "boot" {
		if _, exists := seen["boot"]; !exists {
			normalized = append([]string{"boot"}, normalized...)
			seen["boot"] = struct{}{}
		}
	}

	if _, exists := seen["final_drain"]; !exists {
		normalized = append(normalized, "final_drain")
	}

	return normalized
}

func FindPlanIndex(keys []string, key string) int {
	normalizedKeys := NormalizePhaseKeys(keys)
	trimmedKey := strings.TrimSpace(key)
	if trimmedKey == "" {
		return 0
	}

	for index, phaseKey := range normalizedKeys {
		if phaseKey == trimmedKey {
			return index + 1
		}
	}

	return 0
}

func FindKeyByLabel(label string) string {
	return labelToPhaseKey[strings.TrimSpace(label)]
}

// InferKeyFromMessage is the fallback label/message heuristic used when legacy
// payloads do not carry a canonical phase key.
func InferKeyFromMessage(message string) string {
	lowerMessage := strings.ToLower(strings.TrimSpace(message))
	if lowerMessage == "" {
		return ""
	}

	for _, item := range messageKeywordPhases {
		for _, keyword := range item.keywords {
			normalizedKeyword := strings.ToLower(strings.TrimSpace(keyword))
			if normalizedKeyword == "" {
				continue
			}
			if strings.Contains(lowerMessage, normalizedKeyword) {
				return item.phaseKey
			}
		}
	}

	return ""
}

func IsFinalStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "error", "stopped", "incomplete":
		return true
	default:
		return false
	}
}

func FinalTitleForStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed":
		return "抓取完成"
	case "stopped":
		return "任务已停止"
	case "incomplete":
		return "任务未完成"
	case "error":
		return "任务异常"
	default:
		return ""
	}
}

func catalogKeys() []string {
	keys := make([]string, 0, len(phaseCatalog))
	for _, phase := range phaseCatalog {
		keys = append(keys, phase.Key)
	}
	return keys
}
