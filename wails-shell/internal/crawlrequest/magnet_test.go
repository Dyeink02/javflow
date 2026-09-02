package crawlrequest

import (
	"fmt"
	"net/url"
	"testing"
)

func TestBuildParsedMagnetCandidatesAndSelectLargest(t *testing.T) {
	links := []string{
		"magnet:?xt=urn:btih:ABCDEF123456&dn=small",
		"magnet:?xt=urn:btih:123456ABCDEF&dn=large",
	}
	candidates := BuildParsedMagnetCandidates(links, []string{"900MB", "2GB"})
	selected := SelectLargestMagnetCandidate(candidates)

	if selected == nil {
		t.Fatalf("expected selected candidate")
	}
	if selected.DisplayName != "large" {
		t.Fatalf("expected large candidate, got %#v", selected)
	}
	if selected.Size != 2048 {
		t.Fatalf("expected 2048MB, got %v", selected.Size)
	}
}

func TestBuildMagnetResultKeepsLargestAndBackups(t *testing.T) {
	candidates := []ParsedMagnetCandidate{
		{MagnetLink: "m1", Size: 500},
		{MagnetLink: "m2", Size: 2048},
		{MagnetLink: "m3", Size: 1024},
	}
	result := BuildMagnetResult(candidates, false, 2)

	if result == nil {
		t.Fatalf("expected result")
	}
	if result.Magnet != "m2" {
		t.Fatalf("expected largest magnet m2, got %q", result.Magnet)
	}
	if len(result.BackupMagnetLinks) != 2 || result.BackupMagnetLinks[0].Link != "m2" || result.BackupMagnetLinks[1].Link != "m3" {
		t.Fatalf("unexpected backup links: %#v", result.BackupMagnetLinks)
	}
}

func TestApplyMagnetExcludeFilter(t *testing.T) {
	candidates := []ParsedMagnetCandidate{
		{MagnetLink: "m1", DisplayName: "clean"},
		{MagnetLink: "m2", DisplayName: "trailer sample"},
	}
	filtered := ApplyMagnetExcludeFilter(candidates, "sample")

	if len(filtered) != 1 || filtered[0].MagnetLink != "m1" {
		t.Fatalf("unexpected filtered result: %#v", filtered)
	}
}

func TestApplyMagnetExcludeFilterRemovesCollectionCandidatesByDefault(t *testing.T) {
	collection := ParsedMagnetCandidate{
		MagnetLink:  "magnet:?xt=urn:btih:COLLECTION&dn=%5Bls%5D%2ABF-271%2CBF-272%2CDGL-041%2CSNIS-009%2CSNIS-010.FHD",
		DisplayName: "[ls]*BF-271,BF-272,DGL-041,SNIS-009,SNIS-010.FHD",
		Size:        75786,
	}
	regular := ParsedMagnetCandidate{
		MagnetLink:  "magnet:?xt=urn:btih:REGULAR&dn=SNIS-009",
		DisplayName: "SNIS-009",
		Size:        5200,
	}

	if !IsCollectionMagnetCandidate(collection) {
		t.Fatalf("expected aggregate candidate to be detected: %#v", collection)
	}
	if IsCollectionMagnetCandidate(regular) {
		t.Fatalf("regular single-film candidate was misclassified: %#v", regular)
	}

	filtered := ApplyMagnetExcludeFilter([]ParsedMagnetCandidate{collection, regular}, "")
	if len(filtered) != 1 || filtered[0].DisplayName != regular.DisplayName {
		t.Fatalf("expected only regular candidate after built-in collection filtering: %#v", filtered)
	}
}

func TestIsCollectionMagnetCandidateRecognizesExplicitMarkers(t *testing.T) {
	for _, name := range []string{"SNIS-009 大合集", "SNIS-009 complete collection", "SNIS-009 box set"} {
		if !IsCollectionMagnetCandidate(ParsedMagnetCandidate{DisplayName: name}) {
			t.Fatalf("expected collection marker to be detected in %q", name)
		}
	}
}

func TestExtractMagnetLinksAcceptsMagnetWithoutDN(t *testing.T) {
	html := `<a href="magnet:?xt=urn:btih:1347598F03862100454B828CA065654DEB27A001">plain</a>`
	links := ExtractMagnetLinks(html)
	if len(links) != 1 {
		t.Fatalf("expected one magnet link, got %#v", links)
	}
	if links[0] != "magnet:?xt=urn:btih:1347598F03862100454B828CA065654DEB27A001" {
		t.Fatalf("unexpected magnet link: %q", links[0])
	}
}

func TestExtractMagnetLinksPreservesDisplayNameVariants(t *testing.T) {
	t.Helper()
	names := []string{"ABA-250", "aba-250", "ABA-250-c", "ABA-250-u", "1818.com@ABA-250"}
	hash := "0123456789ABCDEF0123456789ABCDEF01234567"
	html := ""
	for index, name := range names {
		link := fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s&tr=udp://tracker.example/%d", hash, url.QueryEscape(name), index+1)
		html += fmt.Sprintf(`<a href="%s">%s</a>\n`, link, name)
	}

	links := ExtractMagnetLinks(html)
	if len(links) != len(names) {
		t.Fatalf("expected %d magnet links, got %d: %#v", len(names), len(links), links)
	}
	for index, link := range links {
		t.Logf("simulated magnet %d: %s => %q", index+1, link, GetMagnetDisplayName(link))
		if got := GetMagnetDisplayName(link); got != names[index] {
			t.Errorf("display name %d = %q, want %q (link=%q)", index, got, names[index], link)
		}
	}
}

func TestGetMagnetDisplayNameUnescapesHTMLAndSupportsParameterOrder(t *testing.T) {
	link := "magnet:?dn=1818.com%40ABA-250&amp;xt=urn:btih:0123456789ABCDEF0123456789ABCDEF01234567"
	if got := GetMagnetDisplayName(link); got != "1818.com@ABA-250" {
		t.Fatalf("unexpected HTML-escaped display name: %q", got)
	}
}

func TestBuildMagnetResultPersistsDisplayName(t *testing.T) {
	result := BuildMagnetResult([]ParsedMagnetCandidate{{
		MagnetLink:  "magnet:?xt=urn:btih:0123456789ABCDEF0123456789ABCDEF01234567&dn=aba-250-c",
		Size:        2048,
		DisplayName: "aba-250-c",
	}}, false, 1)
	if result == nil || len(result.MagnetLinks) != 1 {
		t.Fatalf("expected one selected magnet, got %#v", result)
	}
	if result.MagnetLinks[0].DisplayName != "aba-250-c" {
		t.Fatalf("display name was not persisted: %#v", result.MagnetLinks[0])
	}
}
