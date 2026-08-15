// Package actresslookup resolves verified actress profiles without taking
// ownership of crawl workflow state or Cloudflare bypass implementation.
//
// Maintenance boundary:
// - accept a name and return a verified actress target/profile
// - keep alias lookup, provider enrichment, and page parsing inside this package
// - route verification-page retrieval to the existing crawlfetch capability
// - never duplicate or modify the crawler's Cloudflare handling
//
// Ownership summary:
// 1) resolve only collision-safe local aliases before a provider request
// 2) persist aliases only after a canonical external result is verified
// 3) keep alias logic independent from HTTP transport and HTML parsing
//
// File map for maintainers:
// 1) alias.go: local Chinese/Japanese alias resolution and persistence
// 2) profile.go: actor profile response shape and normalization
// 3) works.go: paged work parsing and media cache helpers
// 4) service.go: request orchestration and existing crawler routing
package actresslookup

import (
	"fmt"
	"strings"
	"time"

	"javflow/internal/actressalias"
)

// resolveAliasQuery applies only a unique, verified alias automatically.
// Fuzzy or colliding records remain visible errors rather than silently
// opening a similarly named performer's directory.
func (s *Service) resolveAliasQuery(value string) (string, actressalias.Resolution, error) {
	query := strings.TrimSpace(value)
	resolution := actressalias.Resolution{Query: query, MatchKind: "missing"}
	if s != nil && s.aliases != nil {
		resolution = s.aliases.Resolve(query)
	}
	if resolution.Unique && strings.TrimSpace(resolution.Canonical) != "" {
		return resolution.Canonical, resolution, nil
	}
	if len(resolution.Candidates) > 0 {
		names := make([]string, 0, len(resolution.Candidates))
		for _, candidate := range resolution.Candidates {
			names = append(names, candidate.Canonical)
		}
		return "", resolution, fmt.Errorf("actor alias is ambiguous: %s", strings.Join(uniqStrings(names...), ", "))
	}
	return query, resolution, nil
}

// ResolveAlias exposes the local, collision-safe alias index to bridge callers.
// It does not make a provider request and callers may auto-fill a name only when
// Unique is true; missing and ambiguous results remain explicit for fallback UI.
func (s *Service) ResolveAlias(value string) actressalias.Resolution {
	query := strings.TrimSpace(value)
	if s == nil || s.aliases == nil {
		return actressalias.Resolution{Query: query, MatchKind: "missing"}
	}
	return s.aliases.Resolve(query)
}

// RememberAlias is called by the bridge only after an external metadata result
// has also been verified against a canonical JAV directory.
func (s *Service) RememberAlias(canonical string, aliases []string, source string) error {
	if s == nil || s.aliases == nil {
		return fmt.Errorf("actress alias index is not initialized")
	}
	return s.aliases.Remember(actressalias.Record{Canonical: canonical, Aliases: aliases, Source: source, Confidence: "provider-and-jav-verified", UpdatedAt: time.Now().Format(time.RFC3339)})
}

// rememberProviderAliases persists only provider-supplied aliases after a
// canonical JAV directory has already been resolved.
func (s *Service) rememberProviderAliases(canonical string, fields map[string]string) {
	if s == nil || s.aliases == nil || strings.TrimSpace(canonical) == "" {
		return
	}
	aliases := make([]string, 0)
	for _, key := range []string{"别名", "alternateName", "additionalName"} {
		for _, value := range strings.FieldsFunc(fields[key], func(r rune) bool {
			return r == '/' || r == '、' || r == ',' || r == '，' || r == '\n'
		}) {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				aliases = append(aliases, trimmed)
			}
		}
	}
	if len(aliases) == 0 {
		return
	}
	_ = s.aliases.Remember(actressalias.Record{
		Canonical: canonical, Aliases: aliases, Source: "minnano-av", Confidence: "provider-verified", UpdatedAt: time.Now().Format(time.RFC3339),
	})
}

// siteSearchName converts common Chinese character variants before a request is
// sent. Comparisons use normalizeName; source requests need the spelling the
// Japanese directory actually indexes.
func siteSearchName(value string) string {
	return actressalias.DirectoryName(value)
}
