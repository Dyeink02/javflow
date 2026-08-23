package organizer

// expected_code_match.go owns the expected-code path-matching helpers shared by
// the scan phase. They originally lived in name_rescue.go; the rescue feature
// was removed, so the shared matchers moved here unchanged.
//
// Ownership summary:
// 1) match a file path against the expected code list (with magnet aliases)
// 2) walk path segments until a unique expected code is confirmed
// 3) keep ambiguity detection so strict matching never guesses
//
// File map for maintainers:
// 1) path-level expected-code matching entrypoints
// 2) value-level matching with alias and ambiguity handling
import (
	"path/filepath"
)

func matchExpectedCodeFromPath(filePath, rootPath string, codeSet, tokenSet map[string]struct{}) string {
	matched, _, _ := matchExpectedCodeFromPathWithAliases(filePath, rootPath, codeSet, tokenSet, newExpectedCodeAliasIndex())
	return matched
}

func matchExpectedCodeFromPathWithAliases(filePath, rootPath string, codeSet, tokenSet map[string]struct{}, aliasIndex expectedCodeAliasIndex) (string, string, bool) {
	current := filepath.Clean(filePath)
	for {
		if matched, alias, ambiguous := matchExpectedCodeEvidenceWithAliases(filepath.Base(current), codeSet, tokenSet, aliasIndex); matched != "" {
			return matched, alias, false
		} else if ambiguous {
			return "", "", true
		}
		parent := filepath.Dir(current)
		if parent == current || parent == filepath.Clean(rootPath) || !isPathInside(rootPath, parent) {
			break
		}
		current = parent
	}
	return "", "", false
}

func matchExpectedCodeEvidence(value string, codeSet, tokenSet map[string]struct{}) string {
	matched, _, _ := matchExpectedCodeEvidenceWithAliases(value, codeSet, tokenSet, newExpectedCodeAliasIndex())
	return matched
}

func matchExpectedCodeEvidenceWithAliases(value string, codeSet, tokenSet map[string]struct{}, aliasIndex expectedCodeAliasIndex) (string, string, bool) {
	candidate := extractFilmCodeFromFile(value, codeSet, tokenSet)
	if candidate == "" || !containsCode(codeSet, candidate) {
		if fallback := matchExpectedCodeFallback(value, codeSet); fallback != "" {
			return fallback, "", false
		}
		matched, alias, ambiguous := matchExpectedCodeAliasFromValue(value, aliasIndex)
		return matched, alias, ambiguous
	}
	return normalizeFilmID(candidate), "", false
}
