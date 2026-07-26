// Ownership summary:
//   This file implements domain logic for the javflow backend.
//
// File map for maintainers:
//   1) Domain-specific types and helpers.
//   2) Internal service implementation.
//

package librarymetadata

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/metatube-community/metatube-sdk-go/engine"
	"github.com/metatube-community/metatube-sdk-go/engine/providerid"
	metatubemodel "github.com/metatube-community/metatube-sdk-go/model"
	"github.com/metatube-community/metatube-sdk-go/provider"

	"javflow/internal/proxy"
)

const (
	metadataRequestTimeout = 12 * time.Second
	metadataSearchTimeout  = 20 * time.Second
	metadataCacheTTL       = 15 * time.Minute
	providerCooldown       = 2 * time.Minute
)

type metadataEngine interface {
	GetMovieProviders() map[string]provider.MovieProvider
	GetActorProviders() map[string]provider.ActorProvider
	SearchMovie(keyword, name string, fallback bool) ([]*metatubemodel.MovieSearchResult, error)
	GetMovieInfoByProviderID(providerid.ProviderID, bool) (*metatubemodel.MovieInfo, error)
	SearchActor(keyword, name string, fallback bool) ([]*metatubemodel.ActorSearchResult, error)
}

type metadataCacheEntry struct {
	result    ResolveMetadataResult
	expiresAt time.Time
}

type providerHealth struct {
	failures      int
	cooldownUntil time.Time
}

type actorImageCacheEntry struct {
	url       string
	expiresAt time.Time
}

func newDefaultMetadataEngine(proxyURL string) metadataEngine {
	eng := engine.Default()
	configureMetadataEngine(eng, proxyURL)
	return eng
}

func configureMetadataEngine(eng metadataEngine, proxyURL string) {
	if eng == nil {
		return
	}
	normalizedProxy := proxy.NormalizeProxyValue(proxyURL)
	for _, movieProvider := range eng.GetMovieProviders() {
		if setter, ok := movieProvider.(provider.RequestTimeoutSetter); ok {
			setter.SetRequestTimeout(metadataRequestTimeout)
		}
		if normalizedProxy != "" {
			if setter, ok := movieProvider.(provider.ProxySetter); ok {
				_ = setter.SetProxy(normalizedProxy)
			}
		}
	}
	for _, actorProvider := range eng.GetActorProviders() {
		if setter, ok := actorProvider.(provider.RequestTimeoutSetter); ok {
			setter.SetRequestTimeout(metadataRequestTimeout)
		}
		if normalizedProxy != "" {
			if setter, ok := actorProvider.(provider.ProxySetter); ok {
				_ = setter.SetProxy(normalizedProxy)
			}
		}
	}
}

func (s *Service) resolveProxy(requestedProxy string) string {
	requested := proxy.NormalizeProxyValue(requestedProxy)
	if requested != "" {
		return requested
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.proxy
}

func (s *Service) engineForProxy(requestedProxy string) (metadataEngine, string) {
	proxyURL := s.resolveProxy(requestedProxy)
	key := strings.ToLower(proxyURL)

	s.mu.RLock()
	eng := s.engines[key]
	s.mu.RUnlock()
	if eng != nil {
		return eng, proxyURL
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if eng = s.engines[key]; eng != nil {
		return eng, proxyURL
	}
	factory := s.engineFactory
	if factory == nil {
		factory = newDefaultMetadataEngine
	}
	eng = factory(proxyURL)
	s.engines[key] = eng
	return eng, proxyURL
}

func (s *Service) acquire(ctx context.Context) error {
	select {
	case s.semaphore <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) release() {
	select {
	case <-s.semaphore:
	default:
	}
}

// StartJob creates a cancellable context shared by resolve and write commands.
func (s *Service) StartJob(jobID string) error {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return fmt.Errorf("jobId 不能为空")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.jobsMu.Lock()
	if previous := s.jobs[jobID]; previous != nil {
		previous()
	}
	s.jobs[jobID] = cancel
	s.jobContexts[jobID] = ctx
	s.jobsMu.Unlock()
	return nil
}

func (s *Service) ContextForJob(jobID string) context.Context {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return context.Background()
	}
	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()
	if ctx := s.jobContexts[jobID]; ctx != nil {
		return ctx
	}
	return context.Background()
}

func (s *Service) CancelJob(jobID string) bool {
	jobID = strings.TrimSpace(jobID)
	s.jobsMu.Lock()
	cancel := s.jobs[jobID]
	s.jobsMu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

func (s *Service) FinishJob(jobID string) {
	jobID = strings.TrimSpace(jobID)
	s.jobsMu.Lock()
	cancel := s.jobs[jobID]
	delete(s.jobs, jobID)
	delete(s.jobContexts, jobID)
	s.jobsMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Service) metadataCacheKey(code, preferredProvider, proxyURL string) string {
	return strings.ToUpper(strings.TrimSpace(code)) + "|" +
		strings.ToLower(strings.TrimSpace(preferredProvider)) + "|" + strings.ToLower(strings.TrimSpace(proxyURL))
}

func (s *Service) getCachedMetadata(key string) (ResolveMetadataResult, bool) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	entry, ok := s.cache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		delete(s.cache, key)
		return ResolveMetadataResult{}, false
	}
	return cloneResolveMetadataResult(entry.result), true
}

func (s *Service) putCachedMetadata(key string, result ResolveMetadataResult) {
	if result.Info == nil || result.Error != "" {
		return
	}
	s.cacheMu.Lock()
	s.cache[key] = metadataCacheEntry{result: cloneResolveMetadataResult(result), expiresAt: time.Now().Add(metadataCacheTTL)}
	s.cacheMu.Unlock()
}

func cloneResolveMetadataResult(result ResolveMetadataResult) ResolveMetadataResult {
	clone := result
	clone.Info = cloneMovieInfo(result.Info)
	clone.Providers = append([]string(nil), result.Providers...)
	clone.FallbackInfos = append([]ImageInfo(nil), result.FallbackInfos...)
	return clone
}

func cloneMovieInfo(info *MovieInfo) *MovieInfo {
	if info == nil {
		return nil
	}
	clone := *info
	clone.Genres = append([]string(nil), info.Genres...)
	clone.Actors = append([]string(nil), info.Actors...)
	clone.ActorImages = append([]ActorImage(nil), info.ActorImages...)
	return &clone
}

func (s *Service) providerCoolingDown(name string) bool {
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	health := s.providerHealth[strings.ToLower(strings.TrimSpace(name))]
	return health != nil && time.Now().Before(health.cooldownUntil)
}

func (s *Service) recordProviderResult(name string, err error) {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return
	}
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	health := s.providerHealth[key]
	if health == nil {
		health = &providerHealth{}
		s.providerHealth[key] = health
	}
	if err == nil {
		health.failures = 0
		health.cooldownUntil = time.Time{}
		return
	}
	health.failures++
	if health.failures >= 2 {
		health.cooldownUntil = time.Now().Add(providerCooldown)
	}
}

func providerExists(providers map[string]provider.MovieProvider, name string) (string, bool) {
	for candidate := range providers {
		if strings.EqualFold(candidate, name) {
			return candidate, true
		}
	}
	return "", false
}

func (s *Service) prioritizedMovieProviders(eng metadataEngine, code, preferred string) []string {
	providers := eng.GetMovieProviders()
	upperCode := strings.ToUpper(strings.TrimSpace(code))
	priority := []string{"JavBus", "JAV321", "FANZA", "MGS", "SOD", "AVBASE"}
	switch {
	case strings.HasPrefix(upperCode, "FC2"):
		priority = []string{"FC2", "FC2PPVDB", "JavBus", "JAVFREE", "AVBASE"}
	case strings.HasPrefix(upperCode, "HEYZO"):
		priority = []string{"Heyzo", "JavBus", "JAVFREE", "AVBASE"}
	case strings.HasPrefix(upperCode, "KIN8"):
		priority = []string{"KIN8", "JavBus", "JAVFREE", "AVBASE"}
	case strings.HasPrefix(upperCode, "1PON"):
		priority = []string{"1Pondo", "JavBus", "JAVFREE", "AVBASE"}
	}
	if strings.TrimSpace(preferred) != "" {
		priority = append([]string{preferred}, priority...)
	}

	result := make([]string, 0, 4)
	seen := map[string]struct{}{}
	appendProvider := func(requested string, allowCooldown bool) {
		actual, ok := providerExists(providers, requested)
		key := strings.ToLower(actual)
		if !ok || len(result) >= 4 {
			return
		}
		if _, exists := seen[key]; exists {
			return
		}
		if !allowCooldown && s.providerCoolingDown(actual) {
			return
		}
		seen[key] = struct{}{}
		result = append(result, actual)
	}
	for _, name := range priority {
		appendProvider(name, false)
	}
	if len(result) == 0 {
		fallback := make([]string, 0, len(providers))
		for name := range providers {
			fallback = append(fallback, name)
		}
		sort.Strings(fallback)
		for _, name := range fallback {
			appendProvider(name, true)
		}
	}
	return result
}

func (s *Service) getCachedActorImage(name string) (string, bool) {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return "", false
	}
	s.actorCacheMu.Lock()
	defer s.actorCacheMu.Unlock()
	entry, ok := s.actorCache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		delete(s.actorCache, key)
		return "", false
	}
	return entry.url, true
}

func (s *Service) putCachedActorImage(name, imageURL string) {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return
	}
	ttl := 15 * time.Minute
	if strings.TrimSpace(imageURL) == "" {
		ttl = 5 * time.Minute
	}
	s.actorCacheMu.Lock()
	s.actorCache[key] = actorImageCacheEntry{url: strings.TrimSpace(imageURL), expiresAt: time.Now().Add(ttl)}
	s.actorCacheMu.Unlock()
}

func getMovieInfoWithContext(ctx context.Context, eng metadataEngine, id providerid.ProviderID) (*metatubemodel.MovieInfo, error) {
	type response struct {
		info *metatubemodel.MovieInfo
		err  error
	}
	resultCh := make(chan response, 1)
	go func() {
		info, err := eng.GetMovieInfoByProviderID(id, true)
		resultCh <- response{info: info, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-resultCh:
		return result.info, result.err
	}
}
