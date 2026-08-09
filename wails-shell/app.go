// main is the current Wails desktop composition root.
//
// Ownership summary:
// 1) assemble long-lived desktop services once at startup
// 2) wire observers, bridge facades, and runtime state together
// 3) keep domain behavior inside services instead of leaking it into bootstrap
package main

import (
	"context"
	"log"
	"os"
	"time"

	"javflow/internal/actresslookup"
	"javflow/internal/actressranking"
	"javflow/internal/adlearning"
	"javflow/internal/antiblock"
	"javflow/internal/avsubscription"
	"javflow/internal/avsubscriptionv2"
	"javflow/internal/bridge"
	"javflow/internal/crawlfetch"
	"javflow/internal/crawlquality"
	"javflow/internal/crawlresult"
	"javflow/internal/crawlreview"
	"javflow/internal/crawlruncontext"
	"javflow/internal/crawlrunner"
	"javflow/internal/crawlstage"
	"javflow/internal/crawltask"
	"javflow/internal/crawluistate"
	"javflow/internal/dependency"
	"javflow/internal/desktop"
	"javflow/internal/events"
	"javflow/internal/librarymetadata"
	"javflow/internal/organizer"
	"javflow/internal/proxy"
	runtimepaths "javflow/internal/runtime"
	"javflow/internal/runtimecache"
	"javflow/internal/settings"
	"javflow/internal/sidecar"
	"javflow/internal/subcrawl"
	"javflow/internal/subcrawlv2"
)

type App struct {
	ctx               context.Context
	paths             runtimepaths.Paths
	api               *bridge.API
	manager           *sidecar.Manager
	bus               *events.Bus
	crawlTask         *crawltask.Service
	crawlFetchService *crawlfetch.Service
}

func NewApp(repoRoot string) *App {
	paths, err := runtimepaths.BuildPaths(repoRoot)
	if err != nil {
		log.Fatalf("build runtime paths failed: %v", err)
	}

	bus := events.NewBus()
	var app *App
	dialogs := desktop.NewService(func() context.Context {
		if app == nil {
			return nil
		}
		return app.ctx
	})
	store := settings.NewStore(paths)
	crawlFetchService := createCrawlFetchService(store)
	avSubscriptionService := avsubscription.NewService(paths)
	avSubscriptionV2Service := avsubscriptionv2.NewService(paths, crawlFetchService)
	organizerService := organizer.NewService(paths)
	// Actor lookup reuses the already-configured crawl fetch service only when
	// a JAV site returns a verification page. The recovery implementation stays
	// owned by crawlfetch/crawlrequest.
	actressLookupService := actresslookup.NewServiceWithAliasCache(paths.UserData, crawlFetchService)
	actressRankingService := actressranking.NewService()
	subCrawlService := subcrawl.NewService(bus, paths, avSubscriptionService)
	adLearningService := adlearning.NewService(paths)
	antiBlockService := antiblock.NewService()
	crawlQualityService := crawlquality.NewService()
	crawlReviewService := crawlreview.NewService()
	crawlUIStateService := crawluistate.NewService()
	dependencyService := dependency.NewService(paths, bus)
	proxyService := proxy.NewService()
	libraryMetadataService := librarymetadata.NewService()
	libraryMetadataService.LogManager.SetBasePath(paths.AppPath)
	runtimeState := runtimecache.NewState()
	crawlRunnerService := createCrawlRunnerService(store, crawlFetchService, paths)
	subCrawlV2Service := subcrawlv2.NewService(bus, paths, avSubscriptionV2Service, crawlFetchService)
	crawlRunContextService := crawlruncontext.NewService(store, runtimeState)
	crawlStageService := crawlstage.NewService()
	crawlResultService := crawlresult.NewService(crawlQualityService, crawlRunContextService)
	registerCoreObservers(
		bus,
		runtimeState,
		crawlQualityService,
		crawlStageService,
		crawlResultService,
		crawlReviewService,
		crawlRunContextService,
		crawlUIStateService,
	)

	manager := sidecar.NewManager(repoRoot, paths, bus)
	crawlTaskService := crawltask.NewService(manager, runtimeState, store, bus)
	registerTaskObserver(bus, crawlTaskService)
	api := bridge.NewAPI(buildBridgeDependencies(
		store,
		paths,
		avSubscriptionService,
		dialogs,
		manager,
		bus,
		organizerService,
		actressLookupService,
		actressRankingService,
		adLearningService,
		antiBlockService,
		crawlQualityService,
		crawlRunContextService,
		crawlStageService,
		crawlResultService,
		crawlReviewService,
		dependencyService,
		proxyService,
		runtimeState,
		crawlTaskService,
		crawlFetchService,
		crawlRunnerService,
		subCrawlService,
		avSubscriptionV2Service,
		subCrawlV2Service,
		libraryMetadataService,
	))

	app = &App{
		paths:             paths,
		api:               api,
		manager:           manager,
		bus:               bus,
		crawlTask:         crawlTaskService,
		crawlFetchService: crawlFetchService,
	}

	return app
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.bus.SetContext(ctx)
	a.api.SetWailsContext(ctx)
}

func (a *App) shutdown(ctx context.Context) {
	a.api.ShutdownRunner()

	if a.crawlFetchService != nil {
		_ = a.crawlFetchService.Close()
	}

	if a.manager != nil {
		_ = a.manager.Shutdown(ctx)
	}
}

func (a *App) Call(command string, payload map[string]any) (string, error) {
	return a.api.Call(command, payload)
}

func createCrawlFetchService(store *settings.Store) *crawlfetch.Service {
	settingsMap := loadSettingsMap(store)

	antiBlockURLs := []string(nil)
	if homeDir, err := os.UserHomeDir(); err == nil {
		antiBlockURLs, _ = antiblock.LoadURLs(homeDir)
	}

	svc, err := crawlfetch.NewService(crawlfetch.ServiceOptions{
		Proxy:         stringSetting(settingsMap, "proxy"),
		Timeout:       timeoutSetting(settingsMap, "timeout", 30*time.Second),
		ConfigCookie:  stringSetting(settingsMap, "cookie"),
		AntiBlockURLs: antiBlockURLs,
	})
	if err != nil {
		log.Fatalf("crawlFetch service init failed: %v", err)
	}
	return svc
}

func createCrawlRunnerService(store *settings.Store, fetchService *crawlfetch.Service, paths runtimepaths.Paths) *crawlrunner.Runner {
	settingsMap := loadSettingsMap(store)
	outputDir := stringSetting(settingsMap, "output")
	if outputDir == "" {
		outputDir = "."
	}

	baseURL := stringSetting(settingsMap, "baseUrl")
	if baseURL == "" {
		baseURL = "https://www.javbus.com/"
	}

	runner, err := crawlrunner.NewRunner(crawlrunner.Config{
		BaseURL:      baseURL,
		Output:       outputDir,
		UserDataDir:  paths.UserData,
		Parallel:     2,
		ItemsPerPage: 30,
		Delay:        2,
	}, outputDir)
	if err != nil {
		log.Fatalf("crawlRunner init failed: %v", err)
	}

	crawlrunner.BindFetchService(runner, fetchService)
	return runner
}

func loadSettingsMap(store *settings.Store) map[string]any {
	if store == nil {
		return map[string]any{}
	}
	settingsMap, err := store.Load()
	if err != nil {
		return map[string]any{}
	}
	return settingsMap
}

func stringSetting(settingsMap map[string]any, key string) string {
	if settingsMap == nil {
		return ""
	}
	if value, ok := settingsMap[key].(string); ok {
		return value
	}
	return ""
}

func timeoutSetting(settingsMap map[string]any, key string, fallback time.Duration) time.Duration {
	if settingsMap == nil {
		return fallback
	}
	if value, ok := settingsMap[key].(float64); ok && value > 0 {
		return time.Duration(value) * time.Millisecond
	}
	return fallback
}

// registerCoreObservers keeps the read-model/runtime observer wiring in one
// place so NewApp reads as "compose services, then wire observers" instead of a
// long interleaved bootstrap script.
func registerCoreObservers(
	bus *events.Bus,
	runtimeState *runtimecache.State,
	crawlQualityService *crawlquality.Service,
	crawlStageService *crawlstage.Service,
	crawlResultService *crawlresult.Service,
	crawlReviewService *crawlreview.Service,
	crawlRunContextService *crawlruncontext.Service,
	crawlUIStateService *crawluistate.Service,
) {
	bus.AddObserver(runtimeState.ObserveEvent)
	if autoReporter := crawlquality.NewAutoReporter(crawlQualityService, runtimeState, bus); autoReporter != nil {
		bus.AddObserver(autoReporter.ObserveEvent)
	}
	if stageObserver := crawlstage.NewObserver(crawlStageService, bus); stageObserver != nil {
		bus.AddObserver(stageObserver.ObserveEvent)
	}
	if resultObserver := crawlresult.NewObserver(crawlResultService, bus); resultObserver != nil {
		bus.AddObserver(resultObserver.ObserveEvent)
	}
	if reviewObserver := crawlreview.NewObserver(crawlReviewService, bus); reviewObserver != nil {
		bus.AddObserver(reviewObserver.ObserveEvent)
	}
	if runContextObserver := crawlruncontext.NewObserver(crawlRunContextService, bus); runContextObserver != nil {
		bus.AddObserver(runContextObserver.ObserveEvent)
	}
	if uiStateObserver := crawluistate.NewObserver(crawlUIStateService, bus); uiStateObserver != nil {
		bus.AddObserver(uiStateObserver.ObserveEvent)
	}
}

func registerTaskObserver(bus *events.Bus, crawlTaskService *crawltask.Service) {
	if crawlTaskService != nil {
		bus.AddObserver(crawlTaskService.ObserveEvent)
	}
}

// buildBridgeDependencies keeps the Wails bootstrap root on one explicit
// bridge-dependency handoff. This makes the composition boundary easier to scan
// when one domain service needs to be added or removed later.
func buildBridgeDependencies(
	store *settings.Store,
	paths runtimepaths.Paths,
	avSubscriptions *avsubscription.Service,
	dialogs *desktop.Service,
	manager *sidecar.Manager,
	bus *events.Bus,
	organizerService *organizer.Service,
	actressLookupService *actresslookup.Service,
	actressRankingService *actressranking.Service,
	adLearningService *adlearning.Service,
	antiBlockService *antiblock.Service,
	crawlQualityService *crawlquality.Service,
	crawlRunContextService *crawlruncontext.Service,
	crawlStageService *crawlstage.Service,
	crawlResultService *crawlresult.Service,
	crawlReviewService *crawlreview.Service,
	dependencyService *dependency.Service,
	proxyService *proxy.Service,
	runtimeState *runtimecache.State,
	crawlTaskService *crawltask.Service,
	crawlFetchService *crawlfetch.Service,
	crawlRunnerService *crawlrunner.Runner,
	subCrawlService *subcrawl.Service,
	avSubscriptionsV2 *avsubscriptionv2.Service,
	subCrawlV2Service *subcrawlv2.Service,
	libraryMetadataService *librarymetadata.Service,
) bridge.Dependencies {
	return bridge.Dependencies{
		Store:             store,
		AVSubscriptions:   avSubscriptions,
		Dialogs:           dialogs,
		Manager:           manager,
		Bus:               bus,
		OrganizerService:  organizerService,
		ActressLookup:     actressLookupService,
		ActressRanking:    actressRankingService,
		AdLearningService: adLearningService,
		AntiBlock:         antiBlockService,
		CrawlQuality:      crawlQualityService,
		CrawlRunContext:   crawlRunContextService,
		CrawlStage:        crawlStageService,
		CrawlResult:       crawlResultService,
		CrawlReview:       crawlReviewService,
		DependencyService: dependencyService,
		ProxyService:      proxyService,
		RuntimeState:      runtimeState,
		Paths:             paths,
		CrawlTask:         crawlTaskService,
		CrawlFetch:        crawlFetchService,
		CrawlRunner:       crawlRunnerService,
		SubCrawl:          subCrawlService,
		AVSubscriptionsV2: avSubscriptionsV2,
		SubCrawlV2:        subCrawlV2Service,
		LibraryMetadata:   libraryMetadataService,
	}
}
