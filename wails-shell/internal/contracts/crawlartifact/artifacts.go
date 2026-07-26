package crawlartifact

// Package crawlartifact defines the persisted crawl-output contract shared by
// crawler-adjacent modules. Organizer and subscription flows should depend on
// these artifact shapes instead of live crawler runtime state.
//
// Boundary rule:
// this package describes what a persisted crawl artifact looks like; it does
// not own crawl execution, organizer execution, or subscription refresh logic.
//
// Ownership summary:
// 1) define the shared persisted crawl artifact data contract
// 2) keep cross-module artifact DTOs stable for organizer/subscription/read-model consumers
// 3) separate persisted data shapes from execution/runtime logic
//
// File map for maintainers:
// 1) schema/version constants
// 2) shared magnet/code artifact DTOs
// 3) crawl profile and persisted-output artifact DTOs

const CurrentSchemaVersion = 1

// MagnetEntry is the stable cross-module magnet shape shared by organizer and
// post-crawl artifact readers. Keep it narrow so later modules can reuse it
// without importing crawler-side rich result models.
type MagnetEntry struct {
	Link string `json:"link"`
	Size string `json:"size,omitempty"`
}

// CodeEntry is the organizer-facing snapshot shape for one unique film code.
// It is intentionally persisted as data, not as an organizer runtime type.
type CodeEntry struct {
	Code    string        `json:"code"`
	Title   string        `json:"title,omitempty"`
	Magnets []MagnetEntry `json:"magnets,omitempty"`
}

// CrawlProfileArtifact is the small persisted crawl summary intended for
// module-to-module handoff, especially subscription refresh and "recent crawl"
// preload flows.
//
// This is the preferred lightweight handoff for “what was crawled just now”.
// If callers only need actress identity, crawl URL, counts, or output paths,
// they should prefer this artifact over re-reading the whole filmData payload.
type CrawlProfileArtifact struct {
	SchemaVersion int                     `json:"schemaVersion"`
	RunID          string                  `json:"runId"`
	CompletedAt    string                  `json:"completedAt"`
	ActressName    string                  `json:"actressName,omitempty"`
	CrawlURL       string                  `json:"crawlURL,omitempty"`
	TargetCount    int                     `json:"targetCount"`
	CompletedCount int                     `json:"completedCount"`
	ItemsPerPage   int                     `json:"itemsPerPage,omitempty"`
	TotalPages     int                     `json:"totalPages,omitempty"`
	OutputDir      string                  `json:"outputDir"`
	FilmDataPath   string                  `json:"filmDataPath"`
	SiteBase       string                  `json:"siteBase,omitempty"`
	FilteredCodes  []FilteredFilmCodeEntry `json:"filteredCodes,omitempty"`
}

// FilteredFilmCodeEntry records one film code that was removed from public
// deliverables (filmData.json / magnet-links.txt) and the reason it was
// filtered. Keeping the full list lets downstream modules show users exactly
// which codes were excluded instead of only a count.
type FilteredFilmCodeEntry struct {
	Code   string `json:"code"`
	Reason string `json:"reason,omitempty"`
	Remark string `json:"remark,omitempty"`
}

// FilteredCodesArtifact is the user-facing and machine-readable filtered-code
// ledger. It supplements filtered-codes.txt with structured metadata so UI and
// downstream workflows can display the exact codes that were removed.
type FilteredCodesArtifact struct {
	SchemaVersion int                     `json:"schemaVersion"`
	RunID         string                  `json:"runId,omitempty"`
	CompletedAt   string                  `json:"completedAt,omitempty"`
	FilteredCodes []FilteredFilmCodeEntry `json:"filteredCodes"`
}

// OrganizerCodesArtifact is the stable organizer preload snapshot written
// after crawl output changes. It keeps organizer coupled to persisted data
// rather than the crawler runtime state machine.
//
// This artifact exists specifically so organizer can consume the crawler's
// unique-code view without re-deriving it from loose runtime state.
type OrganizerCodesArtifact struct {
	SchemaVersion    int                     `json:"schemaVersion"`
	RunID           string                  `json:"runId"`
	CompletedAt     string                  `json:"completedAt"`
	ActressName     string                  `json:"actressName,omitempty"`
	OutputDir       string                  `json:"outputDir"`
	FilmDataPath    string                  `json:"filmDataPath"`
	TotalRecords    int                     `json:"totalRecords"`
	UniqueCodeCount int                     `json:"uniqueCodeCount"`
	Codes           []string                `json:"codes"`
	CodeEntries     []CodeEntry             `json:"codeEntries"`
	FilteredCodes   []FilteredFilmCodeEntry `json:"filteredCodes,omitempty"`
}
