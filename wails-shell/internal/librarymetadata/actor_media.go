// Ownership summary:
//
//	This file resolves actor profile images through the embedded metadata engine.
//
// File map for maintainers:
//  1. ActorMedia type and provider-aware search.
//  2. Image URL selection and cooldown handling.
//  3. Error normalization for callers.
package librarymetadata

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	metatubemodel "github.com/metatube-community/metatube-sdk-go/model"
)

// ActorMedia is the small actor-photo surface shared with AV subscriptions.
// The caller owns persistence and local file caching.
type ActorMedia struct {
	Name     string   `json:"name"`
	Homepage string   `json:"homepage,omitempty"`
	Images   []string `json:"images,omitempty"`
	// SourceImages preserves provider URLs before the bridge rewrites them to
	// same-origin cached files. The Actor Atlas uses them for cross-source
	// avatar/public-photo de-duplication.
	SourceImages []string `json:"sourceImages,omitempty"`
}

// ActorAlias is the metadata-only subset used by Actor Atlas name search. It
// deliberately excludes images so alias discovery cannot reintroduce the
// retired photo/gallery workflow.
type ActorAlias struct {
	Name     string   `json:"name"`
	Aliases  []string `json:"aliases,omitempty"`
	Homepage string   `json:"homepage,omitempty"`
	Provider string   `json:"provider,omitempty"`
}

// ResolveActorAlias reuses the same provider policy and exact matching as the
// media lookup, but returns names only. A provider result is accepted only when
// the user's query matches its name or one of its declared aliases.
func (s *Service) ResolveActorAlias(ctx context.Context, actorName string, requestedProxy string) (ActorAlias, error) {
	name := strings.TrimSpace(actorName)
	if name == "" {
		return ActorAlias{}, fmt.Errorf("actor name is required")
	}
	eng, _ := s.engineForProxy(requestedProxy)
	if eng == nil {
		return ActorAlias{}, fmt.Errorf("metadata engine is unavailable")
	}
	providers := eng.GetActorProviders()
	providerNames := make([]string, 0, 3)
	seenProviders := make(map[string]struct{})
	for _, preferred := range []string{"Gfriends", "ThePornDB"} {
		if actual, ok := providerExistsActor(providers, preferred); ok && !s.providerCoolingDown(actual) {
			providerNames = append(providerNames, actual)
			seenProviders[strings.ToLower(actual)] = struct{}{}
		}
	}
	remaining := make([]string, 0, len(providers))
	for providerName := range providers {
		if _, exists := seenProviders[strings.ToLower(providerName)]; exists || s.providerCoolingDown(providerName) {
			continue
		}
		remaining = append(remaining, providerName)
	}
	sort.Strings(remaining)
	for _, providerName := range remaining {
		providerNames = append(providerNames, providerName)
		if len(providerNames) >= 3 {
			break
		}
	}
	for _, providerName := range providerNames {
		searchCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		results, err := searchActorWithContext(searchCtx, eng, name, providerName)
		cancel()
		if err != nil {
			s.recordProviderResult(providerName, err)
			continue
		}
		s.recordProviderResult(providerName, nil)
		matched := selectActorMediaResult(results, name)
		if matched == nil {
			continue
		}
		aliases := make([]string, 0, len(matched.Aliases))
		seen := map[string]struct{}{}
		for _, alias := range matched.Aliases {
			alias = strings.TrimSpace(alias)
			if alias == "" || strings.EqualFold(alias, matched.Name) {
				continue
			}
			key := strings.ToLower(alias)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			aliases = append(aliases, alias)
		}
		if len(aliases) == 0 {
			// A canonical-only result is useful for photos but cannot improve the
			// alias index. Continue to the next provider before giving up.
			continue
		}
		canonicalName := chooseActorCanonicalName(matched.Name, aliases)
		return ActorAlias{Name: canonicalName, Aliases: append([]string{strings.TrimSpace(matched.Name)}, aliases...), Homepage: strings.TrimSpace(matched.Homepage), Provider: providerName}, nil
	}
	return ActorAlias{}, fmt.Errorf("no exact actor alias match")
}

func chooseActorCanonicalName(primary string, aliases []string) string {
	for _, candidate := range aliases {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		for _, runeValue := range candidate {
			if (runeValue >= '\u3040' && runeValue <= '\u30ff') || strings.ContainsRune("瀬戸環亜優沢橋桜島辺葉織風斉園宮岡", runeValue) {
				return candidate
			}
		}
	}
	return strings.TrimSpace(primary)
}

// ResolveActorMedia searches the preferred actor providers and returns a
// bounded set of profile/publicity image URLs. It uses the same proxy-aware
// MetaTube engines and provider cooldowns as library scraping.
func (s *Service) ResolveActorMedia(ctx context.Context, actorName string, requestedProxy string) (ActorMedia, error) {
	name := strings.TrimSpace(actorName)
	if name == "" {
		return ActorMedia{}, fmt.Errorf("actor name is required")
	}
	eng, _ := s.engineForProxy(requestedProxy)
	if eng == nil {
		return ActorMedia{}, fmt.Errorf("metadata engine is unavailable")
	}

	providers := eng.GetActorProviders()
	providerNames := make([]string, 0, 3)
	seenProviders := make(map[string]struct{})
	for _, preferred := range []string{"Gfriends", "ThePornDB"} {
		if actual, ok := providerExistsActor(providers, preferred); ok && !s.providerCoolingDown(actual) {
			providerNames = append(providerNames, actual)
			seenProviders[strings.ToLower(actual)] = struct{}{}
		}
	}
	// Keep the scraper's provider policy, but use additional configured actor
	// providers when they can contribute distinct real publicity photos. Stable
	// ordering keeps results reproducible and caps optional enrichment latency.
	remainingProviders := make([]string, 0, len(providers))
	for providerName := range providers {
		if _, exists := seenProviders[strings.ToLower(providerName)]; exists || s.providerCoolingDown(providerName) {
			continue
		}
		remainingProviders = append(remainingProviders, providerName)
	}
	sort.Strings(remainingProviders)
	for _, providerName := range remainingProviders {
		providerNames = append(providerNames, providerName)
		if len(providerNames) >= 3 {
			break
		}
	}
	if len(providerNames) == 0 {
		return ActorMedia{}, fmt.Errorf("no actor metadata provider is available")
	}

	actorSemaphore := s.actorSemaphore
	if actorSemaphore == nil {
		actorSemaphore = make(chan struct{}, 1)
	}
	select {
	case actorSemaphore <- struct{}{}:
		defer func() { <-actorSemaphore }()
	case <-ctx.Done():
		return ActorMedia{}, ctx.Err()
	}

	media := ActorMedia{Name: name, Images: []string{}, SourceImages: []string{}}
	seenImages := map[string]struct{}{}
	var lastErr error
	for _, providerName := range providerNames {
		searchCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		results, err := searchActorWithContext(searchCtx, eng, name, providerName)
		cancel()
		if err != nil {
			lastErr = err
			s.recordProviderResult(providerName, err)
			continue
		}
		s.recordProviderResult(providerName, nil)
		matched := selectActorMediaResult(results, name)
		if matched == nil {
			continue
		}
		if media.Homepage == "" {
			media.Homepage = strings.TrimSpace(matched.Homepage)
		}
		if strings.TrimSpace(matched.Name) != "" {
			media.Name = strings.TrimSpace(matched.Name)
		}
		for _, imageURL := range matched.Images {
			trimmed := strings.TrimSpace(imageURL)
			if trimmed == "" {
				continue
			}
			if _, exists := seenImages[trimmed]; exists {
				continue
			}
			seenImages[trimmed] = struct{}{}
			media.Images = append(media.Images, trimmed)
			media.SourceImages = append(media.SourceImages, trimmed)
			if len(media.Images) >= 8 {
				return media, nil
			}
		}
	}
	if len(media.Images) == 0 && lastErr != nil {
		return media, lastErr
	}
	return media, nil
}

func searchActorWithContext(ctx context.Context, eng metadataEngine, name string, providerName string) ([]*metatubemodel.ActorSearchResult, error) {
	type response struct {
		items []*metatubemodel.ActorSearchResult
		err   error
	}
	resultCh := make(chan response, 1)
	go func() {
		items, err := eng.SearchActor(name, providerName, true)
		resultCh <- response{items: items, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-resultCh:
		return result.items, result.err
	}
}

func selectActorMediaResult(results []*metatubemodel.ActorSearchResult, actorName string) *metatubemodel.ActorSearchResult {
	target := normalizeActorMediaName(actorName)
	var firstValid *metatubemodel.ActorSearchResult
	for _, result := range results {
		if result == nil || !result.IsValid() {
			continue
		}
		if firstValid == nil {
			firstValid = result
		}
		if normalizeActorMediaName(result.Name) == target {
			return result
		}
		for _, alias := range result.Aliases {
			if normalizeActorMediaName(alias) == target {
				return result
			}
		}
	}
	if len(results) == 1 {
		return firstValid
	}
	return nil
}

func normalizeActorMediaName(value string) string {
	return strings.ToLower(strings.NewReplacer(
		" ", "", "\t", "", "\r", "", "\n", "", "·", "", "・", "", "•", "",
		"(", "", ")", "", "（", "", "）", "",
	).Replace(strings.TrimSpace(value)))
}
