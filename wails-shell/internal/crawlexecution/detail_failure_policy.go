package crawlexecution

import (
	"sort"
	"strconv"
	"strings"
)

// detail_failure_policy.go owns classification and prioritization rules for
// failed detail-page recovery candidates.
//
// Ownership summary:
// 1) classify failed detail records into retry/recovery policies
// 2) prioritize recovery candidates and summarize failure reasons
// 3) keep detail-failure policy separate from runner orchestration and storage
//
// File map for maintainers:
// 1) detail failure policy and recovery candidate DTOs
// 2) classification and prioritization helpers
// 3) failure reason summary and retry-advice helpers

type DetailFailurePolicy struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	MaxRetries int    `json:"maxRetries"`
	Priority   int    `json:"priority"`
	Advice     string `json:"advice"`
}

type DetailRecoveryCandidate struct {
	Link             string `json:"link"`
	Reason           string `json:"reason"`
	RetryCount       int    `json:"retryCount"`
	RecoveryAttempts int    `json:"recoveryAttempts"`
	HasFailureRecord bool   `json:"hasFailureRecord"`
}

type DetailRecoverySummaryEntry struct {
	Reason string `json:"reason"`
}

func ClassifyDetailFailure(reason string) DetailFailurePolicy {
	normalizedReason := strings.ToLower(strings.TrimSpace(reason))

	switch {
	case strings.Contains(normalizedReason, "request cancelled"),
		strings.Contains(normalizedReason, "任务已终止"),
		strings.Contains(normalizedReason, "用户主动终止"):
		return DetailFailurePolicy{
			Key:        "stopped",
			Label:      "任务终止",
			MaxRetries: 0,
			Priority:   99,
			Advice:     "任务已被终止，本轮不会继续重试。",
		}
	case strings.Contains(normalizedReason, "cloudflare"),
		strings.Contains(normalizedReason, "challenge"),
		strings.Contains(normalizedReason, "driver-verify"),
		strings.Contains(normalizedReason, "age verification"),
		strings.Contains(normalizedReason, "验证页"),
		strings.Contains(normalizedReason, "403"),
		strings.Contains(normalizedReason, "429"):
		return DetailFailurePolicy{
			Key:        "blocked",
			Label:      "验证拦截",
			MaxRetries: 5,
			Priority:   0,
			Advice:     "建议开启 Cloudflare、切换备用网址或检查代理后再重试。",
		}
	case strings.Contains(normalizedReason, "timed out"),
		strings.Contains(normalizedReason, "timeout"),
		strings.Contains(normalizedReason, "econnreset"),
		strings.Contains(normalizedReason, "enotfound"),
		strings.Contains(normalizedReason, "err_connection"),
		strings.Contains(normalizedReason, "socket hang up"),
		strings.Contains(normalizedReason, "proxy"):
		return DetailFailurePolicy{
			Key:        "network",
			Label:      "网络超时",
			MaxRetries: 4,
			Priority:   1,
			Advice:     "建议检查网络、代理或稍后再次补爬。",
		}
	case strings.Contains(normalizedReason, "响应为空"),
		strings.Contains(normalizedReason, "empty"),
		strings.Contains(normalizedReason, "页面响应为空"),
		strings.Contains(normalizedReason, "返回空"):
		return DetailFailurePolicy{
			Key:        "empty",
			Label:      "页面空响应",
			MaxRetries: 3,
			Priority:   2,
			Advice:     "建议重新抓取该详情页，必要时切换域名后补爬。",
		}
	case strings.Contains(normalizedReason, "parse"),
		strings.Contains(normalizedReason, "metadata"),
		strings.Contains(normalizedReason, "script"),
		strings.Contains(normalizedReason, "cannot read"),
		strings.Contains(normalizedReason, "undefined"),
		strings.Contains(normalizedReason, "null"):
		return DetailFailurePolicy{
			Key:        "parse",
			Label:      "页面解析失败",
			MaxRetries: 3,
			Priority:   3,
			Advice:     "建议稍后重试，若持续失败需检查站点页面结构是否变化。",
		}
	default:
		return DetailFailurePolicy{
			Key:        "unknown",
			Label:      "未知失败",
			MaxRetries: 2,
			Priority:   4,
			Advice:     "建议查看日志后再次补爬。",
		}
	}
}

func GetDetailRecoveryBudget(policy DetailFailurePolicy, largeTaskMode bool) int {
	if policy.Key == "stopped" {
		return 0
	}

	baseBudget := 3
	if largeTaskMode {
		baseBudget = 2
	}
	if policy.Key == "blocked" {
		return minInt(policy.MaxRetries, baseBudget+1)
	}

	return minInt(maxInt(policy.MaxRetries, 1), baseBudget)
}

func BuildRecoveryCategorySummary(entries []DetailRecoverySummaryEntry) string {
	if len(entries) == 0 {
		return ""
	}

	countByLabel := map[string]int{}
	order := []string{}
	for _, entry := range entries {
		policy := ClassifyDetailFailure(entry.Reason)
		if _, exists := countByLabel[policy.Label]; !exists {
			order = append(order, policy.Label)
		}
		countByLabel[policy.Label] += 1
	}

	parts := make([]string, 0, len(order))
	for _, label := range order {
		parts = append(parts, label+" "+strconv.Itoa(countByLabel[label])+" 条")
	}
	return strings.Join(parts, "，")
}

func GetRecoverableMissingDetailLinks(candidates []DetailRecoveryCandidate, largeTaskMode bool) []string {
	type evaluatedCandidate struct {
		Link                string
		Policy              DetailFailurePolicy
		RetryCount          int
		RetriesRemaining    int
		RecoveryAttempts    int
		TotalRecoveryBudget int
		HasFailureRecord    bool
	}

	evaluated := make([]evaluatedCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		policy := ClassifyDetailFailure(candidate.Reason)
		evaluated = append(evaluated, evaluatedCandidate{
			Link:                candidate.Link,
			Policy:              policy,
			RetryCount:          candidate.RetryCount,
			RetriesRemaining:    maxInt(policy.MaxRetries-candidate.RetryCount, 0),
			RecoveryAttempts:    candidate.RecoveryAttempts,
			TotalRecoveryBudget: GetDetailRecoveryBudget(policy, largeTaskMode),
			HasFailureRecord:    candidate.HasFailureRecord,
		})
	}

	filtered := make([]evaluatedCandidate, 0, len(evaluated))
	for _, candidate := range evaluated {
		if (candidate.RetriesRemaining > 0 || !candidate.HasFailureRecord) && candidate.RecoveryAttempts < candidate.TotalRecoveryBudget {
			filtered = append(filtered, candidate)
		}
	}

	sort.Slice(filtered, func(i int, j int) bool {
		left := filtered[i]
		right := filtered[j]

		if left.Policy.Priority != right.Policy.Priority {
			return left.Policy.Priority < right.Policy.Priority
		}
		if left.RecoveryAttempts != right.RecoveryAttempts {
			return left.RecoveryAttempts < right.RecoveryAttempts
		}
		if left.RetryCount != right.RetryCount {
			return left.RetryCount < right.RetryCount
		}
		return strings.Compare(left.Link, right.Link) < 0
	})

	result := make([]string, 0, len(filtered))
	for _, candidate := range filtered {
		result = append(result, candidate.Link)
	}
	return result
}
