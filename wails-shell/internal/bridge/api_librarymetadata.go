// Ownership summary:
//
//	This file exposes library metadata scraping bridge commands to the desktop UI.
//
// File map for maintainers:
//  1. Provider, bootstrap, scan, and scrape command handlers.
//  2. Result marshalling and path normalization.
//  3. Integration with librarymetadata service and crawl artifacts.
package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"javflow/internal/avsubscriptionv2"
	"javflow/internal/contracts/crawlartifact"
	"javflow/internal/librarymetadata"
)

// Library metadata commands expose the embedded metatube-sdk-go scraper to the
// desktop UI without requiring a separate MetaTube server.
func (a *API) handleLibraryMetadataCommand(command string, payload map[string]any) (string, bool, error) {
	switch command {
	case "app:librarymetadata-providers":
		result, err := a.libraryMetadataProvidersResult()
		return result, true, err
	case "app:librarymetadata-bootstrap":
		result, err := a.libraryMetadataBootstrapResult(payload)
		return result, true, err
	case "app:librarymetadata-scan":
		result, err := a.libraryMetadataScanResult(payload)
		return result, true, err
	case "app:librarymetadata-scrape":
		result, err := a.libraryMetadataScrapeResult(payload)
		return result, true, err
	case "app:librarymetadata-build-nfo":
		result, err := a.libraryMetadataBuildNFOResult(payload)
		return result, true, err
	case "app:librarymetadata-write":
		result, err := a.libraryMetadataWriteResult(payload)
		return result, true, err
	case "app:librarymetadata-resolve":
		result, err := a.libraryMetadataResolveResult(payload)
		return result, true, err
	case "app:librarymetadata-job-start":
		return a.libraryMetadataJobStartResult(payload)
	case "app:librarymetadata-job-cancel":
		return a.libraryMetadataJobCancelResult(payload)
	case "app:librarymetadata-job-finish":
		return a.libraryMetadataJobFinishResult(payload)
	case "app:librarymetadata-auto-subscribe":
		result, err := a.libraryMetadataAutoSubscribeResult(payload)
		return result, true, err
	case "app:librarymetadata-crawl-sources":
		result, err := a.libraryMetadataCrawlSourcesResult(payload)
		return result, true, err
	case "app:librarymetadata-count-crawl-artifacts":
		result, err := a.libraryMetadataCountCrawlArtifactsResult(payload)
		return result, true, err
	case "app:librarymetadata-init-log":
		result, err := a.libraryMetadataInitLogResult(payload)
		return result, true, err
	case "app:librarymetadata-append-log":
		result, err := a.libraryMetadataAppendLogResult(payload)
		return result, true, err
	case "app:librarymetadata-open-log-folder":
		result, err := a.libraryMetadataOpenLogFolderResult(payload)
		return result, true, err
	}

	return "", false, nil
}

func (a *API) libraryMetadataInitLogResult(payload map[string]any) (string, error) {
	rootPath := nonEmptyString(payload["rootPath"])
	if rootPath == "" {
		return "", fmt.Errorf("媒体库根目录不能为空")
	}

	logDir, err := a.libraryMetadata.library.LogManager.InitLog(rootPath)
	if err != nil {
		return "", err
	}

	result, err := marshalResult(map[string]any{
		"logDir": logDir,
	})
	if err != nil {
		return "", err
	}
	return result, nil
}

func (a *API) libraryMetadataAppendLogResult(payload map[string]any) (string, error) {
	rootPath := nonEmptyString(payload["rootPath"])
	line := nonEmptyString(payload["line"])
	if rootPath == "" {
		return "", fmt.Errorf("媒体库根目录不能为空")
	}

	if err := a.libraryMetadata.library.LogManager.AppendLog(rootPath, line); err != nil {
		return "", err
	}

	result, err := marshalResult(map[string]any{
		"success": true,
	})
	if err != nil {
		return "", err
	}
	return result, nil
}

func (a *API) libraryMetadataOpenLogFolderResult(payload map[string]any) (string, error) {
	rootPath := nonEmptyString(payload["rootPath"])
	if rootPath == "" {
		return "", fmt.Errorf("媒体库根目录不能为空")
	}

	logDir, err := a.libraryMetadata.library.LogManager.OpenLogFolder(rootPath)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return "", err
	}

	openedPath, err := a.runtime.dialogs.OpenPath(logDir)
	if err != nil {
		return "", err
	}

	result, err := marshalResult(openedPath)
	if err != nil {
		return "", err
	}
	return result, nil
}

func (a *API) libraryMetadataProvidersResult() (string, error) {
	providers := a.libraryMetadata.library.SearchProviders()
	response := map[string]any{
		"providers": providers,
	}
	return string(mustRawJSON(response)), nil
}

func (a *API) libraryMetadataBootstrapResult(payload map[string]any) (string, error) {
	sourcesJSON, err := a.libraryMetadataCrawlSourcesResult(payload)
	if err != nil {
		return "", err
	}
	response := map[string]any{}
	if err := json.Unmarshal([]byte(sourcesJSON), &response); err != nil {
		return "", err
	}
	response["providers"] = a.libraryMetadata.library.SearchProviders()
	return string(mustRawJSON(response)), nil
}

func (a *API) libraryMetadataAutoSubscribeResult(payload map[string]any) (string, error) {
	if a.lookup.avSubscriptionsV2 == nil || a.lookup.actressLookup == nil {
		return "", fmt.Errorf("AV 订阅服务尚未就绪")
	}
	crawlOutputDir := normalizeLibraryMetadataCrawlOutputDir(nonEmptyString(payload["crawlOutputDir"]))
	preferredOutputDir := normalizeLibraryMetadataCrawlOutputDir(nonEmptyString(payload["preferredOutputDir"]))
	if preferredOutputDir == "" {
		preferredOutputDir = crawlOutputDir
	}
	actressName, err := resolveAutoSubscriptionActressName(
		nonEmptyString(payload["actressName"]),
		a.runtime.store.UserDataDir(),
		firstNonEmpty(crawlOutputDir, preferredOutputDir),
	)
	if err != nil {
		return "", err
	}

	items, err := a.lookup.avSubscriptionsV2.List()
	if err != nil {
		return "", err
	}
	for _, item := range items {
		if sameLibraryMetadataActress(item.ActressName, actressName) {
			return string(mustRawJSON(map[string]any{"subscription": item, "added": false})), nil
		}
	}

	profile, err := a.lookup.actressLookup.ResolveTarget(a.buildActressLookupOptions(payload))
	if err != nil {
		return "", err
	}
	resolvedName := strings.TrimSpace(profile.ResolvedActressName)
	if resolvedName == "" {
		resolvedName = actressName
	}
	for _, item := range items {
		if sameLibraryMetadataActress(item.ActressName, resolvedName) ||
			(strings.TrimSpace(profile.ResolvedBase) != "" && strings.EqualFold(strings.TrimSpace(item.CrawlURL), strings.TrimSpace(profile.ResolvedBase))) {
			return string(mustRawJSON(map[string]any{"subscription": item, "added": false})), nil
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	saved, err := a.lookup.avSubscriptionsV2.CreateManual(ctx, avsubscriptionv2.ManualCreateRequest{
		ActressName:     resolvedName,
		CrawlURL:        profile.ResolvedBase,
		PreferredBase:   profile.LookupBaseOrigin,
		DeclaredTotal:   profile.PreferredCount,
		DeclaredPages:   profile.TotalPages,
		DeclaredPerPage: profile.ItemsPerPage,
		PreferredOutDir: autoSubscriptionOutputDir(preferredOutputDir, resolvedName),
		Proxy:           nonEmptyString(payload["proxy"]),
		RuntimeOptions:  a.buildSubscriptionV2RuntimeOptions(payload),
		SourceType:      "metadata-auto",
	})
	if err != nil {
		return "", err
	}
	if a.runtime.bus != nil {
		a.runtime.bus.Emit("avsubscriptionv2.list-updated", map[string]any{
			"trigger":     "librarymetadata-auto-subscribe",
			"actressName": saved.ActressName,
		})
	}
	return string(mustRawJSON(map[string]any{"subscription": saved, "added": true})), nil
}

// resolveAutoSubscriptionActressName applies the actor-safety boundary before
// any lookup or subscription write occurs. A selected crawler snapshot must
// carry its own profile target; falling back to the first actor on a movie page
// would reintroduce the compilation/guest-actor subscription bug.
func resolveAutoSubscriptionActressName(input, userDataDir, crawlOutputDir string) (string, error) {
	input = strings.TrimSpace(input)
	crawlOutputDir = normalizeLibraryMetadataCrawlOutputDir(crawlOutputDir)
	if crawlOutputDir != "" {
		if target := crawlProfileActressName(userDataDir, crawlOutputDir); target != "" {
			return target, nil
		}
		return "", fmt.Errorf("所选爬虫产物缺少目标女优信息，已跳过自动订阅")
	}
	if input == "" {
		return "", fmt.Errorf("女优名称不能为空")
	}
	return input, nil
}

func crawlProfileActressName(userDataDir, outputDir string) string {
	outputDir = normalizeLibraryMetadataCrawlOutputDir(outputDir)
	if outputDir == "" {
		return ""
	}
	_, profile, err := crawlartifact.ReadCrawlProfileArtifactWithUserData(outputDir, userDataDir)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(profile.ActressName)
}

// normalizeLibraryMetadataCrawlOutputDir accepts either the crawl output
// directory or one of its artifact files. The UI intentionally supports both
// forms, so cross-module actor lookup must use the same containing directory
// before resolving hidden artifacts.
func normalizeLibraryMetadataCrawlOutputDir(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	if info, err := os.Stat(trimmed); err == nil && info.Mode().IsRegular() {
		return filepath.Dir(trimmed)
	}

	switch strings.ToLower(filepath.Base(trimmed)) {
	case strings.ToLower(crawlartifact.CrawlFilmDataFile),
		strings.ToLower(crawlartifact.CrawlProfileFile),
		strings.ToLower(crawlartifact.OrganizerCodesFile),
		strings.ToLower(crawlartifact.DefaultMagnetTxt):
		return filepath.Dir(trimmed)
	default:
		return trimmed
	}
}

func sameLibraryMetadataActress(left, right string) bool {
	normalize := func(value string) string {
		return strings.ToLower(strings.NewReplacer(" ", "", "\t", "", "・", "", "·", "").Replace(strings.TrimSpace(value)))
	}
	return normalize(left) != "" && normalize(left) == normalize(right)
}

func autoSubscriptionOutputDir(currentOutputDir, actressName string) string {
	currentOutputDir = strings.TrimSpace(currentOutputDir)
	if currentOutputDir == "" {
		return ""
	}
	if info, err := os.Stat(currentOutputDir); err == nil && !info.IsDir() {
		currentOutputDir = filepath.Dir(currentOutputDir)
	}
	if strings.Contains(strings.ToLower(filepath.Base(currentOutputDir)), strings.ToLower(strings.TrimSpace(actressName))) {
		return currentOutputDir
	}
	safeName := strings.NewReplacer("\\", "_", "/", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_").Replace(strings.TrimSpace(actressName))
	if safeName == "" {
		return currentOutputDir
	}
	return filepath.Join(filepath.Dir(currentOutputDir), safeName)
}

func (a *API) libraryMetadataScanResult(payload map[string]any) (string, error) {
	rootPath := nonEmptyString(payload["root"])
	if rootPath == "" {
		return "", fmt.Errorf("媒体库根目录不能为空")
	}

	// Long-running scans can freeze the UI; cap at 30 minutes and emit progress
	// logs to the library log so operators can see movement.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	progressLogger := func(progress librarymetadata.ScanProgress) {
		_ = a.libraryMetadata.library.LogManager.AppendLog(rootPath, fmt.Sprintf(
			"扫描进度：已扫描 %d 个文件，命中 %d 部影片%s",
			progress.ScannedFiles,
			progress.MatchedItems,
			progressText(progress.CurrentPath),
		))
		if a.runtime.bus != nil {
			a.runtime.bus.Emit("librarymetadata.scan.progress", map[string]any{
				"rootPath":     rootPath,
				"scannedFiles": progress.ScannedFiles,
				"matchedItems": progress.MatchedItems,
				"currentPath":  progress.CurrentPath,
			})
		}
	}

	options := librarymetadata.ScanOptions{
		Root:           rootPath,
		Extensions:     stringSliceValue(payload["extensions"]),
		IgnoreDirs:     stringSliceValue(payload["ignoreDirs"]),
		OutputMode:     nonEmptyString(payload["outputMode"]),
		CrawlOutputDir: nonEmptyString(payload["crawlOutputDir"]),
		UserDataDir:    a.runtime.store.UserDataDir(),
		Context:        ctx,
		OnProgress:     progressLogger,
	}

	_ = a.libraryMetadata.library.LogManager.AppendLog(rootPath, fmt.Sprintf("开始扫描：%s", rootPath))
	result := librarymetadata.ScanLibrary(options)
	_ = a.libraryMetadata.library.LogManager.AppendLog(rootPath, fmt.Sprintf(
		"扫描结束：本地 %d 部，缺失 %d 部%s",
		len(result.Items),
		len(result.MissingItems),
		errorText(result.Error),
	))
	return string(mustRawJSON(result)), nil
}

func errorText(err string) string {
	if err == "" {
		return ""
	}
	return fmt.Sprintf("，错误：%s", err)
}

func progressText(currentPath string) string {
	if currentPath == "" {
		return ""
	}
	return fmt.Sprintf("，当前 %s", filepath.Base(currentPath))
}

func (a *API) libraryMetadataScrapeResult(payload map[string]any) (string, error) {
	proxyValue := nonEmptyString(payload["proxy"])
	if proxyValue == "" {
		proxyValue = a.currentProxyFromSettings()
	}

	options := librarymetadata.ScrapeOptions{
		Number:   nonEmptyString(payload["number"]),
		Provider: nonEmptyString(payload["provider"]),
		Proxy:    proxyValue,
	}

	if options.Number == "" {
		return "", fmt.Errorf("番号不能为空")
	}

	ctx := a.libraryMetadata.library.ContextForJob(nonEmptyString(payload["jobId"]))
	result, err := a.libraryMetadata.library.ScrapeByNumber(ctx, options)
	if err != nil {
		return "", err
	}

	return string(mustRawJSON(result)), nil
}

// currentProxyFromSettings reads the user's saved proxy setting from the settings store.
func (a *API) currentProxyFromSettings() string {
	settings := a.loadBridgeSettingsSnapshot()
	return nonEmptyString(settings["proxy"])
}

func (a *API) libraryMetadataBuildNFOResult(payload map[string]any) (string, error) {
	raw, ok := payload["info"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("缺少 info 数据")
	}

	info := &librarymetadata.MovieInfo{}
	data, err := json.Marshal(raw)
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(data, info); err != nil {
		return "", err
	}

	nfoBytes, err := librarymetadata.BuildNFO(info)
	if err != nil {
		return "", err
	}

	response := map[string]any{
		"nfo": string(nfoBytes),
	}
	return string(mustRawJSON(response)), nil
}

func (a *API) libraryMetadataWriteResult(payload map[string]any) (string, error) {
	proxyValue := nonEmptyString(payload["proxy"])
	if proxyValue == "" {
		proxyValue = a.currentProxyFromSettings()
	}

	info, err := decodeMovieInfoPayload(payload)
	if err != nil {
		return "", err
	}

	item, err := decodeLibraryMediaItemPayload(payload)
	if err != nil {
		return "", err
	}

	writeOptions := librarymetadata.WriteOptions{
		Item:          item,
		Info:          info,
		FallbackInfos: decodeImageInfoSlice(payload["fallbackInfos"]),
		Proxy:         proxyValue,
		LibraryRoot:   nonEmptyString(payload["libraryRoot"]),
		SkipNfo:       boolValue(payload["skipNfo"], false),
		SkipImages:    boolValue(payload["skipImages"], false),
		SkipPoster:    boolValue(payload["skipPoster"], false),
		SkipBackdrop:  boolValue(payload["skipBackdrop"], false),
		SkipLandscape: boolValue(payload["skipLandscape"], false),
	}
	result := librarymetadata.WriteResult{}
	if jobID := nonEmptyString(payload["jobId"]); jobID != "" {
		result = a.libraryMetadata.library.WriteMetadataContext(a.libraryMetadata.library.ContextForJob(jobID), writeOptions)
	} else {
		result = a.libraryMetadata.library.WriteMetadata(writeOptions)
	}
	return string(mustRawJSON(result)), nil
}

func decodeMovieInfoPayload(payload map[string]any) (*librarymetadata.MovieInfo, error) {
	raw, ok := payload["info"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("缺少 info 数据")
	}
	info := &librarymetadata.MovieInfo{}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, info); err != nil {
		return nil, err
	}
	return info, nil
}

func decodeLibraryMediaItemPayload(payload map[string]any) (librarymetadata.LibraryMediaItem, error) {
	raw, ok := payload["item"].(map[string]any)
	if !ok {
		return librarymetadata.LibraryMediaItem{}, fmt.Errorf("缺少 item 数据")
	}
	item := librarymetadata.LibraryMediaItem{}
	data, err := json.Marshal(raw)
	if err != nil {
		return item, err
	}
	if err := json.Unmarshal(data, &item); err != nil {
		return item, err
	}
	return item, nil
}

func (a *API) libraryMetadataResolveResult(payload map[string]any) (string, error) {
	proxyValue := nonEmptyString(payload["proxy"])
	if proxyValue == "" {
		proxyValue = a.currentProxyFromSettings()
	}

	options := librarymetadata.ResolveMetadataOptions{
		Number:         nonEmptyString(payload["number"]),
		Provider:       nonEmptyString(payload["provider"]),
		Proxy:          proxyValue,
		CrawlOutputDir: nonEmptyString(payload["crawlOutputDir"]),
		UserDataDir:    a.runtime.store.UserDataDir(),
		PreferSource:   nonEmptyString(payload["preferSource"]),
		MaxAttempts:    intValue(payload["maxAttempts"], 1),
	}

	result := a.libraryMetadata.library.ResolveMetadata(
		a.libraryMetadata.library.ContextForJob(nonEmptyString(payload["jobId"])),
		options,
	)
	return string(mustRawJSON(result)), nil
}

func (a *API) libraryMetadataJobStartResult(payload map[string]any) (string, bool, error) {
	if err := a.libraryMetadata.library.StartJob(nonEmptyString(payload["jobId"])); err != nil {
		return "", true, err
	}
	return string(mustRawJSON(map[string]any{"started": true})), true, nil
}

func (a *API) libraryMetadataJobCancelResult(payload map[string]any) (string, bool, error) {
	cancelled := a.libraryMetadata.library.CancelJob(nonEmptyString(payload["jobId"]))
	return string(mustRawJSON(map[string]any{"cancelled": cancelled})), true, nil
}

func (a *API) libraryMetadataJobFinishResult(payload map[string]any) (string, bool, error) {
	a.libraryMetadata.library.FinishJob(nonEmptyString(payload["jobId"]))
	return string(mustRawJSON(map[string]any{"finished": true})), true, nil
}

func (a *API) libraryMetadataCrawlSourcesResult(payload map[string]any) (string, error) {
	settings := a.loadBridgeSettingsSnapshot()
	defaultOutputDir := nonEmptyString(settings["output"])
	roots := stringSliceValue(payload["roots"])
	if defaultOutputDir != "" {
		roots = append(roots, defaultOutputDir)
		if parent := filepath.Dir(defaultOutputDir); parent != "" && parent != defaultOutputDir {
			roots = append(roots, parent)
		}
	}
	sources := a.libraryMetadata.library.ListCrawlSources(a.runtime.store.UserDataDir(), roots)
	labels := make([]map[string]string, 0, len(sources))
	for _, source := range sources {
		labels = append(labels, map[string]string{
			"cacheKey":    source.CacheKey,
			"actressName": source.ActressName,
			"outputDir":   source.OutputDir,
			"updatedAt":   source.UpdatedAt,
			"label":       crawlartifact.CacheSnapshotLabel(source),
		})
	}
	return string(mustRawJSON(map[string]any{
		"sources":          labels,
		"defaultOutputDir": defaultOutputDir,
	})), nil
}

func (a *API) libraryMetadataCountCrawlArtifactsResult(payload map[string]any) (string, error) {
	outputDir := nonEmptyString(payload["outputDir"])
	if outputDir == "" {
		return string(mustRawJSON(map[string]any{"count": 0})), nil
	}

	count := a.libraryMetadata.library.CountCrawlArtifacts(outputDir)
	return string(mustRawJSON(map[string]any{"count": count})), nil
}

func decodeImageInfoSlice(raw any) []librarymetadata.ImageInfo {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}

	result := make([]librarymetadata.ImageInfo, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		result = append(result, librarymetadata.ImageInfo{
			Provider:    nonEmptyString(m["provider"]),
			CoverURL:    nonEmptyString(m["coverUrl"]),
			BackdropURL: nonEmptyString(m["backdropUrl"]),
			ThumbURL:    nonEmptyString(m["thumbUrl"]),
			BigThumbURL: nonEmptyString(m["bigThumbUrl"]),
		})
	}
	return result
}
