package main

import (
	"testing"
	"time"

	"javflow/internal/actressalias"
)

func TestMergePlexRecordsAddsSafeAliasesAndPreservesConflicts(t *testing.T) {
	base := []actressalias.Record{
		{Canonical: "新ありな", Aliases: []string{"桥本有菜"}, Source: "wikidata-cc0:Q1", Confidence: "source-labeled"},
		{Canonical: "別の女優", Aliases: []string{"重复别名"}, Source: "wikidata-cc0:Q2", Confidence: "source-labeled"},
	}
	upstream := map[string][]string{
		"新ありな":   {"新有菜", "桥本有菜", "重复别名"},
		"高橋しょう子": {"高桥圣子"},
	}
	merged, report := mergePlexRecords(base, upstream, "fixture", time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC))
	if len(merged) != 3 || report.ImportedRecords != 1 || report.ImportedAliases != 2 {
		t.Fatalf("unexpected merge report=%+v records=%+v", report, merged)
	}
	if len(report.RejectedAliases) != 1 || report.RejectedAliases[0].Alias != "重复别名" {
		t.Fatalf("conflict was not reported: %+v", report.RejectedAliases)
	}
	index := actressalias.New(t.TempDir())
	for _, record := range merged {
		if err := index.Remember(record); err != nil {
			t.Fatal(err)
		}
	}
	for query, want := range map[string]string{"新有菜": "新ありな", "高桥圣子": "高橋しょう子"} {
		result := index.Resolve(query)
		if !result.Unique || result.Canonical != want {
			t.Fatalf("Resolve(%q) = %+v, want %q", query, result, want)
		}
	}
}

func TestParseUpstreamRejectsEmptyDocument(t *testing.T) {
	if _, err := parseUpstream([]byte(`{}`)); err == nil {
		t.Fatal("expected empty upstream document to be rejected")
	}
}

func TestParseUpstreamAcceptsUTF8BOM(t *testing.T) {
	aliases, err := parseUpstream([]byte{0xef, 0xbb, 0xbf, '{', '"', 'A', '"', ':', '[', '"', 'B', '"', ']', '}'})
	if err != nil || len(aliases["A"]) != 1 || aliases["A"][0] != "B" {
		t.Fatalf("parse BOM input aliases=%+v err=%v", aliases, err)
	}
}

func TestRemovePreviousPlexImportDropsOnlyPriorImportedData(t *testing.T) {
	upstream := map[string][]string{"新ありな": {"新有菜"}}
	records := []actressalias.Record{
		{Canonical: "新ありな", Aliases: []string{"桥本有菜", "新有菜"}, Source: "wikidata-cc0:Q1;plex-jav@3236bed"},
		{Canonical: "小沢マリア", Aliases: []string{"小泽玛利亚"}, Source: "plex-jav@3236bed"},
	}
	clean := removePreviousPlexImport(records, upstream)
	if len(clean) != 1 || clean[0].Source != "wikidata-cc0:Q1" || len(clean[0].Aliases) != 1 || clean[0].Aliases[0] != "桥本有菜" {
		t.Fatalf("unexpected cleaned records: %+v", clean)
	}
}
