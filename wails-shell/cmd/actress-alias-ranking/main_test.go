package main

import (
	"testing"
	"time"

	"javflow/internal/actressalias"
)

func TestMergeRankingRecordsAddsMissingCanonicalNames(t *testing.T) {
	base := []actressalias.Record{
		{Canonical: "彩月七緒", Source: "existing"},
		{Canonical: "別の女優", Aliases: []string{"愛才りあ"}, Source: "existing"},
	}
	merged, report := mergeRankingRecords(base, time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC))
	if report.ExistingRecords != 1 || report.AddedRecords != len(rankingNames)-2 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if len(report.RejectedNames) != 1 || report.RejectedNames[0].Canonical != "愛才りあ" {
		t.Fatalf("expected normalized collision to be rejected: %+v", report.RejectedNames)
	}
	index := actressalias.New(t.TempDir())
	for _, record := range merged {
		if err := index.Remember(record); err != nil {
			t.Fatal(err)
		}
	}
	if result := index.Resolve("彩月七绪"); !result.Unique || result.Canonical != "彩月七緒" {
		t.Fatalf("simplified canonical name was not resolved: %+v", result)
	}
}

func TestNamesSHA256IsStable(t *testing.T) {
	if got, want := namesSHA256([]string{"A", "B"}), "daee1cd25194ae952d046ad9b9c81d3c07dc5332440b58d6d7461b248be56712"; got != want {
		t.Fatalf("namesSHA256 = %q, want %q", got, want)
	}
}
