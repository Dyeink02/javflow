package librarymetadata

import (
	"strings"
	"testing"
)

func TestBuildNFO_BBAN452(t *testing.T) {
	info := &MovieInfo{
		Number:      "BBAN-452",
		Title:       "Test Title",
		Plot:        "Test plot.",
		Outline:     "Test outline.",
		ReleaseDate: "2023-01-01",
		Year:        2023,
		Runtime:     120,
		Studio:      "Test Studio",
		Director:    "Test Director",
		Actors:      []string{"Actor A", "Actor B"},
		Genres:      []string{"Genre1", "Genre2"},
		CoverURL:    "https://example.com/cover.jpg",
		BackdropURL: "https://example.com/backdrop.jpg",
	}

	nfo, err := BuildNFO(info)
	if err != nil {
		t.Fatalf("BuildNFO failed: %v", err)
	}
	out := string(nfo)
	if !strings.Contains(out, "<title>BBAN-452 Test Title</title>") {
		t.Error("NFO title missing code prefix")
	}
	if !strings.Contains(out, "<originaltitle>BBAN-452 Test Title</originaltitle>") {
		t.Error("NFO originaltitle missing code prefix")
	}
	if !strings.Contains(out, "<thumb aspect=\"poster\">https://example.com/cover.jpg</thumb>") {
		t.Error("NFO missing poster thumb")
	}
	if !strings.Contains(out, "<fanart>") {
		t.Error("NFO missing fanart")
	}
	t.Logf("NFO preview:\n%s", out)
}

func TestBuildNFO_DoesNotDuplicateProviderNumberPrefix(t *testing.T) {
	info := &MovieInfo{
		Number: "MIDD-820-A",
		Title:  "MIDD-820-A - Split release",
	}

	nfo, err := BuildNFO(info)
	if err != nil {
		t.Fatalf("BuildNFO failed: %v", err)
	}
	out := string(nfo)
	if strings.Contains(out, "MIDD-820-A MIDD-820-A") {
		t.Fatalf("NFO duplicated the number prefix: %s", out)
	}
	if !strings.Contains(out, "<title>MIDD-820-A - Split release</title>") {
		t.Fatalf("NFO did not preserve the provider prefix: %s", out)
	}
}

func TestTitleHasNumberPrefixRequiresSeparator(t *testing.T) {
	for _, test := range []struct {
		title  string
		number string
		want   bool
	}{
		{title: "MIDD-820-A - 标题", number: "MIDD-820-A", want: true},
		{title: "MIDD-820-A 标题", number: "MIDD-820-A", want: true},
		{title: "MIDD-820-Amazing", number: "MIDD-820-A", want: false},
	} {
		if got := titleHasNumberPrefix(test.title, test.number); got != test.want {
			t.Errorf("titleHasNumberPrefix(%q, %q) = %v, want %v", test.title, test.number, got, test.want)
		}
	}
}
