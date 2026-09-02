package organizer

import (
	"path/filepath"
	"strings"
	"testing"
)

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

func TestPlanTargetNamesSanitizesFilmCodeBeforeBuildingFilename(t *testing.T) {
	strategy, err := parseConflictSuffixStrategy("-A")
	if err != nil {
		t.Fatal(err)
	}
	names := planTargetNames([]Candidate{
		{
			Src:              filepath.Join("downloads", "DRDZ-002.mp4**..mp4"),
			FilmCode:         "DRDZ-002.mp4**",
			RenameByFilmCode: true,
		},
		{
			Src:              filepath.Join("downloads", "ABP-001 source.mkv"),
			FilmCode:         "ABP-001",
			RenameByFilmCode: true,
		},
	}, strategy)

	if got, want := names[0], "DRDZ-002.mp4"; got != want {
		t.Fatalf("malformed code target = %q, want %q", got, want)
	}
	if got, want := names[1], "ABP-001.mkv"; got != want {
		t.Fatalf("normal target = %q, want %q", got, want)
	}
}

func TestPlanTargetNamesSanitizesUnmatchedIsoGlobName(t *testing.T) {
	strategy, err := parseConflictSuffixStrategy("-A")
	if err != nil {
		t.Fatal(err)
	}
	names := planTargetNames([]Candidate{
		{
			Src:              filepath.Join("downloads", "*.iso"),
			RenameByFilmCode: false,
		},
		{
			Src:              filepath.Join("downloads", "movie**.ISO"),
			RenameByFilmCode: false,
		},
	}, strategy)
	if names[0] != "_.iso" || names[1] != "movie__.ISO" {
		t.Fatalf("unsafe ISO names = %#v, want sanitized names", names)
	}
	for _, name := range names {
		if strings.ContainsAny(name, `<>:"/\\|?*`) {
			t.Fatalf("sanitized name still contains Windows wildcard: %q", name)
		}
	}
}

func TestMatchExpectedCodeFallbackHandlesNoisyAndZeroPaddedNames(t *testing.T) {
	codeSet, _ := buildExpectedCodeSets([]string{"MXGS-1121", "MXGS-1183", "MXGS-1358"})
	cases := map[string]string{
		"mxgs01121.mp4":            "MXGS-1121",
		"hhd800.com@MXGS-1183.mp4": "MXGS-1183",
		"kfa55.com@MXGS1358.mp4":   "MXGS-1358",
	}

	for filename, expected := range cases {
		if got := matchExpectedCodeFallback(filename, codeSet); got != expected {
			t.Errorf("matchExpectedCodeFallback(%q) = %q, want %q", filename, got, expected)
		}
	}
}

func TestMatchExpectedCodeFallbackRejectsAmbiguousPaths(t *testing.T) {
	codeSet, _ := buildExpectedCodeSets([]string{"MXGS-1121", "MXGS-1183"})
	if got := matchExpectedCodeFallback("MXGS01121-MXGS1183.mp4", codeSet); got != "" {
		t.Fatalf("matchExpectedCodeFallback returned %q for an ambiguous path", got)
	}
}

func TestExpectedCodeAliasIndexMapsMagnetDisplayNamesToCanonicalCodes(t *testing.T) {
	index := buildExpectedCodeAliasIndex([]CodeEntry{
		{Code: "MXGS-112", Magnets: []MagnetEntry{{Link: "magnet:?xt=urn:btih:AAA&dn=MXGS1121"}}},
		{Code: "MXGS-118", Magnets: []MagnetEntry{{Link: "magnet:?xt=urn:btih:BBB&dn=MXGS-1183"}}},
		{Code: "MXGS-135", Magnets: []MagnetEntry{{Link: "magnet:?xt=urn:btih:CCC&dn=kfa55.com%40MXGS1358"}}},
	})

	cases := map[string]struct {
		value     string
		canonical string
		alias     string
	}{
		"zero padded filename": {
			value:     "MXGS01121.mp4",
			canonical: "MXGS-112",
			alias:     "MXGS-1121",
		},
		"hyphenated filename": {
			value:     "hhd800.com@MXGS-1183.mp4",
			canonical: "MXGS-118",
			alias:     "MXGS-1183",
		},
		"domain prefixed filename": {
			value:     "kfa55.com@MXGS1358.mp4",
			canonical: "MXGS-135",
			alias:     "MXGS-1358",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			canonical, alias, ambiguous := matchExpectedCodeAliasFromValue(testCase.value, index)
			if ambiguous {
				t.Fatalf("matchExpectedCodeAliasFromValue(%q) reported ambiguity", testCase.value)
			}
			if canonical != testCase.canonical || alias != testCase.alias {
				t.Fatalf("matchExpectedCodeAliasFromValue(%q) = (%q, %q), want (%q, %q)", testCase.value, canonical, alias, testCase.canonical, testCase.alias)
			}
		})
	}

	if canonical, alias, ambiguous := matchExpectedCodeAliasFromValue("MXGS-1122.mp4", index); canonical != "" || alias != "" || ambiguous {
		t.Fatalf("unknown magnet alias was accepted: (%q, %q, %v)", canonical, alias, ambiguous)
	}
}

func TestExpectedCodeAliasIndexRejectsAmbiguousMagnetDisplayNames(t *testing.T) {
	index := buildExpectedCodeAliasIndex([]CodeEntry{
		{Code: "MXGS-112", Magnets: []MagnetEntry{{Link: "magnet:?xt=urn:btih:AAA&dn=MXGS11291"}}},
		{Code: "MXGS-1129", Magnets: []MagnetEntry{{Link: "magnet:?xt=urn:btih:BBB&dn=MXGS-11291"}}},
	})

	if canonical, alias, ambiguous := matchExpectedCodeAliasFromValue("site@MXGS11291.mp4", index); canonical != "" || alias != "" || !ambiguous {
		t.Fatalf("ambiguous magnet alias result = (%q, %q, %v), want empty and ambiguous", canonical, alias, ambiguous)
	}
}

func TestExpectedCodeAliasIndexRejectsDifferentCodeFamily(t *testing.T) {
	index := buildExpectedCodeAliasIndex([]CodeEntry{
		{Code: "MXGS-112", Magnets: []MagnetEntry{{Link: "magnet:?xt=urn:btih:AAA&dn=ABP1121"}}},
	})
	if canonical, alias, ambiguous := matchExpectedCodeAliasFromValue("ABP1121.mp4", index); canonical != "" || alias != "" || ambiguous {
		t.Fatalf("different code family was accepted: (%q, %q, %v)", canonical, alias, ambiguous)
	}
}

func TestMatchExpectedCodeFromPathWithAliasesReturnsCanonicalCode(t *testing.T) {
	codeSet, tokenSet := buildExpectedCodeSets([]string{"MXGS-112"})
	index := buildExpectedCodeAliasIndex([]CodeEntry{
		{Code: "MXGS-112", Magnets: []MagnetEntry{{Link: "magnet:?xt=urn:btih:AAA&dn=MXGS1121"}}},
	})

	root := filepath.Join("Z:", "佐山愛")
	filePath := filepath.Join(root, "MXGS1121", "mxgs01121.mp4")
	canonical, alias, ambiguous := matchExpectedCodeFromPathWithAliases(filePath, root, codeSet, tokenSet, index)
	if canonical != "MXGS-112" || alias != "MXGS-1121" || ambiguous {
		t.Fatalf("matchExpectedCodeFromPathWithAliases = (%q, %q, %v), want (MXGS-112, MXGS-1121, false)", canonical, alias, ambiguous)
	}
}
