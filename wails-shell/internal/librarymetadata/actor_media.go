// Ownership summary:
//   This file resolves actor profile images through the embedded metadata engine.
//
// File map for maintainers:
//   1) ActorMedia type and provider-aware search.
//   2) Image URL selection and cooldown handling.
//   3) Error normalization for callers.
//
package librarymetadata

import (
	"context"
	"fmt"
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
	providerNames := make([]string, 0, 2)
	for _, preferred := range []string{"Gfriends", "ThePornDB"} {
		if actual, ok := providerExistsActor(providers, preferred); ok && !s.providerCoolingDown(actual) {
			providerNames = append(providerNames, actual)
		}
	}
	if len(providerNames) == 0 {
		for providerName := range providers {
			if !s.providerCoolingDown(providerName) {
				providerNames = append(providerNames, providerName)
			}
			if len(providerNames) >= 2 {
				break
			}
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

	media := ActorMedia{Name: name, Images: []string{}}
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
