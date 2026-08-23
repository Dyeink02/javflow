package bridge

// This file declares the bridge-owned facade groupings that keep the Wails
// command layer aligned with the product's three major domains plus runtime
// support. When a bug crosses modules, start here to see which service cluster
// the bridge thinks owns that workflow.
//
// Ownership summary:
// 1) group bridge dependencies by runtime, crawl, organizer, and lookup concerns
// 2) keep the API constructor/service bag readable at the domain level
// 3) make cross-module ownership visible before command handlers run
//
// File map for maintainers:
// 1) per-domain facade dependency group structs
// 2) API root dependency bag and service references
// 3) bridge constructor-facing ownership layout

import (
	"context"
	"sync"

	"javflow/internal/actresslookup"
	"javflow/internal/actressranking"
	"javflow/internal/adlearning"
	"javflow/internal/antiblock"
	"javflow/internal/appupdate"
	"javflow/internal/avsubscription"
	"javflow/internal/avsubscriptionv2"
	"javflow/internal/crawlfetch"
	"javflow/internal/crawlquality"
	"javflow/internal/crawlresult"
	"javflow/internal/crawlreview"
	"javflow/internal/crawlruncontext"
	"javflow/internal/crawlrunner"
	"javflow/internal/crawlstage"
	"javflow/internal/crawltask"
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

// runtimeFacade keeps read-mostly bootstrap/runtime dependencies together.
// These services mostly answer UI hydration requests instead of executing
// domain workflows.
type runtimeFacade struct {
	store        *settings.Store
	dialogs      *desktop.Service
	manager      *sidecar.Manager
	bus          *events.Bus
	dependency   *dependency.Service
	proxyService *proxy.Service
	runtimeState *runtimecache.State
	paths        runtimepaths.Paths
	appUpdate    *appupdate.Service
}

// lookupFacade groups actress lookup/ranking and subscription state together.
// These operations share target-shaping logic and are usually debugged as one
// functional area.
type lookupFacade struct {
	avSubscriptions   *avsubscription.Service
	avSubscriptionsV2 *avsubscriptionv2.Service
	actressLookup     *actresslookup.Service
	actressRanking    *actressranking.Service
	antiBlock         *antiblock.Service
	subCrawl          *subcrawl.Service
	subCrawlV2        *subcrawlv2.Service
}

// organizerFacade keeps organizer execution and ad-risk learning together so
// organizer-related bugs can be isolated without scanning crawl services.
type organizerFacade struct {
	organizerService *organizer.Service
	adLearning       *adlearning.Service
}

// libraryMetadataFacade isolates the media-library metadata scraper so it can
// be moved or replaced independently of crawl/organizer/subscription domains.
type libraryMetadataFacade struct {
	library *librarymetadata.Service
}

// crawlFacade contains crawl execution, runtime panels, and task-controller
// services. This keeps the bridge aligned with the actual crawl domain.
type crawlFacade struct {
	crawlQuality    *crawlquality.Service
	crawlRunContext *crawlruncontext.Service
	crawlStage      *crawlstage.Service
	crawlResult     *crawlresult.Service
	crawlReview     *crawlreview.Service
	crawlTask       *crawltask.Service
	crawlFetch      *crawlfetch.Service
	crawlRunner     *crawlrunner.Runner
}

// Dependencies is the bridge bootstrap-only assembly bag.
//
// Keep raw startup wiring here so:
// 1) NewAPI can stay focused on constructor intent
// 2) facade grouping can be reviewed without scanning a long literal
// 3) runtime bootstrap details do not leak into ordinary command handlers
//
// This type is exported only so the Wails app bootstrap can pass one coherent
// dependency object instead of another long positional constructor call. It is
// still an internal bootstrap contract, not a product-domain API.
type Dependencies struct {
	Store             *settings.Store
	AppUpdate         *appupdate.Service
	AVSubscriptions   *avsubscription.Service
	AVSubscriptionsV2 *avsubscriptionv2.Service
	Dialogs           *desktop.Service
	Manager           *sidecar.Manager
	Bus               *events.Bus
	OrganizerService  *organizer.Service
	ActressLookup     *actresslookup.Service
	ActressRanking    *actressranking.Service
	AdLearningService *adlearning.Service
	AntiBlock         *antiblock.Service
	CrawlQuality      *crawlquality.Service
	CrawlRunContext   *crawlruncontext.Service
	CrawlStage        *crawlstage.Service
	CrawlResult       *crawlresult.Service
	CrawlReview       *crawlreview.Service
	DependencyService *dependency.Service
	ProxyService      *proxy.Service
	RuntimeState      *runtimecache.State
	Paths             runtimepaths.Paths
	CrawlTask         *crawltask.Service
	CrawlFetch        *crawlfetch.Service
	CrawlRunner       *crawlrunner.Runner
	SubCrawl          *subcrawl.Service
	SubCrawlV2        *subcrawlv2.Service
	LibraryMetadata   *librarymetadata.Service
}

func (deps Dependencies) runtimeFacade() runtimeFacade {
	return runtimeFacade{
		store:        deps.Store,
		dialogs:      deps.Dialogs,
		manager:      deps.Manager,
		bus:          deps.Bus,
		dependency:   deps.DependencyService,
		proxyService: deps.ProxyService,
		runtimeState: deps.RuntimeState,
		paths:        deps.Paths,
		appUpdate:    deps.AppUpdate,
	}
}

func (deps Dependencies) lookupFacade() lookupFacade {
	return lookupFacade{
		avSubscriptions:   deps.AVSubscriptions,
		avSubscriptionsV2: deps.AVSubscriptionsV2,
		actressLookup:     deps.ActressLookup,
		actressRanking:    deps.ActressRanking,
		antiBlock:         deps.AntiBlock,
		subCrawl:          deps.SubCrawl,
		subCrawlV2:        deps.SubCrawlV2,
	}
}

func (deps Dependencies) organizerFacade() organizerFacade {
	return organizerFacade{
		organizerService: deps.OrganizerService,
		adLearning:       deps.AdLearningService,
	}
}

func (deps Dependencies) libraryMetadataFacade() libraryMetadataFacade {
	return libraryMetadataFacade{
		library: deps.LibraryMetadata,
	}
}

func (deps Dependencies) crawlFacade() crawlFacade {
	return crawlFacade{
		crawlQuality:    deps.CrawlQuality,
		crawlRunContext: deps.CrawlRunContext,
		crawlStage:      deps.CrawlStage,
		crawlResult:     deps.CrawlResult,
		crawlReview:     deps.CrawlReview,
		crawlTask:       deps.CrawlTask,
		crawlFetch:      deps.CrawlFetch,
		crawlRunner:     deps.CrawlRunner,
	}
}

func (deps Dependencies) buildAPI() *API {
	return &API{
		runtime:         deps.runtimeFacade(),
		lookup:          deps.lookupFacade(),
		organizer:       deps.organizerFacade(),
		libraryMetadata: deps.libraryMetadataFacade(),
		crawl:           deps.crawlFacade(),
	}
}

// API is the Wails-facing orchestration layer. It should stay thin: group
// services by domain, route commands, and avoid embedding cross-domain business
// rules that belong inside the underlying services.
//
// Mutable fields below are bridge runtime bookkeeping only. If future work
// needs additional domain state, prefer storing it in the owning service/read
// model rather than expanding bridge-owned execution state here.
type API struct {
	runtime         runtimeFacade
	lookup          lookupFacade
	organizer       organizerFacade
	libraryMetadata libraryMetadataFacade
	crawl           crawlFacade
	activeRunner    *crawlrunner.Runner
	runningMu       sync.Mutex
	runnerDone      chan struct{}
	wailsCtx        context.Context
}

const (
	crawlExecutionModeGoNative         = "go-native"
	crawlExecutionModeCloudflareCompat = "cloudflare-compat"
	crawlExecutionModeIdle             = "idle"
	crawlControllerModeGoTask          = "go-task-controller"
)
