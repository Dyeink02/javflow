package organizer

import "testing"

func TestExtractFilmCodeFromFileMatchesExpectedNumericVariants(t *testing.T) {
	codeSet, tokenSet := buildExpectedCodeSets([]string{"TL-001"})
	for _, filename := range []string{
		"1818@tl1.mp4",
		"TL-1.mp4",
		"TL-01.mp4",
		"TL-00001.mp4",
	} {
		if got := extractFilmCodeFromFile(filename, codeSet, tokenSet); got != "TL-001" {
			t.Errorf("extractFilmCodeFromFile(%q) = %q, want TL-001", filename, got)
		}
	}
}

func TestMatchExpectedNumericVariantDoesNotGuessDifferentNumber(t *testing.T) {
	codeSet, _ := buildExpectedCodeSets([]string{"TL-001"})
	for _, value := range []string{"TL-10", "TL-101", "XX-001", "random"} {
		if got := matchExpectedNumericVariant(value, codeSet); got != "" {
			t.Errorf("matchExpectedNumericVariant(%q) = %q, want no match", value, got)
		}
	}
}

func TestExtractFilmCodeFromFileRejectsSubstringAndAmbiguousExpectedMatches(t *testing.T) {
	codeSet, tokenSet := buildExpectedCodeSets([]string{"TL-001", "SSIS-123"})
	for _, filename := range []string{"TL-0010.mp4", "TL001_SSIS123.mp4"} {
		got := extractFilmCodeFromFile(filename, codeSet, tokenSet)
		if got != "" && containsCode(codeSet, got) {
			t.Errorf("extractFilmCodeFromFile(%q) guessed expected code %q", filename, got)
		}
	}
}
