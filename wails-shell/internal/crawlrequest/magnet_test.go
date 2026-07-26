package crawlrequest

import "testing"

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
