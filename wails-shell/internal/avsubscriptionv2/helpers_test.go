package avsubscriptionv2

import "testing"

func TestNormalizeFilmCodeCanonicalizesLeadingZeros(t *testing.T) {
	tests := map[string]string{
		"FWAY-087":                        "FWAY-087",
		"FWAY087":                         "FWAY-087",
		"https://www.javbus.com/fway-087": "FWAY-087",
		"MIDA-438":                        "MIDA-438",
		"TL-1":                            "TL-001",
		"TL-01":                           "TL-001",
		"TL-00001":                        "TL-001",
	}

	for input, expected := range tests {
		if got := normalizeFilmCode(input); got != expected {
			t.Fatalf("normalizeFilmCode(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestContainsCodeNormalizedTreatsLeadingZeroCodesAsSameFilm(t *testing.T) {
	if !containsCodeNormalized([]string{"FWAY-087", "MIDA-470"}, "FWAY-87") {
		t.Fatalf("expected FWAY-087 and FWAY-87 to be treated as the same code")
	}
}

func TestResolveObservedCountFromSnapshotsUsesActualPageScan(t *testing.T) {
	item := Subscription{
		BaselineCodes:        []string{"FWAY-87", "MDVR-406", "MIDA-470", "MIDA-509", "MIDA-543", "MIDA-584", "MIDA-616"},
		BaselineCount:        7,
		CurrentObservedCount: 7,
		PendingCount:         1,
		ItemsPerPage:         30,
		TotalPages:           1,
	}
	snapshots := []RefreshPageSnapshot{{
		Page:          1,
		ObservedCodes: []string{"MIDA-438", "FWAY-87", "MDVR-406", "MIDA-470", "MIDA-509", "MIDA-543", "MIDA-584", "MIDA-616"},
	}}

	if got := resolveObservedCountFromSnapshots(item, snapshots); got != 8 {
		t.Fatalf("expected current observed count 8, got %d", got)
	}
}
