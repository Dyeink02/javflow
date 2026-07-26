package subscriptiontarget

// Package subscriptiontarget defines the lightweight target contract shared by
// lookup and AV-subscription flows. It exists so subscription refresh logic can
// evolve without importing crawler UI/domain types directly.
//
// This package is intentionally smaller than avsubscription itself: it captures
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
// actress lookup flow to the avsubscription package.
//
// Design rule:
// if a field only matters to one concrete UI/controller path, do not add it
// here. Keep this contract focused on cross-module target identity and counts.
type TargetProfile struct {
	ActressName         string `json:"actressName"`
	ResolvedActressName string `json:"resolvedActressName"`
	ResolvedBase        string `json:"resolvedBase"`
	LookupBaseOrigin    string `json:"lookupBaseOrigin"`
	MagnetCount         int    `json:"magnetCount"`
	AllCount            int    `json:"allCount"`
	FillCount           int    `json:"fillCount"`
	PreferredCount      int    `json:"preferredCount"`
	ItemsPerPage        int    `json:"itemsPerPage"`
	TotalPages          int    `json:"totalPages"`
	LatestItemURL       string `json:"latestItemUrl,omitempty"`
}
