package netguard

import (
	"context"
	"net/url"
	"testing"
)

func TestValidatePublicURLRejectsPrivateAndUnsupportedTargets(t *testing.T) {
	for _, rawURL := range []string{
		"http://127.0.0.1/image.png",
		"http://10.0.0.8/file.zip",
		"http://[::1]/file.zip",
		"file:///C:/private.zip",
	} {
		target, err := url.Parse(rawURL)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidatePublicURL(context.Background(), target); err == nil {
			t.Fatalf("expected unsafe URL to be rejected: %s", rawURL)
		}
	}
}

func TestResolvePublicHostAllowsPublicLiteral(t *testing.T) {
	addresses, err := ResolvePublicHost(context.Background(), "8.8.8.8")
	if err != nil {
		t.Fatalf("resolve public literal: %v", err)
	}
	if len(addresses) != 1 || addresses[0].String() != "8.8.8.8" {
		t.Fatalf("unexpected public addresses: %#v", addresses)
	}
}
