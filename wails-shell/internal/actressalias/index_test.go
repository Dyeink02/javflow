package actressalias

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveBundledChineseAndRomanizedAliases(t *testing.T) {
	index := New(t.TempDir())
	for query, want := range map[string]string{
		"三上悠亚":       "三上悠亜",
		"濑户环奈":       "瀬戸環奈",
		"seto kanna": "瀬戸環奈",
		"こよいこなん":     "小宵こなん",
	} {
		result := index.Resolve(query)
		if !result.Unique || result.Canonical != want || result.MatchKind != "exact" {
			t.Fatalf("Resolve(%q) = %+v, want unique %q", query, result, want)
		}
	}
}

func TestDirectoryNameKeepsSearchableJapaneseGlyphs(t *testing.T) {
	if got := DirectoryName("濑户环奈"); got != "瀬戸環奈" {
		t.Fatalf("DirectoryName returned %q", got)
	}
}

func TestResolveReturnsCandidatesForCollision(t *testing.T) {
	index := New(t.TempDir())
	if err := index.Remember(Record{Canonical: "演员甲", Aliases: []string{"相同别名"}, Source: "test", Confidence: "verified"}); err != nil {
		t.Fatal(err)
	}
	if err := index.Remember(Record{Canonical: "演员乙", Aliases: []string{"相同别名"}, Source: "test", Confidence: "verified"}); err != nil {
		t.Fatal(err)
	}
	result := index.Resolve("相同别名")
	if result.Unique || result.MatchKind != "ambiguous" || len(result.Candidates) != 2 {
		t.Fatalf("collision should not auto-resolve: %+v", result)
	}
}

func TestRememberPersistsOnlyUserRecords(t *testing.T) {
	dir := t.TempDir()
	index := New(dir)
	if err := index.Remember(Record{Canonical: "测试演员", Aliases: []string{"测试别称"}, Source: "provider", Confidence: "verified"}); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(dir, "actress-aliases.user.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) == "" || string(payload) == "[]\n" {
		t.Fatalf("user cache was not written: %q", payload)
	}
	reloaded := New(dir).Resolve("测试别称")
	if !reloaded.Unique || reloaded.Canonical != "测试演员" {
		t.Fatalf("cache did not reload: %+v", reloaded)
	}
}
