package main

import (
	"testing"

	"javflow/internal/actressalias"
)

func TestRecordsFromWikidataKeepsOnlyUsableBilingualPairs(t *testing.T) {
	var document wikidataDocument
	document.Results.Bindings = []wikidataBinding{
		{Item: value{Value: "http://www.wikidata.org/entity/Q1"}, Japanese: value{Value: "蒼井そら"}, Chinese: value{Value: "苍井空"}},
		{Item: value{Value: "http://www.wikidata.org/entity/Q2"}, Japanese: value{Value: "Rio"}, Chinese: value{Value: "Rio"}},
	}
	records, rejectedRows := recordsFromWikidata(document)
	if len(records) != 1 || len(rejectedRows) != 1 {
		t.Fatalf("records=%+v rejected=%+v", records, rejectedRows)
	}
	if records[0].Canonical != "蒼井そら" || records[0].Aliases[0] != "苍井空" || records[0].Source != "wikidata-cc0:Q1" {
		t.Fatalf("unexpected record: %+v", records[0])
	}
}

func TestMergeRecordsRejectsCrossActorAliasCollision(t *testing.T) {
	base := []actressalias.Record{{Canonical: "蒼井そら", Aliases: []string{"苍井空"}, Source: "base"}}
	additions := []actressalias.Record{{Canonical: "別の女優", Aliases: []string{"苍井空"}, Source: "wikidata-cc0:Q2"}}
	merged, rejectedRows := mergeRecords(base, additions)
	if len(merged) != 1 || len(rejectedRows) != 1 {
		t.Fatalf("merged=%+v rejected=%+v", merged, rejectedRows)
	}
}

func TestEntityIDRejectsUnexpectedURLs(t *testing.T) {
	if got := entityID("https://www.wikidata.org/entity/Q123"); got != "Q123" {
		t.Fatalf("entityID=%q", got)
	}
	if got := entityID("https://example.com/entity/not-an-item"); got != "" {
		t.Fatalf("unexpected entityID=%q", got)
	}
}
