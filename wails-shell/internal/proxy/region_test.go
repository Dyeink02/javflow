package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckProxyRegionJapan(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ip":"203.0.113.9","city":"Tokyo","country":"JP"}`))
	}))
	defer server.Close()
	previous := geoEndpoints
	geoEndpoints = []string{server.URL}
	defer func() { geoEndpoints = previous }()

	result := NewService().CheckProxyRegion("")
	if result.Status != "japan" || result.Country != "日本" || result.IP != "203.0.113.9" {
		t.Fatalf("unexpected Japan result: %#v", result)
	}
	if result.ViaProxy {
		t.Fatal("empty proxy must report ViaProxy=false")
	}
}

func TestCheckProxyRegionForeignIncludesCountry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"ip":"198.51.100.4","country":"US"}`))
	}))
	defer server.Close()
	previous := geoEndpoints
	geoEndpoints = []string{server.URL}
	defer func() { geoEndpoints = previous }()

	result := NewService().CheckProxyRegion("")
	if result.Status != "foreign" || result.Country != "美国" || result.Message != "非日本节点（美国）" {
		t.Fatalf("unexpected foreign result: %#v", result)
	}
}

func TestCheckProxyRegionFallsBackToSecondEndpoint(t *testing.T) {
	broken := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "boom", http.StatusInternalServerError)
	}))
	defer broken.Close()
	working := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"country":"JP","query":"203.0.113.2"}`))
	}))
	defer working.Close()
	previous := geoEndpoints
	geoEndpoints = []string{broken.URL, working.URL}
	defer func() { geoEndpoints = previous }()

	result := NewService().CheckProxyRegion("")
	if result.Status != "japan" || result.IP != "203.0.113.2" {
		t.Fatalf("unexpected fallback result: %#v", result)
	}
}

func TestCheckProxyRegionInvalidProxyFormat(t *testing.T) {
	result := NewService().CheckProxyRegion("bad proxy")
	if result.Status != "invalid" || !result.ViaProxy {
		t.Fatalf("unexpected invalid proxy result: %#v", result)
	}
}

func TestCountryDisplayNameUnknownFallsBackToCode(t *testing.T) {
	if got := countryDisplayName("zz"); got != "ZZ" {
		t.Fatalf("countryDisplayName(zz) = %q, expected ZZ", got)
	}
}
