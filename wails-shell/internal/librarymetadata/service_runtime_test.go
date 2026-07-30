package librarymetadata

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/metatube-community/metatube-sdk-go/engine/providerid"
	metatubemodel "github.com/metatube-community/metatube-sdk-go/model"
	"github.com/metatube-community/metatube-sdk-go/provider"
)

type fakeMetadataEngine struct {
	movieProviders map[string]provider.MovieProvider
	searchMovie    func(string, string, bool) ([]*metatubemodel.MovieSearchResult, error)
	movieInfo      func(providerid.ProviderID, bool) (*metatubemodel.MovieInfo, error)
}

func (f *fakeMetadataEngine) GetMovieProviders() map[string]provider.MovieProvider {
	if f.movieProviders == nil {
		return map[string]provider.MovieProvider{}
	}
	return f.movieProviders
}

func (f *fakeMetadataEngine) GetActorProviders() map[string]provider.ActorProvider {
	return map[string]provider.ActorProvider{}
}

func (f *fakeMetadataEngine) SearchMovie(keyword, name string, fallback bool) ([]*metatubemodel.MovieSearchResult, error) {
	if f.searchMovie == nil {
		return nil, nil
	}
	return f.searchMovie(keyword, name, fallback)
}

func (f *fakeMetadataEngine) GetMovieInfoByProviderID(id providerid.ProviderID, lazy bool) (*metatubemodel.MovieInfo, error) {
	if f.movieInfo == nil {
		return nil, nil
	}
	return f.movieInfo(id, lazy)
}

func (f *fakeMetadataEngine) SearchActor(string, string, bool) ([]*metatubemodel.ActorSearchResult, error) {
	return nil, nil
}

func newTestMetadataService(eng metadataEngine) *Service {
	return &Service{
		engine:         eng,
		engines:        map[string]metadataEngine{"": eng},
		semaphore:      make(chan struct{}, 5),
		cache:          map[string]metadataCacheEntry{},
		providerHealth: map[string]*providerHealth{},
		actorCache:     map[string]actorImageCacheEntry{},
		actorSemaphore: make(chan struct{}, 4),
		jobs:           map[string]context.CancelFunc{},
		jobContexts:    map[string]context.Context{},
		LogManager:     &LogManager{},
	}
}

func TestLibraryMetadataJobCancellationInterruptsProviderWait(t *testing.T) {
	providerRelease := make(chan struct{})
	eng := &fakeMetadataEngine{
		searchMovie: func(string, string, bool) ([]*metatubemodel.MovieSearchResult, error) {
			<-providerRelease
			return nil, nil
		},
	}
	svc := newTestMetadataService(eng)
	if err := svc.StartJob("cancel-test"); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := svc.ScrapeByNumber(svc.ContextForJob("cancel-test"), ScrapeOptions{
			Number:   "ABP-001",
			Provider: "fake",
		})
		done <- err
	}()

	time.Sleep(20 * time.Millisecond)
	if !svc.CancelJob("cancel-test") {
		t.Fatal("expected active job to be cancelled")
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "canceled") {
			t.Fatalf("expected cancellation error, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("provider wait did not stop after job cancellation")
	}
	close(providerRelease)
	svc.FinishJob("cancel-test")
}

func TestEngineForProxyCachesConfiguredEngine(t *testing.T) {
	eng := &fakeMetadataEngine{}
	svc := newTestMetadataService(eng)
	var mu sync.Mutex
	created := 0
	svc.engineFactory = func(string) metadataEngine {
		mu.Lock()
		created++
		mu.Unlock()
		return &fakeMetadataEngine{}
	}

	first, _ := svc.engineForProxy("127.0.0.1:7897")
	second, _ := svc.engineForProxy("http://127.0.0.1:7897")
	if first != second {
		t.Fatal("expected the same cached engine for normalized proxy values")
	}
	if created != 1 {
		t.Fatalf("expected one engine creation, got %d", created)
	}
}

func TestMetadataCacheReturnsIndependentCopies(t *testing.T) {
	svc := newTestMetadataService(&fakeMetadataEngine{})
	key := svc.metadataCacheKey("ABP-001", "JavBus", "")
	svc.putCachedMetadata(key, ResolveMetadataResult{
		Info:   &MovieInfo{Number: "ABP-001", Genres: []string{"剧情"}},
		Source: "online",
	})

	first, ok := svc.getCachedMetadata(key)
	if !ok {
		t.Fatal("expected cached metadata")
	}
	first.Info.Genres[0] = "mutated"
	second, ok := svc.getCachedMetadata(key)
	if !ok || second.Info.Genres[0] != "剧情" {
		t.Fatal("cached metadata was mutated by a caller")
	}
}

func TestResolveMetadataCoalescesConcurrentSplitLookups(t *testing.T) {
	var searchCalls int32
	searchStarted := make(chan struct{}, 1)
	releaseSearch := make(chan struct{})
	eng := &fakeMetadataEngine{
		movieProviders: map[string]provider.MovieProvider{"Fake": nil},
		searchMovie: func(string, string, bool) ([]*metatubemodel.MovieSearchResult, error) {
			atomic.AddInt32(&searchCalls, 1)
			select {
			case searchStarted <- struct{}{}:
			default:
			}
			<-releaseSearch
			return []*metatubemodel.MovieSearchResult{{
				ID:       "fake-midd-820",
				Number:   "MIDD-820",
				Provider: "Fake",
				Title:    "Split release",
			}}, nil
		},
		movieInfo: func(providerid.ProviderID, bool) (*metatubemodel.MovieInfo, error) {
			return &metatubemodel.MovieInfo{
				ID:       "fake-midd-820",
				Number:   "MIDD-820",
				Provider: "Fake",
				Title:    "Split release",
			}, nil
		},
	}
	svc := newTestMetadataService(eng)

	results := make(chan ResolveMetadataResult, 2)
	resolve := func() {
		results <- svc.ResolveMetadata(context.Background(), ResolveMetadataOptions{
			Number:       "MIDD-820",
			Provider:     "Fake",
			PreferSource: "online",
			MaxAttempts:  1,
		})
	}
	go resolve()
	select {
	case <-searchStarted:
	case <-time.After(time.Second):
		t.Fatal("first metadata lookup did not start")
	}
	go resolve()
	close(releaseSearch)

	for i := 0; i < 2; i++ {
		select {
		case result := <-results:
			if result.Error != "" || result.Info == nil {
				t.Fatalf("split lookup failed: %+v", result)
			}
		case <-time.After(time.Second):
			t.Fatal("split metadata lookup did not finish")
		}
	}
	if got := atomic.LoadInt32(&searchCalls); got != 1 {
		t.Fatalf("expected one provider search for MIDD-820-A/B, got %d", got)
	}
}
