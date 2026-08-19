// Package netguard validates outbound public-network requests.
//
// Ownership summary:
// 1) validate remote URLs and resolved addresses before outbound requests
// 2) provide a DNS-rebinding-resistant direct transport for shared consumers
// 3) keep network-target safety independent from crawler and business domains
//
// File map for maintainers:
// 1) public URL and DNS-address validation
// 2) guarded direct HTTP transport construction
// 3) redirect validation policy
package netguard

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const lookupTimeout = 3 * time.Second

// ValidatePublicURL accepts only HTTP(S) URLs whose host resolves exclusively
// to public addresses. Call it for the initial URL and every redirect.
func ValidatePublicURL(ctx context.Context, target *url.URL) error {
	if target == nil || (!strings.EqualFold(target.Scheme, "http") && !strings.EqualFold(target.Scheme, "https")) {
		return fmt.Errorf("only http/https URLs are allowed")
	}
	host := strings.TrimSpace(target.Hostname())
	if host == "" {
		return fmt.Errorf("URL host is required")
	}
	_, err := ResolvePublicHost(ctx, host)
	return err
}

// ResolvePublicHost resolves a host and rejects the complete request when any
// returned address is local, private, link-local, multicast, or unspecified.
func ResolvePublicHost(ctx context.Context, host string) ([]net.IP, error) {
	normalized := strings.TrimSpace(strings.Trim(host, "[]"))
	if normalized == "" || strings.EqualFold(normalized, "localhost") {
		return nil, fmt.Errorf("URL must not target localhost or a private network")
	}
	if ip := net.ParseIP(normalized); ip != nil {
		if isForbiddenIP(ip) {
			return nil, fmt.Errorf("URL must not target localhost or a private network")
		}
		return []net.IP{ip}, nil
	}

	lookupCtx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupIPAddr(lookupCtx, normalized)
	if err != nil {
		return nil, fmt.Errorf("URL host lookup failed: %w", err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("URL host did not resolve to an address")
	}
	result := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		if isForbiddenIP(address.IP) {
			return nil, fmt.Errorf("URL host resolved to localhost or a private network")
		}
		result = append(result, address.IP)
	}
	return result, nil
}

func isForbiddenIP(ip net.IP) bool {
	return ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}

// NewPublicTransport dials a validated resolved IP directly. This prevents a
// host from passing URL validation and then changing its DNS answer at dial time.
func NewPublicTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := ResolvePublicHost(ctx, host)
		if err != nil {
			return nil, err
		}
		var lastErr error
		for _, ip := range addresses {
			connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return connection, nil
			}
			lastErr = dialErr
		}
		return nil, lastErr
	}
	return transport
}

// ApplyRedirectPolicy limits redirects and validates every redirect target.
func ApplyRedirectPolicy(client *http.Client) {
	if client == nil {
		return
	}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		return ValidatePublicURL(request.Context(), request.URL)
	}
}
