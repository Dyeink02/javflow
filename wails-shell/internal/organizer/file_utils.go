package organizer

import (
	"errors"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// file_utils.go now keeps only organizer-generic helpers that are reused across
// multiple phases. Code matching lives in code_rules.go, and filesystem work
// lives in fs_ops.go.
//
// Ownership summary:
// 1) hold small organizer-generic value/format helpers reused across phases
// 2) keep suffix parsing and shared lightweight helpers out of phase files
// 3) avoid mixing code rules or filesystem ops back into this utility file
//
// File map for maintainers:
// 1) ad-action/default normalization helpers
// 2) suffix/index conversion helpers
// 3) small numeric/path utility helpers

type conflictSuffixStrategy struct {
	Mode      string
	Prefix    string
	StartChar int
	StartNum  int
	Raw       string
}

func normalizeAdFileAction(rawValue string) string {
	if strings.TrimSpace(rawValue) == adFileActionDeleteDirectly {
		return adFileActionDeleteDirectly
	}
	return adFileActionMoveToDelete
}

func getAdFileActionLabel(action string) string {
	if action == adFileActionDeleteDirectly {
		return "\u76f4\u63a5\u5220\u9664\u5e7f\u544a\u6587\u4ef6"
	}
	return "\u79fb\u5165\u5f85\u5220\u9664"
}

func toSafeInteger(value int, fallback int, minimum int) int {
	if value < minimum {
		if fallback < minimum {
			return minimum
		}
		return fallback
	}
	return value
}

func normalizeSuffixInput(rawInput string) string {
	normalized := strings.TrimSpace(rawInput)
	if normalized == "" {
		return "-A"
	}
	return normalized
}

func alphaIndexToText(n int) string {
	index := n
	if index < 1 {
		index = 1
	}
	output := ""
	for index > 0 {
		index--
		output = string(rune('A'+(index%26))) + output
		index = index / 26
	}
	return output
}

func isAlphaNumericASCII(value byte) bool {
	return (value >= '0' && value <= '9') || (value >= 'A' && value <= 'Z') || (value >= 'a' && value <= 'z')
}

func parseConflictSuffixStrategy(rawInput string) (conflictSuffixStrategy, error) {
	raw := normalizeSuffixInput(rawInput)
	if strings.ContainsAny(raw, " \t\r\n") {
		return conflictSuffixStrategy{}, errors.New("\u51b2\u7a81\u540e\u7f00\u4e0d\u80fd\u5305\u542b\u7a7a\u683c\uff0c\u8bf7\u4f7f\u7528\u7c7b\u4f3c -A\u3001-1 \u6216 _DUP \u7684\u683c\u5f0f\u3002")
	}

	if len(raw) > 0 {
		last := raw[len(raw)-1]
		if (last >= 'A' && last <= 'Z') || (last >= 'a' && last <= 'z') {
			prefix := raw[:len(raw)-1]
			canUseAlpha := prefix == "" || !isAlphaNumericASCII(prefix[len(prefix)-1])
			if canUseAlpha {
				startChar := int(strings.ToUpper(string(last))[0] - 'A' + 1)
				if startChar < 1 {
					startChar = 1
				}
				if startChar > 26 {
					startChar = 26
				}
				return conflictSuffixStrategy{Mode: "alpha", Prefix: prefix, StartChar: startChar, StartNum: 1, Raw: raw}, nil
			}
		}
	}

	numericPattern := regexp.MustCompile(`^(.*?)(\d+)$`)
	if matches := numericPattern.FindStringSubmatch(raw); len(matches) == 3 {
		startNum, _ := strconv.Atoi(matches[2])
		if startNum < 1 {
			startNum = 1
		}
		return conflictSuffixStrategy{Mode: "num", Prefix: matches[1], StartChar: 1, StartNum: startNum, Raw: raw}, nil
	}

	return conflictSuffixStrategy{Mode: "num", Prefix: raw, StartChar: 1, StartNum: 1, Raw: raw}, nil
}

func formatSuffix(strategy conflictSuffixStrategy, sequence int) string {
	if strategy.Mode == "alpha" {
		return strategy.Prefix + alphaIndexToText(strategy.StartChar+sequence)
	}
	return strategy.Prefix + intToString(strategy.StartNum+sequence)
}

func intToString(value int) string {
	return strconv.Itoa(value)
}

// managedDirectoryNames is kept near top-level organizer helpers because both
// transfer and cleanup phases need the same definition of "owned" directories.
// This list is intentionally smaller than "all directories seen during a run":
// it describes directories the organizer itself may safely create/compact.
func managedDirectoryNames(paths Paths, includeDelete bool) map[string]struct{} {
	result := map[string]struct{}{
		filepath.Base(paths.WaitingDir):   {},
		filepath.Base(paths.UnmatchedDir): {},
		filepath.Base(paths.IntroAdDir):   {},
		filepath.Base(paths.LogsDir):      {},
		filepath.Base(paths.StateDir):     {},
	}
	if includeDelete {
		result[filepath.Base(paths.ToDeleteDir)] = struct{}{}
	}
	return result
}
