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
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/metatube-community/metatube-sdk-go/engine"
	"github.com/metatube-community/metatube-sdk-go/engine/providerid"
	metatubemodel "github.com/metatube-community/metatube-sdk-go/model"
	"github.com/metatube-community/metatube-sdk-go/provider"

	"javflow/internal/contracts/crawlartifact"
	"javflow/internal/proxy"
)

// Service provides movie metadata scraping via the embedded metatube-sdk-go.
type Service struct {
	engine         metadataEngine
	engineFactory  func(string) metadataEngine
	engines        map[string]metadataEngine
	mu             sync.RWMutex
	proxy          string
	semaphore      chan struct{}
	cacheMu        sync.Mutex
	cache          map[string]metadataCacheEntry
	healthMu       sync.Mutex
	providerHealth map[string]*providerHealth
	actorCacheMu   sync.Mutex
	actorCache     map[string]actorImageCacheEntry
	actorSemaphore chan struct{}
	jobsMu         sync.Mutex
	jobs           map[string]context.CancelFunc
	jobContexts    map[string]context.Context
	LogManager     *LogManager
}

// NewService creates a new metadata scraping service with the default engine.
func NewService() *Service {
	defaultEngine := newDefaultMetadataEngine("")
	return &Service{
		engine:         defaultEngine,
		engineFactory:  newDefaultMetadataEngine,
		engines:        map[string]metadataEngine{"": defaultEngine},
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

// SetProxy updates the proxy used by all movie providers. Empty string clears the proxy.
func (s *Service) SetProxy(proxyURL string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.proxy = proxy.NormalizeProxyValue(proxyURL)
}

// ScrapeByNumber scrapes metadata for a single JAV code.
// If provider is empty, it searches across all available providers and picks the best result.
func (s *Service) ScrapeByNumber(ctx context.Context, options ScrapeOptions) (ScrapeResult, error) {
	code := strings.TrimSpace(options.Number)
	if code == "" {
		return ScrapeResult{Error: "番号不能为空"}, fmt.Errorf("number is required")
	}

	eng, _ := s.engineForProxy(options.Proxy)
	if err := s.acquire(ctx); err != nil {
		return ScrapeResult{Error: err.Error()}, err
	}
	defer s.release()

	provider := strings.TrimSpace(options.Provider)
	var result *metatubemodel.MovieSearchResult
	var err error

	if provider != "" {
		result, err = s.searchSingleProvider(ctx, eng, code, provider)
	} else {
		result, err = s.searchBestResult(ctx, eng, code)
	}
	if err != nil {
		return ScrapeResult{Error: err.Error()}, err
	}
	if result == nil {
		return ScrapeResult{Error: "未找到匹配的影片信息"}, fmt.Errorf("no result found for %s", code)
	}

	info, err := getMovieInfoWithContext(ctx, eng, providerid.ProviderID{
		Provider: result.Provider,
		ID:       result.ID,
	})
	if err != nil {
		return ScrapeResult{Error: err.Error()}, err
	}

	return ScrapeResult{
		Info:      s.mapMovieInfo(ctx, eng, info),
		Providers: s.providerNames(),
	}, nil
}

// SearchProviders returns the list of available online providers.
func (s *Service) SearchProviders() []string {
	return s.providerNames()
}

// ResolveMetadataOptions controls the source priority when resolving metadata.
type ResolveMetadataOptions struct {
	Number         string
	Provider       string
	Proxy          string
	CrawlOutputDir string
	UserDataDir    string
	// PreferSource can be "local", "online", or "auto".
	// "auto" tries local crawler artifacts first, then falls back to online.
	PreferSource string
	// MaxAttempts limits how many online providers are tried when the primary
	// provider fails. Zero or one means only the primary provider is used.
	MaxAttempts int
}

// ResolveMetadataResult reports which source provided the metadata.
type ResolveMetadataResult struct {
	Info          *MovieInfo  `json:"info,omitempty"`
	Source        string      `json:"source"`
	Providers     []string    `json:"providers,omitempty"`
	FallbackInfos []ImageInfo `json:"fallbackInfos,omitempty"`
	Error         string      `json:"error,omitempty"`
}

// ResolveMetadata looks up metadata for a code using the configured source
// priority. It is the single entry point used by "补齐当前" and the NFO-only
// actions so the controller does not need to know about every source.
func (s *Service) ResolveMetadata(ctx context.Context, options ResolveMetadataOptions) ResolveMetadataResult {
	if ctx == nil {
		ctx = context.Background()
	}
	code := strings.ToUpper(strings.TrimSpace(options.Number))
	if code == "" {
		return ResolveMetadataResult{Error: "番号不能为空"}
	}

	prefer := strings.ToLower(strings.TrimSpace(options.PreferSource))
	if prefer != "local" && prefer != "online" {
		prefer = "auto"
	}

	if prefer != "online" && (options.CrawlOutputDir != "" || options.UserDataDir != "") {
		source, err := BuildCrawlSource(options.CrawlOutputDir, options.UserDataDir)
		if err == nil {
			if record, ok := source.Lookup(code); ok {
				info := BuildMovieInfoFromCrawlerRecord(record)
				if info != nil {
					return ResolveMetadataResult{
						Info:   info,
						Source: "local-crawler",
					}
				}
			}
		} else if prefer == "local" {
			return ResolveMetadataResult{
				Error:  fmt.Sprintf("本地元数据加载失败：%s", err.Error()),
				Source: "local-crawler",
			}
		}
	}

	if prefer == "local" {
		return ResolveMetadataResult{
			Error:  fmt.Sprintf("本地未找到 %s 的爬虫元数据", code),
			Source: "local-crawler",
		}
	}

	proxyURL := s.resolveProxy(options.Proxy)
	cacheKey := s.metadataCacheKey(code, options.Provider, proxyURL)
	if cached, ok := s.getCachedMetadata(cacheKey); ok {
		return cached
	}

	info, fallbackInfos, attemptedProviders, err := s.scrapeWithFallback(ctx, code, options.Provider, options.Proxy, options.MaxAttempts)
	if err != nil || info == nil {
		errMsg := "未找到影片信息"
		if err != nil {
			errMsg = err.Error()
		}
		return ResolveMetadataResult{Error: errMsg, Source: "online"}
	}

	result := ResolveMetadataResult{
		Info:          info,
		Source:        "online",
		Providers:     attemptedProviders,
		FallbackInfos: fallbackInfos,
	}
	s.putCachedMetadata(cacheKey, result)
	return result
}

// ListCrawlSources returns the historical crawl snapshots available in the
// user data directory. This powers the "隐藏抓取产物" selector.
func (s *Service) ListCrawlSources(userDataDir string, roots []string) []crawlartifact.CacheSnapshot {
	items, _ := crawlartifact.DiscoverCacheSnapshots(userDataDir, roots)
	return items
}

// CountCrawlArtifacts returns the number of distinct film codes in the given
// crawl output directory. It is used for directory-picker feedback in the
// media-library scraping workspace.
func (s *Service) CountCrawlArtifacts(outputDir string) int {
	return CountCrawlArtifacts(outputDir)
}

// WriteMetadata writes NFO and image files for a library item, applying the
// service-level proxy if no explicit proxy is provided.
func (s *Service) WriteMetadata(options WriteOptions) WriteResult {
	return s.WriteMetadataContext(context.Background(), options)
}

// WriteMetadataContext writes metadata with the caller's cancellable job
// context. The legacy WriteMetadata method remains for tests and compatibility.
func (s *Service) WriteMetadataContext(ctx context.Context, options WriteOptions) WriteResult {
	proxyURL := s.resolveProxy(options.Proxy)
	options.Proxy = proxyURL
	if options.ImageFetcher == nil && options.Info != nil && options.Info.Provider != "" {
		options.ImageFetcher = s.newImageFetcher(ctx, proxyURL)
	}
	return WriteMetadataContext(ctx, options)
}

// newImageFetcher returns a fetcher that delegates image downloads to the
// embedded metatube-sdk-go engine. This lets providers inject their own
// headers/cookies/referers so that sources like FANZA or SOD can be fetched
// reliably, matching the behavior of MetaTube's /v1/images/backdrop endpoint.
func (s *Service) newImageFetcher(ctx context.Context, proxyURL string) func(url, providerName string) ([]byte, error) {
	return func(imageURL, providerName string) ([]byte, error) {
		return s.fetchImageViaEngine(ctx, imageURL, providerName, proxyURL)
	}
}

func (s *Service) fetchImageViaEngine(ctx context.Context, imageURL, providerName, proxyURL string) ([]byte, error) {
	if strings.TrimSpace(imageURL) == "" || strings.TrimSpace(providerName) == "" {
		return nil, fmt.Errorf("image URL or provider name is empty")
	}
	eng, _ := s.engineForProxy(proxyURL)
	e, ok := eng.(*engine.Engine)
	if !ok {
		return nil, fmt.Errorf("engine does not support provider-aware image fetch")
	}
	p, err := e.GetMovieProviderByName(providerName)
	if err != nil {
		return nil, err
	}
	type fetchResponse struct {
		data []byte
		err  error
	}
	ch := make(chan fetchResponse, 1)
	go func() {
		resp, err := e.Fetch(imageURL, p)
		if err != nil {
			ch <- fetchResponse{err: err}
			return
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
		if err != nil {
			ch <- fetchResponse{err: err}
			return
		}
		if int64(len(data)) > maxImageBytes {
			ch <- fetchResponse{err: fmt.Errorf("图片超过 %d MB", maxImageBytes/(1024*1024))}
			return
		}
		ch <- fetchResponse{data: data}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		return res.data, res.err
	}
}

func (s *Service) providerNames() []string {
	providers := s.engine.GetMovieProviders()
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (s *Service) searchSingleProvider(ctx context.Context, eng metadataEngine, code, providerName string) (*metatubemodel.MovieSearchResult, error) {
	ctx, cancel := context.WithTimeout(ctx, metadataSearchTimeout)
	defer cancel()

	type searchResult struct {
		results []*metatubemodel.MovieSearchResult
		err     error
	}
	ch := make(chan searchResult, 1)
	go func() {
		results, err := eng.SearchMovie(code, providerName, true)
		ch <- searchResult{results, err}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("provider %s 搜索 %s 超时: %w", providerName, code, ctx.Err())
	case res := <-ch:
		if res.err != nil {
			return nil, res.err
		}
		if len(res.results) == 0 {
			return nil, fmt.Errorf("provider %s returned no results for %s", providerName, code)
		}
		return res.results[0], nil
	}
}

func (s *Service) searchBestResult(ctx context.Context, eng metadataEngine, code string) (*metatubemodel.MovieSearchResult, error) {
	candidates, err := s.searchCandidateResults(ctx, eng, code, "")
	if err != nil || len(candidates) == 0 {
		return nil, err
	}
	return candidates[0], nil
}

// searchCandidateResults collects exact-match search results from all movie
// providers. It returns the full list so callers can retry with the next
// provider when the primary provider's images fail to download.
func (s *Service) searchCandidateResults(ctx context.Context, eng metadataEngine, code, preferredProvider string) ([]*metatubemodel.MovieSearchResult, error) {
	providers := s.prioritizedMovieProviders(eng, code, preferredProvider)
	if len(providers) == 0 {
		return nil, nil
	}

	type searchResult struct {
		result *metatubemodel.MovieSearchResult
		err    error
	}

	searchCtx, cancel := context.WithTimeout(ctx, metadataSearchTimeout)
	defer cancel()

	resultCh := make(chan searchResult, len(providers))
	var wg sync.WaitGroup
	for _, name := range providers {
		wg.Add(1)
		go func(providerName string) {
			defer wg.Done()
			select {
			case <-searchCtx.Done():
				return
			default:
			}

			results, err := eng.SearchMovie(code, providerName, true)
			if err != nil {
				s.recordProviderResult(providerName, err)
				return
			}
			s.recordProviderResult(providerName, nil)
			for _, r := range results {
				if strings.EqualFold(strings.TrimSpace(r.Number), code) {
					select {
					case resultCh <- searchResult{result: r}:
					case <-searchCtx.Done():
					}
					return
				}
			}
		}(name)
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	var candidates []*metatubemodel.MovieSearchResult
	seen := make(map[string]struct{})
	for res := range resultCh {
		if res.result == nil {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(res.result.Provider)) + "|" + strings.TrimSpace(res.result.ID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		candidates = append(candidates, res.result)
	}

	// Search completion order is nondeterministic; restore provider priority before
	// attempting metadata reads so retry behavior is reproducible.
	priority := make(map[string]int, len(providers))
	for index, name := range providers {
		priority[strings.ToLower(name)] = index
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return priority[strings.ToLower(candidates[i].Provider)] < priority[strings.ToLower(candidates[j].Provider)]
	})
	return candidates, nil
}

// scrapeWithFallback tries several providers in order and returns the first
// successfully scraped MovieInfo plus image URLs from the remaining candidates.
// The preferred provider is moved to the front when it is not empty.
func (s *Service) scrapeWithFallback(ctx context.Context, code, preferredProvider, proxy string, maxAttempts int) (*MovieInfo, []ImageInfo, []string, error) {
	eng, _ := s.engineForProxy(proxy)
	if err := s.acquire(ctx); err != nil {
		return nil, nil, nil, err
	}
	defer s.release()

	candidates, err := s.searchCandidateResults(ctx, eng, code, preferredProvider)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(candidates) == 0 {
		return nil, nil, nil, fmt.Errorf("no results found for %s", code)
	}

	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	if maxAttempts > len(candidates) {
		maxAttempts = len(candidates)
	}

	var primary *MovieInfo
	var fallback []ImageInfo
	var lastErr error
	attempted := make([]string, 0, maxAttempts)

	for i := 0; i < maxAttempts; i++ {
		r := candidates[i]
		providerName := strings.TrimSpace(r.Provider)
		attempted = append(attempted, providerName)

		info, scrapeErr := getMovieInfoWithContext(ctx, eng, providerid.ProviderID{
			Provider: r.Provider,
			ID:       r.ID,
		})
		if scrapeErr != nil {
			lastErr = scrapeErr
			s.recordProviderResult(providerName, scrapeErr)
			continue
		}
		if info == nil {
			continue
		}

		s.recordProviderResult(providerName, nil)
		mapped := s.mapMovieInfo(ctx, eng, info)
		if mapped == nil {
			continue
		}

		if primary == nil {
			primary = mapped
		} else {
			fallback = append(fallback, ImageInfo{
				Provider:    mapped.Provider,
				CoverURL:    mapped.CoverURL,
				BackdropURL: mapped.BackdropURL,
				ThumbURL:    mapped.ThumbURL,
				BigThumbURL: mapped.BigThumbURL,
			})
		}
	}

	if primary == nil {
		return nil, nil, attempted, lastErr
	}

	return primary, fallback, attempted, nil
}

func (s *Service) mapMovieInfo(ctx context.Context, eng metadataEngine, info *metatubemodel.MovieInfo) *MovieInfo {
	if info == nil {
		return nil
	}

	// 某些 provider 会把多个演员用顿号/逗号/半角逗号合并成一个字符串，
	// 这里按常见分隔符拆分，保证每个演员生成独立的 <actor> 节点。
	actors := make([]string, 0, len(info.Actors))
	for _, name := range info.Actors {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		parts := splitActorNames(name)
		for _, part := range parts {
			if part = strings.TrimSpace(part); part != "" {
				actors = append(actors, part)
			}
		}
	}

	// Title 前面加上番号，方便 Jellyfin/Emby 列表直接识别。
	title := strings.TrimSpace(info.Title)
	number := strings.TrimSpace(info.Number)
	if number != "" && !strings.Contains(title, number) {
		title = number + " " + title
	}

	genres := make([]string, 0, len(info.Genres))
	for _, g := range info.Genres {
		if tag := strings.TrimSpace(g); tag != "" {
			genres = append(genres, tag)
		}
	}

	releaseDate := ""
	if date := time.Time(info.ReleaseDate); !date.IsZero() {
		releaseDate = date.Format("2006-01-02")
	}

	// MetaTube 的背景图（/v1/images/backdrop）使用 BigCoverURL -> CoverURL 作为源，
	// 裁剪比例为 0（不裁剪）。为了在主封面不可用时仍能下载到背景，这里扩展回退链：
	// BigCoverURL -> CoverURL -> BigThumbURL -> ThumbURL -> PreviewImages[0]。
	backdropURL := info.BigCoverURL
	if backdropURL == "" {
		backdropURL = info.CoverURL
	}
	if backdropURL == "" {
		backdropURL = info.BigThumbURL
	}
	if backdropURL == "" {
		backdropURL = info.ThumbURL
	}
	if backdropURL == "" && len(info.PreviewImages) > 0 {
		for _, img := range info.PreviewImages {
			if img = strings.TrimSpace(img); img != "" {
				backdropURL = img
				break
			}
		}
	}

	// 为每个演员搜索封面图，供后续 NFO 引用与本地保存。
	actorImages := s.resolveActorImages(ctx, eng, actors)

	return &MovieInfo{
		Number:      info.Number,
		Title:       title,
		Plot:        info.Summary,
		Outline:     info.Summary,
		ReleaseDate: releaseDate,
		Runtime:     info.Runtime,
		Score:       info.Score,
		Director:    info.Director,
		Studio:      info.Maker,
		Label:       info.Label,
		Series:      info.Series,
		Genres:      genres,
		Actors:      actors,
		ActorImages: actorImages,
		CoverURL:    info.CoverURL,
		BackdropURL: backdropURL,
		ThumbURL:    info.ThumbURL,
		BigThumbURL: info.BigThumbURL,
		Provider:    info.Provider,
		Homepage:    info.Homepage,
	}
}

// splitActorNames 把某些 provider 合并在一起的演员名字符串拆成单个演员。
func splitActorNames(name string) []string {
	for _, sep := range []string{"、", "，", ","} {
		if strings.Contains(name, sep) {
			parts := strings.Split(name, sep)
			result := make([]string, 0, len(parts))
			for _, p := range parts {
				if p = strings.TrimSpace(p); p != "" {
					result = append(result, p)
				}
			}
			return result
		}
	}
	return []string{name}
}

// resolveActorImages 并发地为每个演员搜索封面图，避免串行搜索拖慢整体刮削并发度。
func (s *Service) resolveActorImages(ctx context.Context, eng metadataEngine, actors []string) []ActorImage {
	if eng == nil || len(actors) == 0 {
		return nil
	}

	providers := eng.GetActorProviders()
	providerNames := make([]string, 0, len(providers))
	for _, preferred := range []string{"Gfriends", "ThePornDB"} {
		if actual, ok := providerExistsActor(providers, preferred); ok && !s.providerCoolingDown(actual) {
			providerNames = append(providerNames, actual)
		}
	}
	if len(providerNames) == 0 {
		for name := range providers {
			if !s.providerCoolingDown(name) {
				providerNames = append(providerNames, name)
			}
		}
		sort.Strings(providerNames)
	}
	if len(providerNames) > 2 {
		providerNames = providerNames[:2]
	}
	if len(providerNames) == 0 {
		return nil
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	actorSemaphore := s.actorSemaphore
	if actorSemaphore == nil {
		actorSemaphore = make(chan struct{}, 3)
	}
	actorImages := make([]ActorImage, 0, len(actors))

	for _, actorName := range actors {
		name := strings.TrimSpace(actorName)
		if name == "" {
			continue
		}
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			select {
			case actorSemaphore <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-actorSemaphore }()
			if imgURL := s.resolveActorImageURL(ctx, eng, providerNames, n); imgURL != "" {
				mu.Lock()
				actorImages = append(actorImages, ActorImage{Name: n, URL: imgURL})
				mu.Unlock()
			}
		}(name)
	}
	wg.Wait()
	return actorImages
}

// resolveActorImageURL 通过 metatube-sdk-go 搜索演员并返回第一张可用的图片 URL。
func (s *Service) resolveActorImageURL(ctx context.Context, eng metadataEngine, providerNames []string, name string) string {
	if eng == nil || strings.TrimSpace(name) == "" {
		return ""
	}
	if cachedURL, ok := s.getCachedActorImage(name); ok {
		return cachedURL
	}
	searchCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	for _, providerName := range providerNames {
		type actorResponse struct {
			results []*metatubemodel.ActorSearchResult
			err     error
		}
		ch := make(chan actorResponse, 1)
		go func(name string) {
			results, err := eng.SearchActor(name, providerName, true)
			ch <- actorResponse{results: results, err: err}
		}(name)
		select {
		case <-searchCtx.Done():
			return ""
		case response := <-ch:
			if response.err != nil {
				s.recordProviderResult(providerName, response.err)
				continue
			}
			s.recordProviderResult(providerName, nil)
			for _, result := range response.results {
				if result == nil || !result.IsValid() {
					continue
				}
				for _, img := range result.Images {
					if img = strings.TrimSpace(img); img != "" {
						s.putCachedActorImage(name, img)
						return img
					}
				}
			}
		}
	}
	s.putCachedActorImage(name, "")
	return ""
}

func providerExistsActor(providers map[string]provider.ActorProvider, name string) (string, bool) {
	for candidate := range providers {
		if strings.EqualFold(candidate, name) {
			return candidate, true
		}
	}
	return "", false
}
