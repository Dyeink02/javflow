package adlearning

import (
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// learning_samples.go owns sample discovery, normalization, and persistence
// helpers for the ad-learning workspace.
//
// Ownership summary:
// 1) discover and normalize local ad-learning sample inputs
// 2) persist sample metadata and managed workspace structure
// 3) keep sample import/persistence separate from model evaluation logic
//
// File map for maintainers:
// 1) extension/code normalization helpers
// 2) managed-workspace and ignored-directory helpers
// 3) sample import and code-driven learning entrypoints
// 4) sample metadata persistence and observability shaping

var (
	imageExtensions = map[string]struct{}{
		".jpg": {}, ".jpeg": {}, ".png": {}, ".bmp": {}, ".webp": {},
	}
	videoExtensions = map[string]struct{}{
		".mp4": {}, ".mkv": {}, ".avi": {}, ".mov": {}, ".wmv": {}, ".flv": {}, ".ts": {}, ".m4v": {},
	}
	managedDirNames = map[string]struct{}{
		normalizeDirName("待整理"):                    {},
		normalizeDirName("待删除"):                    {},
		normalizeDirName("含开头广告"):                  {},
		normalizeDirName("logs"):                   {},
		normalizeDirName(".video-organizer-state"): {},
	}
	defaultIgnoredDirNames = map[string]struct{}{
		normalizeDirName("2048"): {},
		normalizeDirName("宣传文件"): {},
		normalizeDirName("宣傳文件"): {},
	}
	codeListSplitPattern = regexp.MustCompile(`[\r\n,，、;\t ]+`)
	codeTokenPattern     = regexp.MustCompile(`[^A-Z0-9]`)
	codeNormalizePattern = regexp.MustCompile(`^([A-Z]{2,12})-?(\d{2,8})([A-Z]*)$`)
)

type ProgressSink func(map[string]any)

type ImportSamplesOptions struct {
	Label       string
	SamplePaths []string
	ModelType   string
}

type LearnSamplesOptions struct {
	Label                 string
	Codes                 []string
	RootPath              string
	IncludeSubdirectories bool
	IgnoredDirNames       []string
	ModelType             string
	OnProgress            ProgressSink
}

type SkippedItem struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type ImportSamplesResult struct {
	Summary         Summary       `json:"summary"`
	Imported        []string      `json:"imported"`
	Skipped         []SkippedItem `json:"skipped"`
	SampleIncrement int           `json:"sampleIncrement"`
}

type LearnSamplesResult struct {
	Summary             Summary        `json:"summary"`
	Label               string         `json:"label"`
	SourceRoot          string         `json:"sourceRoot"`
	ScannedRoots        []string       `json:"scannedRoots"`
	UsedManagedFallback bool           `json:"usedManagedFallback"`
	RequestedCodeCount  int            `json:"requestedCodeCount"`
	MatchedVideoCount   int            `json:"matchedVideoCount"`
	ImportedSampleCount int            `json:"importedSampleCount"`
	MatchedCodes        []string       `json:"matchedCodes"`
	MissingCodes        []string       `json:"missingCodes"`
	Imported            []string       `json:"imported"`
	Skipped             []SkippedItem  `json:"skipped"`
	HitRate             float64        `json:"hitRate"`
	FalsePositiveRate   float64        `json:"falsePositiveRate"`
	SampleIncrement     int            `json:"sampleIncrement"`
	Observability       map[string]any `json:"observability"`
}

type videoScanResult struct {
	VideoFiles          []string
	ScannedRoots        []string
	UsedManagedFallback bool
}

type tokenCodePair struct {
	Token string
	Code  string
}

func (s *Service) ImportSamples(options ImportSamplesOptions) (ImportSamplesResult, error) {
	label := normalizeSampleLabel(options.Label)
	model := ensureLearningModelShape(s.loadModel(), s.now)
	activeModelType := normalizeAdModelType(firstNonEmpty(options.ModelType, model.Meta.ActiveModel))
	frameSeconds := getModelFrameSeconds(activeModelType)
	model.Meta.ActiveModel = activeModelType

	command, ok := s.resolveFFmpegCommand()
	if !ok {
		return ImportSamplesResult{}, fmt.Errorf("未检测到 FFmpeg，暂时无法导入样本")
	}

	targetList := &model.AdSamples
	if label == "normal" {
		targetList = &model.NormalSamples
	}

	existingHashes := make(map[string]struct{}, len(*targetList))
	for _, item := range *targetList {
		hashBits := strings.TrimSpace(item.HashBits)
		if hashBits != "" {
			existingHashes[hashBits] = struct{}{}
		}
	}

	imported := make([]string, 0)
	skipped := make([]SkippedItem, 0)
	sampleIncrement := 0

	for _, rawPath := range options.SamplePaths {
		samplePath := strings.TrimSpace(rawPath)
		if samplePath == "" {
			continue
		}

		ext := strings.ToLower(filepath.Ext(samplePath))
		_, isImage := imageExtensions[ext]
		_, isVideo := videoExtensions[ext]
		if !isImage && !isVideo {
			skipped = append(skipped, SkippedItem{Path: samplePath, Reason: "仅支持图片或视频样本"})
			continue
		}

		tryImport := func(bits string, frameSecond *int, sourceType string) {
			hashBits := strings.TrimSpace(bits)
			if hashBits == "" {
				return
			}
			if _, exists := existingHashes[hashBits]; exists {
				reason := "样本重复"
				if frameSecond != nil {
					reason = fmt.Sprintf("%ds 抽帧样本重复", *frameSecond)
				}
				skipped = append(skipped, SkippedItem{Path: samplePath, Reason: reason})
				return
			}

			record := buildSampleRecord(buildSampleRecordOptions{
				ID:          buildHashID(samplePath, hashBits, intPointerValue(frameSecond, -1), sampleIncrement, s.now().UnixNano()),
				Label:       label,
				SourcePath:  samplePath,
				SourceType:  sourceType,
				FrameSecond: frameSecond,
				HashBits:    hashBits,
			}, s.now)
			*targetList = append(*targetList, record)
			existingHashes[hashBits] = struct{}{}
			sampleIncrement++
			if frameSecond == nil {
				imported = append(imported, samplePath)
			} else {
				imported = append(imported, fmt.Sprintf("%s @%ds", samplePath, *frameSecond))
			}
			if label == "ad" {
				appendIntroTemplate(&model, introTemplatePayload{
					HashBits:    hashBits,
					SourcePath:  samplePath,
					FrameSecond: frameSecond,
				}, s.now)
			}
		}

		if isImage {
			bits, err := computeImageHash(command, samplePath)
			if err != nil {
				skipped = append(skipped, SkippedItem{Path: samplePath, Reason: err.Error()})
				continue
			}
			if strings.TrimSpace(bits) == "" {
				skipped = append(skipped, SkippedItem{Path: samplePath, Reason: "样本哈希为空"})
				continue
			}
			tryImport(bits, nil, "image")
			continue
		}

		frameEntries := 0
		for _, second := range frameSeconds {
			secondCopy := second
			bits, err := computeVideoFrameHash(command, samplePath, second)
			if err != nil || strings.TrimSpace(bits) == "" {
				continue
			}
			frameEntries++
			tryImport(bits, &secondCopy, "video-frame")
		}
		if frameEntries == 0 {
			skipped = append(skipped, SkippedItem{Path: samplePath, Reason: "无法从视频中抓取有效样本帧"})
		}
	}

	if sampleIncrement > 0 {
		savedModel, err := s.saveModel(model)
		if err != nil {
			return ImportSamplesResult{}, err
		}
		model = savedModel
	}

	return ImportSamplesResult{
		Summary:         s.summarizeModel(model),
		Imported:        imported,
		Skipped:         skipped,
		SampleIncrement: sampleIncrement,
	}, nil
}

func (s *Service) LearnSamplesByCodes(options LearnSamplesOptions) (LearnSamplesResult, error) {
	label := normalizeSampleLabel(options.Label)
	codes := normalizeCodeList(options.Codes)
	sourceRoot := strings.TrimSpace(options.RootPath)
	if sourceRoot == "" {
		return LearnSamplesResult{}, fmt.Errorf("按番号学习时，来源目录不能为空")
	}
	absoluteRoot, err := filepath.Abs(sourceRoot)
	if err != nil {
		return LearnSamplesResult{}, err
	}
	if len(codes) == 0 {
		return LearnSamplesResult{}, fmt.Errorf("请至少输入一个番号")
	}

	rootInfo, err := os.Stat(absoluteRoot)
	if err != nil || !rootInfo.IsDir() {
		return LearnSamplesResult{}, fmt.Errorf("学习来源目录不存在：%s", absoluteRoot)
	}

	command, ok := s.resolveFFmpegCommand()
	if !ok {
		return LearnSamplesResult{}, fmt.Errorf("未检测到 FFmpeg，无法按番号自动抓帧学习")
	}

	s.emitLearningProgress(options.OnProgress, map[string]any{
		"scope":              "learning",
		"phase":              "starting",
		"label":              label,
		"sourceRoot":         absoluteRoot,
		"requestedCodeCount": len(codes),
	})

	model := ensureLearningModelShape(s.loadModel(), s.now)
	activeModelType := normalizeAdModelType(firstNonEmpty(options.ModelType, model.Meta.ActiveModel))
	frameSeconds := getModelFrameSeconds(activeModelType)
	model.Meta.ActiveModel = activeModelType

	targetList := &model.AdSamples
	oppositeList := model.NormalSamples
	if label == "normal" {
		targetList = &model.NormalSamples
		oppositeList = model.AdSamples
	}

	existingHashes := make(map[string]struct{}, len(*targetList))
	for _, item := range *targetList {
		hashBits := strings.TrimSpace(item.HashBits)
		if hashBits != "" {
			existingHashes[hashBits] = struct{}{}
		}
	}

	tokenPairs := buildTokenPairs(codes)
	scanResult := collectVideoFilesWithManagedFallback(absoluteRoot, options.IncludeSubdirectories, options.IgnoredDirNames)
	videoFiles := scanResult.VideoFiles
	scannedRoots := scanResult.ScannedRoots
	usedManagedFallback := scanResult.UsedManagedFallback

	if usedManagedFallback {
		s.emitLearningProgress(options.OnProgress, map[string]any{
			"scope":              "learning",
			"phase":              "managed-fallback",
			"label":              label,
			"sourceRoot":         absoluteRoot,
			"scannedRoots":       scannedRoots,
			"requestedCodeCount": len(codes),
		})
	}

	imported := make([]string, 0)
	skipped := make([]SkippedItem, 0)
	matchedCodes := map[string]struct{}{}
	matchedVideoCount := 0
	potentialFalsePositiveCount := 0

	s.emitLearningProgress(options.OnProgress, map[string]any{
		"scope":               "learning",
		"phase":               "scan-ready",
		"label":               label,
		"sourceRoot":          absoluteRoot,
		"scannedRoots":        scannedRoots,
		"usedManagedFallback": usedManagedFallback,
		"requestedCodeCount":  len(codes),
		"totalVideos":         len(videoFiles),
		"processedVideos":     0,
		"matchedVideoCount":   matchedVideoCount,
		"importedSampleCount": len(imported),
	})

	for index, videoPath := range videoFiles {
		processedVideos := index + 1
		if shouldReportProgress(processedVideos, len(videoFiles), 30) {
			s.emitLearningProgress(options.OnProgress, map[string]any{
				"scope":               "learning",
				"phase":               "matching",
				"label":               label,
				"sourceRoot":          absoluteRoot,
				"scannedRoots":        scannedRoots,
				"usedManagedFallback": usedManagedFallback,
				"requestedCodeCount":  len(codes),
				"totalVideos":         len(videoFiles),
				"processedVideos":     processedVideos,
				"matchedVideoCount":   matchedVideoCount,
				"importedSampleCount": len(imported),
			})
		}

		matchedCode := detectFilmCodeFromPath(videoPath, tokenPairs)
		if matchedCode == "" {
			continue
		}

		matchedVideoCount++
		matchedCodes[matchedCode] = struct{}{}

		videoFrameHashes := make([]string, 0, len(frameSeconds))
		for _, second := range frameSeconds {
			secondCopy := second
			bits, hashErr := computeVideoFrameHash(command, videoPath, second)
			if hashErr != nil || strings.TrimSpace(bits) == "" {
				reason := "抽帧失败"
				if hashErr != nil {
					reason = hashErr.Error()
				}
				skipped = append(skipped, SkippedItem{
					Path:   videoPath,
					Reason: fmt.Sprintf("%ds 抽帧失败：%s", second, reason),
				})
				continue
			}

			videoFrameHashes = append(videoFrameHashes, bits)
			if _, exists := existingHashes[bits]; exists {
				skipped = append(skipped, SkippedItem{
					Path:   videoPath,
					Reason: fmt.Sprintf("%ds 抽帧与已有%s样本重复", second, sampleLabelText(label)),
				})
				continue
			}

			record := buildSampleRecord(buildSampleRecordOptions{
				ID:          buildHashID(videoPath, matchedCode, second, bits, len(*targetList), s.now().UnixNano()),
				Label:       label,
				SourcePath:  videoPath,
				SourceType:  "video-frame",
				FilmCode:    matchedCode,
				FrameSecond: &secondCopy,
				HashBits:    bits,
			}, s.now)
			*targetList = append(*targetList, record)
			existingHashes[bits] = struct{}{}
			imported = append(imported, fmt.Sprintf("%s @%ds", videoPath, second))

			if label == "ad" {
				appendIntroTemplate(&model, introTemplatePayload{
					HashBits:    bits,
					SourcePath:  videoPath,
					FrameSecond: &secondCopy,
					FilmCode:    matchedCode,
				}, s.now)
			}
		}

		oppositeMatch := findBestSampleMatch(videoFrameHashes, oppositeList)
		if oppositeMatch != nil && oppositeMatch.Distance <= model.Thresholds.HighSimilarityDistance {
			potentialFalsePositiveCount++
		}

		s.emitLearningProgress(options.OnProgress, map[string]any{
			"scope":               "learning",
			"phase":               "learning",
			"label":               label,
			"sourceRoot":          absoluteRoot,
			"scannedRoots":        scannedRoots,
			"usedManagedFallback": usedManagedFallback,
			"requestedCodeCount":  len(codes),
			"totalVideos":         len(videoFiles),
			"processedVideos":     processedVideos,
			"matchedVideoCount":   matchedVideoCount,
			"importedSampleCount": len(imported),
			"currentCode":         matchedCode,
		})
	}

	matchedCodeList := make([]string, 0, len(matchedCodes))
	for _, code := range codes {
		if _, exists := matchedCodes[code]; exists {
			matchedCodeList = append(matchedCodeList, code)
		}
	}
	sort.Strings(matchedCodeList)

	missingCodes := make([]string, 0)
	for _, code := range codes {
		if _, exists := matchedCodes[code]; !exists {
			missingCodes = append(missingCodes, code)
		}
	}
	sort.Strings(missingCodes)

	hitRate := 0.0
	if len(codes) > 0 {
		hitRate = float64(matchedVideoCount) / float64(len(codes)) * 100
	}
	falsePositiveRate := 0.0
	if matchedVideoCount > 0 {
		falsePositiveRate = float64(potentialFalsePositiveCount) / float64(matchedVideoCount) * 100
	}
	sampleIncrement := len(imported)

	model.Metrics.TotalLearningRuns++
	model.Metrics.LastLearning = map[string]any{
		"at":                          s.now().Format(time.RFC3339),
		"label":                       label,
		"rootPath":                    absoluteRoot,
		"scannedRoots":                scannedRoots,
		"usedManagedFallback":         usedManagedFallback,
		"requestedCodeCount":          len(codes),
		"matchedVideoCount":           matchedVideoCount,
		"missingCodeCount":            len(missingCodes),
		"sampleIncrement":             sampleIncrement,
		"hitRate":                     hitRate,
		"falsePositiveRate":           falsePositiveRate,
		"potentialFalsePositiveCount": potentialFalsePositiveCount,
	}

	savedModel, err := s.saveModel(model)
	if err != nil {
		return LearnSamplesResult{}, err
	}
	model = savedModel

	s.emitLearningProgress(options.OnProgress, map[string]any{
		"scope":               "learning",
		"phase":               "completed",
		"label":               label,
		"sourceRoot":          absoluteRoot,
		"scannedRoots":        scannedRoots,
		"usedManagedFallback": usedManagedFallback,
		"requestedCodeCount":  len(codes),
		"totalVideos":         len(videoFiles),
		"processedVideos":     len(videoFiles),
		"matchedVideoCount":   matchedVideoCount,
		"importedSampleCount": len(imported),
		"missingCodeCount":    len(missingCodes),
		"hitRate":             hitRate,
		"falsePositiveRate":   falsePositiveRate,
		"sampleIncrement":     sampleIncrement,
	})

	return LearnSamplesResult{
		Summary:             s.summarizeModel(model),
		Label:               label,
		SourceRoot:          absoluteRoot,
		ScannedRoots:        scannedRoots,
		UsedManagedFallback: usedManagedFallback,
		RequestedCodeCount:  len(codes),
		MatchedVideoCount:   matchedVideoCount,
		ImportedSampleCount: len(imported),
		MatchedCodes:        matchedCodeList,
		MissingCodes:        missingCodes,
		Imported:            imported,
		Skipped:             skipped,
		HitRate:             hitRate,
		FalsePositiveRate:   falsePositiveRate,
		SampleIncrement:     sampleIncrement,
		Observability: map[string]any{
			"hitRate":                     hitRate,
			"falsePositiveRate":           falsePositiveRate,
			"sampleIncrement":             sampleIncrement,
			"potentialFalsePositiveCount": potentialFalsePositiveCount,
		},
	}, nil
}

func computeImageHash(command string, filePath string) (string, error) {
	raw, err := execFFmpegBuffer(command, []string{
		"-v", "error",
		"-i", filePath,
		"-frames:v", "1",
		"-vf", "scale=8:8,format=gray",
		"-f", "rawvideo",
		"-",
	}, 12*time.Second)
	if err != nil {
		return "", err
	}
	return toPerceptualBits(raw), nil
}

type buildSampleRecordOptions struct {
	ID          string
	Label       string
	SourcePath  string
	SourceType  string
	FilmCode    string
	FrameSecond *int
	HashBits    string
}

func buildSampleRecord(options buildSampleRecordOptions, now func() time.Time) SampleRecord {
	return SampleRecord{
		ID:          strings.TrimSpace(options.ID),
		Label:       strings.TrimSpace(options.Label),
		SourceType:  strings.TrimSpace(options.SourceType),
		SourcePath:  strings.TrimSpace(options.SourcePath),
		FilmCode:    strings.TrimSpace(options.FilmCode),
		FrameSecond: options.FrameSecond,
		HashBits:    strings.TrimSpace(options.HashBits),
		HashHex:     hashBitsToHex(options.HashBits),
		AddedAt:     now().Format(time.RFC3339),
	}
}

type introTemplatePayload struct {
	HashBits    string
	SourcePath  string
	FrameSecond *int
	FilmCode    string
}

func appendIntroTemplate(model *LearningModel, payload introTemplatePayload, now func() time.Time) {
	if model == nil {
		return
	}
	hashBits := strings.TrimSpace(payload.HashBits)
	if hashBits == "" {
		return
	}
	for _, item := range model.IntroTemplates {
		if strings.TrimSpace(item.HashBits) == hashBits {
			return
		}
	}
	model.IntroTemplates = append(model.IntroTemplates, SampleRecord{
		ID:          buildHashID("intro", hashBits, payload.SourcePath, intPointerValue(payload.FrameSecond, -1), len(model.IntroTemplates), now().UnixNano()),
		HashBits:    hashBits,
		HashHex:     hashBitsToHex(hashBits),
		SourcePath:  strings.TrimSpace(payload.SourcePath),
		FrameSecond: payload.FrameSecond,
		FilmCode:    strings.TrimSpace(payload.FilmCode),
		AddedAt:     now().Format(time.RFC3339),
	})
}

func buildHashID(parts ...any) string {
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		segments = append(segments, fmt.Sprint(part))
	}
	sum := sha1.Sum([]byte(strings.Join(segments, "|")))
	return fmt.Sprintf("%x", sum[:])[:12]
}

func normalizeSampleLabel(value string) string {
	if strings.TrimSpace(value) == "normal" {
		return "normal"
	}
	return "ad"
}

func sampleLabelText(label string) string {
	if normalizeSampleLabel(label) == "normal" {
		return "正常"
	}
	return "广告"
}

func intPointerValue(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

func normalizeDirName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func buildIgnoredDirSet(rawNames []string, includeManagedDirs bool) map[string]struct{} {
	result := map[string]struct{}{}
	if !includeManagedDirs {
		for key := range managedDirNames {
			result[key] = struct{}{}
		}
	}
	for key := range defaultIgnoredDirNames {
		result[key] = struct{}{}
	}
	for _, rawName := range rawNames {
		if normalized := normalizeDirName(rawName); normalized != "" {
			result[normalized] = struct{}{}
		}
	}
	return result
}

func collectVideoFilesWithManagedFallback(sourceRoot string, includeSubdirectories bool, ignoredDirNames []string) videoScanResult {
	absoluteRoot := sourceRoot
	baseFiles := collectVideoFiles(absoluteRoot, includeSubdirectories, buildIgnoredDirSet(ignoredDirNames, false))
	if len(baseFiles) > 0 {
		return videoScanResult{
			VideoFiles:          baseFiles,
			ScannedRoots:        []string{absoluteRoot},
			UsedManagedFallback: false,
		}
	}

	fallbackRoots := []string{
		filepath.Join(absoluteRoot, "待整理"),
		filepath.Join(absoluteRoot, "含开头广告"),
		filepath.Join(absoluteRoot, "待删除"),
	}
	existingFallbackRoots := make([]string, 0, len(fallbackRoots))
	for _, candidate := range fallbackRoots {
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			existingFallbackRoots = append(existingFallbackRoots, candidate)
		}
	}
	if len(existingFallbackRoots) == 0 {
		return videoScanResult{
			VideoFiles:          baseFiles,
			ScannedRoots:        []string{absoluteRoot},
			UsedManagedFallback: false,
		}
	}

	ignoredWithManaged := buildIgnoredDirSet(ignoredDirNames, true)
	seen := map[string]struct{}{}
	fallbackFiles := make([]string, 0)
	for _, root := range existingFallbackRoots {
		files := collectVideoFiles(root, includeSubdirectories, ignoredWithManaged)
		for _, filePath := range files {
			key := strings.ToLower(filepath.Clean(filePath))
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			fallbackFiles = append(fallbackFiles, filePath)
		}
	}
	sort.Slice(fallbackFiles, func(i int, j int) bool {
		return strings.ToLower(fallbackFiles[i]) < strings.ToLower(fallbackFiles[j])
	})

	if len(fallbackFiles) == 0 {
		return videoScanResult{
			VideoFiles:          baseFiles,
			ScannedRoots:        []string{absoluteRoot},
			UsedManagedFallback: false,
		}
	}
	return videoScanResult{
		VideoFiles:          fallbackFiles,
		ScannedRoots:        existingFallbackRoots,
		UsedManagedFallback: true,
	}
}

func collectVideoFiles(root string, includeSubdirectories bool, ignoredDirSet map[string]struct{}) []string {
	files := make([]string, 0)
	var walk func(string)
	walk = func(currentPath string) {
		entries, err := os.ReadDir(currentPath)
		if err != nil {
			return
		}
		sort.Slice(entries, func(i int, j int) bool {
			return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
		})
		for _, entry := range entries {
			entryPath := filepath.Join(currentPath, entry.Name())
			if entry.IsDir() {
				if _, ignored := ignoredDirSet[normalizeDirName(entry.Name())]; ignored {
					continue
				}
				if !includeSubdirectories {
					continue
				}
				walk(entryPath)
				continue
			}
			if !entry.Type().IsRegular() {
				continue
			}
			if _, ok := videoExtensions[strings.ToLower(filepath.Ext(entry.Name()))]; ok {
				files = append(files, entryPath)
			}
		}
	}
	walk(root)
	sort.Slice(files, func(i int, j int) bool {
		return strings.ToLower(files[i]) < strings.ToLower(files[j])
	})
	return files
}

func normalizeCodeList(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0)
	for _, value := range values {
		for _, item := range codeListSplitPattern.Split(strings.TrimSpace(value), -1) {
			code := normalizeFilmCode(item)
			if code == "" {
				continue
			}
			if _, exists := seen[code]; exists {
				continue
			}
			seen[code] = struct{}{}
			result = append(result, code)
		}
	}
	sort.Strings(result)
	return result
}

func normalizeFilmCode(rawValue string) string {
	compactValue := strings.ToUpper(strings.TrimSpace(rawValue))
	compactValue = strings.Join(strings.Fields(compactValue), "-")
	compactValue = strings.ReplaceAll(compactValue, "_", "-")
	for strings.Contains(compactValue, "--") {
		compactValue = strings.ReplaceAll(compactValue, "--", "-")
	}
	matches := codeNormalizePattern.FindStringSubmatch(compactValue)
	if len(matches) == 4 {
		return matches[1] + "-" + matches[2] + matches[3]
	}
	return compactValue
}

func normalizeCodeToken(value string) string {
	return codeTokenPattern.ReplaceAllString(normalizeFilmCode(value), "")
}

func buildTokenPairs(codes []string) []tokenCodePair {
	pairs := make([]tokenCodePair, 0, len(codes))
	for _, code := range codes {
		token := normalizeCodeToken(code)
		if token == "" {
			continue
		}
		pairs = append(pairs, tokenCodePair{Token: token, Code: code})
	}
	sort.Slice(pairs, func(i int, j int) bool {
		if len(pairs[i].Token) == len(pairs[j].Token) {
			return pairs[i].Token < pairs[j].Token
		}
		return len(pairs[i].Token) > len(pairs[j].Token)
	})
	return pairs
}

func detectFilmCodeFromPath(filePath string, tokenPairs []tokenCodePair) string {
	fileName := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	compact := strings.ToUpper(fileName)
	compact = codeTokenPattern.ReplaceAllString(compact, "")
	if compact == "" {
		return ""
	}
	for _, item := range tokenPairs {
		if item.Token != "" && strings.Contains(compact, item.Token) {
			return item.Code
		}
	}
	return ""
}

func shouldReportProgress(processed int, total int, step int) bool {
	if total <= 0 {
		return false
	}
	if processed <= 1 || processed >= total {
		return true
	}
	if step < 1 {
		step = 1
	}
	return processed%step == 0
}

func (s *Service) emitLearningProgress(sink ProgressSink, payload map[string]any) {
	if sink == nil {
		return
	}
	next := map[string]any{}
	for key, value := range payload {
		next[key] = value
	}
	if _, exists := next["timestamp"]; !exists {
		next["timestamp"] = s.now().Format(time.RFC3339)
	}
	sink(next)
}
