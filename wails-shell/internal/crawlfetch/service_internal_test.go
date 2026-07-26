package crawlfetch

import (
	"reflect"
	"testing"
)

func TestBaseURLListIncludesPrimaryAndAntiBlockURLs(t *testing.T) {
	service, err := NewService(ServiceOptions{
		AntiBlockURLs: []string{
			"https://mirror-a.example.com",
			"https://mirror-b.example.com",
			"https://mirror-a.example.com", // duplicate
			"",
		},
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	bases := service.baseURLList("https://www.javbus.com")
	expected := []string{
		"https://www.javbus.com",
		"https://mirror-a.example.com",
		"https://mirror-b.example.com",
	}
	if !reflect.DeepEqual(bases, expected) {
		t.Fatalf("baseURLList() = %#v, expected %#v", bases, expected)
	}
}

func TestBaseURLListDefaultsToJavBusWhenPrimaryEmpty(t *testing.T) {
	service, err := NewService(ServiceOptions{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	bases := service.baseURLList("")
	if len(bases) != 1 || bases[0] != "https://www.javbus.com" {
		t.Fatalf("unexpected bases: %#v", bases)
	}
}

func TestDetailURLListReplacesHostWithAntiBlockMirrors(t *testing.T) {
	service, err := NewService(ServiceOptions{
		AntiBlockURLs: []string{"https://mirror.example.com"},
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	urls := service.detailURLList("https://www.javbus.com/ABF-055")
	expected := []string{
		"https://www.javbus.com/ABF-055",
		"https://mirror.example.com/ABF-055",
	}
	if !reflect.DeepEqual(urls, expected) {
		t.Fatalf("detailURLList() = %#v, expected %#v", urls, expected)
	}
}

func TestDetailURLListPreservesPathAndQuery(t *testing.T) {
	service, err := NewService(ServiceOptions{
		AntiBlockURLs: []string{"https://mirror.example.com"},
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	urls := service.detailURLList("https://www.javbus.com/star/13zx?page=2")
	expected := []string{
		"https://www.javbus.com/star/13zx?page=2",
		"https://mirror.example.com/star/13zx?page=2",
	}
	if !reflect.DeepEqual(urls, expected) {
		t.Fatalf("detailURLList() = %#v, expected %#v", urls, expected)
	}
}
