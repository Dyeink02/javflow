// Ownership summary:
//   This file defines library metadata domain models shared by scanner, builder, and writer.
//
// File map for maintainers:
//   1) ActorImage and MovieInfo types.
//   2) Scan result and provider option types.
//   3) Service constructor and state types.
//
package librarymetadata

import "context"

// ActorImage pairs an actor name with a chosen cover image URL.
type ActorImage struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// MovieInfo is the normalized metadata returned to callers.
// It intentionally mirrors the fields commonly used by Emby/Jellyfin NFO.
type MovieInfo struct {
	Number      string       `json:"number"`
	Title       string       `json:"title"`
	Plot        string       `json:"plot"`
	Outline     string       `json:"outline"`
	ReleaseDate string       `json:"releaseDate"`
	Year        int          `json:"year"`
	Runtime     int          `json:"runtime"`
	Score       float64      `json:"score"`
	Vote        int          `json:"vote"`
	Director    string       `json:"director"`
	Studio      string       `json:"studio"`
	Label       string       `json:"label"`
	Series      string       `json:"series"`
	Genres      []string     `json:"genres"`
	Actors      []string     `json:"actors"`
	ActorImages []ActorImage `json:"actorImages,omitempty"`
	CoverURL    string       `json:"coverUrl"`
	BackdropURL string       `json:"backdropUrl"`
	ThumbURL    string       `json:"thumbUrl"`
	BigThumbURL string       `json:"bigThumbUrl"`
	Provider    string       `json:"provider"`
	Homepage    string       `json:"homepage"`
}

// ScrapeOptions controls how a single code is scraped.
type ScrapeOptions struct {
	Number   string
	Provider string
	Proxy    string
}

// ScrapeResult wraps a successful scrape plus any provider-specific notes.
type ScrapeResult struct {
	Info      *MovieInfo `json:"info,omitempty"`
	Providers []string   `json:"providers"`
	Error     string     `json:"error,omitempty"`
}

// ImageInfo holds the image URLs from one provider candidate. It is used when
// the primary provider's image download fails and we want to retry with
// another provider's URL without rescraping the whole metadata record.
type ImageInfo struct {
	Provider    string `json:"provider"`
	CoverURL    string `json:"coverUrl"`
	BackdropURL string `json:"backdropUrl"`
	ThumbURL    string `json:"thumbUrl"`
	BigThumbURL string `json:"bigThumbUrl"`
}

// LibraryMediaItem represents a single media file discovered in the library root.
type LibraryMediaItem struct {
	Code           string `json:"code"`
	DisplayCode    string `json:"displayCode,omitempty"`
	Title          string `json:"title,omitempty"`
	MediaPath      string `json:"mediaPath"`
	MediaStem      string `json:"mediaStem"`
	NfoPath        string `json:"nfoPath"`
	PosterPath     string `json:"posterPath"`
	BackdropPath   string `json:"backdropPath"`
	LandscapePath  string `json:"landscapePath"`
	HasMedia       bool   `json:"hasMedia"`
	HasNfo         bool   `json:"hasNfo"`
	HasPoster      bool   `json:"hasPoster"`
	HasBackdrop    bool   `json:"hasBackdrop"`
	HasLandscape   bool   `json:"hasLandscape"`
	CrawlMatch     bool   `json:"crawlMatch"`
	MetadataSource string `json:"metadataSource,omitempty"`
	Status         string `json:"status"`
	Tags           string `json:"tags,omitempty"`
	Part           string `json:"part,omitempty"`
	Failed         bool   `json:"failed"`
}

// ScanProgress is emitted periodically while scanning so callers can log or
// display progress instead of staring at a frozen "scanning" indicator.
type ScanProgress struct {
	ScannedFiles int    `json:"scannedFiles"`
	MatchedItems int    `json:"matchedItems"`
	CurrentPath  string `json:"currentPath,omitempty"`
}

// ScanProgressFunc receives progress updates. It may be called concurrently.
type ScanProgressFunc func(ScanProgress)

// ScanOptions controls how the library directory is scanned.
type ScanOptions struct {
	Root           string
	Extensions     []string
	IgnoreDirs     []string
	OutputMode     string // "inplace" or "subfolder"
	CrawlOutputDir string
	UserDataDir    string
	Context        context.Context
	OnProgress     ScanProgressFunc
}

// ScanResult is returned after scanning a library root.
type ScanResult struct {
	Items        []LibraryMediaItem `json:"items"`
	MissingItems []LibraryMediaItem `json:"missingItems"` // 爬虫产物有记录但本地未找到视频文件的番号
	Error        string             `json:"error,omitempty"`
}
