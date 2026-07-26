package subcrawl

import (
	"strings"
	"testing"

	"javflow/internal/crawlparse"
)

func TestBuildMagnetAjaxURLUsesNormalizedImageParam(t *testing.T) {
	metadata := crawlparse.Metadata{
		GID: "123456",
		UC:  "0",
		Img: `\/storage\/thumb\/abc123.jpg`,
	}

	ajaxURL := buildMagnetAjaxURL(metadata, "https://www.javbus.com")

	if !strings.Contains(ajaxURL, "img=storage/thumb/abc123.jpg") {
		t.Fatalf("expected normalized img parameter, got %q", ajaxURL)
	}
	if strings.Contains(ajaxURL, "%255C") || strings.Contains(ajaxURL, "%5C%2F") {
		t.Fatalf("expected no double-escaped img parameter, got %q", ajaxURL)
	}
}
