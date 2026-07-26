package crawlrequest

import (
	"errors"
	"testing"
)

func TestShouldFallbackToBrowserOnErrorDetectsTLSAndNetworkErrors(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"plain error", errors.New("something went wrong"), false},
		{"http 403", errors.New("HTTP 403"), true},
		{"cloudflare", errors.New("cloudflare challenge"), true},
		{"tls handshake", errors.New(`Get "https://www.javbus.com/star/13zx": tls: first record does not look like a TLS handshake`), true},
		{"tls colon", errors.New("tls: bad record MAC"), true},
		{"connection refused", errors.New("dial tcp: connect: connection refused"), true},
		{"no such host", errors.New("no such host"), true},
		{"i/o timeout", errors.New("read tcp: i/o timeout"), true},
		{"temporary failure in name resolution", errors.New("temporary failure in name resolution"), true},
		{"eof", errors.New("unexpected EOF"), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldFallbackToBrowserOnError(tc.err); got != tc.expected {
				t.Fatalf("shouldFallbackToBrowserOnError(%q) = %v, expected %v", tc.err, got, tc.expected)
			}
		})
	}
}
