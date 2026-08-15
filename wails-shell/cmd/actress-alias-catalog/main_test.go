package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"javflow/internal/librarymetadata"
)

type fakeAliasResolver map[string]librarymetadata.ActorAlias

func (f fakeAliasResolver) ResolveActorAlias(_ context.Context, name string, _ string) (librarymetadata.ActorAlias, error) {
	if result, exists := f[name]; exists {
		return result, nil
	}
	return librarymetadata.ActorAlias{}, errors.New("not found")
}

func TestBuildReportKeepsOnlyVerifiedJapaneseCanonicalResults(t *testing.T) {
	results := resolveCandidates(context.Background(), fakeAliasResolver{
		"相泽南":  {Name: "相沢みなみ", Aliases: []string{"相泽南", "Aizawa Minami"}, Provider: "test"},
		"高桥圣子": {Name: "高橋しょう子", Aliases: []string{"高桥圣子", "Shoko Takahashi"}, Provider: "test"},
	}, []candidate{{Alias: "相泽南", Source: "fixture"}, {Alias: "高桥圣子", Source: "fixture"}}, "", 1, 0)
	report := buildReport(results, time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC))
	if len(report.Records) != 2 || len(report.Rejected) != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if report.Records[0].Canonical != "相沢みなみ" || report.Records[1].Canonical != "高橋しょう子" {
		t.Fatalf("unexpected canonical records: %+v", report.Records)
	}
}

func TestBuildReportRejectsAliasCollision(t *testing.T) {
	report := buildReport([]catalogResult{
		{candidate: candidate{Alias: "中文甲"}, alias: librarymetadata.ActorAlias{Name: "日本語甲", Aliases: []string{"共享别名"}, Provider: "test"}},
		{candidate: candidate{Alias: "中文乙"}, alias: librarymetadata.ActorAlias{Name: "日本語乙", Aliases: []string{"共享别名"}, Provider: "test"}},
	}, time.Now())
	if len(report.Records) != 1 || len(report.Rejected) != 1 {
		t.Fatalf("expected one record and one rejected collision: %+v", report)
	}
	if report.Rejected[0].Reason == "" {
		t.Fatalf("collision rejection must include a reason: %+v", report.Rejected[0])
	}
}
