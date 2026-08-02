// Ownership summary:
//
//	This file scans a media library root and emits video entries for metadata scraping.
//
// File map for maintainers:
//  1. Video extension set and scan depth constants.
//  2. Directory walking and progress reporting.
//  3. Public scan entry point and result collection.
package librarymetadata

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// defaultExtensions defines the media file extensions the scanner recognizes.
//
// maxScanDepth limits how deep the scanner walks below the library root.
// Movies are expected to live either directly under root or in a single
// subfolder per title. Deeper directories are usually ad/promo subfolders
// bundled with downloads; skipping them avoids long scans.
const maxScanDepth = 1

var defaultExtensions = map[string]bool{
	".strm": true,
	".mp4":  true,
	".mkv":  true,
	".avi":  true,
	".mov":  true,
	".flv":  true,
	".wmv":  true,
	".ts":   true,
	".m4v":  true,
	".iso":  true,
}

// ScanLibrary recursively scans root and returns discovered media items.
// It uses filepath.WalkDir for performance and respects options.Context so
// long-running scans can time out instead of freezing the UI.
func ScanLibrary(options ScanOptions) ScanResult {
	root := strings.TrimSpace(options.Root)
	if root == "" {
		return ScanResult{Error: "媒体库根目录不能为空"}
	}

	info, err := os.Stat(root)
	if err != nil {
		return ScanResult{Error: fmt.Sprintf("无法访问媒体库目录：%s", err.Error())}
	}
	if !info.IsDir() {
		return ScanResult{Error: "媒体库路径必须是目录"}
	}

	extensions := normalizeExtensions(options.Extensions)
	if len(extensions) == 0 {
		extensions = defaultExtensions
	}

	ignoreDirs := normalizeIgnoreDirs(options.IgnoreDirs)
	outputMode := strings.ToLower(strings.TrimSpace(options.OutputMode))
	if outputMode != "subfolder" {
		outputMode = "inplace"
	}

	ctx := options.Context
	if ctx == nil {
		// Default 30 minute ceiling prevents a runaway scan from hanging forever.
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
	}

	var crawlSource *CrawlSource
	restrictToSelectedCrawlSource := strings.TrimSpace(options.CrawlOutputDir) != ""
	if options.CrawlOutputDir != "" || options.UserDataDir != "" {
		var sourceErr error
		crawlSource, sourceErr = BuildCrawlSource(options.CrawlOutputDir, options.UserDataDir)
		if restrictToSelectedCrawlSource && sourceErr != nil {
			// A selected artifact is an explicit safety boundary. Never fall back
			// to scanning every local file when that boundary cannot be loaded.
			return ScanResult{Error: fmt.Sprintf("选定爬虫产物不可用：%s", sourceErr.Error())}
		}
	}

	items := make([]LibraryMediaItem, 0, 128)
	seen := make(map[string]bool)
	foundCodes := make(map[string]bool)
	type scannedMediaCandidate struct {
		path      string
		filename  string
		ext       string
		extracted CodeExtractionResult
	}
	candidates := make([]scannedMediaCandidate, 0, 128)
	// A network library makes individual os.Stat calls surprisingly expensive.
	// Cache one directory listing and answer all four sidecar checks from it.
	// The cache only lives for this scan, so newly created files are always seen
	// on the next scan and no persistent state can become stale.
	presenceCache := newSidecarPresenceCache()

	scannedFiles := 0
	matchedItems := 0
	lastProgress := time.Now()
	progressInterval := 2 * time.Second
	logProgress := func(currentPath string) {
		if options.OnProgress == nil {
			return
		}
		options.OnProgress(ScanProgress{
			ScannedFiles: scannedFiles,
			MatchedItems: matchedItems,
			CurrentPath:  currentPath,
		})
	}

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return fmt.Errorf("扫描已取消或超时：%w", ctx.Err())
		}

		if walkErr != nil {
			// Log and skip inaccessible paths instead of aborting the whole scan.
			return nil
		}

		rel, _ := filepath.Rel(root, path)
		depth := relativeDepth(rel)

		if d.IsDir() {
			if rel != "." && shouldIgnoreDir(rel, ignoreDirs) {
				return filepath.SkipDir
			}
			if depth > maxScanDepth {
				return filepath.SkipDir
			}
			return nil
		}

		if depth > maxScanDepth {
			return nil
		}

		scannedFiles++
		ext := strings.ToLower(filepath.Ext(path))
		if !extensions[ext] {
			return nil
		}

		filename := d.Name()
		extracted := ExtractCodeFromFilename(filename)
		if extracted.Code == "" {
			return nil
		}
		candidates = append(candidates, scannedMediaCandidate{
			path:      path,
			filename:  filename,
			ext:       ext,
			extracted: extracted,
		})
		matchedItems = len(candidates)

		if time.Since(lastProgress) >= progressInterval {
			logProgress(path)
			lastProgress = time.Now()
		}
		return nil
	})

	if err != nil {
		return ScanResult{Error: fmt.Sprintf("扫描失败：%s", err.Error())}
	}

	// Finish contextual split detection only after the directory walk. This keeps
	// a standalone "-C" compatible with the historical Chinese-subtitle tag,
	// while alphabetic sibling files (A through Z) are emitted as distinct media rows.
	concreteSplitGroups := make(map[string]bool)
	for _, candidate := range candidates {
		if candidate.extracted.Part != "" {
			concreteSplitGroups[splitGroupKey(candidate.path, candidate.extracted.Code)] = true
		}
	}

	for _, candidate := range candidates {
		if ctx.Err() != nil {
			return ScanResult{Error: fmt.Sprintf("扫描已取消或超时：%s", ctx.Err())}
		}
		extracted := candidate.extracted
		if extracted.Part == "" && extracted.candidatePart != "" && concreteSplitGroups[splitGroupKey(candidate.path, extracted.Code)] {
			extracted.Part = extracted.candidatePart
			if strings.EqualFold(extracted.candidatePart, "C") && extracted.Tags == "中文字幕" {
				extracted.Tags = ""
			}
		}

		displayCode := formatDisplayCode(extracted.Code, extracted.Part)
		if restrictToSelectedCrawlSource {
			if crawlSource == nil {
				return ScanResult{Error: "选定爬虫产物不可用"}
			}
			if record, selected := crawlSource.Lookup(extracted.Code); !selected {
				continue
			} else if record.Code != "" {
				// Keep the original filename/display code for sidecar paths, but
				// use the canonical crawler code for metadata resolution.
				extracted.Code = record.Code
			}
		}

		// Separate files of the same release must remain separate rows even though
		// they share one normalized metadata lookup code. The part and directory
		// are deliberately part of the UI identity; the canonical code is reserved
		// for provider lookup and crawl matching.
		key := extracted.Code + "|" + strings.ToUpper(extracted.Part) + "|" + filepath.Dir(candidate.path)
		if seen[key] {
			continue
		}
		seen[key] = true
		foundCodes[extracted.Code] = true
		items = append(items, buildMediaItem(candidate.path, candidate.filename, candidate.ext, extracted, outputMode, crawlSource, presenceCache))
		// A noisy magnet filename may resolve to a canonical crawler code. Keep
		// the original display code so sidecar/UI rows remain traceable to the
		// actual local file (for example MXGS-1183 -> MXGS-118). Never replace
		// DisplayCode with the canonical value after this point: split and alias
		// outputs depend on that distinction when writing sidecars.
		items[len(items)-1].DisplayCode = displayCode
	}
	matchedItems = len(items)
	logProgress("")

	// 当存在爬虫产物时，把“爬虫期望有但本地未找到视频文件”的番号作为只读行返回，
	// 供前端影片清单展示，提示用户这些番号尚未下载到本地。
	missingItems := buildMissingItems(crawlSource, foundCodes)

	return ScanResult{Items: items, MissingItems: missingItems}
}

func splitGroupKey(path, code string) string {
	return strings.ToUpper(strings.TrimSpace(code)) + "|" + filepath.Clean(filepath.Dir(path))
}

// buildMissingItems compares crawler records against locally discovered video codes.
// It returns read-only rows for codes present in the crawl artifacts but without a
// matching local media file. These rows are for display only and must not be scraped.
func buildMissingItems(crawlSource *CrawlSource, foundCodes map[string]bool) []LibraryMediaItem {
	if crawlSource == nil {
		return nil
	}

	records := crawlSource.Records()
	missing := make([]LibraryMediaItem, 0, len(records))
	for _, record := range records {
		code := strings.ToUpper(strings.TrimSpace(record.Code))
		if code == "" || foundCodes[code] {
			continue
		}

		missing = append(missing, LibraryMediaItem{
			Code:           code,
			Title:          cleanCrawlerTitle(record.Title, record.Code),
			HasMedia:       false,
			CrawlMatch:     true,
			MetadataSource: record.Source,
			Status:         "未下载",
		})
	}

	// 让缺视频行按番号稳定排序，便于用户对照查看。
	sort.Slice(missing, func(i, j int) bool {
		return strings.ToLower(missing[i].Code) < strings.ToLower(missing[j].Code)
	})
	return missing
}

func buildMediaItem(path, filename, ext string, extracted CodeExtractionResult, outputMode string, crawlSource *CrawlSource, presenceCache *sidecarPresenceCache) LibraryMediaItem {
	dir := filepath.Dir(path)
	stem := computeMediaStem(filename, ext)
	displayCode := formatDisplayCode(extracted.Code, extracted.Part)

	var nfoPath, posterPath, backdropPath, landscapePath string
	if outputMode == "subfolder" {
		folder := filepath.Join(dir, displayCode)
		nfoPath = filepath.Join(folder, "movie.nfo")
		posterPath = filepath.Join(folder, "poster.jpg")
		backdropPath = filepath.Join(folder, "backdrop.jpg")
		landscapePath = filepath.Join(folder, "landscape.jpg")
	} else {
		nfoPath = filepath.Join(dir, stem+".nfo")
		posterPath = filepath.Join(dir, stem+"-poster.jpg")
		backdropPath = filepath.Join(dir, stem+"-backdrop.jpg")
		landscapePath = filepath.Join(dir, stem+"-landscape.jpg")
	}

	item := LibraryMediaItem{
		Code:          extracted.Code,
		DisplayCode:   displayCode,
		MediaPath:     path,
		MediaStem:     stem,
		NfoPath:       nfoPath,
		PosterPath:    posterPath,
		BackdropPath:  backdropPath,
		LandscapePath: landscapePath,
		HasMedia:      true,
		HasNfo:        presenceCache.exists(nfoPath),
		HasPoster:     presenceCache.exists(posterPath),
		HasBackdrop:   presenceCache.exists(backdropPath),
		HasLandscape:  presenceCache.exists(landscapePath),
		Tags:          extracted.Tags,
		Part:          extracted.Part,
	}

	if crawlSource != nil {
		if record, ok := crawlSource.Lookup(extracted.Code); ok {
			item.CrawlMatch = true
			item.MetadataSource = record.Source
			if record.Title != "" {
				item.Title = cleanCrawlerTitle(record.Title, record.Code)
			}
		}
	}

	item.Status = computeItemStatus(item)
	return item
}

func formatDisplayCode(code, part string) string {
	code = strings.TrimSpace(code)
	part = strings.TrimSpace(part)
	if code == "" || part == "" {
		return code
	}
	if strings.HasPrefix(part, "_") {
		return code + strings.ToUpper(part)
	}
	part = strings.Trim(part, "-_")
	if part == "" {
		return code
	}
	return code + "-" + strings.ToUpper(part)
}

// computeMediaStem returns the base name used for sidecar metadata files.
// It strips the file extension while preserving any intermediate extension
// that .strm files carry, e.g. "BBAN-452.mp4.strm" becomes "BBAN-452.mp4".
func computeMediaStem(filename, ext string) string {
	return strings.TrimSuffix(filename, ext)
}

func computeItemStatus(item LibraryMediaItem) string {
	if item.HasNfo && item.HasPoster && item.HasBackdrop && item.HasLandscape {
		return "已完整"
	}
	if !item.HasNfo {
		return "缺NFO"
	}
	if !item.HasPoster {
		return "缺封面"
	}
	if !item.HasBackdrop {
		return "缺背景"
	}
	if !item.HasLandscape {
		return "缺横图"
	}
	return "待刮削"
}

// sidecarPresenceCache batches the four metadata existence checks for each
// media item into one directory read. This is especially important for SMB,
// mapped NAS, and other high-latency filesystems where four Stat calls per
// video can turn a small library scan into minutes of waiting.
type sidecarPresenceCache struct {
	directories map[string]map[string]struct{}
}

func newSidecarPresenceCache() *sidecarPresenceCache {
	return &sidecarPresenceCache{directories: make(map[string]map[string]struct{})}
}

func (c *sidecarPresenceCache) exists(path string) bool {
	if c == nil {
		return fileExists(path)
	}

	directory := filepath.Dir(path)
	entries, loaded := c.directories[directory]
	if !loaded {
		entries = make(map[string]struct{})
		if directoryEntries, err := os.ReadDir(directory); err == nil {
			for _, entry := range directoryEntries {
				entries[sidecarNameKey(entry.Name())] = struct{}{}
			}
		}
		// Store empty/error results as well. A missing subfolder is common in
		// subfolder output mode and must not trigger four repeated probes.
		c.directories[directory] = entries
	}

	_, ok := entries[sidecarNameKey(filepath.Base(path))]
	return ok
}

func sidecarNameKey(name string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(name)
	}
	return name
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func normalizeExtensions(input []string) map[string]bool {
	result := make(map[string]bool)
	for _, ext := range input {
		ext := strings.ToLower(strings.TrimSpace(ext))
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		result[ext] = true
	}
	return result
}

func normalizeIgnoreDirs(input []string) []string {
	result := make([]string, 0, len(input))
	for _, dir := range input {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		result = append(result, strings.ToLower(dir))
	}
	return result
}

func shouldIgnoreDir(rel string, ignoreDirs []string) bool {
	lowerRel := strings.ToLower(rel)
	for _, ignore := range ignoreDirs {
		if lowerRel == ignore || strings.HasPrefix(lowerRel, ignore+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// relativeDepth returns how many directory levels rel is below root.
// "." has depth 0, "movie.mp4" has depth 0, "Folder/movie.mp4" has depth 1.
func relativeDepth(rel string) int {
	rel = strings.TrimSpace(rel)
	if rel == "" || rel == "." {
		return 0
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	// The last part is the file or leaf directory name; its parent count is depth.
	return len(parts) - 1
}
