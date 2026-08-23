package actresslookup

import (
	"context"
	"testing"
)

func TestActressWorksPageAcceptsOnlyVerifiedMirrorTargets(t *testing.T) {
	for _, targetURL := range []string{
		"https://www.javbus.com/star/14ac",
		"https://www.busjav.cyou/star/14ac",
		"https://fanbus.bond/star/14ac",
		"https://www.cdnbus.bond/star/14ac",
	} {
		if !IsAllowedActressLookupTargetURL(targetURL) {
			t.Fatalf("expected verified actor target to be accepted: %s", targetURL)
		}
	}

	for _, targetURL := range []string{
		"https://www.javbus.com.evil.example/star/14ac",
		"http://www.javbus.com/star/14ac",
		"https://www.javbus.com/movie/14ac",
		"https://www.javbus.com/star/14ac?next=https://example.com",
		"https://user@example.com/star/14ac",
		"https://127.0.0.1/star/14ac",
	} {
		if IsAllowedActressLookupTargetURL(targetURL) {
			t.Fatalf("unexpectedly accepted untrusted actor target: %s", targetURL)
		}
	}
}

func TestFetchWorksPageRejectsUntrustedTargetBeforeNetworkFetch(t *testing.T) {
	_, err := NewService().FetchWorksPage(context.Background(), "https://127.0.0.1/star/private", 1, "")
	if err == nil {
		t.Fatal("expected untrusted target to be rejected")
	}
}
