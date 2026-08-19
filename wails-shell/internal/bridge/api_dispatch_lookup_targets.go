package bridge

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"javflow/internal/contracts/subscriptiontarget"
)

// Lookup target commands resolve or inspect actress/subscription targets.
// Keep them separate from ranking and anti-block helpers so target-shaping
// problems can be debugged without scanning unrelated lookup features.
//
// Target rule:
// these helpers may normalize actress target inputs/outputs, but they should
// stop short of launching crawls or mutating subscription state.
//
// Ownership summary:
// 1) route actress/subscription target lookup commands
// 2) keep target inspection/resolution separate from rankings and anti-block helpers
// 3) preserve the read-only boundary for lookup target helpers
//
// File map for maintainers:
// 1) lookup target command dispatcher
// 2) resolve/inspect target branches
func (a *API) handleLookupTargetCommand(command string, payload map[string]any) (string, bool, error) {
	switch command {
	case "app:remember-actress-alias":
		if a.lookup.actressLookup == nil {
			return "", true, fmt.Errorf("actress lookup service is not initialized")
		}
		canonical := strings.TrimSpace(nonEmptyString(payload["canonical"]))
		if canonical == "" {
			return "", true, fmt.Errorf("canonical actor name is required")
		}
		aliases := stringSliceValue(payload["aliases"])
		source := strings.TrimSpace(nonEmptyString(payload["source"]))
		if err := a.lookup.actressLookup.RememberAlias(canonical, aliases, source); err != nil {
			return "", true, err
		}
		return "{}", true, nil

	case "app:resolve-actress-alias":
		actorName := strings.TrimSpace(nonEmptyString(payload["actorName"]))
		if actorName == "" {
			return "", true, fmt.Errorf("actor name is required")
		}
		// The bundled/user alias index is synchronous and exact after normalization.
		// Prefer it before any provider request so Chinese aliases do not first fail
		// against a remote actor directory that only knows the Japanese spelling.
		if a.lookup.actressLookup != nil {
			resolution := a.lookup.actressLookup.ResolveAlias(actorName)
			if resolution.Unique && strings.TrimSpace(resolution.Canonical) != "" {
				result, err := marshalResult(map[string]any{
					"name":     resolution.Canonical,
					"aliases":  []string{actorName, resolution.MatchedAs},
					"provider": "local-alias-index",
				})
				return result, true, err
			}
		}
		if boolValue(payload["localOnly"], false) {
			// List filtering may call this while the user is typing. Keep that
			// path local-only so an unknown partial name never starts a provider
			// request or delays the UI.
			result, err := marshalResult(map[string]any{
				"name":     "",
				"aliases":  []string{},
				"provider": "local-alias-index",
			})
			return result, true, err
		}
		if a.libraryMetadata.library == nil {
			return "", true, fmt.Errorf("actor metadata service is not initialized")
		}
		ctx, cancel := a.requestContext(20 * time.Second)
		defer cancel()
		alias, err := a.libraryMetadata.library.ResolveActorAlias(ctx, actorName, strings.TrimSpace(nonEmptyString(payload["proxy"])))
		if err != nil {
			return "", true, err
		}
		result, err := marshalResult(alias)
		return result, true, err

	case "app:resolve-actress-crawl-target":
		if a.lookup.actressLookup == nil {
			return "", true, fmt.Errorf("actress lookup service is not initialized")
		}
		ctx, cancel := a.requestContext(50 * time.Second)
		defer cancel()
		target, err := a.lookup.actressLookup.ResolveTargetContext(ctx, a.buildActressLookupOptions(payload))
		if err != nil {
			return "", true, err
		}
		result, err := marshalResult(target)
		return result, true, err

	case "app:inspect-actress-target":
		if a.lookup.actressLookup == nil {
			return "", true, fmt.Errorf("actress lookup service is not initialized")
		}
		ctx, cancel := a.requestContext(50 * time.Second)
		defer cancel()
		profile, err := a.lookup.actressLookup.InspectTargetContext(ctx, a.buildActressLookupOptions(payload))
		if err != nil {
			return "", true, err
		}
		if boolValue(payload["cacheProfileMedia"], false) {
			cacheContext, cancel := a.requestContext(25 * time.Second)
			defer cancel()
			profile = a.cacheActressAtlasProfileMedia(cacheContext, profile, strings.TrimSpace(nonEmptyString(payload["proxy"])))
		}
		if boolValue(payload["cacheWorkCovers"], false) {
			cacheContext, cancel := a.requestContext(25 * time.Second)
			defer cancel()
			profile = a.cacheActressAtlasWorkCovers(cacheContext, profile, strings.TrimSpace(nonEmptyString(payload["proxy"])))
		}
		result, err := marshalResult(profile)
		return result, true, err

	case "app:cache-actress-work-covers":
		profile, err := actressAtlasWorkCoverProfile(payload)
		if err != nil {
			return "", true, err
		}
		cacheContext, cancel := a.requestContext(25 * time.Second)
		defer cancel()
		profile = a.cacheActressAtlasWorkCovers(cacheContext, profile, strings.TrimSpace(nonEmptyString(payload["proxy"])))
		result, err := marshalResult(map[string]any{"works": profile.Works})
		return result, true, err

	case "app:load-actress-works-page":
		if a.lookup.actressLookup == nil {
			return "", true, fmt.Errorf("actress lookup service is not initialized")
		}
		page := intValue(payload["page"], 1)
		if page < 1 {
			page = 1
		}
		ctx, cancel := a.requestContext(35 * time.Second)
		defer cancel()
		works, err := a.lookup.actressLookup.FetchWorksPage(ctx, nonEmptyString(payload["targetUrl"]), page, strings.TrimSpace(nonEmptyString(payload["proxy"])))
		if err != nil {
			return "", true, err
		}
		result, err := marshalResult(works)
		return result, true, err

	case "app:resolve-actress-media":
		if a.libraryMetadata.library == nil {
			return "", true, fmt.Errorf("actor media service is not initialized")
		}
		actorName := strings.TrimSpace(nonEmptyString(payload["actorName"]))
		if actorName == "" {
			return "", true, fmt.Errorf("actor name is required")
		}
		ctx, cancel := a.requestContext(30 * time.Second)
		defer cancel()
		media, err := a.libraryMetadata.library.ResolveActorMedia(ctx, actorName, strings.TrimSpace(nonEmptyString(payload["proxy"])))
		if err != nil && len(media.Images) == 0 {
			return "", true, err
		}
		// Reuse the subscription media asset route. It is same-origin in Wails,
		// so WebView2 can display cached actor images even when a provider later
		// rejects direct hotlinks. Atlas media has its own hashed subdirectory and
		// never mutates subscription records.
		mediaDir := filepath.Join(a.runtime.paths.UserData, "subscriptions-v2", "media", actressAtlasMediaKey(actorName))
		sourceImages := append([]string(nil), media.Images...)
		localURLs, cacheErr := cacheSubscriptionMedia(ctx, media.Images, mediaDir, strings.TrimSpace(nonEmptyString(payload["proxy"])))
		if cacheErr != nil && len(localURLs) == 0 {
			return "", true, cacheErr
		}
		media.Images = distinctAtlasProfileMedia(a.runtime.paths.UserData, "", localURLs)
		media.SourceImages = sourceImages
		result, marshalErr := marshalResult(media)
		return result, true, marshalErr
	}

	return "", false, nil
}

// actressAtlasWorkCoverProfile accepts only the displayed JAVBus work slice.
// The media cache validates every work again before it makes an HTTP request,
// so a page-turn cannot be used as an arbitrary URL fetch primitive.
func actressAtlasWorkCoverProfile(payload map[string]any) (subscriptiontarget.TargetProfile, error) {
	actorName := strings.TrimSpace(nonEmptyString(payload["actressName"]))
	if actorName == "" {
		return subscriptiontarget.TargetProfile{}, fmt.Errorf("actress name is required")
	}
	encoded, err := marshalResult(payload["works"])
	if err != nil {
		return subscriptiontarget.TargetProfile{}, err
	}
	var works []subscriptiontarget.ActressWork
	if err := json.Unmarshal([]byte(encoded), &works); err != nil || len(works) == 0 || len(works) > actressAtlasWorkCoverLimit {
		return subscriptiontarget.TargetProfile{}, fmt.Errorf("work cover prefetch must contain between 1 and %d works", actressAtlasWorkCoverLimit)
	}
	for _, work := range works {
		if !isAllowedActressAtlasWork(work) {
			return subscriptiontarget.TargetProfile{}, fmt.Errorf("work cover page contains an untrusted JAVBus URL")
		}
	}
	return subscriptiontarget.TargetProfile{ResolvedActressName: actorName, Works: works}, nil
}

func actressAtlasMediaKey(actorName string) string {
	sum := sha1.Sum([]byte(strings.ToLower(strings.TrimSpace(actorName))))
	return fmt.Sprintf("atlas-%x", sum[:6])
}
