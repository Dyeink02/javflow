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
