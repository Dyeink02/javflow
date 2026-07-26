package bridge

// dispatchStep keeps the top-level bridge command chain data-driven so the
// order of command ownership stays explicit and easier to review.
type dispatchStep struct {
	name    string
	handler func(command string, payload map[string]any) (string, bool, error)
}

// dispatchChain lists bridge-owned command domains in the order they should be
// consulted. Keep sidecar fallback outside this chain so compatibility traffic
// remains an explicit last resort in `api_dispatch.go`.
//
// If a command appears to be routed to the wrong domain, inspect this list
// before tracing individual handlers.
//
// Ordering rule:
// put broad runtime/crawl domains before narrower feature modules, but keep
// organizer/subscription as separate tail domains so they do not regress into
// the crawler command surface again.
//
// Decoupling rule:
// if a future feature needs organizer/subscription behavior, add it through the
// existing Go domain handlers instead of routing it to sidecar compatibility.
//
// Ownership summary:
// 1) define the ordered bridge command dispatch chain
// 2) make command-domain ownership explicit and reviewable
// 3) keep sidecar fallback outside the normal bridge domain chain
//
// File map for maintainers:
// 1) dispatch-step DTO
// 2) ordered bridge dispatch chain builder
func (a *API) dispatchChain() []dispatchStep {
	return []dispatchStep{
		{name: "runtime", handler: a.handleRuntimeCommand},
		{name: "crawl", handler: a.handleCrawlCommand},
		{name: "dependency-learning", handler: a.handleDependencyLearningCommand},
		{name: "lookup", handler: a.handleLookupCommand},
		{name: "dialog", handler: a.handleDialogCommand},
		{name: "subscription-v2", handler: a.handleSubscriptionV2Command},
		{name: "subscription", handler: a.handleSubscriptionCommand},
		{name: "organizer", handler: a.handleOrganizerCommand},
		{name: "librarymetadata", handler: a.handleLibraryMetadataCommand},
	}
}
