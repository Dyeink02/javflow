package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"javflow/internal/contracts/subscriptiontarget"
)

// TestLiveActorAtlasDetail uses the same bridge command as the Actor Atlas
// renderer. It is opt-in because it contacts public providers through the
// user's configured proxy and writes only the normal application media cache.
func TestLiveActorAtlasDetail(t *testing.T) {
	if os.Getenv("JAVFLOW_ACTRESS_ATLAS_LIVE_TEST") != "1" {
		t.Skip("set JAVFLOW_ACTRESS_ATLAS_LIVE_TEST=1 to run the Actor Atlas live check")
	}

	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(repoRoot)
	result, err := app.Call("app:inspect-actress-target", map[string]any{
		"actressName":       "瀬戸環奈",
		"targetUrl":         "",
		"proxy":             "http://127.0.0.1:7897",
		"includeProfile":    true,
		"cacheProfileMedia": true,
		"cacheWorkCovers":   true,
	})
	if err != nil {
		t.Fatalf("actor atlas lookup failed: %v", err)
	}

	var profile subscriptiontarget.TargetProfile
	if err := json.Unmarshal([]byte(result), &profile); err != nil {
		t.Fatalf("invalid actor atlas result: %v", err)
	}
	if strings.TrimSpace(profile.ResolvedActressName) == "" {
		t.Fatalf("missing resolved actress identity: %+v", profile)
	}
	if !strings.HasPrefix(profile.AvatarURL, "/subscription-media/") {
		t.Fatalf("profile avatar did not use the local route: %q", profile.AvatarURL)
	}
	for _, work := range profile.Works {
		if !strings.HasPrefix(work.CoverURL, "/subscription-media/") {
			continue
		}
		relative := strings.TrimPrefix(work.CoverURL, "/subscription-media/")
		cachedFile := filepath.Join(app.paths.UserData, "subscriptions-v2", "media", filepath.FromSlash(relative))
		if info, statErr := os.Stat(cachedFile); statErr != nil || info.Size() == 0 {
			t.Fatalf("cached work cover is not readable: %s (%v)", cachedFile, statErr)
		}
		return
	}
	t.Fatalf("no local work cover returned by actor detail: %+v", profile.Works)
}

// TestLiveActorAtlasNamedProfiles checks real lookup rather than the bundled
// ranking snapshot. It is opt-in because the public providers and the user's
// proxy are external dependencies; no profile is written into a bundled cache.
func TestLiveActorAtlasNamedProfiles(t *testing.T) {
	if os.Getenv("JAVFLOW_ACTRESS_ATLAS_LIVE_TEST") != "1" {
		t.Skip("set JAVFLOW_ACTRESS_ATLAS_LIVE_TEST=1 to run the Actor Atlas live check")
	}
	app := NewApp(".")
	for _, actressName := range []string{"三上悠亚", "小宵こなん", "miru"} {
		result, err := app.Call("app:inspect-actress-target", map[string]any{
			"actressName":    actressName,
			"targetUrl":      "",
			"proxy":          "http://127.0.0.1:7897",
			"includeProfile": true,
		})
		if err != nil {
			t.Errorf("%s lookup failed: %v", actressName, err)
			continue
		}
		var profile subscriptiontarget.TargetProfile
		if err := json.Unmarshal([]byte(result), &profile); err != nil {
			t.Errorf("%s returned invalid profile: %v", actressName, err)
			continue
		}
		if strings.TrimSpace(profile.ResolvedActressName) == "" || profile.AllCount <= 0 {
			t.Errorf("%s returned incomplete real profile: %+v", actressName, profile)
		}
		t.Logf("%s: resolved=%s count=%d fields=%v avatar=%t photos=%d works=%d sources=%v", actressName, profile.ResolvedActressName, profile.AllCount, profile.ProfileFields, strings.TrimSpace(profile.AvatarURL) != "", len(profile.PromotionImageURLs), len(profile.Works), profile.DataSources)
	}
}

// TestLiveActorAtlasWorksPage verifies that a second source page is fetched
// on demand instead of pretending the first 30 works are the full catalogue.
func TestLiveActorAtlasWorksPage(t *testing.T) {
	if os.Getenv("JAVFLOW_ACTRESS_ATLAS_LIVE_TEST") != "1" {
		t.Skip("set JAVFLOW_ACTRESS_ATLAS_LIVE_TEST=1 to run the Actor Atlas page check")
	}
	app := NewApp(".")
	profileJSON, err := app.Call("app:inspect-actress-target", map[string]any{
		"actressName":    "三上悠亚",
		"proxy":          "http://127.0.0.1:7897",
		"includeProfile": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var profile subscriptiontarget.TargetProfile
	if err := json.Unmarshal([]byte(profileJSON), &profile); err != nil {
		t.Fatal(err)
	}
	result, err := app.Call("app:load-actress-works-page", map[string]any{
		"actressName": "三上悠亚",
		"targetUrl":   profile.ResolvedBase,
		"page":        2,
		"proxy":       "http://127.0.0.1:7897",
	})
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Page     int                              `json:"page"`
		AllCount int                              `json:"allCount"`
		Works    []subscriptiontarget.ActressWork `json:"works"`
	}
	if err := json.Unmarshal([]byte(result), &page); err != nil {
		t.Fatal(err)
	}
	if page.Page != 2 || page.AllCount <= 30 || len(page.Works) == 0 {
		t.Fatalf("unexpected second page: %+v", page)
	}
}

// TestLiveActorAtlasPrefetchBatch calls the exact bridge command used after a
// UI page turn. It verifies that a 24-work N+1/N+2/N+3 batch is accepted and
// at least one remote cover becomes a readable local media route.
func TestLiveActorAtlasPrefetchBatch(t *testing.T) {
	if os.Getenv("JAVFLOW_ACTRESS_ATLAS_LIVE_TEST") != "1" {
		t.Skip("set JAVFLOW_ACTRESS_ATLAS_LIVE_TEST=1 to run the Actor Atlas prefetch check")
	}
	app := NewApp(".")
	profileJSON, err := app.Call("app:inspect-actress-target", map[string]any{
		"actressName":    "三上悠亚",
		"proxy":          "http://127.0.0.1:7897",
		"includeProfile": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var profile subscriptiontarget.TargetProfile
	if err := json.Unmarshal([]byte(profileJSON), &profile); err != nil {
		t.Fatal(err)
	}
	if len(profile.Works) < 24 {
		t.Fatalf("expected at least 24 works for the cover-cache check, got %d", len(profile.Works))
	}
	currentResult, err := app.Call("app:cache-actress-work-covers", map[string]any{
		"actressName": profile.ResolvedActressName,
		"works":       profile.Works[:8],
		"proxy":       "http://127.0.0.1:7897",
	})
	if err != nil {
		t.Fatalf("current first-page cache command failed: %v", err)
	}
	var currentResponse struct {
		Works []subscriptiontarget.ActressWork `json:"works"`
	}
	if err := json.Unmarshal([]byte(currentResult), &currentResponse); err != nil {
		t.Fatal(err)
	}
	if len(currentResponse.Works) != 8 || !strings.HasPrefix(currentResponse.Works[0].CoverURL, "/subscription-media/") {
		t.Fatalf("first page was not cached locally: %+v", currentResponse.Works)
	}

	prefetchEnd := 8 + 24
	if prefetchEnd > len(profile.Works) {
		prefetchEnd = len(profile.Works)
	}
	prefetchWorks := profile.Works[8:prefetchEnd]
	result, err := app.Call("app:cache-actress-work-covers", map[string]any{
		"actressName": profile.ResolvedActressName,
		"works":       prefetchWorks,
		"proxy":       "http://127.0.0.1:7897",
	})
	if err != nil {
		t.Fatalf("background prefetch command failed: %v", err)
	}
	var response struct {
		Works []subscriptiontarget.ActressWork `json:"works"`
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Works) != len(prefetchWorks) {
		t.Fatalf("unexpected cached work count: %d", len(response.Works))
	}
	for _, work := range response.Works {
		if strings.HasPrefix(work.CoverURL, "/subscription-media/") {
			return
		}
	}
	t.Fatalf("no local cover route returned for prefetch batch: %+v", response.Works)
}
