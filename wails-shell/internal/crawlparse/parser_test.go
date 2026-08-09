package crawlparse

import "testing"

const sampleHTML = `
<html>
  <head>
    <meta property="og:title" content="Sample Title" />
    <meta property="og:image" content="https://img.example.com/cover.jpg" />
  </head>
  <body>
    <script>
      var gid = 12345;
      var uc = 7;
      var img = 'https:\/\/img.example.com\/cover.jpg';
    </script>
    <a class="movie-box" href="https://www.javbus.com/ABF-055"></a>
    <a class="movie-box" href="https://www.javbus.com/ABF-055"></a>
    <a class="movie-box" href="https://www.javbus.com/ABW-006"></a>
    <span class="genre"><label><a>剧情</a></label></span>
    <span class="genre"><label><a>高清</a></label></span>
    <div class="star-name"><a>三上悠亜</a></div>
    <div class="alert alert-info">
      <div class="col-xs-12 col-md-6 col-lg-3 text-center">
        <strong>防屏蔽地址</strong>
        <a href="https://safe.example.com">safe</a>
      </div>
    </div>
  </body>
</html>
`

func TestParsePageLinksDeduplicatesMovieBoxes(t *testing.T) {
	links := ParsePageLinks(sampleHTML)

	if len(links) != 2 {
		t.Fatalf("expected 2 unique links, got %#v", links)
	}
	if links[0] != "https://www.javbus.com/ABF-055" || links[1] != "https://www.javbus.com/ABW-006" {
		t.Fatalf("unexpected links: %#v", links)
	}
}

func TestParsePageLinksWithDiagnosticsPreservesDuplicateEvidence(t *testing.T) {
	parsed := ParsePageLinksWithDiagnostics(sampleHTML)

	if len(parsed.Links) != 2 {
		t.Fatalf("expected two queue links, got %#v", parsed.Links)
	}
	if parsed.RawLinkCount != 3 {
		t.Fatalf("expected three source links, got %d", parsed.RawLinkCount)
	}
	if parsed.DuplicateEntryCount != 1 {
		t.Fatalf("expected one duplicate entry, got %d", parsed.DuplicateEntryCount)
	}
	if len(parsed.DuplicateItemIDs) != 1 || parsed.DuplicateItemIDs[0] != "ABF-055" {
		t.Fatalf("expected duplicate ABF-055, got %#v", parsed.DuplicateItemIDs)
	}
}

func TestParsePageLinksIncludesDetailLinksWithoutMovieBoxClass(t *testing.T) {
	links := ParsePageLinks(`
<html><body>
  <a class="thumbnail" href="https://www.javbus.com/FWAY-087">FWAY-087</a>
  <a href="/MIDA-438">MIDA-438</a>
  <a href="/genre/hd">not a movie</a>
</body></html>`)

	if len(links) != 2 {
		t.Fatalf("expected 2 detail links, got %#v", links)
	}
	if links[0] != "https://www.javbus.com/FWAY-087" || links[1] != "/MIDA-438" {
		t.Fatalf("unexpected links: %#v", links)
	}
}

func TestParseMetadataBuildsStructuredFields(t *testing.T) {
	metadata, err := ParseMetadata(sampleHTML)
	if err != nil {
		t.Fatalf("parse metadata: %v", err)
	}

	if metadata.GID != "12345" || metadata.UC != "7" {
		t.Fatalf("unexpected metadata ids: %#v", metadata)
	}
	if metadata.Title != "Sample Title" {
		t.Fatalf("unexpected title: %#v", metadata)
	}
	if len(metadata.Category) != 2 || len(metadata.Actress) != 1 {
		t.Fatalf("unexpected metadata lists: %#v", metadata)
	}
}

func TestParseMetadataExtractsMakerLabelAndSeries(t *testing.T) {
	html := sampleHTML + `
<p><span>Maker:</span><a>Studio Example</a></p>
<p><span>Label:</span><a>Example Label</a></p>
<p><span>Series:</span><a>Example Series</a></p>`
	metadata, err := ParseMetadata(html)
	if err != nil {
		t.Fatalf("parse metadata: %v", err)
	}
	if metadata.Maker != "Studio Example" || metadata.Label != "Example Label" || metadata.Series != "Example Series" {
		t.Fatalf("unexpected extended metadata: %#v", metadata)
	}
	projected := ParseFilmData(metadata, "https://www.javbus.com/ABF-055")
	if projected.Maker != metadata.Maker || projected.Label != metadata.Label || projected.Series != metadata.Series {
		t.Fatalf("extended metadata was not projected: %#v", projected)
	}
}

func TestExtractAntiBlockURLs(t *testing.T) {
	urls := ExtractAntiBlockURLs(sampleHTML)

	if len(urls) != 1 || urls[0] != "https://safe.example.com" {
		t.Fatalf("unexpected anti-block urls: %#v", urls)
	}
}

func TestParseReleaseDate(t *testing.T) {
	cases := []struct {
		name     string
		html     string
		expected string
	}{
		{
			name:     "chinese label with year-month-day",
			html:     `<p><span>發行日期:</span> 2023年05月12日</p>`,
			expected: "2023-05-12",
		},
		{
			name:     "hyphen separated release date",
			html:     `<div>Release Date: 2018-03-20</div>`,
			expected: "2018-03-20",
		},
		{
			name:     "no date label",
			html:     `<div>2018-03-20</div>`,
			expected: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseReleaseDate(tc.html)
			if got != tc.expected {
				t.Fatalf("ParseReleaseDate(%q) = %q, want %q", tc.html, got, tc.expected)
			}
		})
	}
}
