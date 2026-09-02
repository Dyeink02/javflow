package subscriptiontarget

// Package subscriptiontarget defines the lightweight target contract shared by
// lookup and AV-subscription flows. It exists so subscription refresh logic can
// evolve without importing crawler UI/domain types directly.
//
// This package is intentionally smaller than avsubscriptionv2 itself: it captures
// the neutral “resolved actress target” view that lookup, bridge, and future
// subscription refresh paths can all share.
//
// Ownership summary:
// 1) define the neutral target profile shared by lookup and subscription flows
// 2) keep cross-module target identity/count contracts stable
// 3) avoid leaking UI- or package-specific fields into shared target wiring
//
// File map for maintainers:
// 1) module-neutral subscription target DTO
// 2) cross-module actress identity and count fields
// 3) contract boundary comments for future target growth

// TargetProfile describes the resolved actress target in a module-neutral way.
// It is used by lookup, bridge, and subscription wiring without tying the
// actress lookup flow to the avsubscriptionv2 package.
//
// Design rule:
// if a field only matters to one concrete UI/controller path, do not add it
// here. Keep this contract focused on cross-module target identity and counts.
type TargetProfile struct {
	// RequestedActressName preserves the original query for UI feedback only.
	// It is never used as the stable crawl or subscription identity.
	RequestedActressName string `json:"requestedActressName,omitempty"`
	// Alias metadata is present only when a unique local alias was resolved.
	AliasMatchKind      string            `json:"aliasMatchKind,omitempty"`
	AliasMatchedAs      string            `json:"aliasMatchedAs,omitempty"`
	ActressName         string            `json:"actressName"`
	ResolvedActressName string            `json:"resolvedActressName"`
	ResolvedBase        string            `json:"resolvedBase"`
	LookupBaseOrigin    string            `json:"lookupBaseOrigin"`
	MagnetCount         int               `json:"magnetCount"`
	AllCount            int               `json:"allCount"`
	FillCount           int               `json:"fillCount"`
	PreferredCount      int               `json:"preferredCount"`
	ItemsPerPage        int               `json:"itemsPerPage"`
	TotalPages          int               `json:"totalPages"`
	LatestItemURL       string            `json:"latestItemUrl,omitempty"`
	AvatarURL           string            `json:"avatarUrl,omitempty"`
	ProfileFields       map[string]string `json:"profileFields,omitempty"`
	PromotionImageURLs  []string          `json:"promotionImageUrls,omitempty"`
	Works               []ActressWork     `json:"works,omitempty"`
	DisplayedWorks      int               `json:"displayedWorks,omitempty"`
	DataSources         []string          `json:"dataSources,omitempty"`
	DataFetchedAt       string            `json:"dataFetchedAt,omitempty"`
}

// ActressWork is a real work entry parsed from a whitelisted public source.
// It deliberately carries URLs and labels only; it does not imply that a
// work is present in the user's local library.
type ActressWork struct {
	Code           string `json:"code,omitempty"`
	Title          string `json:"title,omitempty"`
	URL            string `json:"url,omitempty"`
	CoverURL       string `json:"coverUrl,omitempty"`
	SourceCoverURL string `json:"sourceCoverUrl,omitempty"`
	ReleaseDate    string `json:"releaseDate,omitempty"`
}
