package actressalias

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestResolveBundledChineseAndRomanizedAliases(t *testing.T) {
	index := New(t.TempDir())
	for query, want := range map[string]string{
		"三上悠亚":       "三上悠亜",
		"濑户环奈":       "瀬戸環奈",
		"seto kanna": "瀬戸環奈",
		"こよいこなん":     "小宵こなん",
		"相泽南":        "相沢みなみ",
		"高桥圣子":       "高橋しょう子",
		"苍井空":        "蒼井そら",
		"明日花绮罗":      "明日花キララ",
		"桥本有菜":       "新ありな",
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

func TestResolveBundledSimplifiedChineseVariants(t *testing.T) {
	index := New(t.TempDir())
	for query, want := range map[string]string{
		"逢泽美优":  "逢沢みゆ",
		"波多野结衣": "波多野結衣",
		"本乡爱":   "本郷愛",
		"八挂海":   "八掛うみ",
		"神木丽":   "神木麗",
		"七泽米亚":  "七沢みあ",
	} {
		resolution := index.Resolve(query)
		if !resolution.Unique || resolution.Canonical != want {
			t.Fatalf("Resolve(%q) = %+v, want %q", query, resolution, want)
		}
	}
}

func TestResolveBundledRecentRankingCanonicalNames(t *testing.T) {
	index := New(t.TempDir())
	for query, want := range map[string]string{
		"彩月七绪": "彩月七緒",
		"爱才りあ": "愛才りあ",
		"博多彩叶": "博多彩葉",
		"北冈果林": "北岡果林",
		"三澄宁々": "三澄寧々",
	} {
		resolution := index.Resolve(query)
		if !resolution.Unique || resolution.Canonical != want {
			t.Fatalf("Resolve(%q) = %+v, want %q", query, resolution, want)
		}
	}
}

func TestBundledRecordsResolveEveryDeclaredAliasUniquely(t *testing.T) {
	records := loadBundledRecords()
	if len(records) < 200 {
		t.Fatalf("bundled alias pack unexpectedly shrank to %d records", len(records))
	}
	index := New(t.TempDir())
	for _, record := range records {
		for _, alias := range append([]string{record.Canonical}, record.Aliases...) {
			resolution := index.Resolve(alias)
			if !resolution.Unique || resolution.Canonical != record.Canonical {
				t.Fatalf("bundled alias %q resolved to %+v, expected %q", alias, resolution, record.Canonical)
			}
		}
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

func TestRememberDoesNotCopyBundledAliasesIntoUserCache(t *testing.T) {
	dir := t.TempDir()
	index := New(dir)
	if err := index.Remember(Record{
		Canonical:  "三上悠亜",
		Aliases:    []string{"三上悠亚", "三上悠亚新别名"},
		Source:     "provider",
		Confidence: "verified",
	}); err != nil {
		t.Fatal(err)
	}

	payload, err := os.ReadFile(filepath.Join(dir, "actress-aliases.user.json"))
	if err != nil {
		t.Fatal(err)
	}
	var records []Record
	if err := json.Unmarshal(payload, &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected one user-only record, got %#v", records)
	}
	if len(records[0].Aliases) != 1 || records[0].Aliases[0] != "三上悠亚新别名" {
		t.Fatalf("bundled aliases leaked into user cache: %#v", records[0])
	}
}

func TestRememberKeepsConcurrentVerifiedAliases(t *testing.T) {
	directory := t.TempDir()
	index := New(directory)
	entries := []Record{
		{Canonical: "actor-one", Aliases: []string{"alias-one"}, Source: "test", Confidence: "verified"},
		{Canonical: "actor-two", Aliases: []string{"alias-two"}, Source: "test", Confidence: "verified"},
	}
	var workers sync.WaitGroup
	for _, entry := range entries {
		entry := entry
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := index.Remember(entry); err != nil {
				t.Errorf("Remember(%q): %v", entry.Canonical, err)
			}
		}()
	}
	workers.Wait()
	reloaded := New(directory)
	for _, entry := range entries {
		if result := reloaded.Resolve(entry.Aliases[0]); !result.Unique || result.Canonical != entry.Canonical {
			t.Fatalf("concurrent alias %q was lost: %+v", entry.Aliases[0], result)
		}
	}
}
